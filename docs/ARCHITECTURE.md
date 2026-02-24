# Openclawssy Architecture (v0.5)

## Runtime Flow
- Channel adapters (CLI, HTTP, chat, Discord, scheduler) normalize requests into `runtime.ExecuteInput`.
- Engine acquires a global run slot (`engine.max_concurrent_runs`) before execution.
- Prompt assembly merges: system policy, agent files, optional chat/session context, and user input.
- Model response is parsed for tool calls and visible text in a bounded loop.
- Tool invocations pass through registry validation and policy checks before execution.
- Repetition guards prevent same-intent loops (cached identical calls, per-tool caps, normalized task-id keys).
- Run bundle artifacts, trace, and audit events are persisted at completion.

## Runner Loop
```text
Input -> ExecuteWithInput
      -> acquire run slot
      -> build prompt + session context
      -> model turn
      -> parse output (text + tool calls)
      -> execute tools (0..n)
      -> repeat until terminal assistant output
      -> write run bundle + audit + release slot
```

## Parser and Thinking Extraction
- Parsing captures malformed tool snippets and normalized rejection reasons.
- Recovery repair can close unbalanced JSON delimiters (truncated braces/brackets/strings) before parse retry.
- Tagged fallback parsing supports `<tool_call>tool_name,{...}` when providers emit non-fenced tool directives.
- `ParseDiagnostics` is returned when `thinking_mode=always` or parse failure occurred.
- Thinking text extraction is controlled by `output.thinking_mode` (or per-request override).
- Thinking text is truncated to `output.max_thinking_chars` before persistence/return.
- Redaction runs before diagnostics/thinking data is emitted to user-visible outputs.

## Scheduler Execution Path
- Scheduler store persists jobs and pause state on disk.
- Executor ticks at a fixed interval and computes due jobs (`@every` or RFC3339 one-shot).
- Startup behavior is controlled by `scheduler.catch_up`.
- Due jobs are dispatched through a bounded worker pool (`scheduler.max_concurrent_jobs`).
- Each scheduled execution enqueues a normal runtime run via channel/runtime integration.

## Sandbox Provider Architecture

All agent filesystem and shell operations are routed through a `sandbox.Provider` interface. Docker is the recommended provider when sandboxing is enabled. Three implementations exist:

| Provider | Workspace | shell.exec |
|----------|-----------|------------|
| `none`   | disabled  | denied     |
| `local`  | host/container path | allowed |
| `docker` | `/workspace` inside sandbox container | runs via `docker exec` |

### Docker Provider: Two-Container Model

The backend (API/engine) runs in one container. Each agent's workspace runs in a **separate sandbox container**. The backend talks to the host Docker daemon via the mounted Unix socket (`/var/run/docker.sock`) to create and manage sandbox containers.

```text
Backend container                    Sandbox container (per agent)
┌─────────────────┐                 ┌──────────────────────┐
│ Engine/API      │  docker exec    │ /workspace           │
│                 │ ──────────────► │ network=none         │
│ docker CLI      │  docker cp      │ cpu/memory limits    │
│ /var/run/docker │                 │ sleep infinity       │
│   .sock (mount) │                 │ named volume backing │
└─────────────────┘                 └──────────────────────┘
```

### Docker Provider Flow
```text
Tool call (fs.write / shell.exec)
  -> engine: dockerResolvePath() enforces /workspace prefix
  -> sandbox.DockerProvider.WriteFile() / Exec()
     -> validateContainerPath() re-enforces /workspace (defense in depth)
     -> docker cp (read/write) or docker exec (shell)
     -> data never touches host filesystem
```

Key properties of the Docker provider:
- Named volume `openclawssy_ws_<agent_id>` persists workspace across container restarts.
- Container name `openclawssy_agent_<agent_id>` is reused; not recreated on every run.
- Network is `none` by default; configurable via `sandbox.docker.network_enabled`.
- CPU/memory limits configurable via `sandbox.docker.cpu_limit` / `sandbox.docker.memory_limit_mb`.
- Secrets are **never** injected into container environment — only passed to the model API layer.
- Image pull policy: `if-not-present` (default), `always`, or `never`.

### Admin API
The Docker sandbox exposes operator endpoints at `/api/admin/sandbox/docker/*`:
- `GET /status` — container running state, image, volume name, workspace path
- `POST /create` — ensure container exists and start it
- `POST /stop` — stop container (volume retained)
- `POST /reset` — remove container (volume retained)
- `POST /pull` — pull image by reference
- `GET /images` — list local Docker images
- `GET /volumes` — list Docker volumes
- `DELETE /volume` — remove a named volume

All endpoints require bearer auth (same token as the rest of the API).

## Key Persistence Surfaces
- Config: `.openclawssy/config.json` (atomic write + validation).
- Runs: `.openclawssy/agents/<agent>/runs/<run_id>/`.
- Audit: `.openclawssy/agents/<agent>/audit/YYYY-MM-DD.jsonl` (buffered writes, periodic flush, run-end sync).
- Chat sessions: persisted chat store files (session metadata + messages).
- Scheduler: persisted jobs/state file with backup/restore safeguards.
- Docker workspace: named volume `openclawssy_ws_<agent_id>` on Docker host (when provider=docker).
