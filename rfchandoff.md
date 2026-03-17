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
- delegated subagent execution now synthesizes stable delegated `message_id` values and propagates them back through `SubAgentOutput`
- delegation ledger events now persist additive `message_id` and `related_run_id` fields so delegated task outcomes can be correlated with shared message lifecycle metadata
- delegation events now also persist additive `parent_run_id`, `from_agent_id`, and `to_agent_id` fields so trace/ledger consumers can correlate delegated work with sender/recipient identity without inferring it from surrounding context
- runtime `RunTracker` now exposes composite track/cancel/remove helpers so runtime and tool-layer cancellation can converge on the same `(instance_id, agent_id, run_id)` identity model
- `run.cancel` now accepts optional `instance_id` and `agent_id`, rejects partial composite identity, and prefers composite cancellation before falling back to legacy bare `run_id`

### 2.8 Dashboard run/delegation composite identity adoption

Implemented in:

- `internal/channels/dashboard/ui/src/lib/decisions.ts`
- `internal/channels/dashboard/ui/src/pages/RunsPage.tsx`
- `internal/channels/dashboard/ui/src/pages/DelegationPage.tsx`
- `internal/channels/dashboard/ui/tests/e2e/runs.spec.ts`

Current capabilities:

- dashboard run and decision parsers now preserve additive `instance_id` metadata instead of dropping it client-side
- Runs page now threads `instance_id` / `agent_id` into run detail, trace, and decision requests when opening a run
- Delegation page now threads the same composite identity into plan/detail and run-comparison decision requests
- Runs detail now renders a structured identity/delegation summary so operators can inspect lineage without opening raw JSON
- Agent Monitor now threads `instance_id` / `agent_id` through launch and cancel flows, and its row identity no longer depends on bare `run_id` alone
- monitor list/control backend routes now require the `instance_agents` feature, and the dashboard shell hides the Monitor nav entry plus renders a disabled-state panel on direct access when that feature is off

### 2.9 Dashboard API feature-flag enforcement

Implemented in:

- `internal/channels/dashboard/instances_api.go`
- `internal/channels/dashboard/instances_api_test.go`

Current capabilities:

- wizard routes now return structured `403` errors when the `wizard` feature is disabled
- instance CRUD/activate/clone/bootstrap routes now return structured `403` errors when `instance_control` is disabled
- instance-agent admin routes now return structured `403` errors when `instance_agents` is disabled
- eval results routes now return structured `403` errors when the `eval` feature is disabled
- control-plane feature introspection remains ungated so operators can still discover disabled features
- `/api/admin/agents` now also returns structured `403` errors when `instance_agents` is disabled, so agent selection/bootstrap matches the rest of the instance-agent control plane
- `/api/admin/agent/docs` and `/api/admin/skills` now also return structured `403` errors when `instance_agents` is disabled, so those remaining legacy agent surfaces follow the same feature contract
- `/api/admin/roles` and `/api/admin/roles/{name}` now also return structured `403` errors when `instance_agents` is disabled, so role template management follows that same feature contract too
- wizard backend routes were already present, and the dashboard now has a first-class `/wizard` route that consumes `/api/admin/wizard/templates` while honoring `feature.wizard_disabled` in nav and direct-route behavior
- wizard instance plan/create routes now also require `instance_control`, and the dashboard shell can preview/create instances in-place from template-backed wizard forms without leaving `/wizard`
- wizard agent plan/create routes now also require `instance_agents`, and the dashboard shell can target existing instances, preview normalized agent profiles, and create agents in-place with duplicate-agent conflict handling
- wizard now also carries operators from successful instance creation into agent targeting within the same page, and focused parity tests now assert that wizard previews and creates stay aligned on key instance/agent runtime fields

### 2.10 Dashboard inbox lifecycle APIs and chat identity threading

Implemented in:

- `internal/channels/dashboard/instances_api.go`
- `internal/channels/dashboard/instances_api_test.go`
- `internal/chatstore/store.go`
- `internal/tools/agent_tools.go`
- `internal/runtime/engine.go`
- `cmd/openclawssy/main.go`
- `internal/channels/http/server.go`
- `internal/channels/chat/connector.go`
- `internal/channels/dashboard/ui/src/pages/ChatPage.tsx`

Current capabilities:

- dashboard inbox list/detail/ack/run routes now resolve message lifecycle state by `(agent_id, message_id)` instead of duplicating a parallel inbox model
- dashboard inbox `run` reuses the shared message lifecycle runner through a wired `tools.AgentRunner`
- dashboard inbox list/detail handlers now merge sparse lifecycle rows by `message_id`, preserving original message envelope/task/source data while surfacing the latest status and related run metadata
- chat queue/request/response types now carry additive `instance_id` and `agent_id`
- dashboard chat send, run polling, SSE subscribe, and cancel flows now thread composite identity through the operator UI
- chat nav/direct-access behavior now honors the shared `instance_agents` feature state, suppressing agent/session bootstrap work and rendering an explicit disabled-state panel with disabled controls when the feature is off
- `/api/admin/agents` now resolves against the active or explicitly requested instance config and keeps dashboard active-agent pointers isolated per instance-scoped room, so Chat/Monitor selection no longer leaks root-config/global pointer behavior across instances
- Docs and Skills nav/direct-access behavior now honors the same shared `instance_agents` feature state, hiding both entries, rendering disabled-state panels on direct access, and suppressing doc/skill API calls when the feature is off
- Role Templates nav/direct-access behavior now honors that same shared `instance_agents` feature state, hiding the nav entry, rendering a disabled-state panel on direct access, and suppressing role-template API calls when the feature is off
- Wizard nav/direct-access behavior now honors the shared `wizard` feature state too, and the dashboard shell can now browse instance/agent template catalogs through the existing wizard backend as a foundation for the remaining plan/create flows
- Wizard now goes beyond catalog browsing: instance templates can be selected, planned, previewed, and created directly from the dashboard shell, with explicit notes about chat-assistant default-agent behavior and clean duplicate-id failure handling
- Wizard agent creation now follows that same in-shell pattern too: the page loads canonical instances and existing agent IDs, previews normalized agent profiles, and prevents duplicate agent creation before issuing the create call
- Wizard now also shows explicit disabled-state handling when `instance_control` is off, and same-session create flows can continue directly from a new instance into agent planning without requiring a page change or manual instance refresh

### 2.11 Dashboard eval metadata consumer adoption

Implemented in:

- `internal/channels/dashboard/ui/src/pages/eval/types.ts`
- `internal/channels/dashboard/ui/src/pages/eval/utils.ts`
- `internal/channels/dashboard/ui/src/pages/eval/EvalRunDetailPanel.tsx`
- `internal/channels/dashboard/ui/src/pages/EvalPage.tsx`
- `internal/channels/dashboard/ui/src/pages/eval/useEvalRuns.ts`
- `internal/channels/dashboard/ui/src/components/Layout.tsx`
- `internal/channels/dashboard/ui/src/hooks/useControlPlaneFeatures.ts`

Current capabilities:

- dashboard eval parsing now preserves additive `identity` and `metadata` blocks returned by the backend
- eval detail panels now render instance/agent/run lineage, task/session/source linkage, artifact/checkpoint paths, and a lightweight delegation summary for operators
- delegation/decomposition metadata is now visible in the dashboard without requiring raw JSON inspection
- dashboard nav now hides the Eval entry when the feature is disabled, and direct Eval page access renders a disabled-state panel instead of pretending the feature is available

### 2.12 Sessions lifecycle consumer adoption

Implemented in:

- `internal/channels/dashboard/ui/src/pages/SessionsPage.tsx`
- `internal/channels/dashboard/handler_test.go`
- `internal/channels/dashboard/ui/tests/e2e/sessions.spec.ts`

Current capabilities:

- dashboard session-message normalization now preserves lifecycle envelope metadata such as `message_id`, `status`, `instance_id`, sender/recipient IDs, task linkage, and related run linkage
- Sessions detail now renders operator-facing lifecycle cards for system/lifecycle events instead of flattening everything into generic system text
- dashboard session-message API coverage now verifies lifecycle metadata survives the backend/UI boundary
- Sessions lifecycle cards now open canonical inbox detail, rendering merged `message_id` lifecycle state and exposing inbox `ack` / `run` actions backed by the shared dashboard inbox endpoints
- session list/message backend routes now require the `instance_agents` feature, and the dashboard shell hides the Sessions nav entry plus renders a disabled-state panel on direct access when that feature is off

### 2.13 Instance-aware Agent Contract and Prompt Stack consumers

Implemented in:

- `internal/channels/dashboard/contract_api.go`
- `internal/channels/dashboard/instances_api.go`
- `internal/channels/dashboard/contract_api_test.go`
- `internal/channels/dashboard/ui/src/pages/AgentContractPage.tsx`
- `internal/channels/dashboard/ui/src/pages/PromptStackPage.tsx`
- `internal/channels/dashboard/ui/tests/e2e/contract.spec.ts`
- `internal/channels/dashboard/ui/tests/e2e/prompt-stack.spec.ts`

Current capabilities:

- Agent Contract resolved/diff/rollback flows now accept explicit `instance_id` and no longer silently depend on `LoadActiveInstanceID(...)`
- dashboard Agent Contract and Prompt Stack pages now load instance list, active instance, and instance-scoped agent routes before fetching stack/contract data
- legacy flat `/api/admin/agents/{agent_id}/...` contract routes remain as active-instance compatibility wrappers while instance-scoped routes are now the canonical path
- dashboard nav now hides Agent Contract and Prompt Stack when `instance_agents` is disabled, and direct page access renders disabled-state panels with controls suppressed instead of issuing optimistic instance/agent loads

### 2.14 Shared compatibility feature loading and eval CLI runtime gating

Implemented in:

- `internal/instances/features.go`
- `internal/instances/features_test.go`
- `internal/instances/store.go`
- `cmd/openclawssy/eval_cli.go`
- `cmd/openclawssy/main_test.go`

Current capabilities:

- control-plane compatibility feature flags are now loaded from `.openclawssy/controlplane/instances.json` through shared `internal/instances` helpers
- `instances.ResolveEffectiveRuntime(...)` now consumes that shared feature state instead of assuming `DefaultFeatureSet()`
- `openclawssy eval` now blocks operational subcommands (`run`, `list`, `results`, `baseline`, `compare`) when eval is disabled, while leaving help/usage reachable

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
- `go test ./internal/chatstore ./internal/tools ./internal/channels/chat ./internal/channels/http ./internal/channels/dashboard ./internal/runtime ./cmd/openclawssy`
- `go test ./internal/channels/dashboard -run 'TestEvalResultsEndpoint|TestControlPlaneFeaturesAndWizardEndpoints|TestInstanceFeatureFlagEnforcement' -count=1`
- `go test ./internal/agent -run TestExecuteDelegatedTasksRecordsMessageBackedLifecycleMetadata -count=1`
- `go test ./internal/runtime -run 'TestSubAgentAdapterPassesDefaultRestrictions|TestSubAgentAdapterReturnsMessageIDInOutput' -count=1`
- `go test ./internal/runtime -run '^TestRunTracker_' -count=1`
- `go test ./internal/tools -run '^TestRunCancelTool_' -count=1`
- `go test ./internal/channels/dashboard -run 'TestControlPlaneFeaturesAndWizardEndpoints|TestInstanceFeatureFlagEnforcement' -count=1`
- `go test ./internal/runtime ./internal/tools ./internal/channels/dashboard`
- `cd internal/channels/dashboard/ui && npm run typecheck`
- `cd internal/channels/dashboard/ui && npm run build`
- `cd internal/channels/dashboard/ui && CI=1 npx playwright test tests/e2e/runs.spec.ts`
- `cd internal/channels/dashboard/ui && CI=1 npx playwright test tests/e2e/chat.spec.ts tests/e2e/eval.spec.ts`
- `go test ./internal/channels/dashboard ./internal/instances ./cmd/openclawssy`
- `go test ./internal/channels/dashboard -run 'TestInstanceScopedPromptStackRoutesIsolateSameAgentID|TestInstanceScopedContractResolvedAndDiffEndpointsUseRequestedInstance|TestChatSessionMessagesEndpointIncludesLifecycleMetadata' -count=1`
- `go test ./internal/channels/dashboard -run 'TestInstanceInboxListAckAndRunFlow' -count=1`
- `go test ./internal/channels/dashboard ./internal/tools ./internal/runtime`
- `cd internal/channels/dashboard/ui && npm run build && CI=1 npx playwright test tests/e2e/contract.spec.ts tests/e2e/prompt-stack.spec.ts --reporter=line`
- `cd internal/channels/dashboard/ui && CI=1 npx playwright test tests/e2e/sessions.spec.ts --reporter=line`
- `cd internal/channels/dashboard/ui && npm run build`
- `go test ./internal/agent ./internal/runtime ./internal/tools`
- `go test ./internal/channels/dashboard`
- `cd internal/channels/dashboard/ui && CI=1 npx playwright test tests/e2e/monitor.spec.ts --reporter=line`
- `cd internal/channels/dashboard/ui && npm run typecheck && npm run build && CI=1 npx playwright test tests/e2e/contract.spec.ts tests/e2e/prompt-stack.spec.ts --reporter=line`
- `go test ./internal/channels/dashboard -run 'TestMonitor.*|Test.*Monitor.*' -count=1`
- `cd internal/channels/dashboard/ui && npm run typecheck && npm run build && CI=1 npx playwright test tests/e2e/monitor.spec.ts --reporter=line`
- `go test ./internal/channels/dashboard -run 'TestListChatSessionsEndpoint|TestListChatSessionsEndpointPagination|TestChatSessionMessagesEndpoint|TestChatSessionMessagesEndpointIncludesToolMetadata|TestChatSessionMessagesEndpointIncludesLifecycleMetadata|TestChatSessionMessagesEndpointPreservesMultiStepOrder|TestSessionsRoutesRequireInstanceAgentsFeature|TestListChatSessionsEndpointInvalidLimit' -count=1`
- `cd internal/channels/dashboard/ui && npm run typecheck && npm run build && CI=1 npx playwright test tests/e2e/sessions.spec.ts --reporter=line`
- `go test ./internal/channels/dashboard -run TestIntegrationDashboardPagesUseSharedShadcnUIComponents -count=1`
- `cd internal/channels/dashboard/ui && npm run typecheck && npm run build && CI=1 npx playwright test tests/e2e/chat.spec.ts --reporter=line`
- `go test ./internal/channels/dashboard -run 'TestAdminAgentsEndpointListAndSetActive|TestAdminAgentsEndpointUsesActiveInstanceConfigAndInstanceScopedPointers|TestAdminAgentsEndpointRequiresInstanceAgentsFeature|TestMonitorRoutesRequireInstanceAgentsFeature|TestSessionsRoutesRequireInstanceAgentsFeature|TestInstanceFeatureFlagEnforcement' -count=1`
- `cd internal/channels/dashboard/ui && npm run typecheck && npm run build && CI=1 npx playwright test tests/e2e/chat.spec.ts tests/e2e/monitor.spec.ts --reporter=line`

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
- dashboard/UI integration beyond the newly landed Sessions, Agent Contract, and Prompt Stack instance-aware consumers
- deepen first-class messaging / inbox lifecycle wiring so more producers/consumers emit `running` / `completed` / `failed` updates through the same message model
- finish composite run identity adoption across remaining runtime + HTTP + eval/dashboard consumers beyond the newly landed SSE/event-bus, Runs page, and Delegation page slices
- feature flag enforcement from one canonical source across UI/API/runtime beyond the newly landed dashboard API route guards, eval dashboard gating, and eval CLI runtime gating
- connect richer eval/delegation metadata to more real producers/CLI flows and more dashboard surfaces beyond the current additive storage contract + eval detail/Runs detail panels
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

Status update:

- instance-scoped prompt-stack and Agent Contract routes are now wired through the dashboard UI, so the next prompt-unifier slice should focus on mirror-editor clarity and stronger preview/runtime parity validation rather than basic route adoption

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

Status update:

- Sessions, Agent Contract, and Prompt Stack pages now consume canonical instance identity more explicitly; remaining UI work should prioritize broader active-instance visibility, canonical wizard flows, and consistent feature-state presentation across the rest of the dashboard
- shared feature-state presentation now also covers disabled nav/direct access behavior for Agent Contract and Prompt Stack; remaining UI work should push the same determinism into other instance-agent-dependent pages
- shared feature-state presentation now also covers Monitor, Contract, and Prompt Stack; remaining UI work should push the same determinism into more instance-agent-dependent pages and read-only surfaces
- shared feature-state presentation now also covers Sessions, Monitor, Contract, and Prompt Stack; remaining UI work should push the same determinism into Chat and other instance-agent-dependent pages
- shared feature-state presentation now also covers Chat, Sessions, Monitor, Contract, and Prompt Stack; remaining UI work should push the same determinism into additional instance-agent-dependent pages and remaining backend guardrails where useful
- shared feature-state presentation now also covers Chat, Sessions, Monitor, Contract, and Prompt Stack, and the shared `/api/admin/agents` selector route is now instance-aware; remaining work should keep pushing that same convergence into additional operator surfaces

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

Status update:

- eval CLI runtime gating is now covered, so the next qa/eval work should focus on remaining producer adoption, broader dashboard consumers, and wizard parity/feature-guard regression coverage

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

1. extend messaging/concurrency model from compatibility layer to first-class inbox lifecycle across more producers and consumers
2. deepen eval/delegation metadata normalization and parent linkage adoption
3. finish composite identity adoption in remaining dashboard/eval/runtime consumers beyond the latest Sessions/Contract/Prompt Stack slices
4. wire the remaining dashboard UI surfaces to canonical instance-aware APIs
5. finish feature-flag enforcement in API/runtime/UI from the same canonical source
6. run broader validation

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
