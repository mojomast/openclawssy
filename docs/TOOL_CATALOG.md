# Tool Catalog

This page documents the current tool surface, argument shapes, aliases, and safety constraints.

All tools are deny-by-default through capability policy, and all tool inputs are validated before handler execution.

## Aliases

- `fs.rename` -> `fs.move`
- `net.fetch` -> `http.request`
- `bash.exec` -> `shell.exec`
- `terminal.exec` -> `shell.exec`
- `terminal.run` -> `shell.exec`

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
- Notes: returns redacted configuration only.

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

### `scheduler.add`
- Required: `schedule`, `message`
- Optional: `id`, `agent_id`, `enabled`

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
- Notes: scaffolds `.openclawssy/agents/<agent_id>` with `memory`, `audit`, `runs` and seeded control docs (`SOUL.md`, `RULES.md`, `TOOLS.md`, `SPECPLAN.md`, `DEVPLAN.md`, `HANDOFF.md`).

### `agent.switch`
- Required: `agent_id`
- Optional: `scope` (`chat|discord|both`, default `both`), `create_if_missing`
- Notes: updates config defaults (`chat.default_agent_id` / `discord.default_agent_id`) using scoped switching; can scaffold missing agent first when `create_if_missing=true`.

### `agent.message.send`
- Required: `to_agent_id`, `message`
- Optional: `task_id`, `subject`, `channel`, `user_id`, `session_id`
- Notes: writes to inter-agent inbox sessions (`channel=agent-mail`). Optional source context fields are persisted in payload for proactive/memory traceability.

### `agent.message.inbox`
- Required: none
- Optional: `agent_id`, `limit`
- Notes: reads recent inter-agent inbox payloads for the target agent.

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
