# Environment

Environment variables, external dependencies, and setup notes.

**What belongs here:** Required env vars, external API keys/services, dependency quirks, platform-specific notes.
**What does NOT belong here:** Service ports/commands (use `.factory/services.yaml`).

---

## Required

- **Go 1.24+** (installed: 1.24.4)
- **Node.js 20+** (installed: v20.19.6)
- **npm 10+** (installed: 10.8.2)

## Environment Variables

- `ZAI_API_KEY` — Required for Z.AI model provider
- `OPENCLAWSSY_TOKEN` — Bearer token for dashboard auth (default: change-me)
- `DISCORD_BOT_TOKEN` — Optional, for Discord integration
- `BECOMUSSY_DB_USER` / `BECOMUSSY_DB_PASS` — Local dev DB creds for becomussy service

## Docker Services

Running via docker-compose.yml:
- `openclawssy` container on port 8081 (maps to 8080 internal)
- `becomussy` service (internal network only)
- `infrastructure-db-1` (pgvector/postgres) on port 5432
- `infrastructure-redis-1` on port 6379

## Platform Notes

- Machine: 15GB RAM, 4 CPUs, 62GB disk free
- No swap configured — be careful with memory-intensive operations
- Playwright Chromium installed at `~/.cache/ms-playwright/chromium-1208/`
