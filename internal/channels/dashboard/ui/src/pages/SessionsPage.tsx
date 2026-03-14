import { useCallback, useEffect, useMemo, useState } from "react"
import { Link, useParams, useSearchParams } from "react-router-dom"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { ApiError, api } from "@/lib/api"

const SESSION_PAGE_SIZES = [10, 25, 50] as const
const MESSAGE_LIMIT_OPTIONS = [50, 200, 500, 1000] as const
const SESSION_SORT_OPTIONS = [
  { value: "recent", label: "Recently Updated" },
  { value: "oldest", label: "Oldest" },
  { value: "title", label: "Title A-Z" },
  { value: "id", label: "Session ID A-Z" },
] as const

type SortMode = (typeof SESSION_SORT_OPTIONS)[number]["value"]

type SessionItem = {
  sessionID: string
  title: string
  userID: string
  roomID: string
  agentID: string
  channel: string
  createdAt: string
  updatedAt: string
}

type SessionMessage = {
  role: string
  content: string
  ts: string
  runID: string
  toolCallID: string
  toolName: string
}

type ToolEvent = {
  tool: string
  toolCallID: string
  runID: string
  summary: string
  argsText: string
  outputText: string
  errorText: string
  status: "ok" | "failed"
  ts: string
  index: number
}

type SessionsListResponse = {
  sessions?: unknown
  total?: unknown
  limit?: unknown
  offset?: unknown
}

type SessionMessagesResponse = {
  messages?: unknown
}

function asText(value: unknown): string {
  if (value === null || value === undefined) {
    return ""
  }
  return String(value)
}

function normalizeDeepLinkID(rawValue: string | null | undefined): string {
	const raw = asText(rawValue).trim()
	if (!raw) {
		return ""
	}
	try {
		return decodeURIComponent(raw).trim()
	} catch {
		return raw
	}
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return null
  }
  return value as Record<string, unknown>
}

function asNumber(value: unknown): number {
  const parsed = Number(value)
  if (!Number.isFinite(parsed)) {
    return 0
  }
  return parsed
}

function firstNonEmpty(...values: unknown[]): string {
  for (const value of values) {
    const text = asText(value).trim()
    if (text) {
      return text
    }
  }
  return ""
}

function normalizeSearch(value: unknown): string {
  return asText(value)
    .toLowerCase()
    .replace(/\s+/g, " ")
    .trim()
}

function compactText(value: unknown, maxChars: number): string {
  const text = asText(value).replace(/\s+/g, " ").trim()
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

  const text = asText(value).trim()
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
    const trimmed = value.trim()
    if (!trimmed) {
      return ""
    }
    const parsed = parseMaybeJSON(trimmed)
    if (parsed && typeof parsed === "object") {
      try {
        return JSON.stringify(parsed, null, 2)
      } catch {
        return trimmed
      }
    }
    return trimmed
  }
  if (typeof value === "object") {
    try {
      return JSON.stringify(value, null, 2)
    } catch {
      return String(value)
    }
  }
  return String(value)
}

function formatDateTime(value: unknown): string {
  const text = asText(value).trim()
  if (!text) {
    return "-"
  }
  const parsed = new Date(text)
  if (Number.isNaN(parsed.getTime())) {
    return "-"
  }
  return parsed.toLocaleString()
}

function extractErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (typeof error.details === "string" && error.details.trim()) {
      return error.details.trim()
    }
    const details = asRecord(error.details)
    const detailsMessage = asText(details?.message).trim()
    if (detailsMessage) {
      return detailsMessage
    }
    const nestedError = asRecord(details?.error)
    const nestedMessage = asText(nestedError?.message).trim()
    if (nestedMessage) {
      return nestedMessage
    }
  }

  if (error instanceof Error) {
    return error.message || "Unknown error"
  }

  return asText(error).trim() || "Unknown error"
}

function normalizeSession(value: unknown): SessionItem | null {
  const raw = asRecord(value)
  if (!raw) {
    return null
  }
  const sessionID = asText(raw.session_id).trim()
  if (!sessionID) {
    return null
  }

  return {
    sessionID,
    title: asText(raw.title).trim(),
    userID: asText(raw.user_id).trim(),
    roomID: asText(raw.room_id).trim(),
    agentID: asText(raw.agent_id).trim(),
    channel: asText(raw.channel).trim(),
    createdAt: asText(raw.created_at).trim(),
    updatedAt: asText(raw.updated_at).trim(),
  }
}

function normalizeSessions(value: unknown): SessionItem[] {
  if (!Array.isArray(value)) {
    return []
  }
  return value
    .map(normalizeSession)
    .filter((session): session is SessionItem => session !== null)
}

function normalizeMessage(value: unknown): SessionMessage | null {
  const raw = asRecord(value)
  if (!raw) {
    return null
  }
  const role = asText(raw.role).trim().toLowerCase()
  if (!role) {
    return null
  }
  const contentRaw = raw.content
  const content = typeof contentRaw === "string" ? contentRaw : asDisplayText(contentRaw)

  return {
    role,
    content,
    ts: asText(raw.ts).trim(),
    runID: asText(raw.run_id).trim(),
    toolCallID: asText(raw.tool_call_id).trim(),
    toolName: asText(raw.tool_name).trim(),
  }
}

function normalizeMessages(value: unknown): SessionMessage[] {
  if (!Array.isArray(value)) {
    return []
  }
  return value
    .map(normalizeMessage)
    .filter((message): message is SessionMessage => message !== null)
}

function sessionSearchBlob(session: SessionItem): string {
  return normalizeSearch(
    [session.sessionID, session.title, session.userID, session.roomID, session.agentID, session.channel]
      .filter((item) => asText(item).trim())
      .join(" ")
  )
}

function sortSessions(items: SessionItem[], sortMode: SortMode): SessionItem[] {
  const sorted = items.slice()
  sorted.sort((a, b) => {
    const aUpdated = new Date(a.updatedAt || 0).getTime() || 0
    const bUpdated = new Date(b.updatedAt || 0).getTime() || 0
    const aCreated = new Date(a.createdAt || 0).getTime() || 0
    const bCreated = new Date(b.createdAt || 0).getTime() || 0

    if (sortMode === "oldest") {
      if (aUpdated !== bUpdated) {
        return aUpdated - bUpdated
      }
      return aCreated - bCreated
    }

    if (sortMode === "title") {
      const titleCmp = (a.title || "").localeCompare(b.title || "")
      if (titleCmp !== 0) {
        return titleCmp
      }
      if (aUpdated !== bUpdated) {
        return bUpdated - aUpdated
      }
      return a.sessionID.localeCompare(b.sessionID)
    }

    if (sortMode === "id") {
      return a.sessionID.localeCompare(b.sessionID)
    }

    if (aUpdated !== bUpdated) {
      return bUpdated - aUpdated
    }
    return bCreated - aCreated
  })
  return sorted
}

function toToolEvent(message: SessionMessage, index: number): ToolEvent {
  const parsed = asRecord(parseMaybeJSON(message.content))
  const tool = firstNonEmpty(message.toolName, parsed?.tool, parsed?.name, "unknown.tool")
  const toolCallID = firstNonEmpty(message.toolCallID, parsed?.id, parsed?.tool_call_id)
  const runID = firstNonEmpty(message.runID, parsed?.run_id)
  const summary = firstNonEmpty(parsed?.summary, parsed?.message)
  const argsText = asDisplayText(parsed?.arguments ?? parsed?.args ?? parsed?.input ?? parsed?.params)
  const outputText = asDisplayText(parsed?.output ?? parsed?.result ?? parsed?.response)
  const errorText = asDisplayText(parsed?.error ?? parsed?.callback_error ?? parsed?.stderr)

  return {
    tool,
    toolCallID,
    runID,
    summary,
    argsText,
    outputText: outputText || (!parsed ? asDisplayText(message.content) : ""),
    errorText,
    status: errorText ? "failed" : "ok",
    ts: message.ts,
    index,
  }
}

function buildSessionsListQuery(limit: number, offset: number): string {
  const params = new URLSearchParams()
  params.set("limit", String(limit))
  params.set("offset", String(offset))
  return `/api/admin/chat/sessions?${params.toString()}`
}

function buildSessionMessagesQuery(sessionID: string, limit: number): string {
  return `/api/admin/chat/sessions/${encodeURIComponent(sessionID)}/messages?limit=${encodeURIComponent(String(limit))}`
}

function metaRange(offset: number, total: number, count: number): { start: number; end: number } {
  if (total === 0) {
    return { start: 0, end: 0 }
  }
  return {
    start: offset + 1,
    end: Math.min(offset + count, total),
  }
}

export function SessionsPage() {
	const params = useParams<{ sessionId?: string }>()
	const [searchParams, setSearchParams] = useSearchParams()

  const [searchQuery, setSearchQuery] = useState("")
  const [sortMode, setSortMode] = useState<SortMode>("recent")

  const [limit, setLimit] = useState<number>(25)
  const [offset, setOffset] = useState<number>(0)

  const [sessions, setSessions] = useState<SessionItem[]>([])
  const [total, setTotal] = useState(0)
  const [listLoading, setListLoading] = useState(true)
  const [listError, setListError] = useState("")
  const [listReloadToken, setListReloadToken] = useState(0)

  const [selectedSessionID, setSelectedSessionID] = useState("")
  const [selectedSession, setSelectedSession] = useState<SessionItem | null>(null)
  const [messageLimit, setMessageLimit] = useState<number>(200)
  const [messages, setMessages] = useState<SessionMessage[]>([])
  const [messagesLoading, setMessagesLoading] = useState(false)
  const [messagesError, setMessagesError] = useState("")
  const [messagesReloadToken, setMessagesReloadToken] = useState(0)

	const deepLinkedSessionID = useMemo(() => {
		const byPath = normalizeDeepLinkID(params.sessionId)
		if (byPath) {
			return byPath
		}
		return normalizeDeepLinkID(searchParams.get("session"))
	}, [params.sessionId, searchParams])

  const visibleSessions = useMemo(() => {
    const query = normalizeSearch(searchQuery)
    const filtered = query ? sessions.filter((session) => sessionSearchBlob(session).includes(query)) : sessions.slice()
    return sortSessions(filtered, sortMode)
  }, [searchQuery, sessions, sortMode])

  const pageRange = useMemo(() => metaRange(offset, total, sessions.length), [offset, sessions.length, total])

	const openSession = useCallback((session: SessionItem) => {
		setSearchParams((previous) => {
			const next = new URLSearchParams(previous)
			next.set("session", session.sessionID)
			return next
		})

    setSelectedSession((current) => {
      if (current?.sessionID === session.sessionID) {
        return current
      }
      return session
    })

    setSelectedSessionID((current) => {
      if (current === session.sessionID) {
        setMessagesReloadToken((reloadToken) => reloadToken + 1)
        return current
      }
      return session.sessionID
    })
	}, [setSearchParams])

  const loadSessions = useCallback(async () => {
    setListLoading(true)
    setListError("")

    try {
      const payload = await api.get<SessionsListResponse>(buildSessionsListQuery(limit, offset))
      const nextSessions = normalizeSessions(payload.sessions)

      const nextTotal = Math.max(0, asNumber(payload.total) || nextSessions.length)
      const nextLimit = asNumber(payload.limit)
      const nextOffset = asNumber(payload.offset)

      setSessions(nextSessions)
      setTotal(nextTotal)
      if (Number.isFinite(nextLimit) && nextLimit > 0 && nextLimit !== limit) {
        setLimit(nextLimit)
      }
      if (Number.isFinite(nextOffset) && nextOffset >= 0 && nextOffset !== offset) {
        setOffset(nextOffset)
      }

      if (selectedSessionID) {
        const refreshedSelection = nextSessions.find((session) => session.sessionID === selectedSessionID) || null
        if (refreshedSelection) {
          setSelectedSession(refreshedSelection)
        }
      }
    } catch (error) {
      setSessions([])
      setTotal(0)
      setListError(extractErrorMessage(error))
    } finally {
      setListLoading(false)
    }
  }, [limit, offset, selectedSessionID])

  const loadMessages = useCallback(async () => {
    if (!selectedSessionID) {
      setMessages([])
      setMessagesError("")
      setMessagesLoading(false)
      return
    }

    setMessagesLoading(true)
    setMessagesError("")
    setMessages([])

    try {
      const payload = await api.get<SessionMessagesResponse>(buildSessionMessagesQuery(selectedSessionID, messageLimit))
      setMessages(normalizeMessages(payload.messages))
    } catch (error) {
      setMessages([])
      setMessagesError(extractErrorMessage(error))
    } finally {
      setMessagesLoading(false)
    }
  }, [messageLimit, selectedSessionID])

  useEffect(() => {
    void loadSessions()
  }, [listReloadToken, loadSessions])

	useEffect(() => {
		if (!deepLinkedSessionID) {
			return
		}
		setSelectedSessionID((current) => (current === deepLinkedSessionID ? current : deepLinkedSessionID))
		const matched = sessions.find((session) => session.sessionID === deepLinkedSessionID) || null
		if (matched) {
			setSelectedSession(matched)
		}
	}, [deepLinkedSessionID, sessions])

  useEffect(() => {
    void loadMessages()
  }, [loadMessages, messagesReloadToken])

  return (
    <div className="space-y-4 p-6" data-testid="sessions-page">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Sessions</h2>
        <p className="text-sm text-muted-foreground">
          Browse chat sessions, search and sort the current page, and inspect message-level tool events.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Session list</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            <label htmlFor="sessions-search" className="space-y-1 text-sm">
              <span>Search</span>
              <Input
                id="sessions-search"
                type="search"
                placeholder="Session id, title, user, room"
                value={searchQuery}
                onChange={(event) => setSearchQuery(event.target.value)}
                disabled={listLoading}
              />
            </label>

            <label htmlFor="sessions-sort" className="space-y-1 text-sm">
              <span>Sort</span>
              <select
                id="sessions-sort"
                className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                value={sortMode}
                onChange={(event) => setSortMode(event.target.value as SortMode)}
                disabled={listLoading}
              >
                {SESSION_SORT_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>

            <label htmlFor="sessions-page-size" className="space-y-1 text-sm">
              <span>Page size</span>
              <select
                id="sessions-page-size"
                className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                value={String(limit)}
                onChange={(event) => {
                  const nextLimit = Number(event.target.value)
                  setLimit(Number.isFinite(nextLimit) && nextLimit > 0 ? nextLimit : 25)
                  setOffset(0)
                }}
                disabled={listLoading}
              >
                {SESSION_PAGE_SIZES.map((value) => (
                  <option key={value} value={value}>
                    {value}
                  </option>
                ))}
              </select>
            </label>

            <div className="flex items-end justify-between gap-2 md:justify-end">
              <p className="text-sm text-muted-foreground" data-testid="sessions-page-meta">
                {pageRange.start}-{pageRange.end} of {total}
              </p>
              <div className="flex items-center gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setOffset((current) => Math.max(0, current - limit))}
                  disabled={listLoading || offset <= 0}
                >
                  Prev
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setOffset((current) => current + limit)}
                  disabled={listLoading || offset + limit >= total}
                >
                  Next
                </Button>
              </div>
            </div>
          </div>

          {listLoading ? (
            <p className="text-sm text-muted-foreground" data-testid="sessions-list-loading">
              Loading sessions...
            </p>
          ) : null}

          {!listLoading && listError ? (
            <div className="space-y-2 rounded-md border border-destructive/50 bg-destructive/5 p-3">
              <p className="text-sm text-destructive">Failed to load sessions: {listError}</p>
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => setListReloadToken((current) => current + 1)}
              >
                Retry
              </Button>
            </div>
          ) : null}

          {!listLoading && !listError ? (
            <div className="space-y-3">
              <p className="text-sm text-muted-foreground">
                Showing {visibleSessions.length} of {sessions.length} sessions on this page.
              </p>

              {visibleSessions.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  {sessions.length === 0 ? "No sessions found on this page." : "No sessions match the current search/filter."}
                </p>
              ) : (
                <Table data-testid="sessions-table">
                  <TableHeader>
                    <TableRow>
                      <TableHead>Session</TableHead>
                      <TableHead>Title</TableHead>
                      <TableHead>User/Room</TableHead>
                      <TableHead>Updated</TableHead>
                      <TableHead className="text-right">Action</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {visibleSessions.map((session) => {
                      const isSelected = session.sessionID === selectedSessionID
                      return (
                        <TableRow key={session.sessionID} data-state={isSelected ? "selected" : undefined}>
                          <TableCell>
                            <code>{session.sessionID}</code>
                          </TableCell>
                          <TableCell>{session.title || "(untitled)"}</TableCell>
                          <TableCell>
                            {(session.userID || "-") + " / " + (session.roomID || "-")}
                          </TableCell>
                          <TableCell>{formatDateTime(session.updatedAt)}</TableCell>
                          <TableCell className="text-right">
                            <Button
                              type="button"
                              size="sm"
                              variant="outline"
                              onClick={() => openSession(session)}
                            >
                              {isSelected ? "Reload" : "Open"}
                            </Button>
                          </TableCell>
                        </TableRow>
                      )
                    })}
                  </TableBody>
                </Table>
              )}
            </div>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Session detail</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {!selectedSessionID ? (
            <p className="text-sm text-muted-foreground">Select a session to inspect messages and tool events.</p>
          ) : null}

          {selectedSessionID ? (
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border bg-muted/20 p-3 text-sm">
              <p className="text-muted-foreground">
                Session <span className="font-medium text-foreground">{selectedSessionID}</span> · updated{" "}
                {formatDateTime(selectedSession?.updatedAt)} · messages {messages.length}
              </p>

              <label htmlFor="sessions-message-limit" className="space-y-1 text-sm">
                <span>Messages</span>
                <select
                  id="sessions-message-limit"
                  className="ml-2 h-8 rounded-md border bg-background px-2 text-sm"
                  value={String(messageLimit)}
                  onChange={(event) => {
                    const nextLimit = Number(event.target.value)
                    setMessageLimit(Number.isFinite(nextLimit) && nextLimit > 0 ? nextLimit : 200)
                  }}
                  disabled={messagesLoading}
                >
                  {MESSAGE_LIMIT_OPTIONS.map((value) => (
                    <option key={value} value={value}>
                      {value}
                    </option>
                  ))}
                </select>
              </label>
            </div>
          ) : null}

          {selectedSessionID && messagesLoading ? (
            <p className="text-sm text-muted-foreground" data-testid="sessions-messages-loading">
              Loading messages for {selectedSessionID}...
            </p>
          ) : null}

          {selectedSessionID && !messagesLoading && messagesError ? (
            <div className="space-y-2 rounded-md border border-destructive/50 bg-destructive/5 p-3">
              <p className="text-sm text-destructive">Failed to load messages: {messagesError}</p>
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => setMessagesReloadToken((current) => current + 1)}
              >
                Retry
              </Button>
            </div>
          ) : null}

          {selectedSessionID && !messagesLoading && !messagesError && messages.length === 0 ? (
            <p className="text-sm text-muted-foreground">No messages in this session yet.</p>
          ) : null}

          {selectedSessionID && !messagesLoading && !messagesError && messages.length > 0 ? (
            <div className="space-y-3" data-testid="sessions-message-stream">
              {messages.map((message, index) => {
                if (message.role === "user" || message.role === "assistant") {
                  return (
                    <article
                      key={`${message.role}-${message.ts}-${index}`}
                      className={[
                        "rounded-md border p-3",
                        message.role === "assistant" ? "bg-muted/20" : "bg-background",
                      ].join(" ")}
                    >
                      <p className="text-xs text-muted-foreground">
                        {message.role} · {formatDateTime(message.ts)}
                      </p>
                      <pre className="mt-2 whitespace-pre-wrap text-sm">{message.content}</pre>
                    </article>
                  )
                }

                if (message.role === "tool") {
                  const toolEvent = toToolEvent(message, index)
                  return (
                    <article
                      key={`tool-${toolEvent.toolCallID || toolEvent.index}-${toolEvent.ts}-${index}`}
                      className={[
                        "space-y-2 rounded-md border p-3",
                        toolEvent.status === "failed" ? "border-destructive/40 bg-destructive/5" : "bg-muted/20",
                      ].join(" ")}
                      data-testid="sessions-tool-event"
                    >
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <h3 className="text-sm font-semibold">{toolEvent.tool}</h3>
                        <Badge variant={toolEvent.status === "failed" ? "destructive" : "secondary"}>
                          {toolEvent.status}
                        </Badge>
                      </div>

                      <p className="text-xs text-muted-foreground">
                        {[
                          toolEvent.toolCallID ? `call ${toolEvent.toolCallID}` : "",
                          toolEvent.runID ? `run ${toolEvent.runID}` : "",
                          toolEvent.ts ? formatDateTime(toolEvent.ts) : "",
                        ]
                          .filter(Boolean)
                          .join(" · ") || "tool event"}
                      </p>

                      {toolEvent.summary ? <p className="text-sm">{toolEvent.summary}</p> : null}

                      {toolEvent.argsText ? (
                        <details className="rounded-md border bg-background/80 p-2">
                          <summary className="cursor-pointer text-sm">Args</summary>
                          <pre className="mt-2 max-h-64 overflow-auto text-xs">{toolEvent.argsText}</pre>
                        </details>
                      ) : null}

                      {toolEvent.outputText ? (
                        <details className="rounded-md border bg-background/80 p-2">
                          <summary className="cursor-pointer text-sm">Output</summary>
                          <pre className="mt-2 max-h-64 overflow-auto text-xs">{toolEvent.outputText}</pre>
                        </details>
                      ) : null}

                      {toolEvent.errorText ? (
                        <details className="rounded-md border bg-background/80 p-2" open>
                          <summary className="cursor-pointer text-sm">Error</summary>
                          <pre className="mt-2 max-h-64 overflow-auto text-xs">{toolEvent.errorText}</pre>
                        </details>
                      ) : null}

                      <div>
                        {toolEvent.runID ? (
                          <Button asChild size="sm" variant="outline">
                            <Link to={`/runs/${encodeURIComponent(toolEvent.runID)}`}>Inspect Tool</Link>
                          </Button>
                        ) : (
                          <Button type="button" size="sm" variant="outline" disabled>
                            Inspect Tool
                          </Button>
                        )}
                      </div>
                    </article>
                  )
                }

                return (
                  <article key={`${message.role}-${message.ts}-${index}`} className="rounded-md border p-3">
                    <p className="text-xs text-muted-foreground">
                      message role {message.role || "unknown"} · {formatDateTime(message.ts)}
                    </p>
                    <pre className="mt-2 whitespace-pre-wrap text-xs">{compactText(asDisplayText(message.content), 4000)}</pre>
                  </article>
                )
              })}
            </div>
          ) : null}
        </CardContent>
      </Card>
    </div>
  )
}
