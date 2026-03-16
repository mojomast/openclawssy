# Openclawssy Acceptance Checklist (v0.2)

Source of truth: `devplan.md`.

## Current Scope Notes
- This checklist tracks implemented prototype behavior at v0.2.
- Scheduler supports `@every <duration>`, helpful hourly/daily aliases, common hourly/daily cron shorthands, and one-shot RFC3339 schedules (no general cron parser).
- Supported sandbox providers are `none` and `local`.

## Core Platform
- [x] `go.mod` exists with module `openclawssy` and Go 1.24.
- [x] `Makefile` includes `fmt`, `lint`, `test`, and `build` targets.
- [x] Core contracts exist for runs, tools, audit events, scheduler jobs, and HTTP shapes.
- [x] Config and security specs exist.

## Config + Persistence
- [x] Config parse and validation are implemented with secure defaults.
- [x] Atomic write flow is implemented for config/scheduler/secrets persistence.
- [x] Backup/last-known-good recovery paths exist for critical persisted artifacts.
- [x] Invalid/corrupt input tests exist for key persistence flows.

## Runtime + Tooling
- [x] Runner and tool-call loop behavior are implemented and tested.
- [x] Prompt assembly is deterministic with bounded payload behavior.
- [x] Run bundles persist inputs, outputs, tool activity, and trace metadata.
- [x] Tool policy boundaries enforce workspace path/symlink protections.
- [x] Path traversal and protected control-plane writes are denied and tested.
- [x] Runtime enforces `engine.max_concurrent_runs` with bounded execution slots.
- [x] Tool argument validation enforces required + typed optional args.

## Audit + Safety
- [x] Required audit events are emitted (`run.*`, `tool.*`, `policy.denied`).
- [x] Audit logging is append-only JSONL with redaction.
- [x] Audit logger uses buffered writes with periodic flush + run-end sync semantics.
- [x] Structured tool error codes are used (`tool.not_found`, `tool.input_invalid`, `policy.denied`, `timeout`, `internal.error`).

## Sandbox + Exec Gating
- [x] Sandbox provider interface lifecycle is implemented.
- [x] `none` blocks `shell.exec`.
- [x] `local` allows `shell.exec` subject to policy constraints.
- [x] Invariant enforced: no active sandbox means no shell execution.
- [x] Unsupported sandbox providers are rejected at config validation.

## Scheduler
- [x] Job add/list/remove works via CLI/store.
- [x] Scheduler state persists across restarts.
- [x] Missed-job policy is documented and tested (no replay for `@every`; one-shot runs once then disables).
- [x] Scheduler supports bounded concurrent execution with a worker limit.
- [x] Scheduler startup catch-up behavior is configurable via `scheduler.catch_up`.

## Channels
- [x] CLI commands exist: `init`, `ask`, `run`, `serve`, `cron`, `doctor`.
- [x] HTTP run create/status endpoints exist and require bearer token auth.
- [x] HTTP run listing supports pagination + status filtering.
- [x] Server bind defaults to loopback.
- [x] Chat connector allowlist and rate limiting are enforced.
- [x] HTTP and chat requests support per-request `thinking_mode` override with validation.
- [x] Chat rate-limit responses are structured (`chat.rate_limited`) and include cooldown hints.

## Remaining Hardening Work
- [x] Expose scheduler CRUD and pause/resume controls via authenticated HTTP admin endpoints.
- [x] Add scheduler global/per-job pause-resume controls to CLI where needed.
- [x] Add audit logger buffered flush policy (time-based and on-completion flush semantics).
