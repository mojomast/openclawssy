import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { EvalHistoryTable } from "./eval/EvalHistoryTable"
import { useEvalRuns } from "./eval/useEvalRuns"

export function EvalPage() {
  const { runs, loading, error, expandedRun, expandedRunID, loadResults, toggleRun } = useEvalRuns()

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
            <EvalHistoryTable runs={runs} expandedRunID={expandedRunID} onToggleRun={toggleRun} />
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
