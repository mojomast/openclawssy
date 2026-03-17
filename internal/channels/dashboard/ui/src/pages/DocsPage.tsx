import { ChangeEvent, useCallback, useEffect, useMemo, useState } from "react"
import { Button } from "@/components/ui/button"
import { useControlPlaneFeatures } from "@/hooks/useControlPlaneFeatures"
import { api } from "@/lib/api"

const DOC_ORDER = ["SOUL.md", "RULES.md", "TOOLS.md", "SPECPLAN.md", "DEVPLAN.md", "HANDOFF.md", "HEARTBEAT.md"]

type AgentDoc = {
  name: string
  resolvedName: string
  aliasFor: string
  content: string
  exists: boolean
}

type AgentDocsResponse = {
  agent_id?: unknown
  available_agents?: unknown
  documents?: unknown
}

function asText(value: unknown): string {
  if (value === null || value === undefined) {
    return ""
  }
  return String(value)
}

function normalizeDocuments(input: unknown): AgentDoc[] {
  const list = Array.isArray(input) ? input : []
  const normalized = list
    .filter((item): item is Record<string, unknown> => Boolean(item) && typeof item === "object")
    .map((item) => {
      const name = asText(item.name).trim()
      const resolvedName = asText(item.resolved_name).trim() || name
      return {
        name,
        resolvedName,
        aliasFor: asText(item.alias_for).trim(),
        content: asText(item.content),
        exists: Boolean(item.exists),
      }
    })
    .filter((item) => item.name.length > 0)

  const rank = DOC_ORDER.reduce<Record<string, number>>((acc, item, index) => {
    acc[item] = index
    return acc
  }, {})

  normalized.sort((left, right) => {
    const leftRank = Number.isInteger(rank[left.name]) ? rank[left.name] : 100
    const rightRank = Number.isInteger(rank[right.name]) ? rank[right.name] : 100
    if (leftRank !== rightRank) {
      return leftRank - rightRank
    }
    return left.name.localeCompare(right.name)
  })

  return normalized
}

function normalizeAgents(input: unknown): string[] {
  if (!Array.isArray(input)) {
    return []
  }
  return input
    .map((item) => asText(item).trim())
    .filter((item) => item.length > 0)
}

export function DocsPage() {
  const { features, loading: featuresLoading } = useControlPlaneFeatures()
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [agentID, setAgentID] = useState("default")
  const [availableAgents, setAvailableAgents] = useState<string[]>(["default"])
  const [documents, setDocuments] = useState<AgentDoc[]>([])
  const [selectedDocName, setSelectedDocName] = useState("")
  const [baselineByName, setBaselineByName] = useState<Record<string, string>>({})
  const [draftByName, setDraftByName] = useState<Record<string, string>>({})
  const [statusText, setStatusText] = useState("")
  const [statusKind, setStatusKind] = useState<"" | "success" | "error">("")
  const featureDisabled = !featuresLoading && !features.instanceAgents

  const hasUnsavedChanges = useCallback((docName: string) => {
    return asText(draftByName[docName]) !== asText(baselineByName[docName])
  }, [baselineByName, draftByName])

  const selectedDoc = useMemo(() => {
    return documents.find((item) => item.name === selectedDocName) || null
  }, [documents, selectedDocName])

  const loadDocs = useCallback(async (requestedAgent: string, options?: { keepStatus?: boolean }) => {
    if (featureDisabled) {
      setLoading(false)
      return
    }
    const keepStatus = Boolean(options?.keepStatus)
    const targetAgent = asText(requestedAgent).trim() || "default"

    setLoading(true)
    if (!keepStatus) {
      setStatusText("Loading docs...")
      setStatusKind("")
    }

    try {
      const payload = await api.get<AgentDocsResponse>(`/api/admin/agent/docs?agent_id=${encodeURIComponent(targetAgent)}`)
      const nextAgent = asText(payload.agent_id).trim() || targetAgent
      const nextAvailableAgents = normalizeAgents(payload.available_agents)
      const nextDocs = normalizeDocuments(payload.documents)

      const nextBaselineByName: Record<string, string> = {}
      const nextDraftByName: Record<string, string> = {}
      for (const doc of nextDocs) {
        nextBaselineByName[doc.name] = doc.content
        nextDraftByName[doc.name] = doc.content
      }

      setAvailableAgents(nextAvailableAgents.length ? nextAvailableAgents : [nextAgent])
      setAgentID(nextAgent)
      setDocuments(nextDocs)
      setBaselineByName(nextBaselineByName)
      setDraftByName(nextDraftByName)
      setSelectedDocName((previous) => {
        if (nextDocs.some((doc) => doc.name === previous)) {
          return previous
        }
        return nextDocs[0]?.name || ""
      })
      setStatusText("Docs loaded.")
      setStatusKind("success")
    } catch (error) {
      setStatusText(`Failed to load docs: ${error instanceof Error ? error.message : String(error)}`)
      setStatusKind("error")
    } finally {
      setLoading(false)
    }
  }, [featureDisabled])

  useEffect(() => {
    if (featuresLoading) {
      return
    }
    if (featureDisabled) {
      setLoading(false)
      setSaving(false)
      setAgentID("default")
      setAvailableAgents(["default"])
      setDocuments([])
      setSelectedDocName("")
      setBaselineByName({})
      setDraftByName({})
      setStatusText("")
      setStatusKind("")
      return
    }
    void loadDocs("default")
  }, [featureDisabled, featuresLoading, loadDocs])

  const handleAgentChange = useCallback((event: ChangeEvent<HTMLSelectElement>) => {
    if (featureDisabled) {
      return
    }
    const nextAgent = event.target.value
    setAgentID(nextAgent)
    void loadDocs(nextAgent)
  }, [featureDisabled, loadDocs])

  const handleDocumentChange = useCallback((event: ChangeEvent<HTMLSelectElement>) => {
    setSelectedDocName(event.target.value)
    setStatusText("")
    setStatusKind("")
  }, [])

  const handleReload = useCallback(() => {
    if (featureDisabled) {
      return
    }
    void loadDocs(agentID)
  }, [agentID, featureDisabled, loadDocs])

  const handleSave = useCallback(async () => {
    if (featureDisabled) {
      return
    }
    if (!selectedDoc) {
      setStatusText("Select a document before saving.")
      setStatusKind("error")
      return
    }

    const content = asText(draftByName[selectedDoc.name])
    setSaving(true)
    setStatusText(`Saving ${selectedDoc.name}...`)
    setStatusKind("")

    try {
      await api.post("/api/admin/agent/docs", {
        agent_id: agentID,
        name: selectedDoc.name,
        content,
      })
      setBaselineByName((previous) => ({
        ...previous,
        [selectedDoc.name]: content,
      }))
      setStatusText(`Saved ${selectedDoc.name} for ${agentID}.`)
      setStatusKind("success")
    } catch (error) {
      setStatusText(`Failed to save ${selectedDoc.name}: ${error instanceof Error ? error.message : String(error)}`)
      setStatusKind("error")
    } finally {
      setSaving(false)
    }
  }, [agentID, draftByName, featureDisabled, selectedDoc])

  const canSave = Boolean(selectedDoc && hasUnsavedChanges(selectedDoc.name))

  return (
    <div className="space-y-4 p-6" data-testid="docs-page">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Docs</h2>
        <p className="text-sm text-muted-foreground">
          Edit core agent markdown docs per agent and save updates through admin APIs.
        </p>
      </div>

      {featureDisabled ? (
        <div className="rounded-lg border bg-card p-4">
          <div className="rounded-md border border-border bg-muted/30 p-4" data-testid="docs-disabled-state">
            <p className="text-sm font-medium">Docs disabled</p>
            <p className="mt-1 text-sm text-muted-foreground">
              Instance agent controls are disabled for this control plane.
            </p>
          </div>
        </div>
      ) : null}

      <section className="space-y-4 rounded-lg border bg-card p-4">
        <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] md:items-end">
          <label htmlFor="docs-agent" className="space-y-1 text-sm">
            <span>Agent</span>
            <select
              id="docs-agent"
              className="h-10 w-full rounded-md border bg-background px-3 text-sm"
              value={agentID}
              disabled={featureDisabled || loading || saving}
              onChange={handleAgentChange}
            >
              {(availableAgents.length ? availableAgents : [agentID]).map((item) => (
                <option key={item} value={item}>
                  {item}
                </option>
              ))}
            </select>
          </label>

          <label htmlFor="docs-document" className="space-y-1 text-sm">
            <span>Document</span>
            <select
              id="docs-document"
              className="h-10 w-full rounded-md border bg-background px-3 text-sm"
              value={selectedDocName}
              disabled={featureDisabled || loading || saving || documents.length === 0}
              onChange={handleDocumentChange}
            >
              {documents.map((doc) => {
                const unsaved = hasUnsavedChanges(doc.name) ? " *" : ""
                return (
                  <option key={doc.name} value={doc.name}>
                    {doc.name}{unsaved}
                  </option>
                )
              })}
            </select>
          </label>

          <div className="flex flex-wrap gap-2">
            <Button type="button" variant="outline" disabled={featureDisabled || loading || saving} onClick={handleReload}>
              {loading ? "Reloading..." : "Reload docs"}
            </Button>
            <Button type="button" disabled={featureDisabled || loading || saving || !selectedDoc || !canSave} onClick={handleSave}>
              {saving ? "Saving..." : "Save selected doc"}
            </Button>
          </div>
        </div>

        {statusText && (
          <p
            className={[
              "text-sm",
              statusKind === "error" ? "text-destructive" : "",
              statusKind === "success" ? "text-emerald-600 dark:text-emerald-400" : "",
            ].join(" ").trim()}
          >
            {statusText}
          </p>
        )}

        {!selectedDoc ? (
          <p className="text-sm text-muted-foreground">
            {loading ? "Loading documents..." : "No documents available for this agent."}
          </p>
        ) : (
          <article className="space-y-3 rounded-md border bg-muted/20 p-4">
            <header className="space-y-1">
              <h3 className="text-lg font-semibold">{selectedDoc.name}</h3>
              <p className="text-sm text-muted-foreground">
                {selectedDoc.exists ? "File exists" : "File not found yet"} · {hasUnsavedChanges(selectedDoc.name) ? "Unsaved changes" : "No unsaved changes"}
              </p>
              {selectedDoc.aliasFor && (
                <p className="text-sm text-muted-foreground">
                  {selectedDoc.name} is an alias for {selectedDoc.aliasFor} ({selectedDoc.resolvedName}).
                </p>
              )}
            </header>

            <label htmlFor="docs-content" className="space-y-1 text-sm">
              <span>Document content</span>
              <p className="text-xs text-muted-foreground">Resolved file: {selectedDoc.resolvedName}</p>
              <textarea
                id="docs-content"
                className="min-h-[380px] w-full rounded-md border bg-background p-3 font-mono text-sm"
                value={asText(draftByName[selectedDoc.name])}
                disabled={featureDisabled || loading || saving}
                onChange={(event) => {
                  const value = event.target.value
                  setDraftByName((previous) => ({
                    ...previous,
                    [selectedDoc.name]: value,
                  }))
                  if (!statusKind || statusKind === "success") {
                    setStatusText("")
                    setStatusKind("")
                  }
                }}
              />
            </label>
          </article>
        )}
      </section>
    </div>
  )
}
