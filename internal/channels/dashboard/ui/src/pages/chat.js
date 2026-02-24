const CHAT_DEFAULTS = {
  userID: "dashboard_user",
  roomID: "dashboard",
  agentID: "default",
};

const RUN_POLL_MS = 1500;
const SESSION_POLL_MS = 2000;
const STREAM_SESSION_POLL_MS = 5000;
const STREAM_RENDER_THROTTLE_MS = 100;
const SESSION_MESSAGES_LIMIT = 200;
const SESSION_LOOKUP_LIMIT = 1;
const TERMINAL_RUN_STATUSES = new Set(["completed", "failed", "canceled"]);

const chatViewState = {
  draft: "",
  lastPrompt: "",
  transcript: [],
  sendPending: false,
  sendError: null,
  debugCopyStatus: "",

  currentRunID: "",
  currentRunStatus: "idle",
  currentRunStartedAtMs: 0,
  currentSessionID: "",
  currentRunLastUpdatedAt: "",
  currentRunLastOutput: "",

  latestToolActivity: null,
  streamToolEvents: [],
  lastErrorSummary: "",
  loopRisk: {
    level: "low",
    score: 0,
    reasons: [],
    repeatCount: 0,
    failureCount: 0,
    windowSize: 0,
  },

  polling: false,
  pollToken: 0,
  runPollTimer: 0,
  sessionPollTimer: 0,
  idleSessionTimer: 0,
  runPollInFlight: false,
  sessionPollInFlight: false,
  idleSessionInFlight: false,
  sessionBootstrapInFlight: false,
  runPollingEnabled: true,
  sessionPollIntervalMS: SESSION_POLL_MS,

  streamActive: false,
  streamAbortController: null,
  streamLastEventID: 0,
  currentStreamingText: "",
  currentStreamRunID: "",
  streamRenderTimer: 0,

  transcriptPinned: true,
  transcriptScrollTop: 0,

  routeUnsubscribe: null,
  container: null,
  apiClient: null,
  store: null,
  availableAgents: [CHAT_DEFAULTS.agentID],
  selectedAgentID: CHAT_DEFAULTS.agentID,
  activeAgentID: CHAT_DEFAULTS.agentID,
  switchAgentPending: false,
  switchAgentError: "",
  agentProfileContext: null,
  agentGlobalConfig: null,
};

function safeText(value) {
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

function compactText(value, maxChars = 220) {
  const text = safeText(value).replace(/\s+/g, " ");
  if (!text) {
    return "";
  }
  if (text.length <= maxChars) {
    return text;
  }
  return `${text.slice(0, Math.max(0, maxChars - 3))}...`;
}

function parseMaybeJSON(value) {
  if (value === null || value === undefined) {
    return null;
  }
  if (typeof value === "object") {
    return value;
  }
  const text = safeText(value);
  if (!text) {
    return null;
  }
  try {
    return JSON.parse(text);
  } catch (_error) {
    return null;
  }
}

function isLikelyJSONText(value) {
  const text = safeText(value);
  if (!text) {
    return false;
  }
  return text.startsWith("{") || text.startsWith("[");
}

function scanJSONStructure(value) {
  const text = String(value || "");
  const stack = [];
  let inString = false;
  let escaped = false;

  for (let index = 0; index < text.length; index += 1) {
    const char = text[index];
    if (inString) {
      if (escaped) {
        escaped = false;
        continue;
      }
      if (char === "\\") {
        escaped = true;
        continue;
      }
      if (char === '"') {
        inString = false;
      }
      continue;
    }

    if (char === '"') {
      inString = true;
      continue;
    }
    if (char === "{") {
      stack.push("}");
      continue;
    }
    if (char === "[") {
      stack.push("]");
      continue;
    }
    if (char === "}" || char === "]") {
      const expected = stack.pop();
      if (expected !== char) {
        return { complete: false, partial: false, invalid: true };
      }
    }
  }

  if (inString) {
    return { complete: false, partial: true, invalid: false };
  }
  if (stack.length > 0) {
    return { complete: false, partial: true, invalid: false };
  }
  return { complete: true, partial: false, invalid: false };
}

function tryFormatJSONText(value) {
  const text = safeText(value);
  if (!isLikelyJSONText(text)) {
    return "";
  }
  const structure = scanJSONStructure(text);
  if (!structure.complete) {
    return "";
  }
  try {
    const parsed = JSON.parse(text);
    return JSON.stringify(parsed, null, 2);
  } catch (_error) {
    return "";
  }
}

function isJSONFenceLanguage(value) {
  const language = safeText(value).toLowerCase();
  if (!language) {
    return false;
  }
  return language === "json" || language === "jsonc" || language === "application/json";
}

function normalizeJSONFencedBlocks(value) {
  const source = String(value || "");
  if (!source.includes("```")) {
    return source;
  }

  const fencePattern = /```([^\n]*)\n([\s\S]*?)```/g;
  let changed = false;
  const normalized = source.replace(fencePattern, (full, rawLanguage, rawBody) => {
    const body = String(rawBody || "");
    const language = safeText(rawLanguage).toLowerCase();
    if (!isJSONFenceLanguage(language) && !isLikelyJSONText(body)) {
      return full;
    }
    const formatted = tryFormatJSONText(body);
    if (!formatted) {
      return full;
    }
    changed = true;
    return `\`\`\`json\n${formatted}\n\`\`\``;
  });

  return changed ? normalized : source;
}

function formatAssistantContentForDisplay(value) {
  const source = String(value || "");
  if (!source) {
    return "";
  }
  const wholeJSON = tryFormatJSONText(source);
  if (wholeJSON) {
    return wholeJSON;
  }
  return normalizeJSONFencedBlocks(source);
}

function asDisplayText(value) {
  if (value === null || value === undefined) {
    return "";
  }
  if (typeof value === "string") {
    return value;
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch (_error) {
    return String(value);
  }
}

function firstNonEmpty(...values) {
  for (const value of values) {
    const text = safeText(value);
    if (text) {
      return text;
    }
  }
  return "";
}

function isTerminalStatus(status) {
  return TERMINAL_RUN_STATUSES.has(safeText(status).toLowerCase());
}

function settingsProfileHash(agentID) {
  const params = new URLSearchParams();
  params.set("category", "agents");
  const selected = safeText(agentID);
  if (selected) {
    params.set("profile", selected);
  }
  return `#/settings?${params.toString()}`;
}

function copyToClipboardWithFallback(text) {
  const value = String(text || "");
  if (!value) {
    return Promise.reject(new Error("Nothing to copy"));
  }
  if (navigator?.clipboard?.writeText) {
    return navigator.clipboard.writeText(value);
  }
  return new Promise((resolve, reject) => {
    try {
      const textarea = document.createElement("textarea");
      textarea.value = value;
      textarea.setAttribute("readonly", "readonly");
      textarea.style.position = "fixed";
      textarea.style.opacity = "0";
      document.body.append(textarea);
      textarea.focus();
      textarea.select();
      const copied = document.execCommand("copy");
      textarea.remove();
      if (!copied) {
        reject(new Error("Copy command failed"));
        return;
      }
      resolve();
    } catch (error) {
      reject(error instanceof Error ? error : new Error(String(error)));
    }
  });
}

function debugBundlePayload() {
  const lastError = chatViewState.store?.getState?.().lastError || null;
  return {
    context: {
      user_id: CHAT_DEFAULTS.userID,
      room_id: CHAT_DEFAULTS.roomID,
      selected_agent_id: safeText(chatViewState.selectedAgentID) || CHAT_DEFAULTS.agentID,
      active_agent_id: safeText(chatViewState.activeAgentID),
    },
    run: {
      id: safeText(chatViewState.currentRunID),
      status: safeText(chatViewState.currentRunStatus),
      session_id: safeText(chatViewState.currentSessionID),
      updated_at: safeText(chatViewState.currentRunLastUpdatedAt),
      last_output_preview: compactText(chatViewState.currentRunLastOutput, 600),
    },
    activity: {
      polling: Boolean(chatViewState.polling),
      latest_tool: chatViewState.latestToolActivity,
      loop_risk: chatViewState.loopRisk,
      last_error_summary: safeText(chatViewState.lastErrorSummary),
      send_error: chatViewState.sendError || null,
      global_last_error: lastError,
    },
    transcript_tail: chatViewState.transcript.slice(-10),
    timestamp: new Date().toISOString(),
  };
}

async function copyDebugBundle() {
  try {
    const payload = debugBundlePayload();
    const formatted = `${JSON.stringify(payload, null, 2)}\n`;
    await copyToClipboardWithFallback(formatted);
    chatViewState.debugCopyStatus = "Debug bundle copied.";
  } catch (error) {
    chatViewState.debugCopyStatus = `Copy failed: ${error?.message || String(error)}`;
  }
  rerenderIfActive();
}

function retryLastPrompt() {
  const message = safeText(chatViewState.lastPrompt);
  if (!message || chatViewState.sendPending) {
    return;
  }
  chatViewState.draft = message;
  chatViewState.debugCopyStatus = "";
  void sendMessage();
}
function transcriptFingerprint() {
  return chatViewState.transcript
    .map((item) => `${item?.role}:${item?.pending ? "p" : "d"}:${(item?.content || "").length}`)
    .join("|");
}

let lastRenderedFingerprint = "";

function isTranscriptNearBottom(element, threshold = 36) {
  if (!element) {
    return true;
  }
  const distance = element.scrollHeight - (element.scrollTop + element.clientHeight);
  return distance <= threshold;
}

function toErrorPayload(scope, err, extra = {}) {
  if (!err) {
    return { scope, message: "Unknown error", ...extra };
  }
  return {
    scope,
    message: err.message || String(err),
    code: err.code || "error.unknown",
    status: Number(err.status) || 0,
    details: err.details || null,
    ...extra,
  };
}

function updateLastError(store, errorLike) {
  if (!store) {
    return;
  }
  const current = JSON.stringify(store.getState().lastError || null);
  const next = JSON.stringify(errorLike || null);
  if (current === next) {
    return;
  }
  store.setState({ lastError: errorLike || null });
}

function pollSessionMessagesPath(sessionID) {
  return `/api/admin/chat/sessions/${encodeURIComponent(sessionID)}/messages?limit=${encodeURIComponent(String(SESSION_MESSAGES_LIMIT))}`;
}

function listSessionsPath() {
  const params = new URLSearchParams({
    agent_id: safeText(chatViewState.selectedAgentID) || CHAT_DEFAULTS.agentID,
    user_id: CHAT_DEFAULTS.userID,
    room_id: CHAT_DEFAULTS.roomID,
    channel: "dashboard",
    limit: String(SESSION_LOOKUP_LIMIT),
    offset: "0",
  });
  return `/api/admin/chat/sessions?${params.toString()}`;
}

async function refreshAvailableAgents() {
  try {
    const params = new URLSearchParams({
      channel: "dashboard",
      user_id: CHAT_DEFAULTS.userID,
      room_id: CHAT_DEFAULTS.roomID,
    });
    const requestedAgent = safeText(chatViewState.selectedAgentID);
    if (requestedAgent) {
      params.set("agent_id", requestedAgent);
    }
    const payload = await chatViewState.apiClient.get(
      `/api/admin/agents?${params.toString()}`
    );

    const agents = Array.isArray(payload?.agents)
      ? payload.agents.map((item) => safeText(item)).filter((item) => !!item)
      : [];
    if (agents.length) {
      chatViewState.availableAgents = agents;
    }
    const active = safeText(payload?.active_agent);
    if (active) {
      chatViewState.activeAgentID = active;
    }

    const selected = safeText(payload?.selected_agent) || active;
    chatViewState.agentProfileContext =
      payload?.profile_context && typeof payload.profile_context === "object" ? payload.profile_context : null;
    chatViewState.agentGlobalConfig =
      payload?.agents_config && typeof payload.agents_config === "object" ? payload.agents_config : null;

    if (selected) {
      chatViewState.selectedAgentID = selected;
    } else if (!chatViewState.availableAgents.includes(chatViewState.selectedAgentID)) {
      chatViewState.selectedAgentID = chatViewState.availableAgents[0] || CHAT_DEFAULTS.agentID;
    }
  } catch (_error) {
    if (!chatViewState.availableAgents.length) {
      chatViewState.availableAgents = [CHAT_DEFAULTS.agentID];
    }
  }
}

function resetViewForAgentSwitch() {
  stopPolling();
  clearIdleSessionTimer();
  chatViewState.currentRunID = "";
  chatViewState.currentRunStatus = "idle";
  chatViewState.currentRunStartedAtMs = 0;
  chatViewState.currentRunLastUpdatedAt = "";
  chatViewState.currentRunLastOutput = "";
  chatViewState.currentSessionID = "";
  chatViewState.latestToolActivity = null;
  chatViewState.lastErrorSummary = "";
  chatViewState.sendError = null;
  chatViewState.debugCopyStatus = "";
  chatViewState.transcript = [];
  chatViewState.loopRisk = {
    level: "low",
    score: 0,
    reasons: [],
    repeatCount: 0,
    failureCount: 0,
    windowSize: 0,
  };
}

async function switchAgent(nextAgentID) {
  const agentID = safeText(nextAgentID) || CHAT_DEFAULTS.agentID;
  if (chatViewState.switchAgentPending) {
    return;
  }
  chatViewState.switchAgentPending = true;
  chatViewState.switchAgentError = "";
  rerenderIfActive();

  try {
    const payload = await chatViewState.apiClient.post("/api/admin/agents", {
      channel: "dashboard",
      user_id: CHAT_DEFAULTS.userID,
      room_id: CHAT_DEFAULTS.roomID,
      agent_id: agentID,
    });

    const agents = Array.isArray(payload?.agents)
      ? payload.agents.map((item) => safeText(item)).filter((item) => !!item)
      : [];
    if (agents.length) {
      chatViewState.availableAgents = agents;
    }
    chatViewState.selectedAgentID = safeText(payload?.selected_agent) || safeText(payload?.active_agent) || agentID;
    chatViewState.activeAgentID = safeText(payload?.active_agent) || chatViewState.selectedAgentID;
    chatViewState.agentProfileContext =
      payload?.profile_context && typeof payload.profile_context === "object" ? payload.profile_context : null;
    chatViewState.agentGlobalConfig =
      payload?.agents_config && typeof payload.agents_config === "object" ? payload.agents_config : null;

    resetViewForAgentSwitch();

    const sessionID = await ensureCurrentSessionID();
    if (sessionID) {
      const sessionPayload = await chatViewState.apiClient.get(pollSessionMessagesPath(sessionID));
      applySessionMessagesPayload(sessionPayload);
    }
    startIdleSessionWatch();
  } catch (err) {
    chatViewState.switchAgentError = err?.message || String(err);
  } finally {
    chatViewState.switchAgentPending = false;
    rerenderIfActive();
  }
}

function normalizeSessionTranscript(messages) {
  if (!Array.isArray(messages)) {
    return [];
  }
  return messages
    .map((item) => {
      if (!item || typeof item !== "object") {
        return null;
      }
      const role = safeText(item.role).toLowerCase();
      if (role !== "user" && role !== "assistant") {
        return null;
      }
      const content = String(item.content || "");
      if (!safeText(content)) {
        return null;
      }
      return {
        role,
        content,
        pending: false,
        ts: String(item.ts || new Date().toISOString()),
      };
    })
    .filter((item) => item !== null);
}

function syncTranscriptFromSession(messages) {
  const sessionTranscript = normalizeSessionTranscript(messages);
  if (!sessionTranscript.length) {
    return;
  }

  const pending = chatViewState.transcript.find((item) => item?.role === "assistant" && item?.pending) || null;
  chatViewState.transcript = sessionTranscript;
  if (pending && (chatViewState.sendPending || chatViewState.polling || chatViewState.streamActive)) {
    chatViewState.transcript.push(pending);
  }
}

function toToolEvent(message, index) {
  const parsed = parseMaybeJSON(message?.content);
  const toolName = firstNonEmpty(message?.tool_name, parsed?.tool, parsed?.name, "unknown.tool");
  const toolCallID = firstNonEmpty(message?.tool_call_id, parsed?.id, parsed?.tool_call_id);
  const runID = firstNonEmpty(message?.run_id, parsed?.run_id);
  const summary = firstNonEmpty(parsed?.summary, parsed?.message);
  const argsText = asDisplayText(parsed?.arguments ?? parsed?.args ?? parsed?.input ?? parsed?.params);
  const outputText = asDisplayText(parsed?.output ?? parsed?.result ?? parsed?.response);
  const errorText = asDisplayText(parsed?.error ?? parsed?.callback_error ?? parsed?.stderr);
  const status = safeText(errorText) ? "failed" : "ok";

  return {
    tool: toolName,
    toolCallID,
    runID,
    summary,
    argsText,
    outputText,
    errorText,
    status,
    ts: String(message?.ts || ""),
    index,
  };
}

function normalizeToolEvents(messages) {
  if (!Array.isArray(messages)) {
    return [];
  }
  return messages
    .map((message, index) => {
      if (!message || typeof message !== "object") {
        return null;
      }
      const role = safeText(message.role).toLowerCase();
      if (role !== "tool") {
        return null;
      }
      return toToolEvent(message, index);
    })
    .filter((item) => item !== null);
}

function buildLoopRisk(toolEvents) {
  const window = Array.isArray(toolEvents) ? toolEvents.slice(-8) : [];
  const failures = window.filter((event) => event.status === "failed");
  const repeatCounts = new Map();
  const failureSignatures = new Map();

  window.forEach((event) => {
    repeatCounts.set(event.tool, (repeatCounts.get(event.tool) || 0) + 1);
    if (event.status === "failed") {
      const signature = `${event.tool}|${compactText(event.errorText, 180)}`;
      failureSignatures.set(signature, (failureSignatures.get(signature) || 0) + 1);
    }
  });

  let repeatedToolName = "";
  let repeatedToolCount = 0;
  repeatCounts.forEach((count, toolName) => {
    if (count > repeatedToolCount) {
      repeatedToolCount = count;
      repeatedToolName = toolName;
    }
  });

  let repeatedFailureCount = 0;
  failureSignatures.forEach((count) => {
    if (count > repeatedFailureCount) {
      repeatedFailureCount = count;
    }
  });

  let score = 0;
  const reasons = [];

  if (repeatedToolCount >= 3) {
    score += 2;
    reasons.push(`${repeatedToolName || "tool"} repeated ${repeatedToolCount}x in the recent window.`);
  }
  if (repeatedToolCount >= 5) {
    score += 2;
  }
  if (failures.length >= 3) {
    score += 2;
    reasons.push(`${failures.length} recent tool failures detected.`);
  }
  if (window.length >= 4 && failures.length / window.length >= 0.5) {
    score += 1;
  }
  if (repeatedFailureCount >= 2) {
    score += 2;
    reasons.push("Same tool error repeated across recent attempts.");
  }
  if (repeatedFailureCount >= 3) {
    score += 1;
  }

  const level = score >= 6 ? "high" : score >= 3 ? "medium" : "low";
  return {
    level,
    score,
    reasons,
    repeatCount: repeatedToolCount,
    failureCount: failures.length,
    windowSize: window.length,
  };
}

function replacePendingAssistant(message) {
  for (let index = chatViewState.transcript.length - 1; index >= 0; index -= 1) {
    const item = chatViewState.transcript[index];
    if (item?.role === "assistant" && item?.pending) {
      chatViewState.transcript[index] = {
        role: "assistant",
        content: message,
        pending: false,
        ts: new Date().toISOString(),
      };
      return;
    }
  }
  chatViewState.transcript.push({ role: "assistant", content: message, pending: false, ts: new Date().toISOString() });
}

function updatePendingAssistant(message) {
  const text = typeof message === "string" ? message : String(message || "");
  if (!safeText(text)) {
    return;
  }
  for (let index = chatViewState.transcript.length - 1; index >= 0; index -= 1) {
    const item = chatViewState.transcript[index];
    if (item?.role === "assistant" && item?.pending) {
      chatViewState.transcript[index] = {
        role: "assistant",
        content: text,
        pending: true,
        ts: item.ts || new Date().toISOString(),
      };
      return;
    }
  }
}

function pushUserAndPendingAssistant(message) {
  const now = new Date().toISOString();
  chatViewState.transcript.push({ role: "user", content: message, pending: false, ts: now });
  chatViewState.transcript.push({ role: "assistant", content: "Thinking...", pending: true, ts: now });
}

function clearStreamingRenderTimer() {
  if (chatViewState.streamRenderTimer) {
    window.clearTimeout(chatViewState.streamRenderTimer);
    chatViewState.streamRenderTimer = 0;
  }
}

function scheduleStreamingRender() {
  if (chatViewState.streamRenderTimer) {
    return;
  }
  chatViewState.streamRenderTimer = window.setTimeout(() => {
    chatViewState.streamRenderTimer = 0;
    rerenderIfActive({ skipIfUnchanged: true });
  }, STREAM_RENDER_THROTTLE_MS);
}

function clearTimers() {
  if (chatViewState.runPollTimer) {
    window.clearTimeout(chatViewState.runPollTimer);
    chatViewState.runPollTimer = 0;
  }
  if (chatViewState.sessionPollTimer) {
    window.clearTimeout(chatViewState.sessionPollTimer);
    chatViewState.sessionPollTimer = 0;
  }
}

function resetStreamingState(options = {}) {
  const { resetLastEventID = false, keepStreamingText = false } = options;
  clearStreamingRenderTimer();
  chatViewState.streamActive = false;
  if (!keepStreamingText) {
    chatViewState.currentStreamingText = "";
  }
  chatViewState.currentStreamRunID = "";
  if (chatViewState.streamAbortController) {
    chatViewState.streamAbortController.abort();
    chatViewState.streamAbortController = null;
  }
  if (resetLastEventID) {
    chatViewState.streamLastEventID = 0;
  }
}

function enableStreamPollingMode() {
  if (!chatViewState.polling) {
    return;
  }
  chatViewState.runPollingEnabled = false;
  chatViewState.sessionPollIntervalMS = STREAM_SESSION_POLL_MS;
  if (chatViewState.runPollTimer) {
    window.clearTimeout(chatViewState.runPollTimer);
    chatViewState.runPollTimer = 0;
  }
  if (chatViewState.currentSessionID) {
    scheduleSessionPoll(chatViewState.pollToken, true);
  }
}

function restorePollingFallback(runID) {
  chatViewState.runPollingEnabled = true;
  chatViewState.sessionPollIntervalMS = SESSION_POLL_MS;

  const activeRunID = safeText(runID) || safeText(chatViewState.currentRunID);
  if (!activeRunID || isTerminalStatus(chatViewState.currentRunStatus)) {
    return;
  }
  if (!chatViewState.polling) {
    startPolling({ runID: activeRunID, sessionID: chatViewState.currentSessionID });
    return;
  }
  scheduleRunPoll(chatViewState.pollToken, true);
  if (chatViewState.currentSessionID) {
    scheduleSessionPoll(chatViewState.pollToken, true);
  }
}

function clearIdleSessionTimer() {
  if (chatViewState.idleSessionTimer) {
    window.clearTimeout(chatViewState.idleSessionTimer);
    chatViewState.idleSessionTimer = 0;
  }
}

function stopPolling() {
  chatViewState.polling = false;
  chatViewState.pollToken += 1;
  chatViewState.runPollInFlight = false;
  chatViewState.sessionPollInFlight = false;
  chatViewState.runPollingEnabled = true;
  chatViewState.sessionPollIntervalMS = SESSION_POLL_MS;
  clearTimers();
}

function isChatRouteActive() {
  if (!chatViewState.store) {
    return false;
  }
  return chatViewState.store.getState().route === "/chat";
}

function rerenderIfActive(options = {}) {
  const container = chatViewState.container;
  if (!container || !container.isConnected || !isChatRouteActive()) {
    return;
  }
  // Skip re-render if content hasn't changed (avoids unnecessary DOM rebuilds)
  if (options.skipIfUnchanged && transcriptFingerprint() === lastRenderedFingerprint) {
    return;
  }
  renderChatPage();
}

function scheduleRunPoll(token, immediate = false) {
  if (!chatViewState.polling || token !== chatViewState.pollToken || !chatViewState.runPollingEnabled) {
    return;
  }
  if (chatViewState.runPollTimer) {
    window.clearTimeout(chatViewState.runPollTimer);
  }
  chatViewState.runPollTimer = window.setTimeout(() => {
    void pollRunOnce(token);
  }, immediate ? 0 : RUN_POLL_MS);
}

function scheduleSessionPoll(token, immediate = false) {
  if (!chatViewState.polling || token !== chatViewState.pollToken) {
    return;
  }
  if (!safeText(chatViewState.currentSessionID)) {
    return;
  }
  if (chatViewState.sessionPollTimer) {
    window.clearTimeout(chatViewState.sessionPollTimer);
  }
  chatViewState.sessionPollTimer = window.setTimeout(() => {
    void pollSessionMessagesOnce(token);
  }, immediate ? 0 : chatViewState.sessionPollIntervalMS);
}

function startPolling({ runID, sessionID }) {
  const nextRunID = safeText(runID);
  const nextSessionID = safeText(sessionID);
  if (!nextRunID) {
    return;
  }

  const sameRun = chatViewState.polling && chatViewState.currentRunID === nextRunID;
  chatViewState.currentRunID = nextRunID;
  if (nextSessionID) {
    chatViewState.currentSessionID = nextSessionID;
  }
  if (!sameRun) {
    chatViewState.currentRunStartedAtMs = Date.now();
  }

  if (sameRun) {
    if (chatViewState.currentRunStartedAtMs <= 0) {
      chatViewState.currentRunStartedAtMs = Date.now();
    }
    if (nextSessionID) {
      scheduleSessionPoll(chatViewState.pollToken, true);
    }
    if (chatViewState.runPollingEnabled) {
      scheduleRunPoll(chatViewState.pollToken, true);
    }
    return;
  }

  stopPolling();
  chatViewState.currentRunID = nextRunID;
  if (nextSessionID) {
    chatViewState.currentSessionID = nextSessionID;
  }
  chatViewState.currentRunStatus = "running";
  chatViewState.currentRunStartedAtMs = Date.now();
  chatViewState.runPollingEnabled = true;
  chatViewState.sessionPollIntervalMS = SESSION_POLL_MS;
  chatViewState.polling = true;
  chatViewState.pollToken += 1;

  const token = chatViewState.pollToken;
  scheduleRunPoll(token, true);
  if (chatViewState.currentSessionID) {
    scheduleSessionPoll(token, true);
  }
}

async function ensureCurrentSessionID() {
  if (safeText(chatViewState.currentSessionID)) {
    return chatViewState.currentSessionID;
  }
  if (chatViewState.sessionBootstrapInFlight) {
    return "";
  }

  chatViewState.sessionBootstrapInFlight = true;
  try {
    const payload = await chatViewState.apiClient.get(listSessionsPath());
    const sessions = Array.isArray(payload?.sessions) ? payload.sessions : [];
    const first = sessions[0] || null;
    const sessionID = safeText(first?.session_id);
    if (sessionID) {
      chatViewState.currentSessionID = sessionID;
    }
    return sessionID;
  } catch (_err) {
    return "";
  } finally {
    chatViewState.sessionBootstrapInFlight = false;
  }
}

function parseSSEBlock(rawBlock) {
  const lines = String(rawBlock || "").replace(/\r/g, "").split("\n");
  const dataLines = [];
  let eventName = "message";
  let eventID = "";

  lines.forEach((line) => {
    if (!line || line.startsWith(":")) {
      return;
    }
    const separator = line.indexOf(":");
    const key = separator >= 0 ? line.slice(0, separator).trim() : line.trim();
    let value = separator >= 0 ? line.slice(separator + 1) : "";
    if (value.startsWith(" ")) {
      value = value.slice(1);
    }

    if (key === "event") {
      eventName = safeText(value) || "message";
      return;
    }
    if (key === "id") {
      eventID = safeText(value);
      return;
    }
    if (key === "data") {
      dataLines.push(value);
    }
  });

  if (!dataLines.length) {
    return null;
  }
  return {
    eventName,
    eventID,
    data: dataLines.join("\n"),
  };
}

function noteStreamEventID(rawID) {
  const parsed = Number(rawID);
  if (!Number.isFinite(parsed)) {
    return;
  }
  const id = Math.floor(parsed);
  if (id > chatViewState.streamLastEventID) {
    chatViewState.streamLastEventID = id;
  }
}

function handleRunStreamStatus(eventEnvelope) {
  const status = safeText(eventEnvelope?.data?.status || eventEnvelope?.status).toLowerCase();
  if (status) {
    chatViewState.currentRunStatus = status;
  }
  const sessionID = safeText(eventEnvelope?.data?.session_id);
  if (sessionID) {
    chatViewState.currentSessionID = sessionID;
  }
  const ts = safeText(eventEnvelope?.ts);
  chatViewState.currentRunLastUpdatedAt = ts || new Date().toISOString();
  if (chatViewState.currentRunStartedAtMs <= 0 && status && !isTerminalStatus(status)) {
    chatViewState.currentRunStartedAtMs = Date.now();
  }
  scheduleStreamingRender();
  return false;
}

function handleRunStreamToolEnd(eventEnvelope) {
  const payload = eventEnvelope?.data || {};
  const event = {
    tool: firstNonEmpty(payload.tool, payload.name, "unknown.tool"),
    toolCallID: firstNonEmpty(payload.tool_call_id, payload.id),
    runID: firstNonEmpty(eventEnvelope?.run_id, chatViewState.currentRunID),
    summary: firstNonEmpty(payload.summary, payload.message),
    argsText: asDisplayText(payload.arguments ?? payload.args ?? payload.params),
    outputText: asDisplayText(payload.output ?? payload.result),
    errorText: asDisplayText(payload.error ?? payload.callback_error),
    status: safeText(payload.error) ? "failed" : "ok",
    ts: String(eventEnvelope?.ts || new Date().toISOString()),
    index: chatViewState.streamToolEvents.length,
  };

  chatViewState.streamToolEvents.push(event);
  if (chatViewState.streamToolEvents.length > 32) {
    chatViewState.streamToolEvents = chatViewState.streamToolEvents.slice(-32);
  }
  chatViewState.latestToolActivity = event;
  chatViewState.loopRisk = buildLoopRisk(chatViewState.streamToolEvents);

  if (!safeText(chatViewState.currentStreamingText)) {
    const detail = compactText(firstNonEmpty(event.summary, event.errorText, event.outputText, event.argsText), 180);
    if (detail) {
      updatePendingAssistant(`Working... ${event.tool}: ${detail}`);
    } else {
      updatePendingAssistant(`Working... ${event.tool}`);
    }
  }

  if (event.status === "failed" && safeText(event.errorText)) {
    chatViewState.lastErrorSummary = compactText(event.errorText, 280);
    updateLastError(
      chatViewState.store,
      toErrorPayload(
        "chat.stream_tool",
        { message: event.errorText },
        {
          run_id: event.runID || chatViewState.currentRunID,
          session_id: chatViewState.currentSessionID,
          tool: event.tool,
        }
      )
    );
  }

  scheduleStreamingRender();
  return false;
}

function handleRunStreamModelText(eventEnvelope) {
  const payload = eventEnvelope?.data || {};
  const text =
    typeof payload.text === "string"
      ? payload.text
      : payload.text === undefined || payload.text === null
      ? ""
      : asDisplayText(payload.text);
  if (!text) {
    return false;
  }

  if (payload.partial === false) {
    chatViewState.currentStreamingText = text;
  } else {
    chatViewState.currentStreamingText += text;
  }

  if (chatViewState.currentStreamingText) {
    updatePendingAssistant(chatViewState.currentStreamingText);
  }
  scheduleStreamingRender();
  return false;
}

function handleRunStreamCompleted(eventEnvelope) {
  const payload = eventEnvelope?.data || {};
  chatViewState.currentRunStatus = "completed";
  chatViewState.currentRunLastUpdatedAt = safeText(eventEnvelope?.ts) || new Date().toISOString();
  chatViewState.currentRunLastOutput = String(payload.output || chatViewState.currentStreamingText || "");
  const message =
    chatViewState.currentRunLastOutput ||
    "Run completed without assistant output. Open trace or tool activity for details.";
  replacePendingAssistant(message);
  chatViewState.currentStreamingText = "";
  stopPolling();
  resetStreamingState();
  rerenderIfActive();
  return true;
}

function handleRunStreamFailed(eventEnvelope) {
  const payload = eventEnvelope?.data || {};
  const message = firstNonEmpty(payload.error, eventEnvelope?.error, "Run failed.");
  chatViewState.currentRunStatus = "failed";
  chatViewState.currentRunLastUpdatedAt = safeText(eventEnvelope?.ts) || new Date().toISOString();
  replacePendingAssistant(`Error: ${message}`);
  chatViewState.lastErrorSummary = compactText(message, 280);
  updateLastError(
    chatViewState.store,
    toErrorPayload("chat.stream_failed", { message }, { run_id: chatViewState.currentRunID })
  );
  chatViewState.currentStreamingText = "";
  stopPolling();
  resetStreamingState();
  rerenderIfActive();
  return true;
}

function handleRunStreamEvent(rawType, eventEnvelope) {
  const type = safeText(rawType).toLowerCase();
  switch (type) {
    case "status":
      return handleRunStreamStatus(eventEnvelope);
    case "tool_end":
      return handleRunStreamToolEnd(eventEnvelope);
    case "model_text":
      return handleRunStreamModelText(eventEnvelope);
    case "completed":
      return handleRunStreamCompleted(eventEnvelope);
    case "failed":
      return handleRunStreamFailed(eventEnvelope);
    case "heartbeat":
      return false;
    default:
      return false;
  }
}

function processSSEBlock(rawBlock, runID) {
  const block = parseSSEBlock(rawBlock);
  if (!block) {
    return false;
  }

  if (block.eventID) {
    noteStreamEventID(block.eventID);
  }

  const dataText = String(block.data || "");
  if (!safeText(dataText)) {
    return false;
  }
  if (safeText(dataText) === "[DONE]") {
    return true;
  }

  const parsedPayload = parseMaybeJSON(dataText);
  const envelope =
    parsedPayload && typeof parsedPayload === "object"
      ? parsedPayload
      : {
          type: block.eventName,
          run_id: runID,
          data: { text: dataText },
        };

  if (envelope?.id !== undefined && envelope?.id !== null) {
    noteStreamEventID(envelope.id);
  }

  const eventType = safeText(envelope?.type || block.eventName || "message");
  return handleRunStreamEvent(eventType, envelope);
}

async function consumeRunEventStream(readableStream, runID, signal) {
  const reader = readableStream.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  while (true) {
    const { value, done } = await reader.read();
    if (done) {
      break;
    }
    if (signal.aborted || !isChatRouteActive()) {
      throw new Error("stream aborted");
    }

    buffer += decoder.decode(value, { stream: true }).replace(/\r/g, "");
    let boundary = buffer.indexOf("\n\n");
    while (boundary >= 0) {
      const block = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);
      if (processSSEBlock(block, runID)) {
        return true;
      }
      boundary = buffer.indexOf("\n\n");
    }
  }

  const tail = decoder.decode();
  if (tail) {
    buffer += tail.replace(/\r/g, "");
  }
  if (safeText(buffer) && processSSEBlock(buffer, runID)) {
    return true;
  }
  return false;
}

async function connectRunEventStream(runID) {
  const targetRunID = safeText(runID);
  if (!targetRunID || !chatViewState.apiClient || !isChatRouteActive()) {
    return;
  }
  if (chatViewState.streamActive && chatViewState.currentStreamRunID === targetRunID) {
    return;
  }

  resetStreamingState({ keepStreamingText: true });
  const controller = new AbortController();
  chatViewState.streamAbortController = controller;
  chatViewState.streamActive = true;
  chatViewState.currentStreamRunID = targetRunID;

  try {
    const token = safeText(chatViewState.apiClient.resolveBearerToken());
    const headers = new Headers({ Accept: "text/event-stream" });
    if (token) {
      headers.set("Authorization", `Bearer ${token}`);
    }
    if (chatViewState.streamLastEventID > 0) {
      headers.set("Last-Event-ID", String(chatViewState.streamLastEventID));
    }

    const response = await window.fetch(`/v1/runs/events/${encodeURIComponent(targetRunID)}`, {
      method: "GET",
      headers,
      signal: controller.signal,
      cache: "no-store",
    });

    if (!response.ok) {
      throw new Error(`stream request failed (${response.status})`);
    }
    if (!response.body || typeof response.body.getReader !== "function") {
      throw new Error("streaming not supported by this browser/runtime");
    }

    enableStreamPollingMode();
    const terminal = await consumeRunEventStream(response.body, targetRunID, controller.signal);
    if (terminal || isTerminalStatus(chatViewState.currentRunStatus)) {
      return;
    }
    throw new Error("stream disconnected");
  } catch (err) {
    if (controller.signal.aborted) {
      return;
    }
    chatViewState.lastErrorSummary = compactText(err?.message || String(err), 280);
    updateLastError(chatViewState.store, toErrorPayload("chat.stream", err, { run_id: targetRunID }));
  } finally {
    const sameRun = chatViewState.currentStreamRunID === targetRunID;
    if (sameRun) {
      chatViewState.streamActive = false;
      chatViewState.currentStreamRunID = "";
      if (chatViewState.streamAbortController === controller) {
        chatViewState.streamAbortController = null;
      }
      clearStreamingRenderTimer();
    }
    if (!controller.signal.aborted && !isTerminalStatus(chatViewState.currentRunStatus) && isChatRouteActive()) {
      restorePollingFallback(targetRunID);
    }
    rerenderIfActive({ skipIfUnchanged: true });
  }
}

function ensureRouteWatcher() {
  if (chatViewState.routeUnsubscribe || !chatViewState.store) {
    return;
  }
  chatViewState.routeUnsubscribe = chatViewState.store.subscribe((state) => {
    const route = state?.route || "";
    if (route === "/chat") {
      return;
    }
    resetStreamingState();
    stopPolling();
    clearIdleSessionTimer();
  });
}

function scheduleIdleSessionPoll(immediate = false) {
  if (chatViewState.idleSessionTimer) {
    window.clearTimeout(chatViewState.idleSessionTimer);
  }
  chatViewState.idleSessionTimer = window.setTimeout(() => {
    void pollIdleSessionOnce();
  }, immediate ? 0 : SESSION_POLL_MS);
}

function startIdleSessionWatch() {
  if (chatViewState.idleSessionTimer) {
    return;
  }
  scheduleIdleSessionPoll(true);
}

async function pollIdleSessionOnce() {
  if (!isChatRouteActive()) {
    clearIdleSessionTimer();
    return;
  }
  if (chatViewState.polling) {
    scheduleIdleSessionPoll(false);
    return;
  }
  if (chatViewState.idleSessionInFlight) {
    scheduleIdleSessionPoll(false);
    return;
  }

  chatViewState.idleSessionInFlight = true;
  try {
    let sessionID = safeText(chatViewState.currentSessionID);
    if (!sessionID) {
      sessionID = await ensureCurrentSessionID();
    }
    if (sessionID) {
      const payload = await chatViewState.apiClient.get(pollSessionMessagesPath(sessionID));
      applySessionMessagesPayload(payload);
      rerenderIfActive({ skipIfUnchanged: true });
    }
  } finally {
    chatViewState.idleSessionInFlight = false;
    scheduleIdleSessionPoll(false);
  }
}

async function pollRunOnce(token) {
  if (!isChatRouteActive()) {
    stopPolling();
    return;
  }
  if (!chatViewState.polling || token !== chatViewState.pollToken) {
    return;
  }
  if (!chatViewState.runPollingEnabled) {
    return;
  }
  if (chatViewState.runPollInFlight) {
    scheduleRunPoll(token, false);
    return;
  }
  const runID = safeText(chatViewState.currentRunID);
  if (!runID) {
    stopPolling();
    return;
  }

  chatViewState.runPollInFlight = true;
  try {
    const run = await chatViewState.apiClient.get(`/v1/runs/${encodeURIComponent(runID)}`);
    const status = safeText(run?.status).toLowerCase() || "unknown";
    chatViewState.currentRunStatus = status;
    chatViewState.currentRunLastUpdatedAt = String(run?.updated_at || "");
    chatViewState.currentRunLastOutput = String(run?.output || "");

    const discoveredSessionID = safeText(run?.session_id);
    if (discoveredSessionID && discoveredSessionID !== chatViewState.currentSessionID) {
      chatViewState.currentSessionID = discoveredSessionID;
      scheduleSessionPoll(token, true);
    }

    if (safeText(run?.error)) {
      chatViewState.lastErrorSummary = compactText(run.error, 280);
      updateLastError(
        chatViewState.store,
        toErrorPayload("chat.run", { message: run.error }, { run_id: runID, status: run?.status || "" })
      );
    }

    if (isTerminalStatus(status)) {
      if (status === "completed") {
        replacePendingAssistant(
          safeText(run?.output) || "Run completed without assistant output. Open trace or tool activity for details."
        );
      } else if (status === "failed") {
        const message = safeText(run?.error) || "Run failed.";
        replacePendingAssistant(`Error: ${message}`);
        chatViewState.lastErrorSummary = compactText(message, 280);
      } else if (status === "canceled") {
        replacePendingAssistant("Run canceled.");
      }
      stopPolling();
      resetStreamingState();
      rerenderIfActive();
      return;
    }

    const elapsedSeconds =
      chatViewState.currentRunStartedAtMs > 0
        ? Math.max(1, Math.floor((Date.now() - chatViewState.currentRunStartedAtMs) / 1000))
        : 0;
    let progress = `Still working on run ${runID} (status: ${status}`;
    if (elapsedSeconds > 0) {
      progress += `, ${elapsedSeconds}s elapsed`;
    }
    progress += ").";
    const latest = chatViewState.latestToolActivity;
    if (latest) {
      const detail = compactText(firstNonEmpty(latest.summary, latest.errorText, latest.outputText, latest.argsText), 180);
      if (detail) {
        progress += ` Latest tool activity: ${latest.tool} - ${detail}`;
      } else if (latest.tool) {
        progress += ` Latest tool activity: ${latest.tool}.`;
      }
    }
    updatePendingAssistant(progress);
  } catch (err) {
    chatViewState.lastErrorSummary = compactText(err?.message || String(err), 280);
    updateLastError(chatViewState.store, toErrorPayload("chat.run_poll", err, { run_id: chatViewState.currentRunID }));
  } finally {
    chatViewState.runPollInFlight = false;
  }

  rerenderIfActive();
  scheduleRunPoll(token, false);
}

function applySessionMessagesPayload(payload) {
  const messages = Array.isArray(payload?.messages) ? payload.messages : [];
  syncTranscriptFromSession(messages);

  const toolEvents = normalizeToolEvents(messages);
  if (toolEvents.length) {
    chatViewState.streamToolEvents = toolEvents.slice(-32);
  }
  const mergedToolEvents = chatViewState.streamToolEvents.length ? chatViewState.streamToolEvents : toolEvents;
  const latest = mergedToolEvents.length ? mergedToolEvents[mergedToolEvents.length - 1] : null;
  chatViewState.latestToolActivity = latest;
  chatViewState.loopRisk = buildLoopRisk(mergedToolEvents);

  const latestFailed = mergedToolEvents
    .slice()
    .reverse()
    .find((event) => event.status === "failed" && safeText(event.errorText));
  if (latestFailed) {
    chatViewState.lastErrorSummary = compactText(latestFailed.errorText, 280);
    updateLastError(
      chatViewState.store,
      toErrorPayload(
        "chat.tool_activity",
        { message: latestFailed.errorText },
        {
          run_id: latestFailed.runID || chatViewState.currentRunID,
          session_id: safeText(payload?.session_id) || chatViewState.currentSessionID,
          tool: latestFailed.tool,
        }
      )
    );
  }
}

async function pollSessionMessagesOnce(token) {
  if (!isChatRouteActive()) {
    stopPolling();
    return;
  }
  if (!chatViewState.polling || token !== chatViewState.pollToken) {
    return;
  }
  if (chatViewState.sessionPollInFlight) {
    scheduleSessionPoll(token, false);
    return;
  }
  const sessionID = safeText(chatViewState.currentSessionID);
  if (!sessionID) {
    return;
  }

	chatViewState.sessionPollInFlight = true;
	try {
		const payload = await chatViewState.apiClient.get(pollSessionMessagesPath(sessionID));
		applySessionMessagesPayload(payload);
	} catch (err) {
    chatViewState.lastErrorSummary = compactText(err?.message || String(err), 280);
    updateLastError(
      chatViewState.store,
      toErrorPayload("chat.session_poll", err, { session_id: sessionID, run_id: chatViewState.currentRunID })
    );
  } finally {
    chatViewState.sessionPollInFlight = false;
  }

  rerenderIfActive();
  scheduleSessionPoll(token, false);
}

async function sendMessage() {
  if (chatViewState.sendPending) {
    return;
  }
  const message = safeText(chatViewState.draft);
  if (!message) {
    return;
  }

  chatViewState.sendPending = true;
  chatViewState.lastPrompt = message;
  chatViewState.sendError = null;
  chatViewState.debugCopyStatus = "";
  pushUserAndPendingAssistant(message);
  chatViewState.draft = "";
  rerenderIfActive();

  try {
    const payload = await chatViewState.apiClient.post("/v1/chat/messages", {
      user_id: CHAT_DEFAULTS.userID,
      room_id: CHAT_DEFAULTS.roomID,
      agent_id: safeText(chatViewState.selectedAgentID) || CHAT_DEFAULTS.agentID,
      message,
    });

    const runID = safeText(payload?.id);
    const sessionID = safeText(payload?.session_id);
    if (sessionID) {
      chatViewState.currentSessionID = sessionID;
    }

    if (runID) {
      const normalizedStatus = safeText(payload?.status).toLowerCase() || "queued";
      chatViewState.currentRunID = runID;
      chatViewState.currentRunStatus = normalizedStatus;
      chatViewState.currentRunLastUpdatedAt = "";
      chatViewState.currentRunLastOutput = "";
      chatViewState.currentRunStartedAtMs = Date.now();
      chatViewState.streamLastEventID = 0;
      chatViewState.currentStreamingText = "";
      chatViewState.streamToolEvents = [];
      updatePendingAssistant(
        safeText(payload?.response) ||
          `Working on it now. Run ${runID} is ${normalizedStatus || "queued"}. I will follow up when it completes or fails.`
      );
      startPolling({ runID, sessionID });
      void connectRunEventStream(runID);
    } else {
      const directResponse = safeText(payload?.response);
      replacePendingAssistant(directResponse || "Request accepted.");
      chatViewState.currentRunStatus = "idle";
      chatViewState.currentRunStartedAtMs = 0;
      resetStreamingState({ resetLastEventID: true });
    }
  } catch (err) {
    const messageText = err?.message || String(err);
    chatViewState.sendError = messageText;
    chatViewState.lastErrorSummary = compactText(messageText, 280);
    replacePendingAssistant(`Error: ${messageText}`);
    updateLastError(chatViewState.store, toErrorPayload("chat.send", err));
  } finally {
    chatViewState.sendPending = false;
    rerenderIfActive();
  }
}

function renderChatPage() {
  const container = chatViewState.container;
  if (!container) {
    return;
  }

  // Save focus state before destroying DOM
  const activeEl = document.activeElement;
  const hadInputFocus = activeEl && activeEl.classList.contains("chat-input");
  let selStart = 0;
  let selEnd = 0;
  if (hadInputFocus) {
    selStart = activeEl.selectionStart || 0;
    selEnd = activeEl.selectionEnd || 0;
  }

  const previousTranscript = container.querySelector(".chat-transcript");
  if (previousTranscript) {
    chatViewState.transcriptPinned = isTranscriptNearBottom(previousTranscript);
    chatViewState.transcriptScrollTop = previousTranscript.scrollTop;
  }
  container.innerHTML = "";

  const heading = document.createElement("h2");
  heading.textContent = "Chat";

  const note = document.createElement("p");
  note.className = "muted";
  note.textContent = "Send prompts from dashboard defaults and watch live run/tool activity while the agent is in-flight.";

  const page = document.createElement("section");
  page.className = "chat-page";

  const transcriptPane = document.createElement("section");
  transcriptPane.className = "chat-transcript-pane";
  const transcriptTitle = document.createElement("h3");
  transcriptTitle.textContent = "Transcript";
  const transcript = document.createElement("div");
  transcript.className = "chat-transcript";
  transcript.addEventListener("scroll", () => {
    chatViewState.transcriptPinned = isTranscriptNearBottom(transcript);
    chatViewState.transcriptScrollTop = transcript.scrollTop;
  });

  if (!chatViewState.transcript.length) {
    const empty = document.createElement("p");
    empty.className = "muted";
    empty.textContent = "No messages yet. Send a prompt to start.";
    transcript.append(empty);
  } else {
    chatViewState.transcript.forEach((item) => {
      if (!item || (item.role !== "user" && item.role !== "assistant")) {
        return;
      }
      const message = document.createElement("article");
      message.className = `chat-message chat-${item.role}`;
      if (item.pending) {
        message.classList.add("pending");
      }
      if (item.pending && item.role === "assistant" && chatViewState.streamActive) {
        message.classList.add("streaming");
      }

      const meta = document.createElement("p");
      meta.className = "chat-message-meta";
      meta.textContent = `${item.role} · ${formatDateTime(item.ts)}`;

      const body = document.createElement("pre");
      body.className = "chat-message-content";
      body.textContent =
        item.role === "assistant"
          ? formatAssistantContentForDisplay(item.content)
          : String(item.content || "");

      message.append(meta, body);
      if (item.pending && item.role === "assistant" && chatViewState.latestToolActivity) {
        const toolHint = document.createElement("p");
        const latest = chatViewState.latestToolActivity;
        const detail = compactText(
          firstNonEmpty(latest.summary, latest.errorText, latest.outputText, latest.argsText),
          150
        );
        toolHint.className = `chat-tool-indicator ${latest.status === "failed" ? "failed" : "ok"}`;
        toolHint.textContent = detail
          ? `Latest tool: ${latest.tool} · ${detail}`
          : `Latest tool: ${latest.tool}`;
        message.append(toolHint);
      }
      transcript.append(message);
    });
  }

  const composer = document.createElement("form");
  composer.className = "chat-composer";
  composer.addEventListener("submit", (event) => {
    event.preventDefault();
    void sendMessage();
  });

  const input = document.createElement("textarea");
  input.className = "chat-input";
  input.placeholder = "Ask the agent to investigate, summarize, or run a workflow...";
  input.value = chatViewState.draft;
  input.rows = 3;
  input.addEventListener("input", () => {
    chatViewState.draft = input.value;
  });

  const composerActions = document.createElement("div");
  composerActions.className = "chat-composer-actions";
  const defaultsMeta = document.createElement("span");
  defaultsMeta.className = "muted";
  defaultsMeta.textContent = `Context: ${CHAT_DEFAULTS.userID} / ${CHAT_DEFAULTS.roomID} / ${safeText(chatViewState.selectedAgentID) || CHAT_DEFAULTS.agentID}`;

  const agentPicker = document.createElement("select");
  agentPicker.className = "settings-select";
  chatViewState.availableAgents.forEach((agentID) => {
    const option = document.createElement("option");
    option.value = agentID;
    option.textContent = agentID;
    option.selected = agentID === (safeText(chatViewState.selectedAgentID) || CHAT_DEFAULTS.agentID);
    agentPicker.append(option);
  });
  agentPicker.addEventListener("change", () => {
    void switchAgent(agentPicker.value);
  });
  agentPicker.disabled = chatViewState.switchAgentPending || chatViewState.sendPending;

  const refreshAgentsButton = document.createElement("button");
  refreshAgentsButton.type = "button";
  refreshAgentsButton.className = "layout-toggle";
  refreshAgentsButton.textContent = "Refresh agents";
  refreshAgentsButton.addEventListener("click", () => {
    void (async () => {
      await refreshAvailableAgents();
      rerenderIfActive();
    })();
  });
  refreshAgentsButton.disabled = chatViewState.switchAgentPending;

  const stopPollingButton = document.createElement("button");
  stopPollingButton.type = "button";
  stopPollingButton.className = "layout-toggle";
  stopPollingButton.textContent = "Stop polling";
  stopPollingButton.disabled = !chatViewState.polling;
  stopPollingButton.addEventListener("click", () => {
    stopPolling();
    chatViewState.currentRunStatus = "paused";
    replacePendingAssistant("Polling stopped by user. Run may still be executing in the backend.");
    chatViewState.debugCopyStatus = "Polling stopped.";
    rerenderIfActive();
  });

  const retryButton = document.createElement("button");
  retryButton.type = "button";
  retryButton.className = "layout-toggle";
  retryButton.textContent = "Retry";
  retryButton.disabled = chatViewState.sendPending || !safeText(chatViewState.lastPrompt);
  retryButton.addEventListener("click", () => {
    retryLastPrompt();
  });

  const sendButton = document.createElement("button");
  sendButton.type = "submit";
  sendButton.className = "chat-send-button";
  sendButton.textContent = chatViewState.sendPending ? "Sending..." : "Send";
  sendButton.disabled = chatViewState.sendPending;
  composerActions.append(defaultsMeta, agentPicker, refreshAgentsButton, stopPollingButton, retryButton, sendButton);

  if (chatViewState.sendError) {
    const sendError = document.createElement("p");
    sendError.className = "chat-send-error";
    sendError.textContent = `Send failed: ${chatViewState.sendError}`;
    composer.append(input, composerActions, sendError);
  } else {
    composer.append(input, composerActions);
  }

  if (chatViewState.switchAgentError) {
    const switchError = document.createElement("p");
    switchError.className = "chat-send-error";
    switchError.textContent = `Agent switch failed: ${chatViewState.switchAgentError}`;
    composer.append(switchError);
  }

  if (chatViewState.debugCopyStatus) {
    const debugStatus = document.createElement("p");
    debugStatus.className = "muted";
    debugStatus.textContent = chatViewState.debugCopyStatus;
    composer.append(debugStatus);
  }

  transcriptPane.append(transcriptTitle, transcript, composer);

  const activityPane = document.createElement("aside");
  activityPane.className = "chat-activity-pane";
  const activityTitle = document.createElement("h3");
  activityTitle.textContent = "Live Activity";

  const agentControlCard = document.createElement("section");
  agentControlCard.className = "chat-activity-card";
  const agentControlTitle = document.createElement("h4");
  agentControlTitle.textContent = "Agent Control";
  const selectedMeta = document.createElement("p");
  selectedMeta.className = "chat-activity-meta";
  selectedMeta.textContent = `Selected: ${safeText(chatViewState.selectedAgentID) || CHAT_DEFAULTS.agentID} · Active pointer: ${safeText(chatViewState.activeAgentID) || "-"}`;
  const profileContext = chatViewState.agentProfileContext || {};
  const profileInfo = document.createElement("p");
  profileInfo.className = "muted";
  const profileEnabled =
    typeof profileContext.enabled === "boolean" ? String(profileContext.enabled) : "(inherit true)";
  const profileSelfImprovement =
    typeof profileContext.self_improvement === "boolean" ? String(profileContext.self_improvement) : "false";
  const hasProfile = Boolean(profileContext.exists);
  profileInfo.textContent = `Profile: ${hasProfile ? "explicit" : "inherited"} · enabled=${profileEnabled} · self_improvement=${profileSelfImprovement}`;
  const modelInfo = document.createElement("p");
  modelInfo.className = "muted";
  const provider = safeText(profileContext.model_provider);
  const name = safeText(profileContext.model_name);
  const maxTokens = Number(profileContext.model_max_tokens) || 0;
  if (provider || name || maxTokens > 0) {
    modelInfo.textContent = `Model override: ${provider || "(provider unset)"} / ${name || "(name unset)"}${maxTokens > 0 ? ` · max_tokens ${maxTokens}` : ""}`;
  } else {
    modelInfo.textContent = "Model override: none (uses global model).";
  }
  const globalInfo = document.createElement("p");
  globalInfo.className = "muted";
  const globalConfig = chatViewState.agentGlobalConfig || {};
  globalInfo.textContent = `Global: model_overrides=${String(Boolean(globalConfig.allow_agent_model_overrides))} · inter_agent_messaging=${String(Boolean(globalConfig.allow_inter_agent_messaging))} · self_improvement_global=${String(Boolean(globalConfig.self_improvement_enabled))}`;
  const openSettingsButton = document.createElement("button");
  openSettingsButton.type = "button";
  openSettingsButton.className = "layout-toggle";
  openSettingsButton.textContent = "Edit profile in Settings";
  openSettingsButton.addEventListener("click", () => {
    window.location.hash = settingsProfileHash(chatViewState.selectedAgentID);
  });
  agentControlCard.append(agentControlTitle, selectedMeta, profileInfo, modelInfo, globalInfo, openSettingsButton);

  const runID = safeText(chatViewState.currentRunID);
  const status = safeText(chatViewState.currentRunStatus) || "idle";
  const sessionID = safeText(chatViewState.currentSessionID);
  const updatedAt = safeText(chatViewState.currentRunLastUpdatedAt);

  const runMeta = document.createElement("p");
  runMeta.className = "chat-activity-meta";
  const streamState = chatViewState.streamActive ? "stream live" : "stream idle";
  runMeta.textContent = `Run: ${runID || "-"} · status ${status} · ${streamState}`;

  const sessionMeta = document.createElement("p");
  sessionMeta.className = "chat-activity-meta muted";
  sessionMeta.textContent = `Session: ${sessionID || "(pending)"}${updatedAt ? ` · updated ${formatDateTime(updatedAt)}` : ""}`;

  const debugActions = document.createElement("div");
  debugActions.className = "chat-composer-actions";
  const copyDebugButton = document.createElement("button");
  copyDebugButton.type = "button";
  copyDebugButton.className = "layout-toggle";
  copyDebugButton.textContent = "Copy debug bundle";
  copyDebugButton.addEventListener("click", () => {
    void copyDebugBundle();
  });
  debugActions.append(copyDebugButton);

  const latestToolCard = document.createElement("section");
  latestToolCard.className = "chat-activity-card";
  const latestToolTitle = document.createElement("h4");
  latestToolTitle.textContent = "Latest Tool Activity";
  latestToolCard.append(latestToolTitle);

  if (!chatViewState.latestToolActivity) {
    const empty = document.createElement("p");
    empty.className = "muted";
    empty.textContent = "No tool events observed yet.";
    latestToolCard.append(empty);
  } else {
    const event = chatViewState.latestToolActivity;
    const summary = document.createElement("p");
    summary.className = `chat-tool-line ${event.status === "failed" ? "failed" : "ok"}`;
    summary.textContent = `${event.tool} · ${event.status}${event.ts ? ` · ${formatDateTime(event.ts)}` : ""}`;

    const detail = document.createElement("p");
    detail.className = "muted";
    detail.textContent = compactText(firstNonEmpty(event.summary, event.errorText, event.outputText, event.argsText), 260) || "(no summary)";

    latestToolCard.append(summary, detail);
  }

  const errorCard = document.createElement("section");
  errorCard.className = "chat-activity-card";
  const errorTitle = document.createElement("h4");
  errorTitle.textContent = "Last Error Summary";
  const errorBody = document.createElement("p");
  errorBody.className = chatViewState.lastErrorSummary ? "" : "muted";
  errorBody.textContent = chatViewState.lastErrorSummary || "No recent errors.";
  errorCard.append(errorTitle, errorBody);

  const riskCard = document.createElement("section");
  riskCard.className = `chat-activity-card chat-loop-risk ${chatViewState.loopRisk.level}`;
  const riskTitle = document.createElement("h4");
  riskTitle.textContent = "Loop Risk";

  const riskStatus = document.createElement("p");
  riskStatus.className = "chat-loop-risk-status";
  riskStatus.textContent = `${chatViewState.loopRisk.level.toUpperCase()} · score ${chatViewState.loopRisk.score}`;

  const riskMeta = document.createElement("p");
  riskMeta.className = "muted";
  riskMeta.textContent = `${chatViewState.loopRisk.failureCount}/${chatViewState.loopRisk.windowSize} failures in recent tool window, max repeat ${chatViewState.loopRisk.repeatCount}.`;
  riskCard.append(riskTitle, riskStatus, riskMeta);

  if (chatViewState.loopRisk.reasons.length) {
    const reasonList = document.createElement("ul");
    reasonList.className = "chat-loop-risk-reasons";
    chatViewState.loopRisk.reasons.forEach((reason) => {
      const item = document.createElement("li");
      item.textContent = reason;
      reasonList.append(item);
    });
    riskCard.append(reasonList);
  }

  activityPane.append(activityTitle, agentControlCard, runMeta, sessionMeta, debugActions, latestToolCard, errorCard, riskCard);

  page.append(transcriptPane, activityPane);
  container.append(heading, note, page);

  if (chatViewState.transcriptPinned) {
    transcript.scrollTop = transcript.scrollHeight;
  } else {
    const maxScrollTop = Math.max(0, transcript.scrollHeight - transcript.clientHeight);
    transcript.scrollTop = Math.min(chatViewState.transcriptScrollTop, maxScrollTop);
  }

  // Restore focus and cursor position if the textarea had focus before re-render
  if (hadInputFocus) {
    const newInput = container.querySelector(".chat-input");
    if (newInput) {
      newInput.focus();
      try {
        newInput.setSelectionRange(selStart, selEnd);
      } catch (_e) {
        // setSelectionRange can throw on some element types; safe to ignore
      }
    }
  }

  lastRenderedFingerprint = transcriptFingerprint();
}

export const chatPage = {
  key: "chat",
  title: "Chat",
  async render({ container, apiClient, store }) {
    chatViewState.container = container;
    chatViewState.apiClient = apiClient;
    chatViewState.store = store;
    ensureRouteWatcher();

    await refreshAvailableAgents();
    renderChatPage();
    startIdleSessionWatch();

    if (chatViewState.currentRunID && !isTerminalStatus(chatViewState.currentRunStatus)) {
      if (!chatViewState.polling) {
        startPolling({ runID: chatViewState.currentRunID, sessionID: chatViewState.currentSessionID });
      }
      if (!chatViewState.streamActive || chatViewState.currentStreamRunID !== chatViewState.currentRunID) {
        void connectRunEventStream(chatViewState.currentRunID);
      }
      return;
    }

    if (chatViewState.streamActive) {
      resetStreamingState();
    }
  },
};
