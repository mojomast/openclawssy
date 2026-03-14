import { ApiError } from "@/lib/api"
import type { EvalCaseResult, EvalRegression, EvalRun, EvalTestResult } from "./types"

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

function asNumber(value: unknown): number {
  const parsed = Number(value)
  if (!Number.isFinite(parsed)) {
    return 0
  }
  return parsed
}

function asBool(value: unknown): boolean {
  return Boolean(value)
}

function parseTestResult(payload: unknown): EvalTestResult {
  const record = asRecord(payload)
  return {
    passed: asBool(record?.passed),
    expected: asText(record?.expected),
    actual: asText(record?.actual),
    durationMS: asNumber(record?.duration_ms),
    error: asText(record?.error),
  }
}

function parseCaseResult(payload: unknown): EvalCaseResult | null {
  const record = asRecord(payload)
  const name = asText(record?.name).trim()
  if (!name) {
    return null
  }
  return {
    name,
    description: asText(record?.description),
    result: parseTestResult(record?.result),
  }
}

function parseRegression(payload: unknown): EvalRegression | null {
  const record = asRecord(payload)
  const testName = asText(record?.test_name).trim()
  if (!testName) {
    return null
  }
  return {
    testName,
    baseline: parseTestResult(record?.baseline),
    latest: parseTestResult(record?.latest),
  }
}

export function parseEvalRun(payload: unknown): EvalRun | null {
  const record = asRecord(payload)
  const suite = asText(record?.suite).trim()
  if (!suite) {
    return null
  }

  const resultsRaw = Array.isArray(record?.results) ? record.results : []
  const baseline = asRecord(record?.baseline)
  const regressionsRaw = Array.isArray(baseline?.regressions) ? baseline.regressions : []
  const metrics = asRecord(record?.metrics)

  return {
    id: asNumber(record?.id),
    suite,
    timestamp: asText(record?.timestamp),
    total: asNumber(record?.total),
    passed: asNumber(record?.passed),
    failed: asNumber(record?.failed),
    status: asText(record?.status).trim().toLowerCase(),
    results: resultsRaw.map(parseCaseResult).filter((row): row is EvalCaseResult => row !== null),
    metrics: {
      completionRate: asNumber(metrics?.completion_rate),
      toolMisuseRate: asNumber(metrics?.tool_misuse_rate),
      delegationPrecision: asNumber(metrics?.delegation_precision),
      tokenCost: asNumber(metrics?.token_cost),
      timeToCompletion: asNumber(metrics?.time_to_completion),
    },
    baseline: {
      available: asBool(baseline?.available),
      timestamp: asText(baseline?.timestamp),
      regressions: regressionsRaw.map(parseRegression).filter((row): row is EvalRegression => row !== null),
    },
  }
}

export function formatTimestamp(raw: string): string {
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) {
    return raw || "-"
  }
  return date.toLocaleString()
}

export function formatPercent(value: number): string {
  return `${(value * 100).toFixed(1)}%`
}

export function formatDuration(ms: number): string {
  if (ms <= 0) {
    return "0ms"
  }
  if (ms < 1000) {
    return `${Math.round(ms)}ms`
  }
  return `${(ms / 1000).toFixed(1)}s`
}

export function normalizeTestID(value: string): string {
  const trimmed = value.trim().toLowerCase()
  if (!trimmed) {
    return "unknown"
  }
  return trimmed.replace(/[^a-z0-9-_]+/g, "-")
}

export function extractErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    const details = asRecord(error.details)
    const nestedError = asRecord(details?.error)
    const nestedMessage = asText(nestedError?.message).trim()
    if (nestedMessage) {
      return nestedMessage
    }
    const detailsMessage = asText(details?.message).trim()
    if (detailsMessage) {
      return detailsMessage
    }
  }

  if (error instanceof Error) {
    return error.message || "Unknown error"
  }
  return asText(error).trim() || "Unknown error"
}

export function statusBadgeClass(status: string): string {
  if (status === "fail") {
    return "bg-destructive text-destructive-foreground"
  }
  return "bg-emerald-600 text-white"
}
