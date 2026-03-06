const WORKSPACE_AUTO_REFRESH_MS = 4000;

const workspaceViewState = {
  container: null,
  apiClient: null,
  store: null,
  workspaceRoot: "",
  currentPath: ".",
  parentPath: "",
  entries: [],
  selectedPath: "",
  selectedFile: null,
  filterQuery: "",
  autoRefresh: false,
  loadingEntries: false,
  loadingFile: false,
  hasLoaded: false,
  statusText: "",
  statusKind: "",
  refreshTimer: 0,
};

function isWorkspaceRouteActive() {
  return workspaceViewState.store?.getState?.().route === "/workspace";
}

function rerender() {
  if (!workspaceViewState.container || !workspaceViewState.container.isConnected) {
    return;
  }
  renderWorkspacePage();
}

function setStatus(text, kind = "") {
  workspaceViewState.statusText = String(text || "");
  workspaceViewState.statusKind = kind;
}

function asText(value) {
  return String(value || "").trim();
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

function formatBytes(value) {
  const size = Number(value) || 0;
  if (size < 1024) {
    return `${size} B`;
  }
  if (size < 1024 * 1024) {
    return `${(size / 1024).toFixed(1)} KB`;
  }
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

function clearRefreshTimer() {
  if (workspaceViewState.refreshTimer) {
    window.clearTimeout(workspaceViewState.refreshTimer);
    workspaceViewState.refreshTimer = 0;
  }
}

function scheduleRefresh() {
  clearRefreshTimer();
  if (!workspaceViewState.autoRefresh || !isWorkspaceRouteActive()) {
    return;
  }
  workspaceViewState.refreshTimer = window.setTimeout(() => {
    if (!isWorkspaceRouteActive()) {
      clearRefreshTimer();
      return;
    }
    void refreshWorkspace({ silent: true });
  }, WORKSPACE_AUTO_REFRESH_MS);
}

function normalizeEntry(entry) {
  if (!entry || typeof entry !== "object") {
    return null;
  }
  return {
    name: asText(entry.name),
    path: asText(entry.path),
    kind: asText(entry.kind) === "dir" ? "dir" : "file",
    sizeBytes: Number(entry.size_bytes) || 0,
    modifiedAt: asText(entry.modified_at),
    mimeType: asText(entry.mime_type),
  };
}

function entryMatchesFilter(entry) {
  const query = asText(workspaceViewState.filterQuery).toLowerCase();
  if (!query) {
    return true;
  }
  return `${entry.name} ${entry.path} ${entry.kind}`.toLowerCase().includes(query);
}

function parentDirectory(pathValue) {
  const clean = asText(pathValue);
  if (!clean || clean === ".") {
    return ".";
  }
  const parts = clean.split("/").filter(Boolean);
  parts.pop();
  return parts.length ? parts.join("/") : ".";
}

async function loadEntries(pathValue = workspaceViewState.currentPath, options = {}) {
  if (workspaceViewState.loadingEntries) {
    return;
  }
  const { keepStatus = false } = options;
  workspaceViewState.loadingEntries = true;
  if (!keepStatus) {
    setStatus(`Loading ${asText(pathValue) || "."}...`);
  }
  rerender();
  try {
    const payload = await workspaceViewState.apiClient.get(
      `/api/admin/workspace/entries?path=${encodeURIComponent(asText(pathValue) || ".")}`
    );
    workspaceViewState.workspaceRoot = asText(payload?.workspace_root);
    workspaceViewState.currentPath = asText(payload?.path) || ".";
    workspaceViewState.parentPath = asText(payload?.parent_path);
    workspaceViewState.entries = Array.isArray(payload?.entries)
      ? payload.entries.map(normalizeEntry).filter((entry) => entry && entry.path)
      : [];
    workspaceViewState.hasLoaded = true;
    if (!keepStatus) {
      setStatus(`Loaded ${workspaceViewState.entries.length} item(s).`, "success");
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    setStatus(`Failed to load workspace entries: ${message}`, "error");
  } finally {
    workspaceViewState.loadingEntries = false;
    rerender();
  }
}

async function loadFile(pathValue, options = {}) {
  if (workspaceViewState.loadingFile) {
    return;
  }
  const { keepStatus = false } = options;
  workspaceViewState.loadingFile = true;
  workspaceViewState.selectedPath = asText(pathValue);
  if (!keepStatus) {
    setStatus(`Opening ${workspaceViewState.selectedPath}...`);
  }
  rerender();
  try {
    const payload = await workspaceViewState.apiClient.get(
      `/api/admin/workspace/file?path=${encodeURIComponent(workspaceViewState.selectedPath)}`
    );
    workspaceViewState.selectedFile = {
      path: asText(payload?.path),
      name: asText(payload?.name),
      sizeBytes: Number(payload?.size_bytes) || 0,
      modifiedAt: asText(payload?.modified_at),
      mimeType: asText(payload?.mime_type),
      isText: Boolean(payload?.is_text),
      truncated: Boolean(payload?.truncated),
      previewNotice: asText(payload?.preview_notice),
      content: typeof payload?.content === "string" ? payload.content : "",
    };
    workspaceViewState.selectedPath = workspaceViewState.selectedFile.path;
    if (!keepStatus) {
      setStatus(`Opened ${workspaceViewState.selectedFile.path}.`, "success");
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    workspaceViewState.selectedFile = null;
    if (!keepStatus) {
      setStatus(`Failed to open file: ${message}`, "error");
    }
  } finally {
    workspaceViewState.loadingFile = false;
    rerender();
  }
}

async function openEntry(entry) {
  if (!entry) {
    return;
  }
  if (entry.kind === "dir") {
    workspaceViewState.selectedPath = "";
    workspaceViewState.selectedFile = null;
    await loadEntries(entry.path);
    return;
  }
  await loadFile(entry.path);
}

async function refreshWorkspace(options = {}) {
  const { silent = false } = options;
  await loadEntries(workspaceViewState.currentPath, { keepStatus: silent });
  if (workspaceViewState.selectedPath) {
    const selectedDir = parentDirectory(workspaceViewState.selectedPath);
    if (selectedDir === workspaceViewState.currentPath) {
      await loadFile(workspaceViewState.selectedPath, { keepStatus: true });
    }
  }
  scheduleRefresh();
}

function createBreadcrumbs() {
  const nav = document.createElement("nav");
  nav.className = "workspace-breadcrumbs";
  const rootButton = document.createElement("button");
  rootButton.type = "button";
  rootButton.className = "button-link";
  rootButton.textContent = "workspace";
  rootButton.addEventListener("click", () => {
    workspaceViewState.selectedPath = "";
    workspaceViewState.selectedFile = null;
    void loadEntries(".");
  });
  nav.append(rootButton);
  const current = asText(workspaceViewState.currentPath);
  if (!current || current === ".") {
    return nav;
  }
  const parts = current.split("/").filter(Boolean);
  parts.forEach((part, index) => {
    nav.append(document.createTextNode(" / "));
    const button = document.createElement("button");
    button.type = "button";
    button.className = "button-link";
    button.textContent = part;
    const nextPath = parts.slice(0, index + 1).join("/");
    button.addEventListener("click", () => {
      workspaceViewState.selectedPath = "";
      workspaceViewState.selectedFile = null;
      void loadEntries(nextPath);
    });
    nav.append(button);
  });
  return nav;
}

function createToolbar() {
  const toolbar = document.createElement("section");
  toolbar.className = "workspace-toolbar";

  const info = document.createElement("div");
  info.className = "workspace-toolbar-info";
  const title = document.createElement("h3");
  title.textContent = "Workspace Browser";
  const subtitle = document.createElement("p");
  subtitle.className = "muted";
  subtitle.textContent = workspaceViewState.workspaceRoot
    ? `Root: ${workspaceViewState.workspaceRoot}`
    : "Browse the active workspace tree and preview files safely from the dashboard.";
  info.append(title, subtitle, createBreadcrumbs());

  const actions = document.createElement("div");
  actions.className = "workspace-toolbar-actions";
  const filter = document.createElement("input");
  filter.type = "search";
  filter.className = "workspace-filter-input";
  filter.placeholder = "Filter current folder";
  filter.value = workspaceViewState.filterQuery;
  filter.addEventListener("input", () => {
    workspaceViewState.filterQuery = filter.value;
    rerender();
  });

  const autoRefreshRow = document.createElement("label");
  autoRefreshRow.className = "workspace-auto-refresh";
  const autoRefreshToggle = document.createElement("input");
  autoRefreshToggle.type = "checkbox";
  autoRefreshToggle.checked = workspaceViewState.autoRefresh;
  autoRefreshToggle.addEventListener("change", () => {
    workspaceViewState.autoRefresh = autoRefreshToggle.checked;
    scheduleRefresh();
    rerender();
  });
  const autoRefreshText = document.createElement("span");
  autoRefreshText.textContent = `Auto refresh (${WORKSPACE_AUTO_REFRESH_MS / 1000}s)`;
  autoRefreshRow.append(autoRefreshToggle, autoRefreshText);

  const upButton = document.createElement("button");
  upButton.type = "button";
  upButton.className = "layout-toggle";
  upButton.textContent = "Up";
  upButton.disabled = workspaceViewState.loadingEntries || !workspaceViewState.parentPath;
  upButton.addEventListener("click", () => {
    workspaceViewState.selectedPath = "";
    workspaceViewState.selectedFile = null;
    void loadEntries(workspaceViewState.parentPath || ".");
  });

  const reloadButton = document.createElement("button");
  reloadButton.type = "button";
  reloadButton.className = "layout-toggle";
  reloadButton.textContent = workspaceViewState.loadingEntries || workspaceViewState.loadingFile ? "Refreshing..." : "Refresh";
  reloadButton.disabled = workspaceViewState.loadingEntries || workspaceViewState.loadingFile;
  reloadButton.addEventListener("click", () => {
    void refreshWorkspace();
  });

  actions.append(filter, autoRefreshRow, upButton, reloadButton);
  toolbar.append(info, actions);
  return toolbar;
}

function createStatus() {
  const status = document.createElement("p");
  status.className = workspaceViewState.statusKind ? `workspace-status ${workspaceViewState.statusKind}` : "workspace-status muted";
  status.textContent = workspaceViewState.statusText || "Use the browser to inspect files and folders inside the active workspace.";
  return status;
}

function createEntryList() {
  const panel = document.createElement("section");
  panel.className = "workspace-list-panel";
  const heading = document.createElement("div");
  heading.className = "workspace-panel-header";
  const title = document.createElement("h4");
  title.textContent = `Entries (${workspaceViewState.entries.length})`;
  const meta = document.createElement("span");
  meta.className = "muted";
  meta.textContent = asText(workspaceViewState.currentPath) || ".";
  heading.append(title, meta);
  panel.append(heading);

  const entries = workspaceViewState.entries.filter(entryMatchesFilter);
  if (!entries.length) {
    const empty = document.createElement("p");
    empty.className = "muted";
    empty.textContent = workspaceViewState.filterQuery ? "No entries match this filter." : "This folder is empty.";
    panel.append(empty);
    return panel;
  }

  const list = document.createElement("div");
  list.className = "workspace-entry-list";
  entries.forEach((entry) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `workspace-entry ${entry.path === workspaceViewState.selectedPath ? "selected" : ""}`;
    button.addEventListener("click", () => {
      void openEntry(entry);
    });
    const name = document.createElement("strong");
    name.textContent = `${entry.kind === "dir" ? "DIR" : "FILE"} ${entry.name}`;
    const metaLine = document.createElement("span");
    metaLine.textContent = entry.kind === "dir"
      ? `${formatDateTime(entry.modifiedAt)}`
      : `${formatBytes(entry.sizeBytes)} · ${formatDateTime(entry.modifiedAt)}`;
    button.append(name, metaLine);
    list.append(button);
  });
  panel.append(list);
  return panel;
}

function createPreviewPanel() {
  const panel = document.createElement("section");
  panel.className = "workspace-preview-panel";
  const heading = document.createElement("div");
  heading.className = "workspace-panel-header";
  const title = document.createElement("h4");
  title.textContent = "Preview";
  heading.append(title);
  panel.append(heading);

  const file = workspaceViewState.selectedFile;
  if (!file) {
    const empty = document.createElement("p");
    empty.className = "muted";
    empty.textContent = "Select a file to preview its contents.";
    panel.append(empty);
    return panel;
  }

  const meta = document.createElement("p");
  meta.className = "workspace-preview-meta";
  meta.textContent = `${file.path} · ${formatBytes(file.sizeBytes)}${file.mimeType ? ` · ${file.mimeType}` : ""}${file.modifiedAt ? ` · ${formatDateTime(file.modifiedAt)}` : ""}`;
  panel.append(meta);

  if (file.previewNotice) {
    const note = document.createElement("p");
    note.className = `workspace-status ${file.isText ? "" : "warning"}`.trim();
    note.textContent = file.previewNotice;
    panel.append(note);
  }

  if (file.isText) {
    const body = document.createElement("pre");
    body.className = "workspace-preview-body";
    body.textContent = file.content;
    panel.append(body);
  }
  return panel;
}

function renderWorkspacePage() {
  const container = workspaceViewState.container;
  if (!container) {
    return;
  }
  clearRefreshTimer();
  container.innerHTML = "";

  const heading = document.createElement("h2");
  heading.textContent = "Workspace";
  const note = document.createElement("p");
  note.className = "muted";
  note.textContent = "Browse the active workspace, inspect folders, and preview text files without leaving the dashboard.";

  const page = document.createElement("section");
  page.className = "workspace-page";
  page.append(createToolbar(), createStatus());

  const browser = document.createElement("section");
  browser.className = "workspace-browser";
  browser.append(createEntryList(), createPreviewPanel());
  page.append(browser);
  container.append(heading, note, page);
  scheduleRefresh();
}

export const workspacePage = {
  key: "workspace",
  title: "Workspace",
  async render({ container, apiClient, store }) {
    workspaceViewState.container = container;
    workspaceViewState.apiClient = apiClient;
    workspaceViewState.store = store;
    renderWorkspacePage();
    if (!workspaceViewState.hasLoaded) {
      await refreshWorkspace();
      return;
    }
    scheduleRefresh();
  },
};
