const LAYOUT_STORAGE_KEY = "dashboard.layout.p1.2";
const THEME_STORAGE_KEY = "dashboard.theme";

function getCurrentTheme() {
  return document.documentElement.getAttribute("data-theme") === "dark" ? "dark" : "light";
}

function toggleTheme() {
  const isDark = getCurrentTheme() === "dark";
  if (isDark) {
    document.documentElement.removeAttribute("data-theme");
    localStorage.setItem(THEME_STORAGE_KEY, "light");
    return "light";
  }
  document.documentElement.setAttribute("data-theme", "dark");
  localStorage.setItem(THEME_STORAGE_KEY, "dark");
  return "dark";
}
const NARROW_SCREEN_QUERY = "(max-width: 900px)";
const RESIZE_STEP = 16;
const PANE_LIMITS = {
  left: { min: 176, max: 420, default: 224 },
  right: { min: 220, max: 520, default: 288 },
};

function createElement(tag, className, text) {
  const element = document.createElement(tag);
  if (className) {
    element.className = className;
  }
  if (text) {
    element.textContent = text;
  }
  return element;
}

function clamp(value, min, max) {
  return Math.min(max, Math.max(min, value));
}

function readLayoutPrefs() {
  const defaults = {
    leftWidth: PANE_LIMITS.left.default,
    rightWidth: PANE_LIMITS.right.default,
    navCollapsed: false,
    inspectorCollapsed: false,
    inspectorDrawerOpen: false,
  };

  try {
    const raw = window.localStorage.getItem(LAYOUT_STORAGE_KEY);
    if (!raw) {
      return defaults;
    }
    const parsed = JSON.parse(raw);
    return {
      leftWidth: clamp(Number(parsed.leftWidth) || defaults.leftWidth, PANE_LIMITS.left.min, PANE_LIMITS.left.max),
      rightWidth: clamp(Number(parsed.rightWidth) || defaults.rightWidth, PANE_LIMITS.right.min, PANE_LIMITS.right.max),
      navCollapsed: Boolean(parsed.navCollapsed),
      inspectorCollapsed: Boolean(parsed.inspectorCollapsed),
      inspectorDrawerOpen: Boolean(parsed.inspectorDrawerOpen),
    };
  } catch (_error) {
    return defaults;
  }
}

function persistLayoutPrefs(layoutPrefs) {
  try {
    window.localStorage.setItem(LAYOUT_STORAGE_KEY, JSON.stringify(layoutPrefs));
  } catch (_error) {
    // localStorage can fail (private mode / quota); keep layout usable.
  }
}

export function createLayout({ root, routes, store, router, apiClient, inspectors }) {
  root.innerHTML = "";

  const layoutPrefs = readLayoutPrefs();
  let isNarrowScreen = window.matchMedia(NARROW_SCREEN_QUERY).matches;

  const header = createElement("header", "shell-header");
  const titleWrap = createElement("div", "shell-header-title");
  const title = createElement("h1", "", "Openclawssy Dashboard");
  const subtitle = createElement("p", "muted", "Phase 1 modular shell foundation");
  const statusStamp = createElement("p", "muted", "Runtime: loading...");
  titleWrap.append(title, subtitle, statusStamp);

  const headerActions = createElement("div", "shell-header-actions");
  const navToggle = createElement("button", "layout-toggle nav-toggle", "Toggle Nav");
  navToggle.type = "button";
  navToggle.addEventListener("click", () => {
    layoutPrefs.navCollapsed = !layoutPrefs.navCollapsed;
    applyLayoutPrefs();
    persistLayoutPrefs(layoutPrefs);
  });

  const inspectorToggle = createElement("button", "layout-toggle inspector-toggle", "Inspector");
  inspectorToggle.type = "button";
  inspectorToggle.addEventListener("click", () => {
    if (isNarrowScreen) {
      layoutPrefs.inspectorDrawerOpen = !layoutPrefs.inspectorDrawerOpen;
    } else {
      layoutPrefs.inspectorCollapsed = !layoutPrefs.inspectorCollapsed;
    }
    applyLayoutPrefs();
    persistLayoutPrefs(layoutPrefs);
  });
  let currentTheme = getCurrentTheme();
  const themeToggle = createElement("button", "layout-toggle theme-toggle", currentTheme === "dark" ? "Light Mode" : "Dark Mode");
  themeToggle.type = "button";
  themeToggle.addEventListener("click", () => {
    currentTheme = toggleTheme();
    themeToggle.textContent = currentTheme === "dark" ? "Light Mode" : "Dark Mode";
  });

  headerActions.append(navToggle, inspectorToggle, themeToggle);
  header.append(titleWrap, headerActions);

  const shellGrid = createElement("div", "shell-grid");
  const nav = createElement("nav", "pane nav-pane");
  const leftResizer = createElement("div", "pane-resizer left-resizer");
  leftResizer.setAttribute("role", "separator");
  leftResizer.setAttribute("aria-label", "Resize navigation pane");
  leftResizer.tabIndex = 0;
  const content = createElement("main", "pane content-pane");
  const rightResizer = createElement("div", "pane-resizer right-resizer");
  rightResizer.setAttribute("role", "separator");
  rightResizer.setAttribute("aria-label", "Resize inspector pane");
  rightResizer.tabIndex = 0;
  const inspector = createElement("aside", "pane inspector-pane");
  shellGrid.append(nav, leftResizer, content, rightResizer, inspector);

  const inspectorBackdrop = createElement("button", "inspector-backdrop");
  inspectorBackdrop.type = "button";
  inspectorBackdrop.setAttribute("aria-label", "Close inspector drawer");
  inspectorBackdrop.addEventListener("click", () => {
    layoutPrefs.inspectorDrawerOpen = false;
    applyLayoutPrefs();
    persistLayoutPrefs(layoutPrefs);
  });

  const footer = createElement("footer", "shell-footer");
  const legacyLink = createElement("a", "", "Open Legacy Dashboard");
  legacyLink.href = "/dashboard-legacy";
  const bugLink = createElement("a", "", "Report bug");
  bugLink.target = "_blank";
  bugLink.rel = "noopener noreferrer";
  footer.append(legacyLink, document.createTextNode(" · "), bugLink);

  root.append(header, shellGrid, footer, inspectorBackdrop);

  function applyLayoutPrefs() {
    root.classList.toggle("is-narrow-screen", isNarrowScreen);
    root.classList.toggle("nav-collapsed", !isNarrowScreen && layoutPrefs.navCollapsed);
    root.classList.toggle("inspector-collapsed", !isNarrowScreen && layoutPrefs.inspectorCollapsed);
    root.classList.toggle("inspector-drawer-open", isNarrowScreen && layoutPrefs.inspectorDrawerOpen);

    shellGrid.style.setProperty("--pane-left", `${layoutPrefs.leftWidth}px`);
    shellGrid.style.setProperty("--pane-right", `${layoutPrefs.rightWidth}px`);

    leftResizer.setAttribute("aria-valuemin", String(PANE_LIMITS.left.min));
    leftResizer.setAttribute("aria-valuemax", String(PANE_LIMITS.left.max));
    leftResizer.setAttribute("aria-valuenow", String(layoutPrefs.leftWidth));

    rightResizer.setAttribute("aria-valuemin", String(PANE_LIMITS.right.min));
    rightResizer.setAttribute("aria-valuemax", String(PANE_LIMITS.right.max));
    rightResizer.setAttribute("aria-valuenow", String(layoutPrefs.rightWidth));

    navToggle.textContent = layoutPrefs.navCollapsed ? "Show Nav" : "Hide Nav";
    navToggle.setAttribute("aria-pressed", String(layoutPrefs.navCollapsed));

    if (isNarrowScreen) {
      inspectorToggle.textContent = layoutPrefs.inspectorDrawerOpen ? "Close Inspector" : "Open Inspector";
      inspectorToggle.setAttribute("aria-expanded", String(layoutPrefs.inspectorDrawerOpen));
    } else {
      inspectorToggle.textContent = layoutPrefs.inspectorCollapsed ? "Show Inspector" : "Hide Inspector";
      inspectorToggle.setAttribute("aria-pressed", String(layoutPrefs.inspectorCollapsed));
    }
  }

  function updatePaneWidth(which, delta) {
    if (which === "left") {
      layoutPrefs.leftWidth = clamp(layoutPrefs.leftWidth + delta, PANE_LIMITS.left.min, PANE_LIMITS.left.max);
      layoutPrefs.navCollapsed = false;
      return;
    }

    layoutPrefs.rightWidth = clamp(layoutPrefs.rightWidth + delta, PANE_LIMITS.right.min, PANE_LIMITS.right.max);
    layoutPrefs.inspectorCollapsed = false;
  }

  function bindResizerPointer(resizer, paneKey) {
    resizer.addEventListener("pointerdown", (event) => {
      if (event.button !== 0 || isNarrowScreen) {
        return;
      }

      event.preventDefault();
      const pointerId = event.pointerId;
      const startX = event.clientX;
      const startingWidth = paneKey === "left" ? layoutPrefs.leftWidth : layoutPrefs.rightWidth;

      resizer.setPointerCapture(pointerId);

      const onPointerMove = (moveEvent) => {
        const deltaX = moveEvent.clientX - startX;
        const nextWidth = paneKey === "left" ? startingWidth + deltaX : startingWidth - deltaX;
        if (paneKey === "left") {
          layoutPrefs.leftWidth = clamp(nextWidth, PANE_LIMITS.left.min, PANE_LIMITS.left.max);
          layoutPrefs.navCollapsed = false;
        } else {
          layoutPrefs.rightWidth = clamp(nextWidth, PANE_LIMITS.right.min, PANE_LIMITS.right.max);
          layoutPrefs.inspectorCollapsed = false;
        }
        applyLayoutPrefs();
      };

      const onPointerUp = () => {
        window.removeEventListener("pointermove", onPointerMove);
        window.removeEventListener("pointerup", onPointerUp);
        persistLayoutPrefs(layoutPrefs);
      };

      window.addEventListener("pointermove", onPointerMove);
      window.addEventListener("pointerup", onPointerUp, { once: true });
    });

    resizer.addEventListener("dblclick", () => {
      if (isNarrowScreen) {
        return;
      }
      if (paneKey === "left") {
        layoutPrefs.navCollapsed = !layoutPrefs.navCollapsed;
      } else {
        layoutPrefs.inspectorCollapsed = !layoutPrefs.inspectorCollapsed;
      }
      applyLayoutPrefs();
      persistLayoutPrefs(layoutPrefs);
    });

    resizer.addEventListener("keydown", (event) => {
      if (isNarrowScreen) {
        return;
      }

      if (event.key === "ArrowLeft") {
        event.preventDefault();
        updatePaneWidth(paneKey, paneKey === "left" ? -RESIZE_STEP : RESIZE_STEP);
      } else if (event.key === "ArrowRight") {
        event.preventDefault();
        updatePaneWidth(paneKey, paneKey === "left" ? RESIZE_STEP : -RESIZE_STEP);
      } else if (event.key === "Home") {
        event.preventDefault();
        if (paneKey === "left") {
          layoutPrefs.leftWidth = PANE_LIMITS.left.min;
          layoutPrefs.navCollapsed = false;
        } else {
          layoutPrefs.rightWidth = PANE_LIMITS.right.min;
          layoutPrefs.inspectorCollapsed = false;
        }
      } else if (event.key === "End") {
        event.preventDefault();
        if (paneKey === "left") {
          layoutPrefs.leftWidth = PANE_LIMITS.left.max;
          layoutPrefs.navCollapsed = false;
        } else {
          layoutPrefs.rightWidth = PANE_LIMITS.right.max;
          layoutPrefs.inspectorCollapsed = false;
        }
      } else {
        return;
      }

      applyLayoutPrefs();
      persistLayoutPrefs(layoutPrefs);
    });
  }

  bindResizerPointer(leftResizer, "left");
  bindResizerPointer(rightResizer, "right");

  const screenQuery = window.matchMedia(NARROW_SCREEN_QUERY);
  const onScreenChange = (event) => {
    isNarrowScreen = event.matches;
    if (!isNarrowScreen) {
      layoutPrefs.inspectorDrawerOpen = false;
    }
    applyLayoutPrefs();
    persistLayoutPrefs(layoutPrefs);
  };

  if (typeof screenQuery.addEventListener === "function") {
    screenQuery.addEventListener("change", onScreenChange);
  } else {
    screenQuery.addListener(onScreenChange);
  }

  window.addEventListener("keydown", (event) => {
    const tag = String(document.activeElement?.tagName || "").toLowerCase();
    const isTypingContext =
      tag === "input" || tag === "textarea" || document.activeElement?.getAttribute?.("contenteditable") === "true";

    if (event.key === "Escape" && isNarrowScreen && layoutPrefs.inspectorDrawerOpen) {
      layoutPrefs.inspectorDrawerOpen = false;
      applyLayoutPrefs();
      persistLayoutPrefs(layoutPrefs);
      return;
    }

    if (event.key === "/" && !isTypingContext) {
      event.preventDefault();
      const searchInput =
        content.querySelector('input[type="search"]') ||
        content.querySelector(".settings-search") ||
        content.querySelector('input[placeholder*="Search"]');
      if (searchInput) {
        searchInput.focus();
      }
      return;
    }

    if (isTypingContext || event.altKey || event.ctrlKey || event.metaKey) {
      return;
    }

    const now = Date.now();
    if (!window.__dashboardChordState || now - window.__dashboardChordState.ts > 1200) {
      window.__dashboardChordState = { key: "", ts: now };
    }
    const chord = window.__dashboardChordState;
    const key = String(event.key || "").toLowerCase();
    if (chord.key === "g") {
      if (key === "c") {
        event.preventDefault();
        router.navigate("/chat");
      } else if (key === "r") {
        event.preventDefault();
        router.navigate("/runs");
      } else if (key === "s") {
        event.preventDefault();
        router.navigate("/scheduler");
      }
      window.__dashboardChordState = { key: "", ts: now };
      return;
    }
    if (key === "g") {
      window.__dashboardChordState = { key: "g", ts: now };
    }
  });

  applyLayoutPrefs();

  function renderNav(state) {
    nav.innerHTML = "";
    const list = createElement("ul", "nav-list");
    for (const route of routes) {
      const item = createElement("li", "nav-item");
      const link = createElement("a", state.route === route.path ? "active" : "", route.label);
      link.href = `#${route.path}`;
      link.addEventListener("click", (event) => {
        event.preventDefault();
        router.navigate(route.path);
      });
      item.append(link);
      list.append(item);
    }
    nav.append(list);
  }

  function renderAdminStatusStamp(state) {
    const runtime = state?.adminStatus || {};
    if (runtime.loading) {
      statusStamp.textContent = "Runtime: loading status...";
      return;
    }
    if (runtime.error) {
      statusStamp.textContent = `Runtime status unavailable: ${runtime.error}`;
      return;
    }

    const provider = String(runtime.provider || "").trim();
    const model = String(runtime.model || "").trim();
    const runCount = Number(runtime.run_count) || 0;

    if (!provider && !model) {
      statusStamp.textContent = "Runtime: provider/model unknown";
      return;
    }
    statusStamp.textContent = `Runtime: ${provider || "unknown"} / ${model || "unknown"} · runs ${runCount}`;
  }

  async function renderContent(state) {
    const selected = routes.find((route) => route.path === state.route) || routes[0];
    if (!selected) {
      content.textContent = "No routes configured.";
      return;
    }
    await selected.page.render({ container: content, state, store, apiClient, router });
  }

  function buildBugReportURL(state) {
    const lastError = state?.lastError || null;
    const selectedTrace = state?.selectedTrace || null;
    const selectedTool = state?.selectedTool || null;
    const runID = String(selectedTrace?.run_id || selectedTool?.run_id || "").trim();
    const sessionID = String(lastError?.session_id || "").trim();
    const errorSummary = String(lastError?.message || "No error captured.").trim();

    const body = [
      "## Dashboard Bug Report",
      "",
      `- Route: ${state?.route || ""}`,
      `- Run ID: ${runID || "(unknown)"}`,
      `- Session ID: ${sessionID || "(unknown)"}`,
      `- Error: ${errorSummary}`,
      "",
      "## Reproduction",
      "1. ...",
      "2. ...",
      "",
      "## Notes",
      "Add screenshots or extra context here.",
    ].join("\n");

    const params = new URLSearchParams({
      title: `dashboard: ${state?.route || "route"} issue`,
      body,
      labels: "dashboard,bug",
    });
    return `https://github.com/mojomast/openclawssy/issues/new?${params.toString()}`;
  }

  async function renderInspector(state) {
    inspector.innerHTML = "";

    const tabs = createElement("div", "inspector-tabs");
    const body = createElement("div", "inspector-body");
    for (const item of inspectors) {
      const button = createElement("button", state.inspectorTab === item.key ? "active" : "", item.label);
      button.type = "button";
      button.addEventListener("click", () => {
        store.setState({ inspectorTab: item.key });
      });
      tabs.append(button);
    }

    const active = inspectors.find((item) => item.key === state.inspectorTab) || inspectors[0];
    if (active) {
      await active.render({ container: body, state, store });
    }

    inspector.append(tabs, body);
  }

  async function render(state) {
    applyLayoutPrefs();
    renderAdminStatusStamp(state);
    renderNav(state);
    bugLink.href = buildBugReportURL(state);
    await renderContent(state);
    await renderInspector(state);
  }

  return {
    render,
  };
}
