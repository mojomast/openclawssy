import { useCallback, useEffect, useMemo, useState } from "react"
import { useLocation, useNavigate } from "react-router-dom"
import { createApiClient, type ApiClient } from "@/lib/api"

type JsonRecord = Record<string, any>

type CategoryKey =
  | "general"
  | "model"
  | "chat"
  | "agents"
  | "memory"
  | "sandbox"
  | "network"
  | "scheduler"
  | "capabilities"
  | "advanced"

type DiffRow = {
  path: string
  beforeValue: unknown
  afterValue: unknown
}

type CategoryDefinition = {
  key: CategoryKey
  title: string
  summary: string
  paths: string[]
}

const UNSAVED_SETTINGS_MESSAGE =
  "You have unsaved settings changes. Click OK to discard and navigate away, or Cancel to stay and save."

const CATEGORY_DEFINITIONS: CategoryDefinition[] = [
  { key: "general", title: "General", summary: "Server, workspace, and output defaults.", paths: ["server", "workspace", "output", "engine"] },
  { key: "model", title: "Model Provider", summary: "Model selection and provider endpoint settings.", paths: ["model", "providers"] },
  { key: "chat", title: "Chat / Discord / Telegram", summary: "Chat and bridge routing, limits, and allowlists.", paths: ["chat", "discord", "telegram"] },
	{ key: "agents", title: "Agents", summary: "Agent profiles and delegation defaults.", paths: ["agents"] },
  { key: "memory", title: "Memory", summary: "Memory retention, embeddings, and checkpoint behavior.", paths: ["memory"] },
  { key: "sandbox", title: "Sandbox/Shell", summary: "Sandbox provider and shell execution controls.", paths: ["sandbox", "shell"] },
  { key: "network", title: "Network", summary: "Network policy and allowed domains.", paths: ["network"] },
  { key: "scheduler", title: "Scheduler", summary: "Scheduler catch-up and concurrency controls.", paths: ["scheduler"] },
  { key: "capabilities", title: "Capabilities", summary: "Feature flags and derived capability visibility.", paths: ["capabilities"] },
  { key: "advanced", title: "Advanced", summary: "Raw JSON editor and full config diff.", paths: [] },
]

const MODEL_PROVIDERS = ["openai", "openrouter", "requesty", "hatz", "zai", "generic"]

function deepClone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

function isRecord(value: unknown): value is JsonRecord {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value)
}

function readPath(record: JsonRecord | null, path: string, fallback: unknown = ""): any {
  if (!record) {
    return fallback
  }
  const parts = path.split(".")
  let current: any = record
  for (const part of parts) {
    if (!isRecord(current) && !Array.isArray(current)) {
      return fallback
    }
		const indexed = current as Record<string, unknown>
		current = indexed[part]
    if (current === undefined) {
      return fallback
    }
  }
  return current
}

function writePath(record: JsonRecord, path: string, nextValue: unknown): JsonRecord {
  const cloned = deepClone(record)
  const parts = path.split(".")
  let current: JsonRecord = cloned
  for (let index = 0; index < parts.length - 1; index += 1) {
    const part = parts[index]
    const existing = current[part]
    if (!isRecord(existing)) {
      current[part] = {}
    }
    current = current[part] as JsonRecord
  }
  current[parts[parts.length - 1]] = nextValue
  return cloned
}

function collectDiffRows(beforeValue: unknown, afterValue: unknown, prefix = ""): DiffRow[] {
  if (beforeValue === afterValue) {
    return []
  }

  const beforeIsRecord = isRecord(beforeValue)
  const afterIsRecord = isRecord(afterValue)
  if (!beforeIsRecord || !afterIsRecord) {
    return [{ path: prefix || "config", beforeValue, afterValue }]
  }

  const keys = new Set<string>([...Object.keys(beforeValue), ...Object.keys(afterValue)])
  const rows: DiffRow[] = []
  keys.forEach((key) => {
    const nextPrefix = prefix ? `${prefix}.${key}` : key
    rows.push(...collectDiffRows(beforeValue[key], afterValue[key], nextPrefix))
  })
  return rows
}

function formatValue(value: unknown): string {
  if (value === undefined) {
    return "undefined"
  }
  if (typeof value === "string") {
    return value
  }
  try {
    return JSON.stringify(value)
  } catch {
    return String(value)
  }
}

function normalizeCategory(value: string | null): CategoryKey | null {
  const key = String(value || "").trim().toLowerCase()
  if (!key) {
    return null
  }
  const match = CATEGORY_DEFINITIONS.find((item) => item.key === key)
  return match ? match.key : null
}

function routePathFromHref(href: string): string | null {
  try {
    const url = new URL(href, window.location.href)
    const hash = String(url.hash || "").replace(/^#/, "")
    if (!hash) {
      return null
    }
    const [rawPath] = hash.split("?")
    return rawPath.startsWith("/") ? rawPath : `/${rawPath}`
  } catch {
    return null
  }
}

function isSettingsRoute(path: string | null): boolean {
  return path === "/settings"
}

function parseNumber(input: string, fallback: number): number {
  const next = Number.parseInt(String(input), 10)
  return Number.isFinite(next) ? next : fallback
}

function parseFloatValue(input: string, fallback: number): number {
  const next = Number.parseFloat(String(input))
  return Number.isFinite(next) ? next : fallback
}

async function fetchConfig(client: ApiClient): Promise<JsonRecord> {
  const response = await client.get<JsonRecord>("/api/admin/config")
  return isRecord(response) ? response : {}
}

export function SettingsPage() {
  const location = useLocation()
  const navigate = useNavigate()
  const apiClient = useMemo(() => createApiClient(), [])

  const [config, setConfig] = useState<JsonRecord | null>(null)
  const [draft, setDraft] = useState<JsonRecord | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [loadError, setLoadError] = useState("")
  const [saveNotice, setSaveNotice] = useState("")
  const [saveError, setSaveError] = useState("")
  const [searchQuery, setSearchQuery] = useState("")
  const [activeCategory, setActiveCategory] = useState<CategoryKey>("general")
  const [selectedProfile, setSelectedProfile] = useState("default")
  const [providerMessages, setProviderMessages] = useState<Record<string, string>>({})
  const [providerModels, setProviderModels] = useState<Record<string, string[]>>({})
  const [rawEditorValue, setRawEditorValue] = useState("")
  const [rawEditorError, setRawEditorError] = useState("")

  const load = useCallback(async () => {
    setLoading(true)
    setLoadError("")
    setSaveNotice("")
    setSaveError("")
    try {
      const loaded = await fetchConfig(apiClient)
      const cloned = deepClone(loaded)
      setConfig(cloned)
      setDraft(cloned)
      setRawEditorValue(`${JSON.stringify(cloned, null, 2)}\n`)
      const profileKeys = Object.keys((loaded.agents?.profiles as JsonRecord) || {})
      if (profileKeys.length > 0 && !profileKeys.includes(selectedProfile)) {
        setSelectedProfile(profileKeys[0])
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to load config"
      setLoadError(message)
    } finally {
      setLoading(false)
    }
  }, [apiClient, selectedProfile])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    const params = new URLSearchParams(location.search)
    const fromCategory = normalizeCategory(params.get("category"))
    if (fromCategory) {
      setActiveCategory(fromCategory)
    }
    const fromProfile = String(params.get("profile") || "").trim()
    if (fromProfile) {
      setSelectedProfile(fromProfile)
    }
  }, [location.search])

  const filteredCategories = useMemo(() => {
    const query = searchQuery.trim().toLowerCase()
    if (!query) {
      return CATEGORY_DEFINITIONS
    }
    return CATEGORY_DEFINITIONS.filter((category) => {
      if (`${category.title} ${category.summary}`.toLowerCase().includes(query)) {
        return true
      }
      if (category.key === "advanced") {
        return JSON.stringify(draft || {}).toLowerCase().includes(query)
      }
      const snapshot = category.paths
        .map((path) => readPath(draft, path, null))
        .filter((value) => value !== null)
      return JSON.stringify(snapshot).toLowerCase().includes(query)
    })
  }, [draft, searchQuery])

  const activeCategoryDefinition = useMemo(
    () => filteredCategories.find((category) => category.key === activeCategory) || filteredCategories[0] || CATEGORY_DEFINITIONS[0],
    [activeCategory, filteredCategories]
  )

  useEffect(() => {
    if (activeCategoryDefinition.key !== activeCategory) {
      setActiveCategory(activeCategoryDefinition.key)
    }
  }, [activeCategory, activeCategoryDefinition])

  const diffRows = useMemo(() => {
    if (!config || !draft) {
      return []
    }
    return collectDiffRows(config, draft)
  }, [config, draft])

  const hasUnsavedChanges = diffRows.length > 0

  const syncSearchParams = useCallback(
    (updates: { category?: CategoryKey; profile?: string }) => {
      const params = new URLSearchParams(location.search)
      if (updates.category) {
        params.set("category", updates.category)
      }
      if (updates.profile) {
        params.set("profile", updates.profile)
      }
      const search = params.toString()
      navigate(
        {
          pathname: location.pathname,
          search: search ? `?${search}` : "",
        },
        { replace: true }
      )
    },
    [location.pathname, location.search, navigate]
  )

  const setDraftPath = useCallback((path: string, nextValue: unknown) => {
    setDraft((previous) => {
      const baseline = isRecord(previous) ? previous : {}
      return writePath(baseline, path, nextValue)
    })
    setSaveNotice("")
    setSaveError("")
  }, [])

  const withDraftRecord = draft || {}
  const providers = (readPath(withDraftRecord, "providers", {}) || {}) as JsonRecord
  const profileMap = (readPath(withDraftRecord, "agents.profiles", {}) || {}) as JsonRecord
  const profileKeys = Object.keys(profileMap)
  const selectedProfileSafe = profileKeys.includes(selectedProfile) ? selectedProfile : profileKeys[0] || "default"
  const selectedProvider = String(readPath(withDraftRecord, "model.provider", "hatz") || "hatz").trim() || "hatz"
  const selectedModelValue = String(readPath(withDraftRecord, "model.name", "") || "")
  const selectableModels = Array.from(new Set([selectedModelValue, ...(providerModels[selectedProvider] || [])].filter(Boolean)))

  const saveConfig = useCallback(async () => {
    if (!draft) {
      return
    }
    setSaving(true)
    setSaveNotice("")
    setSaveError("")
    try {
      await apiClient.patch("/api/admin/config", draft)
      const snapshot = deepClone(draft)
      setConfig(snapshot)
      setDraft(snapshot)
      setSaveNotice("Config saved.")
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to save config"
      setSaveError(message)
    } finally {
      setSaving(false)
    }
  }, [apiClient, draft])

  const reloadConfig = useCallback(async () => {
    await load()
  }, [load])

  const runProviderTest = useCallback(
    async (providerName: string) => {
      const baseURL = String(readPath(draft, `providers.${providerName}.base_url`, "") || "")
      try {
        const response = await apiClient.post<{ status_text?: string; message?: string }>("/api/admin/providers/test", {
          provider: providerName,
          base_url: baseURL,
        })
        setProviderMessages((previous) => ({
          ...previous,
          [providerName]: String(response.status_text || response.message || "Provider check complete."),
        }))
      } catch (error) {
        const message = error instanceof Error ? error.message : "Provider check failed"
        setProviderMessages((previous) => ({ ...previous, [providerName]: message }))
      }
    },
    [apiClient, draft]
  )

  const queryProviderModels = useCallback(
    async (providerName: string) => {
      try {
        const response = await apiClient.get<{ models?: string[] }>(`/api/admin/providers/models?provider=${encodeURIComponent(providerName)}`)
        const models = Array.isArray(response.models) ? response.models : []
        setProviderModels((previous) => ({ ...previous, [providerName]: models }))
        if (models.length > 0) {
          setProviderMessages((previous) => ({ ...previous, [providerName]: `${models.length} model(s) discovered.` }))
        }
      } catch (error) {
        const message = error instanceof Error ? error.message : "Model discovery failed"
        setProviderMessages((previous) => ({ ...previous, [providerName]: message }))
      }
    },
    [apiClient]
  )

  const applyRawJSON = useCallback(() => {
    try {
      const parsed = JSON.parse(rawEditorValue) as JsonRecord
      if (!isRecord(parsed)) {
        throw new Error("JSON payload must be an object")
      }
      const cloned = deepClone(parsed)
      setDraft(cloned)
      setRawEditorError("")
      setSaveNotice("")
      setSaveError("")
    } catch (error) {
      const message = error instanceof Error ? error.message : "Invalid JSON"
      setRawEditorError(`JSON parse error: ${message}`)
    }
  }, [rawEditorValue])

  useEffect(() => {
    const handleLinkNavigation = (event: MouseEvent) => {
      if (!hasUnsavedChanges) {
        return
      }

      const target = event.target instanceof Element ? event.target.closest("a[href]") : null
      if (!target) {
        return
      }

      const href = target.getAttribute("href")
      if (!href) {
        return
      }

      const destinationPath = routePathFromHref(href)
      if (!destinationPath || isSettingsRoute(destinationPath)) {
        return
      }

      if (!window.confirm(UNSAVED_SETTINGS_MESSAGE)) {
        event.preventDefault()
        event.stopPropagation()
      }
    }

    const handleBeforeUnload = (event: BeforeUnloadEvent) => {
      if (!hasUnsavedChanges) {
        return
      }
      event.preventDefault()
      event.returnValue = ""
    }

    document.addEventListener("click", handleLinkNavigation, true)
    window.addEventListener("beforeunload", handleBeforeUnload)

    return () => {
      document.removeEventListener("click", handleLinkNavigation, true)
      window.removeEventListener("beforeunload", handleBeforeUnload)
    }
  }, [hasUnsavedChanges])

  useEffect(() => {
    if (!draft) {
      return
    }
    setRawEditorValue(`${JSON.stringify(draft, null, 2)}\n`)
  }, [draft])

  if (loading) {
    return (
      <div className="p-6" data-testid="settings-page">
        <h2 className="text-2xl font-semibold mb-4">Settings</h2>
        <p>Loading settings…</p>
      </div>
    )
  }

  return (
    <div className="p-6" data-testid="settings-page">
      <h2 className="text-2xl font-semibold mb-4">Settings</h2>

      {loadError && <p className="settings-save-error">{loadError}</p>}

      <section className="settings-toolbar">
        <p className="settings-breadcrumbs">Control Plane / Settings / {activeCategoryDefinition.title}</p>
        <div className="settings-search">
          <label className="settings-search-label" htmlFor="settings-search-input">
            Search
          </label>
          <input
            id="settings-search-input"
            className="settings-input"
            placeholder="Search categories, fields, or values"
            value={searchQuery}
            onChange={(event) => setSearchQuery(event.target.value)}
          />
        </div>
        <div className="settings-toolbar-actions">
          <button type="button" onClick={() => void saveConfig()} disabled={saving || !draft}>
            {saving ? "Saving..." : "Save Config"}
          </button>
          <button type="button" className="secondary" onClick={() => void reloadConfig()}>
            Reload
          </button>
        </div>
      </section>

      {saveNotice && <p className="settings-save-success">{saveNotice}</p>}
      {saveError && <p className="settings-save-error">{saveError}</p>}

      <div className="settings-workspace">
        <aside className="settings-categories">
          {filteredCategories.map((category) => (
            <button
              type="button"
              key={category.key}
              className={`settings-category-button ${activeCategoryDefinition.key === category.key ? "active" : ""}`}
              onClick={() => {
                setActiveCategory(category.key)
                syncSearchParams({ category: category.key })
              }}
            >
              <strong>{category.title}</strong>
              <span>{category.summary}</span>
            </button>
          ))}
        </aside>

        <section className="settings-category-content">
          <h3>{activeCategoryDefinition.title}</h3>
          <p className="settings-help">{activeCategoryDefinition.summary}</p>

          <div className="settings-panel">
            {activeCategoryDefinition.key === "general" && (
              <>
                <label className="settings-field">
                  <span className="settings-field-title">Bind address</span>
                  <input
                    className="settings-input"
                    value={String(readPath(draft, "server.bind_address", ""))}
                    onChange={(event) => setDraftPath("server.bind_address", event.target.value)}
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Server port</span>
                  <input
                    className="settings-input"
                    value={String(readPath(draft, "server.port", 0))}
                    onChange={(event) => setDraftPath("server.port", parseNumber(event.target.value, 0))}
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Workspace root</span>
                  <input
                    className="settings-input"
                    value={String(readPath(draft, "workspace.root", ""))}
                    onChange={(event) => setDraftPath("workspace.root", event.target.value)}
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Thinking mode</span>
                  <select
                    className="settings-select"
                    value={String(readPath(draft, "output.thinking_mode", "on_error"))}
                    onChange={(event) => setDraftPath("output.thinking_mode", event.target.value)}
                  >
                    <option value="never">never</option>
                    <option value="on_error">on_error</option>
                    <option value="always">always</option>
                  </select>
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Engine max concurrent runs</span>
                  <input
                    className="settings-input"
                    value={String(readPath(draft, "engine.max_concurrent_runs", 0))}
                    onChange={(event) => setDraftPath("engine.max_concurrent_runs", parseNumber(event.target.value, 0))}
                  />
                </label>
              </>
            )}

            {activeCategoryDefinition.key === "model" && (
              <>
                <label className="settings-field">
                  <span className="settings-field-title">Provider</span>
                  <select
                    className="settings-select"
                    value={selectedProvider}
                    onChange={(event) => setDraftPath("model.provider", event.target.value)}
                  >
                    {MODEL_PROVIDERS.map((provider) => (
                      <option key={provider} value={provider}>
                        {provider}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Model name</span>
                  <select
                    className="settings-select"
                    value={selectedModelValue}
                    onChange={(event) => setDraftPath("model.name", event.target.value)}
                  >
                    {selectableModels.map((modelName) => (
                      <option key={modelName} value={modelName}>
                        {modelName}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Temperature</span>
                  <input
                    className="settings-input"
                    value={String(readPath(draft, "model.temperature", 0))}
                    onChange={(event) => setDraftPath("model.temperature", parseFloatValue(event.target.value, 0))}
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Max tokens</span>
                  <input
                    className="settings-input"
                    value={String(readPath(draft, "model.max_tokens", 0))}
                    onChange={(event) => setDraftPath("model.max_tokens", parseNumber(event.target.value, 0))}
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Timeout ms</span>
                  <input
                    className="settings-input"
                    value={String(readPath(draft, "model.timeout_ms", 0))}
                    onChange={(event) => setDraftPath("model.timeout_ms", parseNumber(event.target.value, 0))}
                  />
                </label>

                {MODEL_PROVIDERS.map((providerName) => (
                  <section className="settings-section settings-field" key={providerName}>
                    <h5 className="settings-subheading">{providerName}</h5>
                    <label className="settings-field">
                      <span className="settings-field-title">Base URL</span>
                      <input
                        className="settings-input"
                        value={String(readPath(providers as JsonRecord, `${providerName}.base_url`, ""))}
                        onChange={(event) => setDraftPath(`providers.${providerName}.base_url`, event.target.value)}
                      />
                    </label>
                    <label className="settings-field">
                      <span className="settings-field-title">API key env</span>
                      <input
                        className="settings-input"
                        value={String(readPath(providers as JsonRecord, `${providerName}.api_key_env`, ""))}
                        onChange={(event) => setDraftPath(`providers.${providerName}.api_key_env`, event.target.value)}
                      />
                    </label>
                    <div className="settings-toolbar-actions">
                      <button type="button" onClick={() => void runProviderTest(providerName)}>
                        Test provider
                      </button>
                      <button type="button" className="secondary" onClick={() => void queryProviderModels(providerName)}>
                        Query models
                      </button>
                    </div>
                    {providerMessages[providerName] && <p className="settings-help">{providerMessages[providerName]}</p>}
                    {(providerModels[providerName] || []).length > 0 && (
                      <div className="settings-toolbar-actions">
                        {(providerModels[providerName] || []).map((modelName) => (
                          <button
                            key={modelName}
                            type="button"
                            className="secondary"
                            onClick={() => setDraftPath("model.name", modelName)}
                          >
                            {modelName}
                          </button>
                        ))}
                      </div>
                    )}
                  </section>
                ))}
              </>
            )}

            {activeCategoryDefinition.key === "chat" && (
              <>
                <label className="settings-field">
                  <span className="settings-field-title">Chat enabled</span>
                  <input
                    type="checkbox"
                    checked={Boolean(readPath(draft, "chat.enabled", false))}
                    onChange={(event) => setDraftPath("chat.enabled", event.target.checked)}
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Chat default agent</span>
                  <input
                    className="settings-input"
                    value={String(readPath(draft, "chat.default_agent_id", ""))}
                    onChange={(event) => setDraftPath("chat.default_agent_id", event.target.value)}
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Discord enabled</span>
                  <input
                    type="checkbox"
                    checked={Boolean(readPath(draft, "discord.enabled", false))}
                    onChange={(event) => setDraftPath("discord.enabled", event.target.checked)}
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Telegram enabled</span>
                  <input
                    type="checkbox"
                    checked={Boolean(readPath(draft, "telegram.enabled", false))}
                    onChange={(event) => setDraftPath("telegram.enabled", event.target.checked)}
                  />
                </label>
              </>
            )}

            {activeCategoryDefinition.key === "agents" && (
              <>
                <h4 className="settings-subheading">Agent profile summary</h4>
                <div className="settings-diff-summary">
                  <table className="settings-diff-table">
                    <thead>
                      <tr>
                        <th>Agent</th>
                        <th>Enabled</th>
                        <th>Self-improvement</th>
                      </tr>
                    </thead>
                    <tbody>
                      {profileKeys.map((profileID) => (
                        <tr key={profileID}>
                          <td>
                            <code>{profileID}</code>
                          </td>
                          <td>{String(readPath(profileMap, `${profileID}.enabled`, true))}</td>
                          <td>{String(readPath(profileMap, `${profileID}.self_improvement`, false))}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>

                <h4 className="settings-subheading">Agent Profile Editor</h4>
                <label className="settings-field">
                  <span className="settings-field-title">Profile agent</span>
                  <select
                    className="settings-select"
                    value={selectedProfileSafe}
                    onChange={(event) => {
                      setSelectedProfile(event.target.value)
                      syncSearchParams({ profile: event.target.value })
                    }}
                  >
                    {profileKeys.map((profileID) => (
                      <option key={profileID} value={profileID}>
                        {profileID}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Profile enabled</span>
                  <input
                    type="checkbox"
                    checked={Boolean(readPath(profileMap, `${selectedProfileSafe}.enabled`, true))}
                    onChange={(event) => setDraftPath(`agents.profiles.${selectedProfileSafe}.enabled`, event.target.checked)}
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Profile self improvement</span>
                  <input
                    type="checkbox"
                    checked={Boolean(readPath(profileMap, `${selectedProfileSafe}.self_improvement`, false))}
                    onChange={(event) => setDraftPath(`agents.profiles.${selectedProfileSafe}.self_improvement`, event.target.checked)}
                  />
                </label>

                <h4 className="settings-subheading">Subagent defaults</h4>
                <label className="settings-field">
                  <span className="settings-field-title">Subagent timeout ms</span>
                  <input
                    className="settings-input"
                    value={String(readPath(draft, "agents.subagent_defaults.timeout_ms", 0))}
                    onChange={(event) => setDraftPath("agents.subagent_defaults.timeout_ms", parseNumber(event.target.value, 0))}
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Subagent max tool iterations</span>
                  <input
                    className="settings-input"
                    value={String(readPath(draft, "agents.subagent_defaults.max_tool_iterations", 0))}
                    onChange={(event) => setDraftPath("agents.subagent_defaults.max_tool_iterations", parseNumber(event.target.value, 0))}
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Delegation mode</span>
                  <select
                    className="settings-select"
                    value={String(readPath(draft, "agents.subagent_defaults.delegation_mode", "tool_gated"))}
                    onChange={(event) => setDraftPath("agents.subagent_defaults.delegation_mode", event.target.value)}
                  >
                    <option value="prompt_only">prompt_only</option>
                    <option value="tool_gated">tool_gated</option>
                    <option value="auto_execute">auto_execute</option>
                  </select>
                </label>
              </>
            )}

            {activeCategoryDefinition.key === "memory" && (
              <>
                <label className="settings-field">
                  <span className="settings-field-title">Memory enabled</span>
                  <input
                    type="checkbox"
                    checked={Boolean(readPath(draft, "memory.enabled", false))}
                    onChange={(event) => setDraftPath("memory.enabled", event.target.checked)}
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Max working items</span>
                  <input
                    className="settings-input"
                    value={String(readPath(draft, "memory.max_working_items", 0))}
                    onChange={(event) => setDraftPath("memory.max_working_items", parseNumber(event.target.value, 0))}
                  />
                </label>
              </>
            )}

            {activeCategoryDefinition.key === "sandbox" && (
              <>
                <label className="settings-field">
                  <span className="settings-field-title">Sandbox active</span>
                  <input
                    type="checkbox"
                    checked={Boolean(readPath(draft, "sandbox.active", false))}
                    onChange={(event) => setDraftPath("sandbox.active", event.target.checked)}
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Sandbox provider</span>
                  <input
                    className="settings-input"
                    value={String(readPath(draft, "sandbox.provider", "none"))}
                    onChange={(event) => setDraftPath("sandbox.provider", event.target.value)}
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Shell execution enabled</span>
                  <input
                    type="checkbox"
                    checked={Boolean(readPath(draft, "shell.enable_exec", false))}
                    onChange={(event) => setDraftPath("shell.enable_exec", event.target.checked)}
                  />
                </label>
              </>
            )}

            {activeCategoryDefinition.key === "network" && (
              <>
                <label className="settings-field">
                  <span className="settings-field-title">Network enabled</span>
                  <input
                    type="checkbox"
                    checked={Boolean(readPath(draft, "network.enabled", false))}
                    onChange={(event) => setDraftPath("network.enabled", event.target.checked)}
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Allowed domains (comma separated)</span>
                  <input
                    className="settings-input"
                    value={String((readPath(draft, "network.allowed_domains", []) as string[]).join(", "))}
                    onChange={(event) =>
                      setDraftPath(
                        "network.allowed_domains",
                        event.target.value
                          .split(",")
                          .map((item) => item.trim())
                          .filter(Boolean)
                      )
                    }
                  />
                </label>
              </>
            )}

            {activeCategoryDefinition.key === "scheduler" && (
              <>
                <label className="settings-field">
                  <span className="settings-field-title">Catch up missed jobs</span>
                  <input
                    type="checkbox"
                    checked={Boolean(readPath(draft, "scheduler.catch_up", false))}
                    onChange={(event) => setDraftPath("scheduler.catch_up", event.target.checked)}
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Max concurrent jobs</span>
                  <input
                    className="settings-input"
                    value={String(readPath(draft, "scheduler.max_concurrent_jobs", 0))}
                    onChange={(event) => setDraftPath("scheduler.max_concurrent_jobs", parseNumber(event.target.value, 0))}
                  />
                </label>
              </>
            )}

            {activeCategoryDefinition.key === "capabilities" && (
              <div className="settings-capabilities-list">
                {Object.entries((readPath(draft, "capabilities", {}) || {}) as JsonRecord).map(([name, enabled]) => (
                  <div className={`settings-capability-card ${enabled ? "enabled" : "disabled"}`} key={name}>
                    <p className="settings-capability-title">{name}</p>
                    <p className="settings-help">{enabled ? "Enabled" : "Disabled"}</p>
                  </div>
                ))}
                {Object.keys((readPath(draft, "capabilities", {}) || {}) as JsonRecord).length === 0 && (
                  <p className="settings-help">No capabilities are currently defined.</p>
                )}
              </div>
            )}

            {activeCategoryDefinition.key === "advanced" && (
              <>
                <textarea
                  className="settings-raw-editor"
                  value={rawEditorValue}
                  onChange={(event) => setRawEditorValue(event.target.value)}
                />
                <div className="settings-advanced-actions">
                  <button type="button" onClick={applyRawJSON}>
                    Apply JSON to Draft
                  </button>
                </div>
                {rawEditorError && <p className="settings-inline-error">{rawEditorError}</p>}
              </>
            )}
          </div>

          <section className="settings-diff-section">
            <p className="settings-help">Diff before save ({diffRows.length} changed path{diffRows.length === 1 ? "" : "s"})</p>
            <div className="settings-diff-summary">
              <table className="settings-diff-table">
                <thead>
                  <tr>
                    <th>Path</th>
                    <th>Before</th>
                    <th>After</th>
                  </tr>
                </thead>
                <tbody>
                  {diffRows.map((row) => (
                    <tr key={row.path}>
                      <td>
                        <code>{row.path}</code>
                      </td>
                      <td>{formatValue(row.beforeValue)}</td>
                      <td>{formatValue(row.afterValue)}</td>
                    </tr>
                  ))}
                  {diffRows.length === 0 && (
                    <tr>
                      <td colSpan={3}>No pending changes.</td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </section>
        </section>
      </div>
    </div>
  )
}
