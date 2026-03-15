# Memory System Guide

This document explains what Openclawssy's memory system does, how it works end to end, and how to contribute safely.

## What the Memory System Does

Openclawssy memory provides a full lifecycle:

- Captures important runtime events (messages, tool calls/results, errors, scheduler runs).
- Distills events into structured long-lived memory items.
- Injects relevant memory into prompt assembly before model turns.
- Maintains memory quality with periodic dedupe/archive/report tasks.
- Triggers proactive inter-agent signals when meaningful memory events occur.
- Supports optional embedding-backed semantic retrieval.

## High-Level Architecture

Per agent, memory lives under:

- `.openclawssy/agents/<agent>/memory/events/` (raw append-only JSONL event stream)
- `.openclawssy/agents/<agent>/memory/memory.db` (working memory + embeddings)
- `.openclawssy/agents/<agent>/memory/checkpoints/` (checkpoint outputs)
- `.openclawssy/agents/<agent>/memory/reports/` (maintenance reports)

Main layers:

1. Event stream ingestion (non-blocking queue + JSONL).
2. Working memory store (SQLite + FTS for lexical recall).
3. Checkpoint distillation (model JSON output + strict parser + fallback).
4. Prompt recall injection (importance/status/size bounded).
5. Maintenance and proactive hooks.
6. Optional embeddings for semantic hybrid recall.

## Data Flow

### 1) Event ingestion

Runtime ingests redacted events after run execution, including:

- `user_message`
- `assistant_output`
- `tool_call`
- `tool_result`
- `error`
- `scheduler_run`
- `decision_log`
- `checkpoint`
- `maintenance`

Writes are append-only and non-blocking. Queue pressure drops events safely and tracks drop stats.

### 2) Memory item storage

`memory.db` stores structured items (`memory_items`) and FTS index (`memory_fts`) with fields like:

- `kind`, `title`, `content`
- `importance` (1-5)
- `confidence` (0-1)
- `status` (`active|forgotten|archived`)
- `created_at`, `updated_at`

### 3) Distillation checkpoints

`memory.checkpoint`:

- Reads new events since last checkpoint.
- Uses model distillation with strict JSON schema validation.
- Falls back to deterministic distillation when model path fails.
- Upserts new items and applies updates.
- Persists checkpoint files and emits a checkpoint event.

### 4) Recall injection

Before each model turn, runtime may inject a bounded memory block:

- Prefers active, higher-importance memory.
- Uses recency-aware ordering.
- Respects prompt budget derived from config.

### 5) Weekly maintenance

`memory.maintenance` performs:

- duplicate archival,
- stale low-importance archival,
- verification candidate detection,
- DB compaction (`VACUUM`),
- report generation.

### 6) Proactive hooks

On successful checkpoint/maintenance signals, runtime may send a proactive inter-agent message if required context exists:

- `channel`
- `user_id`
- `session_id`

No context means safe skip.

### 7) Embeddings (optional)

When enabled:

- writes/updates/checkpoint upserts synchronize vectors,
- `memory.search` can run semantic retrieval and merge with FTS results,
- response includes `mode` (`fts` or `semantic_hybrid`).

OpenRouter is supported through OpenAI-compatible `/embeddings` API behavior.

## Tools

Memory-related tools:

- `memory.search`
- `memory.write`
- `memory.update`
- `memory.forget`
- `memory.health`
- `decision.log`
- `memory.checkpoint`
- `memory.maintenance`

Related proactive tool surface:

- `agent.message.send` (supports optional source context fields)
- `agent.message.inbox`

## Configuration

Relevant `memory` settings:

- `enabled`
- `max_working_items`
- `max_prompt_tokens`
- `auto_checkpoint`
- `proactive_enabled`
- `embeddings_enabled`
- `embedding_provider`
- `embedding_model`
- `event_buffer_size`

Embedding provider supports:

- `openai`, `openrouter`, `requesty`, `zai`, `openai_compat`

OpenRouter defaults are wired via `providers.openrouter.base_url` and `OPENROUTER_API_KEY`.

## Scheduler Defaults

When enabled by config, startup ensures default jobs exist:

- checkpoint: `@every 6h` -> `/tool memory.checkpoint {}`
- maintenance: `@every 168h` -> `/tool memory.maintenance {}`

## Observability

Admin memory endpoint:

- `GET /api/admin/memory/<agent>`

Includes:

- memory health counts,
- active items,
- embedding stats (vector count, coverage, model split, semantic availability).

## Security Model

Memory follows existing runtime safety posture:

- redact sensitive content before persistence,
- per-agent scoped paths only,
- no cross-agent path traversal,
- config and policy gating for powerful behaviors,
- append-only event lineage for auditability.

## Contributing to Memory

### Where to work

Core packages:

- `internal/memory/`
- `internal/memory/store/`
- `internal/tools/memory_tools.go`
- `internal/runtime/engine.go`

Related surfaces:

- `internal/config/config.go`
- `internal/channels/dashboard/handler.go`
- `docs/specs/CONFIG.md`
- `docs/TOOL_CATALOG.md`

### Contribution rules

1. Keep changes agent-scoped and path-safe.
2. Reuse redaction before persistence.
3. Preserve deterministic fallbacks for model-dependent logic.
4. Keep tool outputs structured and machine-readable.
5. Add tests for new behavior and edge cases.
6. Update docs when adding config/tools/endpoints.

### Testing

Run at minimum:

```bash
go test ./internal/memory/... ./internal/tools ./internal/runtime ./internal/config
```

Before merge, run full suite:

```bash
go test ./...
```

### Good first contribution ideas

- improve checkpoint prompt quality while preserving strict schema,
- add richer maintenance heuristics with clear report fields,
- add optional embedding provider-specific headers/telemetry,
- expand admin memory stats with trend windows.

### Common pitfalls

- breaking prompt budget constraints,
- writing unredacted strings to memory artifacts,
- assuming proactive context exists in all channels,
- embedding sync failures must be best-effort (all call sites use `_ = maybeSyncMemoryEmbedding`),
- all 8 memory tools must gate on `memory.enabled` and return "memory is disabled" when off.
- depending on embeddings path when embeddings are disabled.
