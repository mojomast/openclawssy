import { useCallback, useEffect, useMemo, useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useControlPlaneFeatures } from "@/hooks/useControlPlaneFeatures"
import { api } from "@/lib/api"

type InstanceSummary = {
  id: string
  name: string
  description: string
  template: string
  source: string
  updatedAt: string
  modelProvider: string
  modelName: string
  agentCount: number
  isActive: boolean
}

type InstancesResponse = {
  instances?: unknown
  active_instance_id?: unknown
}

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
  return String(value).trim()
}

function normalizeInstances(value: unknown, activeInstanceID: string): InstanceSummary[] {
  const items = Array.isArray(value) ? value : []
  return items
    .map((item) => {
      const record = asRecord(item)
      if (!record) {
        return null
      }
      const id = asText(record.id)
      if (!id) {
        return null
      }
      return {
        id,
        name: asText(record.name) || id,
        description: asText(record.description),
        template: asText(record.template),
        source: asText(record.source),
        updatedAt: asText(record.updated_at),
        modelProvider: asText(record.model_provider),
        modelName: asText(record.model_name),
        agentCount: Number(record.agent_count) || 0,
        isActive: asText(record.is_active) === "true" || id === activeInstanceID || record.is_active === true,
      }
    })
    .filter((item): item is InstanceSummary => item !== null)
}

function formatDateTime(value: string): string {
  if (!value) {
    return "-"
  }
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return "-"
  }
  return parsed.toLocaleString()
}

export function InstancesPage() {
  const { features, loading: featuresLoading } = useControlPlaneFeatures()
  const [instances, setInstances] = useState<InstanceSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [activatingID, setActivatingID] = useState("")
  const [error, setError] = useState("")
  const [statusText, setStatusText] = useState("")
  const featureDisabled = !featuresLoading && !features.instanceControl

  const loadInstances = useCallback(async () => {
    if (featureDisabled) {
      setLoading(false)
      return
    }
    setLoading(true)
    setError("")
    try {
      const payload = await api.get<InstancesResponse>("/api/admin/instances")
      const activeInstanceID = asText(payload.active_instance_id)
      setInstances(normalizeInstances(payload.instances, activeInstanceID))
    } catch (loadError) {
      setInstances([])
      setError(loadError instanceof Error ? loadError.message : String(loadError || "Failed to load instances"))
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
      setError("")
      setInstances([])
      setStatusText("")
      setActivatingID("")
      return
    }
    void loadInstances()
  }, [featureDisabled, featuresLoading, loadInstances])

  const activeInstance = useMemo(() => instances.find((instance) => instance.isActive) || null, [instances])

  const activateInstance = useCallback(async (instanceID: string) => {
    if (featureDisabled) {
      return
    }
    setActivatingID(instanceID)
    setStatusText("")
    setError("")
    try {
      await api.post(`/api/admin/instances/${encodeURIComponent(instanceID)}/activate`, {})
      setStatusText(`Activated instance ${instanceID}.`)
      await loadInstances()
    } catch (activateError) {
      setError(activateError instanceof Error ? activateError.message : String(activateError || "Failed to activate instance"))
    } finally {
      setActivatingID("")
    }
  }, [featureDisabled, loadInstances])

  return (
    <div className="space-y-4 p-6" data-testid="instances-page">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Instances</h2>
        <p className="text-sm text-muted-foreground">
          Review canonical instances, confirm which one is active, and switch the active operator context when needed.
        </p>
      </div>

      {featureDisabled ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Instances unavailable</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="rounded-md border border-border bg-muted/30 p-4" data-testid="instances-disabled-state">
              <p className="text-sm font-medium">Instance control disabled</p>
              <p className="mt-1 text-sm text-muted-foreground">
                Instance management is disabled for this control plane.
              </p>
            </div>
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-2">
            <div className="space-y-1">
              <CardTitle className="text-base">Active instance</CardTitle>
              <CardDescription>Track the current instance-scoped operator context before using agent and run surfaces.</CardDescription>
            </div>
            <Button type="button" variant="outline" size="sm" disabled={featureDisabled || loading || Boolean(activatingID)} onClick={() => void loadInstances()}>
              {loading ? "Refreshing..." : "Refresh instances"}
            </Button>
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          {activeInstance ? (
            <div className="rounded-md border border-emerald-500/40 bg-emerald-500/5 p-4" data-testid="active-instance-card">
              <p className="text-sm font-medium">{activeInstance.name}</p>
              <p className="mt-1 text-sm text-muted-foreground">`{activeInstance.id}` is the active instance.</p>
            </div>
          ) : !loading && !featureDisabled ? (
            <p className="text-sm text-muted-foreground">No active instance found.</p>
          ) : null}

          {statusText ? <p className="text-sm text-emerald-700 dark:text-emerald-300">{statusText}</p> : null}

          {loading ? <p className="text-sm text-muted-foreground">Loading instances...</p> : null}

          {!featureDisabled && !loading && error ? (
            <div className="rounded-md border border-destructive/50 bg-destructive/5 p-3">
              <p className="text-sm text-destructive">Failed to load or activate instances: {error}</p>
              <Button type="button" size="sm" variant="outline" className="mt-2" onClick={() => void loadInstances()}>
                Retry
              </Button>
            </div>
          ) : null}
        </CardContent>
      </Card>

      {!featureDisabled ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">All instances</CardTitle>
            <CardDescription>Activation switches the active instance for wizard, agent, and run-adjacent control-plane flows.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {!loading && !error && instances.length === 0 ? <p className="text-sm text-muted-foreground">No instances found.</p> : null}
            {instances.map((instance) => (
              <article key={instance.id} className="rounded-lg border bg-card p-4" data-testid={`instance-item-${instance.id}`}>
                <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                  <div className="space-y-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <h3 className="text-sm font-medium">{instance.name}</h3>
                      {instance.isActive ? (
                        <span className="rounded-full border border-emerald-500/40 bg-emerald-500/10 px-2 py-0.5 text-xs text-emerald-700 dark:text-emerald-300">
                          Active
                        </span>
                      ) : null}
                    </div>
                    <p className="text-sm text-muted-foreground">{instance.description || "No description provided."}</p>
                    <div className="grid gap-2 text-xs text-muted-foreground md:grid-cols-2 xl:grid-cols-4">
                      <p>Instance ID: {instance.id}</p>
                      <p>Agents: {instance.agentCount}</p>
                      <p>Model: {[instance.modelProvider, instance.modelName].filter(Boolean).join(" / ") || "-"}</p>
                      <p>Updated: {formatDateTime(instance.updatedAt)}</p>
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <Button
                      type="button"
                      size="sm"
                      variant={instance.isActive ? "outline" : "default"}
                      data-testid={`activate-instance-${instance.id}`}
                      disabled={instance.isActive || Boolean(activatingID)}
                      onClick={() => void activateInstance(instance.id)}
                    >
                      {activatingID === instance.id ? "Activating..." : instance.isActive ? "Active" : "Make active"}
                    </Button>
                  </div>
                </div>
              </article>
            ))}
          </CardContent>
        </Card>
      ) : null}
    </div>
  )
}
