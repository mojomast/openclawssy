# Handoff

Date: 2026-02-24

## Completed: DOCKER.md update for Docker sandbox provider

## What Was Done

Updated `/home/mojo/projects/openclawssy/DOCKER.md` with comprehensive Docker sandbox documentation:

- Added prominent one-liner `docker run` command at the top using `ghcr.io/mojomast/openclawssy:latest` with `--sandbox-active --sandbox-provider docker` flags
- Removed old "Manual Docker Run" section that mounted `./workspace` from host
- Removed all `$(pwd)/workspace:/app/workspace` host mount patterns
- Made Docker socket mount (`-v /var/run/docker.sock:/var/run/docker.sock`) required with explanation that the process needs it to spawn child containers per agent
- Added "Docker Sandbox" section explaining isolated containers, named volumes (`openclawssy_ws_<agentId>`), network disabled by default, CPU/memory limits, no host filesystem exposure
- Added 6 new environment variables: SANDBOX_ACTIVE, SANDBOX_PROVIDER, SANDBOX_IMAGE, SANDBOX_CPU_LIMIT, SANDBOX_MEMORY_LIMIT_MB, SANDBOX_NETWORK_ENABLED
- Updated docker-compose instructions to note sandbox is enabled by default
- Added "Legacy Local Sandbox" note with security warning for `--sandbox-provider local`
- Added "Sandbox containers not starting" troubleshooting entry
- Preserved all existing Tailscale, API endpoints, and troubleshooting sections

## Files Modified

- `DOCKER.md` — Complete rewrite (147 lines → 201 lines)

## Next Task

No next task queued in devplan.md.

## Previous Context

E2E Docker sandbox validation was completed previously. All Go tests pass. The sandbox admin API routes work correctly. Two bugs were fixed (go:embed placeholder, Docker volume CreatedAt format field).
