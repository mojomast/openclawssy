import { ChangeEvent, useCallback, useEffect, useMemo, useState } from "react"
import { Button } from "@/components/ui/button"
import { useControlPlaneFeatures } from "@/hooks/useControlPlaneFeatures"
import { api } from "@/lib/api"

type SkillsResponse = {
  agent_id?: unknown
  available_agents?: unknown
  installable?: unknown
  installed_skills?: unknown
  activated_skills?: unknown
}

type InstallableSkill = {
  name: string
  installed: boolean
}

function asText(value: unknown): string {
  if (value === null || value === undefined) {
    return ""
  }
  return String(value)
}

function normalizeName(value: unknown): string {
  return asText(value).trim().toLowerCase()
}

function normalizeNames(values: unknown): string[] {
  if (!Array.isArray(values)) {
    return []
  }
  return Array.from(new Set(values.map((item) => normalizeName(item)).filter(Boolean))).sort((a, b) => a.localeCompare(b))
}

function normalizeInstallable(values: unknown): InstallableSkill[] {
  if (!Array.isArray(values)) {
    return []
  }

  const list = values
    .filter((item): item is Record<string, unknown> => Boolean(item) && typeof item === "object")
    .map((item) => ({
      name: normalizeName(item.name),
      installed: Boolean(item.installed),
    }))
    .filter((item) => item.name.length > 0)

  const byName = new Map<string, InstallableSkill>()
  for (const item of list) {
    byName.set(item.name, item)
  }

  return Array.from(byName.values()).sort((a, b) => a.name.localeCompare(b.name))
}

export function SkillsPage() {
  const { features, loading: featuresLoading } = useControlPlaneFeatures()
  const [loading, setLoading] = useState(true)
  const [actionPending, setActionPending] = useState(false)
  const [agentID, setAgentID] = useState("default")
  const [availableAgents, setAvailableAgents] = useState<string[]>(["default"])
  const [installable, setInstallable] = useState<InstallableSkill[]>([])
  const [installedSkills, setInstalledSkills] = useState<string[]>([])
  const [activatedSkills, setActivatedSkills] = useState<string[]>([])
  const [statusText, setStatusText] = useState("")
  const [statusKind, setStatusKind] = useState<"" | "success" | "error">("")
  const featureDisabled = !featuresLoading && !features.instanceAgents

  const setStatus = useCallback((text: string, kind: "" | "success" | "error" = "") => {
    setStatusText(text)
    setStatusKind(kind)
  }, [])

  const loadSkills = useCallback(async (
    requestedAgent: string,
    options?: { keepStatus?: boolean; preserveStatusOnSuccess?: boolean },
  ) => {
    if (featureDisabled) {
      setLoading(false)
      return
    }
    const keepStatus = Boolean(options?.keepStatus)
    const preserveStatusOnSuccess = Boolean(options?.preserveStatusOnSuccess)
    const targetAgent = normalizeName(requestedAgent) || "default"

    setLoading(true)
    if (!keepStatus) {
      setStatus("Loading skills...", "")
    }

    try {
      const payload = await api.get<SkillsResponse>(`/api/admin/skills?agent_id=${encodeURIComponent(targetAgent)}`)
      const nextAgent = normalizeName(payload.agent_id) || targetAgent
      const nextAvailable = normalizeNames(payload.available_agents)
      const nextInstalled = normalizeNames(payload.installed_skills)
      const nextActivated = normalizeNames(payload.activated_skills).filter((name) => nextInstalled.includes(name))

      const nextInstallable = normalizeInstallable(payload.installable).map((item) => ({
        ...item,
        installed: item.installed || nextInstalled.includes(item.name),
      }))

      const allAgents = nextAvailable.length ? nextAvailable : [nextAgent]
      if (!allAgents.includes(nextAgent)) {
        allAgents.push(nextAgent)
      }

      setAgentID(nextAgent)
      setAvailableAgents(allAgents.sort((a, b) => a.localeCompare(b)))
      setInstallable(nextInstallable)
      setInstalledSkills(nextInstalled)
      setActivatedSkills(nextActivated)

      if (!preserveStatusOnSuccess) {
        setStatus("Skills loaded.", "success")
      }
    } catch (error) {
      setStatus(`Failed to load skills: ${error instanceof Error ? error.message : String(error)}`, "error")
    } finally {
      setLoading(false)
    }
  }, [featureDisabled, setStatus])

  useEffect(() => {
    if (featuresLoading) {
      return
    }
    if (featureDisabled) {
      setLoading(false)
      setActionPending(false)
      setAgentID("default")
      setAvailableAgents(["default"])
      setInstallable([])
      setInstalledSkills([])
      setActivatedSkills([])
      setStatusText("")
      setStatusKind("")
      return
    }
    void loadSkills("default")
  }, [featureDisabled, featuresLoading, loadSkills])

  const isInstalled = useCallback((name: string) => installedSkills.includes(normalizeName(name)), [installedSkills])
  const isActivated = useCallback((name: string) => activatedSkills.includes(normalizeName(name)), [activatedSkills])

  const visibleAgents = useMemo(() => {
    const values = availableAgents.length ? availableAgents : [agentID]
    if (!values.includes(agentID)) {
      return [...values, agentID].sort((a, b) => a.localeCompare(b))
    }
    return values
  }, [agentID, availableAgents])

  const handleAgentChange = useCallback((event: ChangeEvent<HTMLSelectElement>) => {
    if (featureDisabled) {
      return
    }
    const selected = normalizeName(event.target.value) || "default"
    setAgentID(selected)
    void loadSkills(selected)
  }, [featureDisabled, loadSkills])

  const handleReload = useCallback(() => {
    if (featureDisabled) {
      return
    }
    void loadSkills(agentID)
  }, [agentID, featureDisabled, loadSkills])

  const installSkill = useCallback(async (name: string) => {
    if (featureDisabled) {
      return
    }
    const skill = normalizeName(name)
    if (!skill) {
      return
    }

    setActionPending(true)
    setStatus(`Installing ${skill}...`, "")

    try {
      await api.post("/api/admin/skills", {
        action: "install",
        name: skill,
        agent_id: agentID,
      })
      setStatus(`Installed ${skill}.`, "success")
      await loadSkills(agentID, { keepStatus: true, preserveStatusOnSuccess: true })
    } catch (error) {
      setStatus(`Failed to install ${skill}: ${error instanceof Error ? error.message : String(error)}`, "error")
    } finally {
      setActionPending(false)
    }
  }, [agentID, featureDisabled, loadSkills, setStatus])

  const setActivation = useCallback(async (name: string, enabled: boolean) => {
    if (featureDisabled) {
      return
    }
    const skill = normalizeName(name)
    if (!skill) {
      return
    }

    setActionPending(true)
    setStatus(`${enabled ? "Activating" : "Deactivating"} ${skill} for ${agentID}...`, "")

    try {
      await api.post("/api/admin/skills", {
        action: enabled ? "activate" : "deactivate",
        name: skill,
        agent_id: agentID,
      })
      setStatus(`${enabled ? "Activated" : "Deactivated"} ${skill} for ${agentID}.`, "success")
      await loadSkills(agentID, { keepStatus: true, preserveStatusOnSuccess: true })
    } catch (error) {
      setStatus(`Failed to update ${skill}: ${error instanceof Error ? error.message : String(error)}`, "error")
    } finally {
      setActionPending(false)
    }
  }, [agentID, featureDisabled, loadSkills, setStatus])

  return (
    <div className="space-y-4 p-6" data-testid="skills-page">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Skills</h2>
        <p className="text-sm text-muted-foreground">
          Install built-in skills into workspace/skills and activate them per agent.
        </p>
      </div>

      {featureDisabled ? (
        <div className="rounded-lg border bg-card p-4">
          <div className="rounded-md border border-border bg-muted/30 p-4" data-testid="skills-disabled-state">
            <p className="text-sm font-medium">Skills disabled</p>
            <p className="mt-1 text-sm text-muted-foreground">
              Instance agent controls are disabled for this control plane.
            </p>
          </div>
        </div>
      ) : null}

      <section className="space-y-4 rounded-lg border bg-card p-4">
        <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_auto] md:items-end">
          <label htmlFor="skills-agent" className="space-y-1 text-sm">
            <span>Agent</span>
            <select
              id="skills-agent"
              className="h-10 w-full rounded-md border bg-background px-3 text-sm"
              value={agentID}
              disabled={featureDisabled || loading || actionPending}
              onChange={handleAgentChange}
            >
              {visibleAgents.map((item) => (
                <option key={item} value={item}>
                  {item}
                </option>
              ))}
            </select>
          </label>

          <div className="flex justify-start md:justify-end">
            <Button type="button" variant="outline" disabled={featureDisabled || loading || actionPending} onClick={handleReload}>
              {loading ? "Reloading..." : "Reload"}
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

        <section className="space-y-3 rounded-md border bg-muted/20 p-4">
          <h3 className="text-lg font-semibold">Installable Skills</h3>
          {installable.length === 0 ? (
            <p className="text-sm text-muted-foreground">No installable skills found.</p>
          ) : (
            <div className="space-y-2">
              {installable.map((item) => (
                <article key={item.name} className="rounded-md border bg-background p-3">
                  <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                    <div>
                      <p className="text-sm font-medium">{item.name}</p>
                      <p className="text-xs text-muted-foreground">
                        {item.installed ? "Installed in workspace/skills." : "Not installed."}
                      </p>
                    </div>

                    <Button
                      type="button"
                      variant={item.installed ? "outline" : "default"}
                      size="sm"
                      disabled={featureDisabled || loading || actionPending || item.installed}
                      onClick={() => {
                        void installSkill(item.name)
                      }}
                    >
                      {item.installed ? "Installed" : "Install"}
                    </Button>
                  </div>
                </article>
              ))}
            </div>
          )}
        </section>

        <section className="space-y-3 rounded-md border bg-muted/20 p-4">
          <h3 className="text-lg font-semibold">Agent Activation</h3>
          <p className="text-sm text-muted-foreground">
            Activation appends an Activated Skills block to TOOLS.md so the selected agent can load skills with skill.read.
          </p>

          {installedSkills.length === 0 ? (
            <p className="text-sm text-muted-foreground">Install a skill first to activate it for an agent.</p>
          ) : (
            <div className="space-y-2">
              {installedSkills.map((name) => {
                const active = isActivated(name)
                return (
                  <article key={name} className="rounded-md border bg-background p-3">
                    <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                      <div>
                        <p className="text-sm font-medium">{name}</p>
                        <p className="text-xs text-muted-foreground">
                          {active ? `Active for ${agentID}.` : `Not active for ${agentID}.`}
                        </p>
                      </div>

                      <Button
                        type="button"
                        variant={active ? "outline" : "default"}
                        size="sm"
                        disabled={featureDisabled || loading || actionPending || !isInstalled(name)}
                        onClick={() => {
                          void setActivation(name, !active)
                        }}
                      >
                        {active ? "Deactivate" : "Activate"}
                      </Button>
                    </div>
                  </article>
                )
              })}
            </div>
          )}
        </section>
      </section>
    </div>
  )
}
