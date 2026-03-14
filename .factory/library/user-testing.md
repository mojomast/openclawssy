# User Testing

Testing surface, validation approach, and resource cost classification.

**What belongs here:** How to validate the mission output through real user surfaces, resource constraints, testing tools.

---

## Validation Surface

### Browser Dashboard
- **URL**: http://localhost:8081/dashboard (production via Docker)
- **Dev URL**: http://localhost:5175 (Vite dev server, proxies API to :8081)
- **Tool**: agent-browser (Playwright Chromium)
- **Auth**: Bearer token required. Default token: `change-me` (set via OPENCLAWSSY_TOKEN or config)
- **Setup**: Dashboard is served by the openclawssy Docker container. After React changes, the container must be rebuilt (`docker compose build openclawssy && docker compose up -d --no-deps openclawssy`) to serve updated assets without blocking on unrelated unhealthy dependencies. For development, use Vite dev server at :5175.

#### Browser Runtime Notes
- If `agent-browser` startup fails with missing shared library errors (for example `libnspr4.so`), run with an isolated `AGENT_BROWSER_HOME` and prepend Playwright bundled libs to `LD_LIBRARY_PATH`.
- If shared-library issues persist, start an isolated Chromium instance with the same `LD_LIBRARY_PATH` and connect `agent-browser` through a dedicated CDP port (for example `9333`) using a non-default session id.
- If agent-browser startup fails due to stale session/daemon state, run `agent-browser session list`, close stale sessions, and retry with a fresh session id.
- For `/dashboard-legacy` verification, if browser network capture misses the request, use `curl -i http://localhost:8081/dashboard-legacy` as supplemental status evidence.
- If agent-browser request tracking returns no captured requests for dashboard flows, collect supplemental network evidence using `curl -i` against the same local API endpoints exercised by the UI flow.
- For decision-ledger UI checks, create a fresh run first (`POST /v1/runs`) and deep-link to `/#/runs/<run-id>` for deterministic "Why this happened" validation against known decision records.

### Playwright Runtime Bootstrap (Linux)
- The Playwright config at `internal/channels/dashboard/ui/playwright.config.js` now bootstraps `LD_LIBRARY_PATH` at process startup.
- If `/home/mojo/.cache/playwright-libs/root/usr/lib/x86_64-linux-gnu` exists, it is prepended to `LD_LIBRARY_PATH` before browsers launch.
- This applies to both direct `npx playwright ...` commands and npm scripts that run Playwright tests (for example `npm run e2e:test -- tests/e2e/help.spec.ts`).
- If the fallback directory is missing, no environment mutation is applied.

### CLI (Eval Harness)
- **Tool**: terminal (Execute tool)
- **Binary**: `./bin/openclawssy`
- **Setup**: `go build -o ./bin/openclawssy ./cmd/openclawssy` before each CLI test

### API Endpoints
- **Tool**: curl
- **Base URL**: http://localhost:8081
- **Auth header**: `Authorization: Bearer change-me`

## Validation Concurrency

### agent-browser
- **Max concurrent validators: 2**
- **Rationale**: Machine has 15GB RAM total, ~8GB available. Each Chromium instance uses ~150-300MB. With Docker containers, Go builds, and Vite dev server consuming resources, 2 concurrent validators is safe. No swap configured means OOM risk with more.
- **CPU**: 4 cores, load ~2 typically. 2 concurrent browser validators is sustainable.

### CLI/curl
- **Max concurrent validators: 3**
- **Rationale**: Terminal and curl are lightweight. Main constraint is concurrent Go test runs.

## Flow Validator Guidance: Browser Dashboard

- Assigned surface: `http://localhost:8081/dashboard` only.
- Use bearer token `change-me` when auth prompt appears; do not modify server auth/config.
- Stay within assigned assertions; do not run unrelated flows.
- Use isolated browser context/session per validator and do not share local storage state across validators.
- Do not modify global runtime settings, scheduler jobs, secrets, or sandbox resources unless explicitly required by an assigned assertion.
- Save all screenshots/network evidence under the assigned evidence directory only.

## Flow Validator Guidance: API Contract Endpoints

- Assigned surface: local authenticated admin HTTP API at `http://localhost:8081`.
- Use only assertion-scoped endpoints needed for the assigned contract checks.
- Always include `Authorization: Bearer change-me`; do not mutate auth configuration.
- Avoid writes to unrelated global state; if an assertion requires mutation (for example rollback), keep it minimal and restore/verify post-condition in the same flow.
- Save request/response evidence under the assigned evidence directory and include status code plus response body excerpts.

## Flow Validator Guidance: Terminal Backend Assertions

- Assigned surface: local project workspace terminal only (`/home/mojo/projects/openclawssy`).
- Limit execution to assertion-scoped commands (targeted `go test`, `go build`, and `openclawssy eval` invocations needed for assigned assertions).
- Do not mutate unrelated config, services, or data stores; avoid destructive commands.
- Capture command output snippets and exit codes in the flow report, and save raw logs under the assigned evidence directory.
