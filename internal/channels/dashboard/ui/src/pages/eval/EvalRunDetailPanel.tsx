import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { TableCell } from "@/components/ui/table"
import type { EvalRun } from "./types"
import { EvalBaselineComparison } from "./EvalBaselineComparison"
import { EvalCaseResultsTable } from "./EvalCaseResultsTable"
import { EvalMetricsCards } from "./EvalMetricsCards"
import { countDelegationEvents, formatValue, summarizeDelegationMode } from "./utils"

type EvalRunDetailPanelProps = {
  run: EvalRun
}

export function EvalRunDetailPanel({ run }: EvalRunDetailPanelProps) {
  const detailRows = [
    ["Instance", run.identity.instanceID],
    ["Agent", run.identity.agentID],
    ["Run", run.identity.runID],
    ["Parent run", run.identity.parentRunID],
    ["Root run", run.identity.rootRunID],
    ["Source", run.identity.source],
    ["Task", run.identity.taskID],
    ["Session", run.identity.sessionID],
    ["Artifact", run.metadata.artifactPath],
    ["Checkpoint", run.metadata.checkpointPath],
  ]

  return (
    <TableCell colSpan={6} className="space-y-4 bg-muted/20">
      <EvalMetricsCards metrics={run.metrics} />
      <Card data-testid={`eval-run-identity-card-${run.id}`}>
        <CardHeader className="pb-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <CardTitle className="text-sm">Identity and delegation</CardTitle>
            <div className="flex flex-wrap gap-2">
              <Badge variant="outline" data-testid={`eval-run-delegation-mode-${run.id}`}>
                {summarizeDelegationMode(run.metadata.decompositionPlan)}
              </Badge>
              <Badge variant="secondary" data-testid={`eval-run-delegation-count-${run.id}`}>
                {countDelegationEvents(run.metadata.delegationEvents)} delegation events
              </Badge>
            </div>
          </div>
        </CardHeader>
        <CardContent className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {detailRows.map(([label, value]) => (
            <div key={label} className="space-y-1">
              <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{label}</p>
              <p className="text-sm break-all" data-testid={`eval-run-${label.toLowerCase().replace(/[^a-z0-9]+/g, "-")}-${run.id}`}>
                {formatValue(value)}
              </p>
            </div>
          ))}
          <div className="space-y-1 md:col-span-2 xl:col-span-3">
            <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Delegation summary</p>
            <p className="text-sm text-muted-foreground" data-testid={`eval-run-delegation-summary-${run.id}`}>
              {run.metadata.delegationEvents.length > 0
                ? run.metadata.delegationEvents
                    .map((event) => formatValue(String(event.outcome ?? event.event ?? event.type ?? "delegated")))
                    .join(", ")
                : "-"}
            </p>
          </div>
        </CardContent>
      </Card>
      <EvalCaseResultsTable run={run} />
      <EvalBaselineComparison run={run} />
    </TableCell>
  )
}
