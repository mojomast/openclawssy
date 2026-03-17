import { useCallback, useEffect, useMemo, useState } from "react"
import { ApiError, api } from "@/lib/api"
import type { EvalRun } from "./types"
import { extractErrorMessage, parseEvalRun } from "./utils"

export type EvalRunsState = {
  runs: EvalRun[]
  loading: boolean
  error: string
  expandedRunID: number | null
  expandedRun: EvalRun | null
  featureDisabled: boolean
  featureMessage: string
  loadResults: () => Promise<void>
  toggleRun: (runID: number) => void
}

export function useEvalRuns(featureEnabled = true): EvalRunsState {
  const [runs, setRuns] = useState<EvalRun[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [expandedRunID, setExpandedRunID] = useState<number | null>(null)
  const [featureDisabled, setFeatureDisabled] = useState(false)
  const [featureMessage, setFeatureMessage] = useState("")

  const loadResults = useCallback(async () => {
    if (!featureEnabled) {
      setRuns([])
      setExpandedRunID(null)
      setError("")
      setFeatureDisabled(true)
      setFeatureMessage("Eval is disabled for this control plane.")
      setLoading(false)
      return
    }
    setLoading(true)
    setError("")
    setFeatureDisabled(false)
    setFeatureMessage("")
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
      if (loadError instanceof ApiError && loadError.status === 403 && loadError.code === "feature.eval_disabled") {
        setFeatureDisabled(true)
        setFeatureMessage(loadError.message || "Eval is disabled for this control plane.")
        setError("")
        return
      }
      setError(`Failed to load eval results: ${extractErrorMessage(loadError)}`)
    } finally {
      setLoading(false)
    }
  }, [featureEnabled])

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
    featureDisabled,
    featureMessage,
    loadResults,
    toggleRun,
  }
}
