const HELP_BASE = "/dashboard/static/help";

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
];

export const HELP_CATEGORIES = [
  { key: "Getting Started", label: "Getting Started", icon: "🚀" },
  { key: "Integrations", label: "Integrations", icon: "🔌" },
  { key: "Settings", label: "Settings", icon: "⚙" },
  { key: "Secrets", label: "Secrets", icon: "🔐" },
  { key: "Dashboards", label: "Dashboards", icon: "📊" },
  { key: "Debugging", label: "Debugging", icon: "🧭" },
  { key: "Scheduler", label: "Scheduler", icon: "⏱" },
  { key: "FAQ", label: "FAQ", icon: "❓" },
];

export const HELP_ROUTE_CONTEXT = {
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
};

let topicsPromise = null;

function parseFrontmatter(raw) {
  const text = String(raw || "");
  if (!text.startsWith("---\n")) {
    return { meta: {}, body: text };
  }
  const end = text.indexOf("\n---\n", 4);
  if (end < 0) {
    return { meta: {}, body: text };
  }
  const frontmatter = text.slice(4, end).split("\n");
  const meta = {};
  frontmatter.forEach((line) => {
    const idx = line.indexOf(":");
    if (idx < 0) return;
    const key = line.slice(0, idx).trim();
    const value = line.slice(idx + 1).trim();
    meta[key] = value;
  });
  return { meta, body: text.slice(end + 5) };
}

function normalizeList(value) {
  return String(value || "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

function stripMarkdown(text) {
  return String(text || "")
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/`([^`]+)`/g, "$1")
    .replace(/^#+\s+/gm, "")
    .replace(/^>\s?/gm, "")
    .replace(/\*\*([^*]+)\*\*/g, "$1")
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g, "$1")
    .replace(/[-*]\s+/g, "")
    .replace(/\s+/g, " ")
    .trim();
}

async function loadTopic(fileName) {
  const response = await fetch(`${HELP_BASE}/${fileName}`);
  if (!response.ok) {
    throw new Error(`Failed to load help topic ${fileName}`);
  }
  const raw = await response.text();
  const { meta, body } = parseFrontmatter(raw);
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
  };
}

export async function loadHelpTopics() {
  if (!topicsPromise) {
    topicsPromise = Promise.all(HELP_TOPIC_FILES.map(loadTopic));
  }
  return topicsPromise;
}

export function getHelpTopicParam() {
  const hash = window.location.hash || "";
  const queryIndex = hash.indexOf("?");
  if (queryIndex < 0) {
    return "";
  }
  const params = new URLSearchParams(hash.slice(queryIndex + 1));
  return (params.get("topic") || "").trim();
}

export function setHelpTopicInHash(topicID) {
  const clean = String(topicID || "").trim();
  const query = clean ? `?topic=${encodeURIComponent(clean)}` : "";
  window.location.hash = `#/help${query}`;
}

export function searchHelpTopics(topics, query) {
  const q = String(query || "").trim().toLowerCase();
  if (!q) {
    return topics;
  }
  return topics
    .map((topic) => {
      const haystack = `${topic.title} ${topic.category} ${topic.keywords.join(" ")} ${topic.plainText}`.toLowerCase();
      const index = haystack.indexOf(q);
      return { topic, index };
    })
    .filter((item) => item.index >= 0)
    .sort((left, right) => left.index - right.index)
    .map((item) => item.topic);
}

export function relatedHelpTopics(topics, topic) {
  if (!topic) return [];
  const set = new Set(topic.related_topics || []);
  return topics.filter((item) => set.has(item.id));
}

export function contextualHelpTopics(topics, route) {
  const ids = HELP_ROUTE_CONTEXT[route] || HELP_ROUTE_CONTEXT["/help"] || [];
  return topics.filter((topic) => ids.includes(topic.id));
}

export function categoryForTopic(topic) {
  return HELP_CATEGORIES.find((item) => item.key === topic.category) || { key: topic.category, label: topic.category, icon: "•" };
}
