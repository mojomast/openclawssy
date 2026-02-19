import { captureFocusSnapshot, restoreFocusSnapshot } from "../ui/focus_restore.js";

const CATEGORY_DEFS = [
  { key: "general", title: "General", summary: "Server, workspace, and output defaults." },
  { key: "model", title: "Model Provider", summary: "Model selection and provider endpoint settings." },
  { key: "chat", title: "Chat/Discord", summary: "Runtime chat and Discord connector controls." },
  { key: "agents", title: "Agents", summary: "Per-agent activation, model overrides, and self-improvement controls." },
  { key: "memory", title: "Memory", summary: "Event persistence, working memory limits, and embeddings." },
  { key: "sandbox", title: "Sandbox/Shell", summary: "Sandbox and shell execution constraints." },
  { key: "network", title: "Network", summary: "Network policy allowlist and localhost behavior." },
  { key: "scheduler", title: "Scheduler", summary: "Job catch-up and concurrency limits." },
  { key: "capabilities", title: "Capabilities", summary: "UI-first capability planning and flags." },
  { key: "advanced", title: "Advanced", summary: "Raw JSON editor and full config diff." },
];

const CATEGORY_LOOKUP = CATEGORY_DEFS.reduce((acc, category) => {
  acc[category.key] = category;
  return acc;
}, {});

const PROVIDERS = ["openai", "openrouter", "requesty", "zai", "generic"];
const THINKING_MODES = ["never", "on_error", "always"];

const settingsState = {
  container: null,
  apiClient: null,
  selectedCategory: "general",
  searchQuery: "",
  baselineConfig: null,
  draftConfig: null,
  loading: false,
  loadError: null,
  savePending: false,
  saveError: null,
  saveSuccess: "",
  saveAttempted: false,
  touchedFields: new Set(),
  advancedRaw: "",
  advancedRawError: "",
  selectedAgentProfile: "",
  lastAppliedRouteHint: "",
};

function cloneJSON(value) {
  return JSON.parse(JSON.stringify(value));
}

function cleanConfigPayload(payload) {
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
    return {};
  }
  const clean = cloneJSON(payload);
  delete clean.ok;
  delete clean.status;
  delete clean.error;
  delete clean.raw;
  return clean;
}

function asString(value) {
  if (value === null || value === undefined) {
    return "";
  }
  return String(value);
}

function asTrimmedString(value) {
  return asString(value).trim();
}

function toLineList(value) {
  if (!Array.isArray(value)) {
    return "";
  }
  return value.join("\n");
}

function parseLineList(value) {
  return asString(value)
    .split(/\n|,/) 
    .map((item) => item.trim())
    .filter((item) => item.length > 0);
}

function parseSettingsRouteHint() {
  const hash = asString(window.location.hash).replace(/^#/, "");
  const [rawPath, rawQuery = ""] = hash.split("?");
  const path = asTrimmedString(rawPath);
  if (path !== "/settings" && path !== "settings") {
    return null;
  }
  const query = asTrimmedString(rawQuery);
  if (!query) {
    return null;
  }
  return { signature: `${path}?${query}`, params: new URLSearchParams(query) };
}

function applySettingsRouteHint() {
  const hint = parseSettingsRouteHint();
  if (!hint) {
    return;
  }
  if (hint.signature === settingsState.lastAppliedRouteHint) {
    return;
  }

  const category = asTrimmedString(hint.params.get("category"));
  if (category && CATEGORY_LOOKUP[category]) {
    settingsState.selectedCategory = category;
  }

  const profile = asTrimmedString(hint.params.get("profile"));
  if (profile) {
    settingsState.selectedAgentProfile = profile;
  }

  settingsState.lastAppliedRouteHint = hint.signature;
}

function normalizeConfigShape(input) {
  const cfg = cloneJSON(input || {});

  if (!cfg.network || typeof cfg.network !== "object") {
    cfg.network = {};
  }
  if (!Array.isArray(cfg.network.allowed_domains)) {
    cfg.network.allowed_domains = [];
  }

  if (!cfg.shell || typeof cfg.shell !== "object") {
    cfg.shell = {};
  }
  if (!Array.isArray(cfg.shell.allowed_commands)) {
    cfg.shell.allowed_commands = [];
  }

  if (!cfg.sandbox || typeof cfg.sandbox !== "object") {
    cfg.sandbox = {};
  }
  if (!cfg.sandbox.provider) {
    cfg.sandbox.provider = "none";
  }

  if (!cfg.server || typeof cfg.server !== "object") {
    cfg.server = {};
  }
  if (!cfg.workspace || typeof cfg.workspace !== "object") {
    cfg.workspace = {};
  }
  if (!cfg.engine || typeof cfg.engine !== "object") {
    cfg.engine = {};
  }
  if (!cfg.scheduler || typeof cfg.scheduler !== "object") {
    cfg.scheduler = {};
  }
  if (!cfg.output || typeof cfg.output !== "object") {
    cfg.output = {};
  }
  if (!cfg.model || typeof cfg.model !== "object") {
    cfg.model = {};
  }
  if (!cfg.chat || typeof cfg.chat !== "object") {
    cfg.chat = {};
  }
  if (!Array.isArray(cfg.chat.allow_users)) {
    cfg.chat.allow_users = [];
  }
  if (!Array.isArray(cfg.chat.allow_rooms)) {
    cfg.chat.allow_rooms = [];
  }

  if (!cfg.discord || typeof cfg.discord !== "object") {
    cfg.discord = {};
  }
  if (!Array.isArray(cfg.discord.allow_guilds)) {
    cfg.discord.allow_guilds = [];
  }
  if (!Array.isArray(cfg.discord.allow_channels)) {
    cfg.discord.allow_channels = [];
  }
  if (!Array.isArray(cfg.discord.allow_users)) {
    cfg.discord.allow_users = [];
  }

  if (!cfg.providers || typeof cfg.providers !== "object") {
    cfg.providers = {};
  }
  PROVIDERS.forEach((provider) => {
    if (!cfg.providers[provider] || typeof cfg.providers[provider] !== "object") {
      cfg.providers[provider] = {};
    }
  });

  if (!cfg.secrets || typeof cfg.secrets !== "object") {
    cfg.secrets = {};
  }

  if (!cfg.memory || typeof cfg.memory !== "object") {
    cfg.memory = {};
  }

  if (!cfg.agents || typeof cfg.agents !== "object") {
    cfg.agents = {};
  }
  if (!Array.isArray(cfg.agents.enabled_agent_ids)) {
    cfg.agents.enabled_agent_ids = [];
  }
  if (!cfg.agents.profiles || typeof cfg.agents.profiles !== "object" || Array.isArray(cfg.agents.profiles)) {
    cfg.agents.profiles = {};
  }
  if (typeof cfg.agents.allow_inter_agent_messaging !== "boolean") {
    cfg.agents.allow_inter_agent_messaging = true;
  }
  if (typeof cfg.agents.allow_agent_model_overrides !== "boolean") {
    cfg.agents.allow_agent_model_overrides = true;
  }
  if (typeof cfg.agents.self_improvement_enabled !== "boolean") {
    cfg.agents.self_improvement_enabled = false;
  }

  return cfg;
}

function isPlainObject(value) {
  return value && typeof value === "object" && !Array.isArray(value);
}

function flattenLeaves(value, pathPrefix = "", acc = {}) {
  if (isPlainObject(value)) {
    const keys = Object.keys(value).sort();
    if (!keys.length && pathPrefix) {
      acc[pathPrefix] = {};
      return acc;
    }
    keys.forEach((key) => {
      const nextPath = pathPrefix ? `${pathPrefix}.${key}` : key;
      flattenLeaves(value[key], nextPath, acc);
    });
    return acc;
  }
  const path = pathPrefix || "(root)";
  acc[path] = value;
  return acc;
}

function deepEqual(a, b) {
  return JSON.stringify(a) === JSON.stringify(b);
}

function compactValue(value, limit = 160) {
  let text = "";
  try {
    text = JSON.stringify(value);
  } catch (_error) {
    text = String(value);
  }
  if (text.length <= limit) {
    return text;
  }
  return `${text.slice(0, Math.max(0, limit - 3))}...`;
}

function computeDiffRows(baseline, draft) {
  if (!baseline || !draft) {
    return [];
  }
  const left = flattenLeaves(baseline);
  const right = flattenLeaves(draft);
  const keys = Array.from(new Set([...Object.keys(left), ...Object.keys(right)])).sort();
  return keys
    .filter((path) => !deepEqual(left[path], right[path]))
    .map((path) => ({
      path,
      before: left[path],
      after: right[path],
      beforePreview: compactValue(left[path]),
      afterPreview: compactValue(right[path]),
    }));
}

function getByPath(root, path) {
  const parts = path.split(".");
  let current = root;
  for (const part of parts) {
    if (!current || typeof current !== "object") {
      return undefined;
    }
    current = current[part];
  }
  return current;
}

function setByPath(root, path, value, options = {}) {
  const { deleteIfUndefined = false } = options;
  const parts = path.split(".");
  let current = root;
  for (let index = 0; index < parts.length - 1; index += 1) {
    const part = parts[index];
    if (!current[part] || typeof current[part] !== "object" || Array.isArray(current[part])) {
      current[part] = {};
    }
    current = current[part];
  }
  const leaf = parts[parts.length - 1];
  if (deleteIfUndefined && value === undefined) {
    delete current[leaf];
    return;
  }
  current[leaf] = value;
}

function fieldVisible(query, ...parts) {
  const needle = asTrimmedString(query).toLowerCase();
  if (!needle) {
    return true;
  }
  const haystack = parts
    .filter((part) => !!part)
    .map((part) => String(part).toLowerCase())
    .join(" ");
  return haystack.includes(needle);
}

function validateDraftConfig(draft) {
  const fieldErrors = {};
  const formErrors = [];

  const setFieldError = (path, message) => {
    if (!fieldErrors[path]) {
      fieldErrors[path] = message;
    }
  };

  const provider = asTrimmedString(draft?.model?.provider).toLowerCase();
  const modelName = asTrimmedString(draft?.model?.name);
  if (!provider) {
    setFieldError("model.provider", "Provider is required.");
  } else if (!PROVIDERS.includes(provider)) {
    setFieldError("model.provider", "Provider must be one of openai, openrouter, requesty, zai, generic.");
  }
  if (!modelName) {
    setFieldError("model.name", "Model name is required.");
  }

  const temperature = draft?.model?.temperature;
  if (temperature !== undefined && temperature !== null && Number.isNaN(Number(temperature))) {
    setFieldError("model.temperature", "Temperature must be numeric.");
  }

  const maxTokens = Number(draft?.model?.max_tokens);
  if (draft?.model?.max_tokens !== undefined && (!Number.isInteger(maxTokens) || maxTokens < 1 || maxTokens > 20000)) {
    setFieldError("model.max_tokens", "Max tokens must be an integer between 1 and 20000.");
  }

  const sandboxProvider = asTrimmedString(draft?.sandbox?.provider).toLowerCase();
  const validProviders = ["none", "local", "docker"];
  if (!sandboxProvider || !validProviders.includes(sandboxProvider)) {
    setFieldError("sandbox.provider", "Sandbox provider must be none, local, or docker.");
  }
  if (draft?.sandbox?.active && sandboxProvider === "none") {
    setFieldError("sandbox.provider", "Sandbox provider must be local or docker when sandbox is active.");
  }
  if (draft?.shell?.enable_exec && !draft?.sandbox?.active) {
    setFieldError("shell.enable_exec", "Shell execution requires sandbox.active=true.");
  }

  const thinkingMode = asTrimmedString(draft?.output?.thinking_mode).toLowerCase();
  if (!thinkingMode) {
    setFieldError("output.thinking_mode", "Thinking mode is required.");
  } else if (!THINKING_MODES.includes(thinkingMode)) {
    setFieldError("output.thinking_mode", "Thinking mode must be one of never, on_error, always.");
  }

  const maxThinkingChars = Number(draft?.output?.max_thinking_chars);
  if (
    draft?.output?.max_thinking_chars !== undefined &&
    (!Number.isInteger(maxThinkingChars) || maxThinkingChars < 64 || maxThinkingChars > 100000)
  ) {
    setFieldError("output.max_thinking_chars", "Max thinking chars must be an integer between 64 and 100000.");
  }

  const engineMaxRuns = Number(draft?.engine?.max_concurrent_runs);
  if (
    draft?.engine?.max_concurrent_runs !== undefined &&
    (!Number.isInteger(engineMaxRuns) || engineMaxRuns < 1 || engineMaxRuns > 10000)
  ) {
    setFieldError("engine.max_concurrent_runs", "Max concurrent runs must be an integer between 1 and 10000.");
  }

  const schedulerMaxJobs = Number(draft?.scheduler?.max_concurrent_jobs);
  if (
    draft?.scheduler?.max_concurrent_jobs !== undefined &&
    (!Number.isInteger(schedulerMaxJobs) || schedulerMaxJobs < 1 || schedulerMaxJobs > 1000)
  ) {
    setFieldError("scheduler.max_concurrent_jobs", "Max concurrent jobs must be an integer between 1 and 1000.");
  }

  const serverPort = Number(draft?.server?.port);
  if (draft?.server?.port !== undefined && (!Number.isInteger(serverPort) || serverPort < 1 || serverPort > 65535)) {
    setFieldError("server.port", "Server port must be an integer between 1 and 65535.");
  }
  if (!asTrimmedString(draft?.server?.bind_address)) {
    setFieldError("server.bind_address", "Server bind address is required.");
  }
  if (!asTrimmedString(draft?.workspace?.root)) {
    setFieldError("workspace.root", "Workspace root is required.");
  }

  const chatRate = Number(draft?.chat?.rate_limit_per_min);
  if (draft?.chat?.rate_limit_per_min !== undefined && (!Number.isInteger(chatRate) || chatRate < 1)) {
    setFieldError("chat.rate_limit_per_min", "Chat rate limit must be an integer >= 1.");
  }

  const chatGlobalRate = Number(draft?.chat?.global_rate_limit_per_min);
  if (
    draft?.chat?.global_rate_limit_per_min !== undefined &&
    (!Number.isInteger(chatGlobalRate) || chatGlobalRate < 1)
  ) {
    setFieldError("chat.global_rate_limit_per_min", "Global chat rate limit must be an integer >= 1.");
  }

  const discordRate = Number(draft?.discord?.rate_limit_per_min);
  if (draft?.discord?.rate_limit_per_min !== undefined && (!Number.isInteger(discordRate) || discordRate < 1)) {
    setFieldError("discord.rate_limit_per_min", "Discord rate limit must be an integer >= 1.");
  }

  if (provider === "generic" && !asTrimmedString(draft?.providers?.generic?.base_url)) {
    setFieldError("providers.generic.base_url", "Generic provider base URL is required when model.provider is generic.");
  }

  const allowedCommands = Array.isArray(draft?.shell?.allowed_commands) ? draft.shell.allowed_commands : [];
  if (allowedCommands.some((item) => !asTrimmedString(item))) {
    setFieldError("shell.allowed_commands", "Allowed commands cannot contain empty entries.");
  }

  const allowedDomains = Array.isArray(draft?.network?.allowed_domains) ? draft.network.allowed_domains : [];
  if (allowedDomains.some((item) => !asTrimmedString(item))) {
    setFieldError("network.allowed_domains", "Allowed domains cannot contain empty entries.");
  }

  const enabledAgentIDs = Array.isArray(draft?.agents?.enabled_agent_ids) ? draft.agents.enabled_agent_ids : [];
  if (enabledAgentIDs.some((item) => !asTrimmedString(item))) {
    setFieldError("agents.enabled_agent_ids", "Enabled agent IDs cannot contain empty entries.");
  }
  const profiles = draft?.agents?.profiles && typeof draft.agents.profiles === "object" ? draft.agents.profiles : {};
  Object.entries(profiles).forEach(([agentID, profile]) => {
    if (!asTrimmedString(agentID)) {
      setFieldError("agents.profiles", "Profile keys must be non-empty agent IDs.");
      return;
    }
    const provider = asTrimmedString(profile?.model?.provider).toLowerCase();
    if (provider && !PROVIDERS.includes(provider)) {
      setFieldError(`agents.profiles.${agentID}.model.provider`, "Profile model provider must match a supported provider.");
    }
    const maxTokensProfile = profile?.model?.max_tokens;
    if (
      maxTokensProfile !== undefined &&
      maxTokensProfile !== null &&
      (!Number.isInteger(Number(maxTokensProfile)) || Number(maxTokensProfile) < 0 || Number(maxTokensProfile) > 20000)
    ) {
      setFieldError(`agents.profiles.${agentID}.model.max_tokens`, "Profile max tokens must be an integer between 0 and 20000.");
    }
  });

  const maxWorkingItems = Number(draft?.memory?.max_working_items);
  if (
    draft?.memory?.max_working_items !== undefined &&
    (!Number.isInteger(maxWorkingItems) || maxWorkingItems < 1 || maxWorkingItems > 100000)
  ) {
    setFieldError("memory.max_working_items", "Max working items must be an integer between 1 and 100000.");
  }

  const maxPromptTokens = Number(draft?.memory?.max_prompt_tokens);
  if (
    draft?.memory?.max_prompt_tokens !== undefined &&
    (!Number.isInteger(maxPromptTokens) || maxPromptTokens < 64 || maxPromptTokens > 100000)
  ) {
    setFieldError("memory.max_prompt_tokens", "Max prompt tokens must be an integer between 64 and 100000.");
  }

  const eventBufferSize = Number(draft?.memory?.event_buffer_size);
  if (
    draft?.memory?.event_buffer_size !== undefined &&
    (!Number.isInteger(eventBufferSize) || eventBufferSize < 1 || eventBufferSize > 10000)
  ) {
    setFieldError("memory.event_buffer_size", "Event buffer size must be an integer between 1 and 10000.");
  }

  const embeddingProvider = asTrimmedString(draft?.memory?.embedding_provider).toLowerCase();
  if (draft?.memory?.embeddings_enabled && embeddingProvider && !PROVIDERS.includes(embeddingProvider)) {
    setFieldError("memory.embedding_provider", "Embedding provider must be one of the supported providers.");
  }
  if (draft?.memory?.embeddings_enabled && !asTrimmedString(draft?.memory?.embedding_model)) {
    setFieldError("memory.embedding_model", "Embedding model is required when embeddings are enabled.");
  }

  if (Object.keys(fieldErrors).length > 0) {
    formErrors.push("Fix validation errors before saving.");
  }
  return { fieldErrors, formErrors };
}

function markTouched(path) {
  settingsState.touchedFields.add(path);
}

function shouldShowFieldError(path, fieldErrors) {
  return Boolean(fieldErrors[path]) && (settingsState.saveAttempted || settingsState.touchedFields.has(path));
}

function updateDraft(path, value, options = {}) {
  settingsState.saveSuccess = "";
  settingsState.saveError = null;
  markTouched(path);
  setByPath(settingsState.draftConfig, path, value, options);
  settingsState.draftConfig = normalizeConfigShape(settingsState.draftConfig);
  settingsState.advancedRaw = `${JSON.stringify(settingsState.draftConfig, null, 2)}\n`;
  rerender({ preserveFocus: true });
}

function categoryMatchesSearch(category, query, draft) {
  const q = asTrimmedString(query).toLowerCase();
  if (!q) {
    return true;
  }
  const titleMatch = fieldVisible(q, category.title, category.summary, category.key);
  if (titleMatch) {
    return true;
  }
  const snapshot = JSON.stringify(getCategorySnapshot(category.key, draft) || {}).toLowerCase();
  return snapshot.includes(q);
}

function getCategorySnapshot(categoryKey, draft) {
  switch (categoryKey) {
    case "general":
      return { server: draft.server, workspace: draft.workspace, output: draft.output, engine: draft.engine };
    case "model":
      return { model: draft.model, providers: draft.providers };
    case "chat":
      return { chat: draft.chat, discord: draft.discord };
    case "agents":
      return { agents: draft.agents };
    case "memory":
      return { memory: draft.memory };
    case "sandbox":
      return { sandbox: draft.sandbox, shell: draft.shell };
    case "network":
      return { network: draft.network };
    case "scheduler":
      return { scheduler: draft.scheduler };
    case "capabilities":
      return {
        chat_enabled: draft.chat?.enabled,
        discord_enabled: draft.discord?.enabled,
        network_enabled: draft.network?.enabled,
        sandbox_active: draft.sandbox?.active,
        shell_exec: draft.shell?.enable_exec,
      };
    case "advanced":
      return draft;
    default:
      return {};
  }
}

function createField({ title, path, helpText, errorText }) {
  const field = document.createElement("label");
  field.className = "settings-field";

  const titleEl = document.createElement("span");
  titleEl.className = "settings-field-title";
  titleEl.textContent = title;

  const pathEl = document.createElement("code");
  pathEl.className = "settings-field-path";
  pathEl.textContent = path;

  field.append(titleEl, pathEl);

  if (helpText) {
    const help = document.createElement("p");
    help.className = "settings-help muted";
    help.textContent = helpText;
    field.append(help);
  }

  if (errorText) {
    const error = document.createElement("p");
    error.className = "settings-inline-error";
    error.textContent = errorText;
    field.append(error);
  }

  return field;
}

function appendTextField({ parent, query, title, path, helpText = "", placeholder = "", readOnly = false, inputType = "text", fieldErrors }) {
  if (!fieldVisible(query, title, path, helpText)) {
    return;
  }
  const errorText = shouldShowFieldError(path, fieldErrors) ? fieldErrors[path] : "";
  const field = createField({ title, path, helpText, errorText });
  const input = document.createElement("input");
  input.type = inputType;
  input.className = "settings-input";
  input.setAttribute("data-focus-id", `settings:${path}`);
  input.value = asString(getByPath(settingsState.draftConfig, path));
  input.placeholder = placeholder;
  input.readOnly = readOnly;
  if (!readOnly) {
    input.addEventListener("input", () => {
      updateDraft(path, asTrimmedString(input.value), { deleteIfUndefined: false });
    });
  }
  field.append(input);
  parent.append(field);
}

function appendNumberField({
  parent,
  query,
  title,
  path,
  helpText = "",
  placeholder = "",
  allowEmpty = true,
  step = "1",
  fieldErrors,
}) {
  if (!fieldVisible(query, title, path, helpText)) {
    return;
  }
  const errorText = shouldShowFieldError(path, fieldErrors) ? fieldErrors[path] : "";
  const field = createField({ title, path, helpText, errorText });
  const input = document.createElement("input");
  input.type = "number";
  input.className = "settings-input";
  input.setAttribute("data-focus-id", `settings:${path}`);
  input.step = step;
  const value = getByPath(settingsState.draftConfig, path);
  input.value = value === undefined || value === null ? "" : String(value);
  input.placeholder = placeholder;
  input.addEventListener("input", () => {
    const raw = asTrimmedString(input.value);
    if (raw === "") {
      if (allowEmpty) {
        updateDraft(path, undefined, { deleteIfUndefined: true });
      }
      return;
    }
    const numeric = Number(raw);
    updateDraft(path, Number.isNaN(numeric) ? raw : numeric, { deleteIfUndefined: false });
  });
  field.append(input);
  parent.append(field);
}

function appendSelectField({ parent, query, title, path, options, helpText = "", fieldErrors }) {
  const optionLabels = options.map((option) => option.label || option.value);
  if (!fieldVisible(query, title, path, helpText, optionLabels.join(" "))) {
    return;
  }
  const errorText = shouldShowFieldError(path, fieldErrors) ? fieldErrors[path] : "";
  const field = createField({ title, path, helpText, errorText });
  const select = document.createElement("select");
  select.className = "settings-select";
  select.setAttribute("data-focus-id", `settings:${path}`);
  const current = asTrimmedString(getByPath(settingsState.draftConfig, path)).toLowerCase();
  options.forEach((option) => {
    const entry = document.createElement("option");
    entry.value = option.value;
    entry.textContent = option.label || option.value;
    if (option.value === current) {
      entry.selected = true;
    }
    select.append(entry);
  });
  select.addEventListener("change", () => {
    updateDraft(path, asTrimmedString(select.value).toLowerCase(), { deleteIfUndefined: false });
  });
  field.append(select);
  parent.append(field);
}

function appendCheckboxField({ parent, query, title, path, helpText = "", fieldErrors }) {
  if (!fieldVisible(query, title, path, helpText, "enabled disabled true false")) {
    return;
  }
  const errorText = shouldShowFieldError(path, fieldErrors) ? fieldErrors[path] : "";
  const field = createField({ title, path, helpText, errorText });
  const row = document.createElement("label");
  row.className = "settings-checkbox-row";
  const input = document.createElement("input");
  input.type = "checkbox";
  input.checked = Boolean(getByPath(settingsState.draftConfig, path));
  input.addEventListener("change", () => {
    updateDraft(path, input.checked, { deleteIfUndefined: false });
  });
  const text = document.createElement("span");
  text.textContent = "Enabled";
  row.append(input, text);
  field.append(row);
  parent.append(field);
}

function appendListField({ parent, query, title, path, helpText = "", placeholder = "", fieldErrors }) {
  if (!fieldVisible(query, title, path, helpText, "list comma newline")) {
    return;
  }
  const errorText = shouldShowFieldError(path, fieldErrors) ? fieldErrors[path] : "";
  const field = createField({ title, path, helpText, errorText });
  const input = document.createElement("textarea");
  input.className = "settings-textarea";
  input.setAttribute("data-focus-id", `settings:${path}`);
  input.rows = 4;
  input.placeholder = placeholder;
  input.value = toLineList(getByPath(settingsState.draftConfig, path));
  input.addEventListener("input", () => {
    updateDraft(path, parseLineList(input.value), { deleteIfUndefined: false });
  });
  field.append(input);
  parent.append(field);
}

function buildGeneralCategory(panel, fieldErrors) {
  const query = settingsState.searchQuery;
  appendTextField({
    parent: panel,
    query,
    title: "Server bind address",
    path: "server.bind_address",
    helpText: "IP address used by the HTTP server listener.",
    placeholder: "127.0.0.1",
    fieldErrors,
  });
  appendNumberField({
    parent: panel,
    query,
    title: "Server port",
    path: "server.port",
    helpText: "HTTP server port.",
    placeholder: "8080",
    allowEmpty: false,
    fieldErrors,
  });
  appendTextField({
    parent: panel,
    query,
    title: "Workspace root",
    path: "workspace.root",
    helpText: "Workspace path for runtime file access.",
    placeholder: "./workspace",
    fieldErrors,
  });
  appendSelectField({
    parent: panel,
    query,
    title: "Thinking mode",
    path: "output.thinking_mode",
    helpText: "Controls when assistant thinking content is surfaced.",
    options: THINKING_MODES.map((mode) => ({ value: mode, label: mode })),
    fieldErrors,
  });
  appendNumberField({
    parent: panel,
    query,
    title: "Max thinking chars",
    path: "output.max_thinking_chars",
    helpText: "Maximum thinking content length before truncation.",
    placeholder: "4000",
    fieldErrors,
  });
  appendNumberField({
    parent: panel,
    query,
    title: "Engine max concurrent runs",
    path: "engine.max_concurrent_runs",
    helpText: "Maximum number of simultaneous runs.",
    placeholder: "64",
    fieldErrors,
  });
}

function buildModelCategory(panel, fieldErrors) {
  const query = settingsState.searchQuery;
  appendSelectField({
    parent: panel,
    query,
    title: "Model provider",
    path: "model.provider",
    helpText: "Primary provider used by runtime model calls.",
    options: PROVIDERS.map((provider) => ({ value: provider, label: provider })),
    fieldErrors,
  });
  appendTextField({
    parent: panel,
    query,
    title: "Model name",
    path: "model.name",
    helpText: "Provider model identifier.",
    placeholder: "GLM-4.7",
    fieldErrors,
  });
  appendNumberField({
    parent: panel,
    query,
    title: "Temperature",
    path: "model.temperature",
    helpText: "Sampling temperature.",
    placeholder: "0.2",
    step: "0.1",
    fieldErrors,
  });
  appendNumberField({
    parent: panel,
    query,
    title: "Max tokens",
    path: "model.max_tokens",
    helpText: "Upper bound for output token generation.",
    placeholder: "20000",
    fieldErrors,
  });

  const activeProvider = asTrimmedString(settingsState.draftConfig?.model?.provider).toLowerCase();
  if (!PROVIDERS.includes(activeProvider)) {
    return;
  }

  const activeProviderTitle = document.createElement("h4");
  activeProviderTitle.className = "settings-subheading";
  activeProviderTitle.textContent = `Active Provider Endpoint: ${activeProvider}`;
  panel.append(activeProviderTitle);

  appendTextField({
    parent: panel,
    query,
    title: "Base URL",
    path: `providers.${activeProvider}.base_url`,
    helpText: "Endpoint base URL used for provider HTTP requests.",
    placeholder: "https://...",
    fieldErrors,
  });
  appendTextField({
    parent: panel,
    query,
    title: "API key env",
    path: `providers.${activeProvider}.api_key_env`,
    helpText: "Environment variable containing provider API key.",
    placeholder: "PROVIDER_API_KEY",
    fieldErrors,
  });

  const secretNote = document.createElement("p");
  secretNote.className = "settings-help muted";
  secretNote.textContent = "Secret values are intentionally redacted in this view. Use Secrets page to rotate keys.";
  panel.append(secretNote);
}

function buildChatCategory(panel, fieldErrors) {
  const query = settingsState.searchQuery;
  appendCheckboxField({
    parent: panel,
    query,
    title: "Chat enabled",
    path: "chat.enabled",
    helpText: "Enables chat API endpoints.",
    fieldErrors,
  });
  appendTextField({
    parent: panel,
    query,
    title: "Chat default agent",
    path: "chat.default_agent_id",
    helpText: "Default agent id for chat requests.",
    placeholder: "default",
    fieldErrors,
  });
  appendNumberField({
    parent: panel,
    query,
    title: "Chat rate limit per minute",
    path: "chat.rate_limit_per_min",
    helpText: "Per-user chat request budget.",
    fieldErrors,
  });
  appendNumberField({
    parent: panel,
    query,
    title: "Chat global rate limit per minute",
    path: "chat.global_rate_limit_per_min",
    helpText: "Global chat request budget.",
    fieldErrors,
  });
  appendListField({
    parent: panel,
    query,
    title: "Allowed chat users",
    path: "chat.allow_users",
    helpText: "Optional allowlist of user ids.",
    placeholder: "alice\nbob",
    fieldErrors,
  });
  appendListField({
    parent: panel,
    query,
    title: "Allowed chat rooms",
    path: "chat.allow_rooms",
    helpText: "Optional allowlist of room ids.",
    placeholder: "ops\nengineering",
    fieldErrors,
  });

  const discordHeader = document.createElement("h4");
  discordHeader.className = "settings-subheading";
  discordHeader.textContent = "Discord";
  panel.append(discordHeader);

  appendCheckboxField({
    parent: panel,
    query,
    title: "Discord enabled",
    path: "discord.enabled",
    helpText: "Enables Discord connector.",
    fieldErrors,
  });
  appendTextField({
    parent: panel,
    query,
    title: "Discord default agent",
    path: "discord.default_agent_id",
    helpText: "Agent used by Discord messages.",
    placeholder: "default",
    fieldErrors,
  });
  appendTextField({
    parent: panel,
    query,
    title: "Discord token env",
    path: "discord.token_env",
    helpText: "Environment variable containing Discord token.",
    placeholder: "DISCORD_BOT_TOKEN",
    fieldErrors,
  });
  appendTextField({
    parent: panel,
    query,
    title: "Discord command prefix",
    path: "discord.command_prefix",
    helpText: "Prefix used for Discord bot commands.",
    placeholder: "!ask",
    fieldErrors,
  });
  appendNumberField({
    parent: panel,
    query,
    title: "Discord rate limit per minute",
    path: "discord.rate_limit_per_min",
    helpText: "Per-user Discord command budget.",
    fieldErrors,
  });
  appendListField({
    parent: panel,
    query,
    title: "Allowed Discord guilds",
    path: "discord.allow_guilds",
    helpText: "Optional allowlist of guild ids.",
    fieldErrors,
  });
  appendListField({
    parent: panel,
    query,
    title: "Allowed Discord channels",
    path: "discord.allow_channels",
    helpText: "Optional allowlist of channel ids.",
    fieldErrors,
  });
  appendListField({
    parent: panel,
    query,
    title: "Allowed Discord users",
    path: "discord.allow_users",
    helpText: "Optional allowlist of user ids.",
    fieldErrors,
  });
}

function buildSandboxCategory(panel, fieldErrors) {
  const query = settingsState.searchQuery;
  appendCheckboxField({
    parent: panel,
    query,
    title: "Sandbox active",
    path: "sandbox.active",
    helpText: "Enables sandboxed execution runtime.",
    fieldErrors,
  });
  appendSelectField({
    parent: panel,
    query,
    title: "Sandbox provider",
    path: "sandbox.provider",
    helpText: "Provider implementation for sandbox execution.",
    options: [
      { value: "none", label: "none" },
      { value: "local", label: "local" },
      { value: "docker", label: "docker" },
    ],
    fieldErrors,
  });
  appendCheckboxField({
    parent: panel,
    query,
    title: "Shell exec enabled",
    path: "shell.enable_exec",
    helpText: "Allows shell command execution tools.",
    fieldErrors,
  });
  appendNumberField({
    parent: panel,
    query,
    title: "Shell default timeout ms",
    path: "shell.default_timeout_ms",
    helpText: "Default timeout for shell commands.",
    placeholder: "120000",
    fieldErrors,
  });
  appendNumberField({
    parent: panel,
    query,
    title: "Shell max timeout ms",
    path: "shell.max_timeout_ms",
    helpText: "Maximum timeout for shell commands.",
    placeholder: "300000",
    fieldErrors,
  });
  appendListField({
    parent: panel,
    query,
    title: "Allowed shell commands",
    path: "shell.allowed_commands",
    helpText: "Optional allowlist, one command per line.",
    placeholder: "python3\nnode\ngit",
    fieldErrors,
  });
}

function resolveSelectedProfileKey() {
  const profiles = settingsState.draftConfig?.agents?.profiles || {};
  const keys = Object.keys(profiles).sort();
  if (settingsState.selectedAgentProfile && keys.includes(settingsState.selectedAgentProfile)) {
    return settingsState.selectedAgentProfile;
  }
  const preferred = asTrimmedString(settingsState.draftConfig?.chat?.default_agent_id) || "default";
  if (keys.includes(preferred)) {
    settingsState.selectedAgentProfile = preferred;
    return preferred;
  }
  if (keys.length > 0) {
    settingsState.selectedAgentProfile = keys[0];
    return keys[0];
  }
  settingsState.selectedAgentProfile = preferred;
  const next = cloneJSON(settingsState.draftConfig);
  next.agents = next.agents || {};
  next.agents.profiles = next.agents.profiles || {};
  next.agents.profiles[preferred] = next.agents.profiles[preferred] || {};
  settingsState.draftConfig = normalizeConfigShape(next);
  settingsState.advancedRaw = `${JSON.stringify(settingsState.draftConfig, null, 2)}\n`;
  return preferred;
}

function buildMemoryCategory(panel, fieldErrors) {
  const query = settingsState.searchQuery;
  appendCheckboxField({
    parent: panel,
    query,
    title: "Memory enabled",
    path: "memory.enabled",
    helpText: "Enables memory system persistence and tools.",
    fieldErrors,
  });
  appendNumberField({
    parent: panel,
    query,
    title: "Max working items",
    path: "memory.max_working_items",
    helpText: "Maximum active memory items (recall/context limit).",
    placeholder: "200",
    fieldErrors,
  });
  appendNumberField({
    parent: panel,
    query,
    title: "Max prompt tokens",
    path: "memory.max_prompt_tokens",
    helpText: "Token budget for injected memory context.",
    placeholder: "1200",
    fieldErrors,
  });
  appendCheckboxField({
    parent: panel,
    query,
    title: "Auto checkpoint",
    path: "memory.auto_checkpoint",
    helpText: "Automatically create checkpoints during runs.",
    fieldErrors,
  });
  appendCheckboxField({
    parent: panel,
    query,
    title: "Proactive enabled",
    path: "memory.proactive_enabled",
    helpText: "Allow agents to use proactive memory tools.",
    fieldErrors,
  });
  appendNumberField({
    parent: panel,
    query,
    title: "Event buffer size",
    path: "memory.event_buffer_size",
    helpText: "In-memory buffer size for event stream.",
    placeholder: "256",
    fieldErrors,
  });

  const embeddingsHeader = document.createElement("h4");
  embeddingsHeader.className = "settings-subheading";
  embeddingsHeader.textContent = "Embeddings & Semantic Search";
  panel.append(embeddingsHeader);

  appendCheckboxField({
    parent: panel,
    query,
    title: "Embeddings enabled",
    path: "memory.embeddings_enabled",
    helpText: "Enables vector embeddings for semantic recall.",
    fieldErrors,
  });
  appendSelectField({
    parent: panel,
    query,
    title: "Embedding provider",
    path: "memory.embedding_provider",
    helpText: "Provider for embedding vectors.",
    options: PROVIDERS.map((provider) => ({ value: provider, label: provider })),
    fieldErrors,
  });
  appendTextField({
    parent: panel,
    query,
    title: "Embedding model",
    path: "memory.embedding_model",
    helpText: "Model name for embeddings (e.g. text-embedding-3-small).",
    placeholder: "text-embedding-3-small",
    fieldErrors,
  });
}

function buildAgentsCategory(panel, fieldErrors) {
  const query = settingsState.searchQuery;
  appendCheckboxField({
    parent: panel,
    query,
    title: "Allow inter-agent messaging",
    path: "agents.allow_inter_agent_messaging",
    helpText: "Enables agent.message.send, agent.message.inbox, and agent.run workflows.",
    fieldErrors,
  });
  appendCheckboxField({
    parent: panel,
    query,
    title: "Allow per-agent model overrides",
    path: "agents.allow_agent_model_overrides",
    helpText: "Allows agents.profiles.<id>.model to override global model config.",
    fieldErrors,
  });
  appendCheckboxField({
    parent: panel,
    query,
    title: "Enable self-improvement globally",
    path: "agents.self_improvement_enabled",
    helpText: "Must be true before any agent can modify its own prompt files.",
    fieldErrors,
  });
  appendListField({
    parent: panel,
    query,
    title: "Enabled agent IDs allowlist",
    path: "agents.enabled_agent_ids",
    helpText: "Optional allowlist. When populated, only listed agents can execute runs.",
    placeholder: "default\nplanner\nreviewer",
    fieldErrors,
  });

  const selected = resolveSelectedProfileKey();
  const profiles = settingsState.draftConfig?.agents?.profiles || {};
  const selectedProfile = profiles[selected] || {};

  const header = document.createElement("h4");
  header.className = "settings-subheading";
  header.textContent = "Agent Profile Editor";
  panel.append(header);

  const picker = createField({ title: "Profile agent", path: "agents.profiles", helpText: "Edit per-agent activation and model override values." });
  const select = document.createElement("select");
  select.className = "settings-select";
  Object.keys(profiles)
    .sort()
    .forEach((agentID) => {
      const option = document.createElement("option");
      option.value = agentID;
      option.textContent = agentID;
      option.selected = agentID === selected;
      select.append(option);
    });
  select.addEventListener("change", () => {
    settingsState.selectedAgentProfile = asTrimmedString(select.value);
    rerender();
  });
  picker.append(select);

  const addRow = document.createElement("div");
  addRow.className = "settings-advanced-actions";
  const addInput = document.createElement("input");
  addInput.className = "settings-input";
  addInput.placeholder = "new-agent-id";
  const addButton = document.createElement("button");
  addButton.type = "button";
  addButton.className = "layout-toggle";
  addButton.textContent = "Add Profile";
  addButton.addEventListener("click", () => {
    const nextID = asTrimmedString(addInput.value);
    if (!nextID) return;
    const next = cloneJSON(settingsState.draftConfig);
    next.agents = next.agents || {};
    next.agents.profiles = next.agents.profiles || {};
    if (!next.agents.profiles[nextID]) {
      next.agents.profiles[nextID] = {};
    }
    settingsState.draftConfig = normalizeConfigShape(next);
    settingsState.selectedAgentProfile = nextID;
    settingsState.advancedRaw = `${JSON.stringify(settingsState.draftConfig, null, 2)}\n`;
    rerender();
  });
  addRow.append(addInput, addButton);
  picker.append(addRow);
  panel.append(picker);

  appendCheckboxField({ parent: panel, query, title: "Profile enabled", path: `agents.profiles.${selected}.enabled`, helpText: "Toggle this agent profile on/off.", fieldErrors });
  appendCheckboxField({ parent: panel, query, title: "Profile self-improvement", path: `agents.profiles.${selected}.self_improvement`, helpText: "Allows this agent to modify its own prompt files when global switch is on.", fieldErrors });
  appendSelectField({
    parent: panel,
    query,
    title: "Profile model provider",
    path: `agents.profiles.${selected}.model.provider`,
    helpText: "Optional override provider for this agent profile.",
    options: [{ value: "", label: "(inherit global)" }, ...PROVIDERS.map((provider) => ({ value: provider, label: provider }))],
    fieldErrors,
  });
  appendTextField({
    parent: panel,
    query,
    title: "Profile model name",
    path: `agents.profiles.${selected}.model.name`,
    helpText: "Optional override model name for this agent profile.",
    placeholder: "(inherit global)",
    fieldErrors,
  });
  appendNumberField({
    parent: panel,
    query,
    title: "Profile model max tokens",
    path: `agents.profiles.${selected}.model.max_tokens`,
    helpText: "Optional override max tokens. Set 0/empty to inherit global.",
    placeholder: "0",
    fieldErrors,
  });

  const clear = document.createElement("button");
  clear.type = "button";
  clear.className = "layout-toggle";
  clear.textContent = "Clear Profile Model Overrides";
  clear.addEventListener("click", () => {
    const next = cloneJSON(settingsState.draftConfig);
    next.agents.profiles[selected] = next.agents.profiles[selected] || {};
    delete next.agents.profiles[selected].model;
    settingsState.draftConfig = normalizeConfigShape(next);
    settingsState.advancedRaw = `${JSON.stringify(settingsState.draftConfig, null, 2)}\n`;
    rerender();
  });
  panel.append(clear);

  const remove = document.createElement("button");
  remove.type = "button";
  remove.className = "layout-toggle";
  remove.textContent = "Remove Profile";
  remove.addEventListener("click", () => {
    const next = cloneJSON(settingsState.draftConfig);
    if (next.agents && next.agents.profiles) {
      delete next.agents.profiles[selected];
    }
    settingsState.draftConfig = normalizeConfigShape(next);
    settingsState.selectedAgentProfile = "";
    settingsState.advancedRaw = `${JSON.stringify(settingsState.draftConfig, null, 2)}\n`;
    rerender();
  });
  panel.append(remove);

  const summary = document.createElement("p");
  summary.className = "muted";
  summary.textContent = `Editing profile: ${selected}. enabled=${String(Boolean(selectedProfile.enabled))}, self_improvement=${String(
    Boolean(selectedProfile.self_improvement)
  )}.`;
  panel.append(summary);
}

function buildNetworkCategory(panel, fieldErrors) {
  const query = settingsState.searchQuery;
  appendCheckboxField({
    parent: panel,
    query,
    title: "Network enabled",
    path: "network.enabled",
    helpText: "Enables network access tools.",
    fieldErrors,
  });
  appendCheckboxField({
    parent: panel,
    query,
    title: "Allow localhost targets",
    path: "network.allow_localhosts",
    helpText: "Permits localhost and loopback network calls.",
    fieldErrors,
  });
  appendListField({
    parent: panel,
    query,
    title: "Allowed domains",
    path: "network.allowed_domains",
    helpText: "Host/domain allowlist for network tools.",
    placeholder: "api.openai.com\nopenrouter.ai",
    fieldErrors,
  });
}

function buildSchedulerCategory(panel, fieldErrors) {
  const query = settingsState.searchQuery;
  appendCheckboxField({
    parent: panel,
    query,
    title: "Scheduler catch-up",
    path: "scheduler.catch_up",
    helpText: "Run missed jobs after downtime.",
    fieldErrors,
  });
  appendNumberField({
    parent: panel,
    query,
    title: "Scheduler max concurrent jobs",
    path: "scheduler.max_concurrent_jobs",
    helpText: "Maximum simultaneous jobs.",
    placeholder: "4",
    fieldErrors,
  });
}

function buildCapabilitiesCategory(panel) {
  const title = document.createElement("h4");
  title.className = "settings-subheading";
  title.textContent = "Capabilities Matrix (UI-first placeholder)";
  panel.append(title);

  const note = document.createElement("p");
  note.className = "muted";
  note.textContent = "This section is UI-first in P3.1. Backend capability APIs can be wired later without changing this layout.";
  panel.append(note);

  const rows = [
    {
      name: "Chat API",
      status: settingsState.draftConfig?.chat?.enabled ? "enabled" : "disabled",
      detail: "Controlled by chat.enabled",
    },
    {
      name: "Discord Connector",
      status: settingsState.draftConfig?.discord?.enabled ? "enabled" : "disabled",
      detail: "Controlled by discord.enabled",
    },
    {
      name: "Network Tools",
      status: settingsState.draftConfig?.network?.enabled ? "enabled" : "disabled",
      detail: "Controlled by network.enabled",
    },
    {
      name: "Sandbox Runtime",
      status: settingsState.draftConfig?.sandbox?.active ? "enabled" : "disabled",
      detail: "Controlled by sandbox.active",
    },
    {
      name: "Shell Execution",
      status: settingsState.draftConfig?.shell?.enable_exec ? "enabled" : "disabled",
      detail: "Controlled by shell.enable_exec",
    },
  ];

  const list = document.createElement("div");
  list.className = "settings-capabilities-list";
  rows.forEach((row) => {
    if (!fieldVisible(settingsState.searchQuery, row.name, row.status, row.detail)) {
      return;
    }
    const card = document.createElement("article");
    card.className = `settings-capability-card ${row.status}`;
    const head = document.createElement("p");
    head.className = "settings-capability-title";
    head.textContent = `${row.name} - ${row.status}`;
    const detail = document.createElement("p");
    detail.className = "muted";
    detail.textContent = row.detail;
    card.append(head, detail);
    list.append(card);
  });

  if (!list.childElementCount) {
    const empty = document.createElement("p");
    empty.className = "muted";
    empty.textContent = "No capability cards match this search.";
    panel.append(empty);
    return;
  }

  panel.append(list);
}

function buildAdvancedCategory(panel, diffRows) {
  const note = document.createElement("p");
  note.className = "muted";
  note.textContent = "Edit JSON directly for full control. Validate and apply to draft before saving.";
  panel.append(note);

  const rawField = document.createElement("label");
  rawField.className = "settings-field";
  const rawTitle = document.createElement("span");
  rawTitle.className = "settings-field-title";
  rawTitle.textContent = "Raw config JSON";
  rawField.append(rawTitle);

  const rawInput = document.createElement("textarea");
  rawInput.className = "settings-raw-editor";
  rawInput.setAttribute("data-focus-id", "settings:advanced.raw");
  rawInput.rows = 20;
  rawInput.value = settingsState.advancedRaw;
  rawInput.addEventListener("input", () => {
    settingsState.advancedRaw = rawInput.value;
    settingsState.advancedRawError = "";
  });
  rawField.append(rawInput);

  const rawActions = document.createElement("div");
  rawActions.className = "settings-advanced-actions";

  const formatButton = document.createElement("button");
  formatButton.type = "button";
  formatButton.className = "layout-toggle";
  formatButton.textContent = "Format JSON";
  formatButton.addEventListener("click", () => {
    try {
      const parsed = JSON.parse(settingsState.advancedRaw);
      settingsState.advancedRaw = `${JSON.stringify(parsed, null, 2)}\n`;
      settingsState.advancedRawError = "";
      rerender();
    } catch (error) {
      settingsState.advancedRawError = error instanceof Error ? error.message : String(error);
      rerender();
    }
  });

  const resetButton = document.createElement("button");
  resetButton.type = "button";
  resetButton.className = "layout-toggle";
  resetButton.textContent = "Reset Editor";
  resetButton.addEventListener("click", () => {
    settingsState.advancedRaw = `${JSON.stringify(settingsState.draftConfig, null, 2)}\n`;
    settingsState.advancedRawError = "";
    rerender();
  });

  const applyButton = document.createElement("button");
  applyButton.type = "button";
  applyButton.className = "layout-toggle";
  applyButton.textContent = "Apply JSON to Draft";
  applyButton.addEventListener("click", () => {
    try {
      const parsed = normalizeConfigShape(cleanConfigPayload(JSON.parse(settingsState.advancedRaw)));
      settingsState.draftConfig = parsed;
      settingsState.advancedRaw = `${JSON.stringify(settingsState.draftConfig, null, 2)}\n`;
      settingsState.advancedRawError = "";
      settingsState.saveSuccess = "";
      settingsState.saveError = null;
      rerender();
    } catch (error) {
      settingsState.advancedRawError = error instanceof Error ? error.message : String(error);
      rerender();
    }
  });

  rawActions.append(formatButton, resetButton, applyButton);
  rawField.append(rawActions);

  if (settingsState.advancedRawError) {
    const rawError = document.createElement("p");
    rawError.className = "settings-inline-error";
    rawError.textContent = `JSON parse error: ${settingsState.advancedRawError}`;
    rawField.append(rawError);
  }
  panel.append(rawField);

  const diffTitle = document.createElement("h4");
  diffTitle.className = "settings-subheading";
  diffTitle.textContent = `Diff before save (${diffRows.length} changed path${diffRows.length === 1 ? "" : "s"})`;
  panel.append(diffTitle);

  panel.append(buildDiffSummary(diffRows));

  const rawBaseline = document.createElement("details");
  rawBaseline.className = "settings-json-block";
  const rawBaselineSummary = document.createElement("summary");
  rawBaselineSummary.textContent = "Baseline JSON";
  const rawBaselineBody = document.createElement("pre");
  rawBaselineBody.textContent = JSON.stringify(settingsState.baselineConfig, null, 2);
  rawBaseline.append(rawBaselineSummary, rawBaselineBody);

  const rawDraft = document.createElement("details");
  rawDraft.className = "settings-json-block";
  const rawDraftSummary = document.createElement("summary");
  rawDraftSummary.textContent = "Edited Draft JSON";
  const rawDraftBody = document.createElement("pre");
  rawDraftBody.textContent = JSON.stringify(settingsState.draftConfig, null, 2);
  rawDraft.append(rawDraftSummary, rawDraftBody);

  panel.append(rawBaseline, rawDraft);
}

function buildDiffSummary(diffRows) {
  const wrapper = document.createElement("section");
  wrapper.className = "settings-diff-summary";

  if (!diffRows.length) {
    const clean = document.createElement("p");
    clean.className = "muted";
    clean.textContent = "No draft changes relative to loaded config.";
    wrapper.append(clean);
    return wrapper;
  }

  const table = document.createElement("table");
  table.className = "settings-diff-table";
  const head = document.createElement("thead");
  const headRow = document.createElement("tr");
  ["Path", "Baseline", "Draft"].forEach((label) => {
    const th = document.createElement("th");
    th.textContent = label;
    headRow.append(th);
  });
  head.append(headRow);

  const body = document.createElement("tbody");
  diffRows.forEach((row) => {
    const tr = document.createElement("tr");

    const pathCell = document.createElement("td");
    const code = document.createElement("code");
    code.textContent = row.path;
    pathCell.append(code);

    const beforeCell = document.createElement("td");
    beforeCell.textContent = row.beforePreview;

    const afterCell = document.createElement("td");
    afterCell.textContent = row.afterPreview;

    tr.append(pathCell, beforeCell, afterCell);
    body.append(tr);
  });

  table.append(head, body);
  wrapper.append(table);
  return wrapper;
}

function renderCategoryPanel(categoryKey, fieldErrors, diffRows) {
  const panel = document.createElement("section");
  panel.className = "settings-panel";

  switch (categoryKey) {
    case "general":
      buildGeneralCategory(panel, fieldErrors);
      break;
    case "model":
      buildModelCategory(panel, fieldErrors);
      break;
    case "chat":
      buildChatCategory(panel, fieldErrors);
      break;
    case "agents":
      buildAgentsCategory(panel, fieldErrors);
      break;
    case "memory":
      buildMemoryCategory(panel, fieldErrors);
      break;
    case "sandbox":
      buildSandboxCategory(panel, fieldErrors);
      break;
    case "network":
      buildNetworkCategory(panel, fieldErrors);
      break;
    case "scheduler":
      buildSchedulerCategory(panel, fieldErrors);
      break;
    case "capabilities":
      buildCapabilitiesCategory(panel);
      break;
    case "advanced":
      buildAdvancedCategory(panel, diffRows);
      break;
    default:
      break;
  }

  if (!panel.childElementCount) {
    const empty = document.createElement("p");
    empty.className = "muted";
    empty.textContent = "No fields match this search in the selected category.";
    panel.append(empty);
  }

  return panel;
}

function rerender(options = {}) {
  if (!settingsState.container || !settingsState.container.isConnected) {
    return;
  }
  const focusSnapshot = options.preserveFocus ? captureFocusSnapshot(settingsState.container) : null;
  renderSettingsPage();
  if (focusSnapshot) {
    restoreFocusSnapshot(settingsState.container, focusSnapshot);
  }
}

async function loadConfig() {
  settingsState.loading = true;
  settingsState.loadError = null;
  settingsState.saveError = null;
  settingsState.saveSuccess = "";
  rerender();

  try {
    const payload = await settingsState.apiClient.get("/api/admin/config");
    const config = normalizeConfigShape(cleanConfigPayload(payload));
    settingsState.baselineConfig = cloneJSON(config);
    settingsState.draftConfig = cloneJSON(config);
    settingsState.advancedRaw = `${JSON.stringify(settingsState.draftConfig, null, 2)}\n`;
    settingsState.advancedRawError = "";
    settingsState.touchedFields = new Set();
    settingsState.saveAttempted = false;
  } catch (error) {
    settingsState.loadError = error instanceof Error ? error.message : String(error);
  } finally {
    settingsState.loading = false;
    rerender();
  }
}

async function saveConfig() {
  if (!settingsState.draftConfig) {
    return;
  }
  settingsState.saveAttempted = true;
  const validation = validateDraftConfig(settingsState.draftConfig);
  if (validation.formErrors.length > 0) {
    rerender();
    return;
  }

  settingsState.savePending = true;
  settingsState.saveError = null;
  settingsState.saveSuccess = "";
  rerender();

  try {
    await settingsState.apiClient.post("/api/admin/config", settingsState.draftConfig);
    settingsState.saveSuccess = "Config saved successfully.";
    await loadConfig();
  } catch (error) {
    settingsState.saveError = {
      message: error?.message || String(error),
      status: Number(error?.status) || 0,
      code: error?.code || "save.failed",
      details: error?.details || null,
    };
  } finally {
    settingsState.savePending = false;
    rerender();
  }
}

function renderSettingsPage() {
  applySettingsRouteHint();

  const container = settingsState.container;
  container.innerHTML = "";

  const heading = document.createElement("h2");
  heading.textContent = "Settings";
  container.append(heading);

  const subtitle = document.createElement("p");
  subtitle.className = "muted";
  subtitle.textContent = "Manage configuration by category with inline validation and a pre-save diff against loaded config.";
  container.append(subtitle);

  if (settingsState.loading && !settingsState.draftConfig) {
    const loading = document.createElement("p");
    loading.className = "muted";
    loading.textContent = "Loading config...";
    container.append(loading);
    return;
  }

  if (settingsState.loadError && !settingsState.draftConfig) {
    const error = document.createElement("p");
    error.className = "settings-inline-error";
    error.textContent = `Failed to load config: ${settingsState.loadError}`;
    container.append(error);

    const retry = document.createElement("button");
    retry.type = "button";
    retry.className = "layout-toggle";
    retry.textContent = "Retry";
    retry.addEventListener("click", () => {
      void loadConfig();
    });
    container.append(retry);
    return;
  }

  const draft = settingsState.draftConfig;
  const baseline = settingsState.baselineConfig;
  const validation = validateDraftConfig(draft);
  const selectedCategory = CATEGORY_LOOKUP[settingsState.selectedCategory] || CATEGORY_DEFS[0];
  const diffRows = computeDiffRows(baseline, draft);

  const toolbar = document.createElement("section");
  toolbar.className = "settings-toolbar";

  const breadcrumbs = document.createElement("p");
  breadcrumbs.className = "settings-breadcrumbs";
  breadcrumbs.textContent = `Settings > ${selectedCategory.title}`;

  const searchField = document.createElement("label");
  searchField.className = "settings-search";
  const searchLabel = document.createElement("span");
  searchLabel.className = "settings-search-label";
  searchLabel.textContent = "Search settings";
  const searchInput = document.createElement("input");
  searchInput.type = "search";
  searchInput.className = "settings-input";
  searchInput.setAttribute("data-focus-id", "settings:search");
  searchInput.placeholder = "Search categories, fields, or values";
  searchInput.value = settingsState.searchQuery;
  searchInput.addEventListener("input", () => {
    settingsState.searchQuery = searchInput.value;
    rerender({ preserveFocus: true });
  });
  searchField.append(searchLabel, searchInput);

  const actions = document.createElement("div");
  actions.className = "settings-toolbar-actions";

  const reloadButton = document.createElement("button");
  reloadButton.type = "button";
  reloadButton.className = "layout-toggle";
  reloadButton.textContent = "Reload";
  reloadButton.disabled = settingsState.loading || settingsState.savePending;
  reloadButton.addEventListener("click", () => {
    void loadConfig();
  });

  const resetDraftButton = document.createElement("button");
  resetDraftButton.type = "button";
  resetDraftButton.className = "layout-toggle";
  resetDraftButton.textContent = "Reset Draft";
  resetDraftButton.disabled = settingsState.savePending || !diffRows.length;
  resetDraftButton.addEventListener("click", () => {
    settingsState.draftConfig = cloneJSON(settingsState.baselineConfig);
    settingsState.advancedRaw = `${JSON.stringify(settingsState.draftConfig, null, 2)}\n`;
    settingsState.advancedRawError = "";
    settingsState.saveError = null;
    settingsState.saveSuccess = "";
    settingsState.touchedFields = new Set();
    settingsState.saveAttempted = false;
    rerender();
  });

  const saveButton = document.createElement("button");
  saveButton.type = "button";
  saveButton.className = "chat-send-button";
  saveButton.textContent = settingsState.savePending ? "Saving..." : "Save Config";
  saveButton.disabled = settingsState.savePending;
  saveButton.addEventListener("click", () => {
    void saveConfig();
  });

  actions.append(reloadButton, resetDraftButton, saveButton);
  toolbar.append(breadcrumbs, searchField, actions);
  container.append(toolbar);

  if (settingsState.saveSuccess) {
    const success = document.createElement("p");
    success.className = "settings-save-success";
    success.textContent = settingsState.saveSuccess;
    container.append(success);
  }

  if (settingsState.saveError) {
    const error = document.createElement("section");
    error.className = "settings-save-error";

    const message = document.createElement("p");
    message.className = "settings-inline-error";
    message.textContent = `Save failed (${settingsState.saveError.code}${
      settingsState.saveError.status ? ` / HTTP ${settingsState.saveError.status}` : ""
    }): ${settingsState.saveError.message}`;
    error.append(message);

    if (settingsState.saveError.details) {
      const details = document.createElement("pre");
      details.textContent = JSON.stringify(settingsState.saveError.details, null, 2);
      error.append(details);
    }

    container.append(error);
  }

  if (settingsState.saveAttempted && validation.formErrors.length > 0) {
    const formError = document.createElement("p");
    formError.className = "settings-inline-error";
    formError.textContent = validation.formErrors[0];
    container.append(formError);
  }

  const workspace = document.createElement("section");
  workspace.className = "settings-workspace";

  const categories = document.createElement("aside");
  categories.className = "settings-categories";

  CATEGORY_DEFS.forEach((category) => {
    const isVisible = categoryMatchesSearch(category, settingsState.searchQuery, draft);
    if (!isVisible) {
      return;
    }
    const button = document.createElement("button");
    button.type = "button";
    button.className = "settings-category-button";
    if (category.key === selectedCategory.key) {
      button.classList.add("active");
    }

    const title = document.createElement("strong");
    title.textContent = category.title;
    const summary = document.createElement("span");
    summary.className = "muted";
    summary.textContent = category.summary;

    button.append(title, summary);
    button.addEventListener("click", () => {
      settingsState.selectedCategory = category.key;
      rerender();
    });
    categories.append(button);
  });

  if (!categories.childElementCount) {
    const empty = document.createElement("p");
    empty.className = "muted";
    empty.textContent = "No categories match your search.";
    categories.append(empty);
  }

  const content = document.createElement("div");
  content.className = "settings-category-content";

  const categoryTitle = document.createElement("h3");
  categoryTitle.textContent = selectedCategory.title;
  const categorySummary = document.createElement("p");
  categorySummary.className = "muted";
  categorySummary.textContent = selectedCategory.summary;

  content.append(categoryTitle, categorySummary, renderCategoryPanel(selectedCategory.key, validation.fieldErrors, diffRows));

  if (selectedCategory.key !== "advanced") {
    const diffSection = document.createElement("section");
    diffSection.className = "settings-diff-section";

    const diffHeading = document.createElement("h4");
    diffHeading.className = "settings-subheading";
    diffHeading.textContent = `Diff before save (${diffRows.length} changed path${diffRows.length === 1 ? "" : "s"})`;

    diffSection.append(diffHeading, buildDiffSummary(diffRows));
    content.append(diffSection);
  }

  workspace.append(categories, content);
  container.append(workspace);
}

export const settingsPage = {
  key: "settings",
  title: "Settings",
  async render({ container, apiClient }) {
    settingsState.container = container;
    settingsState.apiClient = apiClient;
    if (!settingsState.baselineConfig || !settingsState.draftConfig) {
      await loadConfig();
      return;
    }
    renderSettingsPage();
  },
};
