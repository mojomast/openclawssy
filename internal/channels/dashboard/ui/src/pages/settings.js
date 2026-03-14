import { captureFocusSnapshot, restoreFocusSnapshot } from "../ui/focus_restore.js";

const CATEGORY_DEFS = [
  { key: "general", title: "General", summary: "Server, workspace, and output defaults." },
  { key: "model", title: "Model Provider", summary: "Model selection and provider endpoint settings." },
  { key: "chat", title: "Chat/Discord/Telegram", summary: "Runtime chat, Discord, and Telegram connector controls." },
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

const MODEL_PROVIDERS = ["openai", "openrouter", "requesty", "hatz", "zai", "generic"];
const EMBEDDING_PROVIDERS = ["openai", "openrouter", "requesty", "zai", "generic"];
const PROVIDERS_WITH_MODEL_DISCOVERY = new Set(["hatz"]);
const THINKING_MODES = ["never", "on_error", "always"];
const DISCORD_SECRET_KEY = "discord/bot_token";

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
  availableAgentIDs: [],
  lastAppliedRouteHint: "",
  discordSecretPresent: false,
  discordSecretLoading: false,
  discordSecretError: "",
  discordTokenDraft: "",
  discordTokenSaving: false,
  discordTokenDeleting: false,
  discordTokenSuccess: "",
  discordTokenError: "",
  validatePending: false,
  providerTestResults: {},
  providerModelsResults: {},
  providerSecretDrafts: {},
  providerSecretSaving: {},
  providerSecretSuccess: {},
  providerSecretError: {},
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

function providerSupportsModelDiscovery(provider) {
  return PROVIDERS_WITH_MODEL_DISCOVERY.has(asTrimmedString(provider).toLowerCase());
}

function providerSecretStoreKey(provider) {
  return `provider/${asTrimmedString(provider).toLowerCase()}/api_key`;
}

function providerSecretEnvName(provider) {
  return asTrimmedString(settingsState.draftConfig?.providers?.[provider]?.api_key_env);
}

function isMissingProviderAPIKeyMessage(message) {
  return asTrimmedString(message).toLowerCase().includes("missing api key");
}

function normalizeDiscoveredModels(models) {
  if (!Array.isArray(models)) {
    return [];
  }
  const seen = new Set();
  const out = [];
  models.forEach((item) => {
    const modelName = asTrimmedString(item);
    if (!modelName) {
      return;
    }
    const key = modelName.toLowerCase();
    if (seen.has(key)) {
      return;
    }
    seen.add(key);
    out.push(modelName);
  });
  return out;
}

function discoveredModelsForProvider(provider) {
  return normalizeDiscoveredModels(settingsState.providerModelsResults?.[provider]?.models);
}

function maybeLoadDiscoveredProviderModels(provider, options = {}) {
  const normalized = asTrimmedString(provider).toLowerCase();
  if (!providerSupportsModelDiscovery(normalized)) {
    return;
  }
  const current = settingsState.providerModelsResults?.[normalized];
  if (!options.force) {
    if (current?.loading) {
      return;
    }
    if (Array.isArray(current?.models) && current.models.length) {
      return;
    }
  }
  void loadProviderModels(normalized);
}

function toLineList(value) {
  if (!Array.isArray(value)) {
    return "";
  }
  return value.join("\n");
}

function parseLineList(value) {
  return asString(value)
    .split(/[\s,;]+/)
    .map((item) => item.trim())
    .filter((item) => item.length > 0);
}

function sortedUniqueAgentIDs(values) {
  const seen = new Set();
  (Array.isArray(values) ? values : []).forEach((value) => {
    const agentID = asTrimmedString(value);
    if (agentID) {
      seen.add(agentID);
    }
  });
  if (!seen.has("default")) {
    seen.add("default");
  }
  return Array.from(seen).sort((left, right) => left.localeCompare(right));
}

function collectKnownAgentIDs() {
  const cfg = settingsState.draftConfig || {};
  return sortedUniqueAgentIDs([
    ...(settingsState.availableAgentIDs || []),
    ...(Array.isArray(cfg?.agents?.enabled_agent_ids) ? cfg.agents.enabled_agent_ids : []),
    ...Object.keys(cfg?.agents?.profiles || {}),
    cfg?.chat?.default_agent_id,
    cfg?.discord?.default_agent_id,
    cfg?.telegram?.default_agent_id,
  ]);
}

async function loadAvailableAgents() {
  const payload = await settingsState.apiClient.get("/api/admin/agents?channel=dashboard&user_id=dashboard_user&room_id=dashboard");
  settingsState.availableAgentIDs = sortedUniqueAgentIDs(payload?.agents);
}

function resetDiscordSecretFeedback() {
  settingsState.discordTokenSuccess = "";
  settingsState.discordTokenError = "";
}

function resetProviderSecretFeedback(provider) {
  const normalized = asTrimmedString(provider).toLowerCase();
  if (!normalized) {
    return;
  }
  settingsState.providerSecretSuccess[normalized] = "";
  settingsState.providerSecretError[normalized] = "";
}

function updateDiscordSecretPresence(payload) {
  const keys = Array.isArray(payload?.keys) ? payload.keys : [];
  settingsState.discordSecretPresent = keys.some((key) => String(key || "").trim() === DISCORD_SECRET_KEY);
}

async function loadDiscordSecretStatus(options = {}) {
  const { rerenderPage = true } = options;
  settingsState.discordSecretLoading = true;
  settingsState.discordSecretError = "";
  if (rerenderPage) {
    rerender();
  }
  try {
    const payload = await settingsState.apiClient.get("/api/admin/secrets");
    updateDiscordSecretPresence(payload);
  } catch (error) {
    settingsState.discordSecretError = error instanceof Error ? error.message : String(error);
  } finally {
    settingsState.discordSecretLoading = false;
    if (rerenderPage) {
      rerender();
    }
  }
}

async function submitDiscordSecret() {
  const value = String(settingsState.discordTokenDraft || "");
  resetDiscordSecretFeedback();
  if (!value) {
    settingsState.discordTokenError = "Discord token is required.";
    rerender();
    return;
  }
  settingsState.discordTokenSaving = true;
  rerender();
  try {
    await settingsState.apiClient.post("/api/admin/secrets", { name: DISCORD_SECRET_KEY, value });
    settingsState.discordTokenDraft = "";
    settingsState.discordTokenSuccess = "Token stored (write-only).";
    await loadDiscordSecretStatus({ rerenderPage: false });
  } catch (error) {
    settingsState.discordTokenError = error instanceof Error ? error.message : String(error);
  } finally {
    settingsState.discordTokenSaving = false;
    rerender();
  }
}

async function deleteDiscordSecret() {
  resetDiscordSecretFeedback();
  if (!window.confirm("Delete the stored Discord token? This cannot be undone.")) {
    return;
  }
  settingsState.discordTokenDeleting = true;
  rerender();
  try {
    await settingsState.apiClient.delete(`/api/admin/secrets/${encodeURIComponent(DISCORD_SECRET_KEY)}`);
    settingsState.discordTokenDraft = "";
    settingsState.discordTokenSuccess = "Stored Discord token deleted.";
    await loadDiscordSecretStatus({ rerenderPage: false });
  } catch (error) {
    settingsState.discordTokenError = error instanceof Error ? error.message : String(error);
  } finally {
    settingsState.discordTokenDeleting = false;
    rerender();
  }
}

async function submitProviderSecret(provider) {
  const normalized = asTrimmedString(provider).toLowerCase();
  if (!normalized) {
    return;
  }
  const value = String(settingsState.providerSecretDrafts[normalized] || "");
  resetProviderSecretFeedback(normalized);
  if (!value.trim()) {
    settingsState.providerSecretError[normalized] = `${normalized} API key is required.`;
    rerender();
    return;
  }
  settingsState.providerSecretSaving[normalized] = true;
  rerender();
  try {
    await settingsState.apiClient.post("/api/admin/secrets", { name: providerSecretStoreKey(normalized), value });
    settingsState.providerSecretDrafts[normalized] = "";
    settingsState.providerSecretSuccess[normalized] = `${normalized} API key stored. Refreshing models...`;
    await loadProviderModels(normalized);
    if (settingsState.providerModelsResults?.[normalized]?.error) {
      settingsState.providerSecretSuccess[normalized] = `${normalized} API key stored.`;
    }
  } catch (error) {
    settingsState.providerSecretError[normalized] = error instanceof Error ? error.message : String(error);
  } finally {
    settingsState.providerSecretSaving[normalized] = false;
    rerender();
  }
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

  if (!cfg.telegram || typeof cfg.telegram !== "object") {
    cfg.telegram = {};
  }
  if (!Array.isArray(cfg.telegram.allow_users)) {
    cfg.telegram.allow_users = [];
  }
  if (!Array.isArray(cfg.telegram.allow_chats)) {
    cfg.telegram.allow_chats = [];
  }

  if (!cfg.providers || typeof cfg.providers !== "object") {
    cfg.providers = {};
  }
  MODEL_PROVIDERS.forEach((provider) => {
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
  } else if (!MODEL_PROVIDERS.includes(provider)) {
    setFieldError("model.provider", "Provider must be one of openai, openrouter, requesty, hatz, zai, generic.");
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
  const modelTimeout = Number(draft?.model?.timeout_ms);
  if (draft?.model?.timeout_ms !== undefined && (!Number.isInteger(modelTimeout) || modelTimeout < 1000 || modelTimeout > 600000)) {
    setFieldError("model.timeout_ms", "Model timeout must be an integer between 1000 and 600000 ms.");
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

  const telegramRate = Number(draft?.telegram?.rate_limit_per_min);
  if (draft?.telegram?.rate_limit_per_min !== undefined && (!Number.isInteger(telegramRate) || telegramRate < 1)) {
    setFieldError("telegram.rate_limit_per_min", "Telegram rate limit must be an integer >= 1.");
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
    if (provider && !MODEL_PROVIDERS.includes(provider)) {
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
    const profileTemp = profile?.model?.temperature;
    if (profileTemp !== undefined && profileTemp !== null && Number.isNaN(Number(profileTemp))) {
      setFieldError(`agents.profiles.${agentID}.model.temperature`, "Profile temperature must be numeric.");
    }
    const profileTimeout = profile?.model?.timeout_ms;
    if (
      profileTimeout !== undefined &&
      profileTimeout !== null &&
      (!Number.isInteger(Number(profileTimeout)) || Number(profileTimeout) < 0 || Number(profileTimeout) > 600000)
    ) {
      setFieldError(`agents.profiles.${agentID}.model.timeout_ms`, "Profile model timeout must be an integer between 0 and 600000 ms.");
    }
  });

  const subDefaults = draft?.agents?.subagent_defaults || {};
  const subDefaultsMaxTools = Number(subDefaults?.max_tool_iterations);
  if (subDefaults?.max_tool_iterations !== undefined && (!Number.isInteger(subDefaultsMaxTools) || subDefaultsMaxTools < 0)) {
    setFieldError("agents.subagent_defaults.max_tool_iterations", "Subagent max tool iterations must be an integer >= 0.");
  }
  const subDefaultsTimeout = Number(subDefaults?.timeout_ms);
  if (subDefaults?.timeout_ms !== undefined && (!Number.isInteger(subDefaultsTimeout) || subDefaultsTimeout < 0)) {
    setFieldError("agents.subagent_defaults.timeout_ms", "Subagent timeout must be an integer >= 0.");
  }
  const subThinking = asTrimmedString(subDefaults?.thinking_mode).toLowerCase();
  if (subThinking && !THINKING_MODES.includes(subThinking)) {
    setFieldError("agents.subagent_defaults.thinking_mode", "Subagent thinking mode must be one of never, on_error, always.");
  }
  const delegationModes = ["", "prompt_only", "tool_gated", "auto_execute"];
  const subDelegation = asTrimmedString(subDefaults?.delegation_mode);
  if (subDelegation && !delegationModes.includes(subDelegation)) {
    setFieldError("agents.subagent_defaults.delegation_mode", "Subagent delegation mode must be prompt_only, tool_gated, or auto_execute.");
  }

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
  if (draft?.memory?.embeddings_enabled && embeddingProvider && !EMBEDDING_PROVIDERS.includes(embeddingProvider)) {
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
  if (path === "model.provider") {
    maybeLoadDiscoveredProviderModels(value);
  }
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
      return { chat: draft.chat, discord: draft.discord, telegram: draft.telegram };
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
        telegram_enabled: draft.telegram?.enabled,
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

function appendSelectField({ parent, query, title, path, options, helpText = "", fieldErrors, normalizeValue, disabled = false }) {
  const optionLabels = options.map((option) => option.label || option.value);
  if (!fieldVisible(query, title, path, helpText, optionLabels.join(" "))) {
    return;
  }
  const errorText = shouldShowFieldError(path, fieldErrors) ? fieldErrors[path] : "";
  const field = createField({ title, path, helpText, errorText });
  const select = document.createElement("select");
  select.className = "settings-select";
  select.setAttribute("data-focus-id", `settings:${path}`);
  const current = asTrimmedString(getByPath(settingsState.draftConfig, path));
  const currentLower = current.toLowerCase();
  options.forEach((option) => {
    const entry = document.createElement("option");
    const optionValue = asString(option.value);
    entry.value = optionValue;
    entry.textContent = option.label || optionValue;
    if (optionValue === current || optionValue.toLowerCase() === currentLower) {
      entry.selected = true;
    }
    select.append(entry);
  });
  select.disabled = Boolean(disabled);
  select.addEventListener("change", () => {
    const nextValue = typeof normalizeValue === "function"
      ? normalizeValue(select.value)
      : asTrimmedString(select.value);
    updateDraft(path, nextValue, { deleteIfUndefined: false });
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

function appendModelNameField({ parent, query, fieldErrors }) {
  const provider = asTrimmedString(settingsState.draftConfig?.model?.provider).toLowerCase();
  const modelsResult = settingsState.providerModelsResults?.[provider];
  const discoveredModels = discoveredModelsForProvider(provider);
  const currentModel = asTrimmedString(settingsState.draftConfig?.model?.name);
  if (providerSupportsModelDiscovery(provider) && discoveredModels.length) {
    const options = currentModel
      ? []
      : [{ value: "", label: "(select model)" }];
    options.push(...discoveredModels.map((modelName) => ({ value: modelName, label: modelName })));
    if (currentModel && !discoveredModels.some((item) => item.toLowerCase() === currentModel.toLowerCase())) {
      options.unshift({ value: currentModel, label: `${currentModel} (current)` });
    }
    appendSelectField({
      parent,
      query,
      title: "Model name",
      path: "model.name",
      helpText: "Provider model identifier discovered from the selected provider. Use Query models to refresh the list.",
      options,
      fieldErrors,
      normalizeValue: (value) => asTrimmedString(value),
    });
    return;
  }

  let helpText = "Provider model identifier.";
  let placeholder = "GLM-4.7";
  if (providerSupportsModelDiscovery(provider)) {
    placeholder = "Select or type a model";
    if (modelsResult?.loading) {
      helpText = "Loading available models for the selected provider. This field becomes a dropdown when discovery completes.";
    } else if (modelsResult?.error) {
      helpText = "Provider model identifier. Automatic model discovery failed; you can still type a value or retry Query models.";
    } else {
      helpText = "Provider model identifier. Available models load automatically for the selected provider.";
    }
  }

  appendTextField({
    parent,
    query,
    title: "Model name",
    path: "model.name",
    helpText,
    placeholder,
    fieldErrors,
  });
}

function appendProviderSecretPrompt({ parent, provider }) {
  const normalized = asTrimmedString(provider).toLowerCase();
  const modelsResult = settingsState.providerModelsResults?.[normalized];
  if (!providerSupportsModelDiscovery(normalized) || !modelsResult?.missingKey) {
    return;
  }

  const panel = document.createElement("section");
  panel.className = "settings-section";

  const heading = document.createElement("h4");
  heading.className = "settings-subheading";
  heading.textContent = `${normalized} API key required`;
  panel.append(heading);

  const intro = document.createElement("p");
  intro.className = "muted";
  const envName = providerSecretEnvName(normalized) || `${normalized.toUpperCase()}_API_KEY`;
  intro.textContent = `OpenClawssy cannot query ${normalized} models yet because no API key is available. Store a key now under ${providerSecretStoreKey(normalized)}. Runtime can also use ${envName} if it is already present in the environment.`;
  panel.append(intro);

  const form = document.createElement("form");
  form.className = "secrets-form";
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    void submitProviderSecret(normalized);
  });

  const keyField = document.createElement("label");
  keyField.className = "secrets-form-field";
  const keyLabel = document.createElement("span");
  keyLabel.textContent = `${normalized} API key`;
  const keyInput = document.createElement("input");
  keyInput.type = "password";
  keyInput.autocomplete = "new-password";
  keyInput.className = "settings-input";
  keyInput.placeholder = `Paste ${normalized} API key`;
  keyInput.value = settingsState.providerSecretDrafts[normalized] || "";
  keyInput.addEventListener("input", () => {
    settingsState.providerSecretDrafts[normalized] = keyInput.value;
    resetProviderSecretFeedback(normalized);
  });
  keyField.append(keyLabel, keyInput);

  const actions = document.createElement("div");
  actions.className = "secrets-form-actions";
  const saveButton = document.createElement("button");
  saveButton.type = "submit";
  saveButton.className = "chat-send-button";
  saveButton.disabled = Boolean(settingsState.providerSecretSaving[normalized]);
  saveButton.textContent = settingsState.providerSecretSaving[normalized] ? "Saving..." : "Save Key";
  actions.append(saveButton);

  form.append(keyField, actions);
  panel.append(form);

  if (settingsState.providerSecretError[normalized]) {
    const error = document.createElement("p");
    error.className = "settings-inline-error";
    error.textContent = settingsState.providerSecretError[normalized];
    panel.append(error);
  }
  if (settingsState.providerSecretSuccess[normalized]) {
    const success = document.createElement("p");
    success.className = "settings-save-success";
    success.textContent = settingsState.providerSecretSuccess[normalized];
    panel.append(success);
  }

  parent.append(panel);
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
    options: MODEL_PROVIDERS.map((provider) => ({ value: provider, label: provider })),
    fieldErrors,
    normalizeValue: (value) => asTrimmedString(value).toLowerCase(),
  });
  appendModelNameField({ parent: panel, query, fieldErrors });
  appendProviderSecretPrompt({ parent: panel, provider: settingsState.draftConfig?.model?.provider });
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
  appendNumberField({
    parent: panel,
    query,
    title: "Provider timeout (ms)",
    path: "model.timeout_ms",
    helpText: "HTTP timeout for each provider request. Increase this for long tool-heavy or long-form generations.",
    placeholder: "90000",
    fieldErrors,
  });

  const providerHeader = document.createElement("h4");
  providerHeader.className = "settings-subheading";
  providerHeader.textContent = "Provider endpoints";
  panel.append(providerHeader);

  MODEL_PROVIDERS.forEach((provider) => {
    const card = document.createElement("section");
    card.className = "settings-section";
    const heading = document.createElement("h5");
    heading.className = "settings-subheading";
    heading.textContent = provider === settingsState.draftConfig?.model?.provider ? `${provider} (global active)` : provider;
    card.append(heading);
    appendTextField({
      parent: card,
      query,
      title: `${provider} base URL`,
      path: `providers.${provider}.base_url`,
      helpText: "Endpoint base URL used for provider HTTP requests.",
      placeholder: "https://...",
      fieldErrors,
    });
    appendTextField({
      parent: card,
      query,
      title: `${provider} API key env`,
      path: `providers.${provider}.api_key_env`,
      helpText: "Environment variable containing provider API key. Secret values remain redacted.",
      placeholder: "PROVIDER_API_KEY",
      fieldErrors,
    });
    const actions = document.createElement("div");
    actions.className = "settings-advanced-actions";
    const activate = document.createElement("button");
    activate.type = "button";
    activate.className = "layout-toggle";
    activate.textContent = "Use for global model";
    activate.addEventListener("click", () => {
      updateDraft("model.provider", provider);
    });
    const test = document.createElement("button");
    test.type = "button";
    test.className = "layout-toggle";
    test.textContent = settingsState.providerTestResults[provider]?.loading ? "Testing..." : "Test provider";
    test.disabled = settingsState.providerTestResults[provider]?.loading;
    test.addEventListener("click", () => {
      void testProvider(provider);
    });
    actions.append(activate, test);
    if (providerSupportsModelDiscovery(provider)) {
      const queryModels = document.createElement("button");
      queryModels.type = "button";
      queryModels.className = "layout-toggle";
      queryModels.textContent = settingsState.providerModelsResults[provider]?.loading ? "Querying..." : "Query models";
      queryModels.disabled = settingsState.providerModelsResults[provider]?.loading;
      queryModels.addEventListener("click", () => {
        void loadProviderModels(provider);
      });
      actions.append(queryModels);
    }
    card.append(actions);
    const result = settingsState.providerTestResults[provider];
    if (result?.message) {
      const status = document.createElement("p");
      status.className = result.ok ? "settings-save-success" : "settings-inline-error";
      status.textContent = result.message;
      card.append(status);
    }
    const modelsResult = settingsState.providerModelsResults[provider];
    if (modelsResult?.message) {
      const status = document.createElement("p");
      status.className = modelsResult.error ? "settings-inline-error" : "settings-save-success";
      status.textContent = modelsResult.message;
      card.append(status);
    }
    if (Array.isArray(modelsResult?.models) && modelsResult.models.length) {
      const list = document.createElement("div");
      list.className = "settings-advanced-actions";
      modelsResult.models.slice(0, 12).forEach((modelName) => {
        const button = document.createElement("button");
        button.type = "button";
        button.className = "layout-toggle";
        button.textContent = modelName;
        button.title = `Use ${modelName}`;
        button.addEventListener("click", () => {
          updateDraft("model.provider", provider);
          updateDraft("model.name", modelName);
        });
        list.append(button);
      });
      card.append(list);
    }
    panel.append(card);
  });
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
    title: "Allowed chat senders",
    path: "chat.allow_users",
    helpText: "Allowlist of sender/user ids. Include dashboard_user for dashboard chat access.",
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

  panel.append(buildDiscordSetupPanel());

  const telegramHeader = document.createElement("h4");
  telegramHeader.className = "settings-subheading";
  telegramHeader.textContent = "Telegram";
  panel.append(telegramHeader);

  appendCheckboxField({
    parent: panel,
    query,
    title: "Telegram enabled",
    path: "telegram.enabled",
    helpText: "Enables Telegram bot connector.",
    fieldErrors,
  });
  appendTextField({
    parent: panel,
    query,
    title: "Telegram default agent",
    path: "telegram.default_agent_id",
    helpText: "Agent used by Telegram messages.",
    placeholder: "default",
    fieldErrors,
  });
  appendTextField({
    parent: panel,
    query,
    title: "Telegram token env",
    path: "telegram.token_env",
    helpText: "Environment variable containing Telegram bot token.",
    placeholder: "TELEGRAM_BOT_TOKEN",
    fieldErrors,
  });
  appendTextField({
    parent: panel,
    query,
    title: "Telegram command prefix",
    path: "telegram.command_prefix",
    helpText: "Prefix used for Telegram bot commands.",
    placeholder: "/ask",
    fieldErrors,
  });
  appendNumberField({
    parent: panel,
    query,
    title: "Telegram rate limit per minute",
    path: "telegram.rate_limit_per_min",
    helpText: "Per-user Telegram command budget.",
    fieldErrors,
  });
  appendListField({
    parent: panel,
    query,
    title: "Allowed Telegram users",
    path: "telegram.allow_users",
    helpText: "Optional allowlist of Telegram usernames.",
    placeholder: "alice\nbob",
    fieldErrors,
  });
  appendListField({
    parent: panel,
    query,
    title: "Allowed Telegram chats",
    path: "telegram.allow_chats",
    helpText: "Optional allowlist of Telegram chat ids.",
    fieldErrors,
  });
}

function buildDiscordSetupPanel() {
  const panel = document.createElement("section");
  panel.className = "settings-section";

  const heading = document.createElement("h4");
  heading.className = "settings-subheading";
  heading.textContent = "Discord Setup";
  panel.append(heading);

  const intro = document.createElement("p");
  intro.className = "muted";
  intro.textContent = "Store the Discord bot token in the encrypted secrets store under discord/bot_token. This dashboard flow is write-only and supports rotation by saving a new value.";
  panel.append(intro);

  const status = document.createElement("p");
  status.className = "muted";
  if (settingsState.discordSecretLoading) {
    status.textContent = "Discord token: checking...";
  } else {
    status.textContent = `Discord token: ${settingsState.discordSecretPresent ? "Present ✅" : "Missing ❌"}`;
  }
  panel.append(status);

  const envNote = document.createElement("p");
  envNote.className = "muted";
  envNote.textContent = `discord.token_env (${asTrimmedString(settingsState.draftConfig?.discord?.token_env) || "DISCORD_BOT_TOKEN"}) is only needed if you want an external environment variable fallback. The dashboard-managed secret is loaded from ${DISCORD_SECRET_KEY}.`;
  panel.append(envNote);

  if (settingsState.discordSecretError) {
    const error = document.createElement("p");
    error.className = "settings-inline-error";
    error.textContent = `Failed to load Discord token status: ${settingsState.discordSecretError}`;
    panel.append(error);
  }

  if (settingsState.draftConfig?.discord?.enabled && !settingsState.discordSecretPresent) {
    const warning = document.createElement("p");
    warning.className = "settings-inline-error";
    warning.textContent = "Discord is enabled but no dashboard-managed token is stored. Save a token here or ensure the external env named by discord.token_env is present before starting the connector.";
    panel.append(warning);
  }

  const form = document.createElement("form");
  form.className = "secrets-form";
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    void submitDiscordSecret();
  });

  const tokenField = document.createElement("label");
  tokenField.className = "secrets-form-field";
  const tokenLabel = document.createElement("span");
  tokenLabel.textContent = settingsState.discordSecretPresent ? "Rotate Discord token" : "Discord bot token";
  const tokenInput = document.createElement("input");
  tokenInput.type = "password";
  tokenInput.autocomplete = "new-password";
  tokenInput.className = "settings-input";
  tokenInput.placeholder = settingsState.discordSecretPresent ? "Paste new token to rotate" : "Paste Discord bot token";
  tokenInput.value = settingsState.discordTokenDraft;
  tokenInput.addEventListener("input", () => {
    settingsState.discordTokenDraft = tokenInput.value;
    resetDiscordSecretFeedback();
  });
  tokenField.append(tokenLabel, tokenInput);

  const actions = document.createElement("div");
  actions.className = "secrets-form-actions";
  const saveButton = document.createElement("button");
  saveButton.type = "submit";
  saveButton.className = "chat-send-button";
  saveButton.disabled = settingsState.discordTokenSaving;
  saveButton.textContent = settingsState.discordTokenSaving ? "Saving..." : "Save Token";
  actions.append(saveButton);

  if (settingsState.discordSecretPresent) {
    const deleteButton = document.createElement("button");
    deleteButton.type = "button";
    deleteButton.className = "layout-toggle";
    deleteButton.disabled = settingsState.discordTokenDeleting;
    deleteButton.textContent = settingsState.discordTokenDeleting ? "Deleting..." : "Delete stored token";
    deleteButton.addEventListener("click", () => {
      void deleteDiscordSecret();
    });
    actions.append(deleteButton);
  }

  form.append(tokenField, actions);
  panel.append(form);

  const rotateNote = document.createElement("p");
  rotateNote.className = "muted";
  rotateNote.textContent = "Saving a new token overwrites the previous value. The stored token is never shown again after save.";
  panel.append(rotateNote);

  if (settingsState.discordTokenError) {
    const error = document.createElement("p");
    error.className = "settings-inline-error";
    error.textContent = settingsState.discordTokenError;
    panel.append(error);
  }
  if (settingsState.discordTokenSuccess) {
    const success = document.createElement("p");
    success.className = "settings-save-success";
    success.textContent = settingsState.discordTokenSuccess;
    panel.append(success);
  }

  return panel;
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
  const keys = collectKnownAgentIDs();
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
    options: EMBEDDING_PROVIDERS.map((provider) => ({ value: provider, label: provider })),
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
  const knownAgentIDs = collectKnownAgentIDs();
  const inheritsGlobalModel = !asTrimmedString(selectedProfile?.model?.provider) && !asTrimmedString(selectedProfile?.model?.name) && !selectedProfile?.model?.max_tokens && !selectedProfile?.model?.temperature && !selectedProfile?.model?.timeout_ms;

  const tableWrap = document.createElement("section");
  tableWrap.className = "settings-section";
  const tableTitle = document.createElement("h4");
  tableTitle.className = "settings-subheading";
  tableTitle.textContent = "Agent profile summary";
  tableWrap.append(tableTitle);
  const table = document.createElement("table");
  table.className = "settings-diff-table";
  const head = document.createElement("thead");
  head.innerHTML = "<tr><th>Agent</th><th>Enabled</th><th>Provider</th><th>Model</th><th>Temp</th><th>Max tokens</th><th>Timeout ms</th><th>Mode</th></tr>";
  const body = document.createElement("tbody");
  knownAgentIDs.forEach((agentID) => {
      const profile = profiles[agentID] || {};
      const hasSavedProfile = Object.prototype.hasOwnProperty.call(profiles, agentID);
      const row = document.createElement("tr");
      const inherit = !asTrimmedString(profile?.model?.provider) && !asTrimmedString(profile?.model?.name) && !profile?.model?.max_tokens && !profile?.model?.temperature && !profile?.model?.timeout_ms;
      row.innerHTML = `<td>${agentID}</td><td>${profile?.enabled === false ? "off" : "on"}</td><td>${asTrimmedString(profile?.model?.provider) || "(global)"}</td><td>${asTrimmedString(profile?.model?.name) || "(global)"}</td><td>${profile?.model?.temperature ?? "(global)"}</td><td>${profile?.model?.max_tokens ?? "(global)"}</td><td>${profile?.model?.timeout_ms ?? "(global)"}</td><td>${hasSavedProfile ? (inherit ? "inherit" : "override") : "discovered"}</td>`;
      row.addEventListener("click", () => {
        settingsState.selectedAgentProfile = agentID;
        rerender();
      });
      body.append(row);
    });
  table.append(head, body);
  tableWrap.append(table);
  panel.append(tableWrap);

  const header = document.createElement("h4");
  header.className = "settings-subheading";
  header.textContent = "Agent Profile Editor";
  panel.append(header);

  const picker = createField({ title: "Profile agent", path: "agents.profiles", helpText: "Edit per-agent activation and model override values." });
  const select = document.createElement("select");
  select.className = "settings-select";
  knownAgentIDs.forEach((agentID) => {
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
  addButton.textContent = "Add / Select Profile";
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

  const profileState = document.createElement("p");
  profileState.className = "muted";
  profileState.textContent = Object.prototype.hasOwnProperty.call(profiles, selected)
    ? `Editing saved profile: ${selected}`
    : `Editing discovered agent: ${selected}. Fields below will create a profile only when you change them.`;
  panel.append(profileState);

  if (fieldVisible(query, "Profile enabled", `agents.profiles.${selected}.enabled`, "Toggle this agent profile on/off. Unset defaults to on.")) {
    const enabledField = createField({
      title: "Profile enabled",
      path: `agents.profiles.${selected}.enabled`,
      helpText: "Toggle this agent profile on/off. Unset defaults to on.",
      errorText: shouldShowFieldError(`agents.profiles.${selected}.enabled`, fieldErrors) ? fieldErrors[`agents.profiles.${selected}.enabled`] : "",
    });
    const enabledRow = document.createElement("label");
    enabledRow.className = "settings-checkbox-row";
    const enabledInput = document.createElement("input");
    enabledInput.type = "checkbox";
    enabledInput.checked = selectedProfile?.enabled !== false;
    enabledInput.addEventListener("change", () => {
      updateDraft(`agents.profiles.${selected}.enabled`, enabledInput.checked ? undefined : false, { deleteIfUndefined: enabledInput.checked });
    });
    const enabledText = document.createElement("span");
    enabledText.textContent = enabledInput.checked ? "Enabled (default)" : "Disabled";
    enabledRow.append(enabledInput, enabledText);
    enabledField.append(enabledRow);
    panel.append(enabledField);
  }

  appendCheckboxField({ parent: panel, query, title: "Profile self-improvement", path: `agents.profiles.${selected}.self_improvement`, helpText: "Allows this agent to modify its own prompt files when global switch is on.", fieldErrors });
  appendCheckboxField({ parent: panel, query, title: "Inherit global model", path: `agents.profiles.${selected}.__inherit_model`, helpText: "When enabled, this profile uses the global provider/model/temp/max_tokens/timeout.", fieldErrors: {} });
  const inheritField = panel.lastElementChild?.querySelector?.("input[type='checkbox']");
  if (inheritField) {
    inheritField.checked = inheritsGlobalModel;
    inheritField.addEventListener("change", () => {
      const next = cloneJSON(settingsState.draftConfig);
      next.agents.profiles[selected] = next.agents.profiles[selected] || {};
      if (inheritField.checked) {
        delete next.agents.profiles[selected].model;
      } else {
        next.agents.profiles[selected].model = next.agents.profiles[selected].model || {};
      }
      delete next.agents.profiles[selected].__inherit_model;
      settingsState.draftConfig = normalizeConfigShape(next);
      settingsState.advancedRaw = `${JSON.stringify(settingsState.draftConfig, null, 2)}\n`;
      rerender();
    });
  }
  appendSelectField({
    parent: panel,
    query,
    title: "Profile model provider",
    path: `agents.profiles.${selected}.model.provider`,
    helpText: "Optional override provider for this agent profile.",
    options: [{ value: "", label: "(inherit global)" }, ...MODEL_PROVIDERS.map((provider) => ({ value: provider, label: provider }))],
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
  appendNumberField({
    parent: panel,
    query,
    title: "Profile temperature",
    path: `agents.profiles.${selected}.model.temperature`,
    helpText: "Optional override temperature. Leave blank to inherit.",
    placeholder: "(inherit global)",
    step: "0.1",
    fieldErrors,
  });
  appendNumberField({
    parent: panel,
    query,
    title: "Profile provider timeout (ms)",
    path: `agents.profiles.${selected}.model.timeout_ms`,
    helpText: "Optional override provider timeout. Set 0/empty to inherit global.",
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

  const bulk = document.createElement("section");
  bulk.className = "settings-section";
  const bulkTitle = document.createElement("h4");
  bulkTitle.className = "settings-subheading";
  bulkTitle.textContent = "Bulk profile actions";
  const bulkActions = document.createElement("div");
  bulkActions.className = "settings-advanced-actions";
  const bulkProvider = document.createElement("select");
  bulkProvider.className = "settings-select";
  MODEL_PROVIDERS.forEach((provider) => {
    const option = document.createElement("option");
    option.value = provider;
    option.textContent = provider;
    bulkProvider.append(option);
  });
  const bulkModel = document.createElement("input");
  bulkModel.className = "settings-input";
  bulkModel.placeholder = "model name";
  const bulkApply = document.createElement("button");
  bulkApply.type = "button";
  bulkApply.className = "layout-toggle";
  bulkApply.textContent = "Set all agent overrides";
  bulkApply.addEventListener("click", () => {
    if (!window.confirm(`Set all agent profiles to ${bulkProvider.value} / ${bulkModel.value || "(keep name)"}?`)) {
      return;
    }
    const next = cloneJSON(settingsState.draftConfig);
    Object.keys(next.agents.profiles || {}).forEach((agentID) => {
      next.agents.profiles[agentID] = next.agents.profiles[agentID] || {};
      next.agents.profiles[agentID].model = next.agents.profiles[agentID].model || {};
      next.agents.profiles[agentID].model.provider = bulkProvider.value;
      if (bulkModel.value.trim()) {
        next.agents.profiles[agentID].model.name = bulkModel.value.trim();
      }
    });
    settingsState.draftConfig = normalizeConfigShape(next);
    settingsState.advancedRaw = `${JSON.stringify(settingsState.draftConfig, null, 2)}\n`;
    rerender();
  });
  bulkActions.append(bulkProvider, bulkModel, bulkApply);
  bulk.append(bulkTitle, bulkActions);
  panel.append(bulk);

  const subHeader = document.createElement("h4");
  subHeader.className = "settings-subheading";
  subHeader.textContent = "Subagent defaults";
  panel.append(subHeader);
  appendListField({ parent: panel, query, title: "Allowed tools", path: "agents.subagent_defaults.allowed_tools", helpText: "One tool per line. Leave empty to use defaults.", placeholder: "fs.read\ncode.search", fieldErrors });
  appendNumberField({ parent: panel, query, title: "Subagent timeout ms", path: "agents.subagent_defaults.timeout_ms", helpText: "Per-subagent timeout. 0 uses default.", placeholder: "45000", fieldErrors });
  appendNumberField({ parent: panel, query, title: "Subagent max tool iterations", path: "agents.subagent_defaults.max_tool_iterations", helpText: "0 uses default.", placeholder: "12", fieldErrors });
  appendSelectField({ parent: panel, query, title: "Subagent thinking mode", path: "agents.subagent_defaults.thinking_mode", helpText: "Controls subagent thinking output.", options: [{ value: "", label: "(inherit)" }, ...THINKING_MODES.map((mode) => ({ value: mode, label: mode }))], fieldErrors });
  appendSelectField({ parent: panel, query, title: "Subagent delegation mode", path: "agents.subagent_defaults.delegation_mode", helpText: "Controls subagent delegation behavior.", options: [{ value: "", label: "(inherit)" }, { value: "prompt_only", label: "prompt_only" }, { value: "tool_gated", label: "tool_gated" }, { value: "auto_execute", label: "auto_execute" }], fieldErrors });
  const allowedTools = Array.isArray(settingsState.draftConfig?.agents?.subagent_defaults?.allowed_tools) ? settingsState.draftConfig.agents.subagent_defaults.allowed_tools : [];
  const guardrail = document.createElement("p");
  guardrail.className = allowedTools.length > 12 ? "settings-inline-error" : "muted";
  guardrail.textContent = allowedTools.length > 12 ? "Warning: this subagent tool allowlist is broad. Narrower defaults are safer and easier to audit." : "Tip: keep subagent tool lists focused. Prefer narrower permissions and shorter timeouts.";
  panel.append(guardrail);

  const overridesHeader = document.createElement("h4");
  overridesHeader.className = "settings-subheading";
  overridesHeader.textContent = "Subagent overrides";
  panel.append(overridesHeader);
  const overrides = settingsState.draftConfig?.agents?.subagent_overrides || {};
  Object.keys(overrides).sort().forEach((agentID) => {
    const card = document.createElement("section");
    card.className = "settings-section";
    const title = document.createElement("h5");
    title.className = "settings-subheading";
    title.textContent = agentID;
    card.append(title);
    appendListField({ parent: card, query, title: `${agentID} allowed tools`, path: `agents.subagent_overrides.${agentID}.allowed_tools`, helpText: "One tool per line.", placeholder: "fs.read\nagent.run", fieldErrors });
    appendNumberField({ parent: card, query, title: `${agentID} timeout ms`, path: `agents.subagent_overrides.${agentID}.timeout_ms`, helpText: "0 uses default.", placeholder: "30000", fieldErrors });
    appendSelectField({ parent: card, query, title: `${agentID} thinking mode`, path: `agents.subagent_overrides.${agentID}.thinking_mode`, helpText: "Optional override.", options: [{ value: "", label: "(inherit)" }, ...THINKING_MODES.map((mode) => ({ value: mode, label: mode }))], fieldErrors });
    const overrideTools = Array.isArray(overrides[agentID]?.allowed_tools) ? overrides[agentID].allowed_tools : [];
    if (overrideTools.length > 12) {
      const warning = document.createElement("p");
      warning.className = "settings-inline-error";
      warning.textContent = "This override grants a broad tool set. Review whether every listed tool is truly needed.";
      card.append(warning);
    }
    panel.append(card);
  });
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
      name: "Telegram Connector",
      status: settingsState.draftConfig?.telegram?.enabled ? "enabled" : "disabled",
      detail: "Controlled by telegram.enabled",
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
    try {
      await loadAvailableAgents();
    } catch (_error) {
      settingsState.availableAgentIDs = sortedUniqueAgentIDs([]);
    }
    settingsState.advancedRaw = `${JSON.stringify(settingsState.draftConfig, null, 2)}\n`;
    settingsState.advancedRawError = "";
    settingsState.touchedFields = new Set();
    settingsState.saveAttempted = false;
    settingsState.discordTokenDraft = "";
    maybeLoadDiscoveredProviderModels(settingsState.draftConfig?.model?.provider);
    await loadDiscordSecretStatus({ rerenderPage: false });
  } catch (error) {
    settingsState.loadError = error instanceof Error ? error.message : String(error);
  } finally {
    settingsState.loading = false;
    rerender();
  }
}

async function validateConfigRemotely() {
  if (!settingsState.draftConfig) {
    return;
  }
  settingsState.validatePending = true;
  settingsState.saveError = null;
  settingsState.saveSuccess = "";
  settingsState.saveAttempted = true;
  rerender();
  try {
    const payload = await settingsState.apiClient.post("/api/admin/config/validate", settingsState.draftConfig);
    if (payload?.ok === false) {
      settingsState.saveError = {
        message: payload?.message || "Validation failed.",
        status: 200,
        code: "validate.failed",
        details: { field_errors: payload?.field_errors || {} },
      };
    } else {
      settingsState.saveSuccess = "Validation passed.";
    }
  } catch (error) {
    settingsState.saveError = {
      message: error?.message || String(error),
      status: Number(error?.status) || 0,
      code: error?.code || "validate.failed",
      details: error?.details || null,
    };
  } finally {
    settingsState.validatePending = false;
    rerender();
  }
}

async function testProvider(provider) {
  const cfg = settingsState.draftConfig?.providers?.[provider] || {};
  settingsState.providerTestResults[provider] = { loading: true, message: "Testing..." };
  rerender();
  try {
    const payload = await settingsState.apiClient.post("/api/admin/providers/test", {
      provider,
      base_url: asTrimmedString(cfg.base_url),
    });
    settingsState.providerTestResults[provider] = {
      loading: false,
      ok: Boolean(payload?.ok),
      message: payload?.status_text || payload?.message || "Provider probe completed.",
    };
  } catch (error) {
    settingsState.providerTestResults[provider] = {
      loading: false,
      ok: false,
      message: error?.message || String(error),
    };
  }
  rerender();
}

async function loadProviderModels(provider) {
  settingsState.providerModelsResults[provider] = { loading: true, message: "Querying available models...", models: [] };
  rerender();
  try {
    const payload = await settingsState.apiClient.get(`/api/admin/providers/models?provider=${encodeURIComponent(provider)}`);
    const models = Array.isArray(payload?.models)
      ? payload.models.map((item) => asTrimmedString(item)).filter((item) => !!item)
      : [];
    settingsState.providerModelsResults[provider] = {
      loading: false,
      error: false,
      missingKey: false,
      models,
      message: models.length ? `Loaded ${models.length} model${models.length === 1 ? "" : "s"}.` : "No models returned.",
    };
  } catch (error) {
    const message = error?.message || String(error);
    settingsState.providerModelsResults[provider] = {
      loading: false,
      error: true,
      missingKey: isMissingProviderAPIKeyMessage(message),
      models: [],
      message,
    };
  }
  rerender();
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
    await settingsState.apiClient.patch("/api/admin/config", settingsState.draftConfig);
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

  const validateButton = document.createElement("button");
  validateButton.type = "button";
  validateButton.className = "layout-toggle";
  validateButton.textContent = settingsState.validatePending ? "Validating..." : "Validate";
  validateButton.disabled = settingsState.savePending || settingsState.validatePending;
  validateButton.addEventListener("click", () => {
    void validateConfigRemotely();
  });

  const saveButton = document.createElement("button");
  saveButton.type = "button";
  saveButton.className = "chat-send-button";
  saveButton.textContent = settingsState.savePending ? "Saving..." : "Save Config";
  saveButton.disabled = settingsState.savePending;
  saveButton.addEventListener("click", () => {
    void saveConfig();
  });

  actions.append(reloadButton, resetDraftButton, validateButton, saveButton);
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

export function settingsPageHasUnsavedChanges() {
  if (!settingsState.baselineConfig || !settingsState.draftConfig) {
    return false;
  }
  return computeDiffRows(settingsState.baselineConfig, settingsState.draftConfig).length > 0;
}

export function disposeSettingsPage() {
  settingsState.container = null;
}
