import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import type { EvalMetrics } from "./types"
import { formatDuration, formatPercent } from "./utils"

type EvalMetricsCardsProps = {
  metrics: EvalMetrics
}

export function EvalMetricsCards({ metrics }: EvalMetricsCardsProps) {
  return (
    <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-5">
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-xs font-medium text-muted-foreground">Completion rate</CardTitle>
        </CardHeader>
        <CardContent data-testid="eval-metric-completion-rate" className="text-lg font-semibold">
          {formatPercent(metrics.completionRate)}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-xs font-medium text-muted-foreground">Tool misuse rate</CardTitle>
        </CardHeader>
        <CardContent data-testid="eval-metric-tool-misuse-rate" className="text-lg font-semibold">
          {formatPercent(metrics.toolMisuseRate)}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-xs font-medium text-muted-foreground">Delegation precision</CardTitle>
        </CardHeader>
        <CardContent data-testid="eval-metric-delegation-precision" className="text-lg font-semibold">
          {formatPercent(metrics.delegationPrecision)}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-xs font-medium text-muted-foreground">Token cost</CardTitle>
        </CardHeader>
        <CardContent data-testid="eval-metric-token-cost" className="text-lg font-semibold">
          {metrics.tokenCost.toLocaleString()}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-xs font-medium text-muted-foreground">Time to completion</CardTitle>
        </CardHeader>
        <CardContent data-testid="eval-metric-time-to-completion" className="text-lg font-semibold">
          {formatDuration(metrics.timeToCompletion)}
        </CardContent>
      </Card>
    </div>
  )
}
