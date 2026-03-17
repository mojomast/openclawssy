# Handoff

## Status: RFC architecture work in progress

Date: 2026-03-16

## What is complete so far

- Canonical instance/agent storage exists under `internal/instances`.
- Runtime resolves effective execution state from `(instance_id, agent_id)`.
- Prompt stack is the runtime source of truth, with legacy docs acting as compatibility mirrors.
- Dashboard backend instance and agent APIs now project through canonical `internal/instances` storage.
- Dashboard prompt-stack routes are instance-aware.
- HTTP run creation accepts and persists `instance_id`.
- HTTP SSE run events emit `instance_id` and `agent_id`.
- Runtime delegated subagent runs inherit the parent `instance_id`.
- Inter-agent messaging is now explicitly instance-scoped and returns/persists `instance_id`.
- HTTP queued-run tracking now stores both bare `run_id` and composite `instance_id:agent_id:run_id`, and cancel falls back through the composite key.
- Dashboard monitor/status/trace/decision surfaces now emit `instance_id` and avoid cross-instance `run_id` collisions.
- Eval storage and dashboard eval API now carry additive `identity { instance_id, agent_id, run_id, parent_run_id }` metadata.
- Messaging now has a first-class envelope foundation with stable `message_id`, lifecycle status metadata, inbox authorization checks, and communication allowlist enforcement.
- Runtime trace, audit, stored HTTP runs, dashboard trace/decision surfaces, and eval storage now carry deeper lineage metadata including `parent_run_id` and richer eval identity/runtime fields.
- `agent.message.send` can now auto-run the recipient agent and drive real inbox lifecycle transitions through `running`, `completed`, and `failed`.
- Proactive memory hooks now reuse that same message lifecycle path, preserving `parent_run_id` linkage into spawned runs.
- Delegated subagent execution now carries stable delegated `message_id` values and records `message_id` / `related_run_id` in delegation events.
- Runtime `RunTracker` and `run.cancel` now support composite `(instance_id, agent_id, run_id)` cancellation while preserving legacy bare `run_id` compatibility.
- HTTP SSE/event-bus delivery now supports composite `(instance_id, agent_id, run_id)` subscriptions while dual-publishing to legacy bare `run_id` listeners.
- Dashboard eval detail panels now render additive identity/runtime/delegation metadata instead of dropping it in the UI.
- Dashboard Runs and Delegation pages now thread composite identity into detail/trace/decision fetches, and Runs detail renders a structured lineage/delegation summary.
- Dashboard wizard, instance-control, and instance-agent admin APIs now enforce their control-plane feature flags with structured `403` responses.

## Validation completed

- `go test ./internal/instances`
- `go test ./internal/channels/dashboard ./internal/instances`
- `go test ./internal/channels/http`
- `go test ./internal/tools`
- `go test ./internal/runtime`
- `go test ./internal/eval`
- `go test ./cmd/openclawssy`
- `go test ./internal/channels/dashboard ./internal/eval ./internal/channels/http ./internal/tools ./internal/runtime ./cmd/openclawssy`
- `go test ./internal/tools ./internal/runtime ./internal/channels/http`
- `go test ./internal/agent -run TestExecuteDelegatedTasksRecordsMessageBackedLifecycleMetadata -count=1`
- `go test ./internal/runtime -run 'TestSubAgentAdapterPassesDefaultRestrictions|TestSubAgentAdapterReturnsMessageIDInOutput' -count=1`
- `go test ./internal/runtime -run '^TestRunTracker_' -count=1`
- `go test ./internal/tools -run '^TestRunCancelTool_' -count=1`
- `go test ./internal/channels/dashboard -run 'TestControlPlaneFeaturesAndWizardEndpoints|TestInstanceFeatureFlagEnforcement' -count=1`
- `go test ./internal/runtime ./internal/tools ./internal/channels/dashboard`
- `cd internal/channels/dashboard/ui && npm run typecheck`
- `cd internal/channels/dashboard/ui && npm run build`
- `cd internal/channels/dashboard/ui && CI=1 npx playwright test tests/e2e/runs.spec.ts`

Note:

- A broad focused package pass hit an existing flaky/unrelated failure in `internal/runtime` (`TestEngineExecuteIngestsMemoryEventsWhenEnabled`), but rerunning that test directly passed and the targeted changed-package suites succeeded.

## Important files touched in this RFC pass

- `internal/instances/store.go`
- `internal/instances/store_test.go`
- `internal/channels/dashboard/instances_api.go`
- `internal/channels/dashboard/instances_canonical.go`
- `internal/channels/dashboard/prompt_stack_api.go`
- `internal/channels/dashboard/contract_api.go`
- `internal/channels/dashboard/handler.go`
- `internal/channels/dashboard/decisions_api.go`
- `internal/channels/dashboard/eval_api.go`
- `internal/channels/http/server.go`
- `internal/channels/http/pipeline.go`
- `internal/channels/http/events.go`
- `internal/channels/http/run_cancel_tracker.go`
- `internal/channels/http/store.go`
- `internal/runtime/engine.go`
- `internal/runtime/trace.go`
- `internal/audit/logger.go`
- `internal/eval/store.go`
- `internal/eval/types.go`
- `internal/chatstore/store.go`
- `internal/tools/registry.go`
- `internal/tools/agent_tools.go`
- `cmd/openclawssy/main.go`

## Remaining RFC work

Highest priority next:

1. Extend the new messaging lifecycle foundation so more producers/consumers emit `running` / `completed` / `failed` updates through the same `message_id` model.
2. Finish composite identity adoption in remaining dashboard/runtime consumers beyond the newly landed SSE/event-bus, Runs page, Delegation page, and `run.cancel` slices.
3. Connect the richer eval/delegation metadata contract to more real producers and more dashboard consumers beyond the eval detail and Runs detail panels.

Still open overall:

- wizard preview/create parity validation against canonical manifests
- dashboard UI wiring for canonical instance flows
- full feature-flag enforcement polish in UI/API/runtime
- migration cleanup of remaining legacy flat-agent assumptions
- broader validation pass (`go test ./...`, build, doctor, dashboard/e2e as needed)

## Known debt / caveats

- Dashboard instance projection still has lossy compatibility shaping in some paths.
- Canonical clone fidelity and metadata provenance are not fully complete.
- Messaging is now instance-scoped and lifecycle-aware, but it is still chatstore-backed rather than a dedicated canonical inbox store.
- Dashboard/eval/decision views and some API guards are improved, but broader composite identity adoption, UI/runtime feature gating, and delegation metadata rollout are still incomplete.

## Recommended next starting point

- Re-read `rfchandoff.md`, `devplan.md`, and `orchestrator_prompt.md`.
- Then inspect delegation and messaging lifecycle paths so the next slice can build on the new dashboard/eval identity metadata without inventing a parallel model.
