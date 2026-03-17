import { useCallback, useEffect, useMemo, useState } from "react"
import { ApiError, api } from "@/lib/api"
import { CodeEditor } from "@/components/CodeEditor"
import { useToast } from "@/hooks/useToast"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { useControlPlaneFeatures } from "@/hooks/useControlPlaneFeatures"

type LayerDefinition = {
  id: string
  label: string
  description: string
}

type PromptLayer = {
  layerID: string
  content: string
  version: number
  updatedAt: string
}

type PromptPreviewLayer = {
  layerID: string
  content: string
  version: number
  updatedAt: string
  wordCount: number
  tokenCount: number
}

type PromptPreview = {
  assembledPrompt: string
  totalTokens: number
  estimationMethod: string
  layers: PromptPreviewLayer[]
}

type PromptVersion = {
  version: number
  updatedAt: string
  changedLayer: string
  layerVersion: number
}

type PromptDiffLine = {
  type: string
  content: string
}

type PromptDiff = {
  lines: PromptDiffLine[]
}

type LintIssue = {
  severity: string
  description: string
  layerID: string
  suggestedFix: string
}

type StructuralCheck = {
  name: string
  passed: boolean
  explanation: string
}

type StructuralTestResult = {
  passed: boolean
  checks: StructuralCheck[]
}

type InstanceSummary = {
  id: string
  name: string
}

const LAYER_DEFINITIONS: LayerDefinition[] = [
  {
    id: "global_operator_policy",
    label: "Global Operator Policy",
    description: "Top-level guardrails and operator requirements.",
  },
  {
    id: "agent_identity",
    label: "Agent Identity",
    description: "Mission, persona, and agent-specific behavior.",
  },
  {
    id: "tool_safety_rules",
    label: "Tool-Use & Safety",
    description: "Allowed/denied tools and safety boundaries.",
  },
  {
    id: "delegation_policy",
    label: "Delegation Policy",
    description: "Subagent routing and delegation constraints.",
  },
  {
    id: "session_overlay",
    label: "Session Overlay",
    description: "Task/session specific overlays and runtime notes.",
  },
]

const DEFAULT_CONTEXT_WINDOW = 8192

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

function asPositiveInt(value: unknown): number {
  const parsed = Number(value)
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return 0
  }
  return Math.round(parsed)
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

function normalizeAgentIDs(payload: unknown): string[] {
  if (!Array.isArray(payload)) {
    return []
  }

  const seen = new Set<string>()
  const out: string[] = []
  for (const item of payload) {
    const candidate = typeof item === "string"
      ? item
      : asText(asRecord(item)?.id) || asText(asRecord(item)?.agent_id)
    const normalized = candidate.trim()
    if (!normalized || seen.has(normalized)) {
      continue
    }
    seen.add(normalized)
    out.push(normalized)
  }
  return out
}

function normalizeInstances(payload: unknown): InstanceSummary[] {
  if (!Array.isArray(payload)) {
    return []
  }

  const seen = new Set<string>()
  const out: InstanceSummary[] = []
  for (const item of payload) {
    const record = asRecord(item)
    const id = asText(record?.id).trim()
    if (!id || seen.has(id)) {
      continue
    }
    seen.add(id)
    out.push({ id, name: asText(record?.name).trim() || id })
  }
  return out
}

function buildPromptStackRoute(instanceID: string, agentID: string, suffix = ""): string {
  const base = `/api/admin/instances/${encodeURIComponent(instanceID)}/agents/${encodeURIComponent(agentID)}/prompt-stack`
  return suffix ? `${base}/${suffix}` : base
}

function findLayerLabel(layerID: string): string {
  const match = LAYER_DEFINITIONS.find((layer) => layer.id === layerID)
  return match?.label || layerID
}

function parseContextWindow(configPayload: unknown): number {
  const root = asRecord(configPayload)
  const model = asRecord(root?.model)

  const candidates: unknown[] = [
    model?.context_window,
    model?.contextWindow,
    model?.max_input_tokens,
    model?.maxInputTokens,
    root?.context_window,
  ]

  for (const candidate of candidates) {
    const value = asPositiveInt(candidate)
    if (value > 0) {
      return value
    }
  }

  return DEFAULT_CONTEXT_WINDOW
}

function parsePromptLayers(payload: unknown): PromptLayer[] {
  const rawLayers = Array.isArray(payload) ? payload : []
  const layerByID = new Map<string, PromptLayer>()

  for (const item of rawLayers) {
    const record = asRecord(item)
    if (!record) {
      continue
    }
    const layerID = asText(record.layer_id).trim()
    if (!layerID) {
      continue
    }
    layerByID.set(layerID, {
      layerID,
      content: asText(record.content),
      version: asPositiveInt(record.version),
      updatedAt: asText(record.updated_at),
    })
  }

  return LAYER_DEFINITIONS.map((definition) => {
    const existing = layerByID.get(definition.id)
    if (existing) {
      return existing
    }
    return {
      layerID: definition.id,
      content: "",
      version: 0,
      updatedAt: "",
    }
  })
}

function parsePreview(payload: unknown): PromptPreview {
  const record = asRecord(payload)
  const parsedLayers = Array.isArray(record?.layers)
    ? record.layers.map((item) => {
        const layer = asRecord(item)
        return {
          layerID: asText(layer?.layer_id).trim(),
          content: asText(layer?.content),
          version: asPositiveInt(layer?.version),
          updatedAt: asText(layer?.updated_at),
          wordCount: asPositiveInt(layer?.word_count),
          tokenCount: asPositiveInt(layer?.token_count),
        }
      }).filter((layer) => layer.layerID)
    : []

  const byID = new Map<string, PromptPreviewLayer>()
  for (const layer of parsedLayers) {
    byID.set(layer.layerID, layer)
  }

  const layers = LAYER_DEFINITIONS.map((definition) => {
    const existing = byID.get(definition.id)
    if (existing) {
      return existing
    }
    return {
      layerID: definition.id,
      content: "",
      version: 0,
      updatedAt: "",
      wordCount: 0,
      tokenCount: 0,
    }
  })

  return {
    assembledPrompt: asText(record?.assembled_prompt),
    totalTokens: asPositiveInt(record?.total_tokens),
    estimationMethod: asText(record?.estimation_method),
    layers,
  }
}

function parseVersions(payload: unknown): PromptVersion[] {
  const record = asRecord(payload)
  const rawVersions = Array.isArray(record?.versions) ? record.versions : []

  const versions = rawVersions.map((item) => {
    const row = asRecord(item)
    return {
      version: asPositiveInt(row?.version),
      updatedAt: asText(row?.updated_at),
      changedLayer: asText(row?.changed_layer),
      layerVersion: asPositiveInt(row?.layer_version),
    }
  }).filter((row) => row.version > 0)

  versions.sort((a, b) => a.version - b.version)
  return versions
}

function parseDiff(payload: unknown): PromptDiff {
  const record = asRecord(payload)
  const diffRecord = asRecord(record?.diff)
  const linesRaw = Array.isArray(diffRecord?.lines) ? diffRecord.lines : []

  return {
    lines: linesRaw.map((item) => {
      const line = asRecord(item)
      return {
        type: asText(line?.type).trim().toLowerCase() || "unchanged",
        content: asText(line?.content),
      }
    }),
  }
}

function parseLintIssues(payload: unknown): LintIssue[] {
  const record = asRecord(payload)
  const issuesRaw = Array.isArray(record?.issues) ? record.issues : []
  return issuesRaw.map((item) => {
    const issue = asRecord(item)
    return {
      severity: asText(issue?.severity).trim().toLowerCase() || "info",
      description: asText(issue?.description),
      layerID: asText(issue?.layer_id),
      suggestedFix: asText(issue?.suggested_fix),
    }
  })
}

function parseStructuralTest(payload: unknown): StructuralTestResult {
  const record = asRecord(payload)
  const checksRaw = Array.isArray(record?.checks) ? record.checks : []
  return {
    passed: Boolean(record?.passed),
    checks: checksRaw.map((item) => {
      const check = asRecord(item)
      return {
        name: asText(check?.name),
        passed: Boolean(check?.passed),
        explanation: asText(check?.explanation),
      }
    }),
  }
}

function formatTimestamp(raw: string): string {
  if (!raw) {
    return "Unknown time"
  }

  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) {
    return raw
  }
  return date.toLocaleString()
}

function severityBadgeClass(severity: string): string {
  switch (severity) {
    case "error":
      return "bg-destructive text-destructive-foreground"
    case "warning":
      return "bg-amber-500 text-black"
    default:
      return "bg-muted text-muted-foreground"
  }
}

function diffLineClass(type: string): string {
  switch (type) {
    case "added":
      return "bg-emerald-500/10 text-emerald-700"
    case "removed":
      return "bg-red-500/10 text-red-700"
    default:
      return "text-muted-foreground"
  }
}

function diffPrefix(type: string): string {
  switch (type) {
    case "added":
      return "+"
    case "removed":
      return "-"
    default:
      return " "
  }
}

export function PromptStackPage() {
  const { features, loading: featuresLoading } = useControlPlaneFeatures()
  const { toast } = useToast()

  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState("")

  const [instances, setInstances] = useState<InstanceSummary[]>([])
  const [selectedInstance, setSelectedInstance] = useState("")
  const [agents, setAgents] = useState<string[]>([])
  const [selectedAgent, setSelectedAgent] = useState("")

  const [contextWindow, setContextWindow] = useState(DEFAULT_CONTEXT_WINDOW)

  const [layers, setLayers] = useState<PromptLayer[]>([])
  const [draftByLayerID, setDraftByLayerID] = useState<Record<string, string>>({})
  const [activeLayerID, setActiveLayerID] = useState(LAYER_DEFINITIONS[0].id)

  const [preview, setPreview] = useState<PromptPreview | null>(null)
  const [versions, setVersions] = useState<PromptVersion[]>([])

  const [saveNotice, setSaveNotice] = useState("")
  const [saveError, setSaveError] = useState("")
  const [savingLayer, setSavingLayer] = useState(false)

  const [selectedFromVersion, setSelectedFromVersion] = useState("")
  const [selectedToVersion, setSelectedToVersion] = useState("")
  const [selectedRollbackVersion, setSelectedRollbackVersion] = useState("")

  const [diff, setDiff] = useState<PromptDiff | null>(null)
  const [diffLoading, setDiffLoading] = useState(false)
  const [diffError, setDiffError] = useState("")

  const [lintIssues, setLintIssues] = useState<LintIssue[]>([])
  const [lintLoading, setLintLoading] = useState(false)
  const [lintError, setLintError] = useState("")

  const [testResult, setTestResult] = useState<StructuralTestResult | null>(null)
  const [testLoading, setTestLoading] = useState(false)
  const [testError, setTestError] = useState("")

  const activeLayer = useMemo(() => {
    return LAYER_DEFINITIONS.find((layer) => layer.id === activeLayerID) || LAYER_DEFINITIONS[0]
  }, [activeLayerID])

  const topContributors = useMemo(() => {
    if (!preview) {
      return []
    }
    return [...preview.layers]
      .sort((a, b) => b.tokenCount - a.tokenCount)
      .slice(0, 3)
  }, [preview])

  const totalTokens = preview?.totalTokens || 0
  const tokenUsageRatio = contextWindow > 0 ? totalTokens / contextWindow : 0
  const tokenUsagePercent = Math.min(100, Math.round(tokenUsageRatio * 100))
  const isOverflow = contextWindow > 0 && totalTokens > contextWindow
  const featureDisabled = !featuresLoading && !features.instanceAgents

  const applyLayers = useCallback((nextLayers: PromptLayer[]) => {
    setLayers(nextLayers)
    setDraftByLayerID((previous) => {
      const next = { ...previous }
      for (const layer of nextLayers) {
        next[layer.layerID] = layer.content
      }
      return next
    })
  }, [])

  const loadAgentPromptData = useCallback(async (instanceID: string, agentID: string) => {
    const [stackPayload, previewPayload, historyPayload] = await Promise.all([
      api.get<unknown>(buildPromptStackRoute(instanceID, agentID)),
      api.get<unknown>(buildPromptStackRoute(instanceID, agentID, "preview")),
      api.get<unknown>(buildPromptStackRoute(instanceID, agentID, "history")),
    ])

    const stackRecord = asRecord(stackPayload)
    const nextLayers = parsePromptLayers(stackRecord?.layers)
    const nextPreview = parsePreview(previewPayload)
    const nextVersions = parseVersions(historyPayload)

    applyLayers(nextLayers)
    setPreview(nextPreview)
    setVersions(nextVersions)
    setDiff(null)
    setDiffError("")
    setLintIssues([])
    setLintError("")
    setTestResult(null)
    setTestError("")

    if (nextVersions.length > 0) {
      const latest = nextVersions[nextVersions.length - 1].version
      const previous = nextVersions.length > 1
        ? nextVersions[nextVersions.length - 2].version
        : latest

      setSelectedRollbackVersion(String(latest))
      setSelectedToVersion(String(latest))
      setSelectedFromVersion(String(previous))
    } else {
      setSelectedRollbackVersion("")
      setSelectedToVersion("")
      setSelectedFromVersion("")
    }
  }, [applyLayers])

  const initialize = useCallback(async () => {
    setLoading(true)
    setLoadError("")

    try {
      const [instancesPayload, activePayload, configPayload] = await Promise.all([
        api.get<{ instances?: unknown }>("/api/admin/instances"),
        api.get<{ instance?: unknown }>("/api/admin/instances/active"),
        api.get<unknown>("/api/admin/config"),
      ])

      const nextInstances = normalizeInstances(instancesPayload.instances)
      const activeInstanceID = asText(asRecord(activePayload.instance)?.id).trim()
      const nextSelectedInstance =
        (activeInstanceID && nextInstances.some((instance) => instance.id === activeInstanceID) ? activeInstanceID : "") ||
        nextInstances[0]?.id ||
        ""

      const agentsForInstance = nextSelectedInstance
        ? await api.get<{ agents?: unknown }>(`/api/admin/instances/${encodeURIComponent(nextSelectedInstance)}/agents`)
        : { agents: [] }
      const ids = normalizeAgentIDs(agentsForInstance.agents)
      const selected = ids[0] || ""

      setInstances(nextInstances)
      setSelectedInstance(nextSelectedInstance)
      setAgents(ids)
      setContextWindow(parseContextWindow(configPayload))
      setSelectedAgent(selected)

      if (nextSelectedInstance && selected) {
        await loadAgentPromptData(nextSelectedInstance, selected)
      }
    } catch (error) {
      setLoadError(`Failed to load prompt stack page: ${extractErrorMessage(error)}`)
    } finally {
      setLoading(false)
    }
  }, [loadAgentPromptData])

  useEffect(() => {
    if (featuresLoading) {
      return
    }
    if (featureDisabled) {
      setLoading(false)
      setLoadError("")
      setInstances([])
      setSelectedInstance("")
      setAgents([])
      setSelectedAgent("")
      setLayers([])
      setDraftByLayerID({})
      setPreview(null)
      setVersions([])
      setDiff(null)
      setDiffError("")
      setLintIssues([])
      setLintError("")
      setTestResult(null)
      setTestError("")
      setSaveNotice("")
      setSaveError("")
      return
    }
    void initialize()
  }, [featureDisabled, featuresLoading, initialize])

  const handleAgentChange = useCallback(async (agentID: string) => {
    if (!selectedInstance || !agentID || agentID === selectedAgent) {
      return
    }

    setSelectedAgent(agentID)
    setLoading(true)
    setLoadError("")
    setSaveNotice("")
    setSaveError("")

    try {
      await loadAgentPromptData(selectedInstance, agentID)
    } catch (error) {
      setLoadError(`Failed to load prompt stack for ${agentID}: ${extractErrorMessage(error)}`)
    } finally {
      setLoading(false)
    }
  }, [loadAgentPromptData, selectedAgent, selectedInstance])

  const handleSaveLayer = useCallback(async () => {
    if (!selectedInstance || !selectedAgent || !activeLayer) {
      return
    }

    const content = draftByLayerID[activeLayer.id] ?? ""
    setSavingLayer(true)
    setSaveNotice("")
    setSaveError("")

    try {
      const encodedLayer = encodeURIComponent(activeLayer.id)
      const payload = await api.put<unknown>(
        buildPromptStackRoute(selectedInstance, selectedAgent, encodedLayer),
        { content }
      )

      const response = asRecord(payload)
      const updatedLayers = parsePromptLayers(response?.layers)
      applyLayers(updatedLayers)

      const [previewPayload, historyPayload] = await Promise.all([
        api.get<unknown>(buildPromptStackRoute(selectedInstance, selectedAgent, "preview")),
        api.get<unknown>(buildPromptStackRoute(selectedInstance, selectedAgent, "history")),
      ])

      setPreview(parsePreview(previewPayload))
      const parsedVersions = parseVersions(historyPayload)
      setVersions(parsedVersions)
      if (parsedVersions.length > 0) {
        const latest = parsedVersions[parsedVersions.length - 1].version
        setSelectedToVersion(String(latest))
        setSelectedRollbackVersion(String(latest))
      }

      setSaveNotice(`Saved ${activeLayer.label}.`)
      toast({
        title: "Layer saved",
        description: `${activeLayer.label} updated successfully.`,
      })
    } catch (error) {
      setSaveError(`Failed to save layer: ${extractErrorMessage(error)}`)
    } finally {
      setSavingLayer(false)
    }
  }, [activeLayer, applyLayers, draftByLayerID, selectedAgent, selectedInstance, toast])

  const handleLoadDiff = useCallback(async () => {
    if (!selectedInstance || !selectedAgent) {
      return
    }

    const fromVersion = asPositiveInt(selectedFromVersion)
    const toVersion = asPositiveInt(selectedToVersion)

    if (fromVersion < 1 || toVersion < 1) {
      setDiff(null)
      setDiffError("Select both versions to compare.")
      return
    }
    if (fromVersion === toVersion) {
      setDiff(null)
      setDiffError("Choose two different versions for diff view.")
      return
    }

    setDiffLoading(true)
    setDiffError("")

    try {
      const payload = await api.get<unknown>(
        `${buildPromptStackRoute(selectedInstance, selectedAgent, "diff")}?v1=${fromVersion}&v2=${toVersion}`
      )
      setDiff(parseDiff(payload))
    } catch (error) {
      setDiff(null)
      setDiffError(`Failed to load diff: ${extractErrorMessage(error)}`)
    } finally {
      setDiffLoading(false)
    }
  }, [selectedAgent, selectedFromVersion, selectedInstance, selectedToVersion])

  const handleRollback = useCallback(async () => {
    if (!selectedInstance || !selectedAgent) {
      return
    }
    const version = asPositiveInt(selectedRollbackVersion)
    if (version < 1) {
      setSaveError("Select a version to rollback.")
      return
    }

    setLoading(true)
    setSaveNotice("")
    setSaveError("")

    try {
      await api.post(buildPromptStackRoute(selectedInstance, selectedAgent, "rollback"), { version })
      await loadAgentPromptData(selectedInstance, selectedAgent)
      setSaveNotice(`Rolled back to version ${version}.`)
      toast({
        title: "Rollback complete",
        description: `Prompt stack restored to version ${version}.`,
      })
    } catch (error) {
      setSaveError(`Failed to rollback: ${extractErrorMessage(error)}`)
    } finally {
      setLoading(false)
    }
  }, [loadAgentPromptData, selectedAgent, selectedInstance, selectedRollbackVersion, toast])

  const handleLint = useCallback(async () => {
    if (!selectedInstance || !selectedAgent) {
      return
    }
    setLintLoading(true)
    setLintError("")

    try {
      const payload = await api.post<unknown>(buildPromptStackRoute(selectedInstance, selectedAgent, "lint"), {})
      setLintIssues(parseLintIssues(payload))
    } catch (error) {
      setLintIssues([])
      setLintError(`Failed to run lint: ${extractErrorMessage(error)}`)
    } finally {
      setLintLoading(false)
    }
  }, [selectedAgent, selectedInstance])

  const handleRunTests = useCallback(async () => {
    if (!selectedInstance || !selectedAgent) {
      return
    }
    setTestLoading(true)
    setTestError("")

    try {
      const payload = await api.post<unknown>(buildPromptStackRoute(selectedInstance, selectedAgent, "test"), {})
      setTestResult(parseStructuralTest(payload))
    } catch (error) {
      setTestResult(null)
      setTestError(`Failed to run structural tests: ${extractErrorMessage(error)}`)
    } finally {
      setTestLoading(false)
    }
  }, [selectedAgent, selectedInstance])

  return (
    <div className="space-y-4 p-6" data-testid="prompt-stack-page">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Prompt Stack</h2>
        <p className="text-sm text-muted-foreground">
          Edit all 5 prompt layers, inspect merged preview output, and manage versions with lint/test checks.
        </p>
      </div>

      {featureDisabled ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Prompt Stack unavailable</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="rounded-md border border-border bg-muted/30 p-4" data-testid="prompt-stack-disabled-state">
              <p className="text-sm font-medium">Prompt Stack disabled</p>
              <p className="mt-1 text-sm text-muted-foreground">
                Instance agent controls are disabled for this control plane.
              </p>
            </div>
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Prompt stack controls</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            <label htmlFor="prompt-stack-instance-selector" className="space-y-1 text-sm">
              <span>Instance</span>
              <select
                id="prompt-stack-instance-selector"
                className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                value={selectedInstance}
                disabled={featureDisabled || loading || instances.length === 0}
                onChange={async (event) => {
                  const instanceID = event.target.value
                  setSelectedInstance(instanceID)
                  setSelectedAgent("")
                  setPreview(null)
                  setVersions([])
                  setDiff(null)
                  setDiffError("")
                  setLintIssues([])
                  setLintError("")
                  setTestResult(null)
                  setTestError("")
                  setLoading(true)
                  setLoadError("")
                  setSaveNotice("")
                  setSaveError("")
                  try {
                    const agentsPayload = instanceID
                      ? await api.get<{ agents?: unknown }>(`/api/admin/instances/${encodeURIComponent(instanceID)}/agents`)
                      : { agents: [] }
                    const ids = normalizeAgentIDs(agentsPayload.agents)
                    const nextAgent = ids[0] || ""
                    setAgents(ids)
                    setSelectedAgent(nextAgent)
                    if (instanceID && nextAgent) {
                      await loadAgentPromptData(instanceID, nextAgent)
                    }
                  } catch (error) {
                    setAgents([])
                    setSelectedAgent("")
                    setLoadError(`Failed to load prompt stack instance: ${extractErrorMessage(error)}`)
                  } finally {
                    setLoading(false)
                  }
                }}
              >
                {instances.map((instance) => (
                  <option key={instance.id} value={instance.id}>
                    {instance.name}
                  </option>
                ))}
              </select>
            </label>

            <label htmlFor="prompt-stack-agent-selector" className="space-y-1 text-sm">
              <span>Agent</span>
              <select
                id="prompt-stack-agent-selector"
                className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                value={selectedAgent}
                disabled={featureDisabled || loading || agents.length === 0}
                onChange={(event) => {
                  void handleAgentChange(event.target.value)
                }}
              >
                {agents.map((agentID) => (
                  <option key={agentID} value={agentID}>
                    {agentID}
                  </option>
                ))}
              </select>
            </label>

            <div className="space-y-1 text-sm">
              <span>Prompt lint</span>
              <Button
                type="button"
                variant="outline"
                disabled={featureDisabled || loading || lintLoading || !selectedAgent}
                data-testid="prompt-stack-run-lint"
                onClick={() => {
                  void handleLint()
                }}
              >
                {lintLoading ? "Running lint..." : "Run lint"}
              </Button>
            </div>

            <div className="space-y-1 text-sm">
              <span>Structural test</span>
              <Button
                type="button"
                variant="outline"
                disabled={featureDisabled || loading || testLoading || !selectedAgent}
                data-testid="prompt-stack-run-test"
                onClick={() => {
                  void handleRunTests()
                }}
              >
                {testLoading ? "Running tests..." : "Run tests"}
              </Button>
            </div>

            <div className="space-y-1 text-sm">
              <span>Model context window</span>
              <p className="h-10 rounded-md border bg-muted/30 px-3 py-2 text-sm">{contextWindow.toLocaleString()} tokens</p>
            </div>
          </div>
        </CardContent>
      </Card>

      {!featureDisabled && loadError ? (
        <div className="rounded-md border border-destructive/50 bg-destructive/5 p-3">
          <p className="text-sm text-destructive">{loadError}</p>
          <Button
            type="button"
            size="sm"
            className="mt-2"
            variant="outline"
            onClick={() => {
              void initialize()
            }}
          >
            Retry
          </Button>
        </div>
      ) : null}

      {!featureDisabled ? (
        <>
          <div className="grid gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Layer editors</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex flex-wrap gap-2">
              {LAYER_DEFINITIONS.map((layer) => (
                <Button
                  key={layer.id}
                  type="button"
                  size="sm"
                  variant={layer.id === activeLayerID ? "default" : "outline"}
                  data-testid={`prompt-stack-tab-${layer.id}`}
                  onClick={() => {
                    setActiveLayerID(layer.id)
                    setSaveNotice("")
                    setSaveError("")
                  }}
                >
                  {layer.label}
                </Button>
              ))}
            </div>

            <div>
              <h3 className="text-sm font-semibold">{activeLayer.label}</h3>
              <p className="text-xs text-muted-foreground">{activeLayer.description}</p>
            </div>

            <CodeEditor
              className="w-full"
              language="markdown"
              textareaTestId="prompt-stack-editor"
              highlightTestId="prompt-stack-editor-highlight"
              value={draftByLayerID[activeLayer.id] || ""}
              placeholder="Enter layer prompt instructions..."
              minHeight="260px"
              maxHeight="460px"
              onChange={(value) => {
                setDraftByLayerID((previous) => ({
                  ...previous,
                  [activeLayer.id]: value,
                }))
              }}
              onCopy={() => {
                toast({ title: "Layer copied", description: `${activeLayer.label} copied to clipboard.` })
              }}
            />

            <div className="flex items-center gap-2">
              <Button
                type="button"
                disabled={loading || savingLayer || !selectedAgent}
                data-testid="prompt-stack-save-layer"
                onClick={() => {
                  void handleSaveLayer()
                }}
              >
                {savingLayer ? "Saving..." : `Save ${activeLayer.label}`}
              </Button>
              <span className="text-xs text-muted-foreground">
                Version {layers.find((layer) => layer.layerID === activeLayer.id)?.version || 0}
              </span>
            </div>

            {saveNotice ? (
              <p data-testid="prompt-stack-save-notice" className="text-sm text-emerald-600">
                {saveNotice}
              </p>
            ) : null}
            {saveError ? <p className="text-sm text-destructive">{saveError}</p> : null}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Merged preview</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div>
              <div className="mb-2 flex items-center justify-between text-sm">
                <span data-testid="prompt-stack-total-tokens" className="font-medium">
                  Total tokens: {totalTokens.toLocaleString()} / {contextWindow.toLocaleString()}
                </span>
                <Badge variant="secondary">{preview?.estimationMethod || "word_count_x1.3"}</Badge>
              </div>
              <div className="h-2 w-full overflow-hidden rounded bg-muted">
                <div
                  className={isOverflow ? "h-2 rounded bg-red-500" : "h-2 rounded bg-emerald-500"}
                  style={{ width: `${tokenUsagePercent}%` }}
                />
              </div>
            </div>

            {isOverflow ? (
              <div
                data-testid="prompt-stack-overflow-warning"
                className="rounded-md border border-red-500/50 bg-red-500/10 p-3 text-sm"
              >
                <p className="font-medium text-red-700">Token budget overflow detected.</p>
                {topContributors.length > 0 ? (
                  <ul className="mt-2 list-disc space-y-1 pl-5 text-red-700/90">
                    {topContributors.map((layer) => (
                      <li key={layer.layerID}>
                        {findLayerLabel(layer.layerID)} contributes {layer.tokenCount.toLocaleString()} tokens.
                      </li>
                    ))}
                  </ul>
                ) : null}
              </div>
            ) : null}

            <div className="space-y-1 text-sm">
              {preview?.layers.map((layer) => (
                <div key={layer.layerID} className="flex items-center justify-between rounded-md border px-2 py-1">
                  <span>{findLayerLabel(layer.layerID)}</span>
                  <span className="text-muted-foreground">
                    {layer.tokenCount.toLocaleString()} tokens / {layer.wordCount.toLocaleString()} words
                  </span>
                </div>
              ))}
            </div>

            <div data-testid="prompt-stack-preview">
              <CodeEditor
                language="markdown"
                readOnly
                highlightTestId="prompt-stack-preview-highlight"
                value={preview?.assembledPrompt || ""}
                minHeight="260px"
                maxHeight="460px"
              />
            </div>
          </CardContent>
        </Card>
          </div>

          <div className="grid gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Version history & rollback</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <ul data-testid="prompt-stack-version-list" className="max-h-56 space-y-2 overflow-auto rounded-md border p-2">
              {versions.length === 0 ? (
                <li className="text-sm text-muted-foreground">No prompt stack history yet.</li>
              ) : (
                versions.map((version) => (
                  <li key={version.version} className="rounded border p-2 text-sm">
                    <div className="flex items-center justify-between gap-2">
                      <button
                        type="button"
                        className="font-medium underline-offset-2 hover:underline"
                        onClick={() => {
                          setSelectedRollbackVersion(String(version.version))
                        }}
                      >
                        Version {version.version}
                      </button>
                      <Badge variant="outline">{findLayerLabel(version.changedLayer)}</Badge>
                    </div>
                    <p className="mt-1 text-xs text-muted-foreground">Updated {formatTimestamp(version.updatedAt)}</p>
                    <p className="text-xs text-muted-foreground">Layer version {version.layerVersion}</p>
                  </li>
                ))
              )}
            </ul>

            <div className="grid gap-3 md:grid-cols-2">
              <label htmlFor="prompt-stack-diff-from" className="space-y-1 text-sm">
                <span>Diff from</span>
                <select
                  id="prompt-stack-diff-from"
                  className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                  value={selectedFromVersion}
                  onChange={(event) => setSelectedFromVersion(event.target.value)}
                >
                  <option value="">Select version</option>
                  {versions.map((version) => (
                    <option key={`from-${version.version}`} value={String(version.version)}>
                      Version {version.version}
                    </option>
                  ))}
                </select>
              </label>

              <label htmlFor="prompt-stack-diff-to" className="space-y-1 text-sm">
                <span>Diff to</span>
                <select
                  id="prompt-stack-diff-to"
                  className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                  value={selectedToVersion}
                  onChange={(event) => setSelectedToVersion(event.target.value)}
                >
                  <option value="">Select version</option>
                  {versions.map((version) => (
                    <option key={`to-${version.version}`} value={String(version.version)}>
                      Version {version.version}
                    </option>
                  ))}
                </select>
              </label>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <Button
                type="button"
                variant="outline"
                disabled={diffLoading || !selectedAgent}
                data-testid="prompt-stack-load-diff"
                onClick={() => {
                  void handleLoadDiff()
                }}
              >
                {diffLoading ? "Loading diff..." : "Load diff"}
              </Button>

              <label htmlFor="prompt-stack-rollback-version" className="text-sm">
                <span className="mr-2">Rollback version</span>
                <select
                  id="prompt-stack-rollback-version"
                  className="h-9 rounded-md border bg-background px-3 text-sm"
                  value={selectedRollbackVersion}
                  onChange={(event) => setSelectedRollbackVersion(event.target.value)}
                >
                  <option value="">Select version</option>
                  {versions.map((version) => (
                    <option key={`rollback-${version.version}`} value={String(version.version)}>
                      Version {version.version}
                    </option>
                  ))}
                </select>
              </label>

              <Button
                type="button"
                variant="destructive"
                disabled={loading || !selectedAgent}
                data-testid="prompt-stack-rollback"
                onClick={() => {
                  void handleRollback()
                }}
              >
                Rollback
              </Button>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Visual diff</CardTitle>
          </CardHeader>
          <CardContent>
            {diffError ? <p className="mb-2 text-sm text-destructive">{diffError}</p> : null}

            <div
              data-testid="prompt-stack-diff"
              className="max-h-80 overflow-auto rounded-md border bg-muted/20 p-2 font-mono text-xs"
            >
              {!diff || diff.lines.length === 0 ? (
                <p className="text-muted-foreground">Select two versions and click "Load diff".</p>
              ) : (
                <div className="space-y-0.5">
                  {diff.lines.map((line, index) => (
                    <div key={`${line.type}-${index}`} className={`rounded px-2 py-0.5 ${diffLineClass(line.type)}`}>
                      <span className="select-none">{diffPrefix(line.type)} </span>
                      <span>{line.content || " "}</span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </CardContent>
        </Card>
          </div>

          <div className="grid gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Lint issues</CardTitle>
          </CardHeader>
          <CardContent data-testid="prompt-stack-lint-results" className="space-y-2">
            {lintError ? <p className="text-sm text-destructive">{lintError}</p> : null}
            {!lintError && lintIssues.length === 0 ? (
              <p className="text-sm text-muted-foreground">Run lint to review prompt quality issues.</p>
            ) : null}

            {lintIssues.map((issue, index) => (
              <div key={`${issue.layerID}-${index}`} className="rounded-md border p-3 text-sm">
                <div className="mb-1 flex items-center gap-2">
                  <Badge className={severityBadgeClass(issue.severity)}>{issue.severity || "info"}</Badge>
                  <Badge variant="outline">{issue.layerID || "all_layers"}</Badge>
                </div>
                <p className="font-medium">{issue.description}</p>
                {issue.suggestedFix ? (
                  <p className="mt-1 text-xs text-muted-foreground">Suggested fix: {issue.suggestedFix}</p>
                ) : null}
              </div>
            ))}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Structural test results</CardTitle>
          </CardHeader>
          <CardContent data-testid="prompt-stack-test-results" className="space-y-2">
            {testError ? <p className="text-sm text-destructive">{testError}</p> : null}
            {!testError && !testResult ? (
              <p className="text-sm text-muted-foreground">Run tests to validate prompt-stack structure.</p>
            ) : null}

            {testResult ? (
              <div className="space-y-2">
                <Badge className={testResult.passed ? "bg-emerald-600 text-white" : "bg-destructive text-destructive-foreground"}>
                  {testResult.passed ? "PASS" : "FAIL"}
                </Badge>
                {testResult.checks.map((check) => (
                  <div key={check.name} className="rounded-md border p-3 text-sm">
                    <div className="mb-1 flex items-center gap-2">
                      <Badge className={check.passed ? "bg-emerald-600 text-white" : "bg-destructive text-destructive-foreground"}>
                        {check.passed ? "PASS" : "FAIL"}
                      </Badge>
                      <span className="font-medium">{check.name}</span>
                    </div>
                    <p className="text-xs text-muted-foreground">{check.explanation}</p>
                  </div>
                ))}
              </div>
            ) : null}
          </CardContent>
        </Card>
          </div>
        </>
      ) : null}
    </div>
  )
}
