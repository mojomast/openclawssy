import { fetchRecentRuns, renderCompactRunsList } from "./runs.js";
import { fetchSchedulerJobs, renderCompactSchedulerJobs } from "./scheduler.js";

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

function markDirty() {
  dashboardsState.dashboards.forEach((item, index) => {
    item.position = index;
  });
  dashboardsState.dirty = true;
  writeLocalDashboards();
  rerender();
  void saveAllDashboards();
}

async function loadDashboards() {
  dashboardsState.loading = true;
  dashboardsState.error = "";
  rerender();
  try {
    const localItems = readLocalDashboards();
    const payload = await dashboardsState.apiClient.get("/api/admin/dashboards");
    const remoteItems = Array.isArray(payload?.dashboards) ? payload.dashboards : [];
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
    rerender();
  }
}

async function createDashboard() {
  try {
    const payload = await dashboardsState.apiClient.post("/api/admin/dashboards", { name: `Dashboard ${dashboardsState.dashboards.length + 1}` });
    const created = normalizeDashboard(payload?.dashboard || {});
    dashboardsState.dashboards = [...dashboardsState.dashboards, created];
    dashboardsState.selectedID = created.id;
    markDirty();
  } catch (error) {
    dashboardsState.error = error instanceof Error ? error.message : String(error);
    rerender();
  }
}

async function saveAllDashboards() {
  if (dashboardsState.saving) {
    return;
  }
  dashboardsState.saving = true;
  rerender();
  try {
    for (const dashboard of dashboardsState.dashboards) {
      dashboard.updated_at = nowISO();
      await dashboardsState.apiClient.put(`/api/admin/dashboards/${encodeURIComponent(dashboard.id)}`, dashboard);
    }
    dashboardsState.dirty = false;
    writeLocalDashboards();
  } catch (error) {
    dashboardsState.error = error instanceof Error ? error.message : String(error);
  } finally {
    dashboardsState.saving = false;
    rerender();
  }
}

async function deleteDashboard(id) {
  if (dashboardsState.dashboards.length <= 1) {
    return;
  }
  if (!window.confirm("Delete this custom dashboard?")) {
    return;
  }
  await dashboardsState.apiClient.delete(`/api/admin/dashboards/${encodeURIComponent(id)}`);
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

async function fetchConfig() {
  return dashboardsState.apiClient.get("/api/admin/config");
}

async function fetchSecretsKeys() {
  const payload = await dashboardsState.apiClient.get("/api/admin/secrets");
  return Array.isArray(payload?.keys) ? payload.keys : [];
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

function rerender() {
  if (dashboardsState.container?.isConnected) {
    renderDashboardsPage();
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
    updateDashboard((dashboard) => {
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
  const sourceButton = document.createElement("button");
  sourceButton.type = "button";
  sourceButton.className = "layout-toggle";
  sourceButton.textContent = "Open source tab";
  sourceButton.addEventListener("click", () => dashboardsState.router.navigate(spec.sourcePath));
  const duplicateButton = document.createElement("button");
  duplicateButton.type = "button";
  duplicateButton.className = "layout-toggle";
  duplicateButton.textContent = "Duplicate";
  duplicateButton.addEventListener("click", () => {
    updateDashboard((next) => {
      next.layout.push({ ...widget, widget_instance_id: uid("widget"), x: Math.min(GRID_COLUMNS - widget.w, widget.x + 1), y: widget.y + 1, widget_state: { ...widget.widget_state } });
    });
  });
  const configButton = document.createElement("button");
  configButton.type = "button";
  configButton.className = "layout-toggle";
  configButton.textContent = "...";
  configButton.addEventListener("click", () => {
    if (typeof spec.configure === "function") {
      spec.configure({ widget, dashboard });
      markDirty();
    } else {
      updateDashboard((next) => {
        next.layout = next.layout.filter((item) => item.widget_instance_id !== widget.widget_instance_id);
      });
    }
  });
  actions.append(sourceButton, duplicateButton, configButton);
  header.append(title, actions);
  header.addEventListener("pointerdown", (event) => startPointerDrag(event, widget, "move"));

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
      rerender();
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
    nameInput.value = selected.name;
    nameInput.addEventListener("input", () => updateDashboard((dashboard) => { dashboard.name = nameInput.value.trim() || "Untitled Dashboard"; }));
    const addWidget = document.createElement("button");
    addWidget.type = "button";
    addWidget.className = "chat-send-button";
    addWidget.textContent = "Add widget";
    addWidget.addEventListener("click", () => {
      dashboardsState.widgetPickerOpen = !dashboardsState.widgetPickerOpen;
      rerender();
    });
    const resetLayout = document.createElement("button");
    resetLayout.type = "button";
    resetLayout.className = "layout-toggle";
    resetLayout.textContent = "Reset layout";
    resetLayout.addEventListener("click", () => updateDashboard((dashboard) => { dashboard.layout = []; }));
    const saveIndicator = document.createElement("span");
    saveIndicator.className = "muted";
    saveIndicator.textContent = dashboardsState.saving ? "Saving..." : dashboardsState.dirty ? "Dirty" : "Clean";
    toolbar.append(nameInput, addWidget, resetLayout, saveIndicator);
    main.append(toolbar);

    if (dashboardsState.widgetPickerOpen) {
      const picker = document.createElement("div");
      picker.className = "dashboard-widget-picker";
      const search = document.createElement("input");
      search.className = "settings-input";
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
    rerender();
  },
};
