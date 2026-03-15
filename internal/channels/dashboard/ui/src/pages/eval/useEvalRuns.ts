import { useCallback, useEffect, useMemo, useState } from "react"
import { api } from "@/lib/api"
import type { EvalRun } from "./types"
import { extractErrorMessage, parseEvalRun } from "./utils"

export function useEvalRuns() {
  const [runs, setRuns] = useState<EvalRun[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [expandedRunID, setExpandedRunID] = useState<number | null>(null)

  const loadResults = useCallback(async () => {
    setLoading(true)
    setError("")
    try {
      const payload = await api.get<{ runs?: unknown }>("/api/admin/eval/results?limit=50")
      const parsedRuns = Array.isArray(payload.runs)
        ? payload.runs.map(parseEvalRun).filter((run): run is EvalRun => run !== null)
        : []
      setRuns(parsedRuns)
      setExpandedRunID((previous) => {
        if (previous !== null && parsedRuns.some((run) => run.id === previous)) {
          return previous
        }
        return parsedRuns[0]?.id ?? null
      })
    } catch (loadError) {
      setRuns([])
      setExpandedRunID(null)
      setError(`Failed to load eval results: ${extractErrorMessage(loadError)}`)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadResults()
  }, [loadResults])

  const expandedRun = useMemo(
    () => runs.find((run) => run.id === expandedRunID) || null,
    [runs, expandedRunID]
  )

  const toggleRun = useCallback((runID: number) => {
    setExpandedRunID((current) => (current === runID ? null : runID))
  }, [])

  return {
    runs,
    loading,
    error,
    expandedRunID,
    expandedRun,
    loadResults,
    toggleRun,
  }
}
