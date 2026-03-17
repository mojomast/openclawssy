# RFC Handoff

Date: 2026-03-16

## Purpose

This handoff is for completing the architecture described by:

- `ENGINEERING_RFC.md` - source of truth for target architecture
- `devplan.md` - source of truth for sequencing, milestones, and tests
- `orchestrator_prompt.md` - source of truth for subagent topology, ownership, and review discipline

Precedence rules:

1. architecture questions -> `ENGINEERING_RFC.md`
2. implementation order -> `devplan.md`
3. coordination / ownership / anti-patterns -> `orchestrator_prompt.md`

Do not continue this work from memory alone. Re-read all three files first.

## Locked Invariants

These are non-negotiable and already guided the current implementation:

- Prompt stack is the runtime source of truth.
- Legacy prompt docs are compatibility mirrors only.
- Instance is the packaging boundary.
- Agents belong to instances.
- Workspace is instance-scoped first, agent-scoped second.
- Inter-agent communication is same-instance by default.
- Feature flags must be enforced in UI, API, and runtime.
- Eval, delegation, roles, dashboard, and runtime must share the same instance/agent/run identity model.

## What Is Implemented

### 1. Canonical instance foundation

Implemented in:

- `internal/instances/types.go`
- `internal/instances/paths.go`
- `internal/instances/store.go`
- `internal/instances/store_test.go`

Current capabilities:

- canonical `InstanceManifest`
- canonical `AgentManifest`
- canonical `FeatureFlagState` and `FeatureSet`
- canonical `EffectiveRuntime`
- instance/agent path helpers under `.openclawssy/instances/...`
- atomic load/save/list helpers for instances and agents
- active instance pointer support
- default bootstrap migration from current flat config/docs into `default`
- copy-forward of legacy docs into instance agent docs without deleting legacy files
- prompt stack seeding from docs during effective-runtime resolution
- instance roles / skills / channels sidecar persistence

### 2. Runtime integration

Implemented in:

- `internal/runtime/engine.go`
- `internal/promptstack/store.go`

Current capabilities:

- `ExecuteInput` now accepts `InstanceID`
- runtime resolves through `instances.ResolveEffectiveRuntime(...)`
- runtime bootstraps `default` instance on demand for compatibility
- prompt stack is used as runtime truth via instance-aware prompt stack storage
- instance-aware prompt stack storage supports `.openclawssy/instances/<instance>/agents/<agent>/promptstack/...`
- default instance keeps old operational paths where needed for compatibility during transition
- delegated subagent runs now inherit the parent `instance_id`
- tool execution request context now carries `instance_id` so runtime-triggered agent tools can stay instance-scoped

### 2.5 HTTP and messaging identity propagation

Implemented in:

- `internal/channels/http/server.go`
- `internal/channels/http/pipeline.go`
- `internal/channels/http/events.go`
- `internal/channels/http/run_cancel_tracker.go`
- `internal/tools/registry.go`
- `internal/tools/agent_tools.go`

Current capabilities:

- `POST /v1/runs` accepts optional `instance_id`
- persisted HTTP runs store `instance_id`
- SSE run events include `instance_id` and `agent_id`
- SSE/event-bus subscribers can now opt into composite `(instance_id, agent_id, run_id)` addressing while compatibility subscribers still receive dual-published bare `run_id` events
- HTTP queued-run tracker now stores both bare `run_id` and composite `instance_id:agent_id:run_id`
- HTTP cancel keeps bare `run_id` compatibility and falls back to composite identity
- `agent.message.send` and `agent.message.inbox` are explicitly instance-scoped
- inter-agent messages now persist and return `instance_id`
- proactive memory-triggered messages stay in the same instance as the originating run

### 2.6 Dashboard and eval identity normalization

Implemented in:

- `internal/channels/dashboard/handler.go`
- `internal/channels/dashboard/decisions_api.go`
- `internal/channels/dashboard/eval_api.go`
- `internal/eval/types.go`
- `internal/eval/store.go`

Current capabilities:

- dashboard monitor runs now return explicit `instance_id`
- dashboard monitor reconciliation keys runs by composite identity instead of bare `run_id`
- dashboard monitor cancel accepts `instance_id` and `agent_id`, then prefers composite cancel lookup with compatibility fallback
- dashboard trace and status payloads now include `instance_id`
- dashboard decisions payloads now include `instance_id`
- dashboard decision loading reads canonical instance audit roots while preserving legacy default-instance compatibility
- eval `SuiteRun` now carries additive `identity { instance_id, agent_id, run_id, parent_run_id }`
- eval storage persists additive identity metadata via `identity_json` with migration compatibility handling
- dashboard eval API now returns the additive eval identity block

### 2.7 Messaging lifecycle and deeper lineage metadata

Implemented in:

- `internal/tools/agent_tools.go`
- `internal/chatstore/store.go`
- `internal/runtime/trace.go`
- `internal/runtime/engine.go`
- `internal/audit/logger.go`
- `internal/channels/http/store.go`

Current capabilities:

- inter-agent inbox messages now carry stable `message_id`
- `agent.message.send` returns lifecycle metadata (`message_id`, `status`) and persists structured envelope fields alongside legacy raw content
- inbox entries now expose structured lifecycle metadata and collapse repeated status updates by `message_id`
- `agent.message.send` can optionally auto-run the recipient agent and now emits real `running` / `completed` / `failed` lifecycle transitions through the shared message model
- `agent.run` reuses the same lifecycle helper when invoked against an existing `message_id`
- sender/recipient communication allowlists are enforced when present (`can_message`, `can_receive_from`)
- cross-agent inbox reads now require `policy.admin`
- runtime trace, audit, and stored HTTP runs now carry additive `parent_run_id`
- trace and audit metadata now surface additive `instance_id` / `agent_id` lineage directly instead of forcing all consumers to infer from file paths
- eval storage now preserves richer additive identity/runtime metadata (`root_run_id`, `source`, `task_id`, `session_id`, artifact/checkpoint/delegation/trace metadata)
- proactive memory-triggered inbox delivery now uses the shared auto-run lifecycle path and preserves `parent_run_id` linkage into spawned agent execution

### 2.8 Dashboard eval metadata consumer adoption

Implemented in:

- `internal/channels/dashboard/ui/src/pages/eval/types.ts`
- `internal/channels/dashboard/ui/src/pages/eval/utils.ts`
- `internal/channels/dashboard/ui/src/pages/eval/EvalRunDetailPanel.tsx`

Current capabilities:

- dashboard eval parsing now preserves additive `identity` and `metadata` blocks returned by the backend
- eval detail panels now render instance/agent/run lineage, task/session/source linkage, artifact/checkpoint paths, and a lightweight delegation summary for operators
- delegation/decomposition metadata is now visible in the dashboard without requiring raw JSON inspection

### 3. Tests currently passing

Verified:

- `go test ./internal/runtime ./internal/promptstack ./internal/instances`
- `go test ./internal/channels/dashboard`
- `go test ./internal/channels/http`
- `go test ./internal/tools`
- `go test ./internal/eval`
- `go test ./cmd/openclawssy`
- `go test ./internal/channels/dashboard ./internal/eval ./internal/channels/http ./internal/tools ./internal/runtime ./cmd/openclawssy`
- `go test ./internal/tools ./internal/runtime ./internal/channels/http`
- `cd internal/channels/dashboard/ui && npm run typecheck`
- `cd internal/channels/dashboard/ui && npm run build`

Note: one broader `go test ./internal/runtime ./...` package pass exposed an existing flaky/unrelated failure in `TestEngineExecuteIngestsMemoryEventsWhenEnabled`, but the focused rerun of that test passed immediately and the targeted package suites for the changed slices passed.

## Previously Non-Canonical Area That Has Since Been Reworked

The dashboard-local instance API/store was originally non-canonical. That area has since been rewritten to project to and from `internal/instances`, but some projection debt remains.

- Current canonicalized backend entrypoints:
  - `internal/channels/dashboard/instances_api.go`
  - `internal/channels/dashboard/instances_canonical.go`

Remaining issues:

- projection still rebuilds some state through compatibility config shaping
- clone fidelity and metadata provenance are not fully canonical yet
- wizard/backend/UI parity still needs broader review

## Important Current Contracts

These contracts now exist and should be reused everywhere:

### Shared data model

- `internal/instances/types.go`
- `internal/instances/store.go`

Key types:

- `FeatureFlagState`
- `FeatureSet`
- `InstanceManifest`
- `AgentManifest`
- `EffectiveRuntime`

### Runtime authority

- `instances.ResolveEffectiveRuntime(rootDir, instanceID, agentID)`

Rules:

- no new runtime path should independently resolve model/tools/prompt/delegation after this
- all future runtime gating should branch from `EffectiveRuntime`

### Prompt stack authority

- `internal/promptstack/store.go`
- `internal/runtime/engine.go`

Rules:

- prompt stack is runtime truth
- docs are migration/materialization surface only
- legacy docs still exist for compatibility, but should not become hidden truth again

## What Is Still Missing To Complete The RFC

### Milestone 5-7 work remains open

Not finished:

- canonical wizard backend
- dashboard/UI integration
- deepen first-class messaging / inbox lifecycle wiring so more producers/consumers emit `running` / `completed` / `failed` updates through the same message model
- finish composite run identity adoption across remaining runtime + HTTP + eval/dashboard consumers beyond the newly landed SSE/event-bus addressing slice
- feature flag enforcement from one canonical source across UI/API/runtime
- connect richer eval/delegation metadata to more real producers/CLI flows and more dashboard surfaces beyond the current additive storage contract + eval detail panel
- migration cleanup of remaining legacy flat-agent callers

## Required Parallel Subagent Plan

Resume with parallel subagents, but keep file ownership strict.

### A. storage-schema

Owns:

- `internal/instances/**`

Next tasks:

- add any missing manifest fields still required by the RFC
- add helper APIs needed by dashboard/http/messaging without moving logic elsewhere
- add clone helpers if API/backend needs them

Do not:

- move storage logic into dashboard or runtime packages

### B. runtime-integrator

Owns:

- `internal/runtime/**`
- `cmd/openclawssy/main.go`
- `internal/channels/http/**` when touching execution identity only

Next tasks:

- continue normalizing run identity to `(instance_id, agent_id, run_id)` across dashboard/eval surfaces
- remove remaining direct flat-agent assumptions from run/audit/memory/session paths where safe

Do not:

- bypass `ResolveEffectiveRuntime`

### C. prompt-unifier

Owns:

- `internal/promptstack/**`
- prompt-related parts of `internal/channels/dashboard/**`

Next tasks:

- make dashboard prompt-stack routes instance/agent scoped
- make docs editor clearly a compatibility mirror editor
- add stronger parity tests for preview == runtime prompt source
- add materialization/export helpers if missing

Do not:

- reintroduce direct legacy doc reads as runtime truth

### D. api-backend

Owns:

- `internal/channels/dashboard/**` for admin APIs

Next tasks:

- replace dashboard-local instance store with handlers backed by `internal/instances`
- implement canonical routes from RFC section 11
- standardize validation and error shapes
- make wizard plan/create use shared manifest builders/validators

Do not:

- persist instance state in dashboard-local files
- use config snapshots as the primary instance format

### E. dashboard-ui

Owns:

- `internal/channels/dashboard/ui/**`

Next tasks:

- add active instance view
- add instance-scoped agent management
- add wizard flows backed by canonical APIs
- expose prompt source clearly
- expose disabled/read-only feature flags clearly

Do not:

- build UI before backend contracts are finalized

### F. messaging-concurrency

Owns:

- `internal/tools/agent_tools.go`
- `internal/runtime/run_tracker.go`
- `internal/channels/http/run_cancel_tracker.go`
- related messaging/concurrency paths

Next tasks:

- extend current instance-scoped messaging compatibility layer into a first-class message model with `message_id`, sender, recipient, task/run linkage, and status
- enforce same-instance default and permissions from agent manifests, not just instance presence + compatibility config gate
- add inbox lifecycle: queued / acknowledged / running / completed / failed
- finish composite identity adoption in all remaining trackers/consumers

Do not:

- leave messaging as unpermissioned chatstore-only behavior

### G. qa-eval

Owns:

- `internal/eval/**`
- integration tests across runtime/dashboard/http

Next tasks:

- normalize eval metadata to include `instance_id`, `agent_id`, `run_id`, and parent linkage where relevant
- align dashboard run/decision/eval views with the same metadata contract
- add migration tests
- add parallel isolation tests
- add messaging permission tests
- add feature-flag enforcement tests
- add wizard preview/create equality tests against canonical manifests

Do not:

- create a separate metadata model from runtime

## Required Shared Contracts Before More Parallel Work

All subagents must align on these before merging more code:

1. `InstanceManifest`
2. `AgentManifest`
3. `EffectiveRuntime`
4. run identity = `(instance_id, agent_id, run_id)`
5. message model = permissioned, same-instance by default
6. feature flag model = `enabled/read_only/visible`
7. wizard plan/create outputs must come from the same builders
8. validation errors must use structured dashboard/API error payloads

## Merge / Review Rules

Reject changes that:

- add another storage system for instances or agents
- bypass `ResolveEffectiveRuntime`
- make legacy docs runtime truth again
- add wizard logic separate from canonical builders
- enforce features only in UI
- create IDs inconsistent with instance/agent/run identity

Each subagent report should include:

- requirement satisfied
- owning subagent
- dependency status
- tests added or required
- integration risks

And also:

- files touched
- interfaces changed
- blockers
- assumptions

## Recommended Completion Order

1. extend messaging/concurrency model from compatibility layer to first-class inbox lifecycle
2. deepen eval/delegation metadata normalization and parent linkage adoption
3. finish composite identity adoption in any remaining dashboard/eval/runtime consumers
5. wire dashboard UI to canonical APIs
6. finish feature-flag enforcement in API/runtime/UI
7. run broader validation

## Minimum Validation Before Declaring Done

Run at least:

- `go test ./internal/instances`
- `go test ./internal/runtime`
- `go test ./internal/channels/dashboard`
- `go test ./internal/channels/http`
- `go test ./internal/eval`
- broader `go test ./...` once interfaces stabilize

If UI changes land, also run the relevant dashboard Playwright coverage.

## Done Condition Reminder

The RFC is only complete when an operator can answer these clearly:

- What instance is active?
- Which agents belong to it?
- What is the real runtime prompt source?
- Which features are disabled or read-only?
- Which agent owns each channel?
- Which agents can message each other?
- Which model/tools/restrictions applied to a run?
- Why was delegation allowed or blocked?
- Can the environment be cloned and run elsewhere?
- Does wizard preview exactly match created manifests?

If any answer is still fuzzy, the RFC is not complete.
