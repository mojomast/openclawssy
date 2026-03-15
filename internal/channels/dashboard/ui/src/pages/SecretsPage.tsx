import { FormEvent, useCallback, useEffect, useMemo, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { api } from "@/lib/api"

type SecretsResponse = {
  keys?: unknown[]
}

const CONVENTIONS: Array<{ key: string; note: string }> = [
  {
    key: "PERPLEXITY_API_KEY",
    note: "Perplexity provider key when model/provider config points to this env name.",
  },
  {
    key: "OPENAI_API_KEY",
    note: "OpenAI-compatible providers often default to this env key.",
  },
  {
    key: "OPENROUTER_API_KEY",
    note: "Use for OpenRouter when providers.openrouter.api_key_env matches.",
  },
  {
    key: "REQUESTY_API_KEY",
    note: "Use for Requesty integrations when configured as api_key_env.",
  },
  {
    key: "HATZ_API_KEY",
    note: "Use for Hatz when providers.hatz.api_key_env matches this env-style key.",
  },
  {
    key: "provider/hatz/api_key",
    note: "Recommended encrypted secret-store key for Hatz model access and model discovery.",
  },
  {
    key: "ZAI_API_KEY",
    note: "Use when model.provider is zai and api_key_env expects this key.",
  },
  {
    key: "discord/bot_token",
    note: "Recommended Discord bot token key for dashboard-guided setup. Write here to use the encrypted secret store.",
  },
  {
    key: "DISCORD_BOT_TOKEN",
    note: "Optional external env fallback used only when discord.token_env points here and no encrypted discord/bot_token secret is stored.",
  },
  {
    key: "TELEGRAM_BOT_TOKEN",
    note: "Env-style Telegram bot token key; keep token private and rotate regularly.",
  },
  {
    key: "telegram/bot_token",
    note: "Slash-path Telegram bot token key used by the encrypted secret store.",
  },
]

function normalizeKeys(payload: SecretsResponse): string[] {
  if (!Array.isArray(payload.keys)) {
    return []
  }
  return payload.keys
    .filter((item): item is string => typeof item === "string")
    .map((item) => item.trim())
    .filter((item) => item.length > 0)
    .sort((a, b) => a.localeCompare(b))
}

function matchesFilter(value: string, query: string): boolean {
  const needle = String(query || "").trim().toLowerCase()
  if (!needle) {
    return true
  }
  return value.toLowerCase().includes(needle)
}

async function copyText(value: string): Promise<void> {
  if (!value) {
    return
  }

  if (navigator.clipboard && typeof navigator.clipboard.writeText === "function") {
    await navigator.clipboard.writeText(value)
    return
  }

  const input = document.createElement("textarea")
  input.value = value
  input.setAttribute("readonly", "readonly")
  input.style.position = "absolute"
  input.style.left = "-9999px"
  document.body.append(input)
  input.select()
  document.execCommand("copy")
  document.body.removeChild(input)
}

export function SecretsPage() {
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState("")
  const [keys, setKeys] = useState<string[]>([])
  const [filter, setFilter] = useState("")

  const [formName, setFormName] = useState("")
  const [formValue, setFormValue] = useState("")
  const [formError, setFormError] = useState("")
  const [formSuccess, setFormSuccess] = useState("")
  const [saving, setSaving] = useState(false)

  const [deletingKey, setDeletingKey] = useState("")
  const [deleteError, setDeleteError] = useState("")
  const [deleteSuccess, setDeleteSuccess] = useState("")
  const [copyFeedback, setCopyFeedback] = useState("")

  const loadKeys = useCallback(async () => {
    setLoading(true)
    setLoadError("")
    try {
      const payload = await api.get<SecretsResponse>("/api/admin/secrets")
      setKeys(normalizeKeys(payload))
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : String(error))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadKeys()
  }, [loadKeys])

  const filteredKeys = useMemo(() => keys.filter((key) => matchesFilter(key, filter)), [keys, filter])

  const handleStoreSecret = useCallback(async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()

    const name = formName.trim()
    const value = formValue

    setFormError("")
    setFormSuccess("")
    setDeleteError("")
    setDeleteSuccess("")

    if (!name || !value) {
      setFormError("Name and value are required.")
      return
    }

    setSaving(true)
    try {
      await api.post("/api/admin/secrets", { name, value })
      setFormSuccess(`Stored key: ${name}`)
      setFormValue("")
      await loadKeys()
    } catch (error) {
      setFormError(error instanceof Error ? error.message : String(error))
    } finally {
      setSaving(false)
    }
  }, [formName, formValue, loadKeys])

  const handleDeleteSecret = useCallback(async (key: string) => {
    if (!key) {
      return
    }
    if (!window.confirm(`Delete stored key ${key}? This cannot be undone.`)) {
      return
    }

    setDeletingKey(key)
    setDeleteError("")
    setDeleteSuccess("")

    try {
      await api.delete(`/api/admin/secrets/${encodeURIComponent(key)}`)
      setDeleteSuccess(`Deleted key: ${key}`)
      await loadKeys()
    } catch (error) {
      setDeleteError(error instanceof Error ? error.message : String(error))
    } finally {
      setDeletingKey("")
    }
  }, [loadKeys])

  const handleCopyName = useCallback(async (key: string) => {
    try {
      await copyText(key)
      setCopyFeedback(`Copied: ${key}`)
    } catch (error) {
      setCopyFeedback(`Copy failed: ${error instanceof Error ? error.message : String(error)}`)
    }
  }, [])

  return (
    <div className="space-y-4 p-6" data-testid="secrets-page">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Secrets</h2>
        <p className="text-sm text-muted-foreground">
          Manage secret key names and rotate values without exposing stored secret contents.
        </p>
      </div>

      <section className="grid gap-4 xl:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)_minmax(0,1fr)]">
        <article className="space-y-3 rounded-lg border bg-card p-4">
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-lg font-semibold">Stored keys</h3>
            <p className="text-sm text-muted-foreground">{keys.length} total</p>
          </div>

          <label htmlFor="secrets-search" className="space-y-1 text-sm">
            <span>Search key names</span>
            <Input
              id="secrets-search"
              type="search"
              placeholder="PERPLEXITY_API_KEY"
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
            />
          </label>

          {loading ? (
            <p className="text-sm text-muted-foreground">Loading keys...</p>
          ) : loadError ? (
            <div className="space-y-3">
              <p className="text-sm text-destructive">Failed to load keys: {loadError}</p>
              <Button type="button" variant="outline" onClick={() => void loadKeys()}>
                Retry
              </Button>
            </div>
          ) : filteredKeys.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              {keys.length ? "No keys match this search." : "No keys stored yet. Add a key using the form."}
            </p>
          ) : (
            <ul className="space-y-2">
              {filteredKeys.map((key) => (
                <li key={key} className="rounded-md border bg-muted/30 p-3">
                  <div className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
                    <code className="text-xs md:text-sm">{key}</code>
                    <div className="flex flex-wrap gap-2">
                      <Button type="button" variant="outline" size="sm" onClick={() => void handleCopyName(key)}>
                        Copy name
                      </Button>
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        disabled={deletingKey === key}
                        onClick={() => void handleDeleteSecret(key)}
                      >
                        {deletingKey === key ? "Deleting..." : "Delete key"}
                      </Button>
                    </div>
                  </div>
                </li>
              ))}
            </ul>
          )}

          {copyFeedback && <p className="text-sm text-muted-foreground">{copyFeedback}</p>}
          {deleteError && <p className="text-sm text-destructive">{deleteError}</p>}
          {deleteSuccess && <p className="text-sm text-emerald-600 dark:text-emerald-400">{deleteSuccess}</p>}
        </article>

        <article className="space-y-3 rounded-lg border bg-card p-4">
          <h3 className="text-lg font-semibold">Set or rotate secret</h3>
          <p className="text-sm text-muted-foreground">Values are write-only. Existing secret values are never shown here.</p>

          <form className="space-y-3" onSubmit={handleStoreSecret}>
            <label htmlFor="secret-key-name" className="space-y-1 text-sm">
              <span>Secret key name</span>
              <Input
                id="secret-key-name"
                type="text"
                placeholder="PERPLEXITY_API_KEY"
                value={formName}
                onChange={(event) => {
                  setFormName(event.target.value)
                  setFormError("")
                  setFormSuccess("")
                }}
              />
            </label>

            <label htmlFor="secret-value" className="space-y-1 text-sm">
              <span>Secret value</span>
              <Input
                id="secret-value"
                type="password"
                autoComplete="new-password"
                placeholder="Enter new secret value"
                value={formValue}
                onChange={(event) => {
                  setFormValue(event.target.value)
                  setFormError("")
                  setFormSuccess("")
                }}
              />
            </label>

            <Button type="submit" disabled={saving}>
              {saving ? "Saving..." : "Store Secret"}
            </Button>
          </form>

          {formError && <p className="text-sm text-destructive">{formError}</p>}
          {formSuccess && <p className="text-sm text-emerald-600 dark:text-emerald-400">{formSuccess}</p>}
        </article>

        <article className="space-y-3 rounded-lg border bg-card p-4">
          <h3 className="text-lg font-semibold">Naming conventions</h3>
          <p className="text-sm text-muted-foreground">
            Use exact key names referenced by config. Recommended Discord setup stores the token at discord/bot_token;
            discord.token_env is only for external environment fallback. Values remain write-only in this UI.
          </p>

          <ul className="space-y-2">
            {CONVENTIONS.map((item) => (
              <li key={item.key} className="rounded-md border bg-muted/30 p-3">
                <div className="space-y-2">
                  <code className="block text-xs md:text-sm">{item.key}</code>
                  <p className="text-sm text-muted-foreground">{item.note}</p>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      setFormName(item.key)
                      setFormError("")
                      setFormSuccess("")
                    }}
                  >
                    Use key
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        </article>
      </section>
    </div>
  )
}
