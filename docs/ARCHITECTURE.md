# Openclawssy Architecture (v0.5)

## Runtime Flow
- Channel adapters (CLI, HTTP, chat, Discord, Telegram, scheduler) normalize requests into `runtime.ExecuteInput`.
- Runtime resolves effective state from canonical instance + agent manifests before prompt/model/tool execution.
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

## Canonical identity model

- Instance is the packaging boundary.
- Agents belong to instances.
- Runtime identity is converging on `(instance_id, agent_id, run_id)`.
- Bare `run_id` is still accepted in some compatibility paths, but new runtime/HTTP tracker work stores both bare and composite keys.
- Delegated child runs inherit the parent `instance_id`.
- Inter-agent messaging is same-instance by default and now persists `instance_id` explicitly.
- Inter-agent messaging now has a first-class envelope with stable `message_id` and lifecycle states that begin with `queued` and `acknowledged`, with runtime/tooling prepared to carry later `running` / `completed` / `failed` transitions.

## Control-plane layout

Canonical control-plane state lives under:

- `.openclawssy/instances/<instance>/manifest.json`
- `.openclawssy/instances/<instance>/agents/<agent>/agent.json`
- `.openclawssy/instances/<instance>/agents/<agent>/promptstack/...`
- `.openclawssy/instances/<instance>/roles.json`
- `.openclawssy/instances/<instance>/skills.json`

Legacy flat-agent docs and paths still exist as compatibility mirrors in parts of the system, but they are no longer the architectural source of truth.

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
- Executor ticks at a fixed interval and computes due jobs (`@every`, normalized hourly/daily aliases, or RFC3339 one-shot).
- Startup behavior is controlled by `scheduler.catch_up`.
- Due jobs are dispatched through a bounded worker pool (`scheduler.max_concurrent_jobs`).
- Each scheduled execution enqueues a normal runtime run via channel/runtime integration.

## Agent Loop Safeguards
- Successful one-shot state mutations in a single run are repetition-guarded so the model does not re-apply the same action after it already succeeded.
- Control-plane state readers such as `scheduler.list`, `policy.list`, `config.get`, `session.list`, and `agent.list` are always executed fresh rather than served from intra-run cache, so the model sees post-mutation state instead of stale results.

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

## Auto-Delegation System

When tasks exceed safe execution complexity, the runtime can automatically delegate work to subagents using the `agent.run` tool. This prevents context window exhaustion and helps avoid infinite loops.

### Triggering Conditions

The system monitors several signals and computes a complexity score:

| Signal | Weight | Trigger Threshold |
|--------|--------|-------------------|
| Iterations | +1 at 40, +2 at 80, +3 at 120 | ≥40 warn, ≥80 force |
| Consecutive failures | +2 at 2 failures, +3 at 3 | ≥2 |
| No progress iterations | +2 | ≥2 |
| All blocked iterations | +3 | ≥1 |
| Repetition detected | +2 | any tool ≥2 |
| Context pressure | +1 at 75%, +2 at 85%, +3 at 92% | ≥75% warn, ≥85% force, ≥92% critical |

**Complexity levels:**
- Total score ≥2: Moderate (soft hint)
- Total score ≥4 OR all blocked: High (tool gating)
- Total score ≥6 OR blocked + others: Critical (auto-execute)

### Delegation Modes

1. **prompt_only** — Injects a soft suggestion to use `agent.run`. No enforcement.
2. **tool_gated** — Runtime blocks non-agent tools (fs.*, shell.exec, etc.) and rewrites them to `agent.run` calls. Model is forced to delegate.
3. **auto_execute** — Bypasses the model entirely. Tasks are automatically decomposed and executed by subagents.

### Task Decomposition

When delegation triggers, the system:
1. Analyzes the original task for known patterns (refactor, analyze, debug, etc.)
2. Falls back to signal-based decomposition if no pattern matches
3. Generates subtasks with dependencies (e.g., "discover files" → "modify files")
4. Executes subtasks in topological order, passing artifact summaries between them

### Safety Features

- **Cooldown**: 15 iterations by default before re-evaluating delegation
- **User question detection**: Prevents delegation when the model asked the user a question
- **Dependency tracking**: Subtasks wait for dependencies before executing
- **Timeout per subtask**: Configurable via `timeout_ms` on each task

### Configuration

```json
{
  "agents": {
    "auto_delegate": false,
    "delegation_mode": "tool_gated",
    "delegation_threshold": 2,
    "delegation_agent_id": "default",
    "delegation_cooldown_iterations": 15,
    "subagent_defaults": {
      "allowed_tools": ["fs.read", "fs.list", "fs.write", "fs.edit", "code.search", "memory.search"],
      "max_tool_iterations": 30,
      "timeout_ms": 120000,
      "thinking_mode": "never",
      "delegation_mode": "prompt_only"
    },
    "subagent_overrides": {}
  }
}
```

### Subagent Capability Restrictions

Subagents inherit a restricted toolset by default (deny-by-default). The runtime resolves
restrictions per target agent: check `subagent_overrides[agentID]` first, fall back to
`subagent_defaults`, and merge partial overrides with defaults for zero-value fields.

`AllowedTools` and `MaxToolIterations` flow through `AgentRunInput` into `ExecuteInput`,
and `TimeoutMS` is applied as a context deadline on the subagent run.

Delegated subagent execution is instance-stable: once a parent run resolves to an instance, child runs reuse that same `instance_id` rather than falling back to the active/default instance.

### Context Token Tracking

The delegation trigger uses live token counts from model responses (`PromptTokens`,
`TotalTokens`). Context pressure scores fire at 75% (warn), 85% (force), and 92%
(critical) of the context window. When `SubAgentRunner` is nil, execution-dependent
modes are downgraded to `prompt_only` automatically.

## Key Persistence Surfaces
- Config: `.openclawssy/config.json` (atomic write + validation).
- Runs: `.openclawssy/agents/<agent>/runs/<run_id>/`.
- Audit: `.openclawssy/agents/<agent>/audit/YYYY-MM-DD.jsonl` (buffered writes, periodic flush, run-end sync).
- Chat sessions: persisted chat store files (session metadata + messages).
- Scheduler: persisted jobs/state file with backup/restore safeguards.
- Docker workspace: named volume `openclawssy_ws_<agent_id>` on Docker host (when provider=docker).

Compatibility note:

- Some run/audit/chat persistence still uses legacy flat-agent paths for compatibility.
- Canonical configuration, prompt stack, roles, skills, messaging scope, and effective runtime resolution now come from instance-aware storage.
- Trace and audit metadata now carry normalized `instance_id` and `parent_run_id` fields in addition to `run_id` and `agent_id`.
