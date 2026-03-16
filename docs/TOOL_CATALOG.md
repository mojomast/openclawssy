# Tool Catalog

This page documents the current tool surface, argument shapes, aliases, and safety constraints.

All tools are deny-by-default through capability policy, and all tool inputs are validated before handler execution.

## Aliases

- `fs.rename` -> `fs.move`
- `net.fetch` -> `http.request`
- `bash.exec` -> `shell.exec`
- `terminal.exec` -> `shell.exec`
- `terminal.run` -> `shell.exec`
- `continuity.resume` -> `becomussy.resume`
- `becomussy.memory.store` -> `becomussy.memory.create`
- `becomussy.journal.write` -> `becomussy.journal.create`
- `becomussy.reflect` -> `becomussy.journal.create`

## Filesystem and Code

### `fs.read`
- Required: `path`
- Optional: `max_bytes`
- Notes: read-only, workspace/path-guard enforced, truncated when `max_bytes` is exceeded.

### `fs.list`
- Required: `path`
- Optional: none
- Notes: workspace/path-guard enforced.

### `fs.write`
- Required: `path`, `content`
- Optional: none
- Notes: overwrites file; workspace/path-guard enforced; blocks workspace control-plane filenames (`SOUL.md`, `RULES.md`, `TOOLS.md`, `HANDOFF.md`, `DEVPLAN.md`, `SPECPLAN.md`).

### `fs.append`
- Required: `path`, `content`
- Optional: none
- Notes: appends to file without replacing prior content; same workspace/path/control-plane protections as `fs.write`.

### `fs.delete`
- Required: `path`
- Optional: `recursive`, `force`
- Notes: workspace/path-guard and control-plane guard enforced; directory removal requires `recursive=true`.

### `fs.move`
- Required: `src`, `dst`
- Optional: `overwrite`
- Notes: workspace/path-guard for both paths and control-plane guard on source/destination.

### `fs.edit`
- Required: `path`
- Optional: `old`, `new`, `edits`, `patch`
- Notes: supports exactly one mode per call: replace-once (`old`/`new`), line-range patch (`edits`), or unified diff hunks (`patch`).

### `code.search`
- Required: `pattern`
- Optional: `path`, `max_files`, `max_file_bytes`
- Notes: regex-based; text-only scan; skips `.git` and `.openclawssy`.

## Configuration and Secrets

### `config.get`
- Required: none
- Optional: `field`
- Notes: returns redacted configuration only; `field` can target specific allowlisted values such as `output.thinking_mode`, `engine.max_concurrent_runs`, `scheduler`, or `scheduler.catch_up`.

### `config.set`
- Required: `updates`
- Optional: `dry_run`
- Notes: allowlisted mutable fields only; validated and atomically persisted.

### `secrets.set`
- Required: `key`, `value`
- Optional: none
- Notes: encrypted secret store; plaintext value is redacted in audit logs.

### `secrets.list`
- Required: none
- Optional: none
- Notes: returns keys only.

## Scheduling and Sessions

### `scheduler.list`
- Required: none
- Optional: none
- Notes: always reads fresh scheduler state; results are not reused from the intra-run tool cache.

### `scheduler.add`
- Required: `schedule`, `message`
- Optional: `id`, `agent_id`, `enabled`
- Notes: accepts `@every <duration>`, helpful recurring aliases (`@hourly`, `@daily`), common hourly/daily cron shorthands (`0 * * * *`, `0 0 * * *`), or one-shot RFC3339 timestamps. Recurring aliases are normalized before persistence.
- Notes: repeated attempts to add the same explicit job `id` in a single run are loop-guarded after the first successful mutation.

### `scheduler.remove`
- Required: `id`
- Optional: none

### `scheduler.pause`
- Required: none
- Optional: `id`

### `scheduler.resume`
- Required: none
- Optional: `id`

### `session.list`
- Required: none
- Optional: `agent_id`, `user_id`, `room_id`, `channel`, `limit`, `include_closed`
- Notes: reads persisted chat sessions under `.openclawssy/agents/*/memory/chats`.

### `session.close`
- Required: `session_id`
- Optional: `id`
- Notes: closed sessions are not reused by chat routing.

### `agent.list`
- Required: none
- Optional: `limit`, `offset`
- Notes: lists agent directories under `.openclawssy/agents` sorted by agent ID.

### `agent.create`
- Required: `agent_id`
- Optional: `force`
- Notes: scaffolds `.openclawssy/agents/<agent_id>` with `memory`, `audit`, `runs` and seeded control docs (`SOUL.md`, `RULES.md`, `TOOLS.md`, `SPECPLAN.md`, `DEVPLAN.md`, `HANDOFF.md`). `clawdefuckifier*` agent IDs also auto-seed the workspace skill and self-improvement bootstrap. The shared `workspace/skills/clawdefuckifier.md` skill is also available for other agents to load with `skill.read`.

### `agent.switch`
- Required: `agent_id`
- Optional: `scope` (`chat|discord|both`, default `both`), `create_if_missing`
- Notes: updates config defaults (`chat.default_agent_id` / `discord.default_agent_id`) using scoped switching; can scaffold missing agent first when `create_if_missing=true`.

### `agent.message.send`
- Required: `to_agent_id`, `message`
- Optional: `task_id`, `subject`, `channel`, `user_id`, `session_id`
- Notes: writes to inter-agent inbox sessions (`channel=agent-mail`). Messaging is instance-scoped; the current runtime/tool request instance is used implicitly and the response includes `instance_id`. Sender and recipient must both exist in that same instance. Optional source context fields are persisted in payload for proactive/memory traceability.

### `agent.message.inbox`
- Required: none
- Optional: `agent_id`, `limit`
- Notes: reads recent inter-agent inbox payloads for the target agent in the current instance. The response includes top-level `instance_id`, and each message entry includes `instance_id` as well.

## Memory Tools

### `memory.search`
- Required: none
- Optional: `query`, `limit`, `min_importance`, `status`
- Notes: returns `mode` (`fts` or `semantic_hybrid` when embeddings are enabled and available).

### `memory.write`
- Required: `kind`, `title`, `content`
- Optional: `importance`, `confidence`, `status`

### `memory.update`
- Required: `id`
- Optional: `kind`, `title`, `content`, `importance`, `confidence`, `status`

### `memory.forget`
- Required: `id`
- Optional: none

### `memory.health`
- Required: none
- Optional: none

### `decision.log`
- Required: `title`, `content`
- Optional: `importance`, `confidence`, `metadata`

### `memory.checkpoint`
- Required: none
- Optional: `max_events`
- Notes: uses strict model-validated distillation with deterministic fallback.

### `memory.maintenance`
- Required: none
- Optional: `stale_days`, `dry_run`
- Notes: dedupe/archive/verification pass, compaction, and weekly report generation.

## Runs and Networking

### `run.list`
- Required: none
- Optional: `agent_id`, `status`, `limit`, `offset`
- Notes: filtered/paginated summaries from run store.

### `agent.run`
- Required: `agent_id`, `message`
- Optional: `task_id`, `thinking_mode`, `allowed_tools`, `max_tool_iterations`, `timeout_ms`
- Notes: runs a bounded subagent task and returns structured output. Delegated subagent runs inherit the parent `instance_id` automatically so instance boundaries stay stable across orchestration. Use descriptive `task_id` values for iterative workflows so Agent Monitor can distinguish phases.

### `run.get`
- Required: `run_id`
- Optional: `id`
- Notes: returns full run record by ID.

### `run.cancel`
- Required: `run_id`
- Optional: none
- Notes: cancels in-flight tracked run contexts.

### `metrics.get`
- Required: none
- Optional: `agent_id`, `status`, `limit`, `offset`
- Notes: aggregates run statuses and per-tool call/error/latency stats from persisted run traces.

## Policy Management

### `policy.list`
- Required: none
- Optional: `agent_id`, `limit`, `offset`
- Notes: requires `policy.admin`; returns effective capability grants per agent (default or persisted source).

### `policy.grant`
- Required: `agent_id`, `capability`
- Optional: `tool`
- Notes: requires `policy.admin`; persists capability grants and updates live enforcer when available. `tool` is accepted as an alias for `capability`.

### `policy.revoke`
- Required: `agent_id`, `capability`
- Optional: `tool`
- Notes: requires `policy.admin`; persists grant removals and updates live enforcer when available. `tool` is accepted as an alias for `capability`.

### `http.request`
- Required: `url`
- Optional: `method`, `headers`, `body`, `timeout_ms`, `max_response_bytes`
- Notes: requires `network.enabled=true`; enforces `http/https`, domain allowlist, localhost policy, redirect re-check, and response size caps.

## Utility and Shell

### `time.now`
- Required: none
- Optional: none

### `shell.exec`
- Required: `command`
- Optional: `args`, `timeout_ms`
- Notes: available only when shell execution is enabled by policy; supports command-prefix allowlist and shell fallback (`bash` -> `/bin/bash` -> `/usr/bin/bash` -> `sh`).

## Becomussy Continuity Tools

Requires `becomussy.enabled=true` in config. All tools communicate with a running becomussy instance at `becomussy.base_url`.

### Config Fields

| Field | Type | Default | Description |
|---|---|---|---|
| `becomussy.enabled` | `bool` | `false` | Enable becomussy integration |
| `becomussy.base_url` | `string` | `http://localhost:8000` | Becomussy API base URL |
| `becomussy.user_id` | `string` | `openclawssy-agent` | X-User-Id header value |
| `becomussy.user_role` | `string` | `agent_runtime` | X-User-Role header value |
| `becomussy.timeout_ms` | `int` | `15000` | HTTP request timeout in ms |
| `becomussy.headers` | `object` | `null` | Extra HTTP headers to send |

### `becomussy.resume`
- Required: none
- Optional: `query`, `token_budget`
- Notes: loads the continuity resume bundle (threads, commitments, memories, identity changes, constraints, recommended next actions). Call at session start.

### `becomussy.memory.create`
- Required: `memory_type` (episodic|semantic|autobiographical|working|relational)
- Optional: `summary`, `statement`, `importance_score`, `confidence_level`, `metadata`, `source_kind`, `source_ref`

### `becomussy.memory.search`
- Required: none
- Optional: `q`, `memory_type`, `date_from`, `date_to`, `confidence`, `limit`, `offset`

### `becomussy.memory.get`
- Required: `id`
- Optional: none

### `becomussy.memory.reinforce`
- Required: `id`, `reason`
- Optional: `source_ref`
- Notes: bumps the salience score of a memory item.

### `becomussy.journal.create`
- Required: `entry_type`, `title`, `body_md`
- Optional: `confidence_level`, `tags`, `linked_memory_ids`, `linked_project_ids`, `linked_identity_themes`

### `becomussy.journal.search`
- Required: none
- Optional: `keyword`, `entry_type`, `date_from`, `date_to`, `linked_project_id`, `linked_theme`, `limit`, `offset`

### `becomussy.threads.list`
- Required: none
- Optional: `status`, `thread_type`, `limit`, `offset`

### `becomussy.threads.create`
- Required: `title`
- Optional: `description`, `thread_type`, `urgency` (1-10), `importance` (1-10), `next_action`, `blocker`

### `becomussy.projects.list`
- Required: none
- Optional: `status`, `limit`, `offset`

### `becomussy.projects.create`
- Required: `name`
- Optional: `purpose`, `origin`, `current_phase`, `linked_themes`, `linked_people`, `status`

### `becomussy.selfmodel.current`
- Required: none
- Optional: none
- Notes: returns the latest self-model version with descriptive, aspirational, constrained, and relational facets.

### `becomussy.selfmodel.history`
- Required: none
- Optional: none

### `becomussy.selfmodel.propose`
- Required: `revision_type`, `target_entity_type`, `summary`
- Optional: `target_entity_id`, `rationale`, `evidence_links`, `proposed_diff`
- Notes: creates a revision proposal gated by risk-based governance (high-risk identity changes require human approval).

### `becomussy.commitments.list`
- Required: none
- Optional: `project_id`, `status`, `overdue`, `limit`, `offset`

### `becomussy.commitments.create`
- Required: `commitment_text`
- Optional: `project_id`, `made_to`, `due_date`, `risk_if_missed`

### `becomussy.approvals.pending`
- Required: none
- Optional: none
- Notes: lists pending governance approval items (requires steward/admin/reviewer role in becomussy).

### `becomussy.audit.list`
- Required: none
- Optional: `entity_type`, `event_type`, `actor`, `limit`, `offset`
- Notes: lists audit events from becomussy (requires admin/steward/observer role in becomussy).
