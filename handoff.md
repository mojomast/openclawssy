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
- Dashboard inbox APIs now support list/detail/ack/run over the shared `message_id` lifecycle and reuse the same subagent runner path as runtime-triggered inbox execution.
- Dashboard chat now threads `instance_id` and `agent_id` through send, run polling, SSE streaming, and cancel paths.
- Dashboard eval now honors the eval feature flag in both API and UI: the nav entry hides when disabled and direct page access shows a disabled-state panel.
- Dashboard Sessions now preserves lifecycle-rich message metadata and renders operator-facing lifecycle cards for status transitions and related run/task context.
- Dashboard Agent Contract and Prompt Stack pages now load canonical instance-scoped routes using the selected instance, while legacy flat agent routes remain active-instance compatibility wrappers.
- Shared control-plane compatibility feature loading now lives in `internal/instances`, and `openclawssy eval` operational subcommands are blocked when eval is disabled.
- Dashboard inbox list/detail APIs now preserve the original message envelope across ack/run lifecycle updates by merging rows per `message_id` instead of returning sparse latest-row snapshots.
- Sessions lifecycle cards now open the canonical inbox detail view and let operators trigger inbox `ack` / `run` actions without leaving the session transcript.
- Delegation events now carry additive `parent_run_id`, `from_agent_id`, and `to_agent_id` metadata so delegated subagent work is easier to correlate across trace, ledger, and inbox-aligned operator surfaces.
- Agent Monitor launch/cancel flows now send explicit `instance_id` / `agent_id`, and monitor rows use composite identity instead of assuming bare `run_id` uniqueness.
- Agent Contract and Prompt Stack now honor `instance_agents` feature gating in the dashboard shell: nav entries hide when disabled and direct route access shows explicit disabled-state panels instead of attempting live instance/agent loads.
- Agent Monitor now matches that same `instance_agents` feature gating contract across backend routes and dashboard UI, including hidden nav, disabled direct-route shell, and suppressed launch/run controls when disabled.
- Sessions now matches that same `instance_agents` feature gating contract across backend routes and dashboard UI, including hidden nav, disabled direct-route shell, and suppressed session browsing when disabled.
- Chat now matches that same `instance_agents` feature gating contract in the dashboard shell, including hidden nav, disabled direct-route shell, and suppressed agent/session bootstrap activity when disabled.
- `/api/admin/agents` now matches the instance-aware control-plane contract too: it is feature-gated, resolves agents from the active/requested instance config, and isolates dashboard active-agent pointers by instance-scoped room key.
- Docs and Skills now match that same `instance_agents` feature gating contract too: their admin routes are feature-gated, nav entries hide when disabled, direct-route shells render explicit disabled panels, and legacy doc/skill API work is suppressed.
- Role Templates now matches that same `instance_agents` feature gating contract too: role admin routes are feature-gated, the nav entry hides when disabled, direct-route shells render an explicit disabled panel, and role-template API work is suppressed.
- Wizard now has a dashboard shell too: the nav entry hides when `wizard` is disabled, direct-route access renders an explicit disabled panel, and enabled control planes can browse instance/agent template catalogs through `/api/admin/wizard/templates`.
- Wizard instance flows now work end-to-end in the dashboard shell too: operators can choose a template, preview planned config/operations, create the instance in-place, and get clean duplicate-id errors, while wizard instance routes also enforce `instance_control`.
- Wizard agent flows now work end-to-end in the dashboard shell too: operators can target an existing instance, preview normalized profile/operations, create the agent in-place, and get duplicate-agent protection while wizard agent routes also enforce `instance_agents`.
- Wizard now supports same-session handoff from new instance creation into agent targeting, and the latest targeted tests cover preview/create parity on key wizard fields plus in-page disabled-state handling when `instance_control` is off.
- Wizard instance preview/create now shares canonical projection logic too, which reduces preview-vs-persisted drift and hardens the backend side of the milestone beyond the initial UI flow work.
- The dashboard now has an Instances page too: operators can inspect canonical instances, see the active instance clearly, activate another instance, and get deterministic disabled behavior when `instance_control` is off.
- The dashboard header now shows the active instance globally too, so instance context remains visible while working on other pages.
- The dashboard Workspace backend now resolves through canonical effective runtime identity instead of the old config-root/host fallback path, so browsing follows the active/requested instance and agent.
- The dashboard Workspace page now shows resolved workspace mode plus instance/agent identity, and in Docker mode it reads the live `/workspace` volume through the sandbox provider so the UI matches runtime `fs.*` writes.

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
- `go test ./internal/chatstore ./internal/tools ./internal/channels/chat ./internal/channels/http ./internal/channels/dashboard ./internal/runtime ./cmd/openclawssy`
- `go test ./internal/channels/dashboard -run 'TestEvalResultsEndpoint|TestControlPlaneFeaturesAndWizardEndpoints|TestInstanceFeatureFlagEnforcement' -count=1`
- `go test ./internal/channels/dashboard ./internal/instances ./cmd/openclawssy`
- `go test ./internal/channels/dashboard -run 'TestInstanceScopedPromptStackRoutesIsolateSameAgentID|TestInstanceScopedContractResolvedAndDiffEndpointsUseRequestedInstance|TestChatSessionMessagesEndpointIncludesLifecycleMetadata' -count=1`
- `go test ./internal/channels/dashboard -run 'TestInstanceInboxListAckAndRunFlow' -count=1`
- `go test ./internal/channels/dashboard ./internal/tools ./internal/runtime`
- `go test ./internal/agent ./internal/runtime ./internal/tools`
- `go test ./internal/channels/dashboard`
- `cd internal/channels/dashboard/ui && npm run typecheck`
- `cd internal/channels/dashboard/ui && npm run build`
- `cd internal/channels/dashboard/ui && npm run build`
- `cd internal/channels/dashboard/ui && CI=1 npx playwright test tests/e2e/monitor.spec.ts --reporter=line`
- `cd internal/channels/dashboard/ui && CI=1 npx playwright test tests/e2e/runs.spec.ts`
- `cd internal/channels/dashboard/ui && CI=1 npx playwright test tests/e2e/chat.spec.ts tests/e2e/eval.spec.ts`
- `cd internal/channels/dashboard/ui && CI=1 npx playwright test tests/e2e/sessions.spec.ts --reporter=line`
- `cd internal/channels/dashboard/ui && npm run build && CI=1 npx playwright test tests/e2e/contract.spec.ts tests/e2e/prompt-stack.spec.ts --reporter=line`
- `cd internal/channels/dashboard/ui && npm run typecheck && npm run build && CI=1 npx playwright test tests/e2e/contract.spec.ts tests/e2e/prompt-stack.spec.ts --reporter=line`
- `go test ./internal/channels/dashboard -run 'TestMonitor.*|Test.*Monitor.*' -count=1`
- `cd internal/channels/dashboard/ui && npm run typecheck && npm run build && CI=1 npx playwright test tests/e2e/monitor.spec.ts --reporter=line`
- `go test ./internal/channels/dashboard -run 'TestListChatSessionsEndpoint|TestListChatSessionsEndpointPagination|TestChatSessionMessagesEndpoint|TestChatSessionMessagesEndpointIncludesToolMetadata|TestChatSessionMessagesEndpointIncludesLifecycleMetadata|TestChatSessionMessagesEndpointPreservesMultiStepOrder|TestSessionsRoutesRequireInstanceAgentsFeature|TestListChatSessionsEndpointInvalidLimit' -count=1`
- `cd internal/channels/dashboard/ui && npm run typecheck && npm run build && CI=1 npx playwright test tests/e2e/sessions.spec.ts --reporter=line`
- `go test ./internal/channels/dashboard -run TestIntegrationDashboardPagesUseSharedShadcnUIComponents -count=1`
- `cd internal/channels/dashboard/ui && npm run typecheck && npm run build && CI=1 npx playwright test tests/e2e/chat.spec.ts --reporter=line`
- `go test ./internal/channels/dashboard -run 'TestAdminAgentsEndpointListAndSetActive|TestAdminAgentsEndpointUsesActiveInstanceConfigAndInstanceScopedPointers|TestAdminAgentsEndpointRequiresInstanceAgentsFeature|TestMonitorRoutesRequireInstanceAgentsFeature|TestSessionsRoutesRequireInstanceAgentsFeature|TestInstanceFeatureFlagEnforcement' -count=1`
- `cd internal/channels/dashboard/ui && npm run typecheck && npm run build && CI=1 npx playwright test tests/e2e/chat.spec.ts tests/e2e/monitor.spec.ts --reporter=line`
- `go test ./internal/channels/dashboard`
- `cd internal/channels/dashboard/ui && npm run build && npm run e2e:test -- tests/e2e/workspace.spec.ts tests/e2e/auth.spec.ts tests/e2e/cross-area-integration.spec.ts`

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
2. Finish composite identity adoption in remaining dashboard/runtime consumers beyond the newly landed SSE/event-bus, Runs page, Delegation page, Sessions page, Contract/Prompt Stack, and Workspace slices.
3. Connect the richer eval/delegation metadata contract to more real producers and more dashboard consumers beyond the eval detail and Runs detail panels.
4. Finish broader runtime-side feature enforcement so disabled features cannot still be reached through non-dashboard execution paths beyond the newly landed eval CLI gating.

Still open overall:

- wizard preview/create parity validation against canonical manifests
- dashboard UI wiring for canonical instance flows beyond Sessions, Agent Contract, Prompt Stack, and Workspace
- full feature-flag enforcement polish in UI/API/runtime beyond the current dashboard API/UI and eval CLI coverage
- migration cleanup of remaining legacy flat-agent assumptions
- broader validation pass (`go test ./...`, build, doctor, dashboard/e2e as needed)

## Known debt / caveats

- Dashboard instance projection still has lossy compatibility shaping in some paths.
- Canonical clone fidelity and metadata provenance are not fully complete.
- Messaging is now instance-scoped and lifecycle-aware, but it is still chatstore-backed rather than a dedicated canonical inbox store.
- Dashboard/eval/decision views and some API/runtime guards are improved, but broader composite identity adoption, UI/runtime feature gating, and delegation metadata rollout are still incomplete.
- The dashboard UI fix landed for chat/eval operator flows plus Sessions/Contract/Prompt Stack, but more pages still need to consume the shared control-plane feature hook and canonical instance identity consistently.
- Contract and Prompt Stack now match Eval's disabled-nav/direct-access behavior, but the same shared feature-state determinism still needs to reach more instance-agent-dependent pages.
- Monitor now joins Eval, Contract, and Prompt Stack in that disabled-nav/direct-access pattern, but more instance-agent-dependent pages still need the same treatment.
- Sessions now joins Eval, Monitor, Contract, and Prompt Stack in that disabled-nav/direct-access pattern, but Chat and more instance-agent-dependent pages still need the same treatment.
- Chat now joins Sessions, Eval, Monitor, Contract, and Prompt Stack in that disabled-nav/direct-access pattern, but more instance-agent-dependent pages and selective backend guardrails still need the same treatment.
- Chat now joins Sessions, Eval, Monitor, Contract, and Prompt Stack in that disabled-nav/direct-access pattern, and `/api/admin/agents` now aligns with instance-aware selection too; more instance-agent-dependent pages still need the same treatment.
- Workspace browsing now aligns with canonical runtime identity and Docker-backed `/workspace`, but broader live-environment verification and any remaining host-fallback assumptions in adjacent surfaces still need review.

## Recommended next starting point

- Re-read `rfchandoff.md`, `devplan.md`, and `orchestrator_prompt.md`.
- Then inspect delegation and messaging lifecycle paths so the next slice can build on the new dashboard/eval/session identity metadata without inventing a parallel model.
