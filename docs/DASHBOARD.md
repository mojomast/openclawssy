# Dashboard Guide

This guide covers practical frontend dashboard usage for operators and contributors.

## Start the server

```bash
./bin/openclawssy serve --token change-me
```

Open:

- `https://127.0.0.1:8080/dashboard` (TLS)
- `http://127.0.0.1:8080/dashboard` (no TLS)

The UI prompts for a bearer token on first load. Enter the same token used in `serve`.

## First 5-minute workflow

1. Open the Chat page and send `hello`.
2. Send a tool-backed prompt:
   - `/tool time.now {}`
3. Watch run progress update in place.
4. Open run details and inspect output/tool summary.
5. Use `/new` to start a new session and `/chats` to switch timelines.

## Chat productivity controls

- Session commands: `/new`, `/resume <session_id>`, `/chats`
- Agent commands: `/agents`, `/agent`, `/agent <agent_id>`
- Resizable chat panel and collapsible panes for tool/session/status/admin views

## Operator pages (admin)

Use dashboard admin sections to manage runtime behavior:

- **Status**: runtime health and basic diagnostics
- **Settings**: editable safe runtime config fields
- **Secrets**: write-only secret updates and key cleanup (values are not re-displayed)
- **Scheduler**: recurring job create/pause/resume/delete
- **Agents**: profile and routing controls
- **Memory**: memory health/stat visibility per agent

Depending on your build/runtime features, additional pages (for example sandbox manager) may appear.

## Discord onboarding from the dashboard

The fastest operator flow is:

1. Open `Settings` -> `Chat/Discord/Telegram`
2. Use `Discord Setup` to store the bot token in the encrypted secret store
3. Confirm the `Discord token: Present` status
4. Enable `discord.enabled`
5. Set allowlists and command prefix as needed

For full Discord bot creation and invite steps, see `docs/DISCORD.md`.

## Common troubleshooting

- **401/unauthorized in UI**
  - Re-enter token; it must match `serve --token ...`
- **Dashboard not loading**
  - Check bind address/port and TLS mode in config
  - Confirm `server.dashboard_enabled=true`
- **No tool execution**
  - Verify agent policy/capability allows the requested tool
  - For `shell.exec`, verify sandbox + shell settings are enabled
- **Remote chat command issues**
  - Confirm external `openclawremoteussy` binary and `openclaw/remote/auth_token` are configured

## Frontend contributor quick loop

Dashboard UI source lives under:

- `internal/channels/dashboard/ui`

Run e2e checks:

```bash
cd internal/channels/dashboard/ui
npm install
npm run e2e:install:linux
npm run e2e:test
```

If browsers are already installed:

```bash
npm run e2e:install
npm run e2e:test
```
