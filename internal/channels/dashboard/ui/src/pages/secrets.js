import { captureFocusSnapshot, restoreFocusSnapshot } from "../ui/focus_restore.js";

const CONVENTIONS = [
  {
    key: "PERPLEXITY_API_KEY",
    note: "Perplexity provider key when model/provider config points to this env name.",
  },
  {
    key: "OPENAI_API_KEY",
    note: "OpenAI-compatible providers often default to this env key.",
  },
  {
    key: "OPENROUTER_API_KEY",
    note: "Use for OpenRouter when providers.openrouter.api_key_env matches.",
  },
  {
    key: "REQUESTY_API_KEY",
    note: "Use for Requesty integrations when configured as api_key_env.",
  },
  {
    key: "ZAI_API_KEY",
    note: "Use when model.provider is zai and api_key_env expects this key.",
  },
  {
    key: "DISCORD_BOT_TOKEN",
    note: "Env-style Discord token key; keep token private and rotate regularly.",
  },
  {
    key: "discord/bot_token",
    note: "Legacy slash-path naming pattern also supported by existing admin flows.",
  },
  {
    key: "TELEGRAM_BOT_TOKEN",
    note: "Env-style Telegram bot token key; keep token private and rotate regularly.",
  },
  {
    key: "telegram/bot_token",
    note: "Slash-path Telegram bot token key used by the encrypted secret store.",
  },
];

const secretsState = {
  container: null,
  apiClient: null,
  loading: false,
  loadError: "",
  keys: [],
  filter: "",
  formName: "",
  formValue: "",
  formError: "",
  formSuccess: "",
  saving: false,
  copyFeedback: "",
};

function normalizeKeys(payload) {
  if (!payload || !Array.isArray(payload.keys)) {
    return [];
  }
  return payload.keys
    .filter((item) => typeof item === "string")
    .map((item) => item.trim())
    .filter((item) => item.length > 0)
    .sort((a, b) => a.localeCompare(b));
}

function matchesFilter(value, query) {
  const needle = String(query || "").trim().toLowerCase();
  if (!needle) {
    return true;
  }
  return String(value || "").toLowerCase().includes(needle);
}

async function copyText(value) {
  if (!value) {
    return;
  }
  if (navigator.clipboard && typeof navigator.clipboard.writeText === "function") {
    await navigator.clipboard.writeText(value);
    return;
  }
  const input = document.createElement("textarea");
  input.value = value;
  input.setAttribute("readonly", "readonly");
  input.style.position = "absolute";
  input.style.left = "-9999px";
  document.body.append(input);
  input.select();
  document.execCommand("copy");
  document.body.removeChild(input);
}

function rerender(options = {}) {
  if (!secretsState.container || !secretsState.container.isConnected) {
    return;
  }
  const focusSnapshot = options.preserveFocus ? captureFocusSnapshot(secretsState.container) : null;
  renderSecretsPage();
  if (focusSnapshot) {
    restoreFocusSnapshot(secretsState.container, focusSnapshot);
  }
}

async function loadKeys() {
  secretsState.loading = true;
  secretsState.loadError = "";
  rerender();
  try {
    const payload = await secretsState.apiClient.get("/api/admin/secrets");
    secretsState.keys = normalizeKeys(payload);
  } catch (error) {
    secretsState.loadError = error instanceof Error ? error.message : String(error);
  } finally {
    secretsState.loading = false;
    rerender();
  }
}

async function submitSecret() {
  const name = String(secretsState.formName || "").trim();
  const value = secretsState.formValue;
  secretsState.formError = "";
  secretsState.formSuccess = "";

  if (!name || !value) {
    secretsState.formError = "Name and value are required.";
    rerender();
    return;
  }

  secretsState.saving = true;
  rerender();
  try {
    await secretsState.apiClient.post("/api/admin/secrets", { name, value });
    secretsState.formSuccess = `Stored key: ${name}`;
    secretsState.formValue = "";
    await loadKeys();
  } catch (error) {
    secretsState.formError = error instanceof Error ? error.message : String(error);
  } finally {
    secretsState.saving = false;
    rerender();
  }
}

function createConventionsPanel() {
  const panel = document.createElement("section");
  panel.className = "secrets-conventions";

  const title = document.createElement("h3");
  title.textContent = "Naming conventions";
  panel.append(title);

  const note = document.createElement("p");
  note.className = "muted";
  note.textContent = "Use exact key names referenced by config (for example providers.*.api_key_env or discord.token_env). Values remain write-only in this UI.";
  panel.append(note);

  const list = document.createElement("ul");
  list.className = "secrets-conventions-list";
  CONVENTIONS.forEach((item) => {
    const row = document.createElement("li");

    const code = document.createElement("code");
    code.textContent = item.key;

    const text = document.createElement("span");
    text.textContent = item.note;

    const useButton = document.createElement("button");
    useButton.type = "button";
    useButton.className = "layout-toggle";
    useButton.textContent = "Use key";
    useButton.addEventListener("click", () => {
      secretsState.formName = item.key;
      secretsState.formError = "";
      secretsState.formSuccess = "";
      rerender();
    });

    row.append(code, text, useButton);
    list.append(row);
  });

  panel.append(list);
  return panel;
}

function createKeysPanel() {
  const panel = document.createElement("section");
  panel.className = "secrets-keys";

  const titleRow = document.createElement("div");
  titleRow.className = "secrets-panel-title";
  const title = document.createElement("h3");
  title.textContent = "Stored keys";
  const count = document.createElement("p");
  count.className = "muted";
  count.textContent = `${secretsState.keys.length} total`;
  titleRow.append(title, count);
  panel.append(titleRow);

  const filterField = document.createElement("label");
  filterField.className = "secrets-search";
  const filterLabel = document.createElement("span");
  filterLabel.textContent = "Search key names";
  const filterInput = document.createElement("input");
  filterInput.type = "search";
  filterInput.className = "settings-input";
  filterInput.setAttribute("data-focus-id", "secrets:filter");
  filterInput.placeholder = "PERPLEXITY_API_KEY";
  filterInput.value = secretsState.filter;
  filterInput.addEventListener("input", () => {
    secretsState.filter = filterInput.value;
    rerender({ preserveFocus: true });
  });
  filterField.append(filterLabel, filterInput);
  panel.append(filterField);

  if (secretsState.loading) {
    const loading = document.createElement("p");
    loading.className = "muted";
    loading.textContent = "Loading keys...";
    panel.append(loading);
    return panel;
  }

  if (secretsState.loadError) {
    const error = document.createElement("p");
    error.className = "settings-inline-error";
    error.textContent = `Failed to load keys: ${secretsState.loadError}`;
    panel.append(error);

    const retry = document.createElement("button");
    retry.type = "button";
    retry.className = "layout-toggle";
    retry.textContent = "Retry";
    retry.addEventListener("click", () => {
      void loadKeys();
    });
    panel.append(retry);
    return panel;
  }

  const filteredKeys = secretsState.keys.filter((key) => matchesFilter(key, secretsState.filter));

  const list = document.createElement("ul");
  list.className = "secrets-keys-list";
  filteredKeys.forEach((key) => {
    const item = document.createElement("li");

    const code = document.createElement("code");
    code.textContent = key;

    const copyButton = document.createElement("button");
    copyButton.type = "button";
    copyButton.className = "layout-toggle";
    copyButton.textContent = "Copy name";
    copyButton.addEventListener("click", async () => {
      try {
        await copyText(key);
        secretsState.copyFeedback = `Copied: ${key}`;
      } catch (error) {
        secretsState.copyFeedback = `Copy failed: ${error instanceof Error ? error.message : String(error)}`;
      }
      rerender();
    });

    item.append(code, copyButton);
    list.append(item);
  });

  if (!filteredKeys.length) {
    const empty = document.createElement("p");
    empty.className = "muted";
    empty.textContent = secretsState.keys.length
      ? "No keys match this search."
      : "No keys stored yet. Add a key using the form.";
    panel.append(empty);
  } else {
    panel.append(list);
  }

  if (secretsState.copyFeedback) {
    const feedback = document.createElement("p");
    feedback.className = "muted";
    feedback.textContent = secretsState.copyFeedback;
    panel.append(feedback);
  }

  return panel;
}

function createSetSecretPanel() {
  const panel = document.createElement("section");
  panel.className = "secrets-set";

  const title = document.createElement("h3");
  title.textContent = "Set or rotate secret";
  panel.append(title);

  const note = document.createElement("p");
  note.className = "muted";
  note.textContent = "Values are write-only. Existing secret values are never shown here.";
  panel.append(note);

  const form = document.createElement("form");
  form.className = "secrets-form";
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    void submitSecret();
  });

  const nameField = document.createElement("label");
  nameField.className = "secrets-form-field";
  const nameLabel = document.createElement("span");
  nameLabel.textContent = "Secret key name";
  const nameInput = document.createElement("input");
  nameInput.type = "text";
  nameInput.className = "settings-input";
  nameInput.placeholder = "PERPLEXITY_API_KEY";
  nameInput.value = secretsState.formName;
  nameInput.addEventListener("input", () => {
    secretsState.formName = nameInput.value;
    secretsState.formError = "";
    secretsState.formSuccess = "";
  });
  nameField.append(nameLabel, nameInput);

  const valueField = document.createElement("label");
  valueField.className = "secrets-form-field";
  const valueLabel = document.createElement("span");
  valueLabel.textContent = "Secret value";
  const valueInput = document.createElement("input");
  valueInput.type = "password";
  valueInput.autocomplete = "new-password";
  valueInput.className = "settings-input";
  valueInput.placeholder = "Enter new secret value";
  valueInput.value = secretsState.formValue;
  valueInput.addEventListener("input", () => {
    secretsState.formValue = valueInput.value;
    secretsState.formError = "";
    secretsState.formSuccess = "";
  });
  valueField.append(valueLabel, valueInput);

  const actions = document.createElement("div");
  actions.className = "secrets-form-actions";
  const submit = document.createElement("button");
  submit.type = "submit";
  submit.className = "chat-send-button";
  submit.disabled = secretsState.saving;
  submit.textContent = secretsState.saving ? "Saving..." : "Store Secret";
  actions.append(submit);

  form.append(nameField, valueField, actions);
  panel.append(form);

  if (secretsState.formError) {
    const error = document.createElement("p");
    error.className = "settings-inline-error";
    error.textContent = secretsState.formError;
    panel.append(error);
  }

  if (secretsState.formSuccess) {
    const success = document.createElement("p");
    success.className = "settings-save-success";
    success.textContent = secretsState.formSuccess;
    panel.append(success);
  }

  return panel;
}

function renderSecretsPage() {
  const container = secretsState.container;
  container.innerHTML = "";

  const heading = document.createElement("h2");
  heading.textContent = "Secrets";
  container.append(heading);

  const subtitle = document.createElement("p");
  subtitle.className = "muted";
  subtitle.textContent = "Manage secret key names and rotate values without exposing stored secret contents.";
  container.append(subtitle);

  const layout = document.createElement("section");
  layout.className = "secrets-layout";
  layout.append(createKeysPanel(), createSetSecretPanel(), createConventionsPanel());
  container.append(layout);
}

export const secretsPage = {
  key: "secrets",
  title: "Secrets",
  async render({ container, apiClient }) {
    const firstLoad = secretsState.container !== container;
    secretsState.container = container;
    secretsState.apiClient = apiClient;
    renderSecretsPage();
    if (firstLoad || !secretsState.keys.length) {
      await loadKeys();
    }
  },
};
