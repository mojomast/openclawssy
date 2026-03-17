import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useLocation, useNavigate } from "react-router-dom"
import { createApiClient, type ApiClient } from "@/lib/api"
import { Button } from "@/components/ui/button"

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

type PendingNavigation = {
  pathname: string
  search: string
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
const SANDBOX_PROVIDERS = ["none", "local", "docker"]

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

function deletePath(record: JsonRecord, path: string): JsonRecord {
  const cloned = deepClone(record)
  const parts = path.split(".")
  let current: JsonRecord = cloned
  for (let index = 0; index < parts.length - 1; index += 1) {
    const part = parts[index]
    const existing = current[part]
    if (!isRecord(existing)) {
      return cloned
    }
    current = existing
  }
  delete current[parts[parts.length - 1]]
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

function isSettingsRoute(path: string | null): boolean {
  return path === "/settings"
}

function routeTargetFromHash(hashValue: string): PendingNavigation | null {
  const hash = String(hashValue || "").replace(/^#/, "")
  if (!hash) {
    return null
  }
  const [rawPath, rawSearch = ""] = hash.split("?")
  return {
    pathname: rawPath.startsWith("/") ? rawPath : `/${rawPath}`,
    search: rawSearch ? `?${rawSearch}` : "",
  }
}

function routeTargetFromHref(hrefValue: string): PendingNavigation | null {
  try {
    const url = new URL(hrefValue, window.location.href)
    return routeTargetFromHash(url.hash)
  } catch {
    return null
  }
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

async function fetchStatus(client: ApiClient): Promise<JsonRecord> {
  const response = await client.get<JsonRecord>("/api/admin/status")
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
  const [pendingNavigation, setPendingNavigation] = useState<PendingNavigation | null>(null)
  const [unsavedDialogOpen, setUnsavedDialogOpen] = useState(false)
  const dirtyRef = useRef(false)
  const allowNextNavigationRef = useRef(false)
  const restoringHashRef = useRef(false)
  const settingsRouteRef = useRef("/settings")

  const load = useCallback(async () => {
    setLoading(true)
    setLoadError("")
    setSaveNotice("")
    setSaveError("")
    try {
      const loaded = await fetchConfig(apiClient)
      const status = await fetchStatus(apiClient)
      const cloned = deepClone(loaded)
      const runtime = isRecord(status.runtime) ? status.runtime : null
      const runtimeServer = isRecord(runtime?.server) ? runtime.server : null
      const runtimeWorkspace = isRecord(runtime?.workspace) ? runtime.workspace : null
      const runtimeSandbox = isRecord(runtime?.sandbox) ? runtime.sandbox : null
      const runtimeShell = isRecord(runtime?.shell) ? runtime.shell : null
      const runtimeOutput = isRecord(runtime?.output) ? runtime.output : null
      const runtimeEngine = isRecord(runtime?.engine) ? runtime.engine : null
      if (runtimeServer) {
        cloned.server = {
          ...(isRecord(cloned.server) ? cloned.server : {}),
          bind_address: readPath(runtimeServer, "bind_address", readPath(cloned, "server.bind_address", "")),
          port: readPath(runtimeServer, "port", readPath(cloned, "server.port", 0)),
        }
      }
      if (runtimeWorkspace) {
        cloned.workspace = {
          ...(isRecord(cloned.workspace) ? cloned.workspace : {}),
          root: readPath(runtimeWorkspace, "root", readPath(cloned, "workspace.root", "")),
        }
      }
      if (runtimeSandbox) {
        cloned.sandbox = {
          ...(isRecord(cloned.sandbox) ? cloned.sandbox : {}),
          active: readPath(runtimeSandbox, "active", readPath(cloned, "sandbox.active", false)),
          provider: readPath(runtimeSandbox, "provider", readPath(cloned, "sandbox.provider", "none")),
        }
      }
      if (runtimeShell) {
        cloned.shell = {
          ...(isRecord(cloned.shell) ? cloned.shell : {}),
          enable_exec: readPath(runtimeShell, "enable_exec", readPath(cloned, "shell.enable_exec", false)),
        }
      }
      if (runtimeOutput) {
        cloned.output = {
          ...(isRecord(cloned.output) ? cloned.output : {}),
          thinking_mode: readPath(runtimeOutput, "thinking_mode", readPath(cloned, "output.thinking_mode", "never")),
        }
      }
      if (runtimeEngine) {
        cloned.engine = {
          ...(isRecord(cloned.engine) ? cloned.engine : {}),
          max_concurrent_runs: readPath(runtimeEngine, "max_concurrent_runs", readPath(cloned, "engine.max_concurrent_runs", 0)),
        }
      }
      setConfig(cloned)
      setDraft(cloned)
      dirtyRef.current = false
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
    dirtyRef.current = true
    setDraft((previous) => {
      const baseline = isRecord(previous) ? previous : {}
      return writePath(baseline, path, nextValue)
    })
    setSaveNotice("")
    setSaveError("")
  }, [])

  const deleteDraftPath = useCallback((path: string) => {
    dirtyRef.current = true
    setDraft((previous) => {
      const baseline = isRecord(previous) ? previous : {}
      return deletePath(baseline, path)
    })
    setSaveNotice("")
    setSaveError("")
  }, [])

  const withDraftRecord = draft || {}
  const providers = (readPath(withDraftRecord, "providers", {}) || {}) as JsonRecord
  const profileMap = (readPath(withDraftRecord, "agents.profiles", {}) || {}) as JsonRecord
  const profileKeys = Object.keys(profileMap)
  const selectedProfileSafe = profileKeys.includes(selectedProfile) ? selectedProfile : profileKeys[0] || "default"
  const selectedProfileModel = (readPath(profileMap, `${selectedProfileSafe}.model`, {}) || {}) as JsonRecord
  const profileModelOverrideEnabled =
    String(readPath(profileMap, `${selectedProfileSafe}.model.provider`, "") || "").trim().length > 0 ||
    String(readPath(profileMap, `${selectedProfileSafe}.model.name`, "") || "").trim().length > 0 ||
    Number(readPath(profileMap, `${selectedProfileSafe}.model.max_tokens`, 0) || 0) > 0 ||
    Number(readPath(profileMap, `${selectedProfileSafe}.model.timeout_ms`, 0) || 0) > 0 ||
    Number(readPath(profileMap, `${selectedProfileSafe}.model.temperature`, 0) || 0) > 0
  const selectedProvider = String(readPath(withDraftRecord, "model.provider", "hatz") || "hatz").trim() || "hatz"
  const selectedModelValue = String(readPath(withDraftRecord, "model.name", "") || "")
  const selectableModels = Array.from(new Set([selectedModelValue, ...(providerModels[selectedProvider] || [])].filter(Boolean)))
  const savedSandboxActive = Boolean(readPath(config, "sandbox.active", false))
  const savedSandboxProvider = String(readPath(config, "sandbox.provider", "none") || "none")
  const savedShellExecEnabled = Boolean(readPath(config, "shell.enable_exec", false))
  const effectiveSandboxActive = Boolean(readPath(draft, "sandbox.active", false))
  const effectiveSandboxProvider = String(readPath(draft, "sandbox.provider", "none") || "none")
  const effectiveShellExecEnabled = Boolean(readPath(draft, "shell.enable_exec", false))
  const runtimeOverrideNotice =
    savedSandboxActive !== effectiveSandboxActive ||
    savedSandboxProvider !== effectiveSandboxProvider ||
    savedShellExecEnabled !== effectiveShellExecEnabled

  const saveConfig = useCallback(async (): Promise<boolean> => {
    if (!draft) {
      return false
    }
    setSaving(true)
    setSaveNotice("")
    setSaveError("")
    try {
      await apiClient.patch("/api/admin/config", draft)
      const snapshot = deepClone(draft)
      setConfig(snapshot)
      setDraft(snapshot)
      dirtyRef.current = false
      setSaveNotice("Config saved.")
      return true
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to save config"
      setSaveError(message)
      return false
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
      dirtyRef.current = true
      setRawEditorError("")
      setSaveNotice("")
      setSaveError("")
    } catch (error) {
      const message = error instanceof Error ? error.message : "Invalid JSON"
      setRawEditorError(`JSON parse error: ${message}`)
    }
  }, [rawEditorValue])

  const closeUnsavedDialog = useCallback(() => {
    setUnsavedDialogOpen(false)
    setPendingNavigation(null)
  }, [])

  const continueToPendingNavigation = useCallback(() => {
    if (!pendingNavigation) {
      return
    }
    allowNextNavigationRef.current = true
    navigate({ pathname: pendingNavigation.pathname, search: pendingNavigation.search })
    closeUnsavedDialog()
  }, [closeUnsavedDialog, navigate, pendingNavigation])

  const saveAndContinue = useCallback(async () => {
    if (!pendingNavigation) {
      return
    }
    const saved = await saveConfig()
    if (!saved) {
      return
    }
    continueToPendingNavigation()
  }, [continueToPendingNavigation, pendingNavigation, saveConfig])

  useEffect(() => {
    settingsRouteRef.current = `${location.pathname}${location.search}` || "/settings"
  }, [location.pathname, location.search])

  useEffect(() => {
    const handleDocumentClick = (event: MouseEvent) => {
      if (!dirtyRef.current) {
        return
      }

      const eventTarget = event.target
      const eventElement = eventTarget instanceof Element ? eventTarget : eventTarget instanceof Node ? eventTarget.parentElement : null
      const linkElement = eventElement ? eventElement.closest("a[href]") : null
      if (!linkElement) {
        return
      }

      const href = linkElement.getAttribute("href")
      if (!href) {
        return
      }

      const destination = routeTargetFromHref(href)
      if (!destination || isSettingsRoute(destination.pathname)) {
        return
      }

      event.preventDefault()
      event.stopPropagation()
      setPendingNavigation(destination)
      setUnsavedDialogOpen(true)
    }

    document.addEventListener("click", handleDocumentClick, true)
    return () => {
      document.removeEventListener("click", handleDocumentClick, true)
    }
  }, [])

  useEffect(() => {
    const handleHashChange = () => {
      if (restoringHashRef.current) {
        restoringHashRef.current = false
        return
      }
      if (allowNextNavigationRef.current) {
        allowNextNavigationRef.current = false
        return
      }
      if (!dirtyRef.current) {
        return
      }

      const destination = routeTargetFromHash(window.location.hash)
      if (!destination || isSettingsRoute(destination.pathname)) {
        return
      }

      setPendingNavigation(destination)
      setUnsavedDialogOpen(true)

      restoringHashRef.current = true
      window.location.hash = `#${settingsRouteRef.current}`
    }

    window.addEventListener("hashchange", handleHashChange)
    return () => {
      window.removeEventListener("hashchange", handleHashChange)
    }
  }, [])

  useEffect(() => {
    const handleBeforeUnload = (event: BeforeUnloadEvent) => {
      if (!dirtyRef.current) {
        return
      }
      event.preventDefault()
      event.returnValue = ""
    }

    window.addEventListener("beforeunload", handleBeforeUnload)

    return () => {
      window.removeEventListener("beforeunload", handleBeforeUnload)
    }
  }, [])

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

                <h4 className="settings-subheading">Profile model override</h4>
                <p className="settings-help">
                  {profileModelOverrideEnabled
                    ? "This profile overrides one or more global model fields."
                    : "This profile currently inherits global model values."}
                </p>
                <label className="settings-field">
                  <span className="settings-field-title">Profile model provider</span>
                  <select
                    className="settings-select"
                    value={String(readPath(selectedProfileModel, "provider", "") || "")}
                    onChange={(event) => setDraftPath(`agents.profiles.${selectedProfileSafe}.model.provider`, event.target.value)}
                  >
                    <option value="">(inherit global)</option>
                    {MODEL_PROVIDERS.map((provider) => (
                      <option key={provider} value={provider}>
                        {provider}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Profile model name</span>
                  <input
                    className="settings-input"
                    placeholder="(inherit global)"
                    value={String(readPath(selectedProfileModel, "name", "") || "")}
                    onChange={(event) => setDraftPath(`agents.profiles.${selectedProfileSafe}.model.name`, event.target.value)}
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Profile model max tokens</span>
                  <input
                    className="settings-input"
                    placeholder="0"
                    value={String(readPath(selectedProfileModel, "max_tokens", "") || "")}
                    onChange={(event) => setDraftPath(`agents.profiles.${selectedProfileSafe}.model.max_tokens`, parseNumber(event.target.value, 0))}
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Profile temperature</span>
                  <input
                    className="settings-input"
                    placeholder="(inherit global)"
                    value={String(readPath(selectedProfileModel, "temperature", "") || "")}
                    onChange={(event) =>
                      setDraftPath(`agents.profiles.${selectedProfileSafe}.model.temperature`, parseFloatValue(event.target.value, 0))
                    }
                  />
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Profile provider timeout (ms)</span>
                  <input
                    className="settings-input"
                    placeholder="0"
                    value={String(readPath(selectedProfileModel, "timeout_ms", "") || "")}
                    onChange={(event) => setDraftPath(`agents.profiles.${selectedProfileSafe}.model.timeout_ms`, parseNumber(event.target.value, 0))}
                  />
                </label>
                <div className="settings-toolbar-actions">
                  <button
                    type="button"
                    className="secondary"
                    onClick={() => deleteDraftPath(`agents.profiles.${selectedProfileSafe}.model`)}
                  >
                    Clear profile model overrides
                  </button>
                </div>

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
                <label className="settings-field">
                  <span className="settings-field-title">Subagent thinking mode</span>
                  <select
                    className="settings-select"
                    value={String(readPath(draft, "agents.subagent_defaults.thinking_mode", "on_error"))}
                    onChange={(event) => setDraftPath("agents.subagent_defaults.thinking_mode", event.target.value)}
                  >
                    <option value="never">never</option>
                    <option value="on_error">on_error</option>
                    <option value="always">always</option>
                  </select>
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Subagent allowed tools (comma separated)</span>
                  <input
                    className="settings-input"
                    value={String((readPath(draft, "agents.subagent_defaults.allowed_tools", []) as string[]).join(", "))}
                    onChange={(event) =>
                      setDraftPath(
                        "agents.subagent_defaults.allowed_tools",
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
                  <select
                    className="settings-input"
                    value={String(readPath(draft, "sandbox.provider", "none"))}
                    onChange={(event) => setDraftPath("sandbox.provider", event.target.value)}
                  >
                    {SANDBOX_PROVIDERS.map((provider) => (
                      <option key={provider} value={provider}>
                        {provider}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="settings-field">
                  <span className="settings-field-title">Shell execution enabled</span>
                  <input
                    type="checkbox"
                    checked={Boolean(readPath(draft, "shell.enable_exec", false))}
                    onChange={(event) => setDraftPath("shell.enable_exec", event.target.checked)}
                  />
                </label>
                <div className="settings-field settings-field--full">
                  <span className="settings-field-title">Effective runtime mode</span>
                  <p className="settings-help" data-testid="settings-runtime-sandbox-mode">
                    Current runtime is using `{effectiveSandboxProvider}` with sandbox {effectiveSandboxActive ? "enabled" : "disabled"} and shell execution {effectiveShellExecEnabled ? "enabled" : "disabled"}.
                  </p>
                  {runtimeOverrideNotice ? (
                    <p className="settings-help" data-testid="settings-runtime-sandbox-override">
                      Saved config differs from the current runtime, which usually means the server was started with CLI overrides or an older config and needs a restart to match the saved values.
                    </p>
                  ) : null}
                </div>
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

      {unsavedDialogOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
          <div className="absolute inset-0 bg-black/70" onClick={closeUnsavedDialog} />
          <section
            role="dialog"
            aria-modal="true"
            aria-labelledby="settings-unsaved-dialog-title"
            className="relative z-10 w-full max-w-lg rounded-lg border bg-background p-6 shadow-lg"
          >
            <h3 id="settings-unsaved-dialog-title" className="text-lg font-semibold leading-none tracking-tight">
              Unsaved settings changes
            </h3>
            <p className="mt-2 text-sm text-muted-foreground">{UNSAVED_SETTINGS_MESSAGE}</p>
            <div className="mt-6 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
              <Button type="button" variant="outline" onClick={closeUnsavedDialog}>
                Stay on Settings
              </Button>
              <Button type="button" variant="secondary" onClick={continueToPendingNavigation}>
                Discard changes
              </Button>
              <Button type="button" onClick={() => void saveAndContinue()} disabled={saving || !pendingNavigation}>
                {saving ? "Saving..." : "Save and Continue"}
              </Button>
            </div>
          </section>
        </div>
      )}
    </div>
  )
}
