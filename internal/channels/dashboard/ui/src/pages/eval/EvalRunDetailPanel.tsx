import { TableCell } from "@/components/ui/table"
import type { EvalRun } from "./types"
import { EvalBaselineComparison } from "./EvalBaselineComparison"
import { EvalCaseResultsTable } from "./EvalCaseResultsTable"
import { EvalMetricsCards } from "./EvalMetricsCards"

type EvalRunDetailPanelProps = {
  run: EvalRun
}

export function EvalRunDetailPanel({ run }: EvalRunDetailPanelProps) {
  return (
    <TableCell colSpan={6} className="space-y-4 bg-muted/20">
      <EvalMetricsCards metrics={run.metrics} />
      <EvalCaseResultsTable run={run} />
      <EvalBaselineComparison run={run} />
    </TableCell>
  )
}
