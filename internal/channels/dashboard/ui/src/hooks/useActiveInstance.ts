import { useCallback, useEffect, useState } from "react"
import { api } from "@/lib/api"

type ActiveInstance = {
  id: string
  name: string
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
  return String(value).trim()
}

export function useActiveInstance(enabled = true) {
  const [instance, setInstance] = useState<ActiveInstance | null>(null)
  const [loading, setLoading] = useState(true)

  const loadActiveInstance = useCallback(async () => {
    if (!enabled) {
      setInstance(null)
      setLoading(false)
      return
    }
    setLoading(true)
    try {
      const payload = await api.get<{ instance?: unknown }>("/api/admin/instances/active")
      const record = asRecord(payload.instance)
      const id = asText(record?.id)
      if (!id) {
        setInstance(null)
      } else {
        setInstance({ id, name: asText(record?.name) || id })
      }
    } catch {
      setInstance(null)
    } finally {
      setLoading(false)
    }
  }, [enabled])

  useEffect(() => {
    void loadActiveInstance()
  }, [loadActiveInstance])

  return {
    instance,
    loading,
    loadActiveInstance,
  }
}
