import { ChangeEvent, useCallback, useEffect, useMemo, useState } from "react"
import { Button } from "@/components/ui/button"
import { CodeBlock } from "@/components/CodeEditor"
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

type WizardInstanceFormState = {
  templateID: string
  instanceID: string
  name: string
  description: string
  modelProvider: string
  modelName: string
  defaultAgentID: string
}

type WizardInstancePlan = {
  instance: Record<string, unknown>
  operations: string[]
}

type WizardInstancePlanResponse = {
  plan?: {
    instance?: unknown
    operations?: unknown
  }
}

type WizardInstanceCreateResponse = {
  instance?: unknown
}

type WizardInstanceSummary = {
  id: string
  name: string
}

type WizardInstancesResponse = {
  instances?: unknown
  active_instance_id?: unknown
}

type WizardInstanceAgentsResponse = {
  agents?: unknown
}

type WizardAgentFormState = {
  instanceID: string
  templateID: string
  agentID: string
  enabled: boolean
  selfImprovement: boolean
  modelProvider: string
  modelName: string
  modelTimeoutMS: string
}

type WizardAgentPlan = {
  instanceID: string
  agentID: string
  templateID: string
  operations: string[]
  profile: Record<string, unknown>
}

type WizardAgentPlanResponse = {
  plan?: unknown
}

type WizardAgentCreateResponse = {
  instance_id?: unknown
  agent?: unknown
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

function normalizeInstances(value: unknown): WizardInstanceSummary[] {
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
      }
    })
    .filter((item): item is WizardInstanceSummary => item !== null)
}

function normalizeAgentIDs(value: unknown): string[] {
  const items = Array.isArray(value) ? value : []
  return items
    .map((item) => {
      const record = asRecord(item)
      return asText(record?.agent_id).trim()
    })
    .filter((item) => item.length > 0)
}

function emptyInstanceForm(): WizardInstanceFormState {
  return {
    templateID: "blank",
    instanceID: "",
    name: "",
    description: "",
    modelProvider: "",
    modelName: "",
    defaultAgentID: "default",
  }
}

function emptyAgentForm(): WizardAgentFormState {
  return {
    instanceID: "",
    templateID: "general",
    agentID: "",
    enabled: true,
    selfImprovement: false,
    modelProvider: "",
    modelName: "",
    modelTimeoutMS: "",
  }
}

function normalizeOperations(value: unknown): string[] {
  const items = Array.isArray(value) ? value : []
  return items.map((item) => asText(item).trim()).filter((item) => item.length > 0)
}

function prettyJSON(value: unknown): string {
  try {
    return JSON.stringify(value ?? {}, null, 2)
  } catch {
    return "{}"
  }
}

function deriveInstanceIDFromName(name: string): string {
  return name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
}

function buildInstanceRequest(form: WizardInstanceFormState): Record<string, unknown> {
  return {
    template_id: form.templateID,
    instance_id: form.instanceID.trim(),
    name: form.name.trim(),
    description: form.description.trim(),
    model_provider: form.modelProvider.trim(),
    model_name: form.modelName.trim(),
    default_agent_id: form.templateID === "chat-assistant" ? form.defaultAgentID.trim() : "",
  }
}

function parsePlan(value: unknown): WizardInstancePlan | null {
  const record = asRecord(value)
  if (!record) {
    return null
  }
  const instance = asRecord(record.instance)
  if (!instance) {
    return null
  }
  return {
    instance,
    operations: normalizeOperations(record.operations),
  }
}

function parseAgentPlan(value: unknown): WizardAgentPlan | null {
  const record = asRecord(value)
  if (!record) {
    return null
  }
  const profile = asRecord(record.profile)
  if (!profile) {
    return null
  }
  return {
    instanceID: asText(record.instance_id).trim(),
    agentID: asText(record.agent_id).trim(),
    templateID: asText(record.template_id).trim(),
    operations: normalizeOperations(record.operations),
    profile,
  }
}

function buildAgentRequest(form: WizardAgentFormState): Record<string, unknown> {
  const timeoutValue = form.modelTimeoutMS.trim()
  const parsedTimeout = Number.parseInt(timeoutValue || "0", 10)
  return {
    instance_id: form.instanceID,
    agent_id: form.agentID.trim(),
    template_id: form.templateID,
    enabled: form.enabled,
    self_improvement: form.selfImprovement,
    model_provider: form.modelProvider.trim(),
    model_name: form.modelName.trim(),
    ...(timeoutValue ? { model_timeout_ms: Number.isFinite(parsedTimeout) ? parsedTimeout : 0 } : {}),
  }
}

export function WizardPage() {
  const { features, loading: featuresLoading } = useControlPlaneFeatures()
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [instanceTemplates, setInstanceTemplates] = useState<WizardTemplate[]>([])
  const [agentTemplates, setAgentTemplates] = useState<WizardTemplate[]>([])
  const [instanceForm, setInstanceForm] = useState<WizardInstanceFormState>(() => emptyInstanceForm())
  const [instancePlan, setInstancePlan] = useState<WizardInstancePlan | null>(null)
  const [instancePlanError, setInstancePlanError] = useState("")
  const [instanceCreateError, setInstanceCreateError] = useState("")
  const [instanceSuccessMessage, setInstanceSuccessMessage] = useState("")
  const [planningInstance, setPlanningInstance] = useState(false)
  const [creatingInstance, setCreatingInstance] = useState(false)
  const [instances, setInstances] = useState<WizardInstanceSummary[]>([])
  const [instanceAgents, setInstanceAgents] = useState<string[]>([])
  const [instanceListLoading, setInstanceListLoading] = useState(false)
  const [agentForm, setAgentForm] = useState<WizardAgentFormState>(() => emptyAgentForm())
  const [agentPlan, setAgentPlan] = useState<WizardAgentPlan | null>(null)
  const [agentPlanError, setAgentPlanError] = useState("")
  const [agentCreateError, setAgentCreateError] = useState("")
  const [agentSuccessMessage, setAgentSuccessMessage] = useState("")
  const [planningAgent, setPlanningAgent] = useState(false)
  const [creatingAgent, setCreatingAgent] = useState(false)
  const featureDisabled = !featuresLoading && !features.wizard
  const instanceWizardDisabled = featureDisabled || !featuresLoading && !features.instanceControl
  const agentFeatureDisabled = featureDisabled || !featuresLoading && !features.instanceAgents

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
      setInstancePlan(null)
      setInstancePlanError("")
      setInstanceCreateError("")
      setInstanceSuccessMessage("")
      setInstances([])
      setInstanceAgents([])
      setAgentForm(emptyAgentForm())
      setAgentPlan(null)
      setAgentPlanError("")
      setAgentCreateError("")
      setAgentSuccessMessage("")
      setInstanceListLoading(false)
      setPlanningInstance(false)
      setCreatingInstance(false)
      setPlanningAgent(false)
      setCreatingAgent(false)
      return
    }
    void loadTemplates()
  }, [featureDisabled, featuresLoading, loadTemplates])

  const loadInstances = useCallback(async () => {
    if (agentFeatureDisabled) {
      setInstances([])
      setInstanceAgents([])
      return
    }
    setInstanceListLoading(true)
    try {
      const [listPayload, activePayload] = await Promise.all([
        api.get<WizardInstancesResponse>("/api/admin/instances"),
        api.get<{ instance?: unknown }>("/api/admin/instances/active"),
      ])
      const nextInstances = normalizeInstances(listPayload.instances)
      const activeInstance = asRecord(activePayload.instance)
      const activeID = asText(activeInstance?.id).trim() || asText(listPayload.active_instance_id).trim()
      const selectedInstanceID = activeID || nextInstances[0]?.id || ""
      setInstances(nextInstances)
      setAgentForm((current) => ({ ...current, instanceID: selectedInstanceID || current.instanceID }))
    } finally {
      setInstanceListLoading(false)
    }
  }, [agentFeatureDisabled])

  useEffect(() => {
    if (featuresLoading || agentFeatureDisabled) {
      return
    }
    void loadInstances()
  }, [agentFeatureDisabled, featuresLoading, loadInstances])

  const loadInstanceAgents = useCallback(async (instanceID: string) => {
    const normalized = instanceID.trim()
    if (!normalized || agentFeatureDisabled) {
      setInstanceAgents([])
      return
    }
    try {
      const payload = await api.get<WizardInstanceAgentsResponse>(`/api/admin/instances/${encodeURIComponent(normalized)}/agents`)
      setInstanceAgents(normalizeAgentIDs(payload.agents))
    } catch {
      setInstanceAgents([])
    }
  }, [agentFeatureDisabled])

  useEffect(() => {
    if (!agentForm.instanceID) {
      setInstanceAgents([])
      return
    }
    void loadInstanceAgents(agentForm.instanceID)
  }, [agentForm.instanceID, loadInstanceAgents])

  const totalTemplates = useMemo(() => instanceTemplates.length + agentTemplates.length, [agentTemplates.length, instanceTemplates.length])
  const selectedInstanceTemplate = useMemo(() => {
    return instanceTemplates.find((template) => template.id === instanceForm.templateID) || null
  }, [instanceForm.templateID, instanceTemplates])
  const instanceConfigPreview = useMemo(() => prettyJSON(instancePlan?.instance.config ?? {}), [instancePlan])
  const showDefaultAgentField = instanceForm.templateID === "chat-assistant"
  const selectedAgentTemplate = useMemo(() => {
    return agentTemplates.find((template) => template.id === agentForm.templateID) || null
  }, [agentForm.templateID, agentTemplates])
  const selectedAgentTargetInstance = useMemo(() => {
    return instances.find((instance) => instance.id === agentForm.instanceID) || null
  }, [agentForm.instanceID, instances])
  const existingAgentConflict = useMemo(() => {
    const target = agentForm.agentID.trim()
    if (!target) {
      return false
    }
    return instanceAgents.includes(target)
  }, [agentForm.agentID, instanceAgents])
  const agentProfilePreview = useMemo(() => prettyJSON(agentPlan?.profile ?? {}), [agentPlan])

  const selectInstanceTemplate = useCallback((templateID: string) => {
    setInstanceForm((current) => ({
      ...current,
      templateID,
      defaultAgentID: templateID === "chat-assistant" ? current.defaultAgentID || "default" : current.defaultAgentID,
    }))
    setInstancePlan(null)
    setInstancePlanError("")
    setInstanceCreateError("")
    setInstanceSuccessMessage("")
  }, [])

  const updateInstanceForm = useCallback((key: keyof WizardInstanceFormState, value: string) => {
    setInstanceForm((current) => {
      if (key === "name") {
        const nextID = current.instanceID.trim() ? current.instanceID : deriveInstanceIDFromName(value)
        return { ...current, name: value, instanceID: nextID }
      }
      return { ...current, [key]: value }
    })
    setInstancePlan(null)
    setInstancePlanError("")
    setInstanceCreateError("")
    setInstanceSuccessMessage("")
  }, [])

  const handleInstanceFieldChange = useCallback((key: keyof WizardInstanceFormState) => {
    return (event: ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) => {
      updateInstanceForm(key, event.target.value)
    }
  }, [updateInstanceForm])

  const previewInstancePlan = useCallback(async () => {
    if (instanceWizardDisabled) {
      return
    }
    setPlanningInstance(true)
    setInstancePlanError("")
    setInstanceCreateError("")
    setInstanceSuccessMessage("")
    try {
      const payload = await api.post<WizardInstancePlanResponse>("/api/admin/wizard/instances/plan", buildInstanceRequest(instanceForm))
      const nextPlan = parsePlan(payload.plan)
      if (!nextPlan) {
        throw new Error("wizard plan response was incomplete")
      }
      setInstancePlan(nextPlan)
    } catch (planError) {
      setInstancePlan(null)
      setInstancePlanError(planError instanceof Error ? planError.message : String(planError || "Failed to preview plan"))
    } finally {
      setPlanningInstance(false)
    }
  }, [instanceForm, instanceWizardDisabled])

  const createInstance = useCallback(async () => {
    if (instanceWizardDisabled) {
      return
    }
    setCreatingInstance(true)
    setInstanceCreateError("")
    setInstanceSuccessMessage("")
    try {
      const payload = await api.post<WizardInstanceCreateResponse>("/api/admin/wizard/instances/create", buildInstanceRequest(instanceForm))
      const instance = asRecord(payload.instance)
      const createdID = asText(instance?.id).trim() || instanceForm.instanceID.trim()
      const createdName = asText(instance?.name).trim() || instanceForm.name.trim() || createdID
      setInstanceSuccessMessage(`Created instance ${createdName} (${createdID}).`)
      const nextPlan = parsePlan({ instance, operations: instancePlan?.operations || [] })
      setInstancePlan(nextPlan)
      await loadInstances()
      setAgentForm((current) => ({ ...current, instanceID: createdID }))
      await loadInstanceAgents(createdID)
    } catch (createError) {
      setInstanceCreateError(createError instanceof Error ? createError.message : String(createError || "Failed to create instance"))
    } finally {
      setCreatingInstance(false)
    }
  }, [instanceForm, instancePlan?.operations, instanceWizardDisabled, loadInstanceAgents, loadInstances])

  const resetInstanceFlow = useCallback(() => {
    setInstanceForm(emptyInstanceForm())
    setInstancePlan(null)
    setInstancePlanError("")
    setInstanceCreateError("")
    setInstanceSuccessMessage("")
  }, [])

  const selectAgentTemplate = useCallback((templateID: string) => {
    setAgentForm((current) => ({ ...current, templateID }))
    setAgentPlan(null)
    setAgentPlanError("")
    setAgentCreateError("")
    setAgentSuccessMessage("")
  }, [])

  const updateAgentForm = useCallback((key: keyof WizardAgentFormState, value: string | boolean) => {
    setAgentForm((current) => ({ ...current, [key]: value }))
    setAgentPlan(null)
    setAgentPlanError("")
    setAgentCreateError("")
    setAgentSuccessMessage("")
  }, [])

  const previewAgentPlan = useCallback(async () => {
    if (agentFeatureDisabled || !agentForm.instanceID) {
      return
    }
    setPlanningAgent(true)
    setAgentPlanError("")
    setAgentCreateError("")
    setAgentSuccessMessage("")
    try {
      const payload = await api.post<WizardAgentPlanResponse>("/api/admin/wizard/agents/plan", buildAgentRequest(agentForm))
      const nextPlan = parseAgentPlan(payload.plan)
      if (!nextPlan) {
        throw new Error("wizard agent plan response was incomplete")
      }
      setAgentPlan(nextPlan)
    } catch (planError) {
      setAgentPlan(null)
      setAgentPlanError(planError instanceof Error ? planError.message : String(planError || "Failed to preview agent plan"))
    } finally {
      setPlanningAgent(false)
    }
  }, [agentFeatureDisabled, agentForm])

  const createAgent = useCallback(async () => {
    if (agentFeatureDisabled || !agentForm.instanceID || existingAgentConflict) {
      return
    }
    setCreatingAgent(true)
    setAgentCreateError("")
    setAgentSuccessMessage("")
    try {
      const payload = await api.post<WizardAgentCreateResponse>("/api/admin/wizard/agents/create", buildAgentRequest(agentForm))
      const agent = asRecord(payload.agent)
      const createdAgentID = asText(agent?.agent_id).trim() || agentForm.agentID.trim()
      setAgentSuccessMessage(`Created agent ${createdAgentID} in ${agentForm.instanceID}.`)
      await loadInstanceAgents(agentForm.instanceID)
      const nextPlan = parseAgentPlan({
        instance_id: asText(payload.instance_id).trim() || agentForm.instanceID,
        agent_id: createdAgentID,
        template_id: agentForm.templateID,
        operations: agentPlan?.operations || [],
        profile: asRecord(agent?.profile) || agentPlan?.profile || {},
      })
      setAgentPlan(nextPlan)
    } catch (createError) {
      setAgentCreateError(createError instanceof Error ? createError.message : String(createError || "Failed to create agent"))
    } finally {
      setCreatingAgent(false)
    }
  }, [agentFeatureDisabled, agentForm, agentPlan?.operations, agentPlan?.profile, existingAgentConflict, loadInstanceAgents])

  const resetAgentFlow = useCallback(() => {
    setAgentForm((current) => ({ ...emptyAgentForm(), instanceID: current.instanceID }))
    setAgentPlan(null)
    setAgentPlanError("")
    setAgentCreateError("")
    setAgentSuccessMessage("")
  }, [])

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
                    <button
                      key={template.id}
                      type="button"
                      className={`rounded-lg border bg-card p-4 text-left transition-colors ${
                        instanceForm.templateID === template.id ? "border-primary bg-primary/5" : "hover:bg-muted/40"
                      }`}
                      data-testid={`wizard-instance-template-${template.id}`}
                      disabled={instanceWizardDisabled || planningInstance || creatingInstance}
                      onClick={() => selectInstanceTemplate(template.id)}
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div>
                          <h4 className="text-sm font-medium">{template.name}</h4>
                          <p className="mt-1 text-xs uppercase tracking-wide text-muted-foreground">{template.id}</p>
                        </div>
                      </div>
                      <p className="mt-3 text-sm text-muted-foreground">{template.description || "No description provided."}</p>
                    </button>
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

      {!featureDisabled ? (
        <div className="grid gap-4 xl:grid-cols-[minmax(0,0.95fr)_minmax(0,1.05fr)]">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Instance wizard</CardTitle>
              <CardDescription>
                Start from a template, preview the canonical config snapshot, and create the instance when the plan looks right.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="rounded-md border border-border bg-muted/30 p-3" data-testid="wizard-selected-instance-template">
                <p className="text-sm font-medium">Selected template: {selectedInstanceTemplate?.name || "Blank"}</p>
                <p className="mt-1 text-sm text-muted-foreground">
                  {selectedInstanceTemplate?.description || "Start from the default safe configuration."}
                </p>
              </div>

              <div className="grid gap-3 md:grid-cols-2">
                <label className="space-y-1 text-sm">
                  <span>Instance template</span>
                  <select
                    className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                    value={instanceForm.templateID}
                    disabled={planningInstance || creatingInstance}
                    onChange={handleInstanceFieldChange("templateID")}
                  >
                    {instanceTemplates.map((template) => (
                      <option key={template.id} value={template.id}>
                        {template.name}
                      </option>
                    ))}
                  </select>
                </label>

                <label className="space-y-1 text-sm">
                  <span>Instance ID</span>
                  <input
                    className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                    value={instanceForm.instanceID}
                    data-testid="wizard-instance-id"
                    disabled={planningInstance || creatingInstance}
                    onChange={handleInstanceFieldChange("instanceID")}
                    placeholder="team-assistant"
                  />
                </label>

                <label className="space-y-1 text-sm">
                  <span>Name</span>
                  <input
                    className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                    value={instanceForm.name}
                    data-testid="wizard-instance-name"
                    disabled={planningInstance || creatingInstance}
                    onChange={handleInstanceFieldChange("name")}
                    placeholder="Team Assistant"
                  />
                </label>

                <label className="space-y-1 text-sm">
                  <span>Model provider</span>
                  <input
                    className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                    value={instanceForm.modelProvider}
                    data-testid="wizard-model-provider"
                    disabled={planningInstance || creatingInstance}
                    onChange={handleInstanceFieldChange("modelProvider")}
                    placeholder="openai"
                  />
                </label>

                <label className="space-y-1 text-sm md:col-span-2">
                  <span>Model name</span>
                  <input
                    className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                    value={instanceForm.modelName}
                    data-testid="wizard-model-name"
                    disabled={planningInstance || creatingInstance}
                    onChange={handleInstanceFieldChange("modelName")}
                    placeholder="gpt-4.1-mini"
                  />
                </label>

                <label className="space-y-1 text-sm md:col-span-2">
                  <span>Description</span>
                  <textarea
                    className="min-h-24 w-full rounded-md border bg-background px-3 py-2 text-sm"
                    value={instanceForm.description}
                    data-testid="wizard-instance-description"
                    disabled={planningInstance || creatingInstance}
                    onChange={handleInstanceFieldChange("description")}
                    placeholder="Operator-facing assistant for the support team."
                  />
                </label>

                {showDefaultAgentField ? (
                  <label className="space-y-1 text-sm md:col-span-2">
                    <span>Default agent ID</span>
                    <input
                      className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                      value={instanceForm.defaultAgentID}
                      data-testid="wizard-default-agent-id"
                      disabled={planningInstance || creatingInstance}
                      onChange={handleInstanceFieldChange("defaultAgentID")}
                      placeholder="assistant"
                    />
                  </label>
                ) : null}
              </div>

              <div className="rounded-md border border-border bg-muted/20 p-3 text-sm text-muted-foreground" data-testid="wizard-instance-note">
                {showDefaultAgentField
                  ? "Chat Assistant preconfigures channel default-agent pointers but does not create the agent profile yet. Create the instance first, then add the agent in the next wizard step."
                  : "Preview checks the instance manifest shape before create. You can safely iterate on the form until the plan matches what you want to persist."}
              </div>

              {!featuresLoading && !features.instanceControl ? (
                <div className="rounded-md border border-border bg-muted/30 p-4" data-testid="wizard-instance-disabled-state">
                  <p className="text-sm font-medium">Instance creation disabled</p>
                  <p className="mt-1 text-sm text-muted-foreground">
                    Instance control is disabled for this control plane, so wizard instance plan/create actions are unavailable.
                  </p>
                </div>
              ) : null}

              {instancePlanError ? (
                <div className="rounded-md border border-destructive/50 bg-destructive/5 p-3">
                  <p className="text-sm text-destructive">Failed to preview instance plan: {instancePlanError}</p>
                </div>
              ) : null}

              {instanceCreateError ? (
                <div className="rounded-md border border-destructive/50 bg-destructive/5 p-3">
                  <p className="text-sm text-destructive">Failed to create instance: {instanceCreateError}</p>
                </div>
              ) : null}

              {instanceSuccessMessage ? (
                <div className="rounded-md border border-emerald-500/40 bg-emerald-500/5 p-3" data-testid="wizard-instance-success">
                  <p className="text-sm text-emerald-700 dark:text-emerald-300">{instanceSuccessMessage}</p>
                </div>
              ) : null}

              <div className="flex flex-wrap gap-2">
                <Button type="button" data-testid="wizard-preview-instance" disabled={instanceWizardDisabled || planningInstance || creatingInstance} onClick={() => void previewInstancePlan()}>
                  {planningInstance ? "Previewing..." : "Preview plan"}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  data-testid="wizard-create-instance"
                  disabled={instanceWizardDisabled || planningInstance || creatingInstance}
                  onClick={() => void createInstance()}
                >
                  {creatingInstance ? "Creating..." : "Create instance"}
                </Button>
                <Button type="button" variant="ghost" disabled={instanceWizardDisabled || planningInstance || creatingInstance} onClick={resetInstanceFlow}>
                  Start over
                </Button>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">Plan preview</CardTitle>
              <CardDescription>
                Review the instance snapshot and planned operations before persisting anything.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {!instancePlan ? (
                <p className="text-sm text-muted-foreground" data-testid="wizard-instance-plan-empty">
                  Preview an instance plan to inspect the generated config snapshot and wizard operations.
                </p>
              ) : (
                <>
                  <div className="grid gap-3 rounded-md border border-border bg-muted/20 p-3 md:grid-cols-2" data-testid="wizard-instance-plan-summary">
                    <div>
                      <p className="text-xs uppercase tracking-wide text-muted-foreground">Template</p>
                      <p className="text-sm font-medium">{asText(instancePlan.instance.template) || instanceForm.templateID}</p>
                    </div>
                    <div>
                      <p className="text-xs uppercase tracking-wide text-muted-foreground">Instance ID</p>
                      <p className="text-sm font-medium">{asText(instancePlan.instance.id) || instanceForm.instanceID}</p>
                    </div>
                    <div>
                      <p className="text-xs uppercase tracking-wide text-muted-foreground">Name</p>
                      <p className="text-sm font-medium">{asText(instancePlan.instance.name) || instanceForm.name || "-"}</p>
                    </div>
                    <div>
                      <p className="text-xs uppercase tracking-wide text-muted-foreground">Source</p>
                      <p className="text-sm font-medium">{asText(instancePlan.instance.source) || "wizard"}</p>
                    </div>
                  </div>

                  <div className="space-y-2">
                    <h3 className="text-sm font-medium">Planned operations</h3>
                    <ul className="space-y-2 text-sm text-muted-foreground" data-testid="wizard-instance-operations">
                      {instancePlan.operations.map((operation) => (
                        <li key={operation} className="rounded-md border border-border bg-muted/20 px-3 py-2">
                          {operation}
                        </li>
                      ))}
                    </ul>
                  </div>

                  <div className="space-y-2">
                    <h3 className="text-sm font-medium">Config preview</h3>
                    <CodeBlock code={instanceConfigPreview} language="json" maxHeight="360px" className="bg-background" />
                  </div>
                </>
              )}
            </CardContent>
          </Card>
        </div>
      ) : null}

      {!agentFeatureDisabled ? (
        <div className="grid gap-4 xl:grid-cols-[minmax(0,0.95fr)_minmax(0,1.05fr)]">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Agent wizard</CardTitle>
              <CardDescription>
                Choose an existing instance, preview a normalized agent profile, and create the agent in place.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="rounded-md border border-border bg-muted/30 p-3" data-testid="wizard-selected-agent-template">
                <p className="text-sm font-medium">Selected template: {selectedAgentTemplate?.name || "General"}</p>
                <p className="mt-1 text-sm text-muted-foreground">
                  {selectedAgentTemplate?.description || "A general-purpose agent profile with explicit enablement."}
                </p>
              </div>

              <div className="grid gap-3 md:grid-cols-2">
                <label className="space-y-1 text-sm md:col-span-2">
                  <span>Target instance</span>
                  <select
                    className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                    value={agentForm.instanceID}
                    data-testid="wizard-agent-instance"
                    disabled={instanceListLoading || planningAgent || creatingAgent || instances.length === 0}
                    onChange={(event) => updateAgentForm("instanceID", event.target.value)}
                  >
                    <option value="">Select an instance</option>
                    {instances.map((instance) => (
                      <option key={instance.id} value={instance.id}>
                        {instance.name} ({instance.id})
                      </option>
                    ))}
                  </select>
                </label>

                <label className="space-y-1 text-sm">
                  <span>Agent template</span>
                  <select
                    className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                    value={agentForm.templateID}
                    data-testid="wizard-agent-template-select"
                    disabled={planningAgent || creatingAgent}
                    onChange={(event) => selectAgentTemplate(event.target.value)}
                  >
                    {agentTemplates.map((template) => (
                      <option key={template.id} value={template.id}>
                        {template.name}
                      </option>
                    ))}
                  </select>
                </label>

                <label className="space-y-1 text-sm">
                  <span>Agent ID</span>
                  <input
                    className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                    value={agentForm.agentID}
                    data-testid="wizard-agent-id"
                    disabled={planningAgent || creatingAgent}
                    onChange={(event) => updateAgentForm("agentID", event.target.value)}
                    placeholder="researcher"
                  />
                </label>

                <label className="space-y-1 text-sm">
                  <span>Model provider</span>
                  <input
                    className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                    value={agentForm.modelProvider}
                    data-testid="wizard-agent-model-provider"
                    disabled={planningAgent || creatingAgent}
                    onChange={(event) => updateAgentForm("modelProvider", event.target.value)}
                    placeholder="openai"
                  />
                </label>

                <label className="space-y-1 text-sm">
                  <span>Model name</span>
                  <input
                    className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                    value={agentForm.modelName}
                    data-testid="wizard-agent-model-name"
                    disabled={planningAgent || creatingAgent}
                    onChange={(event) => updateAgentForm("modelName", event.target.value)}
                    placeholder="gpt-4.1-mini"
                  />
                </label>

                <label className="space-y-1 text-sm">
                  <span>Model timeout (ms)</span>
                  <input
                    className="h-10 w-full rounded-md border bg-background px-3 text-sm"
                    value={agentForm.modelTimeoutMS}
                    data-testid="wizard-agent-timeout"
                    disabled={planningAgent || creatingAgent}
                    onChange={(event) => updateAgentForm("modelTimeoutMS", event.target.value)}
                    placeholder="180000"
                  />
                </label>

                <label className="flex items-center gap-2 rounded-md border border-border bg-muted/20 px-3 py-2 text-sm">
                  <input
                    type="checkbox"
                    checked={agentForm.enabled}
                    data-testid="wizard-agent-enabled"
                    disabled={planningAgent || creatingAgent}
                    onChange={(event) => updateAgentForm("enabled", event.target.checked)}
                  />
                  <span>Enabled</span>
                </label>

                <label className="flex items-center gap-2 rounded-md border border-border bg-muted/20 px-3 py-2 text-sm">
                  <input
                    type="checkbox"
                    checked={agentForm.selfImprovement}
                    data-testid="wizard-agent-self-improvement"
                    disabled={planningAgent || creatingAgent}
                    onChange={(event) => updateAgentForm("selfImprovement", event.target.checked)}
                  />
                  <span>Self improvement</span>
                </label>
              </div>

              <div className="rounded-md border border-border bg-muted/20 p-3 text-sm text-muted-foreground" data-testid="wizard-agent-note">
                {selectedAgentTargetInstance
                  ? `Targeting ${selectedAgentTargetInstance.name} (${selectedAgentTargetInstance.id}). Existing agents: ${instanceAgents.length ? instanceAgents.join(", ") : "none yet"}.`
                  : "Select an existing instance before previewing or creating an agent."}
              </div>

              {existingAgentConflict ? (
                <div className="rounded-md border border-amber-500/40 bg-amber-500/5 p-3" data-testid="wizard-agent-duplicate-warning">
                  <p className="text-sm text-amber-700 dark:text-amber-300">That agent ID already exists in the selected instance.</p>
                </div>
              ) : null}

              {agentPlanError ? (
                <div className="rounded-md border border-destructive/50 bg-destructive/5 p-3">
                  <p className="text-sm text-destructive">Failed to preview agent plan: {agentPlanError}</p>
                </div>
              ) : null}

              {agentCreateError ? (
                <div className="rounded-md border border-destructive/50 bg-destructive/5 p-3">
                  <p className="text-sm text-destructive">Failed to create agent: {agentCreateError}</p>
                </div>
              ) : null}

              {agentSuccessMessage ? (
                <div className="rounded-md border border-emerald-500/40 bg-emerald-500/5 p-3" data-testid="wizard-agent-success">
                  <p className="text-sm text-emerald-700 dark:text-emerald-300">{agentSuccessMessage}</p>
                </div>
              ) : null}

              <div className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  data-testid="wizard-preview-agent"
                  disabled={instanceListLoading || !agentForm.instanceID || planningAgent || creatingAgent}
                  onClick={() => void previewAgentPlan()}
                >
                  {planningAgent ? "Previewing..." : "Preview agent"}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  data-testid="wizard-create-agent"
                  disabled={instanceListLoading || !agentForm.instanceID || existingAgentConflict || planningAgent || creatingAgent}
                  onClick={() => void createAgent()}
                >
                  {creatingAgent ? "Creating..." : "Create agent"}
                </Button>
                <Button type="button" variant="ghost" disabled={planningAgent || creatingAgent} onClick={resetAgentFlow}>
                  Reset agent form
                </Button>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">Agent plan preview</CardTitle>
              <CardDescription>
                Review the normalized profile and wizard operations before writing the agent into the selected instance.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              {!agentPlan ? (
                <p className="text-sm text-muted-foreground" data-testid="wizard-agent-plan-empty">
                  Preview an agent plan to inspect the generated profile and operations.
                </p>
              ) : (
                <>
                  <div className="grid gap-3 rounded-md border border-border bg-muted/20 p-3 md:grid-cols-2" data-testid="wizard-agent-plan-summary">
                    <div>
                      <p className="text-xs uppercase tracking-wide text-muted-foreground">Target instance</p>
                      <p className="text-sm font-medium">{agentPlan.instanceID || agentForm.instanceID}</p>
                    </div>
                    <div>
                      <p className="text-xs uppercase tracking-wide text-muted-foreground">Agent ID</p>
                      <p className="text-sm font-medium">{agentPlan.agentID || agentForm.agentID}</p>
                    </div>
                    <div>
                      <p className="text-xs uppercase tracking-wide text-muted-foreground">Template</p>
                      <p className="text-sm font-medium">{agentPlan.templateID || agentForm.templateID}</p>
                    </div>
                    <div>
                      <p className="text-xs uppercase tracking-wide text-muted-foreground">Enabled</p>
                      <p className="text-sm font-medium">{String(asRecord(agentPlan.profile.enabled)?.valueOf?.() ?? agentForm.enabled)}</p>
                    </div>
                  </div>

                  <div className="space-y-2">
                    <h3 className="text-sm font-medium">Planned operations</h3>
                    <ul className="space-y-2 text-sm text-muted-foreground" data-testid="wizard-agent-operations">
                      {agentPlan.operations.map((operation) => (
                        <li key={operation} className="rounded-md border border-border bg-muted/20 px-3 py-2">
                          {operation}
                        </li>
                      ))}
                    </ul>
                  </div>

                  <div className="space-y-2">
                    <h3 className="text-sm font-medium">Profile preview</h3>
                    <CodeBlock code={agentProfilePreview} language="json" maxHeight="360px" className="bg-background" />
                  </div>
                </>
              )}
            </CardContent>
          </Card>
        </div>
      ) : null}
    </div>
  )
}
