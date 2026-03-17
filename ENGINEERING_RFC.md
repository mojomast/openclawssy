# RFC: Instance-Centric Operator-Grade Runtime and Control Plane

Status: Proposed  
Author: OpenAI / Beach working draft  
Date: 2026-03-16

## 1. Summary

This RFC turns the current branch into an operator-grade system with:

- clean control-plane feature enable/disable semantics
- a single source of truth for prompt behavior
- first-class packaged runtime environments called **instances**
- first-class **agents** inside instances
- multi-agent parallel execution and explicit inter-agent communication
- guided wizards for creating instances and agents
- one consistent model across delegation, roles, eval, dashboard, and runtime

## 1.1 Implementation progress snapshot

Substantial portions of Milestones 1-5 are now landed on the current branch, including:

- canonical instance/agent storage and effective runtime resolution
- prompt stack as runtime truth with legacy docs treated as compatibility mirrors
- instance-aware HTTP run creation, SSE emission, and composite tracker/cancel identity
- shared inter-agent message lifecycle metadata (`message_id`, status transitions, additive lineage)
- delegated subagent propagation of stable delegated `message_id` plus delegation-event linkage via `related_run_id`
- dashboard eval, Runs, and Delegation consumers adopting additive instance/agent/run metadata
- composite runtime/tool cancellation for `(instance_id, agent_id, run_id)`
- first dashboard API feature-flag enforcement for wizard and instance-control surfaces
- dashboard inbox ack/run APIs reusing the shared `message_id` lifecycle model
- dashboard chat threading now carries `instance_id` and `agent_id` through send, poll, stream, and cancel paths
- eval feature flags now gate both dashboard API responses and dashboard UI visibility/disabled states
- dashboard Sessions now consumes lifecycle-rich `system` events and renders operator-facing message lifecycle cards
- Agent Contract and Prompt Stack dashboard flows now resolve explicitly against selected `instance_id` instead of silently depending on the active instance
- eval feature gating now has a runtime-compatible loader and blocks `openclawssy eval` operational CLI commands when eval is disabled
- dashboard inbox list/detail APIs now project merged per-`message_id` lifecycle state so sparse ack/run status rows do not discard the original message envelope
- Sessions lifecycle cards now let operators open the canonical inbox detail view and trigger inbox `ack` / `run` actions against the shared `message_id` lifecycle endpoints
- delegation events now carry additive `parent_run_id`, `from_agent_id`, and `to_agent_id` metadata so delegated subagent work correlates more directly with the canonical inbox/run identity model
- Agent Monitor launch/cancel flows now thread explicit `instance_id` / `agent_id` identity so monitor operations no longer rely on bare `run_id` or implicit active context alone
- Agent Contract and Prompt Stack dashboard navigation now hide when `instance_agents` is disabled, and direct page access renders explicit disabled-state panels instead of loading instance-scoped controls opportunistically

Remaining work is concentrated in wizard parity, broader composite-identity rollout, and finishing first-class messaging/eval consumer convergence beyond the newly landed dashboard/CLI slices.

This design is grounded in the current code shape:

- dashboard is already acting as the control-plane surface
- runtime execution still flows through `Engine.ExecuteWithInput` and `Runner.Run`
- contract resolution already composes prompt stack and role/delegation state on top of base config
- prompt stack is seeded from legacy docs such as `SOUL.md`, `RULES.md`, `TOOLS.md`, `SPECPLAN.md`, `DEVPLAN.md`, `HEARTBEAT.md`, and `HANDOFF.md`
- runtime still directly loads those docs today, creating prompt split-brain risk
- role templates already exist as config-backed routing/runtime constraints

## 2. Problem Statement

The current system has evolved real control-plane capabilities, but the runtime and storage model still reflect an older single-agent, doc-driven architecture.

### Current issues

1. **Prompt split-brain**
   - prompt stack/control-plane exists
   - legacy docs are still directly read by runtime
   - dashboard and runtime can disagree on the real effective prompt

2. **Weak packaging boundary**
   - behavior is spread across global config, agent docs, prompt stack state, routing config, role templates, and skill state
   - there is no single movable/exportable “bot environment”

3. **Agent model is not first-class enough**
   - agents exist conceptually, but are not packaged under a single environment unit with strong runtime boundaries

4. **Parallelism and communication lack a coherent system model**
   - some messaging primitives exist, but not as a formally permissioned instance-scoped model

5. **Feature gating is not strongly unified**
   - UI/API/runtime can drift unless feature flags are enforced at all three layers

## 3. Goals

### Primary goals

- make **Prompt Stack** the single runtime source of truth for prompt behavior
- make **Instance** the packaging unit
- make **Agent** a first-class runtime object under an instance
- support switching, cloning, parallel runs, and messaging for agents/instances
- unify dashboard, eval, delegation, roles, and runtime around one effective state model
- provide guided wizards for creation of instances and agents
- preserve compatibility with current doc-based setups during migration

### Non-goals

- moving provider secrets into instances
- moving machine/server/TLS binding into instances
- changing the global sandbox daemon ownership model
- replacing current runtime flow wholesale
- introducing a distributed scheduler in this phase

## 4. Core Model

## 4.1 Instance

An **Instance** is a packaged runtime environment.

It owns:

- workspace root
- default agent
- enabled agents for the instance
- prompt behavior mode
- prompt stack state and optional materialized prompt docs
- delegation policy defaults
- role-template set
- activated skills
- channel routing defaults
- inter-agent messaging policy
- parallelism limits

It does not own:

- provider secrets
- machine/server bind/TLS
- global sandbox daemon connection
- master encryption key

### Properties

- exportable
- clonable
- activatable
- runnable in parallel with other instances
- workspace-scoped
- control-plane addressable

## 4.2 Agent

An **Agent** is a named runtime persona/executor belonging to an instance.

It owns:

- identity
- model override policy
- self-improvement policy
- prompt stack/docs
- per-agent restrictions
- communication permissions
- optional role affinity/default role
- optional workspace overlay inside the instance

### Properties

- independently invokable
- independently configurable
- independently restricted
- able to communicate with other agents subject to policy
- can participate in delegation

## 4.3 Control-plane feature flag

A feature flag controls whether a module is:

- enabled
- read-only
- visible in UI

This must be enforced consistently across UI, API, and runtime.

## 5. Source of Truth Decision

## Decision

**Prompt Stack becomes the source of truth for runtime prompt behavior.**

### Why

- it already has versioning, preview, diff, rollback, lint, and test surfaces
- Agent Contract already resolves assembled prompt output as the effective system prompt
- continuing to use legacy docs as runtime truth preserves confusion
- it allows one consistent path across dashboard, eval, and runtime

### New rule

Runtime prompt assembly must read from **Prompt Stack first**.

Legacy prompt docs become:

- compatibility mirrors
- optional exported materializations
- operator-visible docs
- migration seed sources

They are no longer the hidden execution truth.

## Compatibility model

Dashboard doc editor must either:

1. edit prompt-stack-backed views, or
2. clearly indicate it is editing materialized compatibility docs, not the runtime truth

## Migration rule

On first load of an old agent:

- if prompt stack is empty:
  - seed it exactly from current legacy docs in current order
  - mark source as `migrated_from_docs`
  - persist stack
- runtime then reads prompt stack from then on

## 6. Storage Model

## 6.1 Global config additions

```json
{
  "control_plane": {
    "features": {
      "agent_contract": { "enabled": true, "read_only": false, "visible": true },
      "prompt_stack": { "enabled": true, "read_only": false, "visible": true },
      "role_templates": { "enabled": true, "read_only": false, "visible": true },
      "delegation": { "enabled": true, "read_only": false, "visible": true },
      "eval": { "enabled": true, "read_only": false, "visible": true },
      "instances": { "enabled": true, "read_only": false, "visible": true },
      "agent_messaging": { "enabled": true, "read_only": false, "visible": true }
    }
  },
  "instances": {
    "active_instance_id": "default",
    "allow_parallel_instances": true,
    "default_max_concurrent_instance_runs": 8
  },
  "engine": {
    "max_concurrent_runs": 32
  }
}
6.2 Instance manifest

Path:

.openclawssy/instances/<instance_id>/manifest.json

{
  "instance_id": "research-lab",
  "display_name": "Research Lab",
  "description": "Multi-agent research and implementation environment",
  "enabled": true,
  "workspace": {
    "root": "workspace/instances/research-lab",
    "shared": true
  },
  "runtime": {
    "default_agent_id": "orchestrator",
    "enabled_agent_ids": ["orchestrator", "coder", "analyst"],
    "max_concurrent_runs": 8
  },
  "prompting": {
    "source_mode": "prompt_stack",
    "materialize_docs": true
  },
  "delegation": {
    "enabled": true,
    "mode": "approve_plan",
    "threshold": 2,
    "cooldown_iterations": 15,
    "max_depth": 3
  },
  "messaging": {
    "enabled": true,
    "allow_inter_agent_messaging": true,
    "shared_inbox_namespace": "instance",
    "allow_cross_instance": false
  },
  "skills": {
    "activated": ["repo-research", "test-authoring"]
  },
  "roles": {
    "template_names": ["researcher", "implementer", "reviewer"]
  },
  "channels": {
    "dashboard": { "default_agent_id": "orchestrator" },
    "discord": { "default_agent_id": "orchestrator" },
    "telegram": { "default_agent_id": "orchestrator" },
    "scheduler": { "default_agent_id": "orchestrator" }
  },
  "created_at": "2026-03-16T00:00:00Z",
  "updated_at": "2026-03-16T00:00:00Z"
}
6.3 Agent manifest

Path:

.openclawssy/instances/<instance_id>/agents/<agent_id>/agent.json

{
  "agent_id": "coder",
  "display_name": "Coder",
  "enabled": true,
  "identity": {
    "assistant_name": "Claw Coder",
    "user_name": "Beach"
  },
  "model": {
    "provider": "zai",
    "name": "glm-4.7",
    "max_tokens": 8000,
    "timeout_ms": 120000
  },
  "behavior": {
    "self_improvement": false,
    "default_thinking_mode": "never"
  },
  "delegation": {
    "can_delegate": true,
    "can_receive_delegation": true,
    "default_role": "implementer"
  },
  "restrictions": {
    "allowed_tools": ["fs.read", "fs.write", "fs.edit", "code.search", "shell.exec"],
    "max_tool_iterations": 40,
    "timeout_ms": 120000
  },
  "communication": {
    "can_message": ["orchestrator", "reviewer"],
    "can_receive_from": ["orchestrator"],
    "allow_cross_instance": false
  },
  "workspace": {
    "overlay_root": "agents/coder"
  }
}
6.4 Proposed file tree
.openclawssy/
  config.json
  instances/
    default/
      manifest.json
      promptstack/
      roles.json
      skills.json
      channels.json
      agents/
        orchestrator/
          agent.json
          promptstack/
          docs/
          memory/
          audit/
          runs/
        coder/
          agent.json
          promptstack/
          docs/
          memory/
          audit/
          runs/
Why this layout matters

Current behavior is spread across:

config

agent docs

prompt stack store

chatstore pointers

skill activation inside docs

profile overrides

This layout makes the packaging unit explicit and movable.

7. Effective Runtime Resolution

Introduce:

type EffectiveRuntime struct {
    InstanceID            string
    AgentID               string
    WorkspaceRoot         string
    AgentWorkspaceRoot    string
    Model                 ModelConfig
    AllowedTools          []string
    PromptSourceMode      string
    PromptStackState      PromptStackState
    MaterializedDocs      []MaterializedDocRef
    Delegation            DelegationPolicy
    Messaging             MessagingPolicy
    RoleTemplates         []RoleTemplate
    Skills                []string
    ChannelDefaults       map[string]ChannelRoute
    Concurrency           ConcurrencyPolicy
    FeatureFlags          FeatureSet
}

Resolver:

ResolveEffectiveRuntime(instanceID, agentID string) (*EffectiveRuntime, error)

It merges:

global machine config

active instance manifest

agent manifest

prompt stack state

role templates

per-agent restrictions

applicable feature flags

This becomes the single source feeding:

allowed tools

selected model

prompt source

delegation mode

channel defaults

messaging permissions

concurrency controls

8. Runtime Changes
8.1 Engine input

Add instance context:

type ExecuteInput struct {
    InstanceID string
    AgentID    string
    // existing fields...
}

Rules:

if InstanceID empty:

resolve active instance

fallback to default

if AgentID empty:

use instance default agent

8.2 Runtime flow

Current high-level flow remains:

Engine.ExecuteWithInput

Runner.Run

But before execution:

resolve effective runtime

resolve workspace from instance/agent

resolve prompt from prompt stack

enforce feature flags

enforce tool/model/messaging limits

8.3 Workspace resolution

New rules:

instance owns workspace root

agents may get per-agent overlays

same-instance delegation shares instance workspace unless isolated

parallel instances use separate roots by default

8.4 Parallel execution

Support:

many agents in same instance

many instances at once

Add:

global engine.max_concurrent_runs

instance-level runtime.max_concurrent_runs

optional agent-level cap

Run tracker key:

(instance_id, agent_id, run_id)
9. Agent Communication Design

The repo already surfaces messaging concepts; formalize them as a permissioned, auditable model.

9.1 Principles

Communication must be:

explicit

permissioned

same-instance by default

auditable

9.2 Message model
{
  "message_id": "msg_123",
  "instance_id": "research-lab",
  "from_agent_id": "orchestrator",
  "to_agent_id": "coder",
  "subject": "implement patch",
  "task_id": "task-2",
  "run_id": "run_abc",
  "payload": {
    "message": "Implement the patch in x.go",
    "artifacts": {
      "analysis_summary": "..."
    }
  },
  "status": "queued",
  "created_at": "2026-03-16T00:00:00Z"
}
9.3 Communication modes

Support:

direct message

inbox poll

fire-and-forget delegation note

reply-to-task-thread

9.4 Constraints

Per agent:

who it can message

who can message it

whether it can auto-execute inbox work

whether cross-instance messaging is allowed

Defaults:

same-instance only

orchestrator can message all same-instance agents

workers cannot message outside allowlist

10. Wizard Design
10.1 Wizard types

A. New Instance Wizard
B. New Agent Wizard

10.2 New Instance Wizard
Step 1: Basics

Inputs:

instance name

instance id

description

workspace root

shared or partitioned workspace

Step 2: Operating mode

Templates:

solo assistant

coding team

research team

support bot

scheduler/ops bot

blank custom

Each template prefills:

default agents

role set

delegation defaults

channel defaults

Step 3: Agent topology

Options:

single agent

orchestrator + workers

custom set

If orchestrator + workers:

ask worker count/roles

precreate starter agents like orchestrator, coder, researcher, reviewer

Step 4: Prompt behavior

Choose:

import from docs

start fresh prompt stack

clone from existing instance/agent

Then edit:

identity

operating rules

tool safety rules

delegation instructions

session overlay defaults

Step 5: Model behavior

Choose:

global model inheritance

per-agent overrides

thinking mode defaults

timeout defaults

token defaults

Step 6: Delegation

Modes:

disabled

prompt only

suggest only

approve plan

auto trusted

full autonomous

Then configure:

threshold

cooldown

max depth

default worker role routing

Step 7: Communication

Choose:

no inter-agent messaging

orchestrator-only routing

full team messaging

custom allowlist

Step 8: Skills and tools

Choose:

activated skills

role templates

default allowed tools

shell/network availability

Step 9: Channels

Default agent per channel:

dashboard

discord

telegram

scheduler

Step 10: Review + create

Show:

instance package summary

agent list

role list

workspace layout

active feature flags

activate now toggle

10.3 New Agent Wizard
Step 1

Pick instance

Step 2

Agent id / display name / purpose

Step 3

Archetype:

orchestrator

implementer

researcher

reviewer

memory keeper

support

blank

Step 4

Prompt seed:

clone another agent

start from archetype

import legacy docs

Step 5

Role + tools:

default role

tool allowlist

max iterations

timeout

Step 6

Messaging permissions:

who it can talk to

who can delegate to it

Step 7

Review + create

11. API Proposal
11.1 Instances

GET /api/admin/instances

POST /api/admin/instances

GET /api/admin/instances/{instance_id}

PUT /api/admin/instances/{instance_id}

DELETE /api/admin/instances/{instance_id}

POST /api/admin/instances/{instance_id}/activate

GET /api/admin/instances/active

POST /api/admin/instances/{instance_id}/clone

POST /api/admin/instances/bootstrap-from-current

11.2 Agents inside instances

GET /api/admin/instances/{instance_id}/agents

POST /api/admin/instances/{instance_id}/agents

GET /api/admin/instances/{instance_id}/agents/{agent_id}

PUT /api/admin/instances/{instance_id}/agents/{agent_id}

DELETE /api/admin/instances/{instance_id}/agents/{agent_id}

11.3 Wizard endpoints

GET /api/admin/wizard/templates

POST /api/admin/wizard/instances/plan

POST /api/admin/wizard/instances/create

POST /api/admin/wizard/agents/plan

POST /api/admin/wizard/agents/create

Plan responses should include:

previewed manifests

derived agents

derived prompt layers

validation warnings

11.4 Feature flags

GET /api/admin/control-plane/features

PATCH /api/admin/control-plane/features

11.5 Prompt stack under instance/agent scope

GET /api/admin/instances/{instance_id}/agents/{agent_id}/prompt-stack

PUT /api/admin/instances/{instance_id}/agents/{agent_id}/prompt-stack/{layer}

GET /api/admin/instances/{instance_id}/agents/{agent_id}/prompt-stack/preview

GET /api/admin/instances/{instance_id}/agents/{agent_id}/prompt-stack/history

POST /api/admin/instances/{instance_id}/agents/{agent_id}/prompt-stack/diff

POST /api/admin/instances/{instance_id}/agents/{agent_id}/prompt-stack/rollback

POST /api/admin/instances/{instance_id}/agents/{agent_id}/prompt-stack/lint

POST /api/admin/instances/{instance_id}/agents/{agent_id}/prompt-stack/test

11.6 Messaging

POST /api/admin/instances/{instance_id}/messages/send

GET /api/admin/instances/{instance_id}/agents/{agent_id}/inbox

POST /api/admin/instances/{instance_id}/agents/{agent_id}/inbox/{message_id}/ack

POST /api/admin/instances/{instance_id}/agents/{agent_id}/inbox/{message_id}/run

12. Feature Flag Enforcement

This must be hard-enforced in three layers.

12.1 UI

hide nav/pages when not visible

disable inputs when read-only

show reason/state clearly

12.2 API

Every feature-controlled route must pass a feature guard before entering the core handler.

12.3 Runtime

Execution behavior must check feature state before using controlled capability.

Examples

prompt_stack.enabled=false

prompt stack APIs fail

runtime must not source from prompt stack unless explicitly configured fallback is allowed

delegation.enabled=false

planner-led delegation modes rejected

orchestration paths restricted accordingly

role_templates.enabled=false

role CRUD disabled

router falls back to built-ins or neutral mode

eval.enabled=false

eval APIs/pages disabled

Error shape
{
  "error": {
    "code": "feature.disabled",
    "message": "Prompt Stack is disabled by control-plane configuration.",
    "details": {
      "feature": "prompt_stack",
      "read_only": false
    }
  }
}
13. Migration Plan
Phase 1: Bootstrap default instance from current world

Create default instance from:

active config

current workspace root

default agent

current agent docs

current prompt stack state if any

current role templates

current skill activation state

current channel defaults

Phase 2: Make engine instance-aware

add InstanceID to execute input

default to active/default instance

no breaking behavior change yet

Phase 3: Make prompt stack runtime-authoritative

seed from docs where stack empty

runtime uses stack

docs become mirrors

Phase 4: Move dashboard APIs to instance-aware paths

support new paths

keep temporary compatibility aliases if needed

Phase 5: Deprecate global agent behavior storage

keep legacy read compatibility

stop writing new state to deprecated locations

14. Implementation Order
Milestone 1: Instance foundation

storage model

manifest structs

active instance pointer

bootstrap migration

instance CRUD API

Milestone 2: Effective runtime resolver

resolve effective state

wire engine to instance-aware workspace/model/tools/delegation

Milestone 3: Prompt-source unification

runtime reads prompt stack

docs become mirrored/exported layer

Milestone 4: Wizard

template catalog

preview/create flows

dashboard UI

Milestone 5: Communication + parallel ops

instance-scoped messaging

inbox/thread model

concurrency/run tracker policies

Milestone 6: Feature flags

backend guards

UI gating

runtime gating

Milestone 7: Eval/schema cleanup

normalize delegation ledger records

make eval consume stable instance/agent/run metadata

15. Required Tests

bootstrapping default instance from current config works

active instance switching changes workspace/default agent/runtime behavior

two instances can run in parallel without workspace bleed

two agents in same instance can run in parallel

agent messaging respects permissions

prompt stack preview equals runtime prompt source

disabling prompt stack blocks APIs and changes runtime behavior deterministically

disabling delegation blocks planner-led delegation

role templates update routing behavior

wizard create preview matches created manifests

legacy agents/docs migrate without losing behavior

16. Design Decisions Locked In

These decisions are intentionally firm:

Prompt stack is runtime truth.

Instance is the packaging unit.

Agents belong to instances.

Workspace is instance-scoped first, agent-scoped second.

Inter-agent communication is same-instance by default.

Feature flags are enforced at UI, API, and runtime.

Legacy docs become mirrors, not hidden truth.

17. Tiny mental model

global config = machine

instance manifest = packaged bot environment

agent manifest = worker/persona inside the environment

prompt stack = behavior source

role templates = routing constraints

delegation = execution policy

eval = observer
