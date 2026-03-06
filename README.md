# Openclawssy

## The Ussycoded Agent Runtime

Openclawssy is high-agency AI automation for builders who want speed and control at the same time.

| Speed Lane | Control Lane | Audit Lane |
| --- | --- | --- |
| One Go binary, multi-channel runtime (CLI/API/dashboard/chat/scheduler) | Deny-by-default capability policy and workspace-safe boundaries | Reproducible run artifacts + append-only audit logs |
| Fast iteration with tool-enabled agents | Explicit config and policy-gated operations | Clear failure modes and debuggable traces |

### Launch In 3 Commands

```bash
make build
./bin/openclawssy setup
./bin/openclawssy serve --token change-me
```

If `openclawssy` is already on your PATH, you can drop the `./bin/` prefix.

### What New Users Get In The First 10 Minutes

- A running local agent service with a dashboard UI.
- A real tool-capable run (not just plain chat output).
- Inspectable artifacts at `.openclawssy/agents/<agent>/runs/<run-id>/`.
- A secure baseline: policy controls, workspace guards, and redacted secrets.

Try this once `serve` is running:

```bash
./bin/openclawssy ask "hello"
./bin/openclawssy run --agent default --message '/tool time.now {}'
```

Openclawssy is a security-first AI agent runtime in the Ussyverse: one Go binary, explicit controls, auditable runs, and operator-first defaults.

It is for builders who want agent power without mystery behavior, hidden cloud control planes, or hand-wavy safety claims.

## Why Openclawssy

- You can debug agent behavior instead of guessing what happened.
- You can grant capabilities intentionally instead of trusting hidden defaults.
- You can run one agent locally today, then scale to multi-agent orchestration later.
- You can keep one runtime surface across CLI, API, dashboard, scheduler, and chat bridges.
- You can hand this to a team and keep auditability intact.

If you like fast iteration but still need operational guardrails, this is the lane.

## What Makes It Ussycoded

- Built in public, shipped fast, and unapologetically practical.
- Opinionated toward control, traceability, and clear failure modes.
- High-agency tooling for serious builders, not toy prompt demos.
- Weird enough to be fun, disciplined enough to run real workloads.

## Who It Is For

- Engineers building internal agent platforms.
- Solo builders who want local-first control.
- Teams that need audit trails, policy gates, and reproducible behavior.
- Operators who care more about reliability than flashy demos.

## Popular Use Cases

- **Secure coding copilot runtime:** run tool-enabled coding flows with strict path and capability boundaries.
- **Agent ops platform:** provide API + dashboard + audit logs for internal automation teams.
- **Scheduled automation:** run recurring jobs (`cron`) with agent context and replayable outputs.
- **Multi-agent workflows:** split research/build/review work across agents with policy-gated routing.
- **Chat-bridge assistant:** expose the same runtime safely in Discord/Telegram environments.

## Ussyverse Context

Openclawssy is part of the open-source Ussyverse ecosystem: experimental, fast-moving, and built in public.

- Main hub: https://www.ussy.host
- Ussyverse Discord: https://discord.gg/6b2Ej3rS3q

Come chat about Openclawssy and other Ussyverse projects.

## What It Is Great At

- Building a controllable coding/automation agent that can use tools without escaping your policy boundaries.
- Running long-lived assistant workflows with scheduler jobs, chat timelines, and recoverable state.
- Operating multi-agent setups with explicit routing and per-agent model profiles.
- Giving teams auditability: reproducible run artifacts, structured errors, and append-only logs.
- Mixing speed with safety through workspace guards, secret redaction, and capability checks.

## Core Capabilities

- Runtime and channels
  - `openclawssy ask`, `openclawssy run`, `openclawssy serve`, `openclawssy cron`
  - `openclawssy remote` delegates to standalone `openclawremoteussy`
  - HTTP APIs for runs and chat queueing
  - Dashboard admin surface for status/config/scheduler/secrets/docs
  - Discord bridge with allowlists and rate limiting

- Agent and policy control
  - Agent lifecycle tools (`agent.list`, `agent.create`, `agent.switch`)
  - Per-agent config profiles (`agents.profiles.<agent_id>`) with model override fields
  - Inter-agent tooling (`agent.message.send`, `agent.message.inbox`, `agent.run`)
  - Workspace skill loading (`skill.list`, `skill.read`) plus built-in `clawdefuckifier` bootstrap with a globally seeded workspace skill
  - Policy-gated admin operations (`policy.admin` for sensitive cross-agent edits)

- Safety and observability
  - Workspace/path guards, symlink-safe write checks, and control-plane file protection
  - Structured tool errors and bounded loop execution
  - Persisted bundles per run (`input`, `prompt`, `toolcalls`, `output`, `meta`)
  - Audit logs with redaction behavior
  - Agent Monitor UI for main runs + subagent runs with task IDs, model info, and cancel controls
  - Automatic checkpoint trails for `clawdefuckifier*` agents under `workspace/clawdefuckifier/<agent-id>/`
  - Global workspace skill availability at `workspace/skills/clawdefuckifier.md` so any agent can discover and load the repair workflow
  - Memory admin endpoint (`GET /api/admin/memory/<agent>`) with health + embedding stats

- Provider UX
  - Hatz model discovery from Settings with inline `Query models`
  - Automatic `model.name` dropdown once Hatz models are loaded
  - Inline API key prompt in Settings when a discovery-enabled provider is missing a stored key

- Memory system
  - Event stream persisted under `.openclawssy/agents/<agent>/memory/events/*.jsonl`
  - Working memory store with tools (`memory.search`, `memory.write`, `memory.update`, `memory.forget`, `memory.health`)
  - Decision logging + checkpoint distillation (`decision.log`, `memory.checkpoint`)
  - Prompt-time recall injection with bounded memory context
  - Weekly maintenance (`memory.maintenance`) and proactive messaging triggers
  - Optional embeddings + semantic hybrid recall (OpenRouter/OpenAI-compatible providers)

## Installation

Pick a path:

- **Docker:** fastest for most users; good default for trying it now.
- **Build from source:** best for contributors or custom runtime modifications.

Provider note: set at least one provider API key (for example `ZAI_API_KEY` or `HATZ_API_KEY`), then run `setup` + `doctor -v`.

### Option A: Docker (Recommended)

Docker is the fastest way to get started. The backend runs in one container and spawns a separate isolated sandbox container for each agent's workspace.

```bash
docker run -d \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v ~/.openclawssy:/app/.openclawssy \
  -p 8080:8080 \
  -e ZAI_API_KEY=your-key-here \
  -e OPENCLAWSSY_TOKEN=change-me \
  ghcr.io/openclawssy/openclawssy:latest
```

The socket mount (`-v /var/run/docker.sock:...`) lets the backend talk to Docker to create sandbox containers. This is a Unix socket, not HTTP.

If you want stricter isolation controls without changing default UX, enable opt-in hardening via `sandbox.docker.hardened=true` and (recommended) `sandbox.docker.require_dedicated_daemon=true` with a non-default `sandbox.docker.host` endpoint.

Keep the quick-start minimal: for most users, only `ZAI_API_KEY` and (optionally) `OPENCLAWSSY_TOKEN` are needed.
For hardening and advanced Docker options, see [`DOCKER.md`](DOCKER.md).

Or use the included `docker-compose.yml` in the repo root:

```bash
docker compose up -d
```

See [`DOCKER.md`](DOCKER.md) for full details on architecture, configuration, and permissions.

### Option B: Build from Source

Prerequisite: Go 1.24+

```bash
make build
```

Then run the interactive setup:

```bash
./bin/openclawssy setup
./bin/openclawssy doctor -v
./bin/openclawssy serve --token change-me
```

To enable the Docker sandbox when running from a native build:

```bash
./bin/openclawssy serve --token change-me --sandbox-active --sandbox-provider docker
```

This requires Docker to be installed and the Docker socket accessible.

### Access the Dashboard

Once the server is running, open:

- `https://127.0.0.1:8080/dashboard` (TLS enabled)
- `http://127.0.0.1:8080/dashboard` (TLS disabled)

At first load, the dashboard asks for the same bearer token you passed to `serve`.

Good first dashboard flow:

1. Open Chat and send a simple prompt (`hello`)
2. Run a tool-backed prompt (`/tool time.now {}`)
3. Toggle `Tool timeline` in Chat when you want inline tool bubbles with expandable args/output/error
4. If a long response is interrupted, use `Resume interrupted run` in Chat instead of retyping `continue`
5. Open run details to inspect tool summary + artifacts
6. Open Agent Monitor to watch main/subagent execution and cancellation state
7. Check Settings/Secrets/Scheduler pages for operator controls, including `model.timeout_ms`

For a full frontend guide, see [`docs/DASHBOARD.md`](docs/DASHBOARD.md).

## How To Use It

If you just installed, this is a good first sequence:

```bash
./bin/openclawssy doctor -v
./bin/openclawssy ask "hello"
./bin/openclawssy run --agent default --message "summarize this repository"
```

- Fast local run:

```bash
./bin/openclawssy ask "hello"
```

- Tool-driven run:

```bash
./bin/openclawssy run --agent default --message '/tool time.now {}'
```

- API run:

```bash
curl -s -X POST http://127.0.0.1:8080/v1/runs \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"agent_id":"default","message":"summarize project status"}'
```

## OpenClaw Remote Integration (External Repo)

OpenClaw remote support is now split into its own repository: `openclawremoteussy`.

Pull/update it from Openclawssy:

```bash
openclawssy remote pull
```

Then build and wire it:

```bash
go -C .openclawssy/external/openclawremoteussy build ./cmd/openclawremoteussy
```

Set `.openclawssy/config.json`:

- `openclaw.remote.enabled=true`
- `openclaw.remote.binary_path=.openclawssy/external/openclawremoteussy/openclawremoteussy`

Store gateway token in secret store key `openclaw/remote/auth_token`, then run:

```bash
openclawssy remote status
openclawssy remote send "What is up? Also, what model are you using?"
```

## Prototype Warning

This is still a prototype under active development.

- Expect breaking changes.
- Use isolated environments and test credentials only.
- Do not run production-critical workloads on it yet.

## Documentation Map

Detailed operational/reference content has been moved out of the README into `docs/`.

- Getting started: [`docs/GETTING_STARTED.md`](docs/GETTING_STARTED.md)
- Docker deployment: [`DOCKER.md`](DOCKER.md)
- Usage and workflows: [`docs/USAGE.md`](docs/USAGE.md)
- Dashboard operations: [`docs/DASHBOARD.md`](docs/DASHBOARD.md)
- Dashboard Help Center: open `Help` inside the dashboard or use the global `?` Help Drawer
- Discord setup: [`docs/DISCORD.md`](docs/DISCORD.md)
- OpenClaw remote integration: [`docs/OPENCLAWREMOTEUSSY.md`](docs/OPENCLAWREMOTEUSSY.md)
- Architecture: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- Memory system: [`docs/MEMORY_SYSTEM.md`](docs/MEMORY_SYSTEM.md)
- Tool catalog: [`docs/TOOL_CATALOG.md`](docs/TOOL_CATALOG.md)
- Config spec: [`docs/specs/CONFIG.md`](docs/specs/CONFIG.md)
- Contracts + acceptance: [`docs/specs/CONTRACTS.md`](docs/specs/CONTRACTS.md), [`docs/specs/ACCEPTANCE.md`](docs/specs/ACCEPTANCE.md)
- Threat model: [`docs/security/THREAT_MODEL.md`](docs/security/THREAT_MODEL.md)
- Project status: [`docs/PROJECT_STATUS.md`](docs/PROJECT_STATUS.md)
- Contributing guide: [`CONTRIBUTING.md`](CONTRIBUTING.md)
- Contributors notes: [`CONTRIBUTORS.md`](CONTRIBUTORS.md)

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
