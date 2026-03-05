import { contextualHelpTopics, loadHelpTopics, searchHelpTopics } from "../help/content.js";
import { renderMarkdownToFragment } from "../help/markdown.js";

const LAYOUT_STORAGE_KEY = "dashboard.layout.p1.2";
const THEME_STORAGE_KEY = "dashboard.theme";
const HELP_DRAWER_STORAGE_KEY = "dashboard.help_drawer.p1";

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

function readHelpDrawerPrefs() {
  const defaults = { open: false, width: 360 };
  try {
    const raw = window.localStorage.getItem(HELP_DRAWER_STORAGE_KEY);
    if (!raw) return defaults;
    const parsed = JSON.parse(raw);
    return {
      open: Boolean(parsed.open),
      width: clamp(Number(parsed.width) || defaults.width, 300, 560),
    };
  } catch (_error) {
    return defaults;
  }
}

function persistHelpDrawerPrefs(prefs) {
  try {
    window.localStorage.setItem(HELP_DRAWER_STORAGE_KEY, JSON.stringify(prefs));
  } catch (_error) {
    // ignore
  }
}

function highlightHelpMatch(text, query) {
  const source = String(text || "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/\"/g, "&quot;")
    .replace(/'/g, "&#39;");
  const q = String(query || "").trim();
  if (!q) return source;
  const escaped = q.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return source.replace(new RegExp(`(${escaped})`, "ig"), "<mark>$1</mark>");
}

function currentHelpHashTopic() {
  const hash = window.location.hash || "";
  const queryIndex = hash.indexOf("?");
  if (queryIndex < 0) {
    return "";
  }
  const params = new URLSearchParams(hash.slice(queryIndex + 1));
  return String(params.get("topic") || "").trim();
}

export function createLayout({ root, routes, store, router, apiClient, inspectors }) {
  root.innerHTML = "";

  const layoutPrefs = readLayoutPrefs();
  const helpDrawerPrefs = readHelpDrawerPrefs();
  let isNarrowScreen = window.matchMedia(NARROW_SCREEN_QUERY).matches;
  let helpTopics = [];
  const helpDrawerState = {
    search: "",
    selectedTopicId: "",
    renderKey: "",
    topicsLoaded: false,
    topicsLoading: false,
    forceRender: true,
  };

  function requestHelpDrawerRender() {
    helpDrawerState.forceRender = true;
  }

  function toggleHelpDrawer(forceOpen) {
    helpDrawerPrefs.open = typeof forceOpen === "boolean" ? forceOpen : !helpDrawerPrefs.open;
    requestHelpDrawerRender();
    applyLayoutPrefs();
    persistHelpDrawerPrefs(helpDrawerPrefs);
    if (!helpDrawerPrefs.open) {
      helpDrawer.innerHTML = "";
      helpDrawerState.renderKey = "closed";
      helpDrawerState.forceRender = false;
    }
  }

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

  const helpToggle = createElement("button", "layout-toggle help-toggle", "?");
  helpToggle.type = "button";
  helpToggle.setAttribute("aria-label", "Toggle Help Drawer");
  helpToggle.addEventListener("click", () => {
    toggleHelpDrawer();
    void renderHelpDrawer(store.getState());
  });

  headerActions.append(navToggle, inspectorToggle, helpToggle, themeToggle);
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

  const helpDrawer = createElement("aside", "help-drawer");
  const helpDrawerResizer = createElement("div", "help-drawer-resizer");
  helpDrawerResizer.setAttribute("role", "separator");
  helpDrawerResizer.tabIndex = 0;
  const helpBackdrop = createElement("button", "help-backdrop");
  helpBackdrop.type = "button";
  helpBackdrop.setAttribute("aria-label", "Close help drawer");
  helpBackdrop.addEventListener("click", () => {
    toggleHelpDrawer(false);
  });

  root.append(header, shellGrid, footer, inspectorBackdrop, helpDrawerResizer, helpDrawer, helpBackdrop);

  function applyLayoutPrefs() {
    root.classList.toggle("is-narrow-screen", isNarrowScreen);
    root.classList.toggle("nav-collapsed", !isNarrowScreen && layoutPrefs.navCollapsed);
    root.classList.toggle("inspector-collapsed", !isNarrowScreen && layoutPrefs.inspectorCollapsed);
    root.classList.toggle("inspector-drawer-open", isNarrowScreen && layoutPrefs.inspectorDrawerOpen);
    root.classList.toggle("help-drawer-open", helpDrawerPrefs.open);

    shellGrid.style.setProperty("--pane-left", `${layoutPrefs.leftWidth}px`);
    shellGrid.style.setProperty("--pane-right", `${layoutPrefs.rightWidth}px`);
    root.style.setProperty("--help-drawer-width", `${helpDrawerPrefs.width}px`);

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
    helpToggle.setAttribute("aria-pressed", String(helpDrawerPrefs.open));
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
    if (event.key === "Escape" && helpDrawerPrefs.open) {
      toggleHelpDrawer(false);
      return;
    }

    if ((event.key === "F1" || event.key === "?" || (event.key === "/" && event.shiftKey)) && !isTypingContext) {
      event.preventDefault();
      toggleHelpDrawer();
      void renderHelpDrawer(store.getState());
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

  async function ensureHelpTopics() {
    if (!helpTopics.length && !helpDrawerState.topicsLoading) {
      helpDrawerState.topicsLoading = true;
      requestHelpDrawerRender();
      helpTopics = await loadHelpTopics();
      helpDrawerState.topicsLoaded = true;
      helpDrawerState.topicsLoading = false;
      requestHelpDrawerRender();
    }
    return helpTopics;
  }

  async function renderHelpDrawer(state) {
    if (!helpDrawerPrefs.open) {
      if (helpDrawerState.renderKey !== "closed" || helpDrawerState.forceRender) {
        helpDrawer.innerHTML = "";
        helpDrawerState.renderKey = "closed";
        helpDrawerState.forceRender = false;
      }
      return;
    }

    if (state.route === "/help") {
      helpDrawerState.selectedTopicId = currentHelpHashTopic() || helpDrawerState.selectedTopicId || "getting-started";
    } else if (!helpDrawerState.selectedTopicId) {
      helpDrawerState.selectedTopicId = "getting-started";
    }

    const renderKey = JSON.stringify({
      open: helpDrawerPrefs.open,
      route: state.route,
      search: helpDrawerState.search,
      selected: helpDrawerState.selectedTopicId,
      topicsLoaded: helpDrawerState.topicsLoaded,
      topicsLoading: helpDrawerState.topicsLoading,
    });
    if (!helpDrawerState.forceRender && helpDrawerState.renderKey === renderKey) {
      return;
    }
    helpDrawerState.renderKey = renderKey;
    helpDrawerState.forceRender = false;
    helpDrawer.innerHTML = "";
    const title = createElement("div", "help-drawer-header");
    const titleText = createElement("div", "", "");
    titleText.innerHTML = `<h3>Help Drawer</h3><p class="muted">Stay oriented while you work.</p>`;
    const openFull = createElement("button", "layout-toggle", "Open full Help Center");
    openFull.type = "button";
    openFull.addEventListener("click", () => {
      const topicParam = helpDrawerState.selectedTopicId ? `?topic=${encodeURIComponent(helpDrawerState.selectedTopicId)}` : "";
      router.navigate(`/help${topicParam}`);
    });
    title.append(titleText, openFull);
    helpDrawer.append(title);

    const searchWrap = createElement("label", "help-drawer-search");
    const searchInput = document.createElement("input");
    searchInput.type = "search";
    searchInput.className = "settings-input";
    searchInput.placeholder = "Search help topics";
    searchInput.value = helpDrawerState.search;
    searchInput.addEventListener("input", () => {
      helpDrawerState.search = searchInput.value;
      requestHelpDrawerRender();
      void renderHelpDrawer(state).then(() => {
        const next = helpDrawer.querySelector(".help-drawer-search input");
        next?.focus();
        if (typeof next?.setSelectionRange === "function") {
          next.setSelectionRange(helpDrawerState.search.length, helpDrawerState.search.length);
        }
      });
    });
    searchWrap.append(searchInput);
    helpDrawer.append(searchWrap);

    const body = createElement("div", "help-drawer-body");
    try {
      if (helpDrawerState.topicsLoading && !helpDrawerState.topicsLoaded) {
        body.append(createElement("p", "muted", "Loading help topics..."));
        helpDrawer.append(body);
        void ensureHelpTopics().then(() => renderHelpDrawer(state));
        return;
      }
      const topics = await ensureHelpTopics();
      const contextual = contextualHelpTopics(topics, state.route).slice(0, 6);
      const matches = searchHelpTopics(topics, helpDrawerState.search).slice(0, helpDrawerState.search ? 8 : 5);
      const sections = [
        { title: "Contextual help", items: contextual },
        { title: "Quick links", items: topics.filter((item) => ["discord-bot-setup", "providers-and-models", "secrets-guide", "custom-dashboards", "runs-and-debugging"].includes(item.id)) },
        { title: helpDrawerState.search ? "Search results" : "Top topics", items: matches },
      ];
      sections.forEach((section) => {
        const details = document.createElement("details");
        details.className = "help-drawer-section";
        details.open = true;
        const summary = document.createElement("summary");
        summary.textContent = section.title;
        details.append(summary);
        section.items.forEach((item) => {
          const button = createElement("button", `help-topic-link ${item.id === helpDrawerState.selectedTopicId ? "active" : ""}`.trim(), item.title);
          button.type = "button";
          button.innerHTML = `${highlightHelpMatch(item.title, helpDrawerState.search)}<span class="muted">${item.category}</span>`;
          button.addEventListener("click", () => {
            helpDrawerState.selectedTopicId = item.id;
            requestHelpDrawerRender();
            void renderHelpDrawer(state);
          });
          details.append(button);
        });
        body.append(details);
      });

      const selectedTopic = topics.find((item) => item.id === helpDrawerState.selectedTopicId) || contextual[0] || matches[0] || topics[0] || null;
      if (selectedTopic) {
        helpDrawerState.selectedTopicId = selectedTopic.id;
        const preview = createElement("section", "help-drawer-preview");
        const previewTitle = createElement("h4", "", selectedTopic.title);
        const previewMeta = createElement("p", "muted", `Preview · ${selectedTopic.category}`);
        const previewBody = createElement("div", "help-markdown");
        previewBody.append(renderMarkdownToFragment(selectedTopic.body));
        const previewActions = createElement("div", "help-topic-actions");
        const openTopic = createElement("button", "layout-toggle", "Open in Help Center");
        openTopic.type = "button";
        openTopic.addEventListener("click", () => {
          router.navigate(`/help?topic=${encodeURIComponent(selectedTopic.id)}`);
        });
        previewActions.append(openTopic);
        preview.append(previewTitle, previewMeta, previewActions, previewBody);
        body.append(preview);
      }
    } catch (error) {
      body.append(createElement("p", "settings-inline-error", error instanceof Error ? error.message : String(error)));
    }
    helpDrawer.append(body);
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
    if (helpDrawerPrefs.open) {
      await renderHelpDrawer(state);
    }
  }

  helpDrawerResizer.addEventListener("pointerdown", (event) => {
    if (event.button !== 0 || isNarrowScreen) return;
    event.preventDefault();
    const startX = event.clientX;
    const startWidth = helpDrawerPrefs.width;
    const onMove = (moveEvent) => {
      helpDrawerPrefs.width = clamp(startWidth - (moveEvent.clientX - startX), 300, 560);
      applyLayoutPrefs();
    };
    const onUp = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      persistHelpDrawerPrefs(helpDrawerPrefs);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp, { once: true });
  });

  return {
    render,
  };
}
