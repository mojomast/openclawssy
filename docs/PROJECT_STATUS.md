# Project Status

_Ussyverse status board for operators and contributors._

## Current state

Openclawssy is a **prototype in development**.

What works now:
- CLI setup and run loop
- tool registry with policy gates
- sandbox-gated `shell.exec`
- scheduler queueing runs
- HTTP API + dashboard
- Discord bridge queueing runs
- encrypted secret ingestion
- persistent chat sessions (`/new`, `/resume`, `/chats`) with session history
- multi-tool parsing and normalized tool call IDs across runs
- repeated identical tool calls reuse successful cached results within a run
- per-tool execution summaries in trace/dashboard/Discord output
- session context replay with bounded/truncated history budgets
- model response cap (`model.max_tokens`, default 20000)
- thinking extraction with configurable display modes (`never` default, `on_error`, `always`)
- dashboard chat layout controls (resizable chat, collapsible panes, focus mode)
- long-running tool defaults (`120` iterations, `900s` per tool call) for heavy shell workflows
- staged failure recovery (after 2 failures, force error-recovery mode; after 3 additional failures, ask user with attempted commands/errors/outputs, including intermittent failure loops)
- repetition-loop hardening: normalized task-id retry suffix handling (`-v2`, `-retry`, `-continue`, `-attemptN`, `-redo`, `-fix`), `shell.exec` same-command repetition caps, and fast finalization when iterations are fully repetition-blocked
- parser resilience for malformed tool payloads: delimiter-closure repair for truncated JSON candidates plus `<tool_call>...` tagged-call recovery coverage
- dashboard orchestration visibility: loop-risk now surfaces backend repetition guards, shows active tool-call count, and renders explicit repetition/iteration-limit warning badges
- dashboard chat auto-progress updates for long runs (elapsed time + completed tool calls + latest summary)
- chat queue API now returns `session_id` for queued runs so clients can stay attached to session context
- chatstore cross-process locking for writes and lock-respecting reads
- scheduler concurrent execution worker pool + global/per-job pause-resume controls
- dashboard admin scheduler APIs (jobs CRUD + pause/resume control)
- global run queue saturation guard with explicit overload response (`429`)
- canonical tool error codes with machine-readable persistence
- end-to-end memory lifecycle (event stream -> checkpoint -> recall injection -> maintenance -> proactive hooks)
- optional embedding-backed semantic hybrid memory search (OpenRouter/OpenAI-compatible `/embeddings`)
- admin memory observability (`/api/admin/memory/<agent>`) including health and embedding coverage stats
- **Docker sandbox provider** (`sandbox.provider=docker`): all agent workspace activity runs inside an isolated Docker container; no host filesystem access from fs.* or shell.exec tools
- **Sandbox Provider interface** (`internal/sandbox`): pluggable `Provider` abstraction; Local, None, and Docker implementations; all fs.* tools and shell.exec route through it
- **Docker sandbox admin API** (`/api/admin/sandbox/docker/*`): 8 endpoints for container lifecycle, image management, volume management; bearer-auth protected
- **Sandbox Manager dashboard page** (`/sandbox` route): UI for container status, create/stop/reset actions, image pull, volume management with confirmation dialogs

What is not production-ready:
- compatibility and schema stability
- full authn/authz model for multi-tenant use
- external security review
- complete observability and disaster recovery

Current test status:
- `go test ./...` passes.

## Recommendation

Do not deploy this to production.
Use only local/dev environments with test credentials.

Ussyverse maturity label today: **Prototype / Builder Preview**.
