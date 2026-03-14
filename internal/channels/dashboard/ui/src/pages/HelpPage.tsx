import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { useSearchParams } from "react-router-dom"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { useToast } from "@/hooks/useToast"
import {
  HELP_CATEGORIES,
  categoryForTopic,
  loadHelpTopics,
  relatedHelpTopics,
  searchHelpTopics,
  type HelpTopic,
} from "@/help/content"
import { extractHeadings, renderMarkdownToFragment } from "@/help/markdown"

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
}

function highlightMatch(text: string, query: string): ReactNode {
  const cleanQuery = String(query || "").trim()
  if (!cleanQuery) {
    return text
  }

  const matcher = new RegExp(`(${escapeRegExp(cleanQuery)})`, "ig")
  const parts = text.split(matcher)
  return parts.map((part, index) => {
    if (part.toLowerCase() === cleanQuery.toLowerCase()) {
      return (
        <mark key={`${part}-${index}`} className="rounded bg-yellow-200 px-0.5 text-foreground dark:bg-yellow-700/80 dark:text-white">
          {part}
        </mark>
      )
    }
    return <span key={`${part}-${index}`}>{part}</span>
  })
}

export function HelpPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [topics, setTopics] = useState<HelpTopic[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [search, setSearch] = useState("")
  const [selectedTopicID, setSelectedTopicID] = useState("")
  const markdownContainerRef = useRef<HTMLDivElement | null>(null)
  const { toast } = useToast()

  const topicParam = (searchParams.get("topic") || "").trim()

  useEffect(() => {
    let isMounted = true
    async function fetchTopics() {
      setLoading(true)
      setError("")
      try {
        const loadedTopics = await loadHelpTopics()
        if (!isMounted) {
          return
        }
        setTopics(loadedTopics)
      } catch (loadError) {
        if (!isMounted) {
          return
        }
        setError(loadError instanceof Error ? loadError.message : String(loadError))
      } finally {
        if (isMounted) {
          setLoading(false)
        }
      }
    }

    void fetchTopics()
    return () => {
      isMounted = false
    }
  }, [])

  useEffect(() => {
    if (!topics.length) {
      return
    }

    if (topicParam) {
      const topic = topics.find((item) => item.id === topicParam)
      if (topic && topic.id !== selectedTopicID) {
        setSelectedTopicID(topic.id)
      }
      if (topic) {
        return
      }
    }

    if (!selectedTopicID || !topics.some((item) => item.id === selectedTopicID)) {
      setSelectedTopicID(topics[0].id)
    }
  }, [selectedTopicID, topicParam, topics])

  const results = useMemo(() => searchHelpTopics(topics, search), [search, topics])

  const selectedTopic = useMemo(() => {
    if (!topics.length) {
      return null
    }
    return results.find((item) => item.id === selectedTopicID) || results[0] || topics.find((item) => item.id === selectedTopicID) || topics[0]
  }, [results, selectedTopicID, topics])

  useEffect(() => {
    if (selectedTopic && selectedTopic.id !== selectedTopicID) {
      setSelectedTopicID(selectedTopic.id)
    }
  }, [selectedTopic, selectedTopicID])

  useEffect(() => {
    const container = markdownContainerRef.current
    if (!container || !selectedTopic) {
      return
    }
    container.innerHTML = ""
    container.append(renderMarkdownToFragment(selectedTopic.body))
  }, [selectedTopic])

  const groupedResults = useMemo(() => {
    const grouped = HELP_CATEGORIES.map((category) => ({
      category,
      items: results.filter((topic) => topic.category === category.key),
    })).filter((group) => group.items.length > 0)

    const knownCategoryKeys = new Set(HELP_CATEGORIES.map((item) => item.key))
    const unknownCategories = new Map<string, HelpTopic[]>()

    results.forEach((topic) => {
      if (knownCategoryKeys.has(topic.category)) {
        return
      }
      const existing = unknownCategories.get(topic.category) || []
      unknownCategories.set(topic.category, [...existing, topic])
    })

    unknownCategories.forEach((items, key) => {
      grouped.push({
        category: {
          key,
          label: key,
          icon: "•",
        },
        items,
      })
    })

    return grouped
  }, [results])

  const headings = useMemo(() => extractHeadings(selectedTopic?.body || ""), [selectedTopic])
  const related = useMemo(() => relatedHelpTopics(topics, selectedTopic), [selectedTopic, topics])

  const setTopicParam = useCallback(
    (topicID: string) => {
      const nextParams = new URLSearchParams(searchParams)
      nextParams.set("topic", topicID)
      setSearchParams(nextParams)
    },
    [searchParams, setSearchParams]
  )

  const navigateToTopic = useCallback(
    (topicID: string) => {
      setSelectedTopicID(topicID)
      setTopicParam(topicID)
    },
    [setTopicParam]
  )

  const scrollToHeading = useCallback((headingID: string) => {
    const target = markdownContainerRef.current?.querySelector<HTMLElement>(`[id="${headingID}"]`)
    target?.scrollIntoView({ behavior: "smooth", block: "start" })
  }, [])

  const copyTopicLink = useCallback(async () => {
    if (!selectedTopic) {
      return
    }
    const url = `${window.location.origin}${window.location.pathname}#/help?topic=${encodeURIComponent(selectedTopic.id)}`

    try {
      await navigator.clipboard.writeText(url)
      toast({ description: "Topic link copied." })
    } catch (_error) {
      toast({
        variant: "destructive",
        description: "Unable to copy topic link.",
      })
    }
  }, [selectedTopic, toast])

  if (loading) {
    return (
      <div className="space-y-2 p-6">
        <h2 className="text-2xl font-semibold tracking-tight">Help Center</h2>
        <p className="text-sm text-muted-foreground">Loading Help Center...</p>
      </div>
    )
  }

  if (error) {
    return (
      <div className="space-y-3 p-6">
        <h2 className="text-2xl font-semibold tracking-tight">Help Center</h2>
        <p className="text-sm text-destructive">{error}</p>
        <Button onClick={() => window.location.reload()} variant="outline">
          Retry
        </Button>
      </div>
    )
  }

  const topicCategory = selectedTopic ? categoryForTopic(selectedTopic) : null

  return (
    <div className="space-y-4 p-6">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Help Center</h2>
        <p className="text-sm text-muted-foreground">
          Searchable, route-aware guidance you can use alongside the rest of the dashboard.
        </p>
      </div>

      <div className="max-w-md">
        <Input
          type="search"
          aria-label="Search help topics"
          placeholder="Search help topics"
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
      </div>

      <section className="grid gap-4 lg:grid-cols-[320px_minmax(0,1fr)]">
        <aside className="rounded-lg border bg-card p-4">
          {groupedResults.length === 0 ? (
            <p className="text-sm text-muted-foreground">No topics match your search.</p>
          ) : (
            <div className="space-y-4">
              {groupedResults.map((group) => (
                <section key={group.category.key} className="space-y-2">
                  <h3 className="text-sm font-semibold text-muted-foreground">
                    {group.category.icon} {group.category.label}
                  </h3>
                  <div className="space-y-1">
                    {group.items.map((topic) => (
                      <button
                        key={topic.id}
                        type="button"
                        onClick={() => navigateToTopic(topic.id)}
                        className={`w-full rounded-md px-3 py-2 text-left text-sm transition-colors ${
                          topic.id === selectedTopic?.id
                            ? "bg-primary text-primary-foreground"
                            : "hover:bg-muted"
                        }`}
                      >
                        {highlightMatch(topic.title, search)}
                      </button>
                    ))}
                  </div>
                </section>
              ))}
            </div>
          )}
        </aside>

        <article className="rounded-lg border bg-card p-5">
          {selectedTopic ? (
            <div className="space-y-4">
              <p className="text-sm text-muted-foreground">
                {topicCategory?.label || selectedTopic.category} / {selectedTopic.title}
              </p>
              <div className="flex flex-wrap items-center justify-between gap-3">
                <h3 className="text-2xl font-semibold tracking-tight">{selectedTopic.title}</h3>
                <Button variant="outline" onClick={() => void copyTopicLink()}>
                  Copy link to topic
                </Button>
              </div>

              {headings.length > 1 && (
                <nav className="space-y-2 rounded-md border bg-muted/40 p-3">
                  <h4 className="text-sm font-semibold">On this page</h4>
                  <div className="space-y-1">
                    {headings.map((heading) => (
                      <button
                        key={heading.id}
                        type="button"
                        onClick={() => scrollToHeading(heading.id)}
                        className={`block text-sm text-muted-foreground hover:text-foreground ${
                          heading.level >= 3 ? "pl-4" : heading.level === 4 ? "pl-6" : ""
                        }`}
                      >
                        {heading.title}
                      </button>
                    ))}
                  </div>
                </nav>
              )}

              <div
                ref={markdownContainerRef}
                className="help-markdown space-y-3 text-sm leading-6 text-foreground [&_a]:text-primary [&_a]:underline [&_a]:underline-offset-2 [&_code]:rounded [&_code]:bg-muted [&_code]:px-1 [&_h2]:mt-5 [&_h2]:text-xl [&_h2]:font-semibold [&_h3]:mt-4 [&_h3]:text-lg [&_h3]:font-semibold [&_h4]:mt-3 [&_h4]:text-base [&_h4]:font-semibold [&_li]:my-1 [&_ol]:list-decimal [&_ol]:space-y-1 [&_ol]:pl-6 [&_p]:text-sm [&_pre]:overflow-x-auto [&_pre]:rounded-md [&_pre]:bg-muted [&_pre]:p-3 [&_ul]:list-disc [&_ul]:space-y-1 [&_ul]:pl-6"
              />

              {related.length > 0 && (
                <section className="space-y-2 pt-2">
                  <h4 className="text-sm font-semibold">Related topics</h4>
                  <div className="flex flex-wrap gap-2">
                    {related.map((topic) => (
                      <Button
                        key={topic.id}
                        type="button"
                        variant="secondary"
                        size="sm"
                        onClick={() => navigateToTopic(topic.id)}
                        className="h-7"
                      >
                        {topic.title}
                      </Button>
                    ))}
                  </div>
                </section>
              )}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">No help topic selected.</p>
          )}
        </article>
      </section>
    </div>
  )
}
