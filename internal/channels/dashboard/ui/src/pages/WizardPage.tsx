import { useCallback, useEffect, useMemo, useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useControlPlaneFeatures } from "@/hooks/useControlPlaneFeatures"
import { api } from "@/lib/api"

type WizardTemplate = {
  id: string
  name: string
  description: string
}

type WizardTemplatesResponse = {
  instance_templates?: unknown
  agent_templates?: unknown
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
  return String(value)
}

function normalizeTemplates(value: unknown): WizardTemplate[] {
  const items = Array.isArray(value) ? value : []
  return items
    .map((item) => {
      const record = asRecord(item)
      if (!record) {
        return null
      }
      const id = asText(record.id).trim()
      if (!id) {
        return null
      }
      return {
        id,
        name: asText(record.name).trim() || id,
        description: asText(record.description).trim(),
      }
    })
    .filter((item): item is WizardTemplate => item !== null)
}

export function WizardPage() {
  const { features, loading: featuresLoading } = useControlPlaneFeatures()
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [instanceTemplates, setInstanceTemplates] = useState<WizardTemplate[]>([])
  const [agentTemplates, setAgentTemplates] = useState<WizardTemplate[]>([])
  const featureDisabled = !featuresLoading && !features.wizard

  const loadTemplates = useCallback(async () => {
    if (featureDisabled) {
      setLoading(false)
      return
    }
    setLoading(true)
    setError("")
    try {
      const payload = await api.get<WizardTemplatesResponse>("/api/admin/wizard/templates")
      setInstanceTemplates(normalizeTemplates(payload.instance_templates))
      setAgentTemplates(normalizeTemplates(payload.agent_templates))
    } catch (loadError) {
      setInstanceTemplates([])
      setAgentTemplates([])
      setError(loadError instanceof Error ? loadError.message : String(loadError || "Failed to load wizard templates"))
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
      setInstanceTemplates([])
      setAgentTemplates([])
      return
    }
    void loadTemplates()
  }, [featureDisabled, featuresLoading, loadTemplates])

  const totalTemplates = useMemo(() => instanceTemplates.length + agentTemplates.length, [agentTemplates.length, instanceTemplates.length])

  return (
    <div className="space-y-4 p-6" data-testid="wizard-page">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Wizard</h2>
        <p className="text-sm text-muted-foreground">
          Browse guided instance and agent templates before stepping through the control-plane create flows.
        </p>
      </div>

      {featureDisabled ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Wizard unavailable</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="rounded-md border border-border bg-muted/30 p-4" data-testid="wizard-disabled-state">
              <p className="text-sm font-medium">Wizard disabled</p>
              <p className="mt-1 text-sm text-muted-foreground">
                Guided instance and agent creation is disabled for this control plane.
              </p>
            </div>
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-2">
            <div className="space-y-1">
              <CardTitle className="text-base">Template catalog</CardTitle>
              <CardDescription>
                Review the starting templates available for guided instance and agent setup.
              </CardDescription>
            </div>
            <Button type="button" variant="outline" size="sm" disabled={featureDisabled || loading} onClick={() => void loadTemplates()}>
              {loading ? "Refreshing..." : "Refresh templates"}
            </Button>
          </div>
        </CardHeader>
        <CardContent className="space-y-6">
          {loading ? <p className="text-sm text-muted-foreground">Loading wizard templates...</p> : null}

          {!featureDisabled && !loading && error ? (
            <div className="rounded-md border border-destructive/50 bg-destructive/5 p-3">
              <p className="text-sm text-destructive">Failed to load templates: {error}</p>
              <Button type="button" size="sm" variant="outline" className="mt-2" onClick={() => void loadTemplates()}>
                Retry
              </Button>
            </div>
          ) : null}

          {!featureDisabled && !loading && !error ? (
            <p className="text-sm text-muted-foreground" data-testid="wizard-template-count">
              {totalTemplates} templates available across instances and agents.
            </p>
          ) : null}

          {!featureDisabled && !loading && !error ? (
            <div className="grid gap-4 xl:grid-cols-2">
              <section className="space-y-3" aria-labelledby="wizard-instance-templates-heading">
                <div className="space-y-1">
                  <h3 id="wizard-instance-templates-heading" className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
                    Instance templates
                  </h3>
                  <p className="text-sm text-muted-foreground">Choose the base operating shape for a new control-plane instance.</p>
                </div>
                <div className="grid gap-3">
                  {instanceTemplates.map((template) => (
                    <article key={template.id} className="rounded-lg border bg-card p-4" data-testid={`wizard-instance-template-${template.id}`}>
                      <div className="flex items-start justify-between gap-2">
                        <div>
                          <h4 className="text-sm font-medium">{template.name}</h4>
                          <p className="mt-1 text-xs uppercase tracking-wide text-muted-foreground">{template.id}</p>
                        </div>
                      </div>
                      <p className="mt-3 text-sm text-muted-foreground">{template.description || "No description provided."}</p>
                    </article>
                  ))}
                </div>
              </section>

              <section className="space-y-3" aria-labelledby="wizard-agent-templates-heading">
                <div className="space-y-1">
                  <h3 id="wizard-agent-templates-heading" className="text-sm font-semibold uppercase tracking-wide text-muted-foreground">
                    Agent templates
                  </h3>
                  <p className="text-sm text-muted-foreground">Choose the initial profile shape for a new instance-scoped agent.</p>
                </div>
                <div className="grid gap-3">
                  {agentTemplates.map((template) => (
                    <article key={template.id} className="rounded-lg border bg-card p-4" data-testid={`wizard-agent-template-${template.id}`}>
                      <div className="flex items-start justify-between gap-2">
                        <div>
                          <h4 className="text-sm font-medium">{template.name}</h4>
                          <p className="mt-1 text-xs uppercase tracking-wide text-muted-foreground">{template.id}</p>
                        </div>
                      </div>
                      <p className="mt-3 text-sm text-muted-foreground">{template.description || "No description provided."}</p>
                    </article>
                  ))}
                </div>
              </section>
            </div>
          ) : null}
        </CardContent>
      </Card>
    </div>
  )
}
