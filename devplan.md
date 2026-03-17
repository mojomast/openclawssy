
---

## `DEVPLAN.md`

```md
# Dev Plan: Operator-Grade Instance/Agent Architecture

Owner: Platform / Runtime / Dashboard  
Date: 2026-03-16  
Status: In progress

## 1. Intent

Implement the instance-centric architecture in a way that preserves the current runtime path while eliminating prompt split-brain and making agents/instances first-class packaged environments.

This plan is written for parallel execution by multiple agents.

## 2. Success Criteria

We are done when:

- runtime resolves effective state via instance + agent
- prompt stack is runtime-authoritative
- legacy docs are compatibility mirrors only
- instances can be created, activated, cloned, and run in parallel
- agents can be created inside instances and run in parallel
- inter-agent communication is permissioned, auditable, and same-instance by default
- wizard flows preview and create valid manifests
- feature flags gate UI, API, and runtime consistently
- eval, delegation, roles, and dashboard all reference the same stable instance/agent/run metadata model

## 3. Workstreams

Run these as parallel workstreams with one orchestrator and several workers.

### Workstream A: Storage + schema foundation
Owner archetype: backend platform

Deliverables:
- instance manifest structs
- agent manifest structs
- control-plane feature structs
- storage paths under `.openclawssy/instances/...`
- bootstrap migration for `default` instance
- active instance pointer support

### Workstream B: Effective runtime resolver
Owner archetype: runtime/backend

Deliverables:
- `ResolveEffectiveRuntime(instanceID, agentID)`
- engine input expansion with `InstanceID`
- runtime workspace resolution using instance/agent
- model/tool/delegation resolution from effective runtime
- stable run tracker keys `(instance_id, agent_id, run_id)`

### Workstream C: Prompt-source unification
Owner archetype: prompt/runtime

Deliverables:
- prompt stack becomes runtime source
- migration seeding from legacy docs when stack empty
- materialized docs compatibility/export layer
- preview parity tests between prompt stack output and runtime prompt

### Workstream D: Instances + agents API
Owner archetype: backend/api

Deliverables:
- instance CRUD endpoints
- agent CRUD endpoints under instance scope
- activate/clone/bootstrap endpoints
- validation layer for manifests

### Workstream E: Messaging + parallel ops
Owner archetype: runtime/messaging

Deliverables:
- instance-scoped message model
- inbox store
- send/ack/run APIs
- agent comm permission checks
- concurrency guardrails

### Workstream F: Wizard backend + dashboard UI
Owner archetype: full-stack/dashboard

Deliverables:
- template catalog
- wizard plan/create APIs
- wizard review screens
- feature-gated navigation and forms
- instance/agent management screens

### Workstream G: Feature flag enforcement
Owner archetype: backend + UI

Deliverables:
- route guards
- runtime guards
- nav/page gating
- read-only enforcement
- error shape standardization

### Workstream H: Eval + metadata normalization
Owner archetype: eval/observability

Deliverables:
- stable metadata model for instance/agent/run
- delegation ledger cleanup
- eval ingestion/path updates
- dashboard summaries aligned to new model

## 4. Recommended Parallel Agent Topology

Use these agents:

1. **orchestrator**
   - owns global sequencing
   - assigns tasks
   - reconciles schema and interfaces
   - blocks conflicting changes

2. **storage-architect**
   - owns manifests, storage layout, migration bootstrap

3. **runtime-integrator**
   - owns engine wiring, effective runtime resolution, workspace changes

4. **prompt-unifier**
   - owns runtime prompt-source migration and doc materialization

5. **api-builder**
   - owns instance/agent/wizard/feature endpoints

6. **dashboard-builder**
   - owns UI flows and feature gating

7. **messaging-concurrency**
   - owns inter-agent comms, inbox, run tracker, parallel caps

8. **qa-eval**
   - owns integration tests, migration tests, eval metadata alignment

## 5. Dependency graph

### Minimal sequencing

1. Storage/schema foundation starts first
2. Effective runtime resolver can begin once manifest interfaces are stable
3. Prompt-source unification depends on resolver scaffolding
4. API work can begin after storage contracts are defined
5. Messaging can begin after instance/agent IDs and run metadata shapes are stable
6. Wizard backend depends on API/storage contracts
7. Dashboard depends on wizard/API contracts
8. QA/eval runs alongside all streams but finalizes after interfaces stabilize

### Hard dependencies

- prompt-source runtime switch must not merge before migration seeding exists
- instance activation must not merge before effective runtime resolution is in place
- wizard create must use the same validation/manifest builder as direct APIs
- eval metadata normalization must use the same instance/agent/run IDs as runtime

## 6. Milestones

## Current progress snapshot

Completed or substantially landed:

- Milestone 1: foundation
- Milestone 2: effective runtime
- Milestone 3: prompt unification core
- Milestone 4: canonical dashboard/backend instance and agent APIs (backend layer; UI still incomplete)
- Milestone 5 partial:
  - HTTP run creation persists `instance_id`
  - HTTP SSE events emit `instance_id` and `agent_id`
  - HTTP tracker/cancel path stores both bare and composite run keys
  - delegated subagent runs inherit parent `instance_id`
  - inter-agent messaging is explicitly same-instance and persists `instance_id`
  - dashboard monitor/status/trace/decision surfaces now emit `instance_id`
  - dashboard monitor reconciliation no longer collides on duplicate `run_id` across instances
  - `agent.message.send` / `agent.message.inbox` now mint stable `message_id` values and surface lifecycle metadata
- inbox authorization and per-agent communication allowlists are now enforced when configured
- `agent.message.send` can now optionally auto-run the target agent and emit real `running` / `completed` / `failed` lifecycle transitions
- proactive memory hooks now use the same message lifecycle path, preserving `parent_run_id` linkage into the spawned run
- HTTP SSE/event-bus delivery now supports composite subscription by `instance_id` + `agent_id` while dual-publishing to legacy bare `run_id` subscribers
- dashboard eval detail views now surface additive identity/runtime/delegation metadata instead of dropping it at the UI layer
- delegated subagent execution now carries stable delegated `message_id` values and records `message_id` / `related_run_id` in delegation events for shared lifecycle correlation
- dashboard runs/delegation consumers now thread `instance_id` + `agent_id` into run detail, trace, and decision fetches instead of relying on bare `run_id`
- dashboard chat send/poll/SSE/cancel flows now thread `instance_id` + `agent_id`, with active-instance bootstrap in the UI
- dashboard inbox APIs now expose list/detail/ack/run routes backed by the shared message lifecycle and subagent runner path
- eval dashboard surfaces now hide the nav entry when the feature is disabled and render a disabled state on direct page access
- dashboard run detail now renders a structured identity/delegation summary for operators (instance, agent, parent run, session, artifact path, delegation plan)
- runtime `RunTracker` now has first-class composite track/cancel/remove helpers for `(instance_id, agent_id, run_id)` while preserving bare `run_id` compatibility
- `run.cancel` now accepts composite identity and prefers precise composite cancellation when `instance_id` and `agent_id` are supplied together
- dashboard instance, instance-agent, and wizard admin APIs now hard-fail with feature-specific 403s when their control-plane flags are disabled
- dashboard Sessions now preserves lifecycle-rich inbox metadata and renders operator-facing lifecycle cards for message status transitions
- Agent Contract and Prompt Stack dashboard flows now resolve against explicit selected `instance_id` routes, while legacy flat agent routes remain active-instance compatibility wrappers
- control-plane compatibility feature loading now lives in `internal/instances`, and `openclawssy eval` operational CLI commands are runtime-gated when eval is disabled
- dashboard inbox list/detail handlers now merge lifecycle rows by `message_id`, preserving original message/task/source fields while surfacing the latest status and related run linkage
- Sessions lifecycle cards now open canonical inbox detail and expose inbox `ack` / `run` actions, making the shared `message_id` lifecycle model directly actionable from the session transcript
- delegation events now preserve additive `parent_run_id`, `from_agent_id`, and `to_agent_id` metadata, reducing another gap between delegated execution and the canonical inbox/run identity model
- Agent Monitor UI now threads explicit `instance_id` / `agent_id` on launch and cancel operations, reducing another remaining bare-`run_id` operator path

Still open:

- first-class inbox/message lifecycle model
- dashboard/UI instance wiring completion beyond the newly landed Sessions, Agent Contract, and Prompt Stack pages
- deeper eval/delegation metadata normalization beyond the current additive schema and operator detail views
- end-to-end feature flag enforcement polish beyond the newly landed dashboard API/UI guards and eval CLI runtime gating
- broader validation and migration cleanup

## Milestone 1: Foundation

### Tasks
- define config additions
- add Go structs for feature flags, instance manifest, agent manifest
- add storage readers/writers
- implement bootstrap-from-current migration
- persist `default` instance

### Exit criteria
- repo can load/create instance and agent manifests
- active instance can be resolved
- bootstrap command/path works on current world

## Milestone 2: Effective runtime

### Tasks
- add `InstanceID` to execute input
- implement `ResolveEffectiveRuntime`
- route workspace/model/tools/delegation through resolver
- add concurrency metadata to run tracker

### Exit criteria
- runtime behavior varies correctly by instance/agent
- active instance switching changes resolved state without workspace bleed

## Milestone 3: Prompt unification

### Tasks
- runtime prompt assembly reads prompt stack
- seed prompt stack from docs if empty
- tag migrated agents with `migrated_from_docs`
- add materialized doc export/update path
- make dashboard doc editor clearly stack-backed or mirror-backed

### Exit criteria
- prompt preview matches runtime system prompt
- legacy agent migration preserves behavior

## Milestone 4: CRUD APIs

### Tasks
- add instance endpoints
- add agent endpoints
- add activate/clone/bootstrap endpoints
- add validation errors and feature guard hooks

### Exit criteria
- instance and agent lifecycle manageable via API
- new resources persist correctly under instance tree

## Milestone 5: Messaging + parallel ops

### Tasks
- define message model
- add inbox storage
- implement send/list/ack/run APIs
- add permission checks
- enforce same-instance default
- add global/instance/agent concurrency caps

### Exit criteria
- same-instance agents can communicate
- parallel runs are isolated and auditable

Progress update:

- same-instance messaging compatibility layer is landed
- composite tracker identity is landed in runtime and HTTP queued runs
- dashboard run/decision surfaces now consume and emit instance-aware identity more consistently
- inbox lifecycle foundation is landed (`message_id`, `queued`, `acknowledged`) and dashboard inbox ack/run APIs now reuse the same lifecycle model, but broader runtime/UI wiring and canonical storage cleanup are still open
- inbox lifecycle is now exercised by real auto-run, proactive-memory, and delegated-subagent producer paths, but more producers/consumers still need to converge on the same model
- API-side feature-flag enforcement now covers wizard, instance-control, instance-agent, and eval routes, and the dashboard UI now hides/disables eval when that feature is off, but broader UI/runtime read-only and visibility enforcement remains open
- dashboard Sessions now consumes lifecycle metadata from session messages and surfaces the queue/running/completed/failed flow as dedicated lifecycle cards for operators
- dashboard Agent Contract and Prompt Stack flows now consume canonical instance-scoped routes instead of silently binding to the active instance, while preserving legacy flat-route compatibility wrappers
- dashboard inbox list/detail APIs now match the shared tool inbox semantics by projecting merged lifecycle state per `message_id` instead of sparse latest-row snapshots
- Sessions now consumes those canonical inbox detail/ack/run endpoints from lifecycle cards, closing another dashboard-side gap between session history and the shared inbox model
- delegated subagent execution now records additive parent/sender/recipient identity in `DelegationEvent`, so trace/ledger/eval consumers can correlate delegated work to the same instance/agent/run/message model more directly
- Agent Monitor now consumes active-instance identity for new launches and uses per-run composite identity for cancel actions, aligning another operator surface with the canonical `(instance_id, agent_id, run_id)` model
- dashboard nav and direct-route behavior for Agent Contract and Prompt Stack now honor the shared `instance_agents` feature state, hiding disabled entries and showing deterministic disabled-state panels instead of attempting instance-scoped loads
- Agent Monitor list/control routes and dashboard shell now honor the same shared `instance_agents` feature state, extending deterministic disabled-nav/direct-access behavior to another instance-agent-dependent operator surface

## Milestone 6: Wizard

### Tasks
- implement template catalog
- implement plan/create endpoints
- build New Instance Wizard UI
- build New Agent Wizard UI
- ensure preview == create output

### Exit criteria
- operators can create instances and agents from guided flows
- preview output matches persisted manifests

## Milestone 7: Feature flags + eval cleanup

### Tasks
- add full feature guards to UI/API/runtime
- normalize eval metadata
- align delegation ledger format
- update dashboard summaries and filters

### Exit criteria
- disabling a feature behaves deterministically everywhere
- eval and dashboard consume stable metadata

Progress update:

- eval storage and API now carry richer additive identity/runtime metadata (`instance_id`, `agent_id`, `run_id`, `parent_run_id`, `root_run_id`, `source`, `task_id`, `session_id`, artifact/checkpoint/delegation metadata)
- dashboard/eval consumers have stronger composite identity and lineage adoption, and chat now threads composite identity through its operator flows, but delegation producers and broader consumers still need follow-through
- dashboard eval detail panels now consume and render additive identity/runtime/delegation metadata, but richer producer coverage and more operator views still remain
- `internal/instances.ResolveEffectiveRuntime(...)` now loads control-plane compatibility feature state from the shared instance store instead of hardcoding default feature flags
- `openclawssy eval` now blocks `run`, `list`, `results`, `baseline`, and `compare` when eval is disabled, while preserving help/usage output for discoverability

## 7. Detailed task list by agent

## 7.1 orchestrator

### Responsibilities
- define cross-workstream interfaces
- maintain task graph
- review merge order
- resolve conflicts on schema names and endpoint semantics

### Concrete tasks
- publish canonical structs and endpoint contracts
- publish run metadata contract
- maintain migration invariants doc
- review all workstream PRs for coherence

## 7.2 storage-architect

### Concrete tasks
- add:
  - `ControlPlaneFeatureState`
  - `InstanceManifest`
  - `AgentManifest`
  - `MessagingPolicy`
  - `DelegationPolicy`
- implement store helpers:
  - `LoadInstanceManifest`
  - `SaveInstanceManifest`
  - `ListInstances`
  - `LoadAgentManifest`
  - `SaveAgentManifest`
  - `ListAgents`
- implement bootstrap migration:
  - `BootstrapDefaultInstanceFromCurrent()`

### Must preserve
- current system still boots without manual migration steps
- default instance synthesized from current config/docs/roles/channels

## 7.3 runtime-integrator

### Concrete tasks
- extend `ExecuteInput`
- implement active/default instance resolution
- integrate `ResolveEffectiveRuntime`
- route workspace root through instance
- support agent overlay path
- update run tracker key schema
- add concurrency checks:
  - global
  - instance
  - optional agent

### Risks
- hidden global workspace assumptions
- old code paths bypassing resolver

### Required guard
- no runtime path should compute tools/model/delegation independently after resolver exists

## 7.4 prompt-unifier

### Concrete tasks
- define canonical prompt stack assembly contract
- implement migration seeding from legacy docs
- add source marker `migrated_from_docs`
- add doc materialization/export
- add parity tests:
  - legacy docs → seeded prompt stack → runtime prompt equals previous behavior

### Risks
- subtle ordering mismatch in legacy prompt assembly
- dashboard editing semantics becoming ambiguous

### Required guard
- once migrated, runtime must not silently read legacy docs directly

## 7.5 api-builder

### Concrete tasks
- implement endpoints:
  - instances CRUD
  - agents CRUD
  - activate/clone/bootstrap
  - wizard plan/create
  - control-plane features get/patch
  - prompt-stack scoped instance/agent APIs
  - messaging APIs
- add feature guard middleware
- standardize validation and error response shapes

### Required guard
- wizard create must call the same internal manifest builder/validator as direct APIs

## 7.6 dashboard-builder

### Concrete tasks
- add Instances nav and management view
- add Agent management under selected instance
- add New Instance Wizard
- add New Agent Wizard
- add feature gating:
  - hidden
  - disabled
  - read-only
- clarify prompt editor mode:
  - stack-backed
  - materialized-doc mirror

### UX requirement
Operators must always be able to answer:
- which instance is active?
- which agent is selected?
- what is the real prompt source?
- what features are disabled/read-only?

## 7.7 messaging-concurrency

### Concrete tasks
- add instance-scoped inbox store
- add message lifecycle:
  - queued
  - acknowledged
  - running
  - completed
  - failed
- implement permission checks from agent manifests
- add thread/task linkage fields
- add run coordination for parallel execution

### Required guard
- default policy is same-instance only
- no hidden auto-execution unless explicitly allowed

## 7.8 qa-eval

### Concrete tasks
- add migration tests
- add runtime isolation tests
- add prompt parity tests
- add feature flag enforcement tests
- add wizard preview/create equality tests
- update eval metadata ingestion to `(instance_id, agent_id, run_id)`

## 8. Test Matrix

## 8.1 Migration

- bootstrap default instance from current config
- bootstrap preserves current default agent
- bootstrap preserves role templates and channel defaults
- bootstrap seeds prompt stack from legacy docs when empty
- migrated runtime behavior matches prior behavior

## 8.2 Runtime

- active instance switching changes workspace root
- active instance switching changes default agent
- per-agent model override works
- per-agent tool allowlist works
- instance workspace isolation prevents bleed

## 8.3 Parallelism

- two instances run in parallel without workspace bleed
- two agents in one instance run in parallel safely
- global concurrency cap enforced
- instance concurrency cap enforced
- agent cap enforced if configured

## 8.4 Messaging

- allowed message routes succeed
- forbidden message routes fail
- same-instance default enforced
- cross-instance denied by default
- inbox run path respects permissions

## 8.5 Prompt

- preview equals runtime prompt
- prompt stack disabled blocks API
- prompt stack disabled changes runtime deterministically
- materialized docs do not become hidden runtime truth

## 8.6 Features

- UI hides invisible features
- UI disables read-only features
- API blocks disabled features
- runtime blocks disabled features

## 8.7 Wizard

- plan output validates
- create output equals plan
- template derivations generate expected agents/roles/settings

## 9. Merge Strategy

### Rule 1
Do not land UI flows before storage and API contracts stabilize.

### Rule 2
Do not land runtime prompt switch before migration seeding and parity tests exist.

### Rule 3
Land feature guard middleware early so new endpoints don’t bypass control-plane rules.

### Rule 4
Keep compatibility reads until all callers are instance-aware.

## 10. Definition of Done

This effort is done when:

- instance is the packaged runtime boundary
- agent is a first-class child of instance
- prompt stack is runtime truth
- legacy docs are mirrors only
- runtime and dashboard resolve the same effective behavior
- instances/agents can be created through wizard and API
- agents can run in parallel and communicate safely
- feature flags are enforced at UI/API/runtime
- eval/delegation/dashboard all use stable instance/agent/run metadata
