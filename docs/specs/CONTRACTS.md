# Openclawssy Contracts (v0.1)

This document defines wire-level and runtime contracts used by all modules.

## 1) Tool Interface Contracts

### 1.1 Request Envelope

All tool invocations use a normalized request object.

```json
{
  "request_id": "req_123",
  "run_id": "run_123",
  "agent_id": "agent_default",
  "tool": "fs.read",
  "input": {},
  "timeout_ms": 30000,
  "created_at": "2026-02-15T00:00:00Z"
}
```

Rules:
- `request_id`, `run_id`, `agent_id`, `tool`, and `input` are required.
- `tool` must match a registered tool name.
- `input` must validate against the tool input schema.
- `timeout_ms` defaults to `30000` and is clamped by config.

### 1.2 Response Envelope

```json
{
  "request_id": "req_123",
  "run_id": "run_123",
  "tool": "fs.read",
  "ok": true,
  "output": {},
  "error": null,
  "duration_ms": 14,
  "finished_at": "2026-02-15T00:00:00Z"
}
```

Rules:
- `ok=true` requires `error=null`.
- `ok=false` requires `error.code` and `error.message`.
- `output` must be omitted or empty when `ok=false`.

### 1.3 Standard Error Shape

```json
{
  "code": "policy.denied",
  "message": "write outside workspace",
  "retryable": false,
  "details": {
    "path": "../etc/passwd"
  }
}
```

Canonical error codes:
- `invalid.request`
- `tool.not_found`
- `tool.input_invalid`
- `policy.denied`
- `sandbox.required`
- `timeout`
- `internal.error`

## 2) Run Lifecycle Events

All runs emit ordered lifecycle events to audit logs and run bundles.

Required event order:
1. `run.created`
2. `run.started`
3. zero or more `model.requested`
4. zero or more `tool.call`
5. zero or more `tool.result`
6. one terminal event: `run.completed` | `run.failed` | `run.cancelled`

Failure-loop handling contract:
- After 2 consecutive failing tool results, the next model turn is run in explicit error-recovery mode.
- If 3 additional failing tool results occur while recovery mode is active, runner returns a terminal assistant response asking user guidance.
- Recovery mode clears only after sustained success (multiple successful tool outcomes), not a single transient success.
- That escalation response must include recent attempts, errors, and output snippets.

Event minimum fields:
- `event_id`, `event_type`, `ts`
- `run_id`, `agent_id`
- `instance_id` when the caller or runtime resolved an instance-scoped run
- `seq` (strictly increasing per run)
- `payload` (event-specific object)

## 3) Audit Event Schema

Audit log format: append-only JSONL at `.openclawssy/agents/<agentId>/audit/YYYY-MM-DD.jsonl`.

Base schema:

```json
{
  "event_id": "evt_123",
  "event_type": "tool.call",
  "ts": "2026-02-15T00:00:00Z",
  "run_id": "run_123",
  "agent_id": "agent_default",
  "actor": "system",
  "seq": 7,
  "payload": {},
  "redactions": ["payload.input.token"]
}
```

Rules:
- Audit file is append-only; existing lines are never rewritten.
- Sensitive values are redacted before write.
- `redactions` lists JSON paths replaced during redaction.

## 4) Scheduler Job Schema

Scheduler jobs are persisted as JSON records.

```json
{
  "id": "job_daily_report",
  "agentID": "agent_default",
  "schedule": "@every 1h",
  "message": "Generate status report",
  "enabled": true,
  "lastRun": "2026-02-15T00:00:00Z"
}
```

Rules:
- `id` and `schedule` are required at create-time.
- `agentID` and `message` should be provided for runnable jobs.
- `schedule` supports only:
  - `@every <duration>` (Go duration format, for example `@every 30m`)
  - recurring aliases `@hourly`, `hourly`, `@daily`, `daily`
  - hourly/daily cron shorthands `0 * * * *` and `0 0 * * *` (normalized to recurring intervals)
  - one-shot RFC3339 timestamp (for example `2026-02-18T09:00:00Z`)
- General cron expressions are not supported in v0.2 and are rejected on create/update.
- Missed-job policy:
  - Scheduler evaluates due jobs at each tick/startup check; it does not reconstruct every missed interval.
  - `@every` jobs that were missed while offline run at most once on the next check, then continue from that run time.
  - One-shot RFC3339 jobs scheduled in the past run once on the next check and are then disabled.

## 5) Minimal HTTP Endpoints

Default bind: `127.0.0.1` only. Token auth required.

Headers:
- `Authorization: Bearer <token>`

### POST `/v1/runs`
Request:

```json
{
  "instance_id": "default",
  "agent_id": "agent_default",
  "message": "Summarize repository status",
  "thinking_mode": "always"
}
```

Request notes:
- `instance_id` is optional and must be a valid canonical instance id when present.
- `thinking_mode` is optional and must be one of `never|on_error|always`.
- When omitted, runtime uses `output.thinking_mode` from config.

Response `202`:

```json
{
  "instance_id": "default",
  "id": "run_123",
  "status": "queued"
}
```

### GET `/v1/runs`
Query params:
- `instance_id` (optional exact instance filter)
- `status` (optional exact status filter)
- `limit` (optional, default `50`, max `500`)
- `offset` (optional, default `0`)

Response `200`:

```json
{
  "runs": [],
  "total": 0,
  "limit": 50,
  "offset": 0
}
```

### GET `/v1/runs/{run_id}`
Response `200`:

```json
{
  "id": "run_123",
  "instance_id": "default",
  "agent_id": "agent_default",
  "source": "http",
  "status": "completed",
  "output": "...",
  "artifact_path": ".openclawssy/agents/agent_default/runs/run_123",
  "duration_ms": 42,
  "tool_calls": 1,
  "provider": "openrouter",
  "model": "openai/gpt-4o-mini",
  "thinking_mode": "always",
  "trace": {
    "tool_execution_results": [
      {
        "tool": "fs.write",
        "tool_call_id": "tool-json-1",
        "summary": "wrote 24 line(s) to README.md",
        "output": "{...}",
        "error": ""
      }
    ]
  }
}
```

Notes:
- `trace` is optional but typically present for completed/failed runs.
- `tool_execution_results[].summary` is a short display-friendly summary when available.
- External route compatibility still centers on bare `run_id`, but runtime and HTTP tracking are converging on `(instance_id, agent_id, run_id)` internally.

Queue-overload response for `POST /v1/runs`:

```json
{
  "error": {
    "code": "queue.full",
    "message": "run queue is full"
  }
}
```

### POST `/v1/chat/messages`
Request:

```json
{
  "user_id": "123456",
  "room_id": "dev-room",
  "agent_id": "agent_default",
  "message": "Summarize today updates",
  "thinking_mode": "on_error"
}
```

Response `202`:

```json
{
  "id": "run_456",
  "status": "queued",
  "session_id": "session_abc"
}
```

Notes:
- `session_id` is included when the request is associated with a persisted chat session.
- Command-style chat requests (for example `/new`, `/resume`) may return `200` with an immediate `response` message and optional `session_id` instead of queueing a run.
- `thinking_mode` is optional and validated with the same modes as run creation.

Rate-limit response example for `POST /v1/chat/messages`:

```json
{
  "error": {
    "code": "chat.rate_limited",
    "message": "chat sender is rate limited; retry in 3s",
    "retry_after_seconds": 3
  }
}
```

## 6) Chat Session Context Policy

For model context reconstruction from persisted chat history, v0.2 uses **Option A**:
- Tool executions are stored and replayed as `role="tool"` messages.
- Tool messages are normalized into concise context text (`tool <name> result (<id>)` + summary/error/output excerpt).
- Tool payloads are never passed back verbatim at full size.

Session truncation rules before model invocation:
- Per-message cap: `1400` characters.
- Tool-specific caps inside a tool message:
  - `summary`: `220` chars
  - `error`: `320` chars
  - `output` excerpt: `1000` chars
- Total session-context cap (sum of message content): `12000` characters.
- When over budget, oldest messages are dropped first so the latest turns (including recent tool results) are retained.

### Dashboard Admin APIs (token-auth)
- `GET /api/admin/status` -> run list + selected model/provider status
- `GET /api/admin/config` -> config with sensitive value fields blanked
- `POST /api/admin/config` -> persist validated config
- `GET /api/admin/secrets` -> list secret keys only
- `POST /api/admin/secrets` -> one-way secret ingestion `{name,value}` (value not retrievable via API)
- `GET /api/admin/scheduler/jobs` -> list scheduler jobs + global paused state
- `POST /api/admin/scheduler/jobs` -> create scheduler job `{id?,agent_id?,schedule,message,enabled?}`
- `DELETE /api/admin/scheduler/jobs/{id}` -> delete scheduler job
- `POST /api/admin/scheduler/control` -> pause/resume scheduler globally or per job `{action:"pause|resume",job_id?}`
- `GET /api/admin/chat/sessions` -> list chat sessions for an agent/user/room/channel filter, optional `limit`/`offset`
- `GET /api/admin/chat/sessions/{session_id}/messages` -> ordered session messages including tool metadata (`tool_name`, `tool_call_id`, `run_id`)

### GET `/healthz`
Response `200`:

```json
{"ok": true}
```

Error response shape for HTTP endpoints:

```json
{
  "error": {
    "code": "invalid.request",
    "message": "agent_id is required"
  }
}
```
