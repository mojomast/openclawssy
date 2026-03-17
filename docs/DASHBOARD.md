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
- **Settings > Agents**: profile and routing controls
- **Agent Monitor**: live main/subagent monitoring, launch, cancel, task IDs, and checkpoint visibility
- **Memory**: memory health/stat visibility per agent
- **Custom Dashboards**: operator-defined widget layouts with server-backed persistence
- **Help**: full Help Center with searchable docs and a route-aware Help Drawer
- **Control Plane**: Agent Contract, Prompt Stack, Role Templates, Delegation, and Eval pages

Depending on your build/runtime features, additional pages (for example sandbox manager) may appear.

## Control-plane pages

All control-plane routes are hash-based under `/dashboard/#/...`.

### Agent Contract (`/#/agent-contract`)

- Primary APIs: `GET /api/admin/agents`, `GET /api/admin/agents/{id}/resolved`, `GET /api/admin/agents/{id}/diff?base=...`, `POST /api/admin/agents/{id}/rollback-snapshot`, `POST /api/admin/agents/{id}/rollback-restore`
- Use it to inspect resolved vs raw contract fields, inheritance source labels, and field-level diffs against global defaults or another agent.
- Verification notes:
  - Switch `Resolved`/`Raw` and confirm raw view hides inherited global-only rows.
  - Open `Diff` panel and compare against `global`.
  - Save a rollback snapshot and restore it to validate rollback wiring.

### Prompt Stack (`/#/prompt-stack`)

- Primary APIs: `GET /api/admin/agents/{id}/prompt-stack`, `PUT /api/admin/agents/{id}/prompt-stack/{layer}`, `GET /preview`, `GET /history`, `GET /diff?v1=&v2=`, `POST /rollback`, `POST /lint`, `POST /test`
- Includes all five layers: `global_operator_policy`, `agent_identity`, `tool_safety_rules`, `delegation_policy`, `session_overlay`.
- Verification notes:
  - Edit and save a layer, then confirm merged preview and per-layer token counts refresh.
  - Run lint + structural tests and confirm result cards render.
  - Load a version diff and run rollback to a selected version.

### Role Templates (`/#/roles`)

- Primary APIs: `GET/POST /api/admin/roles`, `PUT/DELETE /api/admin/roles/{name}`
- Built-in roles are read-only; custom roles are fully editable/deletable.
- Verification notes:
  - Create a custom role with tool + timeout constraints.
  - Confirm built-in roles show read-only messaging.
  - Update and delete the custom role and verify list refresh.

### Delegation (`/#/delegation`)

- Primary APIs: `PATCH /api/admin/config` (delegation fields), `GET /v1/runs`, `GET /v1/runs/{id}`, `GET /api/admin/runs/{id}/decisions`
- Policy editor controls `delegation_mode`, `delegation_threshold`, `delegation_cooldown_iterations`, and `auto_delegate`.
- Task Graph Preview reads decomposition plans from selected runs; Run Comparison renders decision-ledger timelines side by side.
- Decision and monitor payloads now surface explicit `instance_id` alongside `agent_id` and `run_id`, matching the RFC identity direction.
- Debug trace and monitor payloads now also surface `parent_run_id` when runtime lineage is available.
- Delegation and Runs pages now thread composite `instance_id` + `agent_id` back into detail/decision requests so duplicate `run_id` values remain disambiguated in the UI.
- Verification notes:
  - Save policy changes and confirm success state.
  - Load a run with decomposition data and verify graph nodes/edges render.
  - Compare two runs and confirm divergence summary + timeline panels.

### Eval Results (`/#/eval`)

- Primary API: `GET /api/admin/eval/results?limit=...&suite=...`
- Shows suite history table with expandable details: metrics cards, case-level pass/fail table, baseline regression panel, additive normalized eval identity metadata (`instance_id`, `agent_id`, `run_id`, `parent_run_id`, `root_run_id`, `source`, `task_id`, `session_id`) when present, and additive runtime metadata (`artifact_path`, `checkpoint_path`, decomposition/delegation summaries) when stored.
- Verification notes:
  - Run `openclawssy eval run --suite basic` first so data exists.
  - Refresh page and expand a row to verify metrics, identity/delegation metadata, and baseline comparison blocks.

## Runtime and validation notes

- Dashboard routing is hash-based (`/#/...`) to stay compatible with embedded SPA serving.
- Prompt stack history is persisted in `.openclawssy` via the prompt-stack version store; first-load defaults are seeded from agent docs (`SOUL.md`, `RULES.md`, `TOOLS.md`, `SPECPLAN.md`, `DEVPLAN.md`, `HEARTBEAT.md`, `HANDOFF.md`).
- Delegation run comparison depends on decision records emitted to per-agent audit logs (`.openclawssy/agents/<agent>/audit/events.jsonl`).
- Canonical instance-scoped audits are progressively taking over under `.openclawssy/instances/<instance>/agents/<agent>/audit/events.jsonl`; dashboard decision and monitor flows now preserve `instance_id` where available while keeping compatibility with legacy audit locations.
- Eval UI data is read from `.openclawssy/eval/results.db`; baseline comparisons use saved baselines under `.openclawssy/eval/baselines/`.
- HTTP run SSE streams can now be subscribed with `instance_id` and `agent_id` query parameters to follow the composite event-bus identity path while legacy bare `run_id` streams remain available for compatibility.
- Wizard, instance-control, and instance-agent admin routes now enforce their control-plane feature flags server-side and return structured `403` errors when disabled.
- Current validation commands for this control-plane surface:

```bash
go test -parallel 2 ./...
go vet ./...
cd internal/channels/dashboard/ui && npx tsc --noEmit
```

- Manual sanity pass for docs/features: ensure README + this guide reflect shipped routes, APIs, and CLI commands (`openclawssy eval run|list|results|baseline set|compare`).

## Agent Monitor

Use `Agent Monitor` when you need to supervise long-running or self-iterating agents.

What it shows now:

- main-agent runs and subagent runs in one timeline
- task IDs for iterative phases like diagnose/patch/verify
- model provider/model name used for the run
- latest error text for failed runs
- checkpoint paths for agents that emit resumable run notes

ClawDefuckifier-specific behavior:

- the shared skill is also seeded at `workspace/skills/clawdefuckifier.md`, so non-ClawDefuckifier agents can still discover and load it
- agents whose id starts with `clawdefuckifier` auto-bootstrap with self-improvement enabled
- their latest resumable checkpoint is mirrored to `workspace/clawdefuckifier/<agent-id>/LATEST.md`
- per-run checkpoints are written under `workspace/clawdefuckifier/<agent-id>/runs/`

## Discord onboarding from the dashboard

The fastest operator flow is:

1. Open `Settings` -> `Chat/Discord/Telegram`
2. Use `Discord Setup` to store the bot token in the encrypted secret store
3. Confirm the `Discord token: Present` status
4. Enable `discord.enabled`
5. Set allowlists and command prefix as needed

For full Discord bot creation and invite steps, see `docs/DISCORD.md`.

## Help Center

The dashboard now includes two help surfaces:

- `Help` top-level page for full documentation browsing
- global `?` Help Drawer that stays open while you switch tabs

Help Drawer features:

- persistent open/closed state
- contextual help based on the active route
- instant search across shipped help topics
- quick links to major setup and troubleshooting guides
- mobile-friendly full-screen overlay behavior

Developer note:

- Help topics live in `internal/channels/dashboard/ui/help/*.md`
- They are embedded and served by the dashboard static asset handler under `/dashboard/static/help/*.md`

## Custom Dashboards

`Custom Dashboards` is a top-level dashboard route for building operator-specific layouts.

Workflow:

1. Open `Custom Dashboards`
2. Create a dashboard
3. Rename it from the top bar
4. Add widgets from the searchable widget picker
5. Drag widgets by the header and resize from the bottom-right handle
6. Use arrow keys to nudge a focused widget, or `Delete` to remove it

Current widget registry includes:

- Runs: Recent
- Scheduler: Jobs
- Runtime Status
- Runtime: Overview
- Chat: Quick prompt
- Sessions: Recent
- Secrets: Presence summary
- Secrets: Discord token
- Secrets: Key conventions
- Settings: Model summary + Agent overrides summary
- Settings: Provider endpoints
- Settings: Agents snapshot
- Settings: Subagent defaults
- Settings: Memory summary
- Settings: Network policy
- Settings: Scheduler config
- Discord/Telegram status
- Sessions: Overview
- Skills: Summary
- Skills: Active list
- Docs: Agent prompt docs
- Docs: Top files
- Sandbox: Status
- Sandbox: Images & volumes

Persistence model:

- Local-first cache in browser localStorage for instant UX
- Server-backed persistence in `.openclawssy/dashboard_layouts.json` through:
  - `GET /api/admin/dashboards`
  - `POST /api/admin/dashboards`
  - `PUT /api/admin/dashboards/{id}`
  - `DELETE /api/admin/dashboards/{id}`

Architectural note:

- Widgets are registered from a stable widget registry keyed by `widget_key`
- Each saved dashboard stores layout items using `{widget_key, widget_instance_id, x, y, w, h, widget_state}`
- Compact runs/scheduler widgets reuse exported render helpers from their source page modules instead of duplicating display logic
- Secret widgets may show only key names or presence booleans; secret values are never rendered

## Model & Provider switching

Use `Settings` -> `Model Provider` for global model/provider changes:

1. Set global `model.provider`
2. Set global `model.name`
3. Adjust `temperature`, `max_tokens`, and `timeout_ms`
4. Edit provider endpoint `base_url` and `api_key_env` per provider
5. Use `Test provider` to probe endpoint reachability before saving
6. Use `Query models` when a provider supports model discovery (for example Hatz)
7. When Hatz models are loaded, `model.name` switches from free text to a dropdown of discovered model IDs
8. If Hatz discovery reports a missing API key, use the inline prompt in Settings to store `provider/hatz/api_key` without leaving the page

Chat interruption recovery:

- When a run ends with the runtime's interrupted-stream recovery message, Chat surfaces `Resume interrupted run` automatically.
- The resume action sends a structured continuation prompt for the current session, so operators do not need to type `continue` manually.
- If provider timeouts happen repeatedly, raise global `model.timeout_ms` or the selected agent profile's `model.timeout_ms` in `Settings` -> `Model Provider` / `Agents`.

Chat tool timeline:

- Chat now supports a `Tool timeline: on/off` toggle in the transcript header.
- When enabled, live tool calls stay inline in the transcript as their own bubbles instead of only appearing as `Latest tool` hints.
- Inline tool bubbles remain after the run completes, and each bubble can be expanded to inspect full arguments, output, and error text.
- The final assistant response still lands in its own message bubble after the tool work, which makes long workflows easier to scan.

Workspace browser:

- `Workspace` opens a browser-native view of the configured workspace root.
- You can navigate folders, preview text files, and watch generated artifacts appear without leaving the dashboard.
- File previews are read-only and constrained to the workspace root; traversal outside the workspace is rejected.
- Auto-refresh can be enabled from the workspace toolbar when you want the browser to keep checking for new files while an agent is working.

Agent and subagent controls live under `Settings` -> `Agents`:

- per-agent profile overrides with inheritance-friendly blanks
- bulk action to set all profile model overrides
- structured subagent defaults editor
- structured subagent override editor

Config API ergonomics:

- `GET /api/admin/config` returns redacted config
- `PATCH /api/admin/config` merges partial updates instead of replacing the full blob
- `POST /api/admin/config/validate` checks a config draft without saving and returns structured `field_errors`

## Manual test script

1. Open `Custom Dashboards`
2. Create two dashboards
3. Rename one dashboard
4. Add at least three widgets to one dashboard
5. Drag and resize widgets, refresh, and confirm layout persistence
6. Open the same dashboard from another browser/session and confirm server-backed persistence
7. Go to `Settings` -> `Model Provider`
8. Switch the global provider/model and run `Validate`
9. Set an agent profile override and confirm the profile still shows inherited behavior when provider/model fields are blank
10. Confirm Secrets page and custom dashboard widgets never display secret values

## Help Center QA checklist

1. Open the dashboard and click `?`
2. Confirm the Help Drawer opens without leaving the current route
3. Switch between at least three routes and confirm the drawer stays open
4. Confirm contextual help changes for `Settings`, `Secrets`, and `Custom Dashboards`
5. Press `Esc` and confirm the drawer closes
6. Press `?` or `F1` while not typing and confirm the drawer toggles
7. Open `Help` from the drawer and confirm topic deep links work
8. Search for `discord`, `provider`, and `scheduler` and confirm relevant topics appear quickly
9. Copy a topic link and confirm it opens the correct `Help` topic directly
10. On mobile or narrow width, confirm the drawer becomes an overlay panel

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
