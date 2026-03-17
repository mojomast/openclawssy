export type EvalTestResult = {
  passed: boolean
  expected: string
  actual: string
  durationMS: number
  error: string
}

export type EvalCaseResult = {
  name: string
  description: string
  result: EvalTestResult
}

export type EvalMetrics = {
  completionRate: number
  toolMisuseRate: number
  delegationPrecision: number
  tokenCost: number
  timeToCompletion: number
}

export type EvalRegression = {
  testName: string
  baseline: EvalTestResult
  latest: EvalTestResult
}

export type EvalIdentity = {
  instanceID: string
  agentID: string
  runID: string
  parentRunID: string
  rootRunID: string
  source: string
  taskID: string
  sessionID: string
}

export type EvalMetadata = {
  artifactPath: string
  checkpointPath: string
  decompositionPlan: Record<string, unknown> | null
  delegationEvents: Array<Record<string, unknown>>
  trace: Record<string, unknown> | null
}

export type EvalRun = {
  id: number
  suite: string
  identity: EvalIdentity
  metadata: EvalMetadata
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
