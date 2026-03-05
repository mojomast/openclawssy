import { fetchRecentRuns, renderCompactRunsList } from "./runs.js";
import { fetchSchedulerJobs, renderCompactSchedulerJobs } from "./scheduler.js";
import { fetchRecentSessions, renderCompactSessionsList } from "./sessions.js";
import { fetchDocsSummary, renderCompactDocsSummary } from "./docs.js";
import { fetchSkillsSummary, renderCompactSkillsSummary } from "./skills.js";
import { fetchSandboxSummary, renderCompactSandboxSummary } from "./sandbox.js";
import { captureFocusSnapshot, restoreFocusSnapshot } from "../ui/focus_restore.js";

const STORAGE_KEY = "dashboard.custom_dashboards.p1";
const GRID_COLUMNS = 12;
const ROW_HEIGHT = 110;

const dashboardsState = {
  container: null,
  apiClient: null,
  store: null,
  router: null,
  dashboards: [],
  selectedID: "",
  loading: false,
  error: "",
  dirty: false,
  saving: false,
  widgetPickerOpen: false,
  widgetMenuFor: "",
  drag: null,
  saveTimer: null,
  saveNotice: "",
  lastErrorAt: 0,
  knownServerIDs: new Set(),
};

function nowISO() {
  return new Date().toISOString();
}

function uid(prefix) {
  return `${prefix}_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`;
}

function readLocalDashboards() {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    return raw ? JSON.parse(raw) : [];
  } catch (_error) {
    return [];
  }
}

function writeLocalDashboards() {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(dashboardsState.dashboards));
  } catch (_error) {
    // ignore storage failures
  }
}

function markServerKnown(id) {
  const value = String(id || "").trim();
  if (value) {
    dashboardsState.knownServerIDs.add(value);
  }
}

function normalizeDashboard(record) {
  return {
    id: String(record?.id || uid("dash")).trim(),
    name: String(record?.name || "New Dashboard").trim() || "New Dashboard",
    position: Math.max(0, Number(record?.position) || 0),
    created_at: String(record?.created_at || nowISO()),
    updated_at: String(record?.updated_at || nowISO()),
    layout: Array.isArray(record?.layout)
      ? record.layout.map((item) => ({
          widget_key: String(item?.widget_key || "").trim(),
          widget_instance_id: String(item?.widget_instance_id || uid("widget")).trim(),
          x: Math.max(0, Number(item?.x) || 0),
          y: Math.max(0, Number(item?.y) || 0),
          w: Math.max(2, Number(item?.w) || 4),
          h: Math.max(2, Number(item?.h) || 3),
          widget_state: item?.widget_state && typeof item.widget_state === "object" ? item.widget_state : {},
        }))
      : [],
  };
}

function mergeDashboards(localItems, remoteItems) {
  const merged = new Map();
  [...remoteItems, ...localItems].map(normalizeDashboard).forEach((item) => {
    const existing = merged.get(item.id);
    if (!existing || String(item.updated_at) >= String(existing.updated_at)) {
      merged.set(item.id, item);
    }
  });
  return Array.from(merged.values()).sort((a, b) => (a.position - b.position) || a.created_at.localeCompare(b.created_at));
}

function selectedDashboard() {
  return dashboardsState.dashboards.find((item) => item.id === dashboardsState.selectedID) || dashboardsState.dashboards[0] || null;
}

function scheduleSave() {
  if (dashboardsState.saveTimer) {
    window.clearTimeout(dashboardsState.saveTimer);
  }
  dashboardsState.saveTimer = window.setTimeout(() => {
    dashboardsState.saveTimer = null;
    void saveAllDashboards();
  }, 700);
}

function markDirty(options = {}) {
  const { autosave = true } = options;
  dashboardsState.dashboards.forEach((item, index) => {
    item.position = index;
  });
  dashboardsState.dirty = true;
  dashboardsState.saveNotice = autosave ? "Unsaved changes" : dashboardsState.saveNotice;
  writeLocalDashboards();
  rerender({ preserveFocus: true });
  if (autosave) {
    scheduleSave();
  }
}

async function loadDashboards() {
  dashboardsState.loading = true;
  dashboardsState.error = "";
  rerender({ preserveFocus: true });
  try {
    const localItems = readLocalDashboards();
    const payload = await dashboardsState.apiClient.get("/api/admin/dashboards");
    const remoteItems = Array.isArray(payload?.dashboards) ? payload.dashboards : [];
    dashboardsState.knownServerIDs = new Set(remoteItems.map((item) => String(item?.id || "").trim()).filter(Boolean));
    dashboardsState.dashboards = mergeDashboards(localItems, remoteItems);
    if (!dashboardsState.dashboards.length) {
      dashboardsState.dashboards = [normalizeDashboard({ id: uid("dash"), name: "Main Dashboard", layout: [] })];
    }
    dashboardsState.selectedID = dashboardsState.selectedID || dashboardsState.dashboards[0].id;
    writeLocalDashboards();
  } catch (error) {
    dashboardsState.error = error instanceof Error ? error.message : String(error);
    dashboardsState.dashboards = readLocalDashboards().map(normalizeDashboard);
    if (!dashboardsState.dashboards.length) {
      dashboardsState.dashboards = [normalizeDashboard({ id: uid("dash"), name: "Main Dashboard", layout: [] })];
    }
    dashboardsState.selectedID = dashboardsState.selectedID || dashboardsState.dashboards[0].id;
  } finally {
    dashboardsState.loading = false;
    rerender({ preserveFocus: true });
  }
}

async function createDashboard() {
  try {
    const payload = await dashboardsState.apiClient.post("/api/admin/dashboards", { name: `Dashboard ${dashboardsState.dashboards.length + 1}` });
    const created = normalizeDashboard(payload?.dashboard || {});
    markServerKnown(created.id);
    dashboardsState.dashboards = [...dashboardsState.dashboards, created];
    dashboardsState.selectedID = created.id;
    markDirty();
  } catch (error) {
    dashboardsState.error = error instanceof Error ? error.message : String(error);
    rerender({ preserveFocus: true });
  }
}

async function saveAllDashboards() {
  if (dashboardsState.saving) {
    return;
  }
  dashboardsState.saving = true;
  rerender({ preserveFocus: true });
  try {
    for (const dashboard of dashboardsState.dashboards) {
      dashboard.updated_at = nowISO();
      if (!dashboardsState.knownServerIDs.has(dashboard.id)) {
        const previousID = dashboard.id;
        const created = await dashboardsState.apiClient.post("/api/admin/dashboards", { name: dashboard.name });
        const remote = normalizeDashboard(created?.dashboard || {});
        dashboard.id = remote.id;
        dashboard.created_at = remote.created_at;
        markServerKnown(remote.id);
        if (dashboardsState.selectedID === previousID) {
          dashboardsState.selectedID = remote.id;
        }
      }
      await dashboardsState.apiClient.put(`/api/admin/dashboards/${encodeURIComponent(dashboard.id)}`, dashboard);
    }
    dashboardsState.dirty = false;
    dashboardsState.saveNotice = "Saved";
    writeLocalDashboards();
  } catch (error) {
    dashboardsState.error = error instanceof Error ? error.message : String(error);
    dashboardsState.saveNotice = "Save failed";
    dashboardsState.lastErrorAt = Date.now();
  } finally {
    dashboardsState.saving = false;
    rerender({ preserveFocus: true });
  }
}

async function deleteDashboard(id) {
  if (dashboardsState.dashboards.length <= 1) {
    return;
  }
  if (!window.confirm("Delete this custom dashboard?")) {
    return;
  }
  if (dashboardsState.knownServerIDs.has(id)) {
    await dashboardsState.apiClient.delete(`/api/admin/dashboards/${encodeURIComponent(id)}`);
    dashboardsState.knownServerIDs.delete(id);
  }
  dashboardsState.dashboards = dashboardsState.dashboards.filter((item) => item.id !== id);
  dashboardsState.selectedID = dashboardsState.dashboards[0]?.id || "";
  markDirty();
}

function updateDashboard(mutator) {
  const dashboard = selectedDashboard();
  if (!dashboard) {
    return;
  }
  mutator(dashboard);
  dashboard.updated_at = nowISO();
  markDirty();
}

function updateDashboardLocal(mutator) {
  const dashboard = selectedDashboard();
  if (!dashboard) {
    return;
  }
  mutator(dashboard);
  dashboard.updated_at = nowISO();
  dashboardsState.dirty = true;
  writeLocalDashboards();
  rerender({ preserveFocus: true });
}

function removeWidget(widgetInstanceID) {
  updateDashboard((next) => {
    next.layout = next.layout.filter((item) => item.widget_instance_id !== widgetInstanceID);
  });
}

async function fetchConfig() {
  return dashboardsState.apiClient.get("/api/admin/config");
}

async function fetchAdminStatus() {
	return dashboardsState.apiClient.get("/api/admin/status");
}

async function fetchSecretsKeys() {
  const payload = await dashboardsState.apiClient.get("/api/admin/secrets");
  return Array.isArray(payload?.keys) ? payload.keys : [];
}

function renderKeyValueList(container, rows) {
	container.innerHTML = "";
	const list = document.createElement("div");
	list.className = "widget-list";
	rows.forEach((item) => {
		const row = document.createElement("div");
		row.className = "widget-list-item static";
		row.innerHTML = `<strong>${item.label}</strong><span>${item.value}</span>`;
		list.append(row);
	});
	container.append(list);
}

async function sendQuickPrompt(message, agentID = "default") {
  return dashboardsState.apiClient.post("/v1/chat/messages", {
    user_id: "dashboard_user",
    room_id: "dashboard",
    agent_id: agentID,
    message,
  });
}

function widgetSpec({ key, label, description, sourcePath, defaultW, defaultH, render, configure }) {
  return { key, label, description, sourcePath, defaultW, defaultH, render, configure };
}

const WIDGETS = [
  widgetSpec({
    key: "runs.recent",
    label: "Runs: Recent",
    description: "Compact recent runs list.",
    sourcePath: "/runs",
    defaultW: 6,
    defaultH: 3,
    async render({ body, widgetState, router }) {
      const runs = await fetchRecentRuns(dashboardsState.apiClient, widgetState.limit || 5);
      renderCompactRunsList(body, runs, { limit: widgetState.limit || 5, onOpenRun: (run) => router.navigate(`/runs`) });
    },
    configure({ widget }) {
      const next = Number(window.prompt("How many recent runs?", String(widget.widget_state.limit || 5)) || 5);
      widget.widget_state.limit = Math.max(1, Math.min(20, next || 5));
    },
  }),
  widgetSpec({
    key: "scheduler.jobs",
    label: "Scheduler: Jobs",
    description: "Compact scheduler job list.",
    sourcePath: "/scheduler",
    defaultW: 6,
    defaultH: 3,
    async render({ body }) {
      const payload = await fetchSchedulerJobs(dashboardsState.apiClient);
      renderCompactSchedulerJobs(body, payload.jobs, { limit: 6 });
    },
  }),
  widgetSpec({
    key: "runtime.status",
    label: "Runtime Status",
    description: "Provider, model, and run count.",
    sourcePath: "/chat",
    defaultW: 4,
    defaultH: 2,
    async render({ body, store }) {
      const state = store.getState().adminStatus || {};
      body.innerHTML = `<p><strong>${state.provider || "unknown"}</strong> / ${state.model || "unknown"}</p><p class="muted">Runs: ${Number(state.run_count) || 0}</p>`;
    },
  }),
  widgetSpec({
    key: "runtime.overview",
    label: "Runtime: Overview",
    description: "Run count and channel enablement snapshot.",
    sourcePath: "/chat",
    defaultW: 4,
    defaultH: 3,
    async render({ body }) {
      const status = await fetchAdminStatus();
      renderKeyValueList(body, [
        { label: "Runs", value: String(Number(status?.run_count) || 0) },
        { label: "Discord", value: status?.discord_enabled ? "enabled" : "disabled" },
        { label: "Telegram", value: status?.telegram_enabled ? "enabled" : "disabled" },
      ]);
    },
  }),
  widgetSpec({
    key: "chat.quick_prompt",
    label: "Chat: Quick prompt",
    description: "Send a quick dashboard chat prompt.",
    sourcePath: "/chat",
    defaultW: 5,
    defaultH: 3,
    async render({ body, widget, widgetState }) {
      const form = document.createElement("form");
      form.className = "dashboard-quick-form";
      const input = document.createElement("textarea");
      input.className = "settings-textarea";
      input.setAttribute("data-focus-id", `dashboard-widget:${widget.widget_instance_id}:quick-prompt`);
      input.rows = 4;
      input.placeholder = "Send a quick prompt";
      const button = document.createElement("button");
      button.type = "submit";
      button.className = "chat-send-button";
      button.textContent = "Send";
      const result = document.createElement("p");
      result.className = "muted";
      form.addEventListener("submit", async (event) => {
        event.preventDefault();
        const text = input.value.trim();
        if (!text) return;
        const response = await sendQuickPrompt(text, widgetState.agent_id || "default");
        result.textContent = `Queued: ${response?.id || response?.status || "ok"}`;
        input.value = "";
      });
      body.append(form, result);
      form.append(input, button);
    },
    configure({ widget }) {
      const next = window.prompt("Agent id for quick prompt", String(widget.widget_state.agent_id || "default"));
      if (next !== null) {
        widget.widget_state.agent_id = next.trim() || "default";
      }
    },
  }),
  widgetSpec({
    key: "secrets.summary",
    label: "Secrets: Presence summary",
    description: "Shows which secret keys exist.",
    sourcePath: "/secrets",
    defaultW: 4,
    defaultH: 3,
    async render({ body }) {
      const keys = await fetchSecretsKeys();
      body.innerHTML = "";
      if (!keys.length) {
        body.innerHTML = '<p class="muted">No secret keys stored.</p>';
        return;
      }
      const list = document.createElement("div");
      list.className = "widget-list";
      keys.slice(0, 8).forEach((key) => {
        const row = document.createElement("div");
        row.className = "widget-list-item static";
        row.innerHTML = `<strong>${key}</strong><span>present</span>`;
        list.append(row);
      });
      body.append(list);
    },
  }),
  widgetSpec({
    key: "secrets.discord_token",
    label: "Secrets: Discord token",
    description: "Focused Discord/Telegram token presence widget.",
    sourcePath: "/secrets",
    defaultW: 4,
    defaultH: 2,
    async render({ body }) {
      const keys = await fetchSecretsKeys();
      renderKeyValueList(body, [
        { label: "discord/bot_token", value: keys.includes("discord/bot_token") ? "present" : "missing" },
        { label: "telegram/bot_token", value: keys.includes("telegram/bot_token") ? "present" : "missing" },
      ]);
    },
  }),
  widgetSpec({
    key: "secrets.conventions",
    label: "Secrets: Key conventions",
    description: "Recommended secret naming patterns.",
    sourcePath: "/secrets",
    defaultW: 5,
    defaultH: 3,
    async render({ body }) {
      renderKeyValueList(body, [
        { label: "Discord", value: "discord/bot_token" },
        { label: "Telegram", value: "telegram/bot_token" },
        { label: "OpenAI", value: "OPENAI_API_KEY" },
        { label: "OpenRouter", value: "OPENROUTER_API_KEY" },
      ]);
    },
  }),
  widgetSpec({
    key: "settings.summary",
    label: "Settings: Model summary + Agent overrides summary",
    description: "Global model plus profile overview.",
    sourcePath: "/settings?category=model",
    defaultW: 6,
    defaultH: 3,
    async render({ body }) {
      const cfg = await fetchConfig();
      const profiles = cfg?.agents?.profiles && typeof cfg.agents.profiles === "object" ? Object.keys(cfg.agents.profiles) : [];
      body.innerHTML = `<p><strong>${cfg?.model?.provider || "unknown"}</strong> / ${cfg?.model?.name || "unknown"}</p><p class="muted">Global max_tokens: ${cfg?.model?.max_tokens || 0}</p><p class="muted">Agent profiles with overrides: ${profiles.length}</p>`;
    },
  }),
  widgetSpec({
    key: "settings.providers",
    label: "Settings: Provider endpoints",
    description: "Base URLs and env references for providers.",
    sourcePath: "/settings?category=model",
    defaultW: 6,
    defaultH: 4,
    async render({ body }) {
      const cfg = await fetchConfig();
      const rows = ["openai", "openrouter", "requesty", "zai", "generic"].map((provider) => ({
        label: provider,
        value: `${cfg?.providers?.[provider]?.base_url || "(default)"} · ${cfg?.providers?.[provider]?.api_key_env || "(no env ref)"}`,
      }));
      renderKeyValueList(body, rows);
    },
  }),
  widgetSpec({
    key: "settings.agents",
    label: "Settings: Agents snapshot",
    description: "Enabled agents and override count.",
    sourcePath: "/settings?category=agents",
    defaultW: 5,
    defaultH: 3,
    async render({ body }) {
      const cfg = await fetchConfig();
      const profiles = cfg?.agents?.profiles && typeof cfg.agents.profiles === "object" ? Object.entries(cfg.agents.profiles) : [];
      const overrideCount = profiles.filter(([, profile]) => profile?.model && (profile.model.provider || profile.model.name || profile.model.max_tokens || profile.model.temperature)).length;
      renderKeyValueList(body, [
        { label: "Enabled agent ids", value: String((cfg?.agents?.enabled_agent_ids || []).length) },
        { label: "Profiles", value: String(profiles.length) },
        { label: "Model overrides", value: String(overrideCount) },
      ]);
    },
  }),
  widgetSpec({
    key: "settings.subagents",
    label: "Settings: Subagent defaults",
    description: "Subagent safety and delegation snapshot.",
    sourcePath: "/settings?category=agents",
    defaultW: 5,
    defaultH: 3,
    async render({ body }) {
      const cfg = await fetchConfig();
      const d = cfg?.agents?.subagent_defaults || {};
      renderKeyValueList(body, [
        { label: "Thinking mode", value: d.thinking_mode || "(default)" },
        { label: "Delegation mode", value: d.delegation_mode || "(default)" },
        { label: "Timeout ms", value: String(d.timeout_ms ?? 0) },
        { label: "Allowed tools", value: String((d.allowed_tools || []).length) },
      ]);
    },
  }),
  widgetSpec({
    key: "settings.memory",
    label: "Settings: Memory summary",
    description: "Memory configuration overview.",
    sourcePath: "/settings?category=memory",
    defaultW: 4,
    defaultH: 3,
    async render({ body }) {
      const cfg = await fetchConfig();
      const memory = cfg?.memory || {};
      renderKeyValueList(body, [
        { label: "Enabled", value: memory.enabled ? "yes" : "no" },
        { label: "Embeddings", value: memory.embeddings_enabled ? "on" : "off" },
        { label: "Embedding provider", value: memory.embedding_provider || "(none)" },
        { label: "Max working items", value: String(memory.max_working_items ?? 0) },
      ]);
    },
  }),
  widgetSpec({
    key: "settings.network",
    label: "Settings: Network policy",
    description: "Allowed domains and shell status.",
    sourcePath: "/settings?category=network",
    defaultW: 4,
    defaultH: 3,
    async render({ body }) {
      const cfg = await fetchConfig();
      renderKeyValueList(body, [
        { label: "Allowed domains", value: String((cfg?.network?.allowed_domains || []).length) },
        { label: "Shell exec", value: cfg?.shell?.enable_exec ? "enabled" : "disabled" },
        { label: "Sandbox", value: cfg?.sandbox?.active ? `${cfg?.sandbox?.provider || "active"}` : "inactive" },
      ]);
    },
  }),
  widgetSpec({
    key: "settings.scheduler",
    label: "Settings: Scheduler config",
    description: "Scheduler limits and catch-up policy.",
    sourcePath: "/settings?category=scheduler",
    defaultW: 4,
    defaultH: 2,
    async render({ body }) {
      const cfg = await fetchConfig();
      renderKeyValueList(body, [
        { label: "Catch up", value: cfg?.scheduler?.catch_up ? "enabled" : "disabled" },
        { label: "Max concurrent jobs", value: String(cfg?.scheduler?.max_concurrent_jobs ?? 0) },
      ]);
    },
  }),
  widgetSpec({
    key: "channels.status",
    label: "Discord/Telegram status",
    description: "Connector enabled flags and token presence.",
    sourcePath: "/settings?category=chat",
    defaultW: 4,
    defaultH: 2,
    async render({ body }) {
      const [cfg, keys] = await Promise.all([fetchConfig(), fetchSecretsKeys()]);
      const discordPresent = keys.includes("discord/bot_token");
      const telegramPresent = keys.includes("telegram/bot_token");
      body.innerHTML = `<p><strong>Discord</strong>: ${cfg?.discord?.enabled ? "enabled" : "disabled"} · token ${discordPresent ? "present" : "missing"}</p><p><strong>Telegram</strong>: ${cfg?.telegram?.enabled ? "enabled" : "disabled"} · token ${telegramPresent ? "present" : "missing"}</p>`;
    },
  }),
  widgetSpec({
    key: "sessions.recent",
    label: "Sessions: Recent",
    description: "Recent chat sessions and activity.",
    sourcePath: "/sessions",
    defaultW: 6,
    defaultH: 3,
    async render({ body }) {
      const sessions = await fetchRecentSessions(dashboardsState.apiClient, 5);
      renderCompactSessionsList(body, sessions, { limit: 5, onOpen: () => dashboardsState.router.navigate("/sessions") });
    },
  }),
  widgetSpec({
    key: "sessions.overview",
    label: "Sessions: Overview",
    description: "Total recent sessions and latest activity.",
    sourcePath: "/sessions",
    defaultW: 4,
    defaultH: 2,
    async render({ body }) {
      const sessions = await fetchRecentSessions(dashboardsState.apiClient, 10);
      const latest = sessions[0];
      renderKeyValueList(body, [
        { label: "Recent sessions", value: String(sessions.length) },
        { label: "Latest", value: latest ? `${latest.title || latest.session_id} · ${latest.agent_id || "default"}` : "none" },
      ]);
    },
  }),
  widgetSpec({
    key: "skills.summary",
    label: "Skills: Summary",
    description: "Installed and activated skills by agent.",
    sourcePath: "/skills",
    defaultW: 4,
    defaultH: 3,
    async render({ body, widgetState }) {
      const summary = await fetchSkillsSummary(dashboardsState.apiClient, widgetState.agent_id || "default");
      renderCompactSkillsSummary(body, summary);
    },
    configure({ widget }) {
      const next = window.prompt("Agent id for skills summary", String(widget.widget_state.agent_id || "default"));
      if (next !== null) {
        widget.widget_state.agent_id = next.trim() || "default";
      }
    },
  }),
  widgetSpec({
    key: "skills.active_list",
    label: "Skills: Active list",
    description: "Shows active skills for an agent.",
    sourcePath: "/skills",
    defaultW: 4,
    defaultH: 3,
    async render({ body, widgetState }) {
      const summary = await fetchSkillsSummary(dashboardsState.apiClient, widgetState.agent_id || "default");
      const active = summary?.activated_skills || [];
      if (!active.length) {
        body.innerHTML = '<p class="muted">No active skills.</p>';
        return;
      }
      renderKeyValueList(body, active.slice(0, 6).map((item) => ({ label: item, value: `active for ${summary.agent_id}` })));
    },
    configure({ widget }) {
      const next = window.prompt("Agent id for active skills widget", String(widget.widget_state.agent_id || "default"));
      if (next !== null) {
        widget.widget_state.agent_id = next.trim() || "default";
      }
    },
  }),
  widgetSpec({
    key: "docs.summary",
    label: "Docs: Agent prompt docs",
    description: "Compact overview of agent doc files.",
    sourcePath: "/docs",
    defaultW: 4,
    defaultH: 3,
    async render({ body, widgetState }) {
      const summary = await fetchDocsSummary(dashboardsState.apiClient, widgetState.agent_id || "default");
      renderCompactDocsSummary(body, summary);
    },
    configure({ widget }) {
      const next = window.prompt("Agent id for docs summary", String(widget.widget_state.agent_id || "default"));
      if (next !== null) {
        widget.widget_state.agent_id = next.trim() || "default";
      }
    },
  }),
  widgetSpec({
    key: "docs.doc_list",
    label: "Docs: Top files",
    description: "Lists key prompt/control docs for an agent.",
    sourcePath: "/docs",
    defaultW: 4,
    defaultH: 3,
    async render({ body, widgetState }) {
      const summary = await fetchDocsSummary(dashboardsState.apiClient, widgetState.agent_id || "default");
      const docs = summary?.documents || [];
      if (!docs.length) {
        body.innerHTML = '<p class="muted">No docs found.</p>';
        return;
      }
      renderKeyValueList(body, docs.slice(0, 6).map((doc) => ({ label: doc.name, value: doc.exists ? "present" : "missing" })));
    },
    configure({ widget }) {
      const next = window.prompt("Agent id for doc list widget", String(widget.widget_state.agent_id || "default"));
      if (next !== null) {
        widget.widget_state.agent_id = next.trim() || "default";
      }
    },
  }),
  widgetSpec({
    key: "sandbox.summary",
    label: "Sandbox: Status",
    description: "Docker/local sandbox health and inventory.",
    sourcePath: "/sandbox",
    defaultW: 4,
    defaultH: 3,
    async render({ body, widgetState }) {
      const summary = await fetchSandboxSummary(dashboardsState.apiClient, widgetState.agent_id || "default");
      renderCompactSandboxSummary(body, summary);
    },
    configure({ widget }) {
      const next = window.prompt("Agent id for sandbox summary", String(widget.widget_state.agent_id || "default"));
      if (next !== null) {
        widget.widget_state.agent_id = next.trim() || "default";
      }
    },
  }),
  widgetSpec({
    key: "sandbox.inventory",
    label: "Sandbox: Images & volumes",
    description: "Counts available sandbox images and volumes.",
    sourcePath: "/sandbox",
    defaultW: 4,
    defaultH: 2,
    async render({ body, widgetState }) {
      const summary = await fetchSandboxSummary(dashboardsState.apiClient, widgetState.agent_id || "default");
      renderKeyValueList(body, [
        { label: "Images", value: String((summary?.images || []).length) },
        { label: "Volumes", value: String((summary?.volumes || []).length) },
      ]);
    },
    configure({ widget }) {
      const next = window.prompt("Agent id for sandbox inventory widget", String(widget.widget_state.agent_id || "default"));
      if (next !== null) {
        widget.widget_state.agent_id = next.trim() || "default";
      }
    },
  }),
];

const WIDGET_MAP = new Map(WIDGETS.map((widget) => [widget.key, widget]));

function createDefaultWidget(widgetKey) {
  const spec = WIDGET_MAP.get(widgetKey);
  return {
    widget_key: spec.key,
    widget_instance_id: uid("widget"),
    x: 0,
    y: 0,
    w: spec.defaultW,
    h: spec.defaultH,
    widget_state: {},
  };
}

function rerender(options = {}) {
  if (dashboardsState.container?.isConnected) {
    const focusSnapshot = options.preserveFocus ? captureFocusSnapshot(dashboardsState.container) : null;
    renderDashboardsPage();
    if (focusSnapshot) {
      restoreFocusSnapshot(dashboardsState.container, focusSnapshot);
    }
  }
}

function startPointerDrag(event, widget, mode) {
  event.preventDefault();
  const grid = dashboardsState.container.querySelector(".dashboards-grid");
  if (!grid) return;
  const rect = grid.getBoundingClientRect();
  dashboardsState.drag = { mode, widget, startX: event.clientX, startY: event.clientY, origin: { ...widget }, rect };
  const onMove = (moveEvent) => {
    const colWidth = rect.width / GRID_COLUMNS;
    const dx = Math.round((moveEvent.clientX - dashboardsState.drag.startX) / colWidth);
    const dy = Math.round((moveEvent.clientY - dashboardsState.drag.startY) / ROW_HEIGHT);
    updateDashboardLocal((dashboard) => {
      const target = dashboard.layout.find((item) => item.widget_instance_id === widget.widget_instance_id);
      if (!target) return;
      if (mode === "move") {
        target.x = Math.max(0, Math.min(GRID_COLUMNS - target.w, dashboardsState.drag.origin.x + dx));
        target.y = Math.max(0, dashboardsState.drag.origin.y + dy);
      } else {
        target.w = Math.max(2, Math.min(GRID_COLUMNS - target.x, dashboardsState.drag.origin.w + dx));
        target.h = Math.max(2, dashboardsState.drag.origin.h + dy);
      }
    });
  };
  const onUp = () => {
    window.removeEventListener("pointermove", onMove);
    window.removeEventListener("pointerup", onUp);
    dashboardsState.drag = null;
    dashboardsState.saveNotice = "Unsaved changes";
    scheduleSave();
  };
  window.addEventListener("pointermove", onMove);
  window.addEventListener("pointerup", onUp, { once: true });
}

function renderWidgetCard(widget, dashboard) {
  const spec = WIDGET_MAP.get(widget.widget_key);
  const card = document.createElement("article");
  card.className = "dashboard-widget-card";
  card.tabIndex = 0;
  card.style.gridColumn = `${widget.x + 1} / span ${widget.w}`;
  card.style.gridRow = `${widget.y + 1} / span ${widget.h}`;
  card.addEventListener("keydown", (event) => {
    if (event.key === "Delete") {
      updateDashboard((next) => {
        next.layout = next.layout.filter((item) => item.widget_instance_id !== widget.widget_instance_id);
      });
    }
    if (event.key.startsWith("Arrow")) {
      const delta = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }[event.key];
      if (delta) {
        event.preventDefault();
        updateDashboard((next) => {
          const target = next.layout.find((item) => item.widget_instance_id === widget.widget_instance_id);
          if (!target) return;
          target.x = Math.max(0, Math.min(GRID_COLUMNS - target.w, target.x + delta[0]));
          target.y = Math.max(0, target.y + delta[1]);
        });
      }
    }
  });

  const header = document.createElement("div");
  header.className = "dashboard-widget-header";
  const title = document.createElement("div");
  title.innerHTML = `<strong>${spec.label}</strong><span>${spec.description}</span>`;
  const actions = document.createElement("div");
  actions.className = "dashboard-widget-actions";
  const configButton = document.createElement("button");
  configButton.type = "button";
  configButton.className = "layout-toggle";
  configButton.textContent = "...";
  configButton.addEventListener("click", () => {
    dashboardsState.widgetMenuFor = dashboardsState.widgetMenuFor === widget.widget_instance_id ? "" : widget.widget_instance_id;
    rerender({ preserveFocus: true });
  });
  actions.append(configButton);
  header.append(title, actions);
  header.addEventListener("pointerdown", (event) => startPointerDrag(event, widget, "move"));

  if (dashboardsState.widgetMenuFor === widget.widget_instance_id) {
    const menu = document.createElement("div");
    menu.className = "dashboard-widget-menu";
    const menuOpenSource = document.createElement("button");
    menuOpenSource.type = "button";
    menuOpenSource.className = "layout-toggle";
    menuOpenSource.textContent = "Open source tab";
    menuOpenSource.addEventListener("click", () => dashboardsState.router.navigate(spec.sourcePath));
    const menuDuplicate = document.createElement("button");
    menuDuplicate.type = "button";
    menuDuplicate.className = "layout-toggle";
    menuDuplicate.textContent = "Duplicate widget";
    menuDuplicate.addEventListener("click", () => {
      updateDashboard((next) => {
        next.layout.push({ ...widget, widget_instance_id: uid("widget"), x: Math.min(GRID_COLUMNS - widget.w, widget.x + 1), y: widget.y + 1, widget_state: { ...widget.widget_state } });
      });
      dashboardsState.widgetMenuFor = "";
    });
    if (typeof spec.configure === "function") {
      const menuConfigure = document.createElement("button");
      menuConfigure.type = "button";
      menuConfigure.className = "layout-toggle";
      menuConfigure.textContent = "Configure";
      menuConfigure.addEventListener("click", () => {
        spec.configure({ widget, dashboard });
        dashboardsState.widgetMenuFor = "";
        markDirty();
      });
      menu.append(menuConfigure);
    }
    const menuRemove = document.createElement("button");
    menuRemove.type = "button";
    menuRemove.className = "layout-toggle";
    menuRemove.textContent = "Remove widget";
    menuRemove.addEventListener("click", () => {
      removeWidget(widget.widget_instance_id);
      dashboardsState.widgetMenuFor = "";
    });
    menu.append(menuOpenSource, menuDuplicate, menuRemove);
    header.append(menu);
  }

  const body = document.createElement("div");
  body.className = "dashboard-widget-body";
  body.innerHTML = '<p class="muted">Loading widget...</p>';
  Promise.resolve(spec.render({ body, widget, widgetState: widget.widget_state || {}, store: dashboardsState.store, router: dashboardsState.router }))
    .catch((error) => {
      body.innerHTML = `<p class="settings-inline-error">${error instanceof Error ? error.message : String(error)}</p>`;
    });

  const resizeHandle = document.createElement("button");
  resizeHandle.type = "button";
  resizeHandle.className = "dashboard-widget-resize";
  resizeHandle.setAttribute("aria-label", `Resize ${spec.label}`);
  resizeHandle.addEventListener("pointerdown", (event) => startPointerDrag(event, widget, "resize"));

  card.append(header, body, resizeHandle);
  return card;
}

function renderDashboardsPage() {
  const container = dashboardsState.container;
  container.innerHTML = "";
  const selected = selectedDashboard();

  const heading = document.createElement("h2");
  heading.textContent = "Custom Dashboards";
  const subtitle = document.createElement("p");
  subtitle.className = "muted";
  subtitle.textContent = "Create reusable operator dashboards with drag, resize, widget reuse, and server-backed persistence.";
  container.append(heading, subtitle);

  if (dashboardsState.error) {
    const error = document.createElement("p");
    error.className = "settings-inline-error";
    error.textContent = dashboardsState.error;
    container.append(error);
  }

  const shell = document.createElement("section");
  shell.className = "dashboards-shell";
  const sidebar = document.createElement("aside");
  sidebar.className = "dashboards-sidebar";
  const createButton = document.createElement("button");
  createButton.type = "button";
  createButton.className = "chat-send-button";
  createButton.textContent = "Create dashboard";
  createButton.addEventListener("click", () => void createDashboard());
  sidebar.append(createButton);
  dashboardsState.dashboards.forEach((item, index) => {
    const row = document.createElement("div");
    row.className = `dashboard-tab-row ${item.id === dashboardsState.selectedID ? "active" : ""}`;
    const button = document.createElement("button");
    button.type = "button";
    button.className = "layout-toggle";
    button.textContent = item.name;
    button.addEventListener("click", () => {
      dashboardsState.selectedID = item.id;
      rerender({ preserveFocus: true });
    });
    const up = document.createElement("button");
    up.type = "button";
    up.className = "layout-toggle";
    up.textContent = "↑";
    up.disabled = index === 0;
    up.addEventListener("click", () => {
      const next = [...dashboardsState.dashboards];
      [next[index - 1], next[index]] = [next[index], next[index - 1]];
      dashboardsState.dashboards = next;
      markDirty();
    });
    const down = document.createElement("button");
    down.type = "button";
    down.className = "layout-toggle";
    down.textContent = "↓";
    down.disabled = index === dashboardsState.dashboards.length - 1;
    down.addEventListener("click", () => {
      const next = [...dashboardsState.dashboards];
      [next[index + 1], next[index]] = [next[index], next[index + 1]];
      dashboardsState.dashboards = next;
      markDirty();
    });
    const duplicate = document.createElement("button");
    duplicate.type = "button";
    duplicate.className = "layout-toggle";
    duplicate.textContent = "Duplicate";
    duplicate.addEventListener("click", () => {
      const cloned = normalizeDashboard({ ...item, id: uid("dash"), name: `${item.name} Copy`, created_at: nowISO(), updated_at: nowISO(), layout: item.layout.map((widget) => ({ ...widget, widget_instance_id: uid("widget"), widget_state: { ...widget.widget_state } })) });
      dashboardsState.dashboards = [...dashboardsState.dashboards, cloned];
      dashboardsState.selectedID = cloned.id;
      markDirty();
    });
    const remove = document.createElement("button");
    remove.type = "button";
    remove.className = "layout-toggle";
    remove.textContent = "Delete";
    remove.disabled = dashboardsState.dashboards.length <= 1;
    remove.addEventListener("click", () => void deleteDashboard(item.id));
    row.append(button, up, down, duplicate, remove);
    sidebar.append(row);
  });

  const main = document.createElement("div");
  main.className = "dashboards-main";
  if (selected) {
    const toolbar = document.createElement("div");
    toolbar.className = "dashboards-toolbar";
    const nameInput = document.createElement("input");
    nameInput.className = "settings-input";
    nameInput.setAttribute("data-focus-id", `dashboard:name:${selected.id}`);
    nameInput.value = selected.name;
    nameInput.addEventListener("input", () => updateDashboard((dashboard) => { dashboard.name = nameInput.value.trim() || "Untitled Dashboard"; }));
    const addWidget = document.createElement("button");
    addWidget.type = "button";
    addWidget.className = "chat-send-button";
    addWidget.textContent = "Add widget";
    addWidget.addEventListener("click", () => {
      dashboardsState.widgetPickerOpen = !dashboardsState.widgetPickerOpen;
      rerender({ preserveFocus: true });
    });
    const resetLayout = document.createElement("button");
    resetLayout.type = "button";
    resetLayout.className = "layout-toggle";
    resetLayout.textContent = "Reset layout";
    resetLayout.addEventListener("click", () => updateDashboard((dashboard) => { dashboard.layout = []; }));
    const saveNow = document.createElement("button");
    saveNow.type = "button";
    saveNow.className = "layout-toggle";
    saveNow.textContent = dashboardsState.saving ? "Saving..." : "Save now";
    saveNow.disabled = dashboardsState.saving || !dashboardsState.dirty;
    saveNow.addEventListener("click", () => {
      if (dashboardsState.saveTimer) {
        window.clearTimeout(dashboardsState.saveTimer);
        dashboardsState.saveTimer = null;
      }
      void saveAllDashboards();
    });
    const saveIndicator = document.createElement("span");
    saveIndicator.className = "muted";
    saveIndicator.textContent = dashboardsState.saving ? "Saving..." : dashboardsState.dirty ? "Dirty" : "Clean";
    toolbar.append(nameInput, addWidget, resetLayout, saveNow, saveIndicator);
    if (dashboardsState.saveNotice) {
      const note = document.createElement("span");
      note.className = dashboardsState.saveNotice === "Save failed" ? "settings-inline-error" : "muted";
      note.textContent = dashboardsState.saveNotice;
      toolbar.append(note);
    }
    main.append(toolbar);

    if (dashboardsState.widgetPickerOpen) {
      const picker = document.createElement("div");
      picker.className = "dashboard-widget-picker";
      const search = document.createElement("input");
      search.className = "settings-input";
      search.setAttribute("data-focus-id", `dashboard:widget-search:${selected.id}`);
      search.placeholder = "Search widgets";
      const list = document.createElement("div");
      list.className = "dashboard-widget-picker-list";
      const renderList = () => {
        list.innerHTML = "";
        const query = search.value.trim().toLowerCase();
        WIDGETS.filter((widget) => !query || `${widget.label} ${widget.description}`.toLowerCase().includes(query)).forEach((widget) => {
          const row = document.createElement("button");
          row.type = "button";
          row.className = "widget-list-item";
          row.innerHTML = `<strong>${widget.label}</strong><span>${widget.description}</span>`;
          row.addEventListener("click", () => {
            updateDashboard((dashboard) => {
              const nextWidget = createDefaultWidget(widget.key);
              nextWidget.y = dashboard.layout.reduce((max, item) => Math.max(max, item.y + item.h), 0);
              dashboard.layout.push(nextWidget);
            });
            dashboardsState.widgetPickerOpen = false;
          });
          list.append(row);
        });
      };
      search.addEventListener("input", renderList);
      renderList();
      picker.append(search, list);
      main.append(picker);
    }

    const grid = document.createElement("div");
    grid.className = "dashboards-grid";
    selected.layout.forEach((widget) => grid.append(renderWidgetCard(widget, selected)));
    main.append(grid);
  }

  shell.append(sidebar, main);
  container.append(shell);
}

export const dashboardsPage = {
  key: "dashboards",
  title: "Custom Dashboards",
  async render({ container, apiClient, store, router }) {
    dashboardsState.container = container;
    dashboardsState.apiClient = apiClient;
    dashboardsState.store = store;
    dashboardsState.router = router;
    if (!dashboardsState.dashboards.length && !dashboardsState.loading) {
      await loadDashboards();
      return;
    }
    rerender({ preserveFocus: true });
  },
};
