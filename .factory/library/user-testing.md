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
- **Setup**: Dashboard is served by the openclawssy Docker container. After React changes, the container must be rebuilt (`docker compose build openclawssy && docker compose up -d openclawssy`) to serve updated assets. For development, use Vite dev server at :5175.

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
