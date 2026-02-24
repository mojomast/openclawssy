# Getting Started

Welcome. This is the fastest safe path to try Openclawssy locally.

Think of this as your first walk through the Ussyverse: small scope, clean controls, and auditable outcomes.

## 1) Build

```bash
make fmt
make lint
make test
make build
```

## 2) Guided setup

```bash
./bin/openclawssy setup
```

During setup you can:
- pick provider and model
- ingest API key into encrypted secret store
- enable HTTPS dashboard
- enable Discord bot
- select sandbox provider (Docker recommended — runs workspace in a separate container)

## 3) Verify

```bash
./bin/openclawssy doctor -v
```

## 4) Start server

```bash
./bin/openclawssy serve --token change-me
```

> **Docker sandbox:** If you chose the Docker sandbox provider, you have two
> options:
>
> - **Docker deployment (simplest):** Run Openclawssy via `docker compose` —
>   see [DOCKER.md](../DOCKER.md). The backend spawns a separate sandbox
>   container for each agent's workspace via the mounted Docker socket.
> - **Native host:** Run the binary directly with
>   `--sandbox-active --sandbox-provider docker`. Requires Docker installed
>   and your user in the `docker` group (or use sudo).
>
> Optional hardening: set `sandbox.docker.hardened=true` in `config.json` and,
> for strongest isolation, set `sandbox.docker.require_dedicated_daemon=true`
> with a non-default `sandbox.docker.host` endpoint.

## 5) Open dashboard

- HTTPS mode: `https://127.0.0.1:8080/dashboard`
- HTTP mode: `http://127.0.0.1:8080/dashboard`

Dashboard tips:
- Chat is session-aware (`/new`, `/resume <session_id>`, `/chats`).
- Tool activity is summarized per step (for example file writes show line counts).
- You can resize the chat panel and collapse tool/session/status/admin panes.
- Long runs keep updating in-place with elapsed time, completed tool-call count, and latest tool summary.

## 6) Send a run

```bash
curl -s -X POST http://127.0.0.1:8080/v1/runs \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"agent_id":"default","message":"/tool fs.list {""path"":"".""}"}'
```

Or use chat mode through the same API:

```bash
curl -s -X POST http://127.0.0.1:8080/v1/chat/messages \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"dashboard_user","room_id":"dashboard","agent_id":"default","message":"list files and read README.md"}'
```

Queued chat responses include `session_id` when available so clients can poll the same session history and tool timeline.

## Failure recovery behavior

- After 2 consecutive tool failures, the runner switches the model into recovery mode and expects a changed approach.
- If 3 additional failures occur after recovery mode starts (even if successes are mixed in), the run stops and asks for user guidance.
- The guidance prompt includes attempted commands, error text, and output snippets to make next-step steering explicit.

## Important warning

This project is still a prototype. Use it only in disposable dev environments.

Ussyverse rule #1: if it matters, isolate it.
