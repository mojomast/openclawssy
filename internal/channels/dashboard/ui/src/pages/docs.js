const DOC_ORDER = ["SOUL.md", "RULES.md", "TOOLS.md", "SPECPLAN.md", "DEVPLAN.md", "HANDOFF.md", "HEARTBEAT.md"];

const docsState = {
  container: null,
  apiClient: null,
  loading: false,
  saving: false,
  agentID: "default",
  availableAgents: [],
  documents: [],
  selectedDocName: "",
  baselineByName: {},
  draftByName: {},
  statusText: "",
  statusKind: "",
};

function rerender() {
  if (!docsState.container || !docsState.container.isConnected) {
    return;
  }
  renderDocsPage();
}

function setStatus(text, kind = "") {
  docsState.statusText = String(text || "");
  docsState.statusKind = kind;
}

function asText(value) {
  if (value === null || value === undefined) {
    return "";
  }
  return String(value);
}

function normalizeDocuments(input) {
  const list = Array.isArray(input) ? input : [];
  const normalized = list
    .filter((item) => item && typeof item === "object")
    .map((item) => {
      const name = asText(item.name).trim();
      const resolvedName = asText(item.resolved_name).trim() || name;
      return {
        name,
        resolvedName,
        aliasFor: asText(item.alias_for).trim(),
        content: asText(item.content),
        exists: Boolean(item.exists),
      };
    })
    .filter((item) => item.name.length > 0);

  const rank = DOC_ORDER.reduce((acc, item, index) => {
    acc[item] = index;
    return acc;
  }, {});

  normalized.sort((left, right) => {
    const leftRank = Number.isInteger(rank[left.name]) ? rank[left.name] : 100;
    const rightRank = Number.isInteger(rank[right.name]) ? rank[right.name] : 100;
    if (leftRank !== rightRank) {
      return leftRank - rightRank;
    }
    return left.name.localeCompare(right.name);
  });

  return normalized;
}

export async function fetchDocsSummary(apiClient, agentID = "default") {
	const payload = await apiClient.get(`/api/admin/agent/docs?agent_id=${encodeURIComponent(asText(agentID).trim() || "default")}`);
	const docs = normalizeDocuments(payload?.documents);
	return {
		agent_id: asText(payload?.agent_id).trim() || "default",
		documents: docs,
		existing_count: docs.filter((item) => item.exists).length,
	};
}

export function renderCompactDocsSummary(container, summary) {
	container.innerHTML = "";
	const docs = Array.isArray(summary?.documents) ? summary.documents : [];
	const list = document.createElement("div");
	list.className = "widget-list";
	if (!docs.length) {
		const empty = document.createElement("p");
		empty.className = "muted";
		empty.textContent = "No agent docs found.";
		container.append(empty);
		return;
	}
	list.append(Object.assign(document.createElement("div"), { className: "widget-list-item static", textContent: `Agent: ${summary?.agent_id || "default"}` }));
	list.append(Object.assign(document.createElement("div"), { className: "widget-list-item static", textContent: `Docs present: ${summary?.existing_count || 0}/${docs.length}` }));
	const row = document.createElement("div");
	row.className = "widget-list-item static";
	row.textContent = `Top docs: ${docs.slice(0, 3).map((doc) => doc.name).join(", ")}`;
	list.append(row);
	container.append(list);
}

function getSelectedDocument() {
  return docsState.documents.find((item) => item.name === docsState.selectedDocName) || null;
}

function hasUnsavedChanges(docName) {
  return asText(docsState.draftByName[docName]) !== asText(docsState.baselineByName[docName]);
}

async function loadDocs(options = {}) {
  const { keepStatus = false } = options;
  docsState.loading = true;
  if (!keepStatus) {
    setStatus("Loading docs...", "");
  }
  rerender();

  try {
    const requestedAgent = asText(docsState.agentID).trim() || "default";
    const payload = await docsState.apiClient.get(`/api/admin/agent/docs?agent_id=${encodeURIComponent(requestedAgent)}`);

    docsState.availableAgents = Array.isArray(payload?.available_agents)
      ? payload.available_agents.map((item) => asText(item).trim()).filter((item) => item.length > 0)
      : [];
    docsState.agentID = asText(payload?.agent_id).trim() || requestedAgent;

    const docs = normalizeDocuments(payload?.documents);
    docsState.documents = docs;
    docsState.baselineByName = {};
    docsState.draftByName = {};
    docs.forEach((doc) => {
      docsState.baselineByName[doc.name] = doc.content;
      docsState.draftByName[doc.name] = doc.content;
    });

    const selectedExists = docs.some((doc) => doc.name === docsState.selectedDocName);
    if (!selectedExists) {
      docsState.selectedDocName = docs.length ? docs[0].name : "";
    }

    setStatus("Docs loaded.", "success");
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    setStatus(`Failed to load docs: ${message}`, "error");
  } finally {
    docsState.loading = false;
    rerender();
  }
}

async function saveSelectedDoc() {
  const selected = getSelectedDocument();
  if (!selected) {
    setStatus("Select a document before saving.", "error");
    rerender();
    return;
  }

  docsState.saving = true;
  setStatus(`Saving ${selected.name}...`, "");
  rerender();

  try {
    const content = asText(docsState.draftByName[selected.name]);
    await docsState.apiClient.post("/api/admin/agent/docs", {
      agent_id: docsState.agentID,
      name: selected.name,
      content,
    });
    docsState.baselineByName[selected.name] = content;
    setStatus(`Saved ${selected.name} for ${docsState.agentID}.`, "success");
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    setStatus(`Failed to save ${selected.name}: ${message}`, "error");
  } finally {
    docsState.saving = false;
    rerender();
  }
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
  agentSelect.disabled = docsState.loading || docsState.saving;

  const agentOptions = docsState.availableAgents.length ? docsState.availableAgents : [docsState.agentID || "default"];
  agentOptions.forEach((agentID) => {
    const option = document.createElement("option");
    option.value = agentID;
    option.textContent = agentID;
    option.selected = agentID === docsState.agentID;
    agentSelect.append(option);
  });

  agentSelect.addEventListener("change", () => {
    docsState.agentID = agentSelect.value;
    void loadDocs();
  });
  agentField.append(agentLabel, agentSelect);

  const docField = document.createElement("label");
  docField.className = "docs-field";
  const docLabel = document.createElement("span");
  docLabel.textContent = "Document";
  const docSelect = document.createElement("select");
  docSelect.className = "docs-select";
  docSelect.disabled = docsState.loading || docsState.saving || !docsState.documents.length;

  docsState.documents.forEach((doc) => {
    const option = document.createElement("option");
    const unsaved = hasUnsavedChanges(doc.name) ? " *" : "";
    option.value = doc.name;
    option.textContent = `${doc.name}${unsaved}`;
    option.selected = doc.name === docsState.selectedDocName;
    docSelect.append(option);
  });

  docSelect.addEventListener("change", () => {
    docsState.selectedDocName = docSelect.value;
    setStatus("", "");
    rerender();
  });
  docField.append(docLabel, docSelect);

  const actions = document.createElement("div");
  actions.className = "docs-actions";

  const reloadButton = document.createElement("button");
  reloadButton.type = "button";
  reloadButton.className = "layout-toggle";
  reloadButton.disabled = docsState.loading || docsState.saving;
  reloadButton.textContent = docsState.loading ? "Reloading..." : "Reload docs";
  reloadButton.addEventListener("click", () => {
    void loadDocs();
  });

  const saveButton = document.createElement("button");
  saveButton.type = "button";
  saveButton.className = "chat-send-button";
  const selected = getSelectedDocument();
  const canSave = selected && hasUnsavedChanges(selected.name);
  saveButton.disabled = docsState.loading || docsState.saving || !selected || !canSave;
  saveButton.textContent = docsState.saving ? "Saving..." : "Save selected doc";
  saveButton.addEventListener("click", () => {
    void saveSelectedDoc();
  });

  actions.append(reloadButton, saveButton);
  toolbar.append(agentField, docField, actions);
  return toolbar;
}

function createStatus() {
  if (!docsState.statusText) {
    return null;
  }
  const line = document.createElement("p");
  line.className = `docs-status ${docsState.statusKind}`.trim();
  line.textContent = docsState.statusText;
  return line;
}

function createEditor() {
  const section = document.createElement("section");
  section.className = "docs-editor";

  const selected = getSelectedDocument();
  if (!selected) {
    const empty = document.createElement("p");
    empty.className = "muted";
    empty.textContent = docsState.loading
      ? "Loading documents..."
      : "No documents available for this agent.";
    section.append(empty);
    return section;
  }

  const top = document.createElement("div");
  top.className = "docs-editor-head";

  const title = document.createElement("h3");
  title.textContent = selected.name;

  const meta = document.createElement("p");
  meta.className = "muted";
  const unsaved = hasUnsavedChanges(selected.name) ? "Unsaved changes" : "No unsaved changes";
  meta.textContent = `${selected.exists ? "File exists" : "File not found yet"} - ${unsaved}`;

  top.append(title, meta);
  section.append(top);

  if (selected.aliasFor) {
    const alias = document.createElement("p");
    alias.className = "docs-alias-note";
    alias.textContent = `${selected.name} is an alias for ${selected.aliasFor} (${selected.resolvedName}).`;
    section.append(alias);
  }

  const editorLabel = document.createElement("label");
  editorLabel.className = "docs-field";

  const labelText = document.createElement("span");
  labelText.textContent = `Content (${selected.resolvedName})`;

  const textarea = document.createElement("textarea");
  textarea.className = "docs-textarea";
  textarea.value = asText(docsState.draftByName[selected.name]);
  textarea.disabled = docsState.loading || docsState.saving;
  textarea.addEventListener("input", () => {
    docsState.draftByName[selected.name] = textarea.value;
    if (!docsState.statusKind || docsState.statusKind === "success") {
      setStatus("", "");
    }
  });

  editorLabel.append(labelText, textarea);
  section.append(editorLabel);

  return section;
}

function renderDocsPage() {
  const container = docsState.container;
  container.innerHTML = "";

  const heading = document.createElement("h2");
  heading.textContent = "Docs";
  container.append(heading);

  const subtitle = document.createElement("p");
  subtitle.className = "muted";
  subtitle.textContent = "Edit core agent markdown docs per agent and save updates through admin APIs.";
  container.append(subtitle);

  const page = document.createElement("section");
  page.className = "docs-page";
  page.append(createToolbar());

  const status = createStatus();
  if (status) {
    page.append(status);
  }

  page.append(createEditor());
  container.append(page);
}

export const docsPage = {
  key: "docs",
  title: "Docs",
  async render({ container, apiClient }) {
    const firstLoad = docsState.container !== container;
    docsState.container = container;
    docsState.apiClient = apiClient;

    renderDocsPage();
    if (firstLoad || !docsState.documents.length) {
      await loadDocs();
    }
  },
};
