import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"

export type DecompositionTaskNode = {
  taskID: string
  description: string
  assignedRole: string
  confidence: number
  dependsOn: string[]
}

export type PlanDependencyEdge = {
  fromTaskID: string
  toTaskID: string
}

export type DecompositionPlan = {
  delegationMode: string
  triggerReason: string
  tasks: DecompositionTaskNode[]
  dependencyDAG: PlanDependencyEdge[]
  minConfidence: number
  avgConfidence: number
  allRolesBuiltIn: boolean
  generatedAt: string
}

type PositionedNode = {
  task: DecompositionTaskNode
  x: number
  y: number
  width: number
  height: number
}

type PositionedEdge = {
  fromTaskID: string
  toTaskID: string
  fromX: number
  fromY: number
  toX: number
  toY: number
}

const NODE_WIDTH = 232
const NODE_HEIGHT = 112
const HORIZONTAL_GAP = 88
const VERTICAL_GAP = 36
const PADDING = 24

function toTestIDSegment(value: string): string {
  return value.trim().toLowerCase().replace(/[^a-z0-9_-]+/g, "-").replace(/^-+|-+$/g, "") || "node"
}

function confidenceBadgeClass(confidence: number): string {
  if (confidence >= 0.85) {
    return "bg-emerald-600 text-white"
  }
  if (confidence >= 0.7) {
    return "bg-amber-500 text-black"
  }
  return "bg-destructive text-destructive-foreground"
}

function buildLevels(tasks: DecompositionTaskNode[]): Map<string, number> {
  const byID = new Map<string, DecompositionTaskNode>()
  const levelByID = new Map<string, number>()

  for (const task of tasks) {
    byID.set(task.taskID, task)
  }

  const computeLevel = (taskID: string, lineage: Set<string>): number => {
    if (levelByID.has(taskID)) {
      return levelByID.get(taskID) || 0
    }
    if (lineage.has(taskID)) {
      return 0
    }

    const task = byID.get(taskID)
    if (!task) {
      return 0
    }

    const nextLineage = new Set(lineage)
    nextLineage.add(taskID)

    let level = 0
    for (const dependencyID of task.dependsOn) {
      const dependencyLevel = computeLevel(dependencyID, nextLineage)
      if (dependencyLevel + 1 > level) {
        level = dependencyLevel + 1
      }
    }

    levelByID.set(taskID, level)
    return level
  }

  for (const task of tasks) {
    computeLevel(task.taskID, new Set())
  }

  return levelByID
}

function uniqueEdges(plan: DecompositionPlan): PlanDependencyEdge[] {
  const seen = new Set<string>()
  const edges: PlanDependencyEdge[] = []

  const explicitEdges = Array.isArray(plan.dependencyDAG) ? plan.dependencyDAG : []
  for (const edge of explicitEdges) {
    const fromTaskID = edge.fromTaskID.trim()
    const toTaskID = edge.toTaskID.trim()
    if (!fromTaskID || !toTaskID) {
      continue
    }
    const key = `${fromTaskID}->${toTaskID}`
    if (seen.has(key)) {
      continue
    }
    seen.add(key)
    edges.push({ fromTaskID, toTaskID })
  }

  for (const task of plan.tasks) {
    for (const dep of task.dependsOn) {
      const fromTaskID = dep.trim()
      const toTaskID = task.taskID.trim()
      if (!fromTaskID || !toTaskID) {
        continue
      }
      const key = `${fromTaskID}->${toTaskID}`
      if (seen.has(key)) {
        continue
      }
      seen.add(key)
      edges.push({ fromTaskID, toTaskID })
    }
  }

  return edges
}

function buildGraphLayout(plan: DecompositionPlan): {
  nodes: PositionedNode[]
  edges: PositionedEdge[]
  width: number
  height: number
} {
  const levelByID = buildLevels(plan.tasks)

  const groupedByLevel = new Map<number, DecompositionTaskNode[]>()
  for (const task of plan.tasks) {
    const level = levelByID.get(task.taskID) || 0
    const bucket = groupedByLevel.get(level) || []
    bucket.push(task)
    groupedByLevel.set(level, bucket)
  }

  const sortedLevels = [...groupedByLevel.keys()].sort((a, b) => a - b)
  for (const level of sortedLevels) {
    const tasks = groupedByLevel.get(level) || []
    tasks.sort((a, b) => a.taskID.localeCompare(b.taskID))
    groupedByLevel.set(level, tasks)
  }

  const positionedNodes: PositionedNode[] = []
  const positionByID = new Map<string, PositionedNode>()

  let maxRows = 1
  for (const level of sortedLevels) {
    const tasks = groupedByLevel.get(level) || []
    if (tasks.length > maxRows) {
      maxRows = tasks.length
    }

    tasks.forEach((task, rowIndex) => {
      const x = PADDING + level * (NODE_WIDTH + HORIZONTAL_GAP)
      const y = PADDING + rowIndex * (NODE_HEIGHT + VERTICAL_GAP)
      const positioned: PositionedNode = {
        task,
        x,
        y,
        width: NODE_WIDTH,
        height: NODE_HEIGHT,
      }
      positionedNodes.push(positioned)
      positionByID.set(task.taskID, positioned)
    })
  }

  const edges = uniqueEdges(plan)
    .map((edge) => {
      const from = positionByID.get(edge.fromTaskID)
      const to = positionByID.get(edge.toTaskID)
      if (!from || !to) {
        return null
      }
      return {
        fromTaskID: edge.fromTaskID,
        toTaskID: edge.toTaskID,
        fromX: from.x + from.width,
        fromY: from.y + from.height / 2,
        toX: to.x,
        toY: to.y + to.height / 2,
      }
    })
    .filter((edge): edge is PositionedEdge => edge !== null)

  const levelCount = sortedLevels.length || 1
  const width = PADDING * 2 + levelCount * NODE_WIDTH + (levelCount - 1) * HORIZONTAL_GAP
  const height = PADDING * 2 + maxRows * NODE_HEIGHT + (maxRows - 1) * VERTICAL_GAP

  return {
    nodes: positionedNodes,
    edges,
    width,
    height,
  }
}

export function TaskGraphPreview({
  plan,
  showApprovalActions,
  actionNotice,
  onApprove,
  onReject,
}: {
  plan: DecompositionPlan
  showApprovalActions: boolean
  actionNotice: string
  onApprove: () => void
  onReject: () => void
}) {
  const layout = buildGraphLayout(plan)

  return (
    <div className="space-y-3">
      <div className="grid gap-2 rounded-md border bg-muted/20 p-3 text-sm md:grid-cols-2 xl:grid-cols-4">
        <div>
          <p className="text-xs text-muted-foreground">Delegation mode</p>
          <p className="font-medium">{plan.delegationMode || "-"}</p>
        </div>
        <div>
          <p className="text-xs text-muted-foreground">Trigger reason</p>
          <p className="font-medium">{plan.triggerReason || "-"}</p>
        </div>
        <div>
          <p className="text-xs text-muted-foreground">Min confidence</p>
          <p className="font-medium">{Math.round(plan.minConfidence * 100)}%</p>
        </div>
        <div>
          <p className="text-xs text-muted-foreground">Avg confidence</p>
          <p className="font-medium">{Math.round(plan.avgConfidence * 100)}%</p>
        </div>
      </div>

      <div className="overflow-auto rounded-md border" data-testid="task-graph-preview-canvas">
        <div className="relative" style={{ width: `${layout.width}px`, height: `${layout.height}px` }}>
          <svg className="absolute inset-0" width={layout.width} height={layout.height}>
            <defs>
              <marker id="task-graph-arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto">
                <path d="M0,0 L8,4 L0,8 z" fill="currentColor" />
              </marker>
            </defs>
            {layout.edges.map((edge) => (
              <line
                key={`${edge.fromTaskID}->${edge.toTaskID}`}
                data-testid={`task-graph-edge-${toTestIDSegment(edge.fromTaskID)}-${toTestIDSegment(edge.toTaskID)}`}
                x1={edge.fromX}
                y1={edge.fromY}
                x2={edge.toX}
                y2={edge.toY}
                stroke="currentColor"
                strokeOpacity={0.45}
                strokeWidth={2}
                markerEnd="url(#task-graph-arrow)"
              />
            ))}
          </svg>

          {layout.nodes.map((node) => (
            <div
              key={node.task.taskID}
              data-testid={`task-graph-node-${toTestIDSegment(node.task.taskID)}`}
              className="absolute rounded-md border bg-card p-3 shadow-sm"
              style={{ left: `${node.x}px`, top: `${node.y}px`, width: `${node.width}px`, height: `${node.height}px` }}
            >
              <div className="mb-2 flex items-center justify-between gap-2">
                <p className="truncate text-xs font-semibold uppercase tracking-wider text-muted-foreground">{node.task.taskID}</p>
                <Badge variant="outline" className="truncate text-[10px]">
                  {node.task.assignedRole || "unassigned"}
                </Badge>
              </div>
              <p className="line-clamp-2 text-xs">{node.task.description || "No description provided."}</p>
              <div className="mt-2 flex items-center justify-between">
                <Badge className={confidenceBadgeClass(node.task.confidence)}>
                  {Math.round(node.task.confidence * 100)}%
                </Badge>
                <span className="text-[11px] text-muted-foreground">
                  deps: {node.task.dependsOn.length}
                </span>
              </div>
            </div>
          ))}
        </div>
      </div>

      {showApprovalActions ? (
        <div className="space-y-2">
          <div className="flex flex-wrap gap-2">
            <Button data-testid="task-graph-approve-button" size="sm" onClick={onApprove}>
              Approve plan
            </Button>
            <Button data-testid="task-graph-reject-button" size="sm" variant="outline" onClick={onReject}>
              Reject plan
            </Button>
          </div>
          {actionNotice ? (
            <p data-testid="task-graph-action-notice" className="text-sm text-muted-foreground">
              {actionNotice}
            </p>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}
