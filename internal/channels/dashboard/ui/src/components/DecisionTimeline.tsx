import { Badge } from "@/components/ui/badge"
import { type FlattenedDecisionRecord, formatDecisionTimestamp } from "@/lib/decisions"

export function DecisionTimeline({
  records,
  emptyLabel,
  testID,
}: {
  records: FlattenedDecisionRecord[]
  emptyLabel: string
  testID: string
}) {
  if (records.length === 0) {
    return <p className="text-sm text-muted-foreground">{emptyLabel}</p>
  }

  return (
    <div data-testid={testID} className="space-y-2">
      {records.map((record, index) => (
        <div key={`${record.runID}-${record.recordType}-${record.timestamp}-${index}`} className="rounded-md border p-3">
          <div className="mb-2 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <Badge variant="outline">{record.recordType}</Badge>
            <span>{formatDecisionTimestamp(record.timestamp)}</span>
            <span>run: {record.runID}</span>
            {record.agentID ? <span>agent: {record.agentID}</span> : null}
          </div>
          <p
            className="text-sm"
            style={{ marginLeft: `${record.depth * 10}px` }}
          >
            {record.humanSummary || "No human summary provided."}
          </p>
        </div>
      ))}
    </div>
  )
}
