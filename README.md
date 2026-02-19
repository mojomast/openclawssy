# Openclawssy

Openclawssy is a security-first AI agent runtime in the Ussyverse: one Go binary, explicit controls, auditable runs, and operator-first defaults.

It is for builders who want agent power without mystery behavior.

## Ussyverse Context

Openclawssy is part of the open-source Ussyverse ecosystem: experimental, fast-moving, and built in public.

- Main hub: https://www.ussy.host
- Ussyverse Discord: https://discord.gg/6b2Ej3rS3q

Come chat about Openclawssy and other Ussyverse projects.

## What It Does

- Runs agents through CLI, HTTP, dashboard, Discord, and scheduler channels.
- Enforces deny-by-default capability policy and workspace-safe file boundaries.
- Persists reproducible run artifacts and append-only audit logs.
- Supports encrypted secret ingestion with write-only dashboard/API handling.
- Provides multi-agent control with per-agent profiles, model overrides, and routing pointers.
- Includes session-aware chat timelines and operational controls for long-running workloads.
- Ships a full memory system (event ingestion, checkpoint distillation, recall injection, maintenance, proactive hooks, optional embeddings).

## Core Capabilities

- Runtime and channels
  - `openclawssy ask`, `openclawssy run`, `openclawssy serve`, `openclawssy cron`
  - HTTP APIs for runs and chat queueing
  - Dashboard admin surface for status/config/scheduler/secrets/docs
  - Discord bridge with allowlists and rate limiting

- Agent and policy control
  - Agent lifecycle tools (`agent.list`, `agent.create`, `agent.switch`)
  - Per-agent config profiles (`agents.profiles.<agent_id>`) with model override fields
  - Inter-agent tooling (`agent.message.send`, `agent.message.inbox`, `agent.run`)
  - Policy-gated admin operations (`policy.admin` for sensitive cross-agent edits)

- Safety and observability
  - Workspace/path guards, symlink-safe write checks, and control-plane file protection
  - Structured tool errors and bounded loop execution
  - Persisted bundles per run (`input`, `prompt`, `toolcalls`, `output`, `meta`)
  - Audit logs with redaction behavior
  - Memory admin endpoint (`GET /api/admin/memory/<agent>`) with health + embedding stats

- Memory system
  - Event stream persisted under `.openclawssy/agents/<agent>/memory/events/*.jsonl`
  - Working memory store with tools (`memory.search`, `memory.write`, `memory.update`, `memory.forget`, `memory.health`)
  - Decision logging + checkpoint distillation (`decision.log`, `memory.checkpoint`)
  - Prompt-time recall injection with bounded memory context
  - Weekly maintenance (`memory.maintenance`) and proactive messaging triggers
  - Optional embeddings + semantic hybrid recall (OpenRouter/OpenAI-compatible providers)

## Quickstart

Prerequisite: Go 1.24+

```bash
make fmt
make lint
make test
make build
```

```bash
./bin/openclawssy setup
./bin/openclawssy doctor -v
./bin/openclawssy serve --token change-me
```

Then open:

- `https://127.0.0.1:8080/dashboard` (TLS enabled)
- `http://127.0.0.1:8080/dashboard` (TLS disabled)

## How To Use It

- Fast local run:

```bash
./bin/openclawssy ask -agent default -message "hello"
```

- Tool-driven run:

```bash
./bin/openclawssy run -agent default -message '/tool time.now {}'
```

- API run:

```bash
curl -s -X POST http://127.0.0.1:8080/v1/runs \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"agent_id":"default","message":"summarize project status"}'
```

## Prototype Warning

This is still a prototype under active development.

- Expect breaking changes.
- Use isolated environments and test credentials only.
- Do not run production-critical workloads on it yet.

## Documentation Map

Detailed operational/reference content has been moved out of the README into `docs/`.

- Getting started: [`docs/GETTING_STARTED.md`](docs/GETTING_STARTED.md)
- Usage and workflows: [`docs/USAGE.md`](docs/USAGE.md)
- Architecture: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- Memory system: [`docs/MEMORY_SYSTEM.md`](docs/MEMORY_SYSTEM.md)
- Tool catalog: [`docs/TOOL_CATALOG.md`](docs/TOOL_CATALOG.md)
- Config spec: [`docs/specs/CONFIG.md`](docs/specs/CONFIG.md)
- Contracts + acceptance: [`docs/specs/CONTRACTS.md`](docs/specs/CONTRACTS.md), [`docs/specs/ACCEPTANCE.md`](docs/specs/ACCEPTANCE.md)
- Threat model: [`docs/security/THREAT_MODEL.md`](docs/security/THREAT_MODEL.md)
- Project status: [`docs/PROJECT_STATUS.md`](docs/PROJECT_STATUS.md)

## MIT License

Copyright (c) 2026 Kyle Durepos

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
