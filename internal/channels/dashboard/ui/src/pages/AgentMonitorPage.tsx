import { useCallback, useEffect, useMemo, useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useControlPlaneFeatures } from "@/hooks/useControlPlaneFeatures"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { ApiError, api } from "@/lib/api"

const MONITOR_POLL_INTERVAL_MS = 2500

const THINKING_MODES = ["never", "on_error", "always"] as const

type ThinkingMode = (typeof THINKING_MODES)[number]

type MonitorRun = {
  runID: string
  instanceID: string
  agentID: string
  taskID: string
  source: string
  role: string
  message: string
  modelProvider: string
  modelName: string
  checkpointPath: string
  status: string
  error: string
  startedAt: string
  completedAt: string
}

type AgentSummary = {
  selfImprovementReady: boolean
  activatedSkills: string[]
  modelProvider: string
  modelName: string
}

type AgentsResponse = {
  agents?: unknown
  agent_summaries?: unknown
}

type MonitorRunsResponse = {
  runs?: unknown
  available_agents?: unknown
}

type StartRunResponse = {
  id?: unknown
}

type ActiveInstanceResponse = {
  instance?: unknown
}

type MonitorRunControlResponse = {
  cancelled?: unknown
}

function normalizeActiveInstanceID(value: unknown): string {
  const record = asRecord(value)
  return asText(record?.id).trim()
}

function asText(value: unknown): string {
  if (value === null || value === undefined) {
    return ""
  }
  return String(value)
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return null
  }
  return value as Record<string, unknown>
}

function asBoolean(value: unknown): boolean {
  if (typeof value === "boolean") {
    return value
  }
  if (typeof value === "number") {
    return value !== 0
  }
  if (typeof value === "string") {
    const normalized = value.trim().toLowerCase()
    return normalized === "true" || normalized === "1" || normalized === "yes"
  }
  return false
}

function compactText(value: unknown, maxChars = 80): string {
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

function normalizeAgentIDs(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return []
  }
  const seen = new Set<string>()
  const agentIDs: string[] = []
  value.forEach((entry) => {
    const agentID = asText(entry).trim()
    if (!agentID) {
      return
    }
    if (seen.has(agentID)) {
      return
    }
    seen.add(agentID)
    agentIDs.push(agentID)
  })
  return agentIDs
}

function normalizeRun(value: unknown): MonitorRun | null {
  const raw = asRecord(value)
  if (!raw) {
    return null
  }

  const runID = asText(raw.run_id).trim()
  if (!runID) {
    return null
  }

  return {
    runID,
    instanceID: asText(raw.instance_id).trim(),
    agentID: asText(raw.agent_id).trim(),
    taskID: asText(raw.task_id).trim(),
    source: asText(raw.source).trim(),
    role: asText(raw.role).trim().toLowerCase() || "main",
    message: asText(raw.message).trim(),
    modelProvider: asText(raw.model_provider).trim(),
    modelName: asText(raw.model_name).trim(),
    checkpointPath: asText(raw.checkpoint_path).trim(),
    status: asText(raw.status).trim().toLowerCase(),
    error: asText(raw.error).trim(),
    startedAt: asText(raw.started_at).trim(),
    completedAt: asText(raw.completed_at).trim(),
  }
}

function normalizeRuns(value: unknown): MonitorRun[] {
  if (!Array.isArray(value)) {
    return []
  }
  return value.map(normalizeRun).filter((run): run is MonitorRun => run !== null)
}

function normalizeAgentSummaries(value: unknown): Record<string, AgentSummary> {
  const raw = asRecord(value)
  if (!raw) {
    return {}
  }

  const result: Record<string, AgentSummary> = {}
  Object.entries(raw).forEach(([agentID, entry]) => {
    const summary = asRecord(entry)
    if (!summary) {
      return
    }

    const activatedSkills = Array.isArray(summary.activated_skills)
      ? summary.activated_skills.map((skill) => asText(skill).trim()).filter((skill) => skill.length > 0)
      : []

    result[agentID] = {
      selfImprovementReady: asBoolean(summary.self_improvement_ready),
      activatedSkills,
      modelProvider: asText(summary.model_provider).trim(),
      modelName: asText(summary.model_name).trim(),
    }
  })

  return result
}

function sanitizeForTestID(value: string): string {
  return value.replace(/[^a-zA-Z0-9_-]+/g, "-")
}

function mergeAgentIDs(...sources: unknown[]): string[] {
  const seen = new Set<string>()
  const merged: string[] = []

  sources.forEach((source) => {
    normalizeAgentIDs(source).forEach((agentID) => {
      if (seen.has(agentID)) {
        return
      }
      seen.add(agentID)
      merged.push(agentID)
    })
  })

  return merged
}

export function AgentMonitorPage() {
  const { features, loading: featuresLoading } = useControlPlaneFeatures()
  const [activeInstanceID, setActiveInstanceID] = useState("")
  const [availableAgents, setAvailableAgents] = useState<string[]>(["default"])
  const [agentSummaries, setAgentSummaries] = useState<Record<string, AgentSummary>>({})
  const [runs, setRuns] = useState<MonitorRun[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [promptDraft, setPromptDraft] = useState("")
  const [thinkingMode, setThinkingMode] = useState<ThinkingMode>("never")
  const [actionStatus, setActionStatus] = useState("")
  const [actionError, setActionError] = useState("")
  const featureDisabled = !featuresLoading && !features.instanceAgents

  const loadMonitorData = useCallback(async (options?: { silent?: boolean }) => {
    setLoading(true)
    if (!options?.silent) {
      setError("")
    }

    try {
      const [agentPayload, runPayload] = await Promise.all([
        api.get<AgentsResponse>("/api/admin/agents?channel=dashboard&user_id=dashboard_user&room_id=dashboard"),
        api.get<MonitorRunsResponse>("/api/admin/monitor/runs?limit=120"),
      ])

      const normalizedRuns = normalizeRuns(runPayload.runs)
      const mergedAgents = mergeAgentIDs(
        agentPayload.agents,
        runPayload.available_agents,
        normalizedRuns.map((run) => run.agentID)
      )

      setAvailableAgents(mergedAgents.length > 0 ? mergedAgents : ["default"])
      setAgentSummaries(normalizeAgentSummaries(agentPayload.agent_summaries))
      setRuns(normalizedRuns)
      setError("")
    } catch (loadError) {
      setError(extractErrorMessage(loadError))
    } finally {
      setLoading(false)
    }
  }, [])

  const loadActiveInstance = useCallback(async () => {
    try {
      const payload = await api.get<ActiveInstanceResponse>("/api/admin/instances/active")
      setActiveInstanceID(normalizeActiveInstanceID(payload.instance))
    } catch {
      setActiveInstanceID("")
    }
  }, [])

  useEffect(() => {
    if (featuresLoading) {
      return
    }
    if (featureDisabled) {
      setLoading(false)
      setError("")
      setActiveInstanceID("")
      setAvailableAgents([])
      setAgentSummaries({})
      setRuns([])
      setActionStatus("")
      setActionError("")
      return
    }
    void loadMonitorData()
    void loadActiveInstance()

    const pollTimer = window.setInterval(() => {
      void loadMonitorData({ silent: true })
    }, MONITOR_POLL_INTERVAL_MS)

    return () => {
      window.clearInterval(pollTimer)
    }
  }, [featureDisabled, featuresLoading, loadActiveInstance, loadMonitorData])

  const startAgent = useCallback(
    async (agentID: string) => {
      const message = asText(promptDraft).trim()
      if (!message) {
        setActionError("Enter a prompt before starting an agent run.")
        return
      }

      setActionError("")
      setActionStatus(`Starting ${agentID}...`)

      try {
        let instanceID = activeInstanceID
        if (!instanceID) {
          const payload = await api.get<ActiveInstanceResponse>("/api/admin/instances/active")
          instanceID = normalizeActiveInstanceID(payload.instance)
          if (instanceID) {
            setActiveInstanceID(instanceID)
          }
        }
        const payload = await api.post<StartRunResponse>("/v1/runs", {
          instance_id: instanceID || undefined,
          agent_id: agentID,
          message,
          thinking_mode: thinkingMode,
        })
        const runID = asText(payload.id).trim() || "queued"
        setActionStatus(`Started ${agentID} (${runID}).`)
        await loadMonitorData({ silent: true })
      } catch (startError) {
        setActionError(extractErrorMessage(startError))
      }
    },
    [activeInstanceID, loadMonitorData, promptDraft, thinkingMode]
  )

  const stopRun = useCallback(
    async (run: MonitorRun) => {
      const trimmedRunID = asText(run.runID).trim()
      const instanceID = asText(run.instanceID).trim()
      const agentID = asText(run.agentID).trim()
      if (!trimmedRunID) {
        return
      }

      setActionError("")
      setActionStatus(`Stopping ${trimmedRunID}...`)

      try {
        const payload = await api.post<MonitorRunControlResponse>("/api/admin/monitor/runs/control", {
          action: "cancel",
          run_id: trimmedRunID,
          instance_id: instanceID || undefined,
          agent_id: agentID || undefined,
        })
        setActionStatus(
          asBoolean(payload.cancelled)
            ? `Cancellation requested for ${trimmedRunID}.`
            : `Run ${trimmedRunID} is no longer active.`
        )
        await loadMonitorData({ silent: true })
      } catch (stopError) {
        setActionError(extractErrorMessage(stopError))
      }
    },
    [loadMonitorData]
  )

  const runningCount = useMemo(() => runs.filter((run) => run.status === "running").length, [runs])
  const subagentCount = useMemo(() => runs.filter((run) => run.role === "subagent").length, [runs])

  return (
    <div className="space-y-4 p-6" data-testid="agent-monitor-page">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Agent Monitor</h2>
        <p className="text-sm text-muted-foreground">
          Polls audit-backed internal runs so you can watch main agents and subagents, launch new work, and cancel active
          runs.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Launch controls</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          {featureDisabled ? (
            <div className="rounded-md border border-border bg-muted/30 p-4" data-testid="monitor-disabled-state">
              <p className="text-sm font-medium">Agent Monitor disabled</p>
              <p className="mt-1 text-sm text-muted-foreground">
                Instance agent controls are disabled for this control plane.
              </p>
            </div>
          ) : null}

          <div className="space-y-2">
            <label htmlFor="monitor-launch-prompt" className="text-sm font-medium">
              Launch prompt
            </label>
            <textarea
              id="monitor-launch-prompt"
              className="min-h-[96px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm outline-none focus-visible:ring-1 focus-visible:ring-ring"
              placeholder="Describe the work to start for any agent card below."
              value={promptDraft}
              disabled={featureDisabled}
              onChange={(event) => setPromptDraft(event.target.value)}
            />
          </div>

          <div className="flex flex-wrap items-end gap-3">
            <label htmlFor="monitor-thinking-mode" className="space-y-1 text-sm">
              <span className="block">Thinking mode</span>
              <select
                id="monitor-thinking-mode"
                aria-label="Thinking mode"
                className="h-10 rounded-md border bg-background px-3 text-sm"
                value={thinkingMode}
                disabled={featureDisabled}
                onChange={(event) => setThinkingMode(event.target.value as ThinkingMode)}
              >
                {THINKING_MODES.map((mode) => (
                  <option key={mode} value={mode}>
                    thinking: {mode}
                  </option>
                ))}
              </select>
            </label>

            <Button type="button" variant="outline" onClick={() => void loadMonitorData()} disabled={loading || featureDisabled}>
              {loading ? "Refreshing..." : "Refresh now"}
            </Button>
          </div>

          {actionStatus ? <p className="text-sm text-muted-foreground">{actionStatus}</p> : null}
          {actionError ? <p className="text-sm text-destructive">{actionError}</p> : null}
          {!featureDisabled && error ? <p className="text-sm text-destructive">Monitor load failed: {error}</p> : null}
        </CardContent>
      </Card>

      {!featureDisabled ? (
        <p className="text-sm text-muted-foreground" data-testid="monitor-summary">
          {runs.length} recent internal runs tracked · {runningCount} active · {subagentCount} subagent runs.
        </p>
      ) : null}

      {!featureDisabled ? <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {availableAgents.map((agentID) => {
          const mainRuns = runs.filter((run) => run.agentID === agentID && run.role === "main")
          const subRuns = runs.filter((run) => run.agentID === agentID && run.role === "subagent")
          const activeRun = [...mainRuns, ...subRuns].find((run) => run.status === "running") || null

          const summary = agentSummaries[agentID]
          const skillsText = summary?.activatedSkills?.length ? summary.activatedSkills.join(", ") : ""
          const modelText = [summary?.modelProvider, summary?.modelName].filter((value) => Boolean(value)).join("/")

          const latestMain = mainRuns[0] || null
          const latestSub = subRuns[0] || null
          const latestCheckpoint = [...mainRuns, ...subRuns].find((run) => run.checkpointPath) || null

          return (
            <Card key={agentID} data-testid={`monitor-agent-card-${sanitizeForTestID(agentID)}`}>
              <CardHeader className="pb-3">
                <CardTitle className="text-base">{agentID}</CardTitle>
              </CardHeader>
              <CardContent className="space-y-2 text-sm">
                <p className="text-muted-foreground">
                  main: {mainRuns.length} · subagent: {subRuns.length} · active: {activeRun ? activeRun.runID : "none"}
                </p>
                <p className="text-muted-foreground">
                  self-improvement: {summary?.selfImprovementReady ? "ready" : "off"} · skills: {skillsText || "none"}
                  {modelText ? ` · model: ${modelText}` : ""}
                </p>
                <p className="text-muted-foreground">
                  Latest main: {latestMain ? `${latestMain.status || "-"} · ${compactText(latestMain.taskID || latestMain.message || "-")}` : "none"}
                </p>
                <p className="text-muted-foreground">
                  Latest subagent: {latestSub ? `${latestSub.status || "-"} · ${compactText(latestSub.taskID || latestSub.message || "-")}` : "none"}
                </p>
                <p className="text-muted-foreground">
                  Active detail: {activeRun ? `${compactText(activeRun.taskID || activeRun.message || activeRun.runID)} · ${formatDateTime(activeRun.startedAt || activeRun.completedAt)}` : "none"}
                </p>
                <p className="text-muted-foreground">
                  Latest checkpoint: {latestCheckpoint ? compactText(latestCheckpoint.checkpointPath, 72) : "none"}
                </p>

                <div className="flex flex-wrap gap-2 pt-1">
                  <Button type="button" size="sm" variant="outline" onClick={() => void startAgent(agentID)}>
                    Start
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => {
                      if (activeRun) {
                        void stopRun(activeRun)
                      }
                    }}
                    disabled={!activeRun}
                  >
                    {activeRun ? "Stop active" : "No active run"}
                  </Button>
                </div>
              </CardContent>
            </Card>
          )
        })}
      </div> : null}

      {!featureDisabled ? <Card>
        <CardHeader>
          <CardTitle className="text-base">Recent Main + Subagent Runs</CardTitle>
        </CardHeader>
        <CardContent>
          {runs.length === 0 ? (
            <p className="text-sm text-muted-foreground">{loading ? "Loading monitor data..." : "No audit-backed runs found yet."}</p>
          ) : (
            <Table data-testid="monitor-runs-table">
              <TableHeader>
                <TableRow>
                  <TableHead>Agent</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Task</TableHead>
                  <TableHead>Model</TableHead>
                  <TableHead>Checkpoint</TableHead>
                  <TableHead>Error</TableHead>
                  <TableHead>Started</TableHead>
                  <TableHead>Run ID</TableHead>
                  <TableHead>Action</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {runs.map((run) => {
                  const isRunning = run.status === "running"
                  const taskText = compactText(run.taskID || run.message || run.source || "-", 56) || "-"
                  const modelText =
                    compactText(
                      [run.modelProvider, run.modelName].filter((value) => value.length > 0).join("/"),
                      40
                    ) || "-"
                  const checkpointText = compactText(run.checkpointPath || "-", 48) || "-"
                  const errorText = compactText(run.error || "-", 56) || "-"

                  return (
                    <TableRow
                      key={`${run.instanceID || "default"}:${run.agentID}:${run.runID}`}
                      data-testid={`monitor-run-row-${sanitizeForTestID(`${run.instanceID || "default"}-${run.agentID}-${run.runID}`)}`}
                    >
                      <TableCell>{run.instanceID ? `${run.instanceID}/${run.agentID || "-"}` : run.agentID || "-"}</TableCell>
                      <TableCell>{run.role || "main"}</TableCell>
                      <TableCell>{run.status || "-"}</TableCell>
                      <TableCell>{taskText}</TableCell>
                      <TableCell>{modelText}</TableCell>
                      <TableCell>{checkpointText}</TableCell>
                      <TableCell>{errorText}</TableCell>
                      <TableCell>{formatDateTime(run.startedAt || run.completedAt)}</TableCell>
                      <TableCell>
                        <code>{run.runID}</code>
                      </TableCell>
                      <TableCell>
                        {isRunning ? (
                          <Button type="button" size="sm" variant="outline" onClick={() => void stopRun(run)}>
                            Stop
                          </Button>
                        ) : (
                          "-"
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card> : null}
    </div>
  )
}
