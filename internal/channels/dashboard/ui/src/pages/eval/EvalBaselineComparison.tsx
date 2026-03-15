import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import type { EvalRun } from "./types"
import { formatTimestamp, normalizeTestID } from "./utils"

type EvalBaselineComparisonProps = {
  run: EvalRun
}

export function EvalBaselineComparison({ run }: EvalBaselineComparisonProps) {
  return (
    <div className="space-y-2">
      <h3 className="text-sm font-semibold">Baseline comparison</h3>
      {!run.baseline.available ? (
        <p className="text-sm text-muted-foreground">No baseline saved for this suite.</p>
      ) : run.baseline.regressions.length === 0 ? (
        <p className="text-sm text-emerald-600">No regressions detected against baseline.</p>
      ) : (
        <div className="space-y-2">
          <p className="text-xs text-muted-foreground">Baseline timestamp: {formatTimestamp(run.baseline.timestamp)}</p>
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
  )
}
