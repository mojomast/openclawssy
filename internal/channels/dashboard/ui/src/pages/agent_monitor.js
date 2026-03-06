const monitorState = {
  container: null,
  apiClient: null,
  availableAgents: ["default"],
  agentSummaries: {},
  runs: [],
  loading: false,
  error: "",
  promptDraft: "",
  thinkingMode: "never",
  actionStatus: "",
  actionError: "",
  pollTimer: 0,
};

function safeText(value) {
  return String(value || "").trim();
}

function compactText(value, maxChars = 80) {
  const text = safeText(value).replace(/\s+/g, " ");
  if (!text) {
    return "";
  }
  if (text.length <= maxChars) {
    return text;
  }
  return `${text.slice(0, Math.max(0, maxChars - 3))}...`;
}

function formatDateTime(value) {
  if (!value) {
    return "-";
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return "-";
  }
  return parsed.toLocaleString();
}

function isActiveRoute() {
  return String(window.location.hash || "").replace(/^#/, "").startsWith("/agent-monitor");
}

async function loadMonitorData() {
  monitorState.loading = true;
  monitorState.error = "";
  renderAgentMonitorPage();
  try {
    const [agentPayload, runPayload] = await Promise.all([
      monitorState.apiClient.get("/api/admin/agents?channel=dashboard&user_id=dashboard_user&room_id=dashboard"),
      monitorState.apiClient.get("/api/admin/monitor/runs?limit=120"),
    ]);
    monitorState.availableAgents = Array.isArray(agentPayload?.agents)
      ? agentPayload.agents.map((item) => safeText(item)).filter((item) => item)
      : ["default"];
    monitorState.agentSummaries = agentPayload?.agent_summaries && typeof agentPayload.agent_summaries === "object"
      ? agentPayload.agent_summaries
      : {};
    monitorState.runs = Array.isArray(runPayload?.runs) ? runPayload.runs : [];
  } catch (error) {
    monitorState.error = error instanceof Error ? error.message : String(error);
  } finally {
    monitorState.loading = false;
    renderAgentMonitorPage();
  }
}

function startPolling() {
  if (monitorState.pollTimer) {
    return;
  }
  monitorState.pollTimer = window.setInterval(() => {
    if (!isActiveRoute()) {
      return;
    }
    void loadMonitorData();
  }, 2500);
}

async function startAgent(agentID) {
  const message = safeText(monitorState.promptDraft);
  if (!message) {
    monitorState.actionError = "Enter a prompt before starting an agent run.";
    renderAgentMonitorPage();
    return;
  }
  monitorState.actionError = "";
  monitorState.actionStatus = `Starting ${agentID}...`;
  renderAgentMonitorPage();
  try {
    const payload = await monitorState.apiClient.post("/v1/runs", {
      agent_id: agentID,
      message,
      thinking_mode: monitorState.thinkingMode,
    });
    monitorState.actionStatus = `Started ${agentID} (${safeText(payload?.id) || "queued"}).`;
    await loadMonitorData();
  } catch (error) {
    monitorState.actionError = error instanceof Error ? error.message : String(error);
    renderAgentMonitorPage();
  }
}

async function stopRun(runID) {
  monitorState.actionError = "";
  monitorState.actionStatus = `Stopping ${runID}...`;
  renderAgentMonitorPage();
  try {
    const payload = await monitorState.apiClient.post("/api/admin/monitor/runs/control", {
      action: "cancel",
      run_id: runID,
    });
    monitorState.actionStatus = payload?.cancelled ? `Cancellation requested for ${runID}.` : `Run ${runID} is no longer active.`;
    await loadMonitorData();
  } catch (error) {
    monitorState.actionError = error instanceof Error ? error.message : String(error);
    renderAgentMonitorPage();
  }
}

function summarizeRuns(agentID, role) {
  return monitorState.runs.filter((run) => safeText(run.agent_id) === agentID && safeText(run.role) === role);
}

function renderAgentMonitorPage() {
  const container = monitorState.container;
  if (!container || !container.isConnected) {
    return;
  }
  container.innerHTML = "";

  const heading = document.createElement("h2");
  heading.textContent = "Agent Monitor";
  const note = document.createElement("p");
  note.className = "muted";
  note.textContent = "Polls audit-backed internal runs so you can watch main agents and subagents, launch new work, and cancel active runs.";

  const controls = document.createElement("section");
  controls.className = "settings-section";
  const promptField = document.createElement("label");
  promptField.className = "settings-field";
  const promptTitle = document.createElement("span");
  promptTitle.className = "settings-field-title";
  promptTitle.textContent = "Launch prompt";
  const promptInput = document.createElement("textarea");
  promptInput.className = "settings-textarea";
  promptInput.rows = 4;
  promptInput.placeholder = "Describe the work to start for any agent card below.";
  promptInput.value = monitorState.promptDraft;
  promptInput.addEventListener("input", () => {
    monitorState.promptDraft = promptInput.value;
  });
  promptField.append(promptTitle, promptInput);

  const actionRow = document.createElement("div");
  actionRow.className = "chat-composer-actions";
  const thinkingSelect = document.createElement("select");
  thinkingSelect.className = "settings-select";
  ["never", "on_error", "always"].forEach((mode) => {
    const option = document.createElement("option");
    option.value = mode;
    option.textContent = `thinking: ${mode}`;
    option.selected = monitorState.thinkingMode === mode;
    thinkingSelect.append(option);
  });
  thinkingSelect.addEventListener("change", () => {
    monitorState.thinkingMode = thinkingSelect.value;
  });
  const refreshButton = document.createElement("button");
  refreshButton.type = "button";
  refreshButton.className = "layout-toggle";
  refreshButton.textContent = monitorState.loading ? "Refreshing..." : "Refresh now";
  refreshButton.disabled = monitorState.loading;
  refreshButton.addEventListener("click", () => {
    void loadMonitorData();
  });
  actionRow.append(thinkingSelect, refreshButton);
  controls.append(promptField, actionRow);

  if (monitorState.actionStatus) {
    const status = document.createElement("p");
    status.className = "muted";
    status.textContent = monitorState.actionStatus;
    controls.append(status);
  }
  if (monitorState.actionError) {
    const status = document.createElement("p");
    status.className = "chat-send-error";
    status.textContent = monitorState.actionError;
    controls.append(status);
  }
  if (monitorState.error) {
    const status = document.createElement("p");
    status.className = "chat-send-error";
    status.textContent = `Monitor load failed: ${monitorState.error}`;
    controls.append(status);
  }

  const summary = document.createElement("p");
  summary.className = "muted";
  const runningCount = monitorState.runs.filter((run) => safeText(run.status) === "running").length;
  const subagentCount = monitorState.runs.filter((run) => safeText(run.role) === "subagent").length;
  summary.textContent = `${monitorState.runs.length} recent internal runs tracked · ${runningCount} active · ${subagentCount} subagent runs.`;

  const cards = document.createElement("div");
  cards.className = "widget-list";
  monitorState.availableAgents.forEach((agentID) => {
    const mainRuns = summarizeRuns(agentID, "main");
    const subRuns = summarizeRuns(agentID, "subagent");
    const activeRun = mainRuns.concat(subRuns).find((run) => safeText(run.status) === "running");
    const summaryState = monitorState.agentSummaries?.[agentID] || {};
    const card = document.createElement("section");
    card.className = "chat-activity-card";
    const title = document.createElement("h4");
    title.textContent = agentID;
    const meta = document.createElement("p");
    meta.className = "muted";
    meta.textContent = `main: ${mainRuns.length} · subagent: ${subRuns.length} · active: ${activeRun ? safeText(activeRun.run_id) : "none"}`;
    const profile = document.createElement("p");
    profile.className = "muted";
    const skills = Array.isArray(summaryState.activated_skills) ? summaryState.activated_skills.join(", ") : "";
    const model = [safeText(summaryState.model_provider), safeText(summaryState.model_name)].filter((item) => item).join("/");
    profile.textContent = `self-improvement: ${summaryState.self_improvement_ready ? "ready" : "off"} · skills: ${skills || "none"}${model ? ` · model: ${model}` : ""}`;
    const latestMain = document.createElement("p");
    latestMain.className = "muted";
    latestMain.textContent = mainRuns.length
      ? `Latest main: ${safeText(mainRuns[0].status)} · ${compactText(mainRuns[0].task_id || mainRuns[0].message || "-")}`
      : "Latest main: none";
    const latestSub = document.createElement("p");
    latestSub.className = "muted";
    latestSub.textContent = subRuns.length
      ? `Latest subagent: ${safeText(subRuns[0].status)} · ${compactText(subRuns[0].task_id || subRuns[0].message || "-")}`
      : "Latest subagent: none";
    const activeMeta = document.createElement("p");
    activeMeta.className = "muted";
    activeMeta.textContent = activeRun
      ? `Active detail: ${compactText(activeRun.task_id || activeRun.message || activeRun.run_id)} · ${formatDateTime(activeRun.started_at || activeRun.completed_at)}`
      : "Active detail: none";
    const checkpointMeta = document.createElement("p");
    checkpointMeta.className = "muted";
    const latestCheckpointRun = mainRuns.concat(subRuns).find((run) => safeText(run.checkpoint_path));
    checkpointMeta.textContent = latestCheckpointRun
      ? `Latest checkpoint: ${compactText(latestCheckpointRun.checkpoint_path, 72)}`
      : "Latest checkpoint: none";
    const actions = document.createElement("div");
    actions.className = "chat-composer-actions";
    const startButton = document.createElement("button");
    startButton.type = "button";
    startButton.className = "layout-toggle";
    startButton.textContent = "Start";
    startButton.addEventListener("click", () => {
      void startAgent(agentID);
    });
    const stopButton = document.createElement("button");
    stopButton.type = "button";
    stopButton.className = "layout-toggle";
    stopButton.textContent = activeRun ? "Stop active" : "No active run";
    stopButton.disabled = !activeRun;
    stopButton.addEventListener("click", () => {
      if (activeRun) {
        void stopRun(safeText(activeRun.run_id));
      }
    });
    actions.append(startButton, stopButton);
    card.append(title, meta, profile, latestMain, latestSub, activeMeta, checkpointMeta, actions);
    cards.append(card);
  });

  const timeline = document.createElement("section");
  timeline.className = "settings-section";
  const timelineTitle = document.createElement("h3");
  timelineTitle.textContent = "Recent Main + Subagent Runs";
  timeline.append(timelineTitle);
  if (!monitorState.runs.length) {
    const empty = document.createElement("p");
    empty.className = "muted";
    empty.textContent = monitorState.loading ? "Loading monitor data..." : "No audit-backed runs found yet.";
    timeline.append(empty);
  } else {
    const table = document.createElement("table");
    table.className = "settings-diff-table";
    table.innerHTML = "<thead><tr><th>Agent</th><th>Role</th><th>Status</th><th>Task</th><th>Model</th><th>Checkpoint</th><th>Error</th><th>Started</th><th>Run ID</th><th>Action</th></tr></thead>";
    const body = document.createElement("tbody");
    monitorState.runs.forEach((run) => {
      const row = document.createElement("tr");
      const stopCell = document.createElement("td");
      if (safeText(run.status) === "running") {
        const button = document.createElement("button");
        button.type = "button";
        button.className = "layout-toggle";
        button.textContent = "Stop";
        button.addEventListener("click", () => {
          void stopRun(safeText(run.run_id));
        });
        stopCell.append(button);
      } else {
        stopCell.textContent = "-";
      }
      const taskText = compactText(run.task_id || run.message || run.source || "-", 56);
      const modelText = compactText([safeText(run.model_provider), safeText(run.model_name)].filter((item) => item).join("/"), 40) || "-";
      const checkpointText = compactText(run.checkpoint_path || "-", 48);
      const errorText = compactText(run.error || "-", 56);
      row.innerHTML = `<td>${safeText(run.agent_id) || "-"}</td><td>${safeText(run.role) || "main"}</td><td>${safeText(run.status) || "-"}</td><td>${taskText}</td><td>${modelText}</td><td>${checkpointText}</td><td>${errorText}</td><td>${formatDateTime(run.started_at || run.completed_at)}</td><td><code>${safeText(run.run_id) || "-"}</code></td>`;
      row.append(stopCell);
      body.append(row);
    });
    table.append(body);
    timeline.append(table);
  }

  container.append(heading, note, controls, summary, cards, timeline);
}

export const agentMonitorPage = {
  key: "agent-monitor",
  title: "Agent Monitor",
  async render({ container, apiClient }) {
    monitorState.container = container;
    monitorState.apiClient = apiClient;
    startPolling();
    renderAgentMonitorPage();
    await loadMonitorData();
  },
};
