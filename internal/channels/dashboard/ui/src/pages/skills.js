const skillsState = {
  container: null,
  apiClient: null,
  loading: false,
  actionPending: false,
  statusText: "",
  statusKind: "",
  agentID: "default",
  availableAgents: [],
  installable: [],
  installedSkills: [],
  activatedSkills: [],
};

function asText(value) {
  if (value === null || value === undefined) {
    return "";
  }
  return String(value);
}

function normalizeName(value) {
  return asText(value).trim().toLowerCase();
}

function rerender() {
  if (!skillsState.container || !skillsState.container.isConnected) {
    return;
  }
  renderSkillsPage();
}

function setStatus(text, kind = "") {
  skillsState.statusText = asText(text);
  skillsState.statusKind = asText(kind);
}

async function loadSkills(options = {}) {
  const { keepStatus = false } = options;
  skillsState.loading = true;
  if (!keepStatus) {
    setStatus("Loading skills...", "");
  }
  rerender();

  try {
    const requestedAgent = normalizeName(skillsState.agentID) || "default";
    const payload = await skillsState.apiClient.get(`/api/admin/skills?agent_id=${encodeURIComponent(requestedAgent)}`);
    skillsState.agentID = normalizeName(payload?.agent_id) || requestedAgent;
    skillsState.availableAgents = Array.isArray(payload?.available_agents)
      ? payload.available_agents.map((item) => normalizeName(item)).filter((item) => item)
      : [skillsState.agentID];
    skillsState.installable = Array.isArray(payload?.installable)
      ? payload.installable
          .filter((item) => item && typeof item === "object")
          .map((item) => ({ name: normalizeName(item.name), installed: Boolean(item.installed) }))
          .filter((item) => item.name)
      : [];
    skillsState.installedSkills = Array.isArray(payload?.installed_skills)
      ? payload.installed_skills.map((item) => normalizeName(item)).filter((item) => item)
      : [];
    skillsState.activatedSkills = Array.isArray(payload?.activated_skills)
      ? payload.activated_skills.map((item) => normalizeName(item)).filter((item) => item)
      : [];
    setStatus("Skills loaded.", "success");
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    setStatus(`Failed to load skills: ${message}`, "error");
  } finally {
    skillsState.loading = false;
    rerender();
  }
}

function isInstalled(name) {
  const normalized = normalizeName(name);
  return skillsState.installedSkills.includes(normalized);
}

function isActivated(name) {
  const normalized = normalizeName(name);
  return skillsState.activatedSkills.includes(normalized);
}

async function installSkill(name) {
  const skill = normalizeName(name);
  if (!skill) {
    return;
  }
  skillsState.actionPending = true;
  setStatus(`Installing ${skill}...`, "");
  rerender();
  try {
    await skillsState.apiClient.post("/api/admin/skills", {
      action: "install",
      name: skill,
      agent_id: skillsState.agentID,
    });
    setStatus(`Installed ${skill}.`, "success");
    await loadSkills({ keepStatus: true });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    setStatus(`Failed to install ${skill}: ${message}`, "error");
  } finally {
    skillsState.actionPending = false;
    rerender();
  }
}

async function setActivation(name, enabled) {
  const skill = normalizeName(name);
  if (!skill) {
    return;
  }
  skillsState.actionPending = true;
  setStatus(`${enabled ? "Activating" : "Deactivating"} ${skill} for ${skillsState.agentID}...`, "");
  rerender();
  try {
    await skillsState.apiClient.post("/api/admin/skills", {
      action: enabled ? "activate" : "deactivate",
      name: skill,
      agent_id: skillsState.agentID,
    });
    setStatus(`${enabled ? "Activated" : "Deactivated"} ${skill} for ${skillsState.agentID}.`, "success");
    await loadSkills({ keepStatus: true });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    setStatus(`Failed to update ${skill}: ${message}`, "error");
  } finally {
    skillsState.actionPending = false;
    rerender();
  }
}

function createStatusLine() {
  if (!skillsState.statusText) {
    return null;
  }
  const line = document.createElement("p");
  line.className = `docs-status ${skillsState.statusKind}`.trim();
  line.textContent = skillsState.statusText;
  return line;
}

function createToolbar() {
  const toolbar = document.createElement("section");
  toolbar.className = "docs-toolbar";

  const agentField = document.createElement("label");
  agentField.className = "docs-field";
  const agentLabel = document.createElement("span");
  agentLabel.textContent = "Agent";
  const agentSelect = document.createElement("select");
  agentSelect.className = "docs-select";
  agentSelect.disabled = skillsState.loading || skillsState.actionPending;
  const options = skillsState.availableAgents.length ? skillsState.availableAgents : [skillsState.agentID || "default"];
  options.forEach((agentID) => {
    const option = document.createElement("option");
    option.value = agentID;
    option.textContent = agentID;
    option.selected = agentID === skillsState.agentID;
    agentSelect.append(option);
  });
  agentSelect.addEventListener("change", () => {
    skillsState.agentID = normalizeName(agentSelect.value) || "default";
    void loadSkills();
  });
  agentField.append(agentLabel, agentSelect);

  const actions = document.createElement("div");
  actions.className = "docs-actions";
  const reload = document.createElement("button");
  reload.type = "button";
  reload.className = "layout-toggle";
  reload.disabled = skillsState.loading || skillsState.actionPending;
  reload.textContent = skillsState.loading ? "Reloading..." : "Reload";
  reload.addEventListener("click", () => {
    void loadSkills();
  });
  actions.append(reload);
  toolbar.append(agentField, actions);
  return toolbar;
}

function createInstallableSection() {
  const section = document.createElement("section");
  section.className = "docs-editor";
  const title = document.createElement("h3");
  title.textContent = "Installable Skills";
  section.append(title);

  if (!skillsState.installable.length) {
    const empty = document.createElement("p");
    empty.className = "muted";
    empty.textContent = "No installable skills found.";
    section.append(empty);
    return section;
  }

  const list = document.createElement("div");
  list.className = "skills-list";
  skillsState.installable.forEach((item) => {
    const card = document.createElement("article");
    card.className = "skills-card";
    const left = document.createElement("div");
    const name = document.createElement("p");
    name.className = "settings-field-title";
    name.textContent = item.name;
    const state = document.createElement("p");
    state.className = "muted";
    state.textContent = item.installed ? "Installed in workspace/skills." : "Not installed.";
    left.append(name, state);

    const installButton = document.createElement("button");
    installButton.type = "button";
    installButton.className = item.installed ? "layout-toggle" : "chat-send-button";
    installButton.disabled = skillsState.loading || skillsState.actionPending || item.installed;
    installButton.textContent = item.installed ? "Installed" : "Install";
    installButton.addEventListener("click", () => {
      void installSkill(item.name);
    });

    card.append(left, installButton);
    list.append(card);
  });
  section.append(list);
  return section;
}

function createActivationSection() {
  const section = document.createElement("section");
  section.className = "docs-editor";
  const title = document.createElement("h3");
  title.textContent = "Agent Activation";
  const note = document.createElement("p");
  note.className = "muted";
  note.textContent = "Activation appends an Activated Skills block to TOOLS.md so the selected agent can load skills with skill.read.";
  section.append(title, note);

  if (!skillsState.installedSkills.length) {
    const empty = document.createElement("p");
    empty.className = "muted";
    empty.textContent = "Install a skill first to activate it for an agent.";
    section.append(empty);
    return section;
  }

  const list = document.createElement("div");
  list.className = "skills-list";
  skillsState.installedSkills.forEach((name) => {
    const card = document.createElement("article");
    card.className = "skills-card";
    const left = document.createElement("div");
    const label = document.createElement("p");
    label.className = "settings-field-title";
    label.textContent = name;
    const state = document.createElement("p");
    state.className = "muted";
    state.textContent = isActivated(name) ? `Active for ${skillsState.agentID}.` : `Not active for ${skillsState.agentID}.`;
    left.append(label, state);

    const toggle = document.createElement("button");
    toggle.type = "button";
    toggle.className = isActivated(name) ? "layout-toggle" : "chat-send-button";
    toggle.disabled = skillsState.loading || skillsState.actionPending || !isInstalled(name);
    toggle.textContent = isActivated(name) ? "Deactivate" : "Activate";
    toggle.addEventListener("click", () => {
      void setActivation(name, !isActivated(name));
    });

    card.append(left, toggle);
    list.append(card);
  });
  section.append(list);
  return section;
}

function renderSkillsPage() {
  const container = skillsState.container;
  container.innerHTML = "";

  const heading = document.createElement("h2");
  heading.textContent = "Skills";
  container.append(heading);

  const subtitle = document.createElement("p");
  subtitle.className = "muted";
  subtitle.textContent = "Install built-in skills into workspace/skills and activate them per agent.";
  container.append(subtitle);

  const page = document.createElement("section");
  page.className = "docs-page";
  page.append(createToolbar());
  const status = createStatusLine();
  if (status) {
    page.append(status);
  }
  page.append(createInstallableSection(), createActivationSection());
  container.append(page);
}

export const skillsPage = {
  key: "skills",
  title: "Skills",
  async render({ container, apiClient }) {
    const firstLoad = skillsState.container !== container;
    skillsState.container = container;
    skillsState.apiClient = apiClient;
    renderSkillsPage();
    if (firstLoad || (!skillsState.installable.length && !skillsState.installedSkills.length)) {
      await loadSkills();
    }
  },
};
