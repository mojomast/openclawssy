import { Fragment } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import type { EvalRun } from "./types"
import { EvalRunDetailPanel } from "./EvalRunDetailPanel"
import { formatTimestamp, statusBadgeClass } from "./utils"

type EvalHistoryTableProps = {
  runs: EvalRun[]
  expandedRunID: number | null
  onToggleRun: (runID: number) => void
}

export function EvalHistoryTable({ runs, expandedRunID, onToggleRun }: EvalHistoryTableProps) {
  return (
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
                    onClick={() => onToggleRun(run.id)}
                  >
                    {isExpanded ? "Collapse" : "Expand"}
                  </Button>
                </TableCell>
              </TableRow>

              {isExpanded ? (
                <TableRow key={`details-${run.id}`} data-testid={`eval-run-details-${run.id}`}>
                  <EvalRunDetailPanel run={run} />
                </TableRow>
              ) : null}
            </Fragment>
          )
        })}
      </TableBody>
    </Table>
  )
}
