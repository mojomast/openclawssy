import { useCallback, useEffect, useState } from "react"
import { ApiError, api } from "@/lib/api"

export type ControlPlaneFeatures = {
  instanceControl: boolean
  instanceAgents: boolean
  wizard: boolean
  eval: boolean
}

const DEFAULT_FEATURES: ControlPlaneFeatures = {
  instanceControl: true,
  instanceAgents: true,
  wizard: true,
  eval: true,
}

function asBool(value: unknown, fallback = true): boolean {
  return typeof value === "boolean" ? value : fallback
}

function parseFeatures(payload: unknown): ControlPlaneFeatures {
  const record = payload && typeof payload === "object" ? (payload as Record<string, unknown>) : {}
  const features = record.features && typeof record.features === "object" ? (record.features as Record<string, unknown>) : {}
  return {
    instanceControl: asBool(features.instance_control, true),
    instanceAgents: asBool(features.instance_agents, true),
    wizard: asBool(features.wizard, true),
    eval: asBool(features.eval, true),
  }
}

export function useControlPlaneFeatures() {
  const [features, setFeatures] = useState<ControlPlaneFeatures>(DEFAULT_FEATURES)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")

  const loadFeatures = useCallback(async () => {
    setLoading(true)
    setError("")
    try {
      const payload = await api.get<unknown>("/api/admin/control-plane/features")
      setFeatures(parseFeatures(payload))
    } catch (loadError) {
      setFeatures(DEFAULT_FEATURES)
      if (loadError instanceof ApiError) {
        setError(loadError.message || "Failed to load control plane features")
      } else if (loadError instanceof Error) {
        setError(loadError.message || "Failed to load control plane features")
      } else {
        setError(String(loadError || "Failed to load control plane features"))
      }
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void loadFeatures()
  }, [loadFeatures])

  return {
    features,
    loading,
    error,
    loadFeatures,
  }
}
