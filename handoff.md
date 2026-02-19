# Handoff

Date: 2026-02-19

## Completed: E2E Systems Validation of Docker Sandbox

Full end-to-end validation of the Docker sandbox system was completed successfully.

## What Was Done

### Bugs Found and Fixed

**Bug 1: `go build` failure — embed directory with no embeddable files**
- `internal/channels/dashboard/ui/test-results/` only contained `.last-run.json` (hidden file)
- Go's `//go:embed ui/*` directive can't embed directories that only have hidden files
- Fix: Added `internal/channels/dashboard/ui/test-results/placeholder.txt` with content "placeholder\n"
- File: `internal/channels/dashboard/ui/test-results/placeholder.txt`

**Bug 2: `docker volume ls --format {{.CreatedAt}}` fails on Docker 26.1.5**
- The `ListVolumes` function in `sandbox_admin.go` used `{{.CreatedAt}}` template field
- Docker 26.1.5 (the installed version) does NOT support `{{.CreatedAt}}` in `docker volume ls`
- This caused `GET /api/admin/sandbox/docker/volumes` to return 500 error
- Fix: Removed `{{.CreatedAt}}` from the format string, now uses `{{.Name}}\t{{.Driver}}\t{{.Mountpoint}}`
- File: `internal/channels/http/sandbox_admin.go` (ListVolumes function, ~line 545)

**Issue 3: Production Docker image is stale (not a code bug)**
- The running container `openclawssy-openclawssy-1` uses binary from Feb 18
- That binary predates the sandbox admin routes — so port 8081 returns 404 for sandbox routes
- Additionally, `docker.sock` is commented out in `docker-compose.yml` so even rebuilt, the container couldn't access Docker
- RESOLUTION: Validated locally using `bin/openclawssy serve` on port 18082 (which uses host Docker socket)
- The source code and tests are correct; the production container just needs rebuild + docker.sock mount

### Validation Results (Local Server on Port 18082)

All API endpoints tested and confirmed working:
- `GET /api/admin/sandbox/docker/status` — returns container status
- `GET /api/admin/sandbox/docker/images` — returns 54 images
- `GET /api/admin/sandbox/docker/volumes` — returns volumes (after fix)
- `POST /api/admin/sandbox/docker/create` — creates and starts container
- `POST /api/admin/sandbox/docker/stop` — stops container
- `POST /api/admin/sandbox/docker/reset` — removes container
- Auth: `401` without token, `200` with correct token

### Docker Container Checks
- `openclawssy_agent_default` running from `ubuntu:24.04`
- Volume `openclawssy_ws_default` mounted at `/workspace`
- `/workspace` does NOT exist on host filesystem
- Files written to `/workspace` stay inside container only
- No secrets in container environment
- Path traversal in agent_id sanitized by `sanitizeDockerName()`

## Test Results

```
# Go build
go build ./... clean (after placeholder.txt fix)

# Go tests: all 21 packages pass
go test ./... 21/21 pass

# Sandbox path traversal tests: all pass
go test ./internal/sandbox/... -v PASS

# Sandbox admin HTTP tests: all pass
go test ./internal/channels/http/... -run TestSandbox PASS (16 auth + 14 endpoint tests)
```

## Files Modified

- `internal/channels/dashboard/ui/test-results/placeholder.txt` — Created (fixes go build)
- `internal/channels/http/sandbox_admin.go` — Fixed `ListVolumes` to remove `{{.CreatedAt}}` (Docker 26 compat)
- `bin/openclawssy` — Rebuilt with new code

## Action Required for Production

To make port 8081 serve the sandbox admin routes:
1. Rebuild the Docker image: `docker compose build`
2. Uncomment the docker.sock volume in `docker-compose.yml`: `- /var/run/docker.sock:/var/run/docker.sock`
3. Restart: `docker compose up -d`

## Previous Handoff Context

All 15 Playwright e2e tests for the Sandbox Manager page pass.
Go build and all Go tests also pass.
Pre-existing Playwright failures in qa_scripts.spec.js (tests 1 & 4) remain unrelated.

## Next Task

No next task queued in devplan.md. E2E validation is complete.
