import { useCallback, useEffect, useState } from "react"
import { PageShell } from "@/components/PageShell"
import { api } from "@/lib/api"

type RunsListResponse = {
  runs?: unknown
  total?: unknown
}

function extractErrorMessage(error: unknown): string {
  if (!error || typeof error !== "object") {
    return String(error || "Unknown error")
  }

  const details = (error as { details?: unknown }).details
  if (typeof details === "string" && details.trim().length > 0) {
    return details.trim()
  }

  if (details && typeof details === "object") {
    const detailsMessage = (details as { message?: unknown }).message
    if (typeof detailsMessage === "string" && detailsMessage.trim().length > 0) {
      return detailsMessage.trim()
    }
  }

  const message = (error as { message?: unknown }).message
  if (typeof message === "string" && message.trim().length > 0) {
    return message.trim()
  }

  return "Unknown error"
}

function parseRunCount(payload: RunsListResponse): number {
  const total = Number(payload.total)
  if (Number.isFinite(total) && total >= 0) {
    return total
  }

  if (Array.isArray(payload.runs)) {
    return payload.runs.length
  }

  return 0
}

export function RunsPage() {
  const [loading, setLoading] = useState(true)
  const [runCount, setRunCount] = useState(0)
  const [loadError, setLoadError] = useState("")

  const loadRunsSummary = useCallback(async () => {
    setLoading(true)
    setLoadError("")

    try {
      const payload = await api.get<RunsListResponse>("/v1/runs?limit=1&offset=0")
      setRunCount(parseRunCount(payload))
    } catch (error) {
      setLoadError(extractErrorMessage(error))
      setRunCount(0)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadRunsSummary()
  }, [loadRunsSummary])

  return (
    <div className="space-y-4 p-6" data-testid="runs-page">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Runs</h2>
        <p className="text-sm text-muted-foreground">
          Run history with filtering, pagination, and trace viewer.
        </p>
      </div>

      <PageShell
        loading={loading}
        error={loadError ? `Failed to load runs: ${loadError}` : null}
        onRetry={() => {
          void loadRunsSummary()
        }}
        empty={!loading && !loadError && runCount === 0}
        emptyMessage="No runs found. The React Runs view is still migrating."
      >
        <div className="space-y-2 rounded-lg border bg-card p-4">
          <p className="text-sm font-medium">
            Detected {runCount} run(s). Full React run inspection is still migrating.
          </p>
          <p className="text-sm text-muted-foreground">
            Use the legacy dashboard to filter, paginate, and inspect traces right now.
          </p>
        </div>
      </PageShell>
    </div>
  )
}
