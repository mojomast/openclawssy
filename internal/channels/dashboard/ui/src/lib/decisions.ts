type UnknownRecord = Record<string, unknown>

export type DecisionRecordItem = {
  timestamp: string
  runID: string
  agentID: string
  recordType: string
  humanSummary: string
  payload: UnknownRecord
}

export type RunDecisionNode = {
  runID: string
  agentID: string
  records: DecisionRecordItem[]
  subagents: RunDecisionNode[]
}

export type FlattenedDecisionRecord = DecisionRecordItem & {
  depth: number
}

function asRecord(value: unknown): UnknownRecord | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return null
  }
  return value as UnknownRecord
}

function asText(value: unknown): string {
  if (value === null || value === undefined) {
    return ""
  }
  return String(value)
}

function parseDecisionRecord(value: unknown): DecisionRecordItem | null {
  const raw = asRecord(value)
  if (!raw) {
    return null
  }

  const runID = asText(raw.run_id).trim()
  const recordType = asText(raw.record_type).trim()
  if (!runID || !recordType) {
    return null
  }

  return {
    timestamp: asText(raw.timestamp).trim(),
    runID,
    agentID: asText(raw.agent_id).trim(),
    recordType,
    humanSummary: asText(raw.human_summary).trim(),
    payload: asRecord(raw.payload) || {},
  }
}

function parseNode(value: unknown): RunDecisionNode | null {
  const raw = asRecord(value)
  if (!raw) {
    return null
  }

  const runID = asText(raw.run_id).trim()
  if (!runID) {
    return null
  }

  const records = Array.isArray(raw.records)
    ? raw.records.map(parseDecisionRecord).filter((record): record is DecisionRecordItem => record !== null)
    : []

  const subagents = Array.isArray(raw.subagents)
    ? raw.subagents.map(parseNode).filter((node): node is RunDecisionNode => node !== null)
    : []

  return {
    runID,
    agentID: asText(raw.agent_id).trim(),
    records,
    subagents,
  }
}

function timestampToNumber(raw: string): number {
  if (!raw) {
    return Number.POSITIVE_INFINITY
  }
  const parsed = Date.parse(raw)
  if (Number.isNaN(parsed)) {
    return Number.POSITIVE_INFINITY
  }
  return parsed
}

function compareDecisionRecords(left: DecisionRecordItem, right: DecisionRecordItem): number {
  const timeDiff = timestampToNumber(left.timestamp) - timestampToNumber(right.timestamp)
  if (timeDiff !== 0) {
    return timeDiff
  }

  if (left.runID !== right.runID) {
    return left.runID.localeCompare(right.runID)
  }
  if (left.recordType !== right.recordType) {
    return left.recordType.localeCompare(right.recordType)
  }
  return left.humanSummary.localeCompare(right.humanSummary)
}

export function parseRunDecisionNode(value: unknown): RunDecisionNode | null {
  const node = parseNode(value)
  if (!node) {
    return null
  }

  const sortedNode = (input: RunDecisionNode): RunDecisionNode => {
    const records = [...input.records].sort(compareDecisionRecords)
    const subagents = input.subagents.map(sortedNode).sort((a, b) => a.runID.localeCompare(b.runID))
    return {
      ...input,
      records,
      subagents,
    }
  }

  return sortedNode(node)
}

export function flattenDecisionRecords(root: RunDecisionNode | null): FlattenedDecisionRecord[] {
  if (!root) {
    return []
  }

  const flattened: FlattenedDecisionRecord[] = []

  const visit = (node: RunDecisionNode, depth: number) => {
    for (const record of node.records) {
      flattened.push({
        ...record,
        depth,
      })
    }
    for (const child of node.subagents) {
      visit(child, depth + 1)
    }
  }

  visit(root, 0)
  flattened.sort(compareDecisionRecords)
  return flattened
}

export function formatDecisionTimestamp(raw: string): string {
  if (!raw) {
    return "Unknown time"
  }
  const parsed = new Date(raw)
  if (Number.isNaN(parsed.getTime())) {
    return raw
  }
  return parsed.toLocaleString()
}
