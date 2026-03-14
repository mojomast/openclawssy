import { useCallback, useEffect, useMemo, useState } from "react"
import { useNavigate, useParams } from "react-router-dom"
import { DecisionTimeline } from "@/components/DecisionTimeline"
import { JSONViewer } from "@/components/JSONViewer"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { flattenDecisionRecords, parseRunDecisionNode, type FlattenedDecisionRecord } from "@/lib/decisions"
import { ApiError, api } from "@/lib/api"

const STATUS_FILTERS = [
  { value: "", label: "All" },
  { value: "queued", label: "Queued" },
  { value: "running", label: "Running" },
  { value: "completed", label: "Completed" },
  { value: "failed", label: "Failed" },
  { value: "canceled", label: "Canceled" },
] as const

const PAGE_SIZES = [10, 25, 50] as const
const TOOL_ARGS_PREVIEW_CHARS = 140
const TOOL_ERROR_KEY_CHARS = 260

type RunItem = {
  id: string
  agentID: string
  source: string
  status: string
  updatedAt: string
  provider: string
  model: string
  trace: Record<string, unknown> | null
  payload: Record<string, unknown>
}

type RunsListResponse = {
  runs?: unknown
  total?: unknown
  limit?: unknown
  offset?: unknown
}

type RunTraceResponse = {
  trace?: unknown
}

type ModelStep = {
  iteration: number
  promptLength: number
  message: string
  historyInjected: boolean
}

type ToolExecutionItem = {
  tool: string
  toolCallID: string
  durationMS: number
  argumentsText: string
  outputText: string
  summary: string
  errorText: string
  callbackErrorText: string
  status: "ok" | "failed"
  timelineIndex: number
  selectionKey: string
}

type TimelineBlock =
  | { type: "tool"; item: ToolExecutionItem }
  | { type: "failure-group"; tool: string; error: string; items: ToolExecutionItem[] }

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return null
  }
  return value as Record<string, unknown>
}

function asText(value: unknown): string {
  if (value === null || value === undefined) {
    return ""
  }
  return String(value)
}

function asNumber(value: unknown): number {
  const parsed = Number(value)
  if (!Number.isFinite(parsed)) {
    return 0
  }
  return parsed
}

function parseMaybeJSON(value: string): unknown {
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

function formatDuration(ms: unknown): string {
  const value = asNumber(ms)
  if (value <= 0) {
    return "-"
  }
  if (value < 1000) {
    return `${value}ms`
  }
  return `${(value / 1000).toFixed(2)}s`
}

function extractErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (typeof error.details === "string" && error.details.trim().length > 0) {
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

    const directMessage = asText(error.message).trim()
    if (directMessage) {
      return directMessage
    }
  }

  if (error instanceof Error) {
    return error.message || "Unknown error"
  }

  return asText(error).trim() || "Unknown error"
}

function normalizeRun(value: unknown): RunItem | null {
  const raw = asRecord(value)
  if (!raw) {
    return null
  }

  const id = asText(raw.id).trim()
  if (!id) {
    return null
  }

  const trace = asRecord(raw.trace)
  return {
    id,
    agentID: asText(raw.agent_id).trim(),
    source: asText(raw.source).trim(),
    status: asText(raw.status).trim(),
    updatedAt: asText(raw.updated_at).trim(),
    provider: asText(raw.provider).trim(),
    model: asText(raw.model).trim(),
    trace,
    payload: raw,
  }
}

function normalizeRuns(value: unknown): RunItem[] {
  if (!Array.isArray(value)) {
    return []
  }
  return value
    .map(normalizeRun)
    .filter((run): run is RunItem => run !== null)
}

function normalizeModelSteps(trace: Record<string, unknown> | null): ModelStep[] {
  if (!trace || !Array.isArray(trace.model_inputs)) {
    return []
  }

  return trace.model_inputs
    .map((entry) => {
      const raw = asRecord(entry)
      if (!raw) {
        return null
      }

      return {
        iteration: asNumber(raw.iteration),
        promptLength: asNumber(raw.prompt_length),
        message: asText(raw.message),
        historyInjected: Boolean(raw.history_injected),
      }
    })
    .filter((entry): entry is ModelStep => entry !== null)
}

function normalizeToolExecutionResults(trace: Record<string, unknown> | null): ToolExecutionItem[] {
  if (!trace || !Array.isArray(trace.tool_execution_results)) {
    return []
  }

  return trace.tool_execution_results
    .map((entry, index) => {
      const raw = asRecord(entry)
      if (!raw) {
        return null
      }

      const tool = asText(raw.tool).trim() || "unknown.tool"
      const toolCallID = asText(raw.tool_call_id).trim()
      const errorText = asText(raw.error).trim()
      const callbackErrorText = asText(raw.callback_error).trim()
      const status: "ok" | "failed" = errorText || callbackErrorText ? "failed" : "ok"
      const durationMS = asNumber(raw.duration_ms)
      const argumentsText = asText(raw.arguments)
      const selectionKey = toolCallID
        ? `${tool}|${toolCallID}`
        : `${tool}|${index}|${durationMS}|${argumentsText}`

      return {
        tool,
        toolCallID,
        durationMS,
        argumentsText,
        outputText: asText(raw.output),
        summary: asText(raw.summary).trim(),
        errorText,
        callbackErrorText,
        status,
        timelineIndex: index,
        selectionKey,
      }
    })
    .filter((entry): entry is ToolExecutionItem => entry !== null)
}

function formatArgsPreview(argumentsText: string): string {
  const parsed = parseMaybeJSON(argumentsText)
  if (parsed !== null) {
    return compactText(JSON.stringify(parsed), TOOL_ARGS_PREVIEW_CHARS)
  }
  return compactText(argumentsText, TOOL_ARGS_PREVIEW_CHARS) || "(no args)"
}

function groupedTimelineBlocks(toolEntries: ToolExecutionItem[]): TimelineBlock[] {
  const blocks: TimelineBlock[] = []
  let index = 0

  while (index < toolEntries.length) {
    const current = toolEntries[index]
    const currentError = compactText(current.errorText || current.callbackErrorText, TOOL_ERROR_KEY_CHARS)
    if (current.status !== "failed" || !currentError) {
      blocks.push({ type: "tool", item: current })
      index += 1
      continue
    }

    let end = index + 1
    while (end < toolEntries.length) {
      const candidate = toolEntries[end]
      const candidateError = compactText(candidate.errorText || candidate.callbackErrorText, TOOL_ERROR_KEY_CHARS)
      if (candidate.status !== "failed" || candidate.tool !== current.tool || candidateError !== currentError) {
        break
      }
      end += 1
    }

    if (end - index > 1) {
      blocks.push({
        type: "failure-group",
        tool: current.tool,
        error: currentError,
        items: toolEntries.slice(index, end),
      })
    } else {
      blocks.push({ type: "tool", item: current })
    }
    index = end
  }

  return blocks
}

function buildListQuery({
  status,
  agentID,
  source,
  limit,
  offset,
}: {
  status: string
  agentID: string
  source: string
  limit: number
  offset: number
}): string {
  const params = new URLSearchParams()
  if (status) {
    params.set("status", status)
  }
  if (agentID.trim()) {
    params.set("agent_id", agentID.trim())
  }
  if (source.trim()) {
    params.set("source", source.trim())
  }
  params.set("limit", String(limit))
  params.set("offset", String(offset))
  return `/v1/runs?${params.toString()}`
}

function decodeRunID(rawRunID: string | undefined): string {
  if (!rawRunID) {
    return ""
  }
  try {
    return decodeURIComponent(rawRunID)
  } catch {
    return rawRunID
  }
}

function badgeVariantForStatus(status: string): "default" | "secondary" | "destructive" | "outline" {
  const normalized = status.toLowerCase()
  if (normalized === "running") {
    return "default"
  }
  if (normalized === "completed") {
    return "secondary"
  }
  if (normalized === "failed") {
    return "destructive"
  }
  return "outline"
}

export function RunsPage() {
  const navigate = useNavigate()
  const params = useParams<{ runId?: string }>()
  const selectedRunID = useMemo(() => decodeRunID(params.runId), [params.runId])

  const [statusInput, setStatusInput] = useState("")
  const [agentInput, setAgentInput] = useState("")
  const [sourceInput, setSourceInput] = useState("")

  const [statusFilter, setStatusFilter] = useState("")
  const [agentFilter, setAgentFilter] = useState("")
  const [sourceFilter, setSourceFilter] = useState("")

  const [limit, setLimit] = useState<number>(10)
  const [offset, setOffset] = useState<number>(0)

  const [runs, setRuns] = useState<RunItem[]>([])
  const [total, setTotal] = useState(0)
  const [listLoading, setListLoading] = useState(true)
  const [listError, setListError] = useState("")
  const [listReloadToken, setListReloadToken] = useState(0)

  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState("")
  const [detailReloadToken, setDetailReloadToken] = useState(0)
  const [selectedRun, setSelectedRun] = useState<RunItem | null>(null)
  const [selectedTrace, setSelectedTrace] = useState<Record<string, unknown> | null>(null)
  const [traceSourceNote, setTraceSourceNote] = useState("")
  const [traceFetchError, setTraceFetchError] = useState("")
  const [selectedTool, setSelectedTool] = useState<ToolExecutionItem | null>(null)
  const [decisionDrawerOpen, setDecisionDrawerOpen] = useState(false)
  const [decisionLoading, setDecisionLoading] = useState(false)
  const [decisionError, setDecisionError] = useState("")
  const [decisionRecords, setDecisionRecords] = useState<FlattenedDecisionRecord[]>([])

  const modelSteps = useMemo(() => normalizeModelSteps(selectedTrace), [selectedTrace])
  const toolEntries = useMemo(() => normalizeToolExecutionResults(selectedTrace), [selectedTrace])
  const timelineBlocks = useMemo(() => groupedTimelineBlocks(toolEntries), [toolEntries])

  const currentPageStart = total === 0 ? 0 : offset + 1
  const currentPageEnd = total === 0 ? 0 : Math.min(offset + runs.length, total)

  const loadRuns = useCallback(async () => {
    setListLoading(true)
    setListError("")

    try {
      const payload = await api.get<RunsListResponse>(
        buildListQuery({ status: statusFilter, agentID: agentFilter, source: sourceFilter, limit, offset })
      )
      const nextRuns = normalizeRuns(payload.runs)
      const parsedTotal = asNumber(payload.total)

      setRuns(nextRuns)
      setTotal(parsedTotal >= 0 ? parsedTotal : nextRuns.length)
    } catch (error) {
      setRuns([])
      setTotal(0)
      setListError(extractErrorMessage(error))
    } finally {
      setListLoading(false)
    }
  }, [agentFilter, limit, offset, sourceFilter, statusFilter])

  const loadRunDetail = useCallback(async (runID: string) => {
    setDetailLoading(true)
    setDetailError("")
    setSelectedRun(null)
    setSelectedTrace(null)
    setSelectedTool(null)
    setTraceSourceNote("")
    setTraceFetchError("")
    setDecisionDrawerOpen(false)
    setDecisionError("")
    setDecisionRecords([])

    try {
      const runPayload = await api.get<unknown>(`/v1/runs/${encodeURIComponent(runID)}`)
      const normalizedRun = normalizeRun(runPayload)
      if (!normalizedRun) {
        throw new Error("Run payload is missing required fields.")
      }

      let tracePayload: RunTraceResponse | null = null
      let traceErrorMessage = ""
      try {
        tracePayload = await api.get<RunTraceResponse>(`/api/admin/debug/runs/${encodeURIComponent(runID)}/trace`)
      } catch (error) {
        const maybeApiError = error as ApiError
        if (!(maybeApiError instanceof ApiError) || maybeApiError.status !== 404) {
          traceErrorMessage = extractErrorMessage(error)
        }
      }

      const debugTrace = asRecord(tracePayload?.trace)
      const runTrace = normalizedRun.trace
      const trace = debugTrace || runTrace || null

      if (debugTrace) {
        setTraceSourceNote("Trace source: debug endpoint (/api/admin/debug/runs/{id}/trace).")
      } else if (runTrace) {
        setTraceSourceNote("Trace source: run payload (/v1/runs/{id}).")
      } else {
        setTraceSourceNote("Trace source: unavailable.")
      }

      setTraceFetchError(traceErrorMessage)
      setSelectedRun(normalizedRun)
      setSelectedTrace(trace)

      const firstTool = normalizeToolExecutionResults(trace)[0] || null
      setSelectedTool(firstTool)
      setDetailError("")
    } catch (error) {
      setDetailError(extractErrorMessage(error))
    } finally {
      setDetailLoading(false)
    }
  }, [])

  const loadRunDecisions = useCallback(async (runID: string) => {
    const targetRunID = runID.trim()
    if (!targetRunID) {
      setDecisionError("Select a run before opening the decision ledger.")
      setDecisionRecords([])
      return
    }

    setDecisionLoading(true)
    setDecisionError("")

    try {
      const payload = await api.get<unknown>(`/api/admin/runs/${encodeURIComponent(targetRunID)}/decisions`)
      const root = parseRunDecisionNode(payload)
      if (!root) {
        setDecisionRecords([])
        setDecisionError("No decision records were returned for this run.")
        return
      }
      setDecisionRecords(flattenDecisionRecords(root))
    } catch (error) {
      setDecisionRecords([])
      setDecisionError(extractErrorMessage(error))
    } finally {
      setDecisionLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadRuns()
  }, [loadRuns, listReloadToken])

  useEffect(() => {
    if (!selectedRunID) {
      setDetailLoading(false)
      setDetailError("")
      setSelectedRun(null)
      setSelectedTrace(null)
      setSelectedTool(null)
      setTraceSourceNote("")
      setTraceFetchError("")
      setDecisionDrawerOpen(false)
      setDecisionError("")
      setDecisionRecords([])
      return
    }
    void loadRunDetail(selectedRunID)
  }, [detailReloadToken, loadRunDetail, selectedRunID])

  return (
    <div className="space-y-4 p-6" data-testid="runs-page">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Runs</h2>
        <p className="text-sm text-muted-foreground">
          Browse run history with filters, pagination, deep-linkable run detail, model steps, and tool timelines.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Run list filters</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-5">
            <label htmlFor="runs-status" className="space-y-1 text-sm">
              <span>Status</span>
              <select
                id="runs-status"
                className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                value={statusInput}
                onChange={(event) => setStatusInput(event.target.value)}
                disabled={listLoading}
              >
                {STATUS_FILTERS.map((option) => (
                  <option key={option.value || "all"} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>

            <label htmlFor="runs-agent-filter" className="space-y-1 text-sm">
              <span>Agent filter</span>
              <Input
                id="runs-agent-filter"
                placeholder="agent_id"
                value={agentInput}
                disabled={listLoading}
                onChange={(event) => setAgentInput(event.target.value)}
              />
            </label>

            <label htmlFor="runs-source-filter" className="space-y-1 text-sm">
              <span>Source filter</span>
              <Input
                id="runs-source-filter"
                placeholder="source"
                value={sourceInput}
                disabled={listLoading}
                onChange={(event) => setSourceInput(event.target.value)}
              />
            </label>

            <label htmlFor="runs-page-size" className="space-y-1 text-sm">
              <span>Page size</span>
              <select
                id="runs-page-size"
                className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                value={String(limit)}
                onChange={(event) => {
                  const nextLimit = Number(event.target.value)
                  setLimit(Number.isFinite(nextLimit) && nextLimit > 0 ? nextLimit : 10)
                  setOffset(0)
                }}
                disabled={listLoading}
              >
                {PAGE_SIZES.map((value) => (
                  <option key={value} value={value}>
                    {value}
                  </option>
                ))}
              </select>
            </label>

            <div className="flex items-end gap-2">
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  setOffset(0)
                  setStatusFilter(statusInput)
                  setAgentFilter(agentInput.trim())
                  setSourceFilter(sourceInput.trim())
                }}
                disabled={listLoading}
              >
                Apply filters
              </Button>
              <Button
                type="button"
                variant="ghost"
                onClick={() => {
                  setStatusInput("")
                  setAgentInput("")
                  setSourceInput("")
                  setStatusFilter("")
                  setAgentFilter("")
                  setSourceFilter("")
                  setOffset(0)
                }}
                disabled={listLoading}
              >
                Clear
              </Button>
            </div>
          </div>

          <div className="flex flex-wrap items-center justify-between gap-2">
            <p className="text-sm text-muted-foreground" data-testid="runs-page-meta">
              {currentPageStart}-{currentPageEnd} of {total}
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

          {listLoading ? (
            <p className="text-sm text-muted-foreground" data-testid="runs-list-loading">
              Loading runs...
            </p>
          ) : null}

          {!listLoading && listError ? (
            <div className="space-y-2 rounded-md border border-destructive/50 bg-destructive/5 p-3">
              <p className="text-sm text-destructive">Failed to load runs: {listError}</p>
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

          {!listLoading && !listError && runs.length === 0 ? (
            <p className="text-sm text-muted-foreground">No runs found for this filter.</p>
          ) : null}

          {!listLoading && !listError && runs.length > 0 ? (
            <Table data-testid="runs-table">
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Agent</TableHead>
                  <TableHead>Source</TableHead>
                  <TableHead>Updated</TableHead>
                  <TableHead className="text-right">Action</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {runs.map((run) => {
                  const isSelected = run.id === selectedRunID
                  return (
                    <TableRow key={run.id} data-state={isSelected ? "selected" : undefined}>
                      <TableCell>
                        <code>{run.id}</code>
                      </TableCell>
                      <TableCell>
                        <Badge variant={badgeVariantForStatus(run.status)}>{run.status || "unknown"}</Badge>
                      </TableCell>
                      <TableCell>{run.agentID || "-"}</TableCell>
                      <TableCell>{run.source || "-"}</TableCell>
                      <TableCell>{formatDateTime(run.updatedAt)}</TableCell>
                      <TableCell className="text-right">
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          onClick={() => {
                            if (run.id === selectedRunID) {
                              setDetailReloadToken((current) => current + 1)
                              return
                            }
                            navigate(`/runs/${encodeURIComponent(run.id)}`)
                          }}
                        >
                          {run.id === selectedRunID ? "Reload" : "Open"}
                        </Button>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          ) : null}
        </CardContent>
      </Card>

      <Card data-testid="run-detail-panel">
        <CardHeader className="space-y-2">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <CardTitle className="text-base">Run detail</CardTitle>
            {selectedRunID ? (
              <div className="flex items-center gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  data-testid="run-why-button"
                  disabled={detailLoading}
                  onClick={() => {
                    setDecisionDrawerOpen(true)
                    void loadRunDecisions(selectedRunID)
                  }}
                >
                  Why this happened
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setDetailReloadToken((current) => current + 1)}
                  disabled={detailLoading}
                >
                  Reload detail
                </Button>
                <Button type="button" variant="ghost" size="sm" onClick={() => navigate("/runs")}>
                  Close
                </Button>
              </div>
            ) : null}
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          {!selectedRunID ? (
            <p className="text-sm text-muted-foreground">Select a run to inspect details and trace data.</p>
          ) : null}

          {selectedRunID && detailLoading ? (
            <p className="text-sm text-muted-foreground" data-testid="run-detail-loading">
              Loading run {selectedRunID}...
            </p>
          ) : null}

          {selectedRunID && !detailLoading && detailError ? (
            <div className="space-y-2 rounded-md border border-destructive/50 bg-destructive/5 p-3">
              <p className="text-sm text-destructive">Failed to load run: {detailError}</p>
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => setDetailReloadToken((current) => current + 1)}
              >
                Retry
              </Button>
            </div>
          ) : null}

          {selectedRunID && !detailLoading && !detailError && selectedRun ? (
            <div className="space-y-5">
              <section className="grid gap-3 rounded-md border bg-muted/20 p-4 sm:grid-cols-2 xl:grid-cols-4">
                <div>
                  <p className="text-xs text-muted-foreground">ID</p>
                  <p className="text-sm font-medium">
                    <code>{selectedRun.id}</code>
                  </p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Status</p>
                  <p className="text-sm font-medium">{selectedRun.status || "-"}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Updated</p>
                  <p className="text-sm font-medium">{formatDateTime(selectedRun.updatedAt)}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Model</p>
                  <p className="text-sm font-medium">
                    {selectedRun.provider || selectedRun.model
                      ? `${selectedRun.provider || "unknown"} / ${selectedRun.model || "unknown"}`
                      : "-"}
                  </p>
                </div>
              </section>

              <section className="space-y-2" data-testid="run-payload-json">
                <h3 className="text-sm font-semibold">Full payload</h3>
                <JSONViewer data={selectedRun.payload} maxHeight="260px" searchable />
              </section>

              <section className="space-y-2">
                <h3 className="text-sm font-semibold">Model Steps</h3>
                {traceSourceNote ? <p className="text-xs text-muted-foreground">{traceSourceNote}</p> : null}
                {traceFetchError ? (
                  <p className="text-xs text-destructive">Trace endpoint warning: {traceFetchError}</p>
                ) : null}
                {modelSteps.length === 0 ? (
                  <p className="text-sm text-muted-foreground">No model steps captured in this trace.</p>
                ) : (
                  <ol className="list-decimal space-y-1 pl-5 text-sm">
                    {modelSteps.map((step, index) => (
                      <li key={`${step.iteration}-${index}`}>
                        #{step.iteration || "?"} · prompt {step.promptLength} chars · {step.historyInjected ? "history" : "no-history"} ·{" "}
                        {compactText(step.message, 220) || "(empty message)"}
                      </li>
                    ))}
                  </ol>
                )}
              </section>

              <section className="space-y-2">
                <h3 className="text-sm font-semibold">Tool Calls</h3>
                {toolEntries.length === 0 ? (
                  <p className="text-sm text-muted-foreground">No tool calls captured for this run.</p>
                ) : (
                  <div className="space-y-2" data-testid="tool-timeline">
                    {timelineBlocks.map((block, index) => {
                      if (block.type === "tool") {
                        const item = block.item
                        return (
                          <button
                            key={item.selectionKey}
                            type="button"
                            className={[
                              "w-full rounded-md border px-3 py-2 text-left text-sm",
                              selectedTool?.selectionKey === item.selectionKey ? "border-primary bg-primary/5" : "hover:bg-muted/50",
                            ].join(" ")}
                            onClick={() => setSelectedTool(item)}
                          >
                            <div className="flex flex-wrap items-center justify-between gap-2">
                              <span className="font-medium">{item.tool}</span>
                              <span className="text-xs text-muted-foreground">
                                {item.status === "failed" ? "failed" : "ok"} · {formatDuration(item.durationMS)}
                              </span>
                            </div>
                            <p className="mt-1 text-xs text-muted-foreground">args: {formatArgsPreview(item.argumentsText)}</p>
                            {item.summary ? <p className="mt-1 text-xs text-muted-foreground">{item.summary}</p> : null}
                          </button>
                        )
                      }

                      return (
                        <details key={`group-${block.tool}-${index}`} className="rounded-md border bg-muted/20 px-3 py-2">
                          <summary className="cursor-pointer text-sm">
                            {block.items.length} repeated failures · {block.tool} · {block.error}
                          </summary>
                          <div className="mt-2 space-y-2">
                            {block.items.map((item) => (
                              <button
                                key={item.selectionKey}
                                type="button"
                                className={[
                                  "w-full rounded-md border px-3 py-2 text-left text-sm",
                                  selectedTool?.selectionKey === item.selectionKey
                                    ? "border-primary bg-primary/5"
                                    : "hover:bg-muted/50",
                                ].join(" ")}
                                onClick={() => setSelectedTool(item)}
                              >
                                <div className="flex flex-wrap items-center justify-between gap-2">
                                  <span className="font-medium">{item.tool}</span>
                                  <span className="text-xs text-muted-foreground">
                                    failed · {formatDuration(item.durationMS)}
                                  </span>
                                </div>
                                <p className="mt-1 text-xs text-muted-foreground">args: {formatArgsPreview(item.argumentsText)}</p>
                              </button>
                            ))}
                          </div>
                        </details>
                      )
                    })}
                  </div>
                )}
              </section>

              <section className="space-y-2 rounded-md border p-4" data-testid="tool-inspection-panel">
                <h3 className="text-sm font-semibold">Tool inspection</h3>
                {!selectedTool ? (
                  <p className="text-sm text-muted-foreground">Click a tool call to inspect arguments, output, and errors.</p>
                ) : (
                  <div className="space-y-3 text-sm">
                    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                      <div>
                        <p className="text-xs text-muted-foreground">Name</p>
                        <p className="font-medium">{selectedTool.tool}</p>
                      </div>
                      <div>
                        <p className="text-xs text-muted-foreground">Status</p>
                        <p className="font-medium">{selectedTool.status}</p>
                      </div>
                      <div>
                        <p className="text-xs text-muted-foreground">Duration</p>
                        <p className="font-medium">{formatDuration(selectedTool.durationMS)}</p>
                      </div>
                      <div>
                        <p className="text-xs text-muted-foreground">Call ID</p>
                        <p className="font-medium">{selectedTool.toolCallID || "-"}</p>
                      </div>
                    </div>

                    <div>
                      <p className="text-xs text-muted-foreground">Arguments</p>
                      <pre className="mt-1 max-h-48 overflow-auto rounded-md border bg-muted/30 p-2 text-xs">
                        {selectedTool.argumentsText || "(empty)"}
                      </pre>
                    </div>

                    <div>
                      <p className="text-xs text-muted-foreground">Output</p>
                      <pre className="mt-1 max-h-48 overflow-auto rounded-md border bg-muted/30 p-2 text-xs">
                        {selectedTool.outputText || "(empty)"}
                      </pre>
                    </div>

                    <div>
                      <p className="text-xs text-muted-foreground">Error</p>
                      <pre className="mt-1 max-h-48 overflow-auto rounded-md border bg-muted/30 p-2 text-xs">
                        {selectedTool.errorText || selectedTool.callbackErrorText || "(none)"}
                      </pre>
                    </div>
                  </div>
                )}
              </section>
            </div>
          ) : null}
        </CardContent>
      </Card>

      <Sheet open={decisionDrawerOpen} onOpenChange={setDecisionDrawerOpen}>
        <SheetContent data-testid="decision-drawer" className="overflow-y-auto">
          <SheetHeader>
            <SheetTitle>Why this happened</SheetTitle>
            <SheetDescription>
              Chronological decision ledger records for run {selectedRunID || "-"}.
            </SheetDescription>
          </SheetHeader>

          <div className="mt-4 space-y-3">
            {decisionLoading ? <p className="text-sm text-muted-foreground">Loading decision ledger...</p> : null}
            {decisionError ? <p className="text-sm text-destructive">{decisionError}</p> : null}
            {!decisionLoading && !decisionError ? (
              <DecisionTimeline
                records={decisionRecords}
                emptyLabel="No decision ledger records found for this run."
                testID="decision-drawer-timeline"
              />
            ) : null}
          </div>
        </SheetContent>
      </Sheet>
    </div>
  )
}
