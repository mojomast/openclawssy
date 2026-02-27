import { createSandboxApi } from "../api/sandbox.js";

/**
 * Sandbox Manager page.
 * Provides a UI for all /api/admin/sandbox/docker/* operations.
 */

const sandboxState = {
  container: null,
  sandboxApi: null,
  /** Current agent ID being managed */
  agentId: "default",
  /** Status response from getStatus() */
  status: null,
  statusLoading: false,
  statusError: "",
  /** Whether any action (create/stop/reset/pull/delete) is in-progress */
  actionPending: false,
  actionError: "",
  actionSuccess: "",
  /** Pull image input value */
  pullImage: "ubuntu:24.04",
  pullPending: false,
  pullError: "",
  pullSuccess: "",
  /** Images list */
  images: null,
  imagesLoading: false,
  imagesError: "",
  /** Volumes list */
  volumes: null,
  volumesLoading: false,
  volumesError: "",
  /** Advanced mount config textarea value (display-only) */
  mountConfig: "# Example:\n# /host/path:/container/path:ro\n",
  advancedOpen: false,
};

// ─── Re-render ────────────────────────────────────────────────────────────────

function rerender() {
  if (!sandboxState.container || !sandboxState.container.isConnected) {
    return;
  }
  renderSandboxPage();
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function el(tag, className, text) {
  const e = document.createElement(tag);
  if (className) e.className = className;
  if (text !== undefined && text !== null) e.textContent = text;
  return e;
}

function badge(text, variant) {
  // variant: "success" | "danger" | "info" | "warning" | "neutral"
  const b = el("span", `sandbox-badge sandbox-badge--${variant}`, text);
  return b;
}

function spinner() {
  const s = el("span", "sandbox-spinner");
  s.setAttribute("aria-label", "Loading");
  return s;
}

// ─── Data loading ─────────────────────────────────────────────────────────────

async function loadStatus() {
  sandboxState.statusLoading = true;
  sandboxState.statusError = "";
  rerender();
  try {
    const data = await sandboxState.sandboxApi.getStatus(sandboxState.agentId);
    sandboxState.status = data;
  } catch (error) {
    sandboxState.statusError = error instanceof Error ? error.message : String(error);
  } finally {
    sandboxState.statusLoading = false;
    rerender();
  }
}

async function loadImages() {
  sandboxState.imagesLoading = true;
  sandboxState.imagesError = "";
  rerender();
  try {
    const data = await sandboxState.sandboxApi.getImages();
    sandboxState.images = Array.isArray(data?.images) ? data.images : [];
  } catch (error) {
    sandboxState.imagesError = error instanceof Error ? error.message : String(error);
  } finally {
    sandboxState.imagesLoading = false;
    rerender();
  }
}

async function loadVolumes() {
  sandboxState.volumesLoading = true;
  sandboxState.volumesError = "";
  rerender();
  try {
    const data = await sandboxState.sandboxApi.getVolumes();
    sandboxState.volumes = Array.isArray(data?.volumes) ? data.volumes : [];
  } catch (error) {
    sandboxState.volumesError = error instanceof Error ? error.message : String(error);
  } finally {
    sandboxState.volumesLoading = false;
    rerender();
  }
}

// ─── Actions ──────────────────────────────────────────────────────────────────

async function runAction(label, fn) {
  if (sandboxState.actionPending) return;
  sandboxState.actionPending = true;
  sandboxState.actionError = "";
  sandboxState.actionSuccess = "";
  rerender();
  try {
    await fn();
    sandboxState.actionSuccess = `${label} succeeded.`;
    // Refresh status after action
    await loadStatus();
  } catch (error) {
    sandboxState.actionError = error instanceof Error ? error.message : String(error);
  } finally {
    sandboxState.actionPending = false;
    rerender();
  }
}

async function doCreate() {
  await runAction("Create container", () =>
    sandboxState.sandboxApi.createContainer(sandboxState.agentId)
  );
}

async function doStop() {
  await runAction("Stop container", () =>
    sandboxState.sandboxApi.stopContainer(sandboxState.agentId)
  );
}

async function doReset() {
  const confirmed = window.confirm(
    "This will destroy all files in the container volume. Are you sure?"
  );
  if (!confirmed) return;
  await runAction("Reset container", () =>
    sandboxState.sandboxApi.resetContainer(sandboxState.agentId)
  );
  // Also reload volumes since volume is recreated
  await loadVolumes();
}

async function doPull() {
  const image = sandboxState.pullImage.trim();
  if (!image) return;
  sandboxState.pullPending = true;
  sandboxState.pullError = "";
  sandboxState.pullSuccess = "";
  rerender();
  try {
    await sandboxState.sandboxApi.pullImage(image);
    sandboxState.pullSuccess = `Pulled image: ${image}`;
    await loadImages();
  } catch (error) {
    sandboxState.pullError = error instanceof Error ? error.message : String(error);
  } finally {
    sandboxState.pullPending = false;
    rerender();
  }
}

async function doDeleteVolume(name) {
  const confirmed = window.confirm(
    `Delete volume "${name}"? This cannot be undone.`
  );
  if (!confirmed) return;
  sandboxState.actionPending = true;
  sandboxState.actionError = "";
  sandboxState.actionSuccess = "";
  rerender();
  try {
    await sandboxState.sandboxApi.deleteVolume(name);
    sandboxState.actionSuccess = `Deleted volume: ${name}`;
    await loadVolumes();
  } catch (error) {
    sandboxState.actionError = error instanceof Error ? error.message : String(error);
  } finally {
    sandboxState.actionPending = false;
    rerender();
  }
}

// ─── Section builders ─────────────────────────────────────────────────────────

/**
 * Header: "Sandbox Manager" with provider badge.
 */
function buildHeader() {
  const section = el("section", "sandbox-header-section");
  const row = el("div", "sandbox-header-row");

  const heading = el("h2", "", "Sandbox Manager");

  const provider = sandboxState.status?.provider || "";
  const providerVariant =
    provider === "docker" ? "success" : provider === "local" ? "info" : "neutral";
  const providerBadge = badge(provider || "unknown", providerVariant);
  providerBadge.setAttribute("data-testid", "provider-badge");

  row.append(heading, providerBadge);
  section.append(row);

  const subtitle = el("p", "muted", "Manage Docker sandbox containers, images, and volumes.");
  section.append(subtitle);

  return section;
}

/**
 * Status card: shows container info or a message if not docker.
 */
function buildStatusCard() {
  const card = el("article", "sandbox-card");
  const titleRow = el("div", "sandbox-card-title-row");
  const title = el("h3", "", "Container Status");

  const refreshBtn = el("button", "layout-toggle", "Refresh");
  refreshBtn.type = "button";
  refreshBtn.disabled = sandboxState.statusLoading;
  refreshBtn.setAttribute("data-testid", "refresh-btn");
  refreshBtn.addEventListener("click", () => {
    void loadStatus();
  });

  if (sandboxState.statusLoading) {
    refreshBtn.append(spinner());
  }

  titleRow.append(title, refreshBtn);
  card.append(titleRow);

  if (sandboxState.statusLoading && !sandboxState.status) {
    card.append(el("p", "muted", "Loading status..."));
    return card;
  }

  if (sandboxState.statusError) {
    const errEl = el("p", "settings-inline-error", `Error: ${sandboxState.statusError}`);
    errEl.setAttribute("data-testid", "status-error");
    card.append(errEl);
    return card;
  }

  const status = sandboxState.status;
  if (!status) {
    card.append(el("p", "muted", "Status not loaded yet. Click Refresh."));
    return card;
  }

  const provider = String(status.provider || "").trim();
  if (provider !== "docker") {
    const notice = el(
      "p",
      "sandbox-notice sandbox-notice--warning",
      "Switch to docker provider in Settings → Sandbox to use container management."
    );
    notice.setAttribute("data-testid", "non-docker-notice");
    card.append(notice);
    return card;
  }

  const table = el("table", "sandbox-status-table");
  const tbody = el("tbody");

  const rows = [
    ["Container name", status.container_name || "—"],
    ["Container ID", status.container_id ? status.container_id.slice(0, 12) : "—"],
    ["Image", status.image || "—"],
    ["Workspace path", status.workspace_path || "—"],
    ["Volume name", status.volume_name || "—"],
    ["Network mode", status.network_mode || "—"],
  ];

  for (const [label, value] of rows) {
    const tr = el("tr");
    const th = el("th", "sandbox-status-th", label);
    const td = el("td", "sandbox-status-td");

    if (label === "Image") {
      // Add running badge next to image
      td.append(document.createTextNode(value));
      const runningBadge = status.running
        ? badge("running", "success")
        : badge("stopped", "danger");
      runningBadge.setAttribute("data-testid", "running-badge");
      td.append(document.createTextNode(" "), runningBadge);
    } else {
      td.textContent = value;
    }

    tr.append(th, td);
    tbody.append(tr);
  }

  table.append(tbody);
  card.append(table);

  return card;
}

/**
 * Active workspace banner.
 */
function buildWorkspaceBanner() {
  const status = sandboxState.status;
  if (!status || status.provider !== "docker") {
    return null;
  }

  const banner = el("div");
  if (status.running) {
    banner.className = "sandbox-banner sandbox-banner--active";
    banner.setAttribute("data-testid", "workspace-banner-active");
    banner.textContent = `Active workspace: ${status.workspace_path || "/workspace"} (inside container)`;
  } else {
    banner.className = "sandbox-banner sandbox-banner--warning";
    banner.setAttribute("data-testid", "workspace-banner-stopped");
    banner.textContent = "Container stopped — workspace not accessible";
  }
  return banner;
}

/**
 * Actions card: Create, Stop, Reset buttons.
 */
function buildActionsCard() {
  const card = el("article", "sandbox-card");
  card.append(el("h3", "", "Container Actions"));

  if (sandboxState.actionError) {
    const err = el("p", "settings-inline-error", `Action failed: ${sandboxState.actionError}`);
    err.setAttribute("data-testid", "action-error");
    card.append(err);
  }
  if (sandboxState.actionSuccess) {
    const ok = el("p", "settings-save-success");
    ok.setAttribute("data-testid", "action-success");
    ok.textContent = sandboxState.actionSuccess;
    card.append(ok);
  }

  const actions = el("div", "sandbox-actions");
  const busy = sandboxState.actionPending;

  // Create button
  const createBtn = el("button", "chat-send-button", "");
  createBtn.type = "button";
  createBtn.setAttribute("data-testid", "create-btn");
  createBtn.disabled = busy;
  if (busy) {
    createBtn.append(spinner(), document.createTextNode(" Creating..."));
  } else {
    createBtn.textContent = "Create Container";
  }
  createBtn.addEventListener("click", () => void doCreate());

  // Stop button
  const stopBtn = el("button", "layout-toggle", "");
  stopBtn.type = "button";
  stopBtn.setAttribute("data-testid", "stop-btn");
  stopBtn.disabled = busy;
  if (busy) {
    stopBtn.append(spinner(), document.createTextNode(" Stopping..."));
  } else {
    stopBtn.textContent = "Stop Container";
  }
  stopBtn.addEventListener("click", () => void doStop());

  // Reset button
  const resetBtn = el("button", "layout-toggle sandbox-btn--danger", "");
  resetBtn.type = "button";
  resetBtn.setAttribute("data-testid", "reset-btn");
  resetBtn.disabled = busy;
  if (busy) {
    resetBtn.append(spinner(), document.createTextNode(" Resetting..."));
  } else {
    resetBtn.textContent = "Reset Container";
  }
  resetBtn.addEventListener("click", () => void doReset());

  const resetNote = el(
    "p",
    "muted",
    "⚠ Reset destroys all files in the container volume and recreates it from scratch."
  );

  actions.append(createBtn, stopBtn, resetBtn);
  card.append(actions, resetNote);

  return card;
}

/**
 * Pull Image card.
 */
function buildPullCard() {
  const card = el("article", "sandbox-card");
  card.append(el("h3", "", "Pull Image"));

  const row = el("div", "sandbox-pull-row");

  const input = el("input");
  input.type = "text";
  input.className = "settings-input";
  input.placeholder = "ubuntu:24.04";
  input.value = sandboxState.pullImage;
  input.setAttribute("data-testid", "pull-image-input");
  input.addEventListener("input", () => {
    sandboxState.pullImage = input.value;
  });

  const pullBtn = el("button", "chat-send-button", "");
  pullBtn.type = "button";
  pullBtn.setAttribute("data-testid", "pull-btn");
  pullBtn.disabled = sandboxState.pullPending;
  if (sandboxState.pullPending) {
    pullBtn.append(spinner(), document.createTextNode(" Pulling..."));
  } else {
    pullBtn.textContent = "Pull";
  }
  pullBtn.addEventListener("click", () => void doPull());

  row.append(input, pullBtn);
  card.append(row);

  if (sandboxState.pullError) {
    const err = el("p", "settings-inline-error", `Pull failed: ${sandboxState.pullError}`);
    err.setAttribute("data-testid", "pull-error");
    card.append(err);
  }
  if (sandboxState.pullSuccess) {
    const ok = el("p", "settings-save-success");
    ok.setAttribute("data-testid", "pull-success");
    ok.textContent = sandboxState.pullSuccess;
    card.append(ok);
  }

  return card;
}

/**
 * Images list card.
 */
function buildImagesCard() {
  const card = el("article", "sandbox-card");
  const titleRow = el("div", "sandbox-card-title-row");
  titleRow.append(el("h3", "", "Available Images"));

  const refreshBtn = el("button", "layout-toggle", "Refresh");
  refreshBtn.type = "button";
  refreshBtn.disabled = sandboxState.imagesLoading;
  refreshBtn.addEventListener("click", () => void loadImages());
  titleRow.append(refreshBtn);
  card.append(titleRow);

  if (sandboxState.imagesLoading) {
    card.append(el("p", "muted", "Loading images..."));
    return card;
  }

  if (sandboxState.imagesError) {
    card.append(el("p", "settings-inline-error", `Error: ${sandboxState.imagesError}`));
    return card;
  }

  const images = sandboxState.images;
  if (!images) {
    card.append(el("p", "muted", "Click Refresh to load images."));
    return card;
  }

  if (images.length === 0) {
    card.append(el("p", "muted", "No images found."));
    return card;
  }

  const table = el("table", "sandbox-table");
  const thead = el("thead");
  const headRow = el("tr");
  for (const col of ["Repository:Tag", "ID", "Size (MB)"]) {
    headRow.append(el("th", "", col));
  }
  thead.append(headRow);

  const tbody = el("tbody");
  for (const img of images) {
    const tr = el("tr");
    tr.setAttribute("data-testid", "image-row");
    const repoTag = `${img.repo || ""}:${img.tag || ""}`;
    const shortId = String(img.id || "").replace(/^sha256:/, "").slice(0, 12);
    const sizeMb = img.size_mb != null ? String(img.size_mb) : "—";
    tr.append(el("td", "", repoTag), el("td", "sandbox-mono", shortId), el("td", "", sizeMb));
    tbody.append(tr);
  }

  table.append(thead, tbody);
  card.append(table);
  return card;
}

/**
 * Volumes list card.
 */
function buildVolumesCard() {
  const card = el("article", "sandbox-card");
  const titleRow = el("div", "sandbox-card-title-row");
  titleRow.append(el("h3", "", "Docker Volumes"));

  const refreshBtn = el("button", "layout-toggle", "Refresh");
  refreshBtn.type = "button";
  refreshBtn.disabled = sandboxState.volumesLoading;
  refreshBtn.addEventListener("click", () => void loadVolumes());
  titleRow.append(refreshBtn);
  card.append(titleRow);

  if (sandboxState.volumesLoading) {
    card.append(el("p", "muted", "Loading volumes..."));
    return card;
  }

  if (sandboxState.volumesError) {
    card.append(el("p", "settings-inline-error", `Error: ${sandboxState.volumesError}`));
    return card;
  }

  const volumes = sandboxState.volumes;
  if (!volumes) {
    card.append(el("p", "muted", "Click Refresh to load volumes."));
    return card;
  }

  if (volumes.length === 0) {
    card.append(el("p", "muted", "No volumes found."));
    return card;
  }

  const table = el("table", "sandbox-table");
  const thead = el("thead");
  const headRow = el("tr");
  for (const col of ["Name", "Driver", "Mountpoint", "Actions"]) {
    headRow.append(el("th", "", col));
  }
  thead.append(headRow);

  const tbody = el("tbody");
  for (const vol of volumes) {
    const tr = el("tr");
    tr.setAttribute("data-testid", "volume-row");
    const name = String(vol.name || "");
    const driver = String(vol.driver || "");
    const mount = String(vol.mountpoint || "");

    const deleteBtn = el("button", "layout-toggle sandbox-btn--danger", "Delete");
    deleteBtn.type = "button";
    deleteBtn.setAttribute("data-testid", "delete-volume-btn");
    deleteBtn.disabled = sandboxState.actionPending;
    deleteBtn.addEventListener("click", () => void doDeleteVolume(name));

    const actionTd = el("td");
    actionTd.append(deleteBtn);

    tr.append(
      el("td", "sandbox-mono", name),
      el("td", "", driver),
      el("td", "sandbox-mono sandbox-small", mount),
      actionTd
    );
    tbody.append(tr);
  }

  table.append(thead, tbody);
  card.append(table);
  return card;
}

/**
 * Advanced mount configuration section (collapsed, display-only).
 */
function buildAdvancedSection() {
  const details = el("details", "sandbox-advanced");
  details.open = sandboxState.advancedOpen;

  const summary = el("summary", "sandbox-advanced-summary", "Advanced: Mount Configuration");
  details.append(summary);

  details.addEventListener("toggle", () => {
    sandboxState.advancedOpen = details.open;
  });

  const warning = el(
    "p",
    "sandbox-notice sandbox-notice--warning",
    "⚠ Enabling custom mounts may expose host filesystem paths. Only configure if you understand the security implications."
  );
  details.append(warning);

  const label = el("label", "settings-field");
  const labelTitle = el("span", "settings-field-title", "Mount specs (display only)");
  const helpText = el("p", "settings-help muted", "One mount per line, format: /host/path:/container/path[:ro]");
  const textarea = el("textarea", "settings-textarea settings-raw-editor");
  textarea.rows = 5;
  textarea.placeholder = "/host/path:/container/path:ro";
  textarea.value = sandboxState.mountConfig;
  textarea.setAttribute("data-testid", "mount-config-textarea");
  // Display-only: update state but no save action
  textarea.addEventListener("input", () => {
    sandboxState.mountConfig = textarea.value;
  });

  const note = el(
    "p",
    "muted",
    "Mount configuration is display-only in this release. Apply via server config."
  );

  label.append(labelTitle, helpText, textarea);
  details.append(label, note);

  return details;
}

// ─── Main render ──────────────────────────────────────────────────────────────

function renderSandboxPage() {
  const container = sandboxState.container;
  container.innerHTML = "";

  container.append(buildHeader());

  const banner = buildWorkspaceBanner();
  if (banner) container.append(banner);

  const statusCard = buildStatusCard();
  statusCard.setAttribute("data-testid", "status-card");
  container.append(statusCard);

  container.append(buildActionsCard());
  container.append(buildPullCard());
  container.append(buildImagesCard());
  container.append(buildVolumesCard());
  container.append(buildAdvancedSection());
}

// ─── Page export ──────────────────────────────────────────────────────────────

export const sandboxPage = {
  key: "sandbox",
  title: "Sandbox",

  async render({ container, apiClient }) {
    const firstLoad = sandboxState.container !== container;
    sandboxState.container = container;

    if (!sandboxState.sandboxApi) {
      sandboxState.sandboxApi = createSandboxApi(apiClient);
    }

    try {
      renderSandboxPage();
    } catch (err) {
      container.innerHTML = `<pre style="color:red;padding:1rem">Sandbox render error: ${err && err.message ? err.message : String(err)}\n${err && err.stack ? err.stack : ""}</pre>`;
      return;
    }

    if (firstLoad || !sandboxState.status) {
      // Load all data in parallel on first render
      await Promise.allSettled([
        loadStatus(),
        loadImages(),
        loadVolumes(),
      ]);
    }
  },
};
