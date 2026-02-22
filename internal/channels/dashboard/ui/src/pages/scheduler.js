const schedulerState = {
  container: null,
  apiClient: null,
  jobs: [],
  paused: false,
  loading: false,
  loadError: "",
  notice: "",
  noticeKind: "",
  pending: new Set(),
  deleteConfirm: null,
  addForm: {
    id: "",
    agentID: "",
    schedule: "",
    message: "",
    includeEnabled: false,
    enabled: true,
  },
};

function rerender() {
  if (!schedulerState.container || !schedulerState.container.isConnected) {
    return;
  }
  renderSchedulerPage();
}

function extractErrorMessage(error) {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return String(error || "Unknown scheduler error");
}

function normalizeJobsPayload(payload) {
  const paused = !!payload?.paused;
  const jobs = Array.isArray(payload?.jobs) ? payload.jobs : [];
  const normalizedJobs = jobs
    .filter((job) => job && typeof job === "object")
    .map((job) => ({
      id: String(job.id || "").trim(),
      agentID: String(job.agentID || job.agent_id || "default").trim() || "default",
      schedule: String(job.schedule || "").trim(),
      message: String(job.message || "").trim(),
      enabled: !!job.enabled,
      lastRun: String(job.lastRun || job.last_run || "").trim(),
    }))
    .sort((left, right) => left.id.localeCompare(right.id));
  return { paused, jobs: normalizedJobs };
}

async function loadJobs(options = {}) {
  const { keepNotice = false } = options;
  schedulerState.loading = true;
  schedulerState.loadError = "";
  if (!keepNotice) {
    schedulerState.notice = "";
    schedulerState.noticeKind = "";
  }
  rerender();

  try {
    const payload = await schedulerState.apiClient.get("/api/admin/scheduler/jobs");
    const normalized = normalizeJobsPayload(payload);
    schedulerState.paused = normalized.paused;
    schedulerState.jobs = normalized.jobs;
  } catch (error) {
    schedulerState.loadError = extractErrorMessage(error);
  } finally {
    schedulerState.loading = false;
    rerender();
  }
}

async function runAction(actionKey, task) {
  schedulerState.pending.add(actionKey);
  schedulerState.notice = "";
  schedulerState.noticeKind = "";
  rerender();
  try {
    await task();
  } finally {
    schedulerState.pending.delete(actionKey);
    rerender();
  }
}

async function addJob() {
  const id = String(schedulerState.addForm.id || "").trim();
  const agentID = String(schedulerState.addForm.agentID || "").trim();
  const schedule = String(schedulerState.addForm.schedule || "").trim();
  const message = String(schedulerState.addForm.message || "").trim();

  if (!schedule || !message) {
    schedulerState.noticeKind = "error";
    schedulerState.notice = "Schedule and message are required.";
    rerender();
    return;
  }

  const body = { schedule, message };
  if (id) {
    body.id = id;
  }
  if (agentID) {
    body.agent_id = agentID;
  }
  if (schedulerState.addForm.includeEnabled) {
    body.enabled = !!schedulerState.addForm.enabled;
  }

  await runAction("add", async () => {
    try {
      const payload = await schedulerState.apiClient.post("/api/admin/scheduler/jobs", body);
      const createdID = String(payload?.id || id || "").trim();
      schedulerState.noticeKind = "success";
      schedulerState.notice = createdID ? `Added scheduler job: ${createdID}` : "Added scheduler job.";
      schedulerState.addForm.id = "";
      schedulerState.addForm.agentID = "";
      schedulerState.addForm.schedule = "";
      schedulerState.addForm.message = "";
      schedulerState.addForm.includeEnabled = false;
      schedulerState.addForm.enabled = true;
      await loadJobs({ keepNotice: true });
    } catch (error) {
      schedulerState.noticeKind = "error";
      schedulerState.notice = `Failed to add job: ${extractErrorMessage(error)}`;
    }
  });
}

async function setGlobalPaused(paused) {
  const action = paused ? "pause" : "resume";
  await runAction(`global:${action}`, async () => {
    try {
      await schedulerState.apiClient.post("/api/admin/scheduler/control", { action });
      schedulerState.noticeKind = "success";
      schedulerState.notice = paused ? "Scheduler paused globally." : "Scheduler resumed globally.";
      await loadJobs({ keepNotice: true });
    } catch (error) {
      schedulerState.noticeKind = "error";
      schedulerState.notice = `Failed to ${action} scheduler: ${extractErrorMessage(error)}`;
    }
  });
}

async function setJobEnabled(jobID, enabled) {
  const action = enabled ? "resume" : "pause";
  await runAction(`job:${jobID}:${action}`, async () => {
    try {
      await schedulerState.apiClient.post("/api/admin/scheduler/control", {
        action,
        job_id: jobID,
      });
      schedulerState.noticeKind = "success";
      schedulerState.notice = enabled ? `Enabled job: ${jobID}` : `Disabled job: ${jobID}`;
      await loadJobs({ keepNotice: true });
    } catch (error) {
      schedulerState.noticeKind = "error";
      schedulerState.notice = `Failed to update job ${jobID}: ${extractErrorMessage(error)}`;
    }
  });
}

async function deleteJob(jobID) {
  schedulerState.deleteConfirm = null;
  await runAction(`delete:${jobID}`, async () => {
    try {
      await schedulerState.apiClient.delete(`/api/admin/scheduler/jobs/${encodeURIComponent(jobID)}`);
      schedulerState.noticeKind = "success";
      schedulerState.notice = `Deleted job: ${jobID}`;
      await loadJobs({ keepNotice: true });
    } catch (error) {
      schedulerState.noticeKind = "error";
      schedulerState.notice = `Failed to delete job ${jobID}: ${extractErrorMessage(error)}`;
    }
  });
}

function createBanner(text, kind) {
  const banner = document.createElement("p");
  banner.className = `scheduler-banner ${kind === "error" ? "error" : kind === "success" ? "success" : ""}`.trim();
  banner.textContent = text;
  return banner;
}

function createToolbar() {
  const section = document.createElement("section");
  section.className = "scheduler-panel scheduler-toolbar";

  const statusRow = document.createElement("div");
  statusRow.className = "scheduler-status-row";

  const statusText = document.createElement("p");
  statusText.className = "scheduler-status-text";
  statusText.textContent = "Global scheduler state";

  const statusPill = document.createElement("span");
  statusPill.className = `scheduler-status-pill ${schedulerState.paused ? "paused" : "running"}`;
  statusPill.textContent = schedulerState.paused ? "Paused" : "Running";

  statusRow.append(statusText, statusPill);
  section.append(statusRow);

  const actions = document.createElement("div");
  actions.className = "scheduler-actions";

  const reloadButton = document.createElement("button");
  reloadButton.type = "button";
  reloadButton.className = "layout-toggle";
  reloadButton.disabled = schedulerState.loading;
  reloadButton.textContent = schedulerState.loading ? "Refreshing..." : "Refresh jobs";
  reloadButton.addEventListener("click", () => {
    void loadJobs({ keepNotice: true });
  });

  const pauseResumeButton = document.createElement("button");
  pauseResumeButton.type = "button";
  pauseResumeButton.className = "chat-send-button";
  const pauseActionPending = schedulerState.pending.has("global:pause") || schedulerState.pending.has("global:resume");
  pauseResumeButton.disabled = pauseActionPending;
  if (pauseActionPending) {
    pauseResumeButton.textContent = "Saving...";
  } else {
    pauseResumeButton.textContent = schedulerState.paused ? "Resume scheduler" : "Pause scheduler";
  }
  pauseResumeButton.addEventListener("click", () => {
    void setGlobalPaused(!schedulerState.paused);
  });

  actions.append(reloadButton, pauseResumeButton);
  section.append(actions);

  return section;
}

function createAddForm() {
  const section = document.createElement("section");
  section.className = "scheduler-panel";

  const title = document.createElement("h3");
  title.textContent = "Add scheduler job";
  section.append(title);

  const help = document.createElement("p");
  help.className = "muted";
  help.textContent = "Provide a duration schedule (for example @every 5m) or RFC3339 timestamp for one-shot jobs.";
  section.append(help);

  const form = document.createElement("form");
  form.className = "scheduler-form";
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    void addJob();
  });

  const fields = document.createElement("div");
  fields.className = "scheduler-form-grid";

  const idField = document.createElement("label");
  idField.className = "scheduler-field";
  const idLabel = document.createElement("span");
  idLabel.textContent = "Job ID (optional)";
  const idInput = document.createElement("input");
  idInput.type = "text";
  idInput.className = "settings-input";
  idInput.placeholder = "job_custom_id";
  idInput.value = schedulerState.addForm.id;
  idInput.addEventListener("input", () => {
    schedulerState.addForm.id = idInput.value;
  });
  idField.append(idLabel, idInput);

  const agentField = document.createElement("label");
  agentField.className = "scheduler-field";
  const agentLabel = document.createElement("span");
  agentLabel.textContent = "agent_id";
  const agentInput = document.createElement("input");
  agentInput.type = "text";
  agentInput.className = "settings-input";
  agentInput.placeholder = "default";
  agentInput.value = schedulerState.addForm.agentID;
  agentInput.addEventListener("input", () => {
    schedulerState.addForm.agentID = agentInput.value;
  });
  agentField.append(agentLabel, agentInput);

  const scheduleField = document.createElement("label");
  scheduleField.className = "scheduler-field";
  const scheduleLabel = document.createElement("span");
  scheduleLabel.textContent = "schedule";
  const scheduleInput = document.createElement("input");
  scheduleInput.type = "text";
  scheduleInput.className = "settings-input";
  scheduleInput.placeholder = "@every 5m";
  scheduleInput.required = true;
  scheduleInput.value = schedulerState.addForm.schedule;
  scheduleInput.addEventListener("input", () => {
    schedulerState.addForm.schedule = scheduleInput.value;
  });
  scheduleField.append(scheduleLabel, scheduleInput);

  const messageField = document.createElement("label");
  messageField.className = "scheduler-field";
  const messageLabel = document.createElement("span");
  messageLabel.textContent = "message";
  const messageInput = document.createElement("textarea");
  messageInput.className = "settings-textarea";
  messageInput.required = true;
  messageInput.rows = 3;
  messageInput.placeholder = "status ping";
  messageInput.value = schedulerState.addForm.message;
  messageInput.addEventListener("input", () => {
    schedulerState.addForm.message = messageInput.value;
  });
  messageField.append(messageLabel, messageInput);

  fields.append(idField, agentField, scheduleField, messageField);
  form.append(fields);

  const includeEnabledRow = document.createElement("label");
  includeEnabledRow.className = "scheduler-checkbox-row";
  const includeEnabledInput = document.createElement("input");
  includeEnabledInput.type = "checkbox";
  includeEnabledInput.checked = schedulerState.addForm.includeEnabled;
  includeEnabledInput.addEventListener("change", () => {
    schedulerState.addForm.includeEnabled = includeEnabledInput.checked;
    rerender();
  });
  const includeEnabledText = document.createElement("span");
  includeEnabledText.textContent = "Set enabled explicitly (optional field)";
  includeEnabledRow.append(includeEnabledInput, includeEnabledText);
  form.append(includeEnabledRow);

  if (schedulerState.addForm.includeEnabled) {
    const enabledRow = document.createElement("label");
    enabledRow.className = "scheduler-checkbox-row";
    const enabledInput = document.createElement("input");
    enabledInput.type = "checkbox";
    enabledInput.checked = schedulerState.addForm.enabled;
    enabledInput.addEventListener("change", () => {
      schedulerState.addForm.enabled = enabledInput.checked;
    });
    const enabledText = document.createElement("span");
    enabledText.textContent = "enabled";
    enabledRow.append(enabledInput, enabledText);
    form.append(enabledRow);
  }

  const actions = document.createElement("div");
  actions.className = "scheduler-actions";
  const submit = document.createElement("button");
  submit.type = "submit";
  submit.className = "chat-send-button";
  submit.disabled = schedulerState.pending.has("add");
  submit.textContent = schedulerState.pending.has("add") ? "Adding..." : "Add job";
  actions.append(submit);

  form.append(actions);
  section.append(form);

  return section;
}

function createJobsTable() {
  const section = document.createElement("section");
  section.className = "scheduler-panel";

  const head = document.createElement("div");
  head.className = "scheduler-jobs-head";
  const title = document.createElement("h3");
  title.textContent = "Scheduled jobs";
  const count = document.createElement("p");
  count.className = "muted";
  count.textContent = `${schedulerState.jobs.length} total`;
  head.append(title, count);
  section.append(head);

  if (schedulerState.loading) {
    const loading = document.createElement("p");
    loading.className = "muted";
    loading.textContent = "Loading scheduler jobs...";
    section.append(loading);
    return section;
  }

  if (!schedulerState.jobs.length) {
    const empty = document.createElement("p");
    empty.className = "muted";
    empty.textContent = "No jobs found. Add a job to start using scheduler automation.";
    section.append(empty);
    return section;
  }

  const table = document.createElement("table");
  table.className = "runs-table scheduler-jobs-table";

  const headRow = document.createElement("tr");
  ["Job", "Agent", "Schedule", "Message", "Enabled", "Last run", "Actions"].forEach((label) => {
    const cell = document.createElement("th");
    cell.textContent = label;
    headRow.append(cell);
  });
  const thead = document.createElement("thead");
  thead.append(headRow);
  table.append(thead);

  const body = document.createElement("tbody");
  schedulerState.jobs.forEach((job) => {
    const row = document.createElement("tr");

    const idCell = document.createElement("td");
    const code = document.createElement("code");
    code.textContent = job.id;
    idCell.append(code);

    const agentCell = document.createElement("td");
    agentCell.textContent = job.agentID || "default";

    const scheduleCell = document.createElement("td");
    scheduleCell.textContent = job.schedule || "-";

    const messageCell = document.createElement("td");
    messageCell.textContent = job.message || "-";

    const enabledCell = document.createElement("td");
    const enabledPill = document.createElement("span");
    enabledPill.className = `scheduler-status-pill ${job.enabled ? "running" : "paused"}`;
    enabledPill.textContent = job.enabled ? "Enabled" : "Disabled";
    enabledCell.append(enabledPill);

    const lastRunCell = document.createElement("td");
    lastRunCell.textContent = job.lastRun || "Never";

    const actionsCell = document.createElement("td");
    const actionWrap = document.createElement("div");
    actionWrap.className = "scheduler-actions";

    const toggleKey = `job:${job.id}:${job.enabled ? "pause" : "resume"}`;
    const deleteKey = `delete:${job.id}`;

    const toggleButton = document.createElement("button");
    toggleButton.type = "button";
    toggleButton.className = "layout-toggle";
    toggleButton.disabled = schedulerState.pending.has(toggleKey);
    toggleButton.textContent = schedulerState.pending.has(toggleKey)
      ? "Saving..."
      : job.enabled
        ? "Disable"
        : "Enable";
    toggleButton.addEventListener("click", () => {
      void setJobEnabled(job.id, !job.enabled);
    });

    const deleteButton = document.createElement("button");
    deleteButton.type = "button";
    deleteButton.className = "layout-toggle";
    deleteButton.disabled = schedulerState.pending.has(deleteKey);

    const isConfirming = schedulerState.deleteConfirm === job.id;
    if (isConfirming) {
      deleteButton.textContent = "Confirm delete";
      deleteButton.style.color = "var(--Destructive)";
      deleteButton.style.borderColor = "var(--Destructive)";
      deleteButton.style.background = "var(--Destructive-bg)";
    } else {
      deleteButton.textContent = schedulerState.pending.has(deleteKey) ? "Deleting..." : "Delete";
    }

    deleteButton.addEventListener("click", () => {
      if (isConfirming) {
        void deleteJob(job.id);
      } else {
        schedulerState.deleteConfirm = job.id;
        rerender();
      }
    });

    actionWrap.append(toggleButton, deleteButton);
    actionsCell.append(actionWrap);

    row.append(idCell, agentCell, scheduleCell, messageCell, enabledCell, lastRunCell, actionsCell);
    body.append(row);
  });
  table.append(body);

  section.append(table);
  return section;
}

function renderSchedulerPage() {
  const container = schedulerState.container;
  container.innerHTML = "";

  const heading = document.createElement("h2");
  heading.textContent = "Scheduler";
  container.append(heading);

  const subtitle = document.createElement("p");
  subtitle.className = "muted";
  subtitle.textContent = "Manage scheduler jobs, pause or resume globally, and control enabled state per job.";
  container.append(subtitle);

  const page = document.createElement("section");
  page.className = "scheduler-page";
  page.append(createToolbar(), createAddForm());

  if (schedulerState.notice) {
    page.append(createBanner(schedulerState.notice, schedulerState.noticeKind));
  }
  if (schedulerState.loadError) {
    page.append(createBanner(`Failed to load jobs: ${schedulerState.loadError}`, "error"));
  }

  page.append(createJobsTable());
  container.append(page);
}

export const schedulerPage = {
  key: "scheduler",
  title: "Scheduler",
  async render({ container, apiClient }) {
    const firstLoad = schedulerState.container !== container;
    schedulerState.container = container;
    schedulerState.apiClient = apiClient;

    renderSchedulerPage();
    if (firstLoad || !schedulerState.jobs.length) {
      await loadJobs();
    }
  },
};
