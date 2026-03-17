import { useCallback, useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ApiError, api } from "@/lib/api"

type SourceType = "global" | "agent-profile" | "subagent-override"

type ContractFieldRow = {
  path: string
  value: unknown
  source: SourceType
}

type DiffRow = {
  field: string
  targetValue: unknown
  baseValue: unknown
  targetSource: SourceType
  baseSource: SourceType
}

type Snapshot = {
  id: string
  createdAt: string
}

type InstanceSummary = {
  id: string
  name: string
}

const CONTRACT_SECTIONS = [
  { key: "identity", label: "Identity" },
  { key: "mission", label: "Mission" },
  { key: "system_prompt", label: "System Prompt" },
  { key: "tool_policy", label: "Tool Policy" },
  { key: "delegation_policy", label: "Delegation Policy" },
  { key: "memory_policy", label: "Memory Policy" },
  { key: "model_policy", label: "Model Policy" },
  { key: "sandbox_policy", label: "Sandbox Policy" },
  { key: "observability_policy", label: "Observability Policy" },
] as const

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

function normalizeSource(value: unknown): SourceType {
  const text = asText(value).trim().toLowerCase()
  if (text === "agent-profile") {
    return "agent-profile"
  }
  if (text === "subagent-override") {
    return "subagent-override"
  }
  return "global"
}

function sourceLabel(source: SourceType): string {
  if (source === "agent-profile") {
    return "Agent Profile"
  }
  if (source === "subagent-override") {
    return "Subagent Override"
  }
  return "Global"
}

function sourceBadgeClass(source: SourceType): string {
  if (source === "agent-profile") {
    return "bg-blue-600 text-white border-transparent"
  }
  if (source === "subagent-override") {
    return "bg-amber-500 text-black border-transparent"
  }
  return "bg-muted text-muted-foreground"
}

function formatValue(value: unknown): string {
  if (typeof value === "string") {
    return value || "(empty)"
  }
  if (value === undefined) {
    return "undefined"
  }
  if (value === null) {
    return "null"
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value)
  }
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

function flattenSectionFields(prefix: string, value: unknown): Array<{ path: string; value: unknown }> {
  if (Array.isArray(value)) {
    return [{ path: prefix, value }]
  }

  const record = asRecord(value)
  if (!record) {
    return [{ path: prefix, value }]
  }

  const rows: Array<{ path: string; value: unknown }> = []
  for (const key of Object.keys(record).sort()) {
    const nextPath = `${prefix}.${key}`
    rows.push(...flattenSectionFields(nextPath, record[key]))
  }
  return rows
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
    const nested = asRecord(details?.error)
    const nestedMessage = asText(nested?.message).trim()
    if (nestedMessage) {
      return nestedMessage
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
  const out: string[] = []
  const seen = new Set<string>()
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
  const out: InstanceSummary[] = []
  const seen = new Set<string>()
  for (const item of payload) {
    const record = asRecord(item)
    const id = asText(record?.id).trim()
    if (!id || seen.has(id)) {
      continue
    }
    seen.add(id)
    out.push({
      id,
      name: asText(record?.name).trim() || id,
    })
  }
  return out
}

function buildInstanceContractRoute(instanceID: string, agentID: string, action: string, query = ""): string {
  const base = `/api/admin/instances/${encodeURIComponent(instanceID)}/agents/${encodeURIComponent(agentID)}/${action}`
  return query ? `${base}?${query}` : base
}

export function AgentContractPage() {
  const [instances, setInstances] = useState<InstanceSummary[]>([])
  const [selectedInstance, setSelectedInstance] = useState("")
  const [agents, setAgents] = useState<string[]>([])
  const [selectedAgent, setSelectedAgent] = useState("")
  const [viewMode, setViewMode] = useState<"resolved" | "raw">("resolved")
  const [activePanel, setActivePanel] = useState<"contract" | "diff">("contract")
  const [resolvedContract, setResolvedContract] = useState<Record<string, unknown> | null>(null)
  const [sourceMap, setSourceMap] = useState<Record<string, SourceType>>({})

  const [loadingAgents, setLoadingAgents] = useState(true)
  const [loadingResolved, setLoadingResolved] = useState(false)
  const [errorMessage, setErrorMessage] = useState("")

  const [diffBase, setDiffBase] = useState("global")
  const [loadingDiff, setLoadingDiff] = useState(false)
  const [diffError, setDiffError] = useState("")
  const [diffRows, setDiffRows] = useState<DiffRow[]>([])

  const [snapshots, setSnapshots] = useState<Snapshot[]>([])
  const [snapshotMessage, setSnapshotMessage] = useState("")
  const [rollbackError, setRollbackError] = useState("")
  const [rollingBackSnapshotID, setRollingBackSnapshotID] = useState("")

  const loadSelectors = useCallback(async () => {
    setLoadingAgents(true)
    setErrorMessage("")
    try {
      const [instancesPayload, activePayload] = await Promise.all([
        api.get<{ instances?: unknown }>("/api/admin/instances"),
        api.get<{ instance?: unknown }>("/api/admin/instances/active"),
      ])

      const nextInstances = normalizeInstances(instancesPayload.instances)
      const activeInstanceRecord = asRecord(activePayload.instance)
      const activeInstanceID = asText(activeInstanceRecord?.id).trim()
      const nextSelectedInstance =
        (activeInstanceID && nextInstances.some((instance) => instance.id === activeInstanceID) ? activeInstanceID : "") ||
        nextInstances[0]?.id ||
        ""

      setInstances(nextInstances)
      setSelectedInstance(nextSelectedInstance)

      const agentPayload = nextSelectedInstance
        ? await api.get<{ agents?: unknown }>(`/api/admin/instances/${encodeURIComponent(nextSelectedInstance)}/agents`)
        : { agents: [] }

      const agentIDs = normalizeAgentIDs(agentPayload.agents)
      setAgents(agentIDs)

      const nextSelected = agentIDs[0] || ""
      setSelectedAgent(nextSelected)
      setDiffBase("global")
    } catch (error) {
      setInstances([])
      setSelectedInstance("")
      setAgents([])
      setSelectedAgent("")
      setErrorMessage(`Failed to load contract controls: ${extractErrorMessage(error)}`)
    } finally {
      setLoadingAgents(false)
    }
  }, [])

  const loadResolvedContract = useCallback(async (instanceID: string, agentID: string) => {
    if (!instanceID || !agentID) {
      setResolvedContract(null)
      setSourceMap({})
      return
    }
    setLoadingResolved(true)
    setErrorMessage("")

    try {
      const payload = await api.get<unknown>(buildInstanceContractRoute(instanceID, agentID, "resolved"))
      const contract = asRecord(payload)
      if (!contract) {
        throw new Error("Resolved contract response was not an object")
      }

      const inheritance = asRecord(contract.inheritance)
      const sourceRaw = asRecord(inheritance?.source)
      const nextSourceMap: Record<string, SourceType> = {}
      if (sourceRaw) {
        for (const [field, source] of Object.entries(sourceRaw)) {
          nextSourceMap[field] = normalizeSource(source)
        }
      }

      setResolvedContract(contract)
      setSourceMap(nextSourceMap)
    } catch (error) {
      setResolvedContract(null)
      setSourceMap({})
      setErrorMessage(`Failed to load resolved contract: ${extractErrorMessage(error)}`)
    } finally {
      setLoadingResolved(false)
    }
  }, [])

  const loadDiff = useCallback(async (instanceID: string, agentID: string, base: string) => {
    if (!instanceID || !agentID) {
      setDiffRows([])
      setDiffError("")
      return
    }

    setLoadingDiff(true)
    setDiffError("")
    try {
      const query = new URLSearchParams({ base }).toString()
      const payload = await api.get<unknown>(
        buildInstanceContractRoute(instanceID, agentID, "diff", query)
      )
      const record = asRecord(payload)
      const rowsRaw = Array.isArray(record?.differences) ? record.differences : []

      const rows: DiffRow[] = rowsRaw
        .map((entry) => {
          const item = asRecord(entry)
          if (!item) {
            return null
          }
          const field = asText(item.field).trim()
          if (!field) {
            return null
          }
          return {
            field,
            targetValue: item.target_value,
            baseValue: item.base_value,
            targetSource: normalizeSource(item.target_source),
            baseSource: normalizeSource(item.base_source),
          }
        })
        .filter((row): row is DiffRow => row !== null)

      setDiffRows(rows)
    } catch (error) {
      setDiffRows([])
      setDiffError(`Failed to load diff: ${extractErrorMessage(error)}`)
    } finally {
      setLoadingDiff(false)
    }
  }, [])

  useEffect(() => {
    void loadSelectors()
  }, [loadSelectors])

  useEffect(() => {
    if (!selectedInstance || !selectedAgent) {
      return
    }
    void loadResolvedContract(selectedInstance, selectedAgent)
  }, [loadResolvedContract, selectedAgent, selectedInstance])

  useEffect(() => {
    if (!selectedInstance || !selectedAgent) {
      return
    }
    if (activePanel !== "diff") {
      return
    }
    void loadDiff(selectedInstance, selectedAgent, diffBase)
  }, [activePanel, diffBase, loadDiff, selectedAgent, selectedInstance])

  const sectionRows = useMemo(() => {
    if (!resolvedContract) {
      return [] as Array<{ sectionKey: string; sectionLabel: string; rows: ContractFieldRow[] }>
    }

    return CONTRACT_SECTIONS.map((section) => {
      const rawValue = resolvedContract[section.key]
      const flattened = flattenSectionFields(section.key, rawValue)
      const rows: ContractFieldRow[] = flattened
        .map((entry) => {
          const source = sourceMap[entry.path] || sourceMap[section.key] || "global"
          return {
            path: entry.path,
            value: entry.value,
            source,
          }
        })
        .filter((entry) => (viewMode === "raw" ? entry.source !== "global" : true))

      return {
        sectionKey: section.key,
        sectionLabel: section.label,
        rows,
      }
    }).filter((section) => section.rows.length > 0)
  }, [resolvedContract, sourceMap, viewMode])

  const saveSnapshot = useCallback(async () => {
    setSnapshotMessage("")
    setRollbackError("")
    if (!selectedInstance || !selectedAgent) {
      setRollbackError("Select an instance and agent before saving a rollback snapshot.")
      return
    }
    try {
      const payload = await api.post<{
        snapshot?: { id?: unknown; created_at?: unknown }
      }>(buildInstanceContractRoute(selectedInstance, selectedAgent, "rollback-snapshot"), {})
      const snapshotPayload = asRecord(payload.snapshot)
      const id = asText(snapshotPayload?.id).trim()
      const createdAt = asText(snapshotPayload?.created_at).trim()
      if (!id || !createdAt) {
        throw new Error("Snapshot response was missing snapshot metadata")
      }
      const snapshot: Snapshot = {
        id,
        createdAt,
      }
      setSnapshots((previous) => [snapshot, ...previous].slice(0, 10))
      setSnapshotMessage("Snapshot saved.")
    } catch (error) {
      setRollbackError(`Failed to save snapshot: ${extractErrorMessage(error)}`)
    }
  }, [selectedAgent, selectedInstance])

  const restoreSnapshot = useCallback(async (snapshot: Snapshot) => {
    setSnapshotMessage("")
    setRollbackError("")
    if (!selectedInstance || !selectedAgent) {
      setRollbackError("Select an instance and agent before restoring a rollback snapshot.")
      return
    }
    setRollingBackSnapshotID(snapshot.id)
    try {
      await api.post(buildInstanceContractRoute(selectedInstance, selectedAgent, "rollback-restore"), {
        snapshot_id: snapshot.id,
      })
      setSnapshotMessage("Rollback restored successfully.")
      if (selectedInstance && selectedAgent) {
        await loadResolvedContract(selectedInstance, selectedAgent)
      }
      if (selectedInstance && selectedAgent && activePanel === "diff") {
        await loadDiff(selectedInstance, selectedAgent, diffBase)
      }
    } catch (error) {
      setRollbackError(`Failed to restore snapshot: ${extractErrorMessage(error)}`)
    } finally {
      setRollingBackSnapshotID("")
    }
  }, [activePanel, diffBase, loadDiff, loadResolvedContract, selectedAgent, selectedInstance])

  return (
    <div className="space-y-4 p-6" data-testid="agent-contract-page">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Agent Contract</h2>
        <p className="text-sm text-muted-foreground">
          Inspect resolved policy contracts, inheritance sources, raw/resolved values, and field-level diffs.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Contract controls</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            <label htmlFor="agent-contract-instance-selector" className="space-y-1 text-sm">
              <span>Instance</span>
              <select
                id="agent-contract-instance-selector"
                className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                value={selectedInstance}
                disabled={loadingAgents || instances.length === 0}
                onChange={async (event) => {
                  const nextInstance = event.target.value
                  setSelectedInstance(nextInstance)
                  setSelectedAgent("")
                  setDiffBase("global")
                  setSnapshots([])
                  setSnapshotMessage("")
                  setRollbackError("")
                  setResolvedContract(null)
                  setDiffRows([])
                  setDiffError("")
                  setLoadingAgents(true)
                  try {
                    const agentPayload = nextInstance
                      ? await api.get<{ agents?: unknown }>(`/api/admin/instances/${encodeURIComponent(nextInstance)}/agents`)
                      : { agents: [] }
                    const agentIDs = normalizeAgentIDs(agentPayload.agents)
                    setAgents(agentIDs)
                    setSelectedAgent(agentIDs[0] || "")
                  } catch (error) {
                    setAgents([])
                    setSelectedAgent("")
                    setErrorMessage(`Failed to load instance agents: ${extractErrorMessage(error)}`)
                  } finally {
                    setLoadingAgents(false)
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

            <label htmlFor="agent-contract-agent-selector" className="space-y-1 text-sm">
              <span>Agent</span>
              <select
                id="agent-contract-agent-selector"
                className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                value={selectedAgent}
                disabled={loadingAgents || agents.length === 0}
                onChange={(event) => {
                  setSelectedAgent(event.target.value)
                  setDiffBase("global")
                  setSnapshots([])
                  setSnapshotMessage("")
                  setRollbackError("")
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
              <span>Panel</span>
              <div className="flex gap-2">
                <Button
                  type="button"
                  size="sm"
                  variant={activePanel === "contract" ? "default" : "outline"}
                  onClick={() => setActivePanel("contract")}
                >
                  Contract
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant={activePanel === "diff" ? "default" : "outline"}
                  onClick={() => setActivePanel("diff")}
                >
                  Diff
                </Button>
              </div>
            </div>

            <div className="space-y-1 text-sm">
              <span>View mode</span>
              <div className="flex gap-2">
                <Button
                  type="button"
                  size="sm"
                  variant={viewMode === "resolved" ? "default" : "outline"}
                  onClick={() => setViewMode("resolved")}
                >
                  Resolved
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant={viewMode === "raw" ? "default" : "outline"}
                  onClick={() => setViewMode("raw")}
                >
                  Raw
                </Button>
              </div>
            </div>

            <div className="space-y-1 text-sm">
              <span>Rollback</span>
               <Button
                 type="button"
                 size="sm"
                 variant="outline"
                 disabled={!selectedInstance || !selectedAgent}
                 onClick={() => void saveSnapshot()}
               >
                 Save rollback snapshot
              </Button>
            </div>
          </div>

          {loadingAgents ? <p className="text-sm text-muted-foreground">Loading instance and agents...</p> : null}
          {!loadingAgents && instances.length === 0 ? (
            <p className="text-sm text-muted-foreground">No configured instances found.</p>
          ) : null}
          {!loadingAgents && instances.length > 0 && agents.length === 0 ? (
            <p className="text-sm text-muted-foreground">No configured agents found in this instance.</p>
          ) : null}

          {snapshotMessage ? <p className="text-sm text-green-600">{snapshotMessage}</p> : null}
          {rollbackError ? <p className="text-sm text-destructive">{rollbackError}</p> : null}

          {snapshots.length > 0 ? (
            <div className="rounded-md border p-3">
              <h3 className="text-sm font-semibold">Saved snapshots</h3>
              <ul className="mt-2 space-y-2">
                {snapshots.map((snapshot) => (
                  <li key={snapshot.id} className="flex items-center justify-between gap-2 text-sm">
                    <span>{new Date(snapshot.createdAt).toLocaleString()}</span>
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      disabled={rollingBackSnapshotID === snapshot.id}
                      onClick={() => void restoreSnapshot(snapshot)}
                    >
                      {rollingBackSnapshotID === snapshot.id ? "Restoring..." : "Restore"}
                    </Button>
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
        </CardContent>
      </Card>

      {errorMessage ? (
        <div className="space-y-2 rounded-md border border-destructive/50 bg-destructive/5 p-3">
          <p className="text-sm text-destructive">{errorMessage}</p>
          <Button type="button" size="sm" variant="outline" onClick={() => void loadSelectors()}>
            Retry
          </Button>
        </div>
      ) : null}

      {activePanel === "contract" ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Resolved contract</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {loadingResolved ? <p className="text-sm text-muted-foreground">Loading resolved contract...</p> : null}
            {!loadingResolved && sectionRows.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                {viewMode === "raw"
                  ? "No explicit fields for this agent in raw view."
                  : "No contract fields available for this agent."}
              </p>
            ) : null}

            {!loadingResolved
              ? sectionRows.map((section) => (
                  <section key={section.sectionKey} className="space-y-2">
                    <h4 className="text-sm font-semibold">{section.sectionLabel}</h4>
                    <div className="rounded-md border">
                      <table className="w-full text-sm">
                        <thead>
                          <tr className="border-b bg-muted/30 text-left">
                            <th className="px-3 py-2 font-medium">Field</th>
                            <th className="px-3 py-2 font-medium">Value</th>
                            <th className="px-3 py-2 font-medium">Source</th>
                          </tr>
                        </thead>
                        <tbody>
                          {section.rows.map((row) => (
                            <tr key={row.path} data-testid={`contract-field-${row.path}`} className="border-b last:border-b-0">
                              <td className="px-3 py-2 align-top">
                                <code>{row.path}</code>
                              </td>
                              <td className="px-3 py-2 align-top">
                                <code className="break-all">{formatValue(row.value)}</code>
                              </td>
                              <td className="px-3 py-2 align-top">
                                <Badge className={sourceBadgeClass(row.source)}>{sourceLabel(row.source)}</Badge>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  </section>
                ))
              : null}
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Diff view</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
              <label htmlFor="agent-contract-diff-base" className="space-y-1 text-sm">
                <span>Compare against</span>
                <select
                  id="agent-contract-diff-base"
                  className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                  value={diffBase}
                  onChange={(event) => setDiffBase(event.target.value)}
                >
                  <option value="global">Global defaults</option>
                  {agents
                    .filter((agentID) => agentID !== selectedAgent)
                    .map((agentID) => (
                      <option key={agentID} value={agentID}>
                        {agentID}
                      </option>
                    ))}
                </select>
              </label>
            </div>

            {loadingDiff ? <p className="text-sm text-muted-foreground">Loading diff...</p> : null}
            {!loadingDiff && diffError ? <p className="text-sm text-destructive">{diffError}</p> : null}
            {!loadingDiff && !diffError && diffRows.length === 0 ? (
              <p className="text-sm text-muted-foreground">No differences found for this comparison.</p>
            ) : null}

            {!loadingDiff && !diffError && diffRows.length > 0 ? (
              <div className="rounded-md border">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b bg-muted/30 text-left">
                      <th className="px-3 py-2 font-medium">Field</th>
                      <th className="px-3 py-2 font-medium">Current</th>
                      <th className="px-3 py-2 font-medium">Baseline</th>
                    </tr>
                  </thead>
                  <tbody>
                    {diffRows.map((row) => (
                      <tr key={row.field} data-testid={`contract-diff-row-${row.field}`} className="border-b last:border-b-0">
                        <td className="px-3 py-2 align-top">
                          <code>{row.field}</code>
                        </td>
                        <td className="px-3 py-2 align-top">
                          <div className="space-y-1">
                            <code className="block break-all">{formatValue(row.targetValue)}</code>
                            <Badge className={sourceBadgeClass(row.targetSource)}>{sourceLabel(row.targetSource)}</Badge>
                          </div>
                        </td>
                        <td className="px-3 py-2 align-top">
                          <div className="space-y-1">
                            <code className="block break-all">{formatValue(row.baseValue)}</code>
                            <Badge className={sourceBadgeClass(row.baseSource)}>{sourceLabel(row.baseSource)}</Badge>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : null}
          </CardContent>
        </Card>
      )}
    </div>
  )
}
