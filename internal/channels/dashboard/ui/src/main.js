import { createApiClient } from "./api/client.js";
import { createHashRouter } from "./router/router.js";
import { createStore } from "./state/store.js";
import { chatPage } from "./pages/chat.js";
import { sessionsPage } from "./pages/sessions.js";
import { runsPage } from "./pages/runs.js";
import { schedulerPage } from "./pages/scheduler.js";
import { settingsPage } from "./pages/settings.js";
import { secretsPage } from "./pages/secrets.js";
import { docsPage } from "./pages/docs.js";
import { skillsPage } from "./pages/skills.js";
import { sandboxPage } from "./pages/sandbox.js";
import { toolInspector } from "./inspectors/tool_inspector.js";
import { traceInspector } from "./inspectors/trace_inspector.js";
import { toolSchemaInspector } from "./inspectors/tool_schema_inspector.js";
import { fixSuggestionsInspector } from "./inspectors/fix_suggestions_inspector.js";
import { pythonEnvInspector } from "./inspectors/python_env_inspector.js";
import { createLayout } from "./ui/layout.js";

const ROUTES = [
  { path: "/chat", label: "Chat", page: chatPage },
  { path: "/sessions", label: "Sessions", page: sessionsPage },
  { path: "/runs", label: "Runs", page: runsPage },
  { path: "/scheduler", label: "Scheduler", page: schedulerPage },
  { path: "/settings", label: "Settings", page: settingsPage },
  { path: "/docs", label: "Docs", page: docsPage },
  { path: "/skills", label: "Skills", page: skillsPage },
  { path: "/secrets", label: "Secrets", page: secretsPage },
  { path: "/sandbox", label: "Sandbox", page: sandboxPage },
];

const INSPECTORS = [toolInspector, traceInspector, toolSchemaInspector, fixSuggestionsInspector, pythonEnvInspector];

function compactText(value, maxChars = 220) {
  const text = String(value || "").replace(/\s+/g, " ").trim();
  if (!text) {
    return "";
  }
  if (text.length <= maxChars) {
    return text;
  }
  return `${text.slice(0, Math.max(0, maxChars - 3))}...`;
}

function toAdminStatusState(payload, loading = false) {
  const model = payload?.model && typeof payload.model === "object" ? payload.model : {};
  return {
    loading,
    fetched_at: new Date().toISOString(),
    provider: String(model.provider || "").trim(),
    model: String(model.name || "").trim(),
    run_count: Number(payload?.run_count) || 0,
    error: "",
  };
}

export function bootDashboardApp() {
  const root = document.getElementById("app");
  if (!root) {
    return;
  }

  document.documentElement.setAttribute("data-dashboard-shell", "phase-1");

  const store = createStore({
    route: "/chat",
    inspectorTab: "tool",
    selectedTrace: null,
    selectedTool: null,
    lastError: null,
    adminStatus: {
      loading: false,
      fetched_at: "",
      provider: "",
      model: "",
      run_count: 0,
      error: "",
    },
  });
  const apiClient = createApiClient();

  let statusRequestInFlight = false;
  let statusFetchedAtMs = 0;

  async function refreshAdminStatus(force = false) {
    const now = Date.now();
    if (!force && statusFetchedAtMs > 0 && now - statusFetchedAtMs < 15_000) {
      return;
    }
    if (statusRequestInFlight) {
      return;
    }
    statusRequestInFlight = true;
    const currentStatus = store.getState().adminStatus || {};
    if (!currentStatus.provider || !currentStatus.model) {
      store.setState({ adminStatus: { ...currentStatus, loading: true, error: "" } });
    }
    try {
      const payload = await apiClient.get("/api/admin/status");
      statusFetchedAtMs = Date.now();
      store.setState({ adminStatus: toAdminStatusState(payload, false) });
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      store.setState({
        adminStatus: {
          ...currentStatus,
          loading: false,
          fetched_at: new Date().toISOString(),
          error: compactText(message, 220) || "Failed to load runtime status.",
        },
      });
    } finally {
      statusRequestInFlight = false;
    }
  }

  const router = createHashRouter({
    routes: ROUTES,
    defaultRoute: "/chat",
    onRouteChange: (route) => {
      store.setState({ route });
      void refreshAdminStatus(false);
    },
  });

  const layout = createLayout({
    root,
    routes: ROUTES,
    store,
    router,
    apiClient,
    inspectors: INSPECTORS,
  });

  store.subscribe(async (state) => {
    try {
      await layout.render(state);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      root.innerHTML = `<pre>Dashboard render failed: ${message}</pre>`;
    }
  });

  // Determine the initial route from the URL hash so we render the correct
  // page immediately without a race between the default "/chat" render and
  // the router's hash-based route change.
  const initialRoute = router.current();
  store.setState({ route: initialRoute });
  router.start();
  void refreshAdminStatus(true);
}
