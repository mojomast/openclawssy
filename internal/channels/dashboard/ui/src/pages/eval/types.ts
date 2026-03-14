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

export type EvalRun = {
  id: number
  suite: string
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
