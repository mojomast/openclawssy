# Docker Setup Guide

## Quick Start

Run Openclawssy with Docker sandbox isolation in a single command:

```bash
docker run \
  -e ZAI_API_KEY=<your_key> \
  -v openclawssy_ws_default:/workspace \
  -v ~/.openclawssy:/app/.openclawssy \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -p 8080:8080 \
  ghcr.io/mojomast/openclawssy:latest \
  serve --token change-me --sandbox-active --sandbox-provider docker
```

This starts the server on port 8080 with the Docker sandbox provider enabled. Each agent runs commands inside an isolated container — no host filesystem mounts leak into the agent environment.

> **Docker socket is required.** The `-v /var/run/docker.sock:/var/run/docker.sock` mount is not optional. The Openclawssy process needs access to the Docker daemon so it can spawn child containers for each agent's sandbox. Without it, the Docker sandbox provider cannot create or manage agent containers.

## Quick Start with ZAI Coding Plan

Openclawssy is pre-configured to use **ZAI's GLM-4.7 Coding Plan** as the default provider.

### Prerequisites

1. Docker and Docker Compose installed
2. A Z.AI API key from https://z.ai/subscribe

### Setup

1. **Copy the environment template:**
   ```bash
   cp .env.example .env
   ```

2. **Edit `.env` and add your ZAI API key:**
   ```bash
   ZAI_API_KEY=your-actual-api-key-here
   OPENCLAWSSY_TOKEN=your-secure-token-here
   ```

3. **Build and run:**
   ```bash
   docker-compose up --build
   ```

   The `docker-compose.yml` enables the Docker sandbox provider by default. Agent commands run inside isolated child containers rather than on the host.

4. **Access the dashboard:**
   - Local: http://localhost:8081/dashboard
   - Tailscale: http://<tailscale-ip>:8081/dashboard (from any device on your tailnet)
   - Enter your bearer token (from `.env` or default: `change-me`)
   - Start chatting with the bot!

### Features

- **Chat Interface**: Built-in chat UI in the dashboard
- **ZAI Integration**: Pre-configured for GLM-4.7 Coding Plan
- **Docker Sandbox**: Agent commands run in isolated per-agent containers
- **Secure Setup**: API key validation on startup
- **Persistent Storage**: Configuration stored in named Docker volumes
- **Shell-ready runtime image**: Includes `bash`, `python3`/`pip`, `node`/`npm`, `git`, `curl`, `wget`, `jq`, and common GNU utilities
- **Network diagnostics included**: `nmap`, `dig`/`nslookup`, `ip`, `ss`, `netstat`, `traceroute`, `tcpdump`, `mtr`, `nc`, `socat`, and related tools
- **Installer-script compatibility**: Includes `openrc` tools (`rc-update`) so common curl-piped installers on Alpine fail less often
- **Long-run progress UX**: Dashboard chat keeps polling with elapsed time + tool progress instead of stalling on manual refresh prompts
- **Failure escalation flow**: After repeated tool failures, the agent shifts to recovery mode and then asks for user guidance with attempted steps/errors/output

## Docker Sandbox

The Docker sandbox provider runs each agent's commands inside an isolated container. This is the recommended (and default) execution mode.

### How It Works

- **Isolated containers**: Each agent gets its own container based on the configured sandbox image (default: `ubuntu:24.04`). Commands execute inside the container, not on the host.
- **Named volumes**: Workspace data is stored in named Docker volumes following the pattern `openclawssy_ws_<agentId>`. For example, the default agent uses `openclawssy_ws_default`. These volumes are managed by Docker and persist across container restarts.
- **Network disabled by default**: Agent containers have networking disabled for security. This prevents agent code from making outbound connections or accessing internal services. Enable it only when the agent genuinely needs network access.
- **Resource limits**: CPU and memory limits are configurable per agent container. Defaults are 1.0 CPU and 2048 MB of memory. Adjust these based on your workload.
- **No host filesystem exposure**: The agent environment never mounts host directories. All file operations happen inside the named volume, which the host cannot accidentally leak into.

### Configuration

All sandbox settings can be controlled via environment variables or CLI flags:

| Variable | Default | Description |
|----------|---------|-------------|
| `SANDBOX_ACTIVE` | `true` | Enable sandbox isolation |
| `SANDBOX_PROVIDER` | `docker` | Sandbox provider (`docker` or `local`) |
| `SANDBOX_IMAGE` | `ubuntu:24.04` | Base image for agent containers |
| `SANDBOX_CPU_LIMIT` | `1.0` | CPU limit per agent container |
| `SANDBOX_MEMORY_LIMIT_MB` | `2048` | Memory limit in MB per agent container |
| `SANDBOX_NETWORK_ENABLED` | `false` | Enable network access in sandbox containers |

### Legacy Local Sandbox

Users who still need direct host filesystem access can pass `--sandbox-provider local` to bypass container isolation. **This is not recommended** — the local provider runs agent commands directly on the host with no resource limits and no filesystem isolation. Use it only for debugging or in trusted single-user environments where you accept the risk.

```bash
docker run \
  -e ZAI_API_KEY=<your_key> \
  -v ~/.openclawssy:/app/.openclawssy \
  -p 8080:8080 \
  ghcr.io/mojomast/openclawssy:latest \
  serve --token change-me --sandbox-provider local
```

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `ZAI_API_KEY` | Yes | - | Your Z.AI API key for GLM-4.7 |
| `OPENCLAWSSY_TOKEN` | No | `change-me` | Bearer token for API/dashboard access |
| `DISCORD_BOT_TOKEN` | No | - | Optional Discord bot integration |
| `SANDBOX_ACTIVE` | No | `true` | Enable sandbox isolation |
| `SANDBOX_PROVIDER` | No | `docker` | Sandbox provider (`docker` or `local`) |
| `SANDBOX_IMAGE` | No | `ubuntu:24.04` | Base image for agent containers |
| `SANDBOX_CPU_LIMIT` | No | `1.0` | CPU limit per agent container |
| `SANDBOX_MEMORY_LIMIT_MB` | No | `2048` | Memory limit in MB per agent container |
| `SANDBOX_NETWORK_ENABLED` | No | `false` | Enable network access in sandbox containers |

For deeper network diagnostics in the container (for example `tcpdump`/advanced `nmap` modes), you may also need extra capabilities:

```yaml
cap_add:
  - NET_ADMIN
  - NET_RAW
```

**Note**: The container exposes port 8080 internally. Map it to any available port on your host (e.g., 8081 to avoid conflicts).

### API Endpoints

- **Dashboard**: http://localhost:8081/dashboard
- **Chat API**: POST `/v1/chat/messages`
- **Run API**: POST `/v1/runs`
- **Admin API**: `/api/admin/*`

All endpoints require Bearer token authentication.

### Tailscale Access

Openclawssy is configured to be accessible over Tailscale for secure remote access:

1. **Ensure Tailscale is running** on your host machine
2. **Get your Tailscale IP**:
   ```bash
   tailscale ip -4
   # or
   tailscale status
   ```

3. **Access from any device on your tailnet**:
   - Dashboard: `http://<tailscale-ip>:8081/dashboard`
   - API: `http://<tailscale-ip>:8081/v1/...`

4. **Security considerations**:
   - The server binds to all interfaces (`0.0.0.0`) by default for Docker/Tailscale compatibility
   - Always use a strong bearer token (set via `OPENCLAWSSY_TOKEN`)
   - Consider enabling TLS if accessing over untrusted networks
   - The bearer token is required for all API access

**Note**: When using Tailscale, the service is accessible from any device on your tailnet, not just localhost. Ensure your bearer token is kept secure!

### Troubleshooting

**Container exits immediately:**
- Check that `ZAI_API_KEY` is set in your `.env` file
- Run `docker-compose logs` to see error messages

**Can't access dashboard:**
- Verify the container is running: `docker-compose ps`
- Check the token matches what you set in `.env`
- View logs: `docker-compose logs -f`

**API errors:**
- Verify your ZAI API key is valid at https://z.ai
- Check network connectivity: `docker-compose exec openclawssy ping api.z.ai`

**Shell commands fail with `bash`/`python` not found:**
- Rebuild with the updated image: `docker-compose build --no-cache openclawssy`
- Restart the service: `docker-compose up -d`
- Verify tools are present: `docker-compose exec openclawssy sh -lc 'bash --version && python3 --version && node --version'`

**Sandbox containers not starting:**
- Verify the Docker socket is mounted: check that `-v /var/run/docker.sock:/var/run/docker.sock` is present in your run command or `docker-compose.yml`
- Check Docker daemon is running: `docker info`
- Review sandbox logs: `docker-compose logs -f openclawssy | grep sandbox`
- List sandbox volumes: `docker volume ls | grep openclawssy_ws_`

**Tool calls keep failing in loops:**
- The runner now auto-enters recovery mode after repeated failures and escalates with a guidance request after additional failures.
- Intermittent success does not immediately clear recovery mode; escalation still occurs if failure patterns continue.
- Review the returned attempted steps/errors/output, then provide a corrective instruction (for example capabilities, auth, or alternate approach).

**Curl installer script fails with `rc-update: not found`:**
- Rebuild with updated image that includes `openrc`: `docker-compose build --no-cache openclawssy && docker-compose up -d`
- Retry installer command after rebuild.

**Verify network diagnostic tools are present:**
- `docker-compose exec openclawssy sh -lc 'nmap --version && dig +short example.com && ip -br a && ss -tulpen | head -n 5'`
