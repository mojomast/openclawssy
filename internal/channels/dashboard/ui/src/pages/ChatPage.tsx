import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import { useNavigate } from "react-router-dom"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { api } from "@/lib/api"
import { useToast } from "@/hooks/useToast"
import { cn } from "@/lib/utils"
import { ChevronDown, ChevronRight, RefreshCw } from "lucide-react"

const CHAT_DEFAULTS = {
  userID: "dashboard_user",
  roomID: "dashboard",
  agentID: "default",
}

const RUN_POLL_MS = 1500
const SESSION_POLL_MS = 2000
const STREAM_SESSION_POLL_MS = 5000
const SESSION_MESSAGES_LIMIT = 200
const SESSION_LOOKUP_LIMIT = 1
const CHAT_LAYOUT_STORAGE_SLOT = "dashboard.chat_layout.p1"
const CHAT_ACTIVITY_WIDTH_MIN = 260
const CHAT_ACTIVITY_WIDTH_MAX = 720
const CHAT_ACTIVITY_WIDTH_DEFAULT = 368
const CHAT_TOOL_TIMELINE_DEFAULT = true
const RESUME_INTERRUPTED_RUN_MESSAGE =
  "Resume your previous response from the exact interruption point. Continue from the cutoff without repeating already-completed text unless needed for coherence."
const TERMINAL_RUN_STATUSES = new Set(["completed", "failed", "canceled", "cancelled"])
const CANCELING_RUN_STATUSES = new Set(["canceling", "cancelling"])

type ToolStatus = "ok" | "failed"

type ToolEvent = {
  tool: string
  toolCallID: string
  runID: string
  summary: string
  argsText: string
  outputText: string
  errorText: string
  durationMS: number
  status: ToolStatus
  ts: string
  index: number
}

type TranscriptRole = "user" | "assistant" | "tool"

type TranscriptItem = {
  role: TranscriptRole
  content: string
  pending: boolean
  ts: string
  source: "local" | "session" | "stream"
  toolEvent?: ToolEvent
  toolKey?: string
}

type LoopRisk = {
  level: "low" | "medium" | "high"
  score: number
  reasons: string[]
  repeatCount: number
  failureCount: number
  windowSize: number
}

type ResumableInterruption = {
  runID: string
  sessionID: string
  updatedAt: string
  source: string
  message: string
}

type ChatState = {
  draft: string
  lastPrompt: string
  transcript: TranscriptItem[]
  sendPending: boolean
  sendError: string
  debugCopyStatus: string

  currentRunID: string
  currentRunStatus: string
  currentRunStartedAtMs: number
  currentSessionID: string
  currentRunLastUpdatedAt: string
  currentRunLastOutput: string

  latestToolActivity: ToolEvent | null
  streamToolEvents: ToolEvent[]
  resumableInterruption: ResumableInterruption | null
  lastErrorSummary: string
  expandedToolEntries: Record<string, boolean>
  expandedErrorEntries: Record<string, boolean>
  loopRisk: LoopRisk

  polling: boolean
  runPollingEnabled: boolean
  sessionPollIntervalMS: number

  streamActive: boolean
  streamLastEventID: number
  currentStreamingText: string
  currentStreamRunID: string
  streamError: string

  availableAgents: string[]
  selectedAgentID: string
  activeAgentID: string
  switchAgentPending: boolean
  switchAgentError: string
  showToolActivityInTranscript: boolean
  agentProfileContext: Record<string, unknown> | null
  agentGlobalConfig: Record<string, unknown> | null
  activityPaneWidth: number
}

type SessionMessage = {
  role?: unknown
  content?: unknown
  ts?: unknown
  tool_name?: unknown
  tool_call_id?: unknown
  run_id?: unknown
  duration_ms?: unknown
}

type ChatSessionMessagesPayload = {
  session_id?: unknown
  messages?: unknown
}

type ClipboardCopyResult = {
  usedFallback: boolean
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value))
}

function safeText(value: unknown): string {
  return String(value ?? "").trim()
}

function firstNonEmpty(...values: unknown[]): string {
  for (const value of values) {
    const text = safeText(value)
    if (text) {
      return text
    }
  }
  return ""
}

function isTerminalStatus(status: unknown): boolean {
  return TERMINAL_RUN_STATUSES.has(safeText(status).toLowerCase())
}

function isCancelingStatus(status: unknown): boolean {
  return CANCELING_RUN_STATUSES.has(safeText(status).toLowerCase())
}

function hasInterruptionSignal(value: unknown): boolean {
  const text = safeText(value).toLowerCase()
  if (!text) {
    return false
  }
  return (
    text.includes("send `continue`") ||
    text.includes("send 'continue'") ||
    text.includes('send "continue"') ||
    text.includes("send continue") ||
    text.includes("resume from the cutoff point") ||
    text.includes("stream interrupted") ||
    text.includes("response stream was interrupted") ||
    text.includes("provider stream interrupted") ||
    text.includes("stream disconnected") ||
    text.includes("connection dropped")
  )
}

function normalizeRunStatus(status: unknown): string {
  const normalized = safeText(status).toLowerCase()
  if (normalized === "cancelled") {
    return "canceled"
  }
  if (normalized === "cancelling") {
    return "canceling"
  }
  return isCancelingStatus(normalized) ? "canceling" : normalized
}

function resolveCancelRunStatus(status: unknown, cancelled: unknown): string {
  const normalizedStatus = normalizeRunStatus(status)
  if (normalizedStatus && (isTerminalStatus(normalizedStatus) || isCancelingStatus(normalizedStatus))) {
    return normalizedStatus
  }
  if (typeof cancelled === "boolean" && cancelled) {
    return "canceling"
  }
  return normalizedStatus || "canceling"
}

function formatDateTime(value: unknown): string {
  const text = safeText(value)
  if (!text) {
    return "-"
  }
  const parsed = new Date(text)
  if (Number.isNaN(parsed.getTime())) {
    return "-"
  }
  return parsed.toLocaleString()
}

function compactText(value: unknown, maxChars = 220): string {
  const text = safeText(value).replace(/\s+/g, " ")
  if (!text) {
    return ""
  }
  if (text.length <= maxChars) {
    return text
  }
  return `${text.slice(0, Math.max(0, maxChars - 3))}...`
}

function parseMaybeJSON(value: unknown): unknown {
  if (value === null || value === undefined) {
    return null
  }
  if (typeof value === "object") {
    return value
  }
  const text = safeText(value)
  if (!text) {
    return null
  }
  try {
    return JSON.parse(text)
  } catch {
    return null
  }
}

function asDisplayText(value: unknown): string {
  if (value === null || value === undefined) {
    return ""
  }
  if (typeof value === "string") {
    return value
  }
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function tryFormatJSONText(value: unknown): string {
  const text = safeText(value)
  if (!text || (!text.startsWith("{") && !text.startsWith("["))) {
    return ""
  }
  try {
    const parsed = JSON.parse(text)
    return JSON.stringify(parsed, null, 2)
  } catch {
    return ""
  }
}

function formatToolDetailValue(value: unknown): string {
  const text = typeof value === "string" ? value : asDisplayText(value)
  return tryFormatJSONText(text) || text
}

function toolEventKey(event: ToolEvent): string {
  return [
    safeText(event.runID),
    safeText(event.tool),
    safeText(event.toolCallID),
    String(event.index || 0),
    safeText(event.ts),
  ].join("|")
}

function parseSSEBlock(rawBlock: string): { eventName: string; eventID: string; data: string } | null {
  const lines = String(rawBlock || "").replace(/\r/g, "").split("\n")
  const dataLines: string[] = []
  let eventName = "message"
  let eventID = ""

  for (const line of lines) {
    if (!line || line.startsWith(":")) {
      continue
    }
    const separator = line.indexOf(":")
    const key = separator >= 0 ? line.slice(0, separator).trim() : line.trim()
    let value = separator >= 0 ? line.slice(separator + 1) : ""
    if (value.startsWith(" ")) {
      value = value.slice(1)
    }
    if (key === "event") {
      eventName = safeText(value) || "message"
      continue
    }
    if (key === "id") {
      eventID = safeText(value)
      continue
    }
    if (key === "data") {
      dataLines.push(value)
    }
  }

  if (!dataLines.length) {
    return null
  }
  return {
    eventName,
    eventID,
    data: dataLines.join("\n"),
  }
}

function buildLoopRisk(toolEvents: ToolEvent[]): LoopRisk {
  const window = toolEvents.slice(-8)
  const failures = window.filter((event) => event.status === "failed")
  const repeatCounts = new Map<string, number>()
  const failureSignatures = new Map<string, number>()

  for (const event of window) {
    repeatCounts.set(event.tool, (repeatCounts.get(event.tool) || 0) + 1)
    if (event.status === "failed") {
      const signature = `${event.tool}|${compactText(event.errorText, 180)}`
      failureSignatures.set(signature, (failureSignatures.get(signature) || 0) + 1)
    }
  }

  let repeatedToolName = ""
  let repeatedToolCount = 0
  for (const [toolName, count] of repeatCounts) {
    if (count > repeatedToolCount) {
      repeatedToolCount = count
      repeatedToolName = toolName
    }
  }

  let repeatedFailureCount = 0
  for (const count of failureSignatures.values()) {
    if (count > repeatedFailureCount) {
      repeatedFailureCount = count
    }
  }

  let score = 0
  const reasons: string[] = []

  if (repeatedToolCount >= 3) {
    score += 2
    reasons.push(`${repeatedToolName || "tool"} repeated ${repeatedToolCount}x in the recent window.`)
  }
  if (repeatedToolCount >= 5) {
    score += 2
  }
  if (failures.length >= 3) {
    score += 2
    reasons.push(`${failures.length} recent tool failures detected.`)
  }
  if (window.length >= 4 && failures.length / window.length >= 0.5) {
    score += 1
  }
  if (repeatedFailureCount >= 2) {
    score += 2
    reasons.push("Same tool error repeated across recent attempts.")
  }
  if (repeatedFailureCount >= 3) {
    score += 1
  }
  if (window.some((event) => safeText(event.errorText).includes("repetition detected"))) {
    score += 3
    reasons.push("Backend repetition guard triggered.")
  }

  const level: LoopRisk["level"] = score >= 6 ? "high" : score >= 3 ? "medium" : "low"
  return {
    level,
    score,
    reasons,
    repeatCount: repeatedToolCount,
    failureCount: failures.length,
    windowSize: window.length,
  }
}

function messageSupportsContinue(value: unknown): boolean {
  const text = safeText(value).toLowerCase()
  if (!text) {
    return false
  }
  return text.includes("send `continue`") || text.includes("resume from the cutoff point")
}

function buildResumableInterruption(
  message: unknown,
  meta: {
    runID?: unknown
    sessionID?: unknown
    updatedAt?: unknown
    source?: unknown
    status?: unknown
    error?: unknown
    streamError?: unknown
    interrupted?: unknown
  }
): ResumableInterruption | null {
  const summary = safeText(message)
  const status = normalizeRunStatus(meta.status)
  const interruptedStatus = status === "failed"
  const canceledStatus = status === "canceled"
  const interruptedSignal =
    hasInterruptionSignal(summary) || hasInterruptionSignal(meta.error) || hasInterruptionSignal(meta.streamError)
  if (!(messageSupportsContinue(summary) || Boolean(meta.interrupted) || canceledStatus || (interruptedStatus && interruptedSignal))) {
    return null
  }

  const resolvedMessage = summary || firstNonEmpty(meta.error, "Run was interrupted before completion.")
  return {
    runID: safeText(meta.runID),
    sessionID: safeText(meta.sessionID),
    updatedAt: safeText(meta.updatedAt) || new Date().toISOString(),
    source: safeText(meta.source) || "assistant_output",
    message: resolvedMessage,
  }
}

function readChatLayoutPrefs(): { activityPaneWidth: number; showToolActivityInTranscript: boolean } {
  try {
    const raw = window.localStorage.getItem(CHAT_LAYOUT_STORAGE_SLOT)
    if (!raw) {
      return {
        activityPaneWidth: CHAT_ACTIVITY_WIDTH_DEFAULT,
        showToolActivityInTranscript: CHAT_TOOL_TIMELINE_DEFAULT,
      }
    }
    const parsed = JSON.parse(raw)
    return {
      activityPaneWidth: clamp(
        Number(parsed.activityPaneWidth) || CHAT_ACTIVITY_WIDTH_DEFAULT,
        CHAT_ACTIVITY_WIDTH_MIN,
        CHAT_ACTIVITY_WIDTH_MAX
      ),
      showToolActivityInTranscript:
        typeof parsed.showToolActivityInTranscript === "boolean"
          ? parsed.showToolActivityInTranscript
          : CHAT_TOOL_TIMELINE_DEFAULT,
    }
  } catch {
    return {
      activityPaneWidth: CHAT_ACTIVITY_WIDTH_DEFAULT,
      showToolActivityInTranscript: CHAT_TOOL_TIMELINE_DEFAULT,
    }
  }
}

function createLoopRiskDefault(): LoopRisk {
  return {
    level: "low",
    score: 0,
    reasons: [],
    repeatCount: 0,
    failureCount: 0,
    windowSize: 0,
  }
}

function replacePendingAssistantInTranscript(transcript: TranscriptItem[], message: string): TranscriptItem[] {
  const index = transcript.findIndex((item) => item.role === "assistant" && item.pending)
  if (index >= 0) {
    const next = transcript.slice()
    next[index] = {
      role: "assistant",
      content: message,
      pending: false,
      ts: new Date().toISOString(),
      source: next[index].source,
    }
    return next
  }
  return [
    ...transcript,
    {
      role: "assistant",
      content: message,
      pending: false,
      ts: new Date().toISOString(),
      source: "local",
    },
  ]
}

function upsertPendingAssistantInTranscript(transcript: TranscriptItem[], message: string): TranscriptItem[] {
  const text = String(message || "")
  if (!safeText(text)) {
    return transcript
  }
  const index = [...transcript]
    .reverse()
    .findIndex((item) => item.role === "assistant" && item.pending)
  if (index >= 0) {
    const pendingIndex = transcript.length - 1 - index
    const next = transcript.slice()
    next[pendingIndex] = {
      role: "assistant",
      content: text,
      pending: true,
      ts: next[pendingIndex].ts || new Date().toISOString(),
      source: next[pendingIndex].source,
    }
    return next
  }
  return [
    ...transcript,
    {
      role: "assistant",
      content: text,
      pending: true,
      ts: new Date().toISOString(),
      source: "local",
    },
  ]
}

function parseObservedSubagent(event: ToolEvent): {
  runID: string
  agentID: string
  provider: string
  model: string
  output: string
  durationMS: number
  toolCalls: number
  ts: string
  status: "failed" | "completed"
  summary: string
} | null {
  if (!event || event.tool !== "agent.run") {
    return null
  }
  const parsedOutput = parseMaybeJSON(event.outputText)
  if (!parsedOutput || typeof parsedOutput !== "object") {
    return null
  }
  const payload = parsedOutput as Record<string, unknown>
  const runID = safeText(payload.run_id)
  if (!runID) {
    return null
  }
  return {
    runID,
    agentID: safeText(payload.agent_id) || "unknown",
    provider: safeText(payload.provider),
    model: safeText(payload.model),
    output: asDisplayText(payload.output),
    durationMS: Number(payload.duration_ms) || 0,
    toolCalls: Number(payload.tool_calls) || 0,
    ts: safeText(event.ts),
    status: event.status === "failed" ? "failed" : "completed",
    summary: firstNonEmpty(event.summary, payload.output, payload.agent_id),
  }
}

function copyToClipboardWithFallback(text: string): Promise<ClipboardCopyResult> {
  const value = String(text || "")
  if (!value) {
    return Promise.reject(new Error("Nothing to copy"))
  }

  const persistDeterministicFallback = (): ClipboardCopyResult => {
    const storageKey = "dashboard.chat.debug_bundle.fallback_copy"
    try {
      window.localStorage.setItem(storageKey, value)
    } catch {
      // ignore storage errors
    }
    try {
      ;(window as Window & { __openclawssyDebugBundleFallback?: string }).__openclawssyDebugBundleFallback =
        value
    } catch {
      // ignore assignment errors
    }
    return { usedFallback: true }
  }

  const fallbackCopy = () =>
    new Promise<ClipboardCopyResult>((resolve, reject) => {
      try {
        const textarea = document.createElement("textarea")
        textarea.value = value
        textarea.setAttribute("readonly", "readonly")
        textarea.style.position = "fixed"
        textarea.style.opacity = "0"
        document.body.append(textarea)
        textarea.focus()
        textarea.select()
        const copied = document.execCommand("copy")
        textarea.remove()
        if (copied) {
          resolve({ usedFallback: true })
          return
        }

        resolve(persistDeterministicFallback())
      } catch (error) {
        try {
          resolve(persistDeterministicFallback())
        } catch {
          reject(error instanceof Error ? error : new Error(String(error)))
        }
      }
    })

  if (navigator?.clipboard?.writeText) {
    return navigator.clipboard
      .writeText(value)
      .then(() => ({ usedFallback: false }))
      .catch(() => fallbackCopy())
  }
  return fallbackCopy()
}

function normalizeToolEventFromMessage(message: SessionMessage, index: number): ToolEvent {
  const parsed = parseMaybeJSON(message.content) as Record<string, unknown> | null
  const toolName = firstNonEmpty(message.tool_name, parsed?.tool, parsed?.name, "unknown.tool")
  const toolCallID = firstNonEmpty(message.tool_call_id, parsed?.id, parsed?.tool_call_id)
  const runID = firstNonEmpty(message.run_id, parsed?.run_id)
  const summary = firstNonEmpty(parsed?.summary, parsed?.message)
  const argsText = asDisplayText(parsed?.arguments ?? parsed?.args ?? parsed?.input ?? parsed?.params)
  const outputText = asDisplayText(parsed?.output ?? parsed?.result ?? parsed?.response)
  const errorText = asDisplayText(parsed?.error ?? parsed?.callback_error ?? parsed?.stderr)
  const durationMS = Number(parsed?.duration_ms ?? message.duration_ms) || 0
  const status: ToolStatus = safeText(errorText) ? "failed" : "ok"

  return {
    tool: toolName,
    toolCallID,
    runID,
    summary,
    argsText,
    outputText,
    errorText,
    durationMS,
    status,
    ts: String(message.ts || ""),
    index,
  }
}

function normalizeSessionTranscript(
  messages: SessionMessage[],
  showToolActivityInTranscript: boolean
): TranscriptItem[] {
  return messages
    .map((item, index): TranscriptItem | null => {
      const role = safeText(item.role).toLowerCase()
      if (role === "tool") {
        if (!showToolActivityInTranscript) {
          return null
        }
        const event = normalizeToolEventFromMessage(item, index)
        return {
          role: "tool",
          content: firstNonEmpty(event.summary, event.errorText, event.outputText, event.argsText, event.tool),
          pending: false,
          ts: String(item.ts || new Date().toISOString()),
          toolEvent: event,
          toolKey: toolEventKey(event),
          source: "session",
        }
      }
      if (role !== "user" && role !== "assistant") {
        return null
      }
      const content = String(item.content || "")
      if (!safeText(content)) {
        return null
      }
      return {
        role,
        content,
        pending: false,
        ts: String(item.ts || new Date().toISOString()),
        source: "session",
      }
    })
    .filter((item): item is TranscriptItem => item !== null)
}

function settingsProfilePath(agentID: string): string {
  const params = new URLSearchParams()
  params.set("category", "agents")
  if (safeText(agentID)) {
    params.set("profile", safeText(agentID))
  }
  return `/settings?${params.toString()}`
}

export function ChatPage() {
  const navigate = useNavigate()
  const { toast } = useToast()
  const transcriptRef = useRef<HTMLDivElement | null>(null)
  const transcriptPinnedRef = useRef(true)
  const transcriptScrollTopRef = useRef(0)
  const streamAbortControllerRef = useRef<AbortController | null>(null)
  const runPollInFlightRef = useRef(false)
  const sessionPollInFlightRef = useRef(false)
  const sessionBootstrapInFlightRef = useRef(false)
  const mountedRef = useRef(true)

  const [state, setState] = useState<ChatState>(() => {
    const prefs = readChatLayoutPrefs()
    return {
      draft: "",
      lastPrompt: "",
      transcript: [],
      sendPending: false,
      sendError: "",
      debugCopyStatus: "",

      currentRunID: "",
      currentRunStatus: "idle",
      currentRunStartedAtMs: 0,
      currentSessionID: "",
      currentRunLastUpdatedAt: "",
      currentRunLastOutput: "",

      latestToolActivity: null,
      streamToolEvents: [],
      resumableInterruption: null,
      lastErrorSummary: "",
      expandedToolEntries: {},
      expandedErrorEntries: {},
      loopRisk: createLoopRiskDefault(),

      polling: false,
      runPollingEnabled: true,
      sessionPollIntervalMS: SESSION_POLL_MS,

      streamActive: false,
      streamLastEventID: 0,
      currentStreamingText: "",
      currentStreamRunID: "",
      streamError: "",

      availableAgents: [CHAT_DEFAULTS.agentID],
      selectedAgentID: CHAT_DEFAULTS.agentID,
      activeAgentID: CHAT_DEFAULTS.agentID,
      switchAgentPending: false,
      switchAgentError: "",
      showToolActivityInTranscript: prefs.showToolActivityInTranscript,
      agentProfileContext: null,
      agentGlobalConfig: null,
      activityPaneWidth: prefs.activityPaneWidth,
    }
  })

  const stateRef = useRef(state)
  useEffect(() => {
    stateRef.current = state
  }, [state])

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      if (streamAbortControllerRef.current) {
        streamAbortControllerRef.current.abort()
      }
    }
  }, [])

  useEffect(() => {
    try {
      window.localStorage.setItem(
        CHAT_LAYOUT_STORAGE_SLOT,
        JSON.stringify({
          activityPaneWidth: state.activityPaneWidth,
          showToolActivityInTranscript: state.showToolActivityInTranscript,
        })
      )
    } catch {
      // ignore localStorage failures
    }
  }, [state.activityPaneWidth, state.showToolActivityInTranscript])

  const clearResumableInterruption = useCallback((runID?: string) => {
    const targetRunID = safeText(runID)
    setState((prev) => {
      if (!prev.resumableInterruption) {
        return prev
      }
      if (
        targetRunID &&
        safeText(prev.resumableInterruption.runID) &&
        safeText(prev.resumableInterruption.runID) !== targetRunID
      ) {
        return prev
      }
      return {
        ...prev,
        resumableInterruption: null,
      }
    })
  }, [])

  const shouldOfferContinueAction = useMemo(() => {
    const status = normalizeRunStatus(state.currentRunStatus)
    if (status === "canceled") {
      return true
    }

    const latestAssistant = state.transcript
      .slice()
      .reverse()
      .find((item) => item.role === "assistant")
    const hasInterruptionFallbackSignal =
      status === "failed" &&
      (hasInterruptionSignal(state.lastErrorSummary) ||
        hasInterruptionSignal(state.streamError) ||
        hasInterruptionSignal(state.currentRunLastOutput) ||
        hasInterruptionSignal(latestAssistant?.content))

    if (!state.resumableInterruption && !hasInterruptionFallbackSignal) {
      return false
    }
    if (!status || status === "idle" || status === "paused") {
      return true
    }
    return isTerminalStatus(status)
  }, [
    state.currentRunLastOutput,
    state.currentRunStatus,
    state.lastErrorSummary,
    state.resumableInterruption,
    state.streamError,
    state.transcript,
  ])

  const observedSubagentRuns = useMemo(() => {
    const seen = new Set<string>()
    const out: Array<{
      runID: string
      agentID: string
      provider: string
      model: string
      output: string
      durationMS: number
      toolCalls: number
      ts: string
      status: "failed" | "completed"
      summary: string
    }> = []
    for (const event of state.streamToolEvents) {
      const item = parseObservedSubagent(event)
      if (!item || seen.has(item.runID)) {
        continue
      }
      seen.add(item.runID)
      out.push(item)
    }
    return out.slice().reverse()
  }, [state.streamToolEvents])

  const ensureCurrentSessionID = useCallback(async (): Promise<string> => {
    const current = safeText(stateRef.current.currentSessionID)
    if (current) {
      return current
    }
    if (sessionBootstrapInFlightRef.current) {
      return ""
    }

    sessionBootstrapInFlightRef.current = true
    try {
      const params = new URLSearchParams({
        agent_id: safeText(stateRef.current.selectedAgentID) || CHAT_DEFAULTS.agentID,
        user_id: CHAT_DEFAULTS.userID,
        room_id: CHAT_DEFAULTS.roomID,
        channel: "dashboard",
        limit: String(SESSION_LOOKUP_LIMIT),
        offset: "0",
      })
      const payload = await api.get<{ sessions?: Array<{ session_id?: unknown }> }>(
        `/api/admin/chat/sessions?${params.toString()}`
      )
      const sessions = Array.isArray(payload?.sessions) ? payload.sessions : []
      const sessionID = safeText(sessions[0]?.session_id)
      if (sessionID) {
        setState((prev) => ({
          ...prev,
          currentSessionID: sessionID,
        }))
      }
      return sessionID
    } catch {
      return ""
    } finally {
      sessionBootstrapInFlightRef.current = false
    }
  }, [])

  const applySessionMessagesPayload = useCallback(
    (payload: ChatSessionMessagesPayload) => {
      setState((prev) => {
        const rawMessages = Array.isArray(payload?.messages) ? (payload.messages as SessionMessage[]) : []
        const sessionTranscript = normalizeSessionTranscript(rawMessages, prev.showToolActivityInTranscript)

        let nextTranscript = prev.transcript
        if (sessionTranscript.length) {
          const pendingAssistant = [...prev.transcript]
            .reverse()
            .find((item) => item.role === "assistant" && item.pending)
          const sessionToolKeys = new Set(
            sessionTranscript
              .filter((item) => item.role === "tool" && safeText(item.toolKey))
              .map((item) => item.toolKey as string)
          )
          const liveToolItems = prev.showToolActivityInTranscript
            ? prev.transcript.filter(
                (item) =>
                  item.role === "tool" &&
                  item.source === "stream" &&
                  safeText(item.toolKey) &&
                  !sessionToolKeys.has(item.toolKey as string)
              )
            : []

          nextTranscript = [...sessionTranscript, ...liveToolItems]
          if (pendingAssistant && (prev.sendPending || prev.polling || prev.streamActive)) {
            nextTranscript.push(pendingAssistant)
          }

          const latestLocalAssistant = [...prev.transcript]
            .reverse()
            .find((item) => item.role === "assistant" && item.source !== "session" && safeText(item.content))
          if (latestLocalAssistant) {
            const alreadyPresent = nextTranscript.some(
              (item) => item.role === "assistant" && safeText(item.content) === safeText(latestLocalAssistant.content)
            )
            if (!alreadyPresent && (prev.sendPending || prev.polling || prev.streamActive || isTerminalStatus(prev.currentRunStatus))) {
              nextTranscript.push({
                ...latestLocalAssistant,
                pending: false,
              })
            }
          }
        }

        const toolEvents = rawMessages
          .map((message, index) => {
            if (safeText(message.role).toLowerCase() !== "tool") {
              return null
            }
            return normalizeToolEventFromMessage(message, index)
          })
          .filter((item): item is ToolEvent => item !== null)

        const streamToolEvents = toolEvents.length ? toolEvents.slice(-32) : prev.streamToolEvents
        const mergedToolEvents = streamToolEvents.length ? streamToolEvents : toolEvents
        const latestToolActivity = mergedToolEvents.length
          ? mergedToolEvents[mergedToolEvents.length - 1]
          : null
        const loopRisk = buildLoopRisk(mergedToolEvents)

        let lastErrorSummary = prev.lastErrorSummary
        const failedEvents = mergedToolEvents
          .slice()
          .reverse()
          .filter((event) => event.status === "failed" && safeText(event.errorText))
        const currentRunID = safeText(prev.currentRunID)
        const latestFailed = currentRunID
          ? failedEvents.find((event) => safeText(event.runID) === currentRunID) || null
          : failedEvents[0] || null
        if (latestFailed) {
          lastErrorSummary = compactText(latestFailed.errorText, 280)
        } else if (currentRunID && normalizeRunStatus(prev.currentRunStatus) !== "failed") {
          lastErrorSummary = ""
        }

        let resumableInterruption = prev.resumableInterruption
        const status = normalizeRunStatus(prev.currentRunStatus)
        if (!status || status === "idle" || status === "paused" || isTerminalStatus(status)) {
          const latestAssistant = nextTranscript
            .slice()
            .reverse()
            .find((item) => item.role === "assistant")
          if (latestAssistant) {
            const resumable = buildResumableInterruption(latestAssistant.content, {
              runID: currentRunID,
              sessionID: safeText(payload?.session_id) || prev.currentSessionID,
              updatedAt: prev.currentRunLastUpdatedAt,
              source: "session.transcript",
              status,
              error: prev.lastErrorSummary,
              streamError: prev.streamError,
            })
            resumableInterruption = resumable
          } else if (!currentRunID || safeText(resumableInterruption?.runID) === currentRunID) {
            resumableInterruption = null
          }
        }

        return {
          ...prev,
          transcript: nextTranscript,
          streamToolEvents,
          latestToolActivity,
          loopRisk,
          lastErrorSummary,
          resumableInterruption,
        }
      })
    },
    []
  )

  const refreshTranscriptFromCurrentSession = useCallback(async () => {
    const sessionID = await ensureCurrentSessionID()
    if (!sessionID) {
      return
    }
    const payload = await api.get<ChatSessionMessagesPayload>(
      `/api/admin/chat/sessions/${encodeURIComponent(sessionID)}/messages?limit=${encodeURIComponent(
        String(SESSION_MESSAGES_LIMIT)
      )}`
    )
    applySessionMessagesPayload(payload)
  }, [applySessionMessagesPayload, ensureCurrentSessionID])

  const refreshAvailableAgents = useCallback(async () => {
    try {
      const params = new URLSearchParams({
        channel: "dashboard",
        user_id: CHAT_DEFAULTS.userID,
        room_id: CHAT_DEFAULTS.roomID,
      })
      const requestedAgent = safeText(stateRef.current.selectedAgentID)
      if (requestedAgent) {
        params.set("agent_id", requestedAgent)
      }

      const payload = await api.get<{
        agents?: unknown
        active_agent?: unknown
        selected_agent?: unknown
        profile_context?: unknown
        agents_config?: unknown
      }>(`/api/admin/agents?${params.toString()}`)

      const agents = Array.isArray(payload?.agents)
        ? payload.agents.map((item) => safeText(item)).filter(Boolean)
        : []

      setState((prev) => {
        const activeAgent = safeText(payload?.active_agent)
        const selected = safeText(payload?.selected_agent) || activeAgent
        return {
          ...prev,
          availableAgents: agents.length ? agents : prev.availableAgents,
          activeAgentID: activeAgent || prev.activeAgentID,
          selectedAgentID:
            selected ||
            (agents.length
              ? agents.includes(prev.selectedAgentID)
                ? prev.selectedAgentID
                : agents[0]
              : prev.selectedAgentID),
          agentProfileContext:
            payload?.profile_context && typeof payload.profile_context === "object"
              ? (payload.profile_context as Record<string, unknown>)
              : null,
          agentGlobalConfig:
            payload?.agents_config && typeof payload.agents_config === "object"
              ? (payload.agents_config as Record<string, unknown>)
              : null,
        }
      })
    } catch {
      setState((prev) => ({
        ...prev,
        availableAgents: prev.availableAgents.length ? prev.availableAgents : [CHAT_DEFAULTS.agentID],
      }))
    }
  }, [])

  const resetStreamingState = useCallback((options?: { resetLastEventID?: boolean; keepStreamingText?: boolean }) => {
    const resetLastEventID = Boolean(options?.resetLastEventID)
    const keepStreamingText = Boolean(options?.keepStreamingText)
    if (streamAbortControllerRef.current) {
      streamAbortControllerRef.current.abort()
      streamAbortControllerRef.current = null
    }
    setState((prev) => ({
      ...prev,
      streamActive: false,
      currentStreamRunID: "",
      currentStreamingText: keepStreamingText ? prev.currentStreamingText : "",
      streamLastEventID: resetLastEventID ? 0 : prev.streamLastEventID,
    }))
  }, [])

  const handleRunStreamStatus = useCallback((eventEnvelope: Record<string, unknown>) => {
    const data =
      eventEnvelope.data && typeof eventEnvelope.data === "object"
        ? (eventEnvelope.data as Record<string, unknown>)
        : {}
    const status = normalizeRunStatus(data.status || eventEnvelope.status)
    const sessionID = safeText(data.session_id)
    const ts = safeText(eventEnvelope.ts)

    setState((prev) => ({
      ...prev,
      currentRunStatus: status || normalizeRunStatus(prev.currentRunStatus),
      currentSessionID: sessionID || prev.currentSessionID,
      currentRunLastUpdatedAt: ts || new Date().toISOString(),
      currentRunStartedAtMs:
        prev.currentRunStartedAtMs <= 0 && status && !isTerminalStatus(status) && !isCancelingStatus(status)
          ? Date.now()
          : prev.currentRunStartedAtMs,
    }))
  }, [])

  const handleRunStreamToolEnd = useCallback((eventEnvelope: Record<string, unknown>) => {
    const payload =
      eventEnvelope.data && typeof eventEnvelope.data === "object"
        ? (eventEnvelope.data as Record<string, unknown>)
        : {}

    setState((prev) => {
      const event: ToolEvent = {
        tool: firstNonEmpty(payload.tool, payload.name, "unknown.tool"),
        toolCallID: firstNonEmpty(payload.tool_call_id, payload.id),
        runID: firstNonEmpty(eventEnvelope.run_id, prev.currentRunID),
        summary: firstNonEmpty(payload.summary, payload.message),
        argsText: asDisplayText(payload.arguments ?? payload.args ?? payload.params),
        outputText: asDisplayText(payload.output ?? payload.result),
        errorText: asDisplayText(payload.error ?? payload.callback_error),
        durationMS: Number(payload.duration_ms) || 0,
        status: safeText(payload.error) ? "failed" : "ok",
        ts: String(eventEnvelope.ts || new Date().toISOString()),
        index: prev.streamToolEvents.length,
      }

      const streamToolEvents = [...prev.streamToolEvents, event].slice(-32)
      let transcript = prev.transcript
      if (safeText(prev.currentStreamingText)) {
        transcript = upsertPendingAssistantInTranscript(transcript, "")
      }

      if (prev.showToolActivityInTranscript) {
        transcript = [
          ...transcript,
          {
            role: "tool",
            content: firstNonEmpty(event.summary, event.errorText, event.outputText, event.argsText, event.tool),
            pending: false,
            ts: String(event.ts || new Date().toISOString()),
            toolEvent: event,
            toolKey: toolEventKey(event),
            source: "stream",
          },
        ]
      } else {
        const detail = compactText(firstNonEmpty(event.summary, event.errorText, event.outputText, event.argsText), 180)
        transcript = upsertPendingAssistantInTranscript(
          transcript,
          detail ? `Working... ${event.tool}: ${detail}` : `Working... ${event.tool}`
        )
      }

      return {
        ...prev,
        transcript,
        currentStreamingText: "",
        streamToolEvents,
        latestToolActivity: event,
        loopRisk: buildLoopRisk(streamToolEvents),
        lastErrorSummary:
          event.status === "failed" && safeText(event.errorText)
            ? compactText(event.errorText, 280)
            : prev.lastErrorSummary,
      }
    })
  }, [])

  const handleRunStreamModelText = useCallback((eventEnvelope: Record<string, unknown>) => {
    const payload =
      eventEnvelope.data && typeof eventEnvelope.data === "object"
        ? (eventEnvelope.data as Record<string, unknown>)
        : {}
    const rawText =
      typeof payload.text === "string"
        ? payload.text
        : payload.text === undefined || payload.text === null
        ? ""
        : asDisplayText(payload.text)
    if (!rawText) {
      return
    }

    setState((prev) => {
      const currentStreamingText = payload.partial === false ? rawText : `${prev.currentStreamingText}${rawText}`
      if (!currentStreamingText) {
        return prev
      }
      const transcript = upsertPendingAssistantInTranscript(prev.transcript, currentStreamingText)
      return {
        ...prev,
        currentStreamingText,
        transcript,
      }
    })
  }, [])

  const handleRunStreamCompleted = useCallback(
    (eventEnvelope: Record<string, unknown>): boolean => {
      const payload =
        eventEnvelope.data && typeof eventEnvelope.data === "object"
          ? (eventEnvelope.data as Record<string, unknown>)
          : {}
      setState((prev) => {
        const updatedAt = safeText(eventEnvelope.ts) || new Date().toISOString()
        const output = String(payload.output || prev.currentStreamingText || "")
        const message =
          output || "Run completed without assistant output. Open trace or tool activity for details."
        const transcript = replacePendingAssistantInTranscript(prev.transcript, message)
        const resumable = buildResumableInterruption(message, {
          runID: prev.currentRunID,
          sessionID: prev.currentSessionID,
          updatedAt,
          source: "stream.completed",
          status: "completed",
        })
        return {
          ...prev,
          transcript,
          currentRunStatus: "completed",
          currentRunLastUpdatedAt: updatedAt,
          currentRunLastOutput: output,
          currentStreamingText: "",
          polling: false,
          runPollingEnabled: true,
          sessionPollIntervalMS: SESSION_POLL_MS,
          streamError: "",
          resumableInterruption: resumable,
        }
      })
      return true
    },
    []
  )

  const handleRunStreamCanceled = useCallback(
    (eventEnvelope: Record<string, unknown>): boolean => {
      const payload =
        eventEnvelope.data && typeof eventEnvelope.data === "object"
          ? (eventEnvelope.data as Record<string, unknown>)
          : {}
      const message = firstNonEmpty(payload.error, payload.message, "Run canceled.")
      setState((prev) => {
        const updatedAt = safeText(eventEnvelope.ts) || new Date().toISOString()
        const transcript = replacePendingAssistantInTranscript(prev.transcript, message)
        const resumable = buildResumableInterruption(message, {
          runID: prev.currentRunID,
          sessionID: prev.currentSessionID,
          updatedAt,
          source: "stream.canceled",
          status: "canceled",
          error: firstNonEmpty(payload.error, payload.message),
          streamError: prev.streamError,
        })
        return {
          ...prev,
          transcript,
          currentRunStatus: "canceled",
          currentRunLastUpdatedAt: updatedAt,
          currentStreamingText: "",
          polling: false,
          runPollingEnabled: true,
          sessionPollIntervalMS: SESSION_POLL_MS,
          streamError: "",
          resumableInterruption: resumable,
        }
      })
      return true
    },
    []
  )

  const handleRunStreamFailed = useCallback(
    (eventEnvelope: Record<string, unknown>): boolean => {
      const payload =
        eventEnvelope.data && typeof eventEnvelope.data === "object"
          ? (eventEnvelope.data as Record<string, unknown>)
          : {}
      const message = firstNonEmpty(payload.error, eventEnvelope.error, "Run failed.")
      setState((prev) => {
        const updatedAt = safeText(eventEnvelope.ts) || new Date().toISOString()
        const transcript = replacePendingAssistantInTranscript(prev.transcript, `Error: ${message}`)
        const resumable = buildResumableInterruption(message, {
          runID: prev.currentRunID,
          sessionID: prev.currentSessionID,
          updatedAt,
          source: "stream.failed",
          status: "failed",
          error: message,
          streamError: prev.streamError,
          interrupted: payload.interrupted,
        })
        return {
          ...prev,
          transcript,
          currentRunStatus: "failed",
          currentRunLastUpdatedAt: updatedAt,
          currentStreamingText: "",
          polling: false,
          runPollingEnabled: true,
          sessionPollIntervalMS: SESSION_POLL_MS,
          streamError: "",
          lastErrorSummary: compactText(message, 280),
          resumableInterruption: resumable,
        }
      })
      return true
    },
    []
  )

  const handleRunStreamEvent = useCallback(
    (rawType: unknown, eventEnvelope: Record<string, unknown>): boolean => {
      const type = safeText(rawType).toLowerCase()
      switch (type) {
        case "status":
          handleRunStreamStatus(eventEnvelope)
          return false
        case "tool_end":
          handleRunStreamToolEnd(eventEnvelope)
          return false
        case "model_text":
          handleRunStreamModelText(eventEnvelope)
          return false
        case "completed":
          return handleRunStreamCompleted(eventEnvelope)
        case "canceled":
          return handleRunStreamCanceled(eventEnvelope)
        case "failed":
          return handleRunStreamFailed(eventEnvelope)
        case "heartbeat":
          return false
        default:
          return false
      }
    },
    [
      handleRunStreamCanceled,
      handleRunStreamCompleted,
      handleRunStreamFailed,
      handleRunStreamModelText,
      handleRunStreamStatus,
      handleRunStreamToolEnd,
    ]
  )

  const processSSEBlock = useCallback(
    (rawBlock: string, runID: string): boolean => {
      const block = parseSSEBlock(rawBlock)
      if (!block) {
        return false
      }

      if (block.eventID) {
        const parsedID = Number(block.eventID)
        if (Number.isFinite(parsedID)) {
          const normalizedID = Math.floor(parsedID)
          setState((prev) => ({
            ...prev,
            streamLastEventID: normalizedID > prev.streamLastEventID ? normalizedID : prev.streamLastEventID,
          }))
        }
      }

      const dataText = String(block.data || "")
      if (!safeText(dataText)) {
        return false
      }
      if (safeText(dataText) === "[DONE]") {
        return true
      }

      const parsedPayload = parseMaybeJSON(dataText)
      const envelope =
        parsedPayload && typeof parsedPayload === "object"
          ? (parsedPayload as Record<string, unknown>)
          : {
              type: block.eventName,
              run_id: runID,
              data: { text: dataText },
            }

      const envelopeID = Number(envelope.id)
      if (Number.isFinite(envelopeID)) {
        const normalizedID = Math.floor(envelopeID)
        setState((prev) => ({
          ...prev,
          streamLastEventID: normalizedID > prev.streamLastEventID ? normalizedID : prev.streamLastEventID,
        }))
      }

      const eventType = safeText(envelope.type || block.eventName || "message")
      return handleRunStreamEvent(eventType, envelope)
    },
    [handleRunStreamEvent]
  )

  const connectRunEventStream = useCallback(
    async (runID: string) => {
      const targetRunID = safeText(runID)
      if (!targetRunID) {
        return
      }
      if (stateRef.current.streamActive && stateRef.current.currentStreamRunID === targetRunID) {
        return
      }

      if (streamAbortControllerRef.current) {
        streamAbortControllerRef.current.abort()
      }

      const controller = new AbortController()
      streamAbortControllerRef.current = controller

      setState((prev) => ({
        ...prev,
        streamActive: true,
        currentStreamRunID: targetRunID,
        streamError: "",
      }))

      try {
        const token = safeText(await api.resolveBearerToken().catch(() => ""))
        const headers = new Headers({ Accept: "text/event-stream" })
        if (token) {
          headers.set("Authorization", `Bearer ${token}`)
        }
        if (stateRef.current.streamLastEventID > 0) {
          headers.set("Last-Event-ID", String(stateRef.current.streamLastEventID))
        }

        const response = await window.fetch(`/v1/runs/events/${encodeURIComponent(targetRunID)}`, {
          method: "GET",
          headers,
          signal: controller.signal,
          cache: "no-store",
        })

        if (!response.ok) {
          throw new Error(`stream request failed (${response.status})`)
        }
        if (!response.body || typeof response.body.getReader !== "function") {
          throw new Error("streaming not supported by this browser/runtime")
        }

        setState((prev) => ({
          ...prev,
          runPollingEnabled: false,
          sessionPollIntervalMS: STREAM_SESSION_POLL_MS,
        }))

        const reader = response.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ""
        let terminal = false

        while (true) {
          const { value, done } = await reader.read()
          if (done) {
            break
          }
          if (controller.signal.aborted || !mountedRef.current) {
            throw new Error("stream aborted")
          }

          buffer += decoder.decode(value, { stream: true }).replace(/\r/g, "")
          let boundary = buffer.indexOf("\n\n")
          while (boundary >= 0) {
            const block = buffer.slice(0, boundary)
            buffer = buffer.slice(boundary + 2)
            if (processSSEBlock(block, targetRunID)) {
              terminal = true
              break
            }
            boundary = buffer.indexOf("\n\n")
          }
          if (terminal) {
            break
          }
        }

        if (!terminal) {
          const tail = decoder.decode()
          if (tail) {
            buffer += tail.replace(/\r/g, "")
          }
          if (safeText(buffer) && processSSEBlock(buffer, targetRunID)) {
            terminal = true
          }
        }

        if (!terminal && !isTerminalStatus(stateRef.current.currentRunStatus)) {
          throw new Error("stream disconnected")
        }
      } catch (error) {
        if (!controller.signal.aborted) {
          const message = compactText((error as Error)?.message || String(error), 280)
          setState((prev) => ({
            ...prev,
            streamError: message,
            lastErrorSummary: message,
          }))
        }
      } finally {
        const sameRun = stateRef.current.currentStreamRunID === targetRunID
        if (sameRun) {
          setState((prev) => ({
            ...prev,
            streamActive: false,
            currentStreamRunID: "",
          }))
        }
        if (streamAbortControllerRef.current === controller) {
          streamAbortControllerRef.current = null
        }

        if (!controller.signal.aborted && !isTerminalStatus(stateRef.current.currentRunStatus)) {
          setState((prev) => ({
            ...prev,
            runPollingEnabled: true,
            sessionPollIntervalMS: SESSION_POLL_MS,
          }))
        }
      }
    },
    [processSSEBlock]
  )

  const startPolling = useCallback((runID: string, sessionID = "") => {
    const normalizedRunID = safeText(runID)
    const normalizedSessionID = safeText(sessionID)
    if (!normalizedRunID) {
      return
    }
    setState((prev) => ({
      ...prev,
      currentRunID: normalizedRunID,
      currentSessionID: normalizedSessionID || prev.currentSessionID,
      currentRunStatus: prev.currentRunID === normalizedRunID ? prev.currentRunStatus : "running",
      currentRunStartedAtMs: Date.now(),
      polling: true,
      runPollingEnabled: true,
      sessionPollIntervalMS: SESSION_POLL_MS,
    }))
  }, [])

  const pollRunOnce = useCallback(async () => {
    if (runPollInFlightRef.current) {
      return
    }
    const runID = safeText(stateRef.current.currentRunID)
    if (!runID || !stateRef.current.polling || !stateRef.current.runPollingEnabled) {
      return
    }

    runPollInFlightRef.current = true
    try {
      const run = await api.get<Record<string, unknown>>(`/v1/runs/${encodeURIComponent(runID)}`)
      const status = normalizeRunStatus(run.status) || "unknown"
      const discoveredSessionID = safeText(run.session_id)
      const updatedAt = safeText(run.updated_at)
      const output = String(run.output || "")
      const errorText = safeText(run.error)

      setState((prev) => {
        let transcript = prev.transcript
        let polling = prev.polling
        let runPollingEnabled = prev.runPollingEnabled
        let sessionPollIntervalMS = prev.sessionPollIntervalMS
        let resumableInterruption = prev.resumableInterruption
        let lastErrorSummary = prev.lastErrorSummary

        if (isTerminalStatus(status)) {
          if (status === "completed") {
            const message = safeText(output) || "Run completed without assistant output. Open trace or tool activity for details."
            transcript = replacePendingAssistantInTranscript(transcript, message)
            resumableInterruption = buildResumableInterruption(message, {
              runID,
              sessionID: discoveredSessionID || prev.currentSessionID,
              updatedAt,
              source: "poll.completed",
              status,
            })
            if (!errorText) {
              lastErrorSummary = ""
            }
          } else if (status === "failed") {
            const message = errorText || "Run failed."
            transcript = replacePendingAssistantInTranscript(transcript, `Error: ${message}`)
            resumableInterruption = buildResumableInterruption(message, {
              runID,
              sessionID: discoveredSessionID || prev.currentSessionID,
              updatedAt,
              source: "poll.failed",
              status,
              error: message,
              streamError: prev.streamError,
              interrupted: run.interrupted,
            })
            lastErrorSummary = compactText(message, 280)
          } else if (status === "canceled") {
            transcript = replacePendingAssistantInTranscript(transcript, "Run canceled.")
            resumableInterruption = buildResumableInterruption("Run canceled.", {
              runID,
              sessionID: discoveredSessionID || prev.currentSessionID,
              updatedAt,
              source: "poll.canceled",
              status,
              error: errorText,
              streamError: prev.streamError,
              interrupted: run.interrupted,
            })
          }
          polling = false
          runPollingEnabled = true
          sessionPollIntervalMS = SESSION_POLL_MS
        } else if (!prev.showToolActivityInTranscript) {
          const elapsedSeconds =
            prev.currentRunStartedAtMs > 0
              ? Math.max(1, Math.floor((Date.now() - prev.currentRunStartedAtMs) / 1000))
              : 0
          let progress = `Still working on run ${runID} (status: ${status}`
          if (elapsedSeconds > 0) {
            progress += `, ${elapsedSeconds}s elapsed`
          }
          progress += ")."
          if (prev.latestToolActivity) {
            const detail = compactText(
              firstNonEmpty(
                prev.latestToolActivity.summary,
                prev.latestToolActivity.errorText,
                prev.latestToolActivity.outputText,
                prev.latestToolActivity.argsText
              ),
              180
            )
            progress += detail
              ? ` Latest tool activity: ${prev.latestToolActivity.tool} - ${detail}`
              : ` Latest tool activity: ${prev.latestToolActivity.tool}.`
          }
          transcript = upsertPendingAssistantInTranscript(transcript, progress)
        }

        return {
          ...prev,
          transcript,
          currentRunStatus: status,
          currentRunLastUpdatedAt: updatedAt,
          currentRunLastOutput: output,
          currentSessionID: discoveredSessionID || prev.currentSessionID,
          polling,
          runPollingEnabled,
          sessionPollIntervalMS,
          currentStreamingText: isTerminalStatus(status) ? "" : prev.currentStreamingText,
          resumableInterruption,
          lastErrorSummary: errorText ? compactText(errorText, 280) : lastErrorSummary,
        }
      })
    } catch (error) {
      const message = compactText((error as Error)?.message || String(error), 280)
      setState((prev) => ({
        ...prev,
        lastErrorSummary: message,
      }))
    } finally {
      runPollInFlightRef.current = false
    }
  }, [])

  const pollSessionMessagesOnce = useCallback(async () => {
    if (sessionPollInFlightRef.current) {
      return
    }
    const sessionID = safeText(stateRef.current.currentSessionID)
    if (!sessionID) {
      return
    }

    sessionPollInFlightRef.current = true
    try {
      const payload = await api.get<ChatSessionMessagesPayload>(
        `/api/admin/chat/sessions/${encodeURIComponent(sessionID)}/messages?limit=${encodeURIComponent(
          String(SESSION_MESSAGES_LIMIT)
        )}`
      )
      applySessionMessagesPayload(payload)
    } catch (error) {
      const message = compactText((error as Error)?.message || String(error), 280)
      setState((prev) => ({
        ...prev,
        lastErrorSummary: message,
      }))
    } finally {
      sessionPollInFlightRef.current = false
    }
  }, [applySessionMessagesPayload])

  const sendMessage = useCallback(
    async (explicitMessage = "", options?: { preserveDraft?: boolean; resumeInterruptedRun?: boolean }) => {
      if (stateRef.current.sendPending) {
        return
      }
      const directMessage = safeText(explicitMessage)
      const message = directMessage || safeText(stateRef.current.draft)
      if (!message) {
        return
      }
      const preserveDraft = Boolean(options?.preserveDraft && directMessage)
      if (!options?.resumeInterruptedRun) {
        clearResumableInterruption()
      }

      setState((prev) => {
        const transcript: TranscriptItem[] = [
          ...prev.transcript,
          {
            role: "user",
            content: message,
            pending: false,
            ts: new Date().toISOString(),
            source: "local",
          },
        ]
        if (!prev.showToolActivityInTranscript) {
          transcript.push({
            role: "assistant",
            content: "Thinking...",
            pending: true,
            ts: new Date().toISOString(),
            source: "local",
          })
        }
        return {
          ...prev,
          sendPending: true,
          lastPrompt: message,
          sendError: "",
          debugCopyStatus: "",
          draft: preserveDraft ? prev.draft : "",
          transcript,
          streamError: "",
        }
      })

      try {
        const payload = await api.post<Record<string, unknown>>("/v1/chat/messages", {
          user_id: CHAT_DEFAULTS.userID,
          room_id: CHAT_DEFAULTS.roomID,
          agent_id: safeText(stateRef.current.selectedAgentID) || CHAT_DEFAULTS.agentID,
          message,
        })

        const runID = safeText(payload.id)
        const sessionID = safeText(payload.session_id)
        const runStatus = normalizeRunStatus(payload.status) || "queued"
        if (runID) {
          setState((prev) => ({
            ...prev,
            currentRunID: runID,
            currentRunStatus: runStatus,
            currentRunLastUpdatedAt: "",
            currentRunLastOutput: "",
            currentRunStartedAtMs: Date.now(),
            currentSessionID: sessionID || prev.currentSessionID,
            streamLastEventID: 0,
            currentStreamingText: "",
            streamToolEvents: [],
            latestToolActivity: null,
            polling: true,
            runPollingEnabled: true,
            sessionPollIntervalMS: SESSION_POLL_MS,
          }))

          if (!stateRef.current.showToolActivityInTranscript) {
            setState((prev) => ({
              ...prev,
              transcript: upsertPendingAssistantInTranscript(
                prev.transcript,
                safeText(payload.response) || `Working on it now. Run ${runID} is ${runStatus}.`
              ),
            }))
          }

          startPolling(runID, sessionID)
          void connectRunEventStream(runID)
        } else {
          const directResponse = safeText(payload.response) || "Request accepted."
          setState((prev) => ({
            ...prev,
            transcript: replacePendingAssistantInTranscript(prev.transcript, directResponse),
            currentRunStatus: "idle",
            currentRunStartedAtMs: 0,
            polling: false,
          }))
          resetStreamingState({ resetLastEventID: true })
        }
      } catch (error) {
        const messageText = (error as Error)?.message || String(error)
        setState((prev) => ({
          ...prev,
          sendError: messageText,
          lastErrorSummary: compactText(messageText, 280),
          transcript: replacePendingAssistantInTranscript(prev.transcript, `Error: ${messageText}`),
        }))
      } finally {
        setState((prev) => ({
          ...prev,
          sendPending: false,
        }))
      }
    },
    [clearResumableInterruption, connectRunEventStream, resetStreamingState, startPolling]
  )

  const sendContinuationPrompt = useCallback(() => {
    if (stateRef.current.sendPending || !shouldOfferContinueAction) {
      return
    }
    void sendMessage(RESUME_INTERRUPTED_RUN_MESSAGE, {
      preserveDraft: true,
      resumeInterruptedRun: true,
    })
  }, [sendMessage, shouldOfferContinueAction])

  const retryLastPrompt = useCallback(() => {
    const message = safeText(state.lastPrompt)
    if (!message || state.sendPending) {
      return
    }
    setState((prev) => ({
      ...prev,
      debugCopyStatus: "",
    }))
    void sendMessage(message)
  }, [sendMessage, state.lastPrompt, state.sendPending])

  const copyDebugBundle = useCallback(async () => {
    try {
      const payload = {
        context: {
          user_id: CHAT_DEFAULTS.userID,
          room_id: CHAT_DEFAULTS.roomID,
          selected_agent_id: safeText(stateRef.current.selectedAgentID) || CHAT_DEFAULTS.agentID,
          active_agent_id: safeText(stateRef.current.activeAgentID),
        },
        run: {
          id: safeText(stateRef.current.currentRunID),
          status: safeText(stateRef.current.currentRunStatus),
          session_id: safeText(stateRef.current.currentSessionID),
          updated_at: safeText(stateRef.current.currentRunLastUpdatedAt),
          last_output_preview: compactText(stateRef.current.currentRunLastOutput, 600),
        },
        activity: {
          polling: Boolean(stateRef.current.polling),
          latest_tool: stateRef.current.latestToolActivity,
          loop_risk: stateRef.current.loopRisk,
          last_error_summary: safeText(stateRef.current.lastErrorSummary),
          send_error: stateRef.current.sendError || null,
          stream_error: stateRef.current.streamError || null,
        },
        transcript_tail: stateRef.current.transcript.slice(-10),
        timestamp: new Date().toISOString(),
      }
      const copied = await copyToClipboardWithFallback(`${JSON.stringify(payload, null, 2)}\n`)
      const statusMessage = copied.usedFallback
        ? "Debug bundle copied (clipboard fallback)."
        : "Debug bundle copied."
      setState((prev) => ({
        ...prev,
        debugCopyStatus: statusMessage,
      }))
      toast({ description: statusMessage })
    } catch (error) {
      setState((prev) => ({
        ...prev,
        debugCopyStatus: `Copy failed: ${(error as Error)?.message || String(error)}`,
      }))
    }
  }, [toast])

  const cancelCurrentChatRun = useCallback(async () => {
    const runID = safeText(stateRef.current.currentRunID)
    const runStatus = normalizeRunStatus(stateRef.current.currentRunStatus)
    if (!runID || isTerminalStatus(runStatus) || isCancelingStatus(runStatus)) {
      return
    }

    setState((prev) => ({
      ...prev,
      currentRunStatus: "canceling",
      polling: true,
      runPollingEnabled: true,
      sessionPollIntervalMS: SESSION_POLL_MS,
      sendError: "",
      debugCopyStatus: `Stopping run ${runID}...`,
    }))

    try {
      const response = await api.post<Record<string, unknown>>(`/v1/runs/${encodeURIComponent(runID)}/cancel`, {})
      const nextStatus = resolveCancelRunStatus(response?.status, response?.cancelled)
      const statusMessage =
        nextStatus === "canceled"
          ? `Run ${runID} canceled.`
          : nextStatus === "failed"
          ? `Run ${runID} failed while canceling.`
          : nextStatus === "completed"
          ? `Run ${runID} completed before cancellation finished.`
          : `Stopping run ${runID}...`

      const updatedAt = new Date().toISOString()
      setState((prev) => ({
        ...prev,
        currentRunStatus: nextStatus,
        currentRunLastUpdatedAt: updatedAt,
        polling: !isTerminalStatus(nextStatus),
        runPollingEnabled: true,
        sessionPollIntervalMS: SESSION_POLL_MS,
        currentStreamingText: isTerminalStatus(nextStatus) ? "" : prev.currentStreamingText,
        debugCopyStatus: statusMessage,
        sendError: "",
        resumableInterruption:
          nextStatus === "canceled" || nextStatus === "failed"
            ? buildResumableInterruption(statusMessage, {
                runID,
                sessionID: prev.currentSessionID,
                updatedAt,
                source: "cancel.request",
                status: nextStatus,
                error: statusMessage,
                streamError: prev.streamError,
                interrupted: true,
              }) || prev.resumableInterruption
            : nextStatus === "completed" && safeText(prev.resumableInterruption?.runID) === runID
            ? null
            : prev.resumableInterruption,
      }))
      toast({ description: statusMessage })
    } catch (error) {
      setState((prev) => ({
        ...prev,
        sendError: (error as Error)?.message || String(error),
      }))
    }
  }, [toast])

  const cancelObservedSubagentRun = useCallback(async (runID: string) => {
    const targetRunID = safeText(runID)
    if (!targetRunID) {
      return
    }
    try {
      await api.post("/api/admin/monitor/runs/control", { action: "cancel", run_id: targetRunID })
      setState((prev) => ({
        ...prev,
        debugCopyStatus: `Cancellation requested for subagent ${targetRunID}.`,
      }))
      toast({ description: `Cancellation requested for subagent ${targetRunID}.` })
    } catch (error) {
      setState((prev) => ({
        ...prev,
        sendError: (error as Error)?.message || String(error),
      }))
    }
  }, [toast])

  const switchAgent = useCallback(
    async (nextAgentID: string) => {
      const agentID = safeText(nextAgentID) || CHAT_DEFAULTS.agentID
      if (stateRef.current.switchAgentPending) {
        return
      }

      setState((prev) => ({
        ...prev,
        switchAgentPending: true,
        switchAgentError: "",
      }))

      if (streamAbortControllerRef.current) {
        streamAbortControllerRef.current.abort()
        streamAbortControllerRef.current = null
      }

      try {
        const payload = await api.post<{
          agents?: unknown
          active_agent?: unknown
          selected_agent?: unknown
          profile_context?: unknown
          agents_config?: unknown
        }>("/api/admin/agents", {
          channel: "dashboard",
          user_id: CHAT_DEFAULTS.userID,
          room_id: CHAT_DEFAULTS.roomID,
          agent_id: agentID,
        })

        const agents = Array.isArray(payload?.agents)
          ? payload.agents.map((item) => safeText(item)).filter(Boolean)
          : []
        const selectedAgentID =
          safeText(payload?.selected_agent) || safeText(payload?.active_agent) || agentID
        const activeAgentID = safeText(payload?.active_agent) || selectedAgentID

        setState((prev) => ({
          ...prev,
          availableAgents: agents.length ? agents : prev.availableAgents,
          selectedAgentID,
          activeAgentID,
          switchAgentPending: false,
          switchAgentError: "",
          agentProfileContext:
            payload?.profile_context && typeof payload.profile_context === "object"
              ? (payload.profile_context as Record<string, unknown>)
              : null,
          agentGlobalConfig:
            payload?.agents_config && typeof payload.agents_config === "object"
              ? (payload.agents_config as Record<string, unknown>)
              : null,
          currentRunID: "",
          currentRunStatus: "idle",
          currentRunStartedAtMs: 0,
          currentSessionID: "",
          currentRunLastUpdatedAt: "",
          currentRunLastOutput: "",
          latestToolActivity: null,
          streamToolEvents: [],
          resumableInterruption: null,
          lastErrorSummary: "",
          expandedToolEntries: {},
          expandedErrorEntries: {},
          loopRisk: createLoopRiskDefault(),
          polling: false,
          runPollingEnabled: true,
          sessionPollIntervalMS: SESSION_POLL_MS,
          streamActive: false,
          streamLastEventID: 0,
          currentStreamingText: "",
          currentStreamRunID: "",
          streamError: "",
          sendError: "",
          debugCopyStatus: "",
          transcript: [],
        }))

        const sessionID = await ensureCurrentSessionID()
        if (sessionID) {
          await refreshTranscriptFromCurrentSession()
        }
      } catch (error) {
        setState((prev) => ({
          ...prev,
          switchAgentPending: false,
          switchAgentError: (error as Error)?.message || String(error),
        }))
      }
    },
    [ensureCurrentSessionID, refreshTranscriptFromCurrentSession]
  )

  const toggleToolEntryExpanded = useCallback((eventKey: string, kind: "tool" | "error") => {
    setState((prev) => {
      if (kind === "error") {
        return {
          ...prev,
          expandedErrorEntries: {
            ...prev.expandedErrorEntries,
            [eventKey]: !Boolean(prev.expandedErrorEntries[eventKey]),
          },
        }
      }
      return {
        ...prev,
        expandedToolEntries: {
          ...prev.expandedToolEntries,
          [eventKey]: !Boolean(prev.expandedToolEntries[eventKey]),
        },
      }
    })
  }, [])

  const toggleToolTimelineMode = useCallback(async () => {
    setState((prev) => {
      const nextShowToolTimeline = !prev.showToolActivityInTranscript
      let transcript = prev.transcript
      if (!nextShowToolTimeline) {
        transcript = transcript.filter((item) => item.role !== "tool")
      } else {
        const existingToolKeys = new Set(
          transcript
            .filter((item) => item.role === "tool" && safeText(item.toolKey))
            .map((item) => item.toolKey as string)
        )
        const streamedToolItems = prev.streamToolEvents
          .map((event) => ({
            role: "tool" as const,
            content: firstNonEmpty(event.summary, event.errorText, event.outputText, event.argsText, event.tool),
            pending: false,
            ts: String(event.ts || new Date().toISOString()),
            toolEvent: event,
            toolKey: toolEventKey(event),
            source: "stream" as const,
          }))
          .filter((item) => !existingToolKeys.has(item.toolKey))
        transcript = [...transcript, ...streamedToolItems]
      }
      return {
        ...prev,
        showToolActivityInTranscript: nextShowToolTimeline,
        transcript,
      }
    })
    try {
      await refreshTranscriptFromCurrentSession()
    } catch {
      // keep current transcript if refresh fails
    }
  }, [refreshTranscriptFromCurrentSession])

  const retryStreamConnection = useCallback(() => {
    const runID = safeText(stateRef.current.currentRunID)
    if (!runID || isTerminalStatus(stateRef.current.currentRunStatus)) {
      return
    }
    void connectRunEventStream(runID)
  }, [connectRunEventStream])

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      await refreshAvailableAgents()
      if (cancelled) {
        return
      }
      const sessionID = await ensureCurrentSessionID()
      if (cancelled || !sessionID) {
        return
      }
      try {
        await refreshTranscriptFromCurrentSession()
      } catch {
        // ignore bootstrap fetch failures
      }
    })()

    return () => {
      cancelled = true
    }
  }, [ensureCurrentSessionID, refreshAvailableAgents, refreshTranscriptFromCurrentSession])

  useEffect(() => {
    if (!state.polling || !state.runPollingEnabled || !safeText(state.currentRunID)) {
      return
    }
    const tick = async () => {
      await pollRunOnce()
    }
    void tick()
    const timer = window.setInterval(() => {
      void tick()
    }, RUN_POLL_MS)
    return () => {
      window.clearInterval(timer)
    }
  }, [pollRunOnce, state.currentRunID, state.polling, state.runPollingEnabled])

  useEffect(() => {
    const sessionID = safeText(state.currentSessionID)
    if (!sessionID) {
      return
    }
    const interval = state.polling ? state.sessionPollIntervalMS : SESSION_POLL_MS
    const tick = async () => {
      await pollSessionMessagesOnce()
    }
    void tick()
    const timer = window.setInterval(() => {
      void tick()
    }, interval)
    return () => {
      window.clearInterval(timer)
    }
  }, [pollSessionMessagesOnce, state.currentSessionID, state.polling, state.sessionPollIntervalMS])

  useLayoutEffect(() => {
    const transcript = transcriptRef.current
    if (!transcript) {
      return
    }
    if (transcriptPinnedRef.current) {
      transcript.scrollTop = transcript.scrollHeight
      return
    }
    const maxScrollTop = Math.max(0, transcript.scrollHeight - transcript.clientHeight)
    transcript.scrollTop = Math.min(transcriptScrollTopRef.current, maxScrollTop)
  }, [state.transcript])

  const onTranscriptScroll = useCallback(() => {
    const transcript = transcriptRef.current
    if (!transcript) {
      return
    }
    const distance = transcript.scrollHeight - (transcript.scrollTop + transcript.clientHeight)
    transcriptPinnedRef.current = distance <= 36
    transcriptScrollTopRef.current = transcript.scrollTop
  }, [])

  const onPaneResizePointerDown = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) {
      return
    }
    event.preventDefault()
    const startX = event.clientX
    const startWidth = stateRef.current.activityPaneWidth

    const onMove = (moveEvent: PointerEvent) => {
      const nextWidth = clamp(
        startWidth - (moveEvent.clientX - startX),
        CHAT_ACTIVITY_WIDTH_MIN,
        CHAT_ACTIVITY_WIDTH_MAX
      )
      setState((prev) => ({
        ...prev,
        activityPaneWidth: nextWidth,
      }))
    }

    const onUp = () => {
      window.removeEventListener("pointermove", onMove)
      window.removeEventListener("pointerup", onUp)
    }

    window.addEventListener("pointermove", onMove)
    window.addEventListener("pointerup", onUp, { once: true })
  }, [])

  const onPaneResizeKeyDown = useCallback((event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "ArrowLeft") {
      event.preventDefault()
      setState((prev) => ({
        ...prev,
        activityPaneWidth: clamp(prev.activityPaneWidth + 16, CHAT_ACTIVITY_WIDTH_MIN, CHAT_ACTIVITY_WIDTH_MAX),
      }))
    } else if (event.key === "ArrowRight") {
      event.preventDefault()
      setState((prev) => ({
        ...prev,
        activityPaneWidth: clamp(prev.activityPaneWidth - 16, CHAT_ACTIVITY_WIDTH_MIN, CHAT_ACTIVITY_WIDTH_MAX),
      }))
    }
  }, [])

  const runStatus = normalizeRunStatus(state.currentRunStatus)
  const cancelInFlight = isCancelingStatus(runStatus)
  const stopButtonDisabled = !safeText(state.currentRunID) || isTerminalStatus(runStatus) || cancelInFlight
  const stopButtonLabel = cancelInFlight ? "Stopping..." : "Stop current run"
  const sendDisabled = state.sendPending || safeText(state.draft).length === 0
  const failedToolEvents = state.streamToolEvents.filter((event) => event.status === "failed").slice().reverse()

  return (
    <div className="space-y-4 p-6" data-testid="chat-page">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Chat</h2>
        <p className="text-sm text-muted-foreground">
          Send prompts, watch streaming model output, and inspect live tool activity in one place.
        </p>
      </div>

      <div
        className="grid rounded-lg border bg-card"
        style={{
          gridTemplateColumns: `minmax(0, 1fr) 8px ${state.activityPaneWidth}px`,
          minHeight: "640px",
        }}
      >
        <section className="flex min-w-0 flex-col">
          <div className="flex items-center justify-between border-b px-4 py-3">
            <h3 className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">Transcript</h3>
            <Button variant="outline" size="sm" onClick={() => void toggleToolTimelineMode()}>
              {state.showToolActivityInTranscript ? "Tool timeline: on" : "Tool timeline: off"}
            </Button>
          </div>

          <div
            ref={transcriptRef}
            onScroll={onTranscriptScroll}
            data-testid="chat-transcript"
            className="h-[420px] space-y-3 overflow-y-auto px-4 py-4"
          >
            {!state.transcript.length ? (
              <p className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">
                Send a message to begin.
              </p>
            ) : (
              state.transcript.map((item, index) => {
                if (item.role === "tool" && item.toolEvent) {
                  const event = item.toolEvent
                  const eventKey = item.toolKey || toolEventKey(event)
                  const expanded = Boolean(state.expandedToolEntries[eventKey])
                  return (
                    <article
                      key={`${eventKey}-${index}`}
                      data-testid="transcript-tool-card"
                      className={cn(
                        "rounded-md border p-3",
                        event.status === "failed"
                          ? "border-destructive/40 bg-destructive/5"
                          : "border-border bg-muted/20"
                      )}
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div className="min-w-0">
                          <p className="text-sm font-medium">
                            {event.tool}
                            {event.toolCallID ? ` [${event.toolCallID}]` : ""}
                          </p>
                          <p className="text-xs text-muted-foreground">
                            {event.status}
                            {event.durationMS > 0 ? ` · ${event.durationMS}ms` : ""}
                            {event.ts ? ` · ${formatDateTime(event.ts)}` : ""}
                            {event.runID ? ` · run ${event.runID}` : ""}
                          </p>
                        </div>
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={`${expanded ? "Collapse" : "Expand"} tool ${event.tool}`}
                          onClick={() => toggleToolEntryExpanded(eventKey, "tool")}
                        >
                          {expanded ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
                        </Button>
                      </div>
                      <p className="mt-2 text-xs text-muted-foreground">
                        {compactText(
                          firstNonEmpty(event.summary, event.errorText, event.outputText, event.argsText),
                          expanded ? 600 : 220
                        ) || "(no tool summary)"}
                      </p>
                      {expanded ? (
                        <div className="mt-3 space-y-3">
                          {safeText(event.argsText) ? (
                            <div>
                              <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                                Arguments
                              </p>
                              <pre className="mt-1 max-h-48 overflow-auto rounded border bg-background p-2 text-xs">
                                {formatToolDetailValue(event.argsText)}
                              </pre>
                            </div>
                          ) : null}
                          {safeText(event.outputText) ? (
                            <div>
                              <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                                Output
                              </p>
                              <pre className="mt-1 max-h-48 overflow-auto rounded border bg-background p-2 text-xs">
                                {formatToolDetailValue(event.outputText)}
                              </pre>
                            </div>
                          ) : null}
                          {safeText(event.errorText) ? (
                            <div>
                              <p className="text-xs font-semibold uppercase tracking-wide text-destructive">Error</p>
                              <pre className="mt-1 max-h-48 overflow-auto rounded border border-destructive/40 bg-destructive/5 p-2 text-xs text-destructive">
                                {formatToolDetailValue(event.errorText)}
                              </pre>
                            </div>
                          ) : null}
                        </div>
                      ) : null}
                    </article>
                  )
                }

                return (
                  <article
                    key={`${item.role}-${item.ts}-${index}`}
                    className={cn(
                      "rounded-md border p-3",
                      item.role === "user"
                        ? "ml-12 border-primary/20 bg-primary/5"
                        : "mr-12 border-border bg-muted/20",
                      item.pending && item.role === "assistant" ? "animate-pulse" : ""
                    )}
                  >
                    <p className="text-xs uppercase tracking-wide text-muted-foreground">
                      {item.role} · {formatDateTime(item.ts)}
                    </p>
                    <pre className="mt-2 whitespace-pre-wrap break-words text-sm leading-6">{item.content}</pre>
                    {!state.showToolActivityInTranscript &&
                    item.pending &&
                    item.role === "assistant" &&
                    state.latestToolActivity ? (
                      <p
                        className={cn(
                          "mt-2 text-xs",
                          state.latestToolActivity.status === "failed" ? "text-destructive" : "text-muted-foreground"
                        )}
                      >
                        Latest tool: {state.latestToolActivity.tool}
                      </p>
                    ) : null}
                  </article>
                )
              })
            )}
          </div>

          <form
            className="space-y-3 border-t px-4 py-4"
            onSubmit={(event) => {
              event.preventDefault()
              void sendMessage()
            }}
          >
            <div className="space-y-2">
              <label htmlFor="chat-composer" className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                Message composer
              </label>
              <textarea
                id="chat-composer"
                aria-label="Message composer"
                className="min-h-[96px] w-full resize-y rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm outline-none focus-visible:ring-1 focus-visible:ring-ring"
                placeholder="Ask the agent to investigate, summarize, or run a workflow..."
                value={state.draft}
                onKeyDown={(event) => {
                  if (event.key !== "Enter" || event.shiftKey) {
                    return
                  }
                  if (event.nativeEvent.isComposing) {
                    return
                  }
                  event.preventDefault()
                  if (!sendDisabled) {
                    void sendMessage()
                  }
                }}
                onChange={(event) => {
                  setState((prev) => ({
                    ...prev,
                    draft: event.target.value,
                  }))
                }}
              />
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs text-muted-foreground">
                Context: {CHAT_DEFAULTS.userID} / {CHAT_DEFAULTS.roomID} / {safeText(state.selectedAgentID) || CHAT_DEFAULTS.agentID}
              </span>

              <label className="sr-only" htmlFor="chat-agent-picker">
                Agent picker
              </label>
              <select
                id="chat-agent-picker"
                aria-label="Agent picker"
                className="h-9 rounded-md border border-input bg-background px-2 text-sm"
                value={state.selectedAgentID}
                disabled={state.switchAgentPending || state.sendPending}
                onChange={(event) => {
                  void switchAgent(event.target.value)
                }}
              >
                {state.availableAgents.map((agentID) => (
                  <option key={agentID} value={agentID}>
                    {agentID}
                  </option>
                ))}
              </select>

              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={state.switchAgentPending}
                onClick={() => void refreshAvailableAgents()}
              >
                <RefreshCw className="h-3.5 w-3.5" />
                Refresh agents
              </Button>

              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={state.sendPending || !safeText(state.lastPrompt)}
                onClick={retryLastPrompt}
              >
                Retry
              </Button>

              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={state.sendPending || !shouldOfferContinueAction}
                onClick={sendContinuationPrompt}
              >
                Resume interrupted run
              </Button>

              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={stopButtonDisabled}
                onClick={() => void cancelCurrentChatRun()}
              >
                {stopButtonLabel}
              </Button>

              <Button type="submit" size="sm" disabled={sendDisabled}>
                {state.sendPending ? "Sending..." : "Send"}
              </Button>
            </div>

            {state.sendError ? <p className="text-sm text-destructive">Send failed: {state.sendError}</p> : null}
            {state.switchAgentError ? (
              <p className="text-sm text-destructive">Agent switch failed: {state.switchAgentError}</p>
            ) : null}
            {state.debugCopyStatus ? <p className="text-xs text-muted-foreground">{state.debugCopyStatus}</p> : null}
          </form>
        </section>

        <div
          role="separator"
          tabIndex={0}
          aria-label="Resize chat activity pane"
          aria-valuemin={CHAT_ACTIVITY_WIDTH_MIN}
          aria-valuemax={CHAT_ACTIVITY_WIDTH_MAX}
          aria-valuenow={state.activityPaneWidth}
          className="cursor-col-resize bg-border/40 hover:bg-border focus:bg-border"
          onPointerDown={onPaneResizePointerDown}
          onKeyDown={onPaneResizeKeyDown}
        />

        <aside className="min-w-0 space-y-3 overflow-y-auto border-l bg-muted/20 p-3">
          {state.streamError ? (
            <Card className="border-destructive/40 bg-destructive/5">
              <CardHeader className="pb-2">
                <CardTitle className="text-sm">SSE connection error</CardTitle>
              </CardHeader>
              <CardContent className="space-y-2 text-sm">
                <p className="text-destructive">{state.streamError}</p>
                <Button variant="outline" size="sm" onClick={retryStreamConnection}>
                  Retry stream
                </Button>
              </CardContent>
            </Card>
          ) : null}

          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">Live Activity</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 text-sm">
              <p>Run: {safeText(state.currentRunID) || "-"}</p>
              <p>
                Status: <strong>{safeText(state.currentRunStatus) || "idle"}</strong>
              </p>
              <p>Stream: {state.streamActive ? "connected" : "idle"}</p>
              <p>Tool count: {state.streamToolEvents.length}</p>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">Agent Control</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 text-sm">
              <p>Selected: {safeText(state.selectedAgentID) || CHAT_DEFAULTS.agentID}</p>
              <p className="text-muted-foreground">Active pointer: {safeText(state.activeAgentID) || "-"}</p>
              <p className="text-xs text-muted-foreground">
                Profile: {Boolean(state.agentProfileContext?.exists) ? "explicit" : "inherited"} · enabled=
                {String(
                  typeof state.agentProfileContext?.enabled === "boolean"
                    ? state.agentProfileContext.enabled
                    : true
                )}
              </p>
              <p className="text-xs text-muted-foreground">
                Model override: {firstNonEmpty(state.agentProfileContext?.model_provider, "(provider unset)")} /{" "}
                {firstNonEmpty(state.agentProfileContext?.model_name, "(name unset)")}
              </p>
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  navigate(settingsProfilePath(state.selectedAgentID))
                }}
              >
                Edit profile in Settings
              </Button>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">Session metadata</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 text-sm">
              <p>Session: {safeText(state.currentSessionID) || "(pending)"}</p>
              <p className="text-muted-foreground">
                Last updated: {state.currentRunLastUpdatedAt ? formatDateTime(state.currentRunLastUpdatedAt) : "-"}
              </p>
              <div className="flex flex-wrap gap-2">
                <Button variant="outline" size="sm" onClick={() => void copyDebugBundle()}>
                  Copy debug bundle
                </Button>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">Latest Tool Activity</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 text-sm">
              {state.latestToolActivity ? (
                <>
                  <p>
                    {state.latestToolActivity.tool} · {state.latestToolActivity.status}
                    {state.latestToolActivity.durationMS > 0 ? ` · ${state.latestToolActivity.durationMS}ms` : ""}
                  </p>
                  <p className="text-muted-foreground">
                    {compactText(
                      firstNonEmpty(
                        state.latestToolActivity.summary,
                        state.latestToolActivity.errorText,
                        state.latestToolActivity.outputText,
                        state.latestToolActivity.argsText
                      ),
                      260
                    ) || "(no summary)"}
                  </p>
                </>
              ) : (
                <p className="text-muted-foreground">No tool events observed yet.</p>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">Last Error</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 text-sm">
              <p className={state.lastErrorSummary ? "text-destructive" : "text-muted-foreground"}>
                {state.lastErrorSummary || "No recent errors."}
              </p>
              {shouldOfferContinueAction ? (
                <Button size="sm" variant="outline" onClick={sendContinuationPrompt}>
                  Resume interrupted run
                </Button>
              ) : null}
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">Observed Subagent Runs ({observedSubagentRuns.length})</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 text-sm">
              {observedSubagentRuns.length ? (
                observedSubagentRuns.map((item) => (
                  <div key={item.runID} className="rounded border p-2">
                    <p className="font-medium">
                      {item.agentID} · {item.runID}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {compactText(item.summary, 180) || "subagent completed"}
                      {item.durationMS > 0 ? ` · ${item.durationMS}ms` : ""}
                      {item.toolCalls > 0 ? ` · ${item.toolCalls} tools` : ""}
                    </p>
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      className="mt-2"
                      onClick={() => void cancelObservedSubagentRun(item.runID)}
                    >
                      Stop subagent
                    </Button>
                  </div>
                ))
              ) : (
                <p className="text-muted-foreground">
                  No subagent runs observed from the current chat activity yet.
                </p>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">Tool Calls &amp; Results ({state.streamToolEvents.length})</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 text-sm">
              {state.streamToolEvents.length ? (
                state.streamToolEvents
                  .slice()
                  .reverse()
                  .map((event) => {
                    const eventKey = toolEventKey(event)
                    const expanded = Boolean(state.expandedToolEntries[eventKey])
                    return (
                      <div key={eventKey} className="rounded border p-2">
                        <div className="flex items-center justify-between gap-2">
                          <p className="font-medium">
                            {event.tool}
                            {event.toolCallID ? ` [${event.toolCallID}]` : ""}
                          </p>
                          <Button
                            variant="ghost"
                            size="icon"
                            aria-label={`${expanded ? "Collapse" : "Expand"} tool ${event.tool}`}
                            onClick={() => toggleToolEntryExpanded(eventKey, "tool")}
                          >
                            {expanded ? (
                              <ChevronDown className="h-4 w-4" />
                            ) : (
                              <ChevronRight className="h-4 w-4" />
                            )}
                          </Button>
                        </div>
                        <p className="text-xs text-muted-foreground">
                          {event.status}
                          {event.durationMS > 0 ? ` · ${event.durationMS}ms` : ""}
                          {event.ts ? ` · ${formatDateTime(event.ts)}` : ""}
                        </p>
                        <p className="mt-1 text-xs text-muted-foreground">
                          {compactText(firstNonEmpty(event.summary, event.errorText, event.outputText, event.argsText), 200)}
                        </p>
                        {expanded ? (
                          <div className="mt-2 space-y-2">
                            {safeText(event.argsText) ? (
                              <div>
                                <p className="text-xs font-semibold uppercase text-muted-foreground">Arguments</p>
                                <pre className="max-h-36 overflow-auto rounded border bg-background p-2 text-xs">
                                  {formatToolDetailValue(event.argsText)}
                                </pre>
                              </div>
                            ) : null}
                            {safeText(event.outputText) ? (
                              <div>
                                <p className="text-xs font-semibold uppercase text-muted-foreground">Output</p>
                                <pre className="max-h-36 overflow-auto rounded border bg-background p-2 text-xs">
                                  {formatToolDetailValue(event.outputText)}
                                </pre>
                              </div>
                            ) : null}
                            {safeText(event.errorText) ? (
                              <div>
                                <p className="text-xs font-semibold uppercase text-destructive">Error</p>
                                <pre className="max-h-36 overflow-auto rounded border border-destructive/40 bg-destructive/5 p-2 text-xs text-destructive">
                                  {formatToolDetailValue(event.errorText)}
                                </pre>
                              </div>
                            ) : null}
                          </div>
                        ) : null}
                      </div>
                    )
                  })
              ) : (
                <p className="text-muted-foreground">No tool calls captured yet.</p>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">Recent Tool Errors ({failedToolEvents.length})</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 text-sm">
              {failedToolEvents.length ? (
                failedToolEvents.map((event) => {
                  const eventKey = toolEventKey(event)
                  const expanded = Boolean(state.expandedErrorEntries[eventKey])
                  return (
                    <div key={`error-${eventKey}`} className="rounded border border-destructive/30 bg-destructive/5 p-2">
                      <div className="flex items-center justify-between gap-2">
                        <p className="font-medium text-destructive">{event.tool}</p>
                        <Button
                          variant="ghost"
                          size="icon"
                          aria-label={`${expanded ? "Collapse" : "Expand"} tool ${event.tool}`}
                          onClick={() => toggleToolEntryExpanded(eventKey, "error")}
                        >
                          {expanded ? (
                            <ChevronDown className="h-4 w-4" />
                          ) : (
                            <ChevronRight className="h-4 w-4" />
                          )}
                        </Button>
                      </div>
                      <p className="text-xs text-destructive/90">
                        {compactText(firstNonEmpty(event.errorText, event.summary), 220)}
                      </p>
                      {expanded && safeText(event.errorText) ? (
                        <pre className="mt-2 max-h-36 overflow-auto rounded border border-destructive/40 bg-destructive/10 p-2 text-xs text-destructive">
                          {formatToolDetailValue(event.errorText)}
                        </pre>
                      ) : null}
                    </div>
                  )
                })
              ) : (
                <p className="text-muted-foreground">No tool errors in the current activity window.</p>
              )}
            </CardContent>
          </Card>

          <Card className={cn("border", state.loopRisk.level === "high" ? "border-destructive" : "")}> 
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">Loop Risk</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 text-sm">
              <div className="flex items-center gap-2">
                <Badge variant={state.loopRisk.level === "high" ? "destructive" : "secondary"}>
                  {state.loopRisk.level.toUpperCase()}
                </Badge>
                <span>score {state.loopRisk.score}</span>
              </div>
              <p className="text-muted-foreground">
                {state.loopRisk.failureCount}/{state.loopRisk.windowSize} failures in recent tool window, max repeat {" "}
                {state.loopRisk.repeatCount}.
              </p>
              {state.loopRisk.reasons.length ? (
                <ul className="list-disc space-y-1 pl-4 text-muted-foreground">
                  {state.loopRisk.reasons.map((reason, index) => (
                    <li key={`${reason}-${index}`}>{reason}</li>
                  ))}
                </ul>
              ) : null}
            </CardContent>
          </Card>
        </aside>
      </div>
    </div>
  )
}
