import { useCallback, useEffect, useMemo, useState } from "react"
import { DecisionTimeline } from "@/components/DecisionTimeline"
import { TaskGraphPreview, type DecompositionPlan, type DecompositionTaskNode, type PlanDependencyEdge } from "@/components/TaskGraphPreview"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { flattenDecisionRecords, parseRunDecisionNode, type FlattenedDecisionRecord } from "@/lib/decisions"
import { ApiError, api } from "@/lib/api"

type RunSummary = {
  id: string
  status: string
  updatedAt: string
  decompositionPlan: DecompositionPlan | null
}

type RunsPage = {
  runs: RunSummary[]
  total: number
  limit: number
  offset: number
}

const modernDelegationModes = ["suggest_only", "approve_plan", "auto_trusted", "full_autonomous"] as const
const legacyDelegationModes = ["prompt_only", "tool_gated", "auto_execute"] as const
const supportedDelegationModes = [...modernDelegationModes, ...legacyDelegationModes]
const runsPageSize = 100
const maxRunsPagesToScan = 20

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

function extractErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    const details = asRecord(error.details)
    const nested = asRecord(details?.error)
    const nestedMessage = asText(nested?.message).trim()
    if (nestedMessage) {
      return nestedMessage
    }
    const detailsMessage = asText(details?.message).trim()
    if (detailsMessage) {
      return detailsMessage
    }
  }

  if (error instanceof Error) {
    return error.message || "Unknown error"
  }
  return asText(error).trim() || "Unknown error"
}

function parseRunSummary(value: unknown): RunSummary | null {
  const raw = asRecord(value)
  if (!raw) {
    return null
  }
  const id = asText(raw.id).trim()
  if (!id) {
    return null
  }

  const trace = asRecord(raw.trace)
  const decompositionPlan = parsePlan(raw.decomposition_plan) || parsePlan(trace?.decomposition_plan)

  return {
    id,
    status: asText(raw.status).trim(),
    updatedAt: asText(raw.updated_at).trim(),
    decompositionPlan,
  }
}

function parseRunsPage(payload: unknown): RunsPage {
  const record = asRecord(payload)
  const rawRuns = Array.isArray(record?.runs) ? record.runs : []
  const runs = rawRuns.map(parseRunSummary).filter((run): run is RunSummary => run !== null)

  return {
    runs,
    total: Math.max(0, Math.round(asNumber(record?.total))),
    limit: Math.max(1, Math.round(asNumber(record?.limit)) || 50),
    offset: Math.max(0, Math.round(asNumber(record?.offset))),
  }
}

function parseTask(value: unknown): DecompositionTaskNode | null {
  const raw = asRecord(value)
  if (!raw) {
    return null
  }

  const taskID = asText(raw.task_id).trim()
  if (!taskID) {
    return null
  }

  const dependsOn = Array.isArray(raw.depends_on)
    ? raw.depends_on.map((item) => asText(item).trim()).filter(Boolean)
    : []

  return {
    taskID,
    description: asText(raw.description).trim(),
    assignedRole: asText(raw.assigned_role).trim(),
    confidence: asNumber(raw.confidence),
    dependsOn,
  }
}

function parseEdge(value: unknown): PlanDependencyEdge | null {
  const raw = asRecord(value)
  if (!raw) {
    return null
  }
  const fromTaskID = asText(raw.from_task_id).trim()
  const toTaskID = asText(raw.to_task_id).trim()
  if (!fromTaskID || !toTaskID) {
    return null
  }
  return { fromTaskID, toTaskID }
}

function parsePlan(value: unknown): DecompositionPlan | null {
  const raw = asRecord(value)
  if (!raw) {
    return null
  }

  const tasks = Array.isArray(raw.tasks)
    ? raw.tasks.map(parseTask).filter((task): task is DecompositionTaskNode => task !== null)
    : []

  if (tasks.length === 0) {
    return null
  }

  const dependencyDAG = Array.isArray(raw.dependency_dag)
    ? raw.dependency_dag.map(parseEdge).filter((edge): edge is PlanDependencyEdge => edge !== null)
    : []

  return {
    delegationMode: asText(raw.delegation_mode).trim(),
    triggerReason: asText(raw.trigger_reason).trim(),
    tasks,
    dependencyDAG,
    minConfidence: asNumber(raw.min_confidence),
    avgConfidence: asNumber(raw.avg_confidence),
    allRolesBuiltIn: Boolean(raw.all_roles_built_in),
    generatedAt: asText(raw.generated_at).trim(),
  }
}

function extractPlanFromRunDetail(value: unknown): DecompositionPlan | null {
  const raw = asRecord(value)
  if (!raw) {
    return null
  }

  const topLevelPlan = parsePlan(raw.decomposition_plan)
  if (topLevelPlan) {
    return topLevelPlan
  }

  const trace = asRecord(raw.trace)
  return parsePlan(trace?.decomposition_plan)
}

function parseDelegationMode(rawConfig: Record<string, unknown>): string {
  const agents = asRecord(rawConfig.agents)
  const mode = asText(agents?.delegation_mode).trim().toLowerCase()
  if (supportedDelegationModes.includes(mode as (typeof supportedDelegationModes)[number])) {
    return mode
  }
  return "suggest_only"
}

function formatRunOption(run: RunSummary): string {
  const status = run.status || "unknown"
  return `${run.id} (${status})`
}

function findFirstDivergence(left: FlattenedDecisionRecord[], right: FlattenedDecisionRecord[]): string {
  const maxLength = Math.max(left.length, right.length)
  for (let index = 0; index < maxLength; index += 1) {
    const leftRecord = left[index]
    const rightRecord = right[index]
    if (!leftRecord || !rightRecord) {
      return `Divergence at position ${index + 1}: one run contains more decision records.`
    }
    if (leftRecord.recordType !== rightRecord.recordType || leftRecord.humanSummary !== rightRecord.humanSummary) {
      return `Divergence at position ${index + 1}: ${leftRecord.recordType} vs ${rightRecord.recordType}.`
    }
  }
  return "No divergence detected in ordered decision records."
}

export function DelegationPage() {
  const [mode, setMode] = useState("suggest_only")
  const [threshold, setThreshold] = useState(2)
  const [cooldown, setCooldown] = useState(15)
  const [autoDelegate, setAutoDelegate] = useState(false)

  const [runs, setRuns] = useState<RunSummary[]>([])
  const [planRuns, setPlanRuns] = useState<RunSummary[]>([])
  const [selectedRunID, setSelectedRunID] = useState("")
  const [selectedPlan, setSelectedPlan] = useState<DecompositionPlan | null>(null)

  const [leftRunID, setLeftRunID] = useState("")
  const [rightRunID, setRightRunID] = useState("")

  const [leftRecords, setLeftRecords] = useState<FlattenedDecisionRecord[]>([])
  const [rightRecords, setRightRecords] = useState<FlattenedDecisionRecord[]>([])

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [loadingPlan, setLoadingPlan] = useState(false)
  const [comparing, setComparing] = useState(false)

  const [loadError, setLoadError] = useState("")
  const [saveError, setSaveError] = useState("")
  const [saveNotice, setSaveNotice] = useState("")
  const [planError, setPlanError] = useState("")
  const [runListNotice, setRunListNotice] = useState("")
  const [actionNotice, setActionNotice] = useState("")
  const [comparisonError, setComparisonError] = useState("")

  const divergenceSummary = useMemo(() => findFirstDivergence(leftRecords, rightRecords), [leftRecords, rightRecords])

  const loadPolicyConfig = useCallback(async () => {
    const configPayload = await api.get<Record<string, unknown>>("/api/admin/config")
    const agents = asRecord(configPayload.agents)

    setMode(parseDelegationMode(configPayload))

    const nextThreshold = Math.round(asNumber(agents?.delegation_threshold))
    setThreshold(nextThreshold > 0 ? nextThreshold : 2)

    const nextCooldown = Math.round(asNumber(agents?.delegation_cooldown_iterations))
    setCooldown(nextCooldown > 0 ? nextCooldown : 15)

    setAutoDelegate(Boolean(agents?.auto_delegate))
  }, [])

  const loadRuns = useCallback(async (): Promise<{ allRuns: RunSummary[]; planRuns: RunSummary[] }> => {
    let nextOffset = 0
    let total = Number.POSITIVE_INFINITY
    let pagesFetched = 0

    const allRuns: RunSummary[] = []
    const runsWithPlan: RunSummary[] = []
    const seen = new Set<string>()

    while (nextOffset < total && pagesFetched < maxRunsPagesToScan) {
      const runsPayload = await api.get<Record<string, unknown>>(
        `/v1/runs?limit=${encodeURIComponent(String(runsPageSize))}&offset=${encodeURIComponent(String(nextOffset))}`
      )
      const page = parseRunsPage(runsPayload)
      total = page.total
      pagesFetched += 1

      for (const run of page.runs) {
        if (seen.has(run.id)) {
          continue
        }
        seen.add(run.id)
        allRuns.push(run)
        if (run.decompositionPlan) {
          runsWithPlan.push(run)
        }
      }

      if (page.runs.length === 0) {
        break
      }
      nextOffset += page.limit
    }

    const nextAllRuns = allRuns
    const nextPlanRuns = runsWithPlan
    setRuns(nextAllRuns)
    setPlanRuns(nextPlanRuns)

    const hitScanLimit = pagesFetched >= maxRunsPagesToScan && nextOffset < total

    if (nextPlanRuns.length > 0) {
      setRunListNotice(
        hitScanLimit
          ? `Loaded ${nextAllRuns.length} runs from the latest ${maxRunsPagesToScan} pages. Older runs may require direct deep-links.`
          : ""
      )
    } else if (hitScanLimit && nextAllRuns.length > 0) {
      setRunListNotice(
        `Loaded ${nextAllRuns.length} runs from the latest ${maxRunsPagesToScan} pages. No decomposition plans were found in that range.`
      )
    } else if (nextAllRuns.length > 0) {
      setRunListNotice("No runs in the scanned window exposed decomposition plans. Select a run and reload to re-check detail data.")
    } else {
      setRunListNotice("No runs found.")
    }

    return { allRuns: nextAllRuns, planRuns: nextPlanRuns }
  }, [])

  const loadRunPlan = useCallback(async (runID: string, fallbackPlan: DecompositionPlan | null = null) => {
    const targetRunID = runID.trim()
    if (!targetRunID) {
      setSelectedPlan(null)
      setPlanError("")
      return
    }

    setLoadingPlan(true)
    setPlanError("")
    setActionNotice("")
    try {
      const detailPayload = await api.get<unknown>(`/v1/runs/${encodeURIComponent(targetRunID)}`)
      const plan = extractPlanFromRunDetail(detailPayload) || fallbackPlan
      if (!plan) {
        setSelectedPlan(null)
        setPlanError("This run does not include a decomposition plan.")
        return
      }
      setSelectedPlan(plan)
    } catch (error) {
      setSelectedPlan(null)
      setPlanError(`Failed to load run plan: ${extractErrorMessage(error)}`)
    } finally {
      setLoadingPlan(false)
    }
  }, [])

  const initialize = useCallback(async () => {
    setLoading(true)
    setLoadError("")
    try {
      const [_, loadedRuns] = await Promise.all([loadPolicyConfig(), loadRuns()])
      const initialPlanRun = loadedRuns.planRuns[0] || loadedRuns.allRuns[0] || null
      const initialRunID = initialPlanRun?.id || ""
      setSelectedRunID(initialRunID)
      setLeftRunID(loadedRuns.allRuns[0]?.id || "")
      setRightRunID(loadedRuns.allRuns[1]?.id || loadedRuns.allRuns[0]?.id || "")
      if (initialRunID) {
        await loadRunPlan(initialRunID, initialPlanRun?.decompositionPlan || null)
      } else {
        setSelectedPlan(null)
      }
    } catch (error) {
      setLoadError(`Failed to load delegation page: ${extractErrorMessage(error)}`)
    } finally {
      setLoading(false)
    }
  }, [loadPolicyConfig, loadRunPlan, loadRuns])

  useEffect(() => {
    void initialize()
  }, [initialize])

  const savePolicy = useCallback(async () => {
    setSaving(true)
    setSaveError("")
    setSaveNotice("")
    try {
      await api.patch("/api/admin/config", {
        agents: {
          delegation_mode: mode,
          delegation_threshold: threshold,
          delegation_cooldown_iterations: cooldown,
          auto_delegate: autoDelegate,
        },
      })
      setSaveNotice("Delegation policy saved.")
    } catch (error) {
      setSaveError(`Failed to save policy: ${extractErrorMessage(error)}`)
    } finally {
      setSaving(false)
    }
  }, [autoDelegate, cooldown, mode, threshold])

  const compareRuns = useCallback(async () => {
    const left = leftRunID.trim()
    const right = rightRunID.trim()
    if (!left || !right) {
      setComparisonError("Select two runs to compare.")
      setLeftRecords([])
      setRightRecords([])
      return
    }

    setComparing(true)
    setComparisonError("")
    try {
      const [leftPayload, rightPayload] = await Promise.all([
        api.get<unknown>(`/api/admin/runs/${encodeURIComponent(left)}/decisions`),
        api.get<unknown>(`/api/admin/runs/${encodeURIComponent(right)}/decisions`),
      ])

      const leftNode = parseRunDecisionNode(leftPayload)
      const rightNode = parseRunDecisionNode(rightPayload)
      setLeftRecords(flattenDecisionRecords(leftNode))
      setRightRecords(flattenDecisionRecords(rightNode))
    } catch (error) {
      setLeftRecords([])
      setRightRecords([])
      setComparisonError(`Failed to compare runs: ${extractErrorMessage(error)}`)
    } finally {
      setComparing(false)
    }
  }, [leftRunID, rightRunID])

  const showApprovalActions = selectedPlan?.delegationMode === "suggest_only" || selectedPlan?.delegationMode === "approve_plan"

  return (
    <div className="space-y-4 p-6" data-testid="delegation-page">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Delegation</h2>
        <p className="text-sm text-muted-foreground">
          Configure delegation policy, preview decomposition task graphs, and compare decision ledgers between runs.
        </p>
      </div>

      {loadError ? (
        <div className="rounded-md border border-destructive/50 bg-destructive/5 p-3">
          <p className="text-sm text-destructive">{loadError}</p>
          <Button className="mt-2" size="sm" variant="outline" onClick={() => void initialize()}>
            Retry
          </Button>
        </div>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Delegation policy editor</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            <label className="space-y-1 text-sm" htmlFor="delegation-mode-select">
              <span>Mode</span>
              <select
                id="delegation-mode-select"
                data-testid="delegation-mode-select"
                className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                value={mode}
                disabled={loading || saving}
                onChange={(event) => setMode(event.target.value)}
              >
                {modernDelegationModes.map((modeName) => (
                  <option key={modeName} value={modeName}>
                    {modeName}
                  </option>
                ))}
                {legacyDelegationModes.map((modeName) => (
                  <option key={modeName} value={modeName}>
                    {modeName} (legacy)
                  </option>
                ))}
              </select>
            </label>

            <label className="space-y-1 text-sm" htmlFor="delegation-threshold-slider">
              <span>Delegation threshold ({threshold})</span>
              <Input
                id="delegation-threshold-slider"
                data-testid="delegation-threshold-slider"
                type="range"
                min={1}
                max={10}
                step={1}
                value={String(threshold)}
                disabled={loading || saving}
                onChange={(event) => setThreshold(Math.max(1, Math.min(10, Number(event.target.value) || 1)))}
              />
            </label>

            <label className="space-y-1 text-sm" htmlFor="delegation-cooldown-input">
              <span>Cooldown (iterations)</span>
              <Input
                id="delegation-cooldown-input"
                data-testid="delegation-cooldown-input"
                type="number"
                min={0}
                value={String(cooldown)}
                disabled={loading || saving}
                onChange={(event) => setCooldown(Math.max(0, Number(event.target.value) || 0))}
              />
            </label>

            <label className="flex items-center gap-2 self-end rounded-md border px-3 py-2 text-sm">
              <input
                data-testid="delegation-auto-toggle"
                type="checkbox"
                checked={autoDelegate}
                disabled={loading || saving}
                onChange={(event) => setAutoDelegate(event.target.checked)}
              />
              <span>Enable auto-delegate</span>
            </label>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <Button data-testid="delegation-save-button" disabled={loading || saving} onClick={() => void savePolicy()}>
              {saving ? "Saving..." : "Save policy"}
            </Button>
            {saveNotice ? (
              <span data-testid="delegation-save-notice" className="text-sm text-emerald-600">
                {saveNotice}
              </span>
            ) : null}
          </div>

          {saveError ? <p className="text-sm text-destructive">{saveError}</p> : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Task graph preview</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex flex-wrap items-end gap-3">
            <label htmlFor="delegation-run-select" className="min-w-72 space-y-1 text-sm">
              <span>Run with decomposition plan</span>
              <select
                id="delegation-run-select"
                data-testid="delegation-run-select"
                className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                value={selectedRunID}
                disabled={loading || loadingPlan || (planRuns.length === 0 && runs.length === 0)}
                onChange={(event) => {
                  const nextRunID = event.target.value
                  const selectedRun = planRuns.find((run) => run.id === nextRunID) || runs.find((run) => run.id === nextRunID)
                  setSelectedRunID(nextRunID)
                  void loadRunPlan(nextRunID, selectedRun?.decompositionPlan || null)
                }}
              >
                {(planRuns.length > 0 ? planRuns : runs).map((run) => (
                  <option key={run.id} value={run.id}>
                    {formatRunOption(run)}
                  </option>
                ))}
              </select>
            </label>

            <Button
              variant="outline"
              size="sm"
              disabled={loading || loadingPlan}
              onClick={() => {
                const selectedRun = planRuns.find((run) => run.id === selectedRunID) || runs.find((run) => run.id === selectedRunID)
                void loadRunPlan(selectedRunID, selectedRun?.decompositionPlan || null)
              }}
            >
              Reload plan
            </Button>
          </div>

          {runListNotice ? <p className="text-sm text-muted-foreground">{runListNotice}</p> : null}
          {loadingPlan ? <p className="text-sm text-muted-foreground">Loading decomposition plan...</p> : null}
          {planError ? <p className="text-sm text-destructive">{planError}</p> : null}

          {selectedPlan ? (
            <TaskGraphPreview
              plan={selectedPlan}
              showApprovalActions={showApprovalActions}
              actionNotice={actionNotice}
              onApprove={() => setActionNotice(`Plan approved for run ${selectedRunID}.`)}
              onReject={() => setActionNotice(`Plan rejected for run ${selectedRunID}.`)}
            />
          ) : null}

          {!selectedPlan && !loadingPlan && !planError ? (
            <p className="text-sm text-muted-foreground">Select a run to preview its decomposition graph.</p>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Run comparison</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-[1fr_1fr_auto]">
            <label className="space-y-1 text-sm" htmlFor="run-compare-left-select">
              <span>Run A</span>
              <select
                id="run-compare-left-select"
                data-testid="run-compare-left-select"
                className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                value={leftRunID}
                disabled={loading || comparing || runs.length === 0}
                onChange={(event) => setLeftRunID(event.target.value)}
              >
                {runs.map((run) => (
                  <option key={`left-${run.id}`} value={run.id}>
                    {formatRunOption(run)}
                  </option>
                ))}
              </select>
            </label>

            <label className="space-y-1 text-sm" htmlFor="run-compare-right-select">
              <span>Run B</span>
              <select
                id="run-compare-right-select"
                data-testid="run-compare-right-select"
                className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                value={rightRunID}
                disabled={loading || comparing || runs.length === 0}
                onChange={(event) => setRightRunID(event.target.value)}
              >
                {runs.map((run) => (
                  <option key={`right-${run.id}`} value={run.id}>
                    {formatRunOption(run)}
                  </option>
                ))}
              </select>
            </label>

            <div className="flex items-end">
              <Button
                data-testid="run-compare-load"
                variant="outline"
                disabled={loading || comparing || !leftRunID || !rightRunID}
                onClick={() => void compareRuns()}
              >
                {comparing ? "Comparing..." : "Compare runs"}
              </Button>
            </div>
          </div>

          {comparisonError ? <p className="text-sm text-destructive">{comparisonError}</p> : null}

          {(leftRecords.length > 0 || rightRecords.length > 0) ? (
            <p data-testid="run-compare-divergence" className="rounded-md border bg-muted/20 p-2 text-sm">
              <strong>Divergence:</strong> {divergenceSummary}
            </p>
          ) : null}

          <div className="grid gap-4 xl:grid-cols-2">
            <div data-testid="run-compare-left-panel" className="space-y-2 rounded-md border p-3">
              <p className="text-sm font-medium">Run A: {leftRunID || "-"}</p>
              <DecisionTimeline records={leftRecords} emptyLabel="No records loaded for run A." testID="run-compare-left-timeline" />
            </div>

            <div data-testid="run-compare-right-panel" className="space-y-2 rounded-md border p-3">
              <p className="text-sm font-medium">Run B: {rightRunID || "-"}</p>
              <DecisionTimeline records={rightRecords} emptyLabel="No records loaded for run B." testID="run-compare-right-timeline" />
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
