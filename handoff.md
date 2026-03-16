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

## Validation completed

- `go test ./internal/instances`
- `go test ./internal/channels/dashboard ./internal/instances`
- `go test ./internal/channels/http`
- `go test ./internal/tools`
- `go test ./internal/runtime`
- `go test ./internal/eval`
- `go test ./cmd/openclawssy`
- `go test ./internal/channels/dashboard ./internal/eval ./internal/channels/http ./internal/tools ./internal/runtime ./cmd/openclawssy`

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
2. Connect the richer eval/delegation metadata contract to more real producers and dashboard consumers.
3. Finish composite identity adoption in remaining dashboard/runtime/SSE consumers.

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
- Dashboard/eval/decision views are improved, but some broader composite identity, SSE addressing, and delegation metadata adoption is still incomplete.

## Recommended next starting point

- Re-read `rfchandoff.md`, `devplan.md`, and `orchestrator_prompt.md`.
- Then inspect delegation and messaging lifecycle paths so the next slice can build on the new dashboard/eval identity metadata without inventing a parallel model.
