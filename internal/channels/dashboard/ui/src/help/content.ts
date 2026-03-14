import gettingStartedRaw from "../../help/getting-started.md?raw"
import discordBotSetupRaw from "../../help/discord-bot-setup.md?raw"
import providersAndModelsRaw from "../../help/providers-and-models.md?raw"
import agentOverridesAndSubagentsRaw from "../../help/agent-overrides-and-subagents.md?raw"
import secretsGuideRaw from "../../help/secrets-guide.md?raw"
import customDashboardsRaw from "../../help/custom-dashboards.md?raw"
import runsAndDebuggingRaw from "../../help/runs-and-debugging.md?raw"
import schedulerGuideRaw from "../../help/scheduler-guide.md?raw"
import faqRaw from "../../help/faq.md?raw"
import troubleshootingIntegrationsRaw from "../../help/troubleshooting-integrations.md?raw"

export interface HelpTopic {
  id: string
  title: string
  category: string
  keywords: string[]
  related_topics: string[]
  route_hints: string[]
  body: string
  plainText: string
  fileName: string
}

export interface HelpCategory {
  key: string
  label: string
  icon: string
}

export const HELP_TOPIC_FILES = [
  "getting-started.md",
  "discord-bot-setup.md",
  "providers-and-models.md",
  "agent-overrides-and-subagents.md",
  "secrets-guide.md",
  "custom-dashboards.md",
  "runs-and-debugging.md",
  "scheduler-guide.md",
  "faq.md",
  "troubleshooting-integrations.md",
]

const HELP_RAW_BY_FILE: Record<string, string> = {
  "getting-started.md": gettingStartedRaw,
  "discord-bot-setup.md": discordBotSetupRaw,
  "providers-and-models.md": providersAndModelsRaw,
  "agent-overrides-and-subagents.md": agentOverridesAndSubagentsRaw,
  "secrets-guide.md": secretsGuideRaw,
  "custom-dashboards.md": customDashboardsRaw,
  "runs-and-debugging.md": runsAndDebuggingRaw,
  "scheduler-guide.md": schedulerGuideRaw,
  "faq.md": faqRaw,
  "troubleshooting-integrations.md": troubleshootingIntegrationsRaw,
}

export const HELP_CATEGORIES: HelpCategory[] = [
  { key: "Getting Started", label: "Getting Started", icon: "🚀" },
  { key: "Integrations", label: "Integrations", icon: "🔌" },
  { key: "Settings", label: "Settings", icon: "⚙" },
  { key: "Secrets", label: "Secrets", icon: "🔐" },
  { key: "Dashboards", label: "Dashboards", icon: "📊" },
  { key: "Debugging", label: "Debugging", icon: "🧭" },
  { key: "Scheduler", label: "Scheduler", icon: "⏱" },
  { key: "FAQ", label: "FAQ", icon: "❓" },
]

export const HELP_ROUTE_CONTEXT: Record<string, string[]> = {
  "/chat": ["getting-started", "runs-and-debugging", "faq"],
  "/runs": ["runs-and-debugging", "providers-and-models", "faq"],
  "/settings": ["providers-and-models", "agent-overrides-and-subagents", "discord-bot-setup", "troubleshooting-integrations"],
  "/secrets": ["secrets-guide", "discord-bot-setup", "troubleshooting-integrations"],
  "/dashboards": ["custom-dashboards", "agent-overrides-and-subagents", "faq"],
  "/scheduler": ["scheduler-guide", "runs-and-debugging", "faq"],
  "/sandbox": ["runs-and-debugging", "faq"],
  "/skills": ["getting-started", "faq"],
  "/docs": ["getting-started", "faq"],
  "/help": ["getting-started", "discord-bot-setup", "providers-and-models", "secrets-guide", "custom-dashboards", "runs-and-debugging", "scheduler-guide", "faq"],
}

const HELP_TOPIC_ALIASES: Record<string, string> = {
  start: "getting-started",
  onboarding: "getting-started",
  discord: "discord-bot-setup",
  providers: "providers-and-models",
  provider: "providers-and-models",
  models: "providers-and-models",
  agents: "agent-overrides-and-subagents",
  "agent-overrides": "agent-overrides-and-subagents",
  subagents: "agent-overrides-and-subagents",
  secrets: "secrets-guide",
  secret: "secrets-guide",
  dashboards: "custom-dashboards",
  dashboard: "custom-dashboards",
  runs: "runs-and-debugging",
  run: "runs-and-debugging",
  debugging: "runs-and-debugging",
  scheduler: "scheduler-guide",
  troubleshooting: "troubleshooting-integrations",
  integrations: "troubleshooting-integrations",
}

let topicsPromise: Promise<HelpTopic[]> | null = null

function normalizeTopicLookupKey(value: string): string {
  const decoded = (() => {
    try {
      return decodeURIComponent(String(value || ""))
    } catch (_error) {
      return String(value || "")
    }
  })()

  return decoded
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
}

function normalizeSearchText(value: string): string {
  return String(value || "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, " ")
    .replace(/\s+/g, " ")
    .trim()
}

export function parseFrontmatter(raw: string): { meta: Record<string, string>; body: string } {
  const text = String(raw || "")
  if (!text.startsWith("---\n")) {
    return { meta: {}, body: text }
  }

  const end = text.indexOf("\n---\n", 4)
  if (end < 0) {
    return { meta: {}, body: text }
  }

  const frontmatter = text.slice(4, end).split("\n")
  const meta: Record<string, string> = {}
  frontmatter.forEach((line) => {
    const idx = line.indexOf(":")
    if (idx < 0) {
      return
    }
    const key = line.slice(0, idx).trim()
    const value = line.slice(idx + 1).trim()
    meta[key] = value
  })

  return { meta, body: text.slice(end + 5) }
}

function normalizeList(value: string): string[] {
  return String(value || "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean)
}

export function stripMarkdown(text: string): string {
  return String(text || "")
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/`([^`]+)`/g, "$1")
    .replace(/^#+\s+/gm, "")
    .replace(/^>\s?/gm, "")
    .replace(/\*\*([^*]+)\*\*/g, "$1")
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g, "$1")
    .replace(/[-*]\s+/g, "")
    .replace(/\s+/g, " ")
    .trim()
}

async function loadTopic(fileName: string): Promise<HelpTopic> {
  const raw = HELP_RAW_BY_FILE[fileName]
  if (!raw) {
    throw new Error(`Failed to load help topic ${fileName}`)
  }

  const { meta, body } = parseFrontmatter(raw)
  return {
    id: meta.id || fileName.replace(/\.md$/, ""),
    title: meta.title || fileName,
    category: meta.category || "General",
    keywords: normalizeList(meta.keywords),
    related_topics: normalizeList(meta.related_topics),
    route_hints: normalizeList(meta.route_hints),
    body,
    plainText: stripMarkdown(body),
    fileName,
  }
}

export async function loadHelpTopics(): Promise<HelpTopic[]> {
  if (!topicsPromise) {
    topicsPromise = Promise.all(HELP_TOPIC_FILES.map((fileName) => loadTopic(fileName)))
  }

  return topicsPromise
}

export function resolveHelpTopicID(topics: HelpTopic[], topicID: string): string {
  const lookup = normalizeTopicLookupKey(topicID)
  if (!lookup) {
    return ""
  }

  const directIDMatch = topics.find((topic) => normalizeTopicLookupKey(topic.id) === lookup)
  if (directIDMatch) {
    return directIDMatch.id
  }

  const directTitleMatch = topics.find((topic) => normalizeTopicLookupKey(topic.title) === lookup)
  if (directTitleMatch) {
    return directTitleMatch.id
  }

  const idWithoutCommonSuffix = topics.find((topic) => {
    const stripped = normalizeTopicLookupKey(topic.id.replace(/-(guide|setup)$/i, ""))
    return stripped === lookup
  })
  if (idWithoutCommonSuffix) {
    return idWithoutCommonSuffix.id
  }

  const aliasedID = HELP_TOPIC_ALIASES[lookup]
  if (aliasedID && topics.some((topic) => topic.id === aliasedID)) {
    return aliasedID
  }

  const keywordMatches = topics.filter((topic) =>
    topic.keywords.some((keyword) => normalizeTopicLookupKey(keyword) === lookup)
  )
  if (keywordMatches.length === 1) {
    return keywordMatches[0].id
  }

  return ""
}

export function searchHelpTopics(topics: HelpTopic[], query: string): HelpTopic[] {
  const normalizedQuery = normalizeSearchText(query)
  if (!normalizedQuery) {
    return topics
  }

  const queryTerms = normalizedQuery.split(" ").filter(Boolean)

  return topics
    .map((topic) => {
      const searchText = normalizeSearchText(
        `${topic.id} ${topic.title} ${topic.category} ${topic.keywords.join(" ")} ${topic.route_hints.join(" ")} ${topic.plainText}`
      )

      if (!queryTerms.every((term) => searchText.includes(term))) {
        return null
      }

      const phraseIndex = searchText.indexOf(normalizedQuery)
      const firstTermIndex = queryTerms
        .map((term) => searchText.indexOf(term))
        .reduce((lowest, index) => (index >= 0 ? Math.min(lowest, index) : lowest), Number.MAX_SAFE_INTEGER)
      const score = (phraseIndex >= 0 ? phraseIndex : firstTermIndex) + (phraseIndex >= 0 ? 0 : 1000)
      return { topic, score }
    })
    .filter((item): item is { topic: HelpTopic; score: number } => Boolean(item))
    .sort((left, right) => left.score - right.score)
    .map((item) => item.topic)
}

export function relatedHelpTopics(topics: HelpTopic[], topic: HelpTopic | null): HelpTopic[] {
  if (!topic) {
    return []
  }
  const set = new Set(topic.related_topics || [])
  return topics.filter((item) => set.has(item.id))
}

export function contextualHelpTopics(topics: HelpTopic[], route: string): HelpTopic[] {
  const ids = HELP_ROUTE_CONTEXT[route] || HELP_ROUTE_CONTEXT["/help"] || []
  return topics.filter((topic) => ids.includes(topic.id))
}

export function categoryForTopic(topic: HelpTopic): HelpCategory {
  return HELP_CATEGORIES.find((item) => item.key === topic.category) || {
    key: topic.category,
    label: topic.category,
    icon: "•",
  }
}
