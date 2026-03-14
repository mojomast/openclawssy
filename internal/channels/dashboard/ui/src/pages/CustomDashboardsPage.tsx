import { useCallback, useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from "react"
import { useNavigate } from "react-router-dom"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { ApiError, api } from "@/lib/api"

const LOCAL_STORAGE_SLOT = "dashboard.custom_dashboards.p1"
const GRID_COLUMNS = 12
const GRID_ROW_HEIGHT = 110
const AUTO_SAVE_DEBOUNCE_MS = 700

type WidgetState = Record<string, unknown>

type DashboardWidgetLayout = {
  widget_key: string
  widget_instance_id: string
  x: number
  y: number
  w: number
  h: number
  widget_state: WidgetState
}

type DashboardRecord = {
  id: string
  name: string
  position: number
  created_at: string
  updated_at: string
  layout: DashboardWidgetLayout[]
}

type WidgetSpec = {
  key: string
  label: string
  description: string
  sourcePath: string
  defaultW: number
  defaultH: number
  configure?: (currentState: WidgetState) => WidgetState | null
}

type WidgetRow = {
  label: string
  value: string
}

type WidgetDataResult = {
  rows: WidgetRow[]
  emptyMessage?: string
}

type DragMode = "move" | "resize"

type DragSession = {
  mode: DragMode
  widgetID: string
  startX: number
  startY: number
  gridRect: DOMRect
  originX: number
  originY: number
  originW: number
  originH: number
}

function nowISO(): string {
  return new Date().toISOString()
}

function uid(prefix: string): string {
  return `${prefix}_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`
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

function asNumber(value: unknown, fallback = 0): number {
  const parsed = Number(value)
  if (!Number.isFinite(parsed)) {
    return fallback
  }
  return parsed
}

function asBoolean(value: unknown): boolean {
  if (typeof value === "boolean") {
    return value
  }
  if (typeof value === "number") {
    return value !== 0
  }
  const normalized = asText(value).trim().toLowerCase()
  return normalized === "true" || normalized === "1" || normalized === "yes"
}

function extractErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (typeof error.details === "string" && error.details.trim().length > 0) {
      return error.details.trim()
    }
    const details = asRecord(error.details)
    const detailMessage = asText(details?.message).trim()
    if (detailMessage) {
      return detailMessage
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

function normalizeWidget(value: unknown): DashboardWidgetLayout | null {
  const raw = asRecord(value)
  if (!raw) {
    return null
  }

  const widgetKey = asText(raw.widget_key).trim()
  const widgetID = asText(raw.widget_instance_id).trim()
  if (!widgetKey || !widgetID) {
    return null
  }

  const widgetState = asRecord(raw.widget_state) ?? {}
  return {
    widget_key: widgetKey,
    widget_instance_id: widgetID,
    x: Math.max(0, asNumber(raw.x, 0)),
    y: Math.max(0, asNumber(raw.y, 0)),
    w: Math.max(1, Math.min(12, asNumber(raw.w, 4))),
    h: Math.max(1, Math.min(12, asNumber(raw.h, 3))),
    widget_state: { ...widgetState },
  }
}

function normalizeDashboard(value: unknown): DashboardRecord {
  const raw = asRecord(value)
  const layoutInput = Array.isArray(raw?.layout) ? raw?.layout : []
  return {
    id: asText(raw?.id).trim() || uid("dash"),
    name: asText(raw?.name).trim() || "New Dashboard",
    position: Math.max(0, asNumber(raw?.position, 0)),
    created_at: asText(raw?.created_at).trim() || nowISO(),
    updated_at: asText(raw?.updated_at).trim() || nowISO(),
    layout: layoutInput.map(normalizeWidget).filter((item): item is DashboardWidgetLayout => item !== null),
  }
}

function cloneWidget(widget: DashboardWidgetLayout): DashboardWidgetLayout {
  return {
    ...widget,
    widget_state: { ...widget.widget_state },
  }
}

function cloneDashboard(dashboard: DashboardRecord): DashboardRecord {
  return {
    ...dashboard,
    layout: dashboard.layout.map(cloneWidget),
  }
}

function mergeDashboards(localItems: DashboardRecord[], remoteItems: DashboardRecord[]): DashboardRecord[] {
  const merged = new Map<string, DashboardRecord>()
  ;[...remoteItems, ...localItems].forEach((item) => {
    const existing = merged.get(item.id)
    if (!existing || item.updated_at >= existing.updated_at) {
      merged.set(item.id, cloneDashboard(item))
    }
  })

  return Array.from(merged.values()).sort((left, right) => {
    if (left.position !== right.position) {
      return left.position - right.position
    }
    return left.created_at.localeCompare(right.created_at)
  })
}

function readLocalDashboards(): DashboardRecord[] {
  try {
    const raw = window.localStorage.getItem(LOCAL_STORAGE_SLOT)
    if (!raw) {
      return []
    }
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) {
      return []
    }
    return parsed.map(normalizeDashboard)
  } catch {
    return []
  }
}

function writeLocalDashboards(dashboards: DashboardRecord[]): void {
  try {
    window.localStorage.setItem(LOCAL_STORAGE_SLOT, JSON.stringify(dashboards))
  } catch {
    // ignore storage failures
  }
}

function createDefaultDashboard(): DashboardRecord {
  return normalizeDashboard({
    id: uid("dash"),
    name: "Main Dashboard",
    position: 0,
    layout: [],
  })
}

function createLimitConfigure(promptText: string): (state: WidgetState) => WidgetState | null {
  return (state) => {
    const current = Math.max(1, Math.min(20, asNumber(state.limit, 5)))
    const input = window.prompt(promptText, String(current))
    if (input === null) {
      return null
    }
    const parsed = Number(input)
    const next = Number.isFinite(parsed) ? parsed : current
    return {
      ...state,
      limit: Math.max(1, Math.min(20, Math.round(next))),
    }
  }
}

function createAgentConfigure(promptText: string): (state: WidgetState) => WidgetState | null {
  return (state) => {
    const current = asText(state.agent_id).trim() || "default"
    const input = window.prompt(promptText, current)
    if (input === null) {
      return null
    }
    return {
      ...state,
      agent_id: input.trim() || "default",
    }
  }
}

const WIDGETS: WidgetSpec[] = [
  {
    key: "runs.recent",
    label: "Runs: Recent",
    description: "Compact recent runs list.",
    sourcePath: "/runs",
    defaultW: 6,
    defaultH: 3,
    configure: createLimitConfigure("How many recent runs?"),
  },
  { key: "scheduler.jobs", label: "Scheduler: Jobs", description: "Compact scheduler job list.", sourcePath: "/scheduler", defaultW: 6, defaultH: 3 },
  { key: "runtime.status", label: "Runtime Status", description: "Provider, model, and run count.", sourcePath: "/chat", defaultW: 4, defaultH: 2 },
  { key: "runtime.overview", label: "Runtime: Overview", description: "Run count and channel enablement snapshot.", sourcePath: "/chat", defaultW: 4, defaultH: 3 },
  {
    key: "chat.quick_prompt",
    label: "Chat: Quick prompt",
    description: "Send a quick dashboard chat prompt.",
    sourcePath: "/chat",
    defaultW: 5,
    defaultH: 3,
    configure: createAgentConfigure("Agent id for quick prompt"),
  },
  { key: "secrets.summary", label: "Secrets: Presence summary", description: "Shows which secret keys exist.", sourcePath: "/secrets", defaultW: 4, defaultH: 3 },
  { key: "secrets.discord_token", label: "Secrets: Discord token", description: "Focused Discord/Telegram token presence widget.", sourcePath: "/secrets", defaultW: 4, defaultH: 2 },
  { key: "secrets.conventions", label: "Secrets: Key conventions", description: "Recommended secret naming patterns.", sourcePath: "/secrets", defaultW: 5, defaultH: 3 },
  { key: "settings.summary", label: "Settings: Model summary + Agent overrides summary", description: "Global model plus profile overview.", sourcePath: "/settings?category=model", defaultW: 6, defaultH: 3 },
  { key: "settings.providers", label: "Settings: Provider endpoints", description: "Base URLs and env references for providers.", sourcePath: "/settings?category=model", defaultW: 6, defaultH: 4 },
  { key: "settings.agents", label: "Settings: Agents snapshot", description: "Enabled agents and override count.", sourcePath: "/settings?category=agents", defaultW: 5, defaultH: 3 },
  { key: "settings.subagents", label: "Settings: Subagent defaults", description: "Subagent safety and delegation snapshot.", sourcePath: "/settings?category=agents", defaultW: 5, defaultH: 3 },
  { key: "settings.memory", label: "Settings: Memory summary", description: "Memory configuration overview.", sourcePath: "/settings?category=memory", defaultW: 4, defaultH: 3 },
  { key: "settings.network", label: "Settings: Network policy", description: "Allowed domains and shell status.", sourcePath: "/settings?category=network", defaultW: 4, defaultH: 3 },
  { key: "settings.scheduler", label: "Settings: Scheduler config", description: "Scheduler limits and catch-up policy.", sourcePath: "/settings?category=scheduler", defaultW: 4, defaultH: 2 },
  { key: "channels.status", label: "Discord/Telegram status", description: "Connector enabled flags and token presence.", sourcePath: "/settings?category=chat", defaultW: 4, defaultH: 2 },
  { key: "sessions.recent", label: "Sessions: Recent", description: "Recent chat sessions and activity.", sourcePath: "/sessions", defaultW: 6, defaultH: 3 },
  { key: "sessions.overview", label: "Sessions: Overview", description: "Total recent sessions and latest activity.", sourcePath: "/sessions", defaultW: 4, defaultH: 2 },
  { key: "skills.summary", label: "Skills: Summary", description: "Installed and activated skills by agent.", sourcePath: "/skills", defaultW: 4, defaultH: 3, configure: createAgentConfigure("Agent id for skills summary") },
  { key: "skills.active_list", label: "Skills: Active list", description: "Shows active skills for an agent.", sourcePath: "/skills", defaultW: 4, defaultH: 3, configure: createAgentConfigure("Agent id for active skills widget") },
  { key: "docs.summary", label: "Docs: Agent prompt docs", description: "Compact overview of agent doc files.", sourcePath: "/docs", defaultW: 4, defaultH: 3, configure: createAgentConfigure("Agent id for docs summary") },
  { key: "docs.doc_list", label: "Docs: Top files", description: "Lists key prompt/control docs for an agent.", sourcePath: "/docs", defaultW: 4, defaultH: 3, configure: createAgentConfigure("Agent id for doc list widget") },
  { key: "sandbox.summary", label: "Sandbox: Status", description: "Docker/local sandbox health and inventory.", sourcePath: "/sandbox", defaultW: 4, defaultH: 3, configure: createAgentConfigure("Agent id for sandbox summary") },
  { key: "sandbox.inventory", label: "Sandbox: Images & volumes", description: "Counts available sandbox images and volumes.", sourcePath: "/sandbox", defaultW: 4, defaultH: 2, configure: createAgentConfigure("Agent id for sandbox inventory widget") },
]

const WIDGET_MAP = new Map<string, WidgetSpec>(WIDGETS.map((widget) => [widget.key, widget]))

function createDefaultWidget(widgetKey: string): DashboardWidgetLayout {
  const spec = WIDGET_MAP.get(widgetKey)
  return {
    widget_key: widgetKey,
    widget_instance_id: uid("widget"),
    x: 0,
    y: 0,
    w: spec?.defaultW ?? 4,
    h: spec?.defaultH ?? 3,
    widget_state: {},
  }
}

async function fetchWidgetData(widgetKey: string, widgetState: WidgetState): Promise<WidgetDataResult> {
  const readAgentID = () => asText(widgetState.agent_id).trim() || "default"
  const readLimit = () => Math.max(1, Math.min(20, asNumber(widgetState.limit, 5)))

  if (widgetKey === "runs.recent") {
    const payload = await api.get<{ runs?: unknown }>(`/v1/runs?limit=${encodeURIComponent(String(readLimit()))}&offset=0`)
    const runs = Array.isArray(payload.runs) ? payload.runs : []
    const rows = runs.slice(0, readLimit()).map((run) => {
      const item = asRecord(run) ?? {}
      return {
        label: asText(item.id).trim() || "run",
        value: `${asText(item.status).trim() || "unknown"} · ${asText(item.updated_at).trim() || "-"}`,
      }
    })
    return { rows, emptyMessage: "No recent runs." }
  }

  if (widgetKey === "scheduler.jobs") {
    const payload = await api.get<{ jobs?: unknown }>("/api/admin/scheduler/jobs")
    const jobs = Array.isArray(payload.jobs) ? payload.jobs : []
    const rows = jobs.slice(0, 6).map((job) => {
      const item = asRecord(job) ?? {}
      const id = asText(item.id).trim() || "job"
      const schedule = asText(item.schedule).trim() || "(no schedule)"
      const enabled = asBoolean(item.enabled)
      return { label: id, value: `${schedule} · ${enabled ? "enabled" : "disabled"}` }
    })
    return { rows, emptyMessage: "No scheduler jobs." }
  }

  if (widgetKey === "runtime.status" || widgetKey === "runtime.overview") {
    const status = await api.get<Record<string, unknown>>("/api/admin/status")
    const rows: WidgetRow[] = [
      {
        label: "Model",
        value: `${asText(status.provider).trim() || "unknown"} / ${asText(status.model).trim() || "unknown"}`,
      },
      { label: "Runs", value: String(asNumber(status.run_count, 0)) },
      { label: "Discord", value: asBoolean(status.discord_enabled) ? "enabled" : "disabled" },
      { label: "Telegram", value: asBoolean(status.telegram_enabled) ? "enabled" : "disabled" },
    ]
    return { rows }
  }

  if (widgetKey === "secrets.summary") {
    const payload = await api.get<{ keys?: unknown }>("/api/admin/secrets")
    const keys = Array.isArray(payload.keys) ? payload.keys.map((item) => asText(item).trim()).filter(Boolean) : []
    return {
      rows: keys.slice(0, 8).map((key) => ({ label: key, value: "present" })),
      emptyMessage: "No secret keys stored.",
    }
  }

  if (widgetKey === "secrets.discord_token") {
    const payload = await api.get<{ keys?: unknown }>("/api/admin/secrets")
    const keys = Array.isArray(payload.keys) ? payload.keys.map((item) => asText(item).trim()) : []
    return {
      rows: [
        { label: "discord/bot_token", value: keys.includes("discord/bot_token") ? "present" : "missing" },
        { label: "telegram/bot_token", value: keys.includes("telegram/bot_token") ? "present" : "missing" },
      ],
    }
  }

  if (widgetKey === "secrets.conventions") {
    return {
      rows: [
        { label: "Discord", value: "discord/bot_token" },
        { label: "Telegram", value: "telegram/bot_token" },
        { label: "OpenAI", value: "OPENAI_API_KEY" },
        { label: "OpenRouter", value: "OPENROUTER_API_KEY" },
      ],
    }
  }

  if (widgetKey.startsWith("settings.")) {
    const cfg = await api.get<Record<string, unknown>>("/api/admin/config")
    const model = asRecord(cfg.model) ?? {}
    const agents = asRecord(cfg.agents) ?? {}
    const providers = asRecord(cfg.providers) ?? {}
    const memory = asRecord(cfg.memory) ?? {}
    const network = asRecord(cfg.network) ?? {}
    const shell = asRecord(cfg.shell) ?? {}
    const sandbox = asRecord(cfg.sandbox) ?? {}
    const scheduler = asRecord(cfg.scheduler) ?? {}

    if (widgetKey === "settings.summary") {
      const profiles = asRecord(agents.profiles)
      return {
        rows: [
          { label: "Model", value: `${asText(model.provider) || "unknown"} / ${asText(model.name) || "unknown"}` },
          { label: "Global max_tokens", value: String(asNumber(model.max_tokens, 0)) },
          { label: "Agent profiles", value: String(Object.keys(profiles ?? {}).length) },
        ],
      }
    }

    if (widgetKey === "settings.providers") {
      const providerNames = ["openai", "openrouter", "requesty", "zai", "generic"]
      return {
        rows: providerNames.map((name) => {
          const provider = asRecord(providers[name]) ?? {}
          const base = asText(provider.base_url).trim() || "(default)"
          const env = asText(provider.api_key_env).trim() || "(no env ref)"
          return { label: name, value: `${base} · ${env}` }
        }),
      }
    }

    if (widgetKey === "settings.agents") {
      const enabled = Array.isArray(agents.enabled_agent_ids) ? agents.enabled_agent_ids.length : 0
      const profiles = asRecord(agents.profiles)
      const profileEntries = Object.entries(profiles ?? {})
      const overrideCount = profileEntries.filter(([, value]) => {
        const profile = asRecord(value)
        const profileModel = asRecord(profile?.model)
        if (!profileModel) {
          return false
        }
        return Boolean(profileModel.provider || profileModel.name || profileModel.max_tokens || profileModel.temperature)
      }).length
      return {
        rows: [
          { label: "Enabled agent ids", value: String(enabled) },
          { label: "Profiles", value: String(profileEntries.length) },
          { label: "Model overrides", value: String(overrideCount) },
        ],
      }
    }

    if (widgetKey === "settings.subagents") {
      const defaults = asRecord(agents.subagent_defaults) ?? {}
      const allowedTools = Array.isArray(defaults.allowed_tools) ? defaults.allowed_tools.length : 0
      return {
        rows: [
          { label: "Thinking mode", value: asText(defaults.thinking_mode).trim() || "(default)" },
          { label: "Delegation mode", value: asText(defaults.delegation_mode).trim() || "(default)" },
          { label: "Timeout ms", value: String(asNumber(defaults.timeout_ms, 0)) },
          { label: "Allowed tools", value: String(allowedTools) },
        ],
      }
    }

    if (widgetKey === "settings.memory") {
      return {
        rows: [
          { label: "Enabled", value: asBoolean(memory.enabled) ? "yes" : "no" },
          { label: "Embeddings", value: asBoolean(memory.embeddings_enabled) ? "on" : "off" },
          { label: "Embedding provider", value: asText(memory.embedding_provider).trim() || "(none)" },
          { label: "Max working items", value: String(asNumber(memory.max_working_items, 0)) },
        ],
      }
    }

    if (widgetKey === "settings.network") {
      return {
        rows: [
          { label: "Allowed domains", value: String(Array.isArray(network.allowed_domains) ? network.allowed_domains.length : 0) },
          { label: "Shell exec", value: asBoolean(shell.enable_exec) ? "enabled" : "disabled" },
          { label: "Sandbox", value: asBoolean(sandbox.active) ? asText(sandbox.provider).trim() || "active" : "inactive" },
        ],
      }
    }

    return {
      rows: [
        { label: "Catch up", value: asBoolean(scheduler.catch_up) ? "enabled" : "disabled" },
        { label: "Max concurrent jobs", value: String(asNumber(scheduler.max_concurrent_jobs, 0)) },
      ],
    }
  }

  if (widgetKey === "channels.status") {
    const [cfg, secretsPayload] = await Promise.all([
      api.get<Record<string, unknown>>("/api/admin/config"),
      api.get<{ keys?: unknown }>("/api/admin/secrets"),
    ])
    const discord = asRecord(cfg.discord) ?? {}
    const telegram = asRecord(cfg.telegram) ?? {}
    const keys = Array.isArray(secretsPayload.keys) ? secretsPayload.keys.map((item) => asText(item).trim()) : []
    return {
      rows: [
        {
          label: "Discord",
          value: `${asBoolean(discord.enabled) ? "enabled" : "disabled"} · token ${keys.includes("discord/bot_token") ? "present" : "missing"}`,
        },
        {
          label: "Telegram",
          value: `${asBoolean(telegram.enabled) ? "enabled" : "disabled"} · token ${keys.includes("telegram/bot_token") ? "present" : "missing"}`,
        },
      ],
    }
  }

  if (widgetKey === "sessions.recent" || widgetKey === "sessions.overview") {
    const limit = widgetKey === "sessions.recent" ? 5 : 10
    const payload = await api.get<{ sessions?: unknown }>(`/api/admin/chat/sessions?limit=${encodeURIComponent(String(limit))}&offset=0`)
    const sessions = Array.isArray(payload.sessions) ? payload.sessions : []
    if (widgetKey === "sessions.recent") {
      const rows = sessions.slice(0, 5).map((session) => {
        const item = asRecord(session) ?? {}
        const title = asText(item.title).trim() || asText(item.session_id).trim() || "session"
        const detail = `${asText(item.agent_id).trim() || "default"} · ${asText(item.updated_at).trim() || "-"}`
        return { label: title, value: detail }
      })
      return { rows, emptyMessage: "No chat sessions yet." }
    }

    const latest = asRecord(sessions[0]) ?? {}
    return {
      rows: [
        { label: "Recent sessions", value: String(sessions.length) },
        {
          label: "Latest",
          value: sessions.length
            ? `${asText(latest.title).trim() || asText(latest.session_id).trim() || "session"} · ${asText(latest.agent_id).trim() || "default"}`
            : "none",
        },
      ],
    }
  }

  if (widgetKey === "skills.summary" || widgetKey === "skills.active_list") {
    const payload = await api.get<Record<string, unknown>>(`/api/admin/skills?agent_id=${encodeURIComponent(readAgentID())}`)
    const installed = Array.isArray(payload.installed_skills) ? payload.installed_skills.map((item) => asText(item).trim()).filter(Boolean) : []
    const activated = Array.isArray(payload.activated_skills) ? payload.activated_skills.map((item) => asText(item).trim()).filter(Boolean) : []
    if (widgetKey === "skills.summary") {
      return {
        rows: [
          { label: "Agent", value: asText(payload.agent_id).trim() || readAgentID() },
          { label: "Installed skills", value: String(installed.length) },
          { label: "Activated skills", value: String(activated.length) },
          { label: "Active", value: activated.length ? activated.slice(0, 3).join(", ") : "None" },
        ],
      }
    }
    return {
      rows: activated.slice(0, 6).map((item) => ({ label: item, value: `active for ${asText(payload.agent_id).trim() || readAgentID()}` })),
      emptyMessage: "No active skills.",
    }
  }

  if (widgetKey === "docs.summary" || widgetKey === "docs.doc_list") {
    const payload = await api.get<Record<string, unknown>>(`/api/admin/agent/docs?agent_id=${encodeURIComponent(readAgentID())}`)
    const documents = Array.isArray(payload.documents) ? payload.documents : []
    const docs = documents
      .map((item) => {
        const doc = asRecord(item)
        if (!doc) {
          return null
        }
        const name = asText(doc.name).trim()
        if (!name) {
          return null
        }
        return { name, exists: asBoolean(doc.exists) }
      })
      .filter((item): item is { name: string; exists: boolean } => item !== null)

    if (widgetKey === "docs.summary") {
      return {
        rows: [
          { label: "Agent", value: asText(payload.agent_id).trim() || readAgentID() },
          { label: "Docs present", value: `${docs.filter((doc) => doc.exists).length}/${docs.length}` },
          { label: "Top docs", value: docs.slice(0, 3).map((doc) => doc.name).join(", ") || "none" },
        ],
      }
    }

    return {
      rows: docs.slice(0, 6).map((doc) => ({ label: doc.name, value: doc.exists ? "present" : "missing" })),
      emptyMessage: "No docs found.",
    }
  }

  if (widgetKey === "sandbox.summary" || widgetKey === "sandbox.inventory") {
    const agentID = readAgentID()
    const [statusPayload, imagesPayload, volumesPayload] = await Promise.all([
      api
        .get<Record<string, unknown>>(`/api/admin/sandbox/docker/status?agent_id=${encodeURIComponent(agentID)}`)
        .catch((): Record<string, unknown> => ({})),
      api
        .get<Record<string, unknown>>("/api/admin/sandbox/docker/images")
        .catch((): Record<string, unknown> => ({ images: [] })),
      api
        .get<Record<string, unknown>>("/api/admin/sandbox/docker/volumes")
        .catch((): Record<string, unknown> => ({ volumes: [] })),
    ])

    const images = Array.isArray(imagesPayload.images) ? imagesPayload.images : []
    const volumes = Array.isArray(volumesPayload.volumes) ? volumesPayload.volumes : []

    if (widgetKey === "sandbox.summary") {
      return {
        rows: [
          { label: "Provider", value: asText(statusPayload.provider).trim() || "unknown" },
          { label: "Agent", value: agentID },
          { label: "Container", value: asBoolean(statusPayload.running) ? "running" : "stopped" },
          { label: "Images", value: String(images.length) },
          { label: "Volumes", value: String(volumes.length) },
        ],
      }
    }

    return {
      rows: [
        { label: "Images", value: String(images.length) },
        { label: "Volumes", value: String(volumes.length) },
      ],
    }
  }

  return {
    rows: [],
    emptyMessage: "No data for this widget.",
  }
}

type WidgetCardProps = {
  widget: DashboardWidgetLayout
  spec: WidgetSpec
  menuOpen: boolean
  onToggleMenu: () => void
  onStartDrag: (event: ReactPointerEvent<HTMLElement>, widgetID: string) => void
  onStartResize: (event: ReactPointerEvent<HTMLElement>, widgetID: string) => void
  onOpenSource: () => void
  onDuplicate: () => void
  onConfigure: () => void
  onRemove: () => void
}

function WidgetCard({
  widget,
  spec,
  menuOpen,
  onToggleMenu,
  onStartDrag,
  onStartResize,
  onOpenSource,
  onDuplicate,
  onConfigure,
  onRemove,
}: WidgetCardProps) {
  const [rows, setRows] = useState<WidgetRow[]>([])
  const [emptyMessage, setEmptyMessage] = useState("")
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState("")

  const [quickPromptText, setQuickPromptText] = useState("")
  const [quickPromptResult, setQuickPromptResult] = useState("")
  const [quickPromptPending, setQuickPromptPending] = useState(false)

  const widgetStateKey = useMemo(() => JSON.stringify(widget.widget_state ?? {}), [widget.widget_state])
  const isQuickPrompt = spec.key === "chat.quick_prompt"

  useEffect(() => {
    if (isQuickPrompt) {
      return
    }

    let cancelled = false
    setLoading(true)
    setError("")

    void (async () => {
      try {
        const result = await fetchWidgetData(spec.key, widget.widget_state)
        if (cancelled) {
          return
        }
        setRows(result.rows)
        setEmptyMessage(result.emptyMessage || "")
      } catch (err) {
        if (cancelled) {
          return
        }
        setRows([])
        setEmptyMessage("")
        setError(extractErrorMessage(err))
      } finally {
        if (!cancelled) {
          setLoading(false)
        }
      }
    })()

    return () => {
      cancelled = true
    }
  }, [isQuickPrompt, spec.key, widgetStateKey, widget.widget_state])

  const submitQuickPrompt = useCallback(async () => {
    const message = quickPromptText.trim()
    if (!message || quickPromptPending) {
      return
    }

    setQuickPromptPending(true)
    setQuickPromptResult("")
    try {
      const agentID = asText(widget.widget_state.agent_id).trim() || "default"
      const response = await api.post<Record<string, unknown>>("/v1/chat/messages", {
        user_id: "dashboard_user",
        room_id: "dashboard",
        agent_id: agentID,
        message,
      })
      const resultLabel = asText(response.id).trim() || asText(response.status).trim() || "ok"
      setQuickPromptResult(`Queued: ${resultLabel}`)
      setQuickPromptText("")
    } catch (err) {
      setQuickPromptResult(`Failed: ${extractErrorMessage(err)}`)
    } finally {
      setQuickPromptPending(false)
    }
  }, [quickPromptPending, quickPromptText, widget.widget_state.agent_id])

  return (
    <article
      className="relative overflow-visible rounded-md border bg-card"
      style={{
        gridColumn: `${widget.x + 1} / span ${widget.w}`,
        gridRow: `${widget.y + 1} / span ${widget.h}`,
      }}
      data-testid={`widget-card-${widget.widget_instance_id}`}
      data-widget-key={widget.widget_key}
      data-widget-x={String(widget.x)}
      data-widget-y={String(widget.y)}
      data-widget-w={String(widget.w)}
      data-widget-h={String(widget.h)}
    >
      <header
        className="flex items-start justify-between gap-2 border-b px-3 py-2"
        data-testid={`widget-drag-handle-${widget.widget_instance_id}`}
        onPointerDown={(event) => {
          const target = event.target as HTMLElement
          if (target.closest("[data-widget-menu-root='true']")) {
            return
          }
          onStartDrag(event, widget.widget_instance_id)
        }}
      >
        <div className="min-w-0">
          <p className="text-sm font-semibold">{spec.label}</p>
          <p className="text-xs text-muted-foreground">{spec.description}</p>
        </div>
        <div className="relative" data-widget-menu-root="true">
          <Button type="button" variant="outline" size="sm" onClick={onToggleMenu} aria-label={`Widget menu ${spec.label}`}>
            ...
          </Button>
          {menuOpen ? (
            <div className="absolute right-0 top-full z-20 mt-1 min-w-[170px] rounded-md border bg-popover p-1 shadow-lg">
              <button
                type="button"
                className="block w-full rounded-sm px-2 py-1 text-left text-sm hover:bg-muted"
                onClick={onOpenSource}
              >
                Open source tab
              </button>
              <button
                type="button"
                className="block w-full rounded-sm px-2 py-1 text-left text-sm hover:bg-muted"
                onClick={onDuplicate}
              >
                Duplicate widget
              </button>
              {spec.configure ? (
                <button
                  type="button"
                  className="block w-full rounded-sm px-2 py-1 text-left text-sm hover:bg-muted"
                  onClick={onConfigure}
                >
                  Configure
                </button>
              ) : null}
              <button
                type="button"
                className="block w-full rounded-sm px-2 py-1 text-left text-sm hover:bg-muted"
                onClick={onRemove}
              >
                Remove widget
              </button>
            </div>
          ) : null}
        </div>
      </header>

      <div className="space-y-2 overflow-auto p-3 text-sm">
        {isQuickPrompt ? (
          <div className="space-y-2">
            <textarea
              value={quickPromptText}
              onChange={(event) => setQuickPromptText(event.target.value)}
              rows={4}
              className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              placeholder="Send a quick prompt"
            />
            <div className="flex items-center gap-2">
              <Button type="button" size="sm" onClick={() => void submitQuickPrompt()} disabled={quickPromptPending || !quickPromptText.trim()}>
                {quickPromptPending ? "Sending..." : "Send"}
              </Button>
              <span className="text-xs text-muted-foreground">
                Agent: {asText(widget.widget_state.agent_id).trim() || "default"}
              </span>
            </div>
            {quickPromptResult ? <p className="text-xs text-muted-foreground">{quickPromptResult}</p> : null}
          </div>
        ) : null}

        {!isQuickPrompt && loading ? <p className="text-xs text-muted-foreground">Loading widget...</p> : null}
        {!isQuickPrompt && error ? <p className="text-xs text-destructive">{error}</p> : null}

        {!isQuickPrompt && !loading && !error && rows.length === 0 ? (
          <p className="text-xs text-muted-foreground">{emptyMessage || "No widget data."}</p>
        ) : null}

        {!isQuickPrompt && rows.length > 0 ? (
          <ul className="space-y-1">
            {rows.map((row) => (
              <li key={`${row.label}:${row.value}`} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] gap-2 rounded-sm border px-2 py-1 text-xs">
                <span className="font-medium">{row.label}</span>
                <span className="text-right text-muted-foreground">{row.value}</span>
              </li>
            ))}
          </ul>
        ) : null}
      </div>

      <button
        type="button"
        aria-label={`Resize ${spec.label}`}
        className="absolute bottom-0 right-0 h-4 w-4 cursor-se-resize rounded-tl-sm border-l border-t border-border bg-muted/80"
        data-testid={`widget-resize-handle-${widget.widget_instance_id}`}
        onPointerDown={(event) => onStartResize(event, widget.widget_instance_id)}
      />
    </article>
  )
}

export function CustomDashboardsPage() {
  const navigate = useNavigate()
  const [dashboards, setDashboards] = useState<DashboardRecord[]>([])
  const [selectedID, setSelectedID] = useState("")
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveNotice, setSaveNotice] = useState("")
  const [widgetPickerOpen, setWidgetPickerOpen] = useState(false)
  const [widgetPickerQuery, setWidgetPickerQuery] = useState("")
  const [widgetMenuFor, setWidgetMenuFor] = useState("")

  const dashboardsRef = useRef<DashboardRecord[]>([])
  const selectedIDRef = useRef("")
  const knownServerIDsRef = useRef<Set<string>>(new Set())
  const saveTimerRef = useRef<number | null>(null)
  const saveAllRef = useRef<() => Promise<void>>(async () => undefined)
  const savingRef = useRef(false)
  const mountedRef = useRef(true)
  const gridRef = useRef<HTMLDivElement | null>(null)
  const dragRef = useRef<DragSession | null>(null)

  useEffect(() => {
    dashboardsRef.current = dashboards
  }, [dashboards])

  useEffect(() => {
    selectedIDRef.current = selectedID
  }, [selectedID])

  useEffect(() => {
    return () => {
      mountedRef.current = false
      if (saveTimerRef.current !== null) {
        window.clearTimeout(saveTimerRef.current)
        saveTimerRef.current = null
      }
    }
  }, [])

  const selectedDashboard = useMemo(() => {
    return dashboards.find((item) => item.id === selectedID) ?? dashboards[0] ?? null
  }, [dashboards, selectedID])

  const filteredWidgets = useMemo(() => {
    const query = widgetPickerQuery.trim().toLowerCase()
    if (!query) {
      return WIDGETS
    }
    return WIDGETS.filter((widget) => `${widget.label} ${widget.description}`.toLowerCase().includes(query))
  }, [widgetPickerQuery])

  const persistDashboards = useCallback((nextDashboards: DashboardRecord[]) => {
    setDashboards(nextDashboards)
    dashboardsRef.current = nextDashboards
    writeLocalDashboards(nextDashboards)
  }, [])

  const scheduleSave = useCallback(() => {
    if (saveTimerRef.current !== null) {
      window.clearTimeout(saveTimerRef.current)
    }
    saveTimerRef.current = window.setTimeout(() => {
      saveTimerRef.current = null
      void saveAllRef.current()
    }, AUTO_SAVE_DEBOUNCE_MS)
  }, [])

  const markDirty = useCallback(
    (nextDashboards: DashboardRecord[], autosave = true) => {
      const positioned = nextDashboards.map((dashboard, index) => ({
        ...cloneDashboard(dashboard),
        position: index,
      }))
      persistDashboards(positioned)
      setDirty(true)
      setSaveNotice("Unsaved changes")
      if (autosave) {
        scheduleSave()
      }
    },
    [persistDashboards, scheduleSave]
  )

  const saveAllDashboards = useCallback(async () => {
    if (savingRef.current) {
      return
    }

    const current = dashboardsRef.current
    if (!current.length) {
      setDirty(false)
      setSaveNotice("Saved")
      return
    }

    savingRef.current = true
    setSaving(true)
    setError("")

    try {
      const working = current.map(cloneDashboard)
      let selectedRemap = selectedIDRef.current

      for (let index = 0; index < working.length; index += 1) {
        const dashboard = working[index]
        dashboard.position = index
        dashboard.updated_at = nowISO()

        if (!knownServerIDsRef.current.has(dashboard.id)) {
          const created = await api.post<{ dashboard?: unknown }>("/api/admin/dashboards", {
            name: dashboard.name,
          })
          const remote = normalizeDashboard(created.dashboard ?? {})
          const previousID = dashboard.id
          dashboard.id = remote.id
          dashboard.created_at = remote.created_at
          knownServerIDsRef.current.add(remote.id)
          if (selectedRemap === previousID) {
            selectedRemap = remote.id
          }
        }

        await api.put(`/api/admin/dashboards/${encodeURIComponent(dashboard.id)}`, dashboard)
      }

      if (!mountedRef.current) {
        return
      }

      persistDashboards(working)
      setSelectedID(selectedRemap || working[0]?.id || "")
      setDirty(false)
      setSaveNotice("Saved")
    } catch (err) {
      if (!mountedRef.current) {
        return
      }
      setError(extractErrorMessage(err))
      setSaveNotice("Save failed")
    } finally {
      if (mountedRef.current) {
        setSaving(false)
      }
      savingRef.current = false
    }
  }, [persistDashboards])

  useEffect(() => {
    saveAllRef.current = saveAllDashboards
  }, [saveAllDashboards])

  const loadDashboards = useCallback(async () => {
    setLoading(true)
    setError("")

    try {
      const localItems = readLocalDashboards()
      const payload = await api.get<{ dashboards?: unknown }>("/api/admin/dashboards")
      const remoteItems = Array.isArray(payload.dashboards) ? payload.dashboards.map(normalizeDashboard) : []
      knownServerIDsRef.current = new Set(remoteItems.map((item) => item.id))

      let merged = mergeDashboards(localItems, remoteItems)
      if (merged.length === 0) {
        merged = [createDefaultDashboard()]
      }
      merged = merged.map((dashboard, index) => ({ ...cloneDashboard(dashboard), position: index }))

      persistDashboards(merged)
      setSelectedID((current) => {
        if (current && merged.some((item) => item.id === current)) {
          return current
        }
        return merged[0]?.id || ""
      })
      setDirty(false)
      setSaveNotice("")
    } catch (err) {
      const fallback = readLocalDashboards()
      const normalizedFallback = (fallback.length ? fallback : [createDefaultDashboard()]).map((dashboard, index) => ({
        ...cloneDashboard(dashboard),
        position: index,
      }))
      persistDashboards(normalizedFallback)
      setSelectedID((current) => {
        if (current && normalizedFallback.some((item) => item.id === current)) {
          return current
        }
        return normalizedFallback[0]?.id || ""
      })
      setError(extractErrorMessage(err))
      setDirty(false)
    } finally {
      setLoading(false)
    }
  }, [persistDashboards])

  useEffect(() => {
    void loadDashboards()
  }, [loadDashboards])

  const mutateSelectedDashboard = useCallback(
    (mutator: (dashboard: DashboardRecord) => void, autosave = true) => {
      const activeID = selectedIDRef.current
      if (!activeID) {
        return
      }

      const nextDashboards = dashboardsRef.current.map((dashboard) => {
        if (dashboard.id !== activeID) {
          return cloneDashboard(dashboard)
        }

        const draft = cloneDashboard(dashboard)
        mutator(draft)
        draft.updated_at = nowISO()
        return draft
      })

      markDirty(nextDashboards, autosave)
    },
    [markDirty]
  )

  const createDashboard = useCallback(async () => {
    try {
      const payload = await api.post<{ dashboard?: unknown }>("/api/admin/dashboards", {
        name: `Dashboard ${dashboardsRef.current.length + 1}`,
      })
      const created = normalizeDashboard(payload.dashboard ?? {})
      knownServerIDsRef.current.add(created.id)
      const next = [...dashboardsRef.current.map(cloneDashboard), created]
      markDirty(next)
      setSelectedID(created.id)
      setWidgetPickerOpen(false)
      setWidgetPickerQuery("")
      setError("")
    } catch (err) {
      setError(extractErrorMessage(err))
    }
  }, [markDirty])

  const deleteDashboard = useCallback(
    async (id: string) => {
      if (dashboardsRef.current.length <= 1) {
        return
      }

      if (!window.confirm("Delete this custom dashboard?")) {
        return
      }

      try {
        if (knownServerIDsRef.current.has(id)) {
          await api.delete(`/api/admin/dashboards/${encodeURIComponent(id)}`)
          knownServerIDsRef.current.delete(id)
        }

        const next = dashboardsRef.current.filter((item) => item.id !== id).map(cloneDashboard)
        markDirty(next)
        setSelectedID((current) => {
          if (current !== id) {
            return current
          }
          return next[0]?.id || ""
        })
      } catch (err) {
        setError(extractErrorMessage(err))
      }
    },
    [markDirty]
  )

  const duplicateDashboard = useCallback(
    (id: string) => {
      const original = dashboardsRef.current.find((item) => item.id === id)
      if (!original) {
        return
      }

      const cloned = cloneDashboard(original)
      cloned.id = uid("dash")
      cloned.name = `${original.name} Copy`
      cloned.created_at = nowISO()
      cloned.updated_at = nowISO()
      cloned.layout = cloned.layout.map((widget) => ({
        ...cloneWidget(widget),
        widget_instance_id: uid("widget"),
      }))

      const next = [...dashboardsRef.current.map(cloneDashboard), cloned]
      markDirty(next)
      setSelectedID(cloned.id)
    },
    [markDirty]
  )

  const moveDashboard = useCallback(
    (id: string, direction: "up" | "down") => {
      const next = dashboardsRef.current.map(cloneDashboard)
      const index = next.findIndex((item) => item.id === id)
      if (index < 0) {
        return
      }
      if (direction === "up" && index > 0) {
        ;[next[index - 1], next[index]] = [next[index], next[index - 1]]
      }
      if (direction === "down" && index < next.length - 1) {
        ;[next[index + 1], next[index]] = [next[index], next[index + 1]]
      }
      markDirty(next)
    },
    [markDirty]
  )

  const addWidget = useCallback(
    (widgetKey: string) => {
      const spec = WIDGET_MAP.get(widgetKey)
      if (!spec) {
        return
      }

      mutateSelectedDashboard((dashboard) => {
        const nextWidget = createDefaultWidget(widgetKey)
        nextWidget.y = dashboard.layout.reduce((max, item) => Math.max(max, item.y + item.h), 0)
        dashboard.layout = [...dashboard.layout, nextWidget]
      })

      setWidgetPickerOpen(false)
      setWidgetPickerQuery("")
    },
    [mutateSelectedDashboard]
  )

  const removeWidget = useCallback(
    (widgetID: string) => {
      mutateSelectedDashboard((dashboard) => {
        dashboard.layout = dashboard.layout.filter((item) => item.widget_instance_id !== widgetID)
      })
    },
    [mutateSelectedDashboard]
  )

  const duplicateWidget = useCallback(
    (widgetID: string) => {
      mutateSelectedDashboard((dashboard) => {
        const original = dashboard.layout.find((item) => item.widget_instance_id === widgetID)
        if (!original) {
          return
        }
        const duplicate = cloneWidget(original)
        duplicate.widget_instance_id = uid("widget")
        duplicate.x = Math.max(0, Math.min(GRID_COLUMNS - duplicate.w, duplicate.x + 1))
        duplicate.y = duplicate.y + 1
        dashboard.layout = [...dashboard.layout, duplicate]
      })
    },
    [mutateSelectedDashboard]
  )

  const configureWidget = useCallback(
    (widgetID: string) => {
      mutateSelectedDashboard((dashboard) => {
        const target = dashboard.layout.find((item) => item.widget_instance_id === widgetID)
        if (!target) {
          return
        }
        const spec = WIDGET_MAP.get(target.widget_key)
        if (!spec?.configure) {
          return
        }
        const nextState = spec.configure({ ...target.widget_state })
        if (!nextState) {
          return
        }
        target.widget_state = { ...nextState }
      })
    },
    [mutateSelectedDashboard]
  )

  const startPointerDrag = useCallback(
    (event: ReactPointerEvent<HTMLElement>, widgetID: string, mode: DragMode) => {
      event.preventDefault()
      const grid = gridRef.current
      const dashboard = selectedDashboard
      if (!grid || !dashboard) {
        return
      }

      const widget = dashboard.layout.find((item) => item.widget_instance_id === widgetID)
      if (!widget) {
        return
      }

      const rect = grid.getBoundingClientRect()
      dragRef.current = {
        mode,
        widgetID,
        startX: event.clientX,
        startY: event.clientY,
        gridRect: rect,
        originX: widget.x,
        originY: widget.y,
        originW: widget.w,
        originH: widget.h,
      }

      const handleMove = (moveEvent: PointerEvent) => {
        const session = dragRef.current
        if (!session) {
          return
        }

        const colWidth = session.gridRect.width / GRID_COLUMNS
        const dx = Math.round((moveEvent.clientX - session.startX) / colWidth)
        const dy = Math.round((moveEvent.clientY - session.startY) / GRID_ROW_HEIGHT)

        mutateSelectedDashboard((draft) => {
          const target = draft.layout.find((item) => item.widget_instance_id === session.widgetID)
          if (!target) {
            return
          }
          if (session.mode === "move") {
            target.x = Math.max(0, Math.min(GRID_COLUMNS - target.w, session.originX + dx))
            target.y = Math.max(0, session.originY + dy)
            return
          }
          target.w = Math.max(2, Math.min(GRID_COLUMNS - target.x, session.originW + dx))
          target.h = Math.max(2, Math.min(12, session.originH + dy))
        }, false)
      }

      const handleUp = () => {
        window.removeEventListener("pointermove", handleMove)
        window.removeEventListener("pointerup", handleUp)
        dragRef.current = null
        setSaveNotice("Unsaved changes")
        scheduleSave()
      }

      window.addEventListener("pointermove", handleMove)
      window.addEventListener("pointerup", handleUp, { once: true })
    },
    [mutateSelectedDashboard, scheduleSave, selectedDashboard]
  )

  useEffect(() => {
    if (!widgetMenuFor) {
      return
    }

    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as HTMLElement | null
      if (!target) {
        return
      }
      if (target.closest("[data-widget-menu-root='true']")) {
        return
      }
      setWidgetMenuFor("")
    }

    document.addEventListener("pointerdown", onPointerDown)
    return () => {
      document.removeEventListener("pointerdown", onPointerDown)
    }
  }, [widgetMenuFor])

  return (
    <div className="space-y-4 p-6" data-testid="dashboards-page">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Custom Dashboards</h2>
        <p className="text-sm text-muted-foreground">
          Create reusable operator dashboards with drag, resize, widget reuse, and server-backed persistence.
        </p>
      </div>

      {error ? <p className="text-sm text-destructive">{error}</p> : null}

      <div className="grid gap-4 lg:grid-cols-[280px_minmax(0,1fr)]">
        <Card>
          <CardHeader className="space-y-3">
            <CardTitle className="text-base">Dashboards</CardTitle>
            <Button type="button" onClick={() => void createDashboard()} disabled={loading || saving}>
              Create dashboard
            </Button>
          </CardHeader>
          <CardContent>
            <div className="space-y-2" data-testid="dashboard-tab-list">
              {dashboards.map((dashboard, index) => {
                const isSelected = dashboard.id === selectedDashboard?.id
                return (
                  <div
                    key={dashboard.id}
                    data-testid={`dashboard-tab-row-${dashboard.id}`}
                    className={`space-y-2 rounded-md border p-2 ${isSelected ? "border-primary" : ""}`}
                  >
                    <button
                      type="button"
                      className={`w-full rounded-sm px-2 py-1 text-left text-sm ${isSelected ? "bg-primary text-primary-foreground" : "hover:bg-muted"}`}
                      onClick={() => {
                        setSelectedID(dashboard.id)
                        setWidgetPickerOpen(false)
                        setWidgetPickerQuery("")
                      }}
                    >
                      {dashboard.name}
                    </button>
                    <div className="flex flex-wrap gap-1">
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        aria-label={`Move ${dashboard.name} up`}
                        disabled={index === 0}
                        onClick={() => moveDashboard(dashboard.id, "up")}
                      >
                        ↑
                      </Button>
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        aria-label={`Move ${dashboard.name} down`}
                        disabled={index === dashboards.length - 1}
                        onClick={() => moveDashboard(dashboard.id, "down")}
                      >
                        ↓
                      </Button>
                      <Button type="button" size="sm" variant="outline" onClick={() => duplicateDashboard(dashboard.id)}>
                        Duplicate
                      </Button>
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        disabled={dashboards.length <= 1}
                        onClick={() => void deleteDashboard(dashboard.id)}
                      >
                        Delete
                      </Button>
                    </div>
                  </div>
                )
              })}

              {!loading && dashboards.length === 0 ? (
                <p className="text-sm text-muted-foreground">No dashboards yet.</p>
              ) : null}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="space-y-3">
            <div className="flex flex-wrap items-center gap-2">
              <Input
                aria-label="Dashboard name"
                value={selectedDashboard?.name || ""}
                onChange={(event) => {
                  const nextName = event.target.value
                  mutateSelectedDashboard((dashboard) => {
                    dashboard.name = nextName.trim() || "Untitled Dashboard"
                  })
                }}
                disabled={!selectedDashboard || loading}
              />
              <Button
                type="button"
                data-testid="toolbar-add-widget"
                onClick={() => setWidgetPickerOpen((current) => !current)}
                disabled={!selectedDashboard || loading}
              >
                Add widget
              </Button>
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  mutateSelectedDashboard((dashboard) => {
                    dashboard.layout = []
                  })
                }}
                disabled={!selectedDashboard || loading}
              >
                Reset layout
              </Button>
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  if (saveTimerRef.current !== null) {
                    window.clearTimeout(saveTimerRef.current)
                    saveTimerRef.current = null
                  }
                  void saveAllDashboards()
                }}
                disabled={saving || !dirty}
              >
                {saving ? "Saving..." : "Save now"}
              </Button>
            </div>

            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <span>{saving ? "Saving..." : dirty ? "Dirty" : "Clean"}</span>
              {saveNotice ? <span>{saveNotice}</span> : null}
            </div>
          </CardHeader>
          <CardContent className="space-y-4">
            {widgetPickerOpen ? (
              <div className="space-y-2 rounded-md border bg-muted/20 p-3">
                <Input
                  placeholder="Search widgets"
                  value={widgetPickerQuery}
                  onChange={(event) => setWidgetPickerQuery(event.target.value)}
                  aria-label="Search widgets"
                />
                <div className="max-h-56 space-y-1 overflow-auto">
                  {filteredWidgets.map((widget) => (
                    <button
                      key={widget.key}
                      type="button"
                      className="block w-full rounded-md border bg-background px-2 py-2 text-left text-sm hover:bg-muted"
                      data-testid={`widget-picker-option-${widget.key.replace(/\./g, "-")}`}
                      onClick={() => addWidget(widget.key)}
                    >
                      <p className="font-medium">{widget.label}</p>
                      <p className="text-xs text-muted-foreground">{widget.description}</p>
                    </button>
                  ))}
                  {filteredWidgets.length === 0 ? (
                    <p className="text-sm text-muted-foreground">No widgets match this search.</p>
                  ) : null}
                </div>
              </div>
            ) : null}

            {loading ? <p className="text-sm text-muted-foreground">Loading dashboards...</p> : null}

            {!loading && selectedDashboard && selectedDashboard.layout.length === 0 ? (
              <div className="rounded-md border border-dashed p-8 text-center" data-testid="dashboard-empty-state">
                <p className="text-sm text-muted-foreground">This dashboard has no widgets yet.</p>
                <Button type="button" className="mt-3" data-testid="empty-add-widget" onClick={() => setWidgetPickerOpen(true)}>
                  Add Widget
                </Button>
              </div>
            ) : null}

            {!loading && selectedDashboard && selectedDashboard.layout.length > 0 ? (
              <div
                ref={gridRef}
                className="grid gap-3"
                style={{
                  gridTemplateColumns: `repeat(${GRID_COLUMNS}, minmax(0, 1fr))`,
                  gridAutoRows: `${GRID_ROW_HEIGHT}px`,
                }}
                data-testid="dashboard-widget-grid"
              >
                {selectedDashboard.layout.map((widget) => {
                  const spec = WIDGET_MAP.get(widget.widget_key)
                  if (!spec) {
                    return null
                  }

                  return (
                    <WidgetCard
                      key={widget.widget_instance_id}
                      widget={widget}
                      spec={spec}
                      menuOpen={widgetMenuFor === widget.widget_instance_id}
                      onToggleMenu={() => setWidgetMenuFor((current) => (current === widget.widget_instance_id ? "" : widget.widget_instance_id))}
                      onStartDrag={(event, widgetID) => startPointerDrag(event, widgetID, "move")}
                      onStartResize={(event, widgetID) => startPointerDrag(event, widgetID, "resize")}
                      onOpenSource={() => {
                        setWidgetMenuFor("")
                        navigate(spec.sourcePath)
                      }}
                      onDuplicate={() => {
                        setWidgetMenuFor("")
                        duplicateWidget(widget.widget_instance_id)
                      }}
                      onConfigure={() => {
                        setWidgetMenuFor("")
                        configureWidget(widget.widget_instance_id)
                      }}
                      onRemove={() => {
                        setWidgetMenuFor("")
                        removeWidget(widget.widget_instance_id)
                      }}
                    />
                  )
                })}
              </div>
            ) : null}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
