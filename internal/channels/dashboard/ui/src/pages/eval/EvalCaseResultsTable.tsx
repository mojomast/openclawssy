import { Badge } from "@/components/ui/badge"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import type { EvalRun } from "./types"
import { formatDuration, normalizeTestID } from "./utils"

type EvalCaseResultsTableProps = {
  run: EvalRun
}

export function EvalCaseResultsTable({ run }: EvalCaseResultsTableProps) {
  return (
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
            const isRegression = run.baseline.regressions.some((regression) => regression.testName === caseResult.name)
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
  )
}
