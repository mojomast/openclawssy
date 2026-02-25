# Usage Guide

This page is the practical reference for running Openclawssy day-to-day.

If `docs/GETTING_STARTED.md` is your first launch, this is your operator playbook.

## CLI Surface

Main commands:

```bash
openclawssy init
openclawssy setup
openclawssy ask "hello"
openclawssy run --agent default --message "summarize changes"
openclawssy run --agent default --message-file ./prompt.txt
openclawssy serve --addr 127.0.0.1:8787 --token local-dev-token
openclawssy cron add --agent default --schedule "@every 1h" --message "status report"
openclawssy cron delete --id job_123
openclawssy cron pause
openclawssy cron resume --id job_123
openclawssy remote pull
openclawssy remote status
openclawssy remote send "What model are you using?"
openclawssy doctor
```

## OpenClaw Remote (External Repository)

OpenClaw gateway connectivity is provided by standalone `openclawremoteussy`.

Pull/update it:

```bash
openclawssy remote pull
```

Then build it and point config `openclaw.remote.binary_path` to the built binary.

Reference: `docs/OPENCLAWREMOTEUSSY.md`

## Setup and Provider Configuration

Recommended path:

```bash
openclawssy setup
openclawssy doctor -v
```

Supported model providers:

- `openai` (`OPENAI_API_KEY`)
- `openrouter` (`OPENROUTER_API_KEY`)
- `requesty` (`REQUESTY_API_KEY`)
- `zai` (`ZAI_API_KEY`)
- `generic` (`OPENAI_COMPAT_API_KEY` + `providers.generic.base_url`)

Provider key precedence:

1. Encrypted secret store key: `provider/<provider>/api_key`
2. Provider env var (`OPENAI_API_KEY`, etc.)
3. `providers.<provider>.api_key` in config (discouraged)

## Common Tool-Driven Runs

```bash
# filesystem
openclawssy run --agent default --message '/tool fs.delete {"path":"scratch.txt","force":true}'
openclawssy run --agent default --message '/tool fs.move {"src":"draft.txt","dst":"final.txt"}'
openclawssy run --agent default --message '/tool fs.append {"path":"notes.txt","content":"\nnew line"}'
openclawssy run --agent default --message '/tool fs.edit {"path":"notes.txt","patch":"@@ -1,1 +1,1 @@\n-old\n+new\n"}'

# config and secrets
openclawssy run --agent default --message '/tool config.get {"field":"output.thinking_mode"}'
openclawssy run --agent default --message '/tool config.set {"updates":{"output.thinking_mode":"on_error"}}'
openclawssy run --agent default --message '/tool secrets.set {"key":"provider/openrouter/api_key","value":"<token>"}'
openclawssy run --agent default --message '/tool secrets.list {}'

# scheduler and sessions
openclawssy run --agent default --message '/tool scheduler.add {"id":"job_1","schedule":"@every 1h","message":"status report"}'
openclawssy run --agent default --message '/tool session.list {"agent_id":"default","limit":10}'

# agent and policy control
openclawssy run --agent default --message '/tool agent.list {"limit":20,"offset":0}'
openclawssy run --agent default --message '/tool run.list {"agent_id":"default","limit":20}'
openclawssy run --agent default --message '/tool policy.list {"agent_id":"default"}'

# utility
openclawssy run --agent default --message '/tool time.now {}'
openclawssy run --agent default --message '/tool http.request {"method":"GET","url":"https://example.com"}'

# memory
openclawssy run --agent default --message '/tool memory.write {"kind":"preference","title":"Tone","content":"Prefer concise responses","importance":4,"confidence":0.9}'
openclawssy run --agent default --message '/tool memory.search {"query":"concise responses","limit":5}'
openclawssy run --agent default --message '/tool decision.log {"title":"Retry strategy","content":"Use exponential backoff for flaky calls"}'
openclawssy run --agent default --message '/tool memory.checkpoint {"max_events":200}'
openclawssy run --agent default --message '/tool memory.maintenance {"dry_run":true}'
```

For complete argument-level details, use `docs/TOOL_CATALOG.md`.

## Dashboard and Chat Operations

Start server:

```bash
openclawssy serve --token change-me
```

Dashboard URLs:

- `https://127.0.0.1:8080/dashboard` (TLS enabled)
- `http://127.0.0.1:8080/dashboard` (TLS disabled)

Auth note:

- The frontend prompts for a bearer token on first load.
- Use the same token passed to `openclawssy serve --token ...`.

Chat behavior and controls:

- Session-aware commands: `/new`, `/resume <session_id>`, `/chats`
- Agent routing commands: `/agents`, `/agent`, `/agent <agent_id>`
- Queued chat responses can include `session_id` to support timeline resume

For a full operator/frontend guide, see `docs/DASHBOARD.md`.

## HTTP APIs

Core APIs (Bearer token required):

- `POST /v1/runs`
- `GET /v1/runs`
- `GET /v1/runs/{id}`
- `POST /v1/chat/messages`

Admin APIs (dashboard/backend control):

- `GET /api/admin/status`
- `GET /api/admin/config`
- `POST /api/admin/config`
- `GET /api/admin/secrets`
- `POST /api/admin/secrets`
- `GET /api/admin/scheduler/jobs`
- `POST /api/admin/scheduler/jobs`
- `DELETE /api/admin/scheduler/jobs/{id}`
- `POST /api/admin/scheduler/control`
- `GET /api/admin/agents`
- `POST /api/admin/agents`
- `GET /api/admin/memory/{agent}`

## Shell and Sandbox

`shell.exec` is available only when all are true:

- `sandbox.active=true`
- `sandbox.provider=local`
- `shell.enable_exec=true`

Optional command allowlisting:

- `shell.allowed_commands` enforces allowed command prefixes

Timeout example:

```bash
openclawssy run --agent default --message '/tool shell.exec {"command":"bash","args":["-lc","sleep 60"],"timeout_ms":10000}'
```

## Run Artifacts and Debugging

Per-run artifacts:

- `.openclawssy/agents/<agent>/runs/<run-id>/input.json`
- `.openclawssy/agents/<agent>/runs/<run-id>/prompt.md`
- `.openclawssy/agents/<agent>/runs/<run-id>/toolcalls.jsonl`
- `.openclawssy/agents/<agent>/runs/<run-id>/output.md`
- `.openclawssy/agents/<agent>/runs/<run-id>/meta.json`

Useful debug endpoints:

- `GET /v1/runs/{id}`
- `GET /api/admin/debug/runs/{id}/trace`

Foreground logging example:

```bash
openclawssy serve --token change-me 2>&1 | tee ./_debug_logs/server_console.log
```

## Safety Notes

- Deny-by-default capability model.
- Network is off by default.
- Writes are workspace-bound and path-guarded.
- Secret values are write-only in dashboard/API surfaces.
- This project remains prototype-grade: use isolated environments.

## Dashboard E2E QA (Playwright)

A lightweight browser e2e harness lives under `internal/channels/dashboard/ui` and is kept separate from Go CI.

Run from that directory:

```bash
npm install
npm run e2e:install:linux
npm run e2e:test
```

If your environment already has browser dependencies:

```bash
npm run e2e:install
npm run e2e:test
```

Current spec file `tests/e2e/qa_scripts.spec.js` maps to devplan QA script flows (tool failure visibility, python guidance, secrets workflow, scheduler usability) with deterministic mocked API responses.

If Playwright fails with missing shared libraries (for example `libnspr4.so`), run `npm run e2e:install:linux` and retry.
