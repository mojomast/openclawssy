import { Fragment, useCallback, useEffect, useMemo, useState } from "react"
import { ApiError, api } from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

type EvalTestResult = {
  passed: boolean
  expected: string
  actual: string
  durationMS: number
  error: string
}

type EvalCaseResult = {
  name: string
  description: string
  result: EvalTestResult
}

type EvalMetrics = {
  completionRate: number
  toolMisuseRate: number
  delegationPrecision: number
  tokenCost: number
  timeToCompletion: number
}

type EvalRegression = {
  testName: string
  baseline: EvalTestResult
  latest: EvalTestResult
}

type EvalRun = {
  id: number
  suite: string
  timestamp: string
  total: number
  passed: number
  failed: number
  status: string
  results: EvalCaseResult[]
  metrics: EvalMetrics
  baseline: {
    available: boolean
    timestamp: string
    regressions: EvalRegression[]
  }
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

function parseEvalRun(payload: unknown): EvalRun | null {
  const record = asRecord(payload)
  const suite = asText(record?.suite).trim()
  if (!suite) {
    return null
  }

  const resultsRaw = Array.isArray(record?.results) ? record.results : []
  const regressionsRaw = Array.isArray(asRecord(record?.baseline)?.regressions)
    ? (asRecord(record?.baseline)?.regressions as unknown[])
    : []
  const metrics = asRecord(record?.metrics)
  const baseline = asRecord(record?.baseline)

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

function formatTimestamp(raw: string): string {
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) {
    return raw || "-"
  }
  return date.toLocaleString()
}

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(1)}%`
}

function formatDuration(ms: number): string {
  if (ms <= 0) {
    return "0ms"
  }
  if (ms < 1000) {
    return `${Math.round(ms)}ms`
  }
  return `${(ms / 1000).toFixed(1)}s`
}

function normalizeTestID(value: string): string {
  const trimmed = value.trim().toLowerCase()
  if (!trimmed) {
    return "unknown"
  }
  return trimmed.replace(/[^a-z0-9-_]+/g, "-")
}

function extractErrorMessage(error: unknown): string {
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

function statusBadgeClass(status: string): string {
  if (status === "fail") {
    return "bg-destructive text-destructive-foreground"
  }
  return "bg-emerald-600 text-white"
}

export function EvalPage() {
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

  return (
    <div className="space-y-4 p-6" data-testid="eval-page">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Eval Results</h2>
        <p className="text-sm text-muted-foreground">
          Review eval suite history, inspect case-level outcomes, and compare runs against saved baselines.
        </p>
      </div>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="text-base">Suite run history</CardTitle>
            <Button type="button" variant="outline" size="sm" onClick={() => void loadResults()} disabled={loading}>
              Refresh
            </Button>
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          {loading ? <p className="text-sm text-muted-foreground">Loading eval results...</p> : null}

          {!loading && error ? (
            <div className="rounded-md border border-destructive/50 bg-destructive/5 p-3">
              <p className="text-sm text-destructive">{error}</p>
              <Button type="button" size="sm" variant="outline" className="mt-2" onClick={() => void loadResults()}>
                Retry
              </Button>
            </div>
          ) : null}

          {!loading && !error && runs.length === 0 ? (
            <p className="text-sm text-muted-foreground">No eval results found.</p>
          ) : null}

          {!loading && !error && runs.length > 0 ? (
            <Table data-testid="eval-history-table">
              <TableHeader>
                <TableRow>
                  <TableHead>Timestamp</TableHead>
                  <TableHead>Suite</TableHead>
                  <TableHead className="text-right">Passed</TableHead>
                  <TableHead className="text-right">Failed</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Details</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {runs.map((run) => {
                  const isExpanded = run.id === expandedRunID
                  return (
                    <Fragment key={`fragment-${run.id}`}>
                      <TableRow key={`row-${run.id}`} data-testid={`eval-run-row-${run.id}`}>
                        <TableCell>{formatTimestamp(run.timestamp)}</TableCell>
                        <TableCell className="font-medium">{run.suite}</TableCell>
                        <TableCell className="text-right">{run.passed}</TableCell>
                        <TableCell className="text-right">{run.failed}</TableCell>
                        <TableCell>
                          <Badge className={statusBadgeClass(run.status)}>{run.status === "fail" ? "FAIL" : "PASS"}</Badge>
                        </TableCell>
                        <TableCell className="text-right">
                          <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            data-testid={`eval-run-toggle-${run.id}`}
                            onClick={() => {
                              setExpandedRunID((current) => (current === run.id ? null : run.id))
                            }}
                          >
                            {isExpanded ? "Collapse" : "Expand"}
                          </Button>
                        </TableCell>
                      </TableRow>

                      {isExpanded ? (
                        <TableRow key={`details-${run.id}`} data-testid={`eval-run-details-${run.id}`}>
                          <TableCell colSpan={6} className="space-y-4 bg-muted/20">
                            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-5">
                              <Card>
                                <CardHeader className="pb-2">
                                  <CardTitle className="text-xs font-medium text-muted-foreground">Completion rate</CardTitle>
                                </CardHeader>
                                <CardContent data-testid="eval-metric-completion-rate" className="text-lg font-semibold">
                                  {formatPercent(run.metrics.completionRate)}
                                </CardContent>
                              </Card>

                              <Card>
                                <CardHeader className="pb-2">
                                  <CardTitle className="text-xs font-medium text-muted-foreground">Tool misuse rate</CardTitle>
                                </CardHeader>
                                <CardContent data-testid="eval-metric-tool-misuse-rate" className="text-lg font-semibold">
                                  {formatPercent(run.metrics.toolMisuseRate)}
                                </CardContent>
                              </Card>

                              <Card>
                                <CardHeader className="pb-2">
                                  <CardTitle className="text-xs font-medium text-muted-foreground">Delegation precision</CardTitle>
                                </CardHeader>
                                <CardContent data-testid="eval-metric-delegation-precision" className="text-lg font-semibold">
                                  {formatPercent(run.metrics.delegationPrecision)}
                                </CardContent>
                              </Card>

                              <Card>
                                <CardHeader className="pb-2">
                                  <CardTitle className="text-xs font-medium text-muted-foreground">Token cost</CardTitle>
                                </CardHeader>
                                <CardContent data-testid="eval-metric-token-cost" className="text-lg font-semibold">
                                  {run.metrics.tokenCost.toLocaleString()}
                                </CardContent>
                              </Card>

                              <Card>
                                <CardHeader className="pb-2">
                                  <CardTitle className="text-xs font-medium text-muted-foreground">Time to completion</CardTitle>
                                </CardHeader>
                                <CardContent data-testid="eval-metric-time-to-completion" className="text-lg font-semibold">
                                  {formatDuration(run.metrics.timeToCompletion)}
                                </CardContent>
                              </Card>
                            </div>

                            <div className="space-y-2">
                              <h3 className="text-sm font-semibold">Test case results</h3>
                              <Table>
                                <TableHeader>
                                  <TableRow>
                                    <TableHead>Case</TableHead>
                                    <TableHead>Status</TableHead>
                                    <TableHead>Expected</TableHead>
                                    <TableHead>Actual</TableHead>
                                    <TableHead>Error</TableHead>
                                    <TableHead className="text-right">Duration</TableHead>
                                  </TableRow>
                                </TableHeader>
                                <TableBody>
                                  {run.results.map((caseResult) => {
                                    const caseID = normalizeTestID(caseResult.name)
                                    const isRegression = run.baseline.regressions.some(
                                      (regression) => regression.testName === caseResult.name
                                    )
                                    return (
                                      <TableRow
                                        key={caseResult.name}
                                        data-testid={`eval-case-row-${caseID}`}
                                        className={isRegression ? "bg-red-500/10 text-red-700 dark:text-red-300" : ""}
                                      >
                                        <TableCell className="font-medium">{caseResult.name}</TableCell>
                                        <TableCell>
                                          <Badge className={caseResult.result.passed ? "bg-emerald-600 text-white" : "bg-destructive text-destructive-foreground"}>
                                            {caseResult.result.passed ? "PASS" : "FAIL"}
                                          </Badge>
                                        </TableCell>
                                        <TableCell>{caseResult.result.expected || "-"}</TableCell>
                                        <TableCell>{caseResult.result.actual || "-"}</TableCell>
                                        <TableCell>{caseResult.result.error || "-"}</TableCell>
                                        <TableCell className="text-right">{formatDuration(caseResult.result.durationMS)}</TableCell>
                                      </TableRow>
                                    )
                                  })}
                                </TableBody>
                              </Table>
                            </div>

                            <div className="space-y-2">
                              <h3 className="text-sm font-semibold">Baseline comparison</h3>
                              {!run.baseline.available ? (
                                <p className="text-sm text-muted-foreground">No baseline saved for this suite.</p>
                              ) : run.baseline.regressions.length === 0 ? (
                                <p className="text-sm text-emerald-600">No regressions detected against baseline.</p>
                              ) : (
                                <div className="space-y-2">
                                  <p className="text-xs text-muted-foreground">
                                    Baseline timestamp: {formatTimestamp(run.baseline.timestamp)}
                                  </p>
                                  <Table>
                                    <TableHeader>
                                      <TableRow>
                                        <TableHead>Test</TableHead>
                                        <TableHead>Baseline actual</TableHead>
                                        <TableHead>Latest actual</TableHead>
                                        <TableHead>Baseline status</TableHead>
                                        <TableHead>Latest status</TableHead>
                                      </TableRow>
                                    </TableHeader>
                                    <TableBody>
                                      {run.baseline.regressions.map((regression) => {
                                        const rowID = normalizeTestID(regression.testName)
                                        return (
                                          <TableRow
                                            key={regression.testName}
                                            data-testid={`eval-regression-row-${rowID}`}
                                            className="bg-red-500/10 text-red-700 dark:text-red-300"
                                          >
                                            <TableCell className="font-medium">{regression.testName}</TableCell>
                                            <TableCell>{regression.baseline.actual || "-"}</TableCell>
                                            <TableCell>{regression.latest.actual || "-"}</TableCell>
                                            <TableCell>PASS</TableCell>
                                            <TableCell>FAIL</TableCell>
                                          </TableRow>
                                        )
                                      })}
                                    </TableBody>
                                  </Table>
                                </div>
                              )}
                            </div>
                          </TableCell>
                        </TableRow>
                      ) : null}
                    </Fragment>
                  )
                })}
              </TableBody>
            </Table>
          ) : null}
        </CardContent>
      </Card>

      {expandedRun && expandedRun.failed > 0 ? (
        <p className="text-xs text-muted-foreground">
          Highlighted red rows indicate regressions where a test passed in baseline but fails in the selected run.
        </p>
      ) : null}
    </div>
  )
}
