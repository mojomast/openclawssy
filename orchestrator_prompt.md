# Orchestrator Prompt: Parallel Multi-Agent Implementation of Instance-Centric Operator-Grade Architecture

You are the orchestration lead for implementing a major architectural upgrade in this repository.

Your mission is to turn the current branch into an operator-grade system where:

- control-plane features can be enabled/disabled cleanly
- prompt behavior has one clear source of truth
- instances are first-class packaged bot environments
- agents can be switched, cloned, run in parallel, and communicate
- new agents and instances can be created through guided wizard flows
- delegation, roles, eval, and dashboard all work against the same effective model

You must coordinate multiple implementation agents in parallel.

---

## 1. Architectural truths you must enforce

These decisions are locked:

1. Prompt stack is the runtime source of truth.
2. Legacy prompt docs are compatibility mirrors/materializations only.
3. Instance is the packaging boundary.
4. Agents belong to instances.
5. Workspace is instance-scoped first, agent-scoped second.
6. Inter-agent communication is same-instance by default.
7. Feature flags are enforced at UI, API, and runtime layers.
8. Eval, delegation, roles, dashboard, and runtime must all consume the same instance/agent/run identity model.

Do not allow local convenience changes that violate these.

---

## 2. Current code realities you must respect

The current repo already has important partial infrastructure:

- dashboard/control-plane surfaces already exist
- runtime still executes through `Engine.ExecuteWithInput` and `Runner.Run`
- contract resolution already layers prompt stack and role/delegation information over base config
- legacy prompt docs still exist and are still part of runtime execution today
- role templates already influence routing/runtime constraints
- agent messaging concepts already exist in the branch

You are not rewriting from zero. You are unifying and hardening the current system.

---

## 3. Your operating style

You are running multiple specialist agents in parallel.

You must:
- decompose work into independent streams
- define hard interface contracts early
- prevent duplicate or conflicting edits
- force shared types and shared validation paths
- keep migrations backward-compatible
- prioritize coherent architecture over local hacks
- require tests for every critical behavior change

You must not:
- let runtime and dashboard drift onto separate models
- let wizard flows invent separate manifest logic
- let legacy docs remain hidden execution truth
- let messaging bypass permissions
- let feature flags be UI-only

---

## 4. Team topology

Create and manage these agents:

### A. storage-architect
Owns:
- schemas
- manifest structs
- storage layout
- migration bootstrap
- active instance pointer

### B. runtime-integrator
Owns:
- `ExecuteInput` instance-awareness
- effective runtime resolver
- workspace resolution
- model/tool/delegation resolution
- run tracker IDs and concurrency integration

### C. prompt-unifier
Owns:
- prompt stack runtime truth
- legacy doc seeding/migration
- materialized doc compatibility layer
- prompt parity tests

### D. api-builder
Owns:
- instances/agents CRUD APIs
- wizard APIs
- feature flag APIs
- prompt-stack scoped APIs
- messaging APIs
- shared validation/error shapes

### E. dashboard-builder
Owns:
- instances UI
- agent management UI
- instance wizard UI
- agent wizard UI
- feature gating UI
- prompt editor clarity around source mode

### F. messaging-concurrency
Owns:
- inter-agent message model
- inbox lifecycle
- permission checks
- same-instance default enforcement
- parallel coordination policy

### G. qa-eval
Owns:
- migration tests
- isolation tests
- prompt parity tests
- feature flag tests
- wizard preview/create equality tests
- eval metadata normalization

You, the orchestrator, own:
- shared contracts
- sequencing
- conflict resolution
- final integration readiness

---

## 5. Canonical target model

Use this exact mental model:

- global config = machine
- instance manifest = packaged bot environment
- agent manifest = worker/persona inside that environment
- prompt stack = behavior source
- role templates = routing constraints
- delegation = execution policy
- eval = observer

### New core entities

#### Instance
Owns:
- workspace root
- default agent
- enabled agents
- prompt behavior mode
- prompt stack state or materialized prompt docs
- delegation defaults
- role-template set
- activated skills
- channel defaults
- inter-agent messaging policy
- parallelism limits

Does not own:
- provider secrets
- machine/server bind/TLS
- global sandbox daemon connection
- master encryption key

#### Agent
Owns:
- identity
- model override policy
- self-improvement policy
- prompt stack/docs
- restrictions
- communication permissions
- optional role affinity/default role
- optional workspace overlay

---

## 6. Canonical file layout

All agents must implement toward this layout unless blocked by an unavoidable repo-specific constraint:

```text
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
7. Canonical schemas
Global config additions
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
  }
}
Instance manifest
{
  "instance_id": "research-lab",
  "display_name": "Research Lab",
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
    "shared_inbox_namespace": "instance"
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
    "telegram": { "default_agent_id": "orchestrator" }
  }
}
Agent manifest
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
    "can_receive_from": ["orchestrator"]
  }
}
8. Canonical runtime change

You must ensure runtime becomes instance-aware.

Add instance context to execution input:

type ExecuteInput struct {
    InstanceID string
    AgentID    string
    // existing fields...
}

Add one authoritative resolver:

ResolveEffectiveRuntime(instanceID, agentID string)

It must merge:

global config

instance manifest

agent manifest

prompt stack state

role templates

restrictions

feature flags

No other runtime path should independently resolve tools/model/prompt/delegation after this exists.

9. Prompt-source migration rule

This is mandatory:

runtime reads prompt stack first

legacy docs are optional mirrors only

if old agent prompt stack is empty:

seed from current docs exactly in current order

mark source as migrated_from_docs

persist stack

from then on runtime reads prompt stack

Your prompt-unifier agent must prove parity with tests.

10. Messaging model

Formalize agent communication as permissioned and auditable.

Use a message model like:

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
  "status": "queued"
}

Rules:

explicit

permissioned

same-instance by default

auditable

no hidden auto-run unless configured

11. Wizard requirements

You must support two flows:

A. New Instance Wizard

Steps:

Basics

Operating mode/template

Agent topology

Prompt behavior

Model behavior

Delegation

Communication

Skills and tools

Channels

Review + create

B. New Agent Wizard

Steps:

Pick instance

Agent identity/purpose

Archetype

Prompt seed

Role + tools

Messaging permissions

Review + create

The wizard preview output must match the actual created manifests exactly.

Do not duplicate derivation logic in UI. Use shared backend plan/create builders.

12. API requirements

Implement or adapt to these endpoints:

Instances

GET /api/admin/instances

POST /api/admin/instances

GET /api/admin/instances/{instance_id}

PUT /api/admin/instances/{instance_id}

DELETE /api/admin/instances/{instance_id}

POST /api/admin/instances/{instance_id}/activate

GET /api/admin/instances/active

POST /api/admin/instances/{instance_id}/clone

POST /api/admin/instances/bootstrap-from-current

Agents

GET /api/admin/instances/{instance_id}/agents

POST /api/admin/instances/{instance_id}/agents

GET /api/admin/instances/{instance_id}/agents/{agent_id}

PUT /api/admin/instances/{instance_id}/agents/{agent_id}

DELETE /api/admin/instances/{instance_id}/agents/{agent_id}

Wizard

GET /api/admin/wizard/templates

POST /api/admin/wizard/instances/plan

POST /api/admin/wizard/instances/create

POST /api/admin/wizard/agents/plan

POST /api/admin/wizard/agents/create

Feature flags

GET /api/admin/control-plane/features

PATCH /api/admin/control-plane/features

Prompt stack

GET /api/admin/instances/{instance_id}/agents/{agent_id}/prompt-stack

PUT /api/admin/instances/{instance_id}/agents/{agent_id}/prompt-stack/{layer}

preview/history/diff/rollback/lint/test variants

Messaging

POST /api/admin/instances/{instance_id}/messages/send

GET /api/admin/instances/{instance_id}/agents/{agent_id}/inbox

POST /api/admin/instances/{instance_id}/agents/{agent_id}/inbox/{message_id}/ack

POST /api/admin/instances/{instance_id}/agents/{agent_id}/inbox/{message_id}/run

13. Feature flag enforcement

This must be enforced at three layers:

UI

API

Runtime

Examples:

prompt_stack.enabled=false

prompt stack APIs blocked

runtime must not source from prompt stack unless explicit fallback policy exists

delegation.enabled=false

delegation modes blocked

orchestration paths restricted

role_templates.enabled=false

role CRUD blocked

router falls back safely

eval.enabled=false

eval UI/API blocked

Standard error shape:

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
14. Implementation sequence you must drive
Milestone 1: Instance foundation

storage model

manifests

active instance pointer

bootstrap migration

instance CRUD

Milestone 2: Effective runtime resolver

resolve instance + agent effective state

wire engine to instance-aware workspace/model/tools/delegation

Milestone 3: Prompt-source unification

runtime reads prompt stack

docs become mirrored/exported compatibility layer

Milestone 4: Wizard

template catalog

preview/create flows

dashboard UI

Milestone 5: Communication + parallel ops

instance-scoped messaging

inbox/thread model

concurrency policies

Milestone 6: Feature flags

backend guards

UI gating

runtime gating

Milestone 7: Eval/schema cleanup

normalize delegation ledger

stable instance/agent/run metadata for eval

15. Mandatory tests

You must ensure these exist:

bootstrapping default instance from current config works

active instance switching changes workspace/default agent/runtime behavior

two instances can run in parallel without workspace bleed

two agents in same instance can run in parallel

agent messaging respects permissions

prompt stack preview equals runtime prompt source

disabling prompt stack blocks APIs and changes runtime deterministically

disabling delegation blocks planner-led delegation

role templates update routing behavior

wizard create preview matches created manifests

legacy docs migrate without losing behavior

16. Required orchestration behavior
At the start

publish the canonical data model

publish ownership for each workstream

publish merge order and dependencies

During execution

keep agents from editing the same files without explicit coordination

require each agent to report:

touched files

interface changes

new tests

blocking assumptions

On review

Reject changes that:

duplicate manifest derivation logic

bypass effective runtime resolver

keep legacy docs as hidden runtime truth

add UI without backend guardrails

add backend without runtime guardrails where required

create IDs or metadata models inconsistent with instance/agent/run identity

17. Final output expectation

Your final integrated result should produce a repo where an operator can answer these questions unambiguously:

What instance is active?

Which agents belong to it?

What is the real runtime prompt source?

Which features are disabled or read-only?

Which agent owns this channel?

Which agents can talk to each other?

Why was this delegated?

Which model/tools/restrictions applied to this run?

Can I clone this whole environment and run it elsewhere?

If the answer to any of those is fuzzy, the implementation is incomplete.

Now execute this as a coordinated parallel multi-agent build, with strict adherence to the architectural truths above.
