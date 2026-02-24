# Docker Setup Guide

## Quick Start

Run Openclawssy with Docker sandbox isolation in a single command:

```bash
docker run -d \
  -e ZAI_API_KEY=<your_key> \
  -v ~/.openclawssy:/app/.openclawssy \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -p 8080:8080 \
  ghcr.io/mojomast/openclawssy:latest
```

### Why the Docker socket mount?

The `-v /var/run/docker.sock:/var/run/docker.sock` mount is required. Here's why:

Openclawssy runs two containers: the **backend** (API, dashboard, engine) and a separate **workspace sandbox** container per agent. The backend needs to talk to the Docker daemon to create and manage those sandbox containers. The socket mount gives it that access.

This is a **Unix domain socket**, not HTTP. The `docker` CLI inside the backend container talks to the host Docker daemon through this file -- the same way `docker` works on your host. Nothing is proxied over the network.

This is the same pattern used by CI/CD systems (Jenkins, GitLab Runner, GitHub Actions self-hosted runners) and container orchestrators.

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

4. **Access the dashboard:**
   - Local: http://localhost:8081/dashboard
   - Tailscale: http://<tailscale-ip>:8081/dashboard (from any device on your tailnet)
   - Enter your bearer token (from `.env` or default: `change-me`)
   - Start chatting with the bot!

### Features

- **Chat Interface**: Built-in chat UI in the dashboard
- **ZAI Integration**: Pre-configured for GLM-4.7 Coding Plan
- **Docker Sandbox**: Agent workspace runs in a separate isolated container
- **Secure Setup**: API key validation on startup
- **Persistent Storage**: Configuration stored in host-mounted volume
- **Shell-ready runtime image**: Includes `bash`, `python3`/`pip`, `node`/`npm`, `git`, `curl`, `wget`, `jq`, and common GNU utilities
- **Network diagnostics included**: `nmap`, `dig`/`nslookup`, `ip`, `ss`, `netstat`, `traceroute`, `tcpdump`, `mtr`, `nc`, `socat`, and related tools
- **Long-run progress UX**: Dashboard chat keeps polling with elapsed time + tool progress instead of stalling on manual refresh prompts
- **Failure escalation flow**: After repeated tool failures, the agent shifts to recovery mode and then asks for user guidance

## Architecture

```
┌─────────────────────────────────┐
│  Host                           │
│                                 │
│  ┌───────────────────────────┐  │
│  │ Backend Container         │  │
│  │ (openclawssy server)      │  │
│  │                           │  │
│  │  API / Dashboard / Engine │  │
│  │         │                 │  │
│  │         │ docker CLI      │  │
│  │         ▼                 │  │
│  │  /var/run/docker.sock ────┼──┼──► Host Docker Daemon
│  └───────────────────────────┘  │           │
│                                 │           ▼
│  ┌───────────────────────────┐  │  ┌─────────────────┐
│  │ Sandbox Container         │  │  │ Created by the   │
│  │ (per agent)               │  │  │ Docker daemon    │
│  │                           │  │  │ on behalf of the │
│  │  /workspace (named vol)   │  │  │ backend          │
│  │  network=none             │  │  └─────────────────┘
│  │  cpu/memory limits        │  │
│  └───────────────────────────┘  │
└─────────────────────────────────┘
```

- The backend container runs the openclawssy server
- It uses `docker exec` / `docker cp` to run commands and transfer files in the sandbox
- The sandbox container runs `sleep infinity` and is reused across agent runs
- Each agent gets its own sandbox container and named volume
- The sandbox has `network=none` by default (no outbound access)
- Workspace data lives in Docker named volumes, never on the host filesystem

## Docker Sandbox Configuration

All sandbox settings are configurable in `config.json` under `sandbox.docker`:

| Setting | Default | Description |
|---------|---------|-------------|
| `sandbox.active` | `true` | Enable sandbox isolation |
| `sandbox.provider` | `docker` | Sandbox provider |
| `sandbox.docker.image` | `ubuntu:24.04` | Base image for sandbox containers |
| `sandbox.docker.host` | inherited | Optional Docker daemon endpoint (`unix://`, `tcp://`, `ssh://`) |
| `sandbox.docker.cpu_limit` | `1.0` | CPU limit per sandbox container |
| `sandbox.docker.memory_limit_mb` | `2048` | Memory limit in MB |
| `sandbox.docker.network_enabled` | `false` | Enable network access in sandbox |
| `sandbox.docker.hardened` | `false` | Opt-in stricter container flags (`cap-drop=ALL`, `no-new-privileges`, read-only rootfs, tmpfs, PID limit) |
| `sandbox.docker.pids_limit` | `256` (when hardened) | Max process count per sandbox container |
| `sandbox.docker.allowed_images` | empty | Optional image allowlist for sandbox containers |
| `sandbox.docker.require_dedicated_daemon` | `false` | Require non-default Docker daemon endpoint |
| `sandbox.docker.pull_policy` | `if-not-present` | Image pull policy |

## Hardened mode (opt-in)

By default, Openclawssy uses the current implementation (compatible and simple). If you want stronger isolation, enable hardened mode explicitly in `config.json`:

```json
{
  "sandbox": {
    "active": true,
    "provider": "docker",
    "docker": {
      "hardened": true,
      "image": "ubuntu:24.04",
      "allowed_images": ["ubuntu:24.04"],
      "pids_limit": 256,
      "require_dedicated_daemon": true,
      "host": "unix:///var/run/openclawssy-docker.sock"
    }
  }
}
```

What hardened mode changes for sandbox containers:

- adds `--security-opt no-new-privileges:true`
- adds `--cap-drop ALL`
- adds `--read-only` root filesystem
- mounts tmpfs for `/tmp`, `/run`, and `/var/tmp`
- applies `--pids-limit` (default `256` in hardened mode)

This keeps default UX unchanged, while allowing operators to opt into stricter controls when security is the priority.

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `ZAI_API_KEY` | Yes | - | Your Z.AI API key for GLM-4.7 |
| `OPENCLAWSSY_TOKEN` | No | `change-me` | Bearer token for API/dashboard access |
| `DISCORD_BOT_TOKEN` | No | - | Optional Discord bot integration |

For deeper network diagnostics in the container (for example `tcpdump`/advanced `nmap` modes), you may also need extra capabilities:

```yaml
cap_add:
  - NET_ADMIN
  - NET_RAW
```

**Note**: The container exposes port 8080 internally. Map it to any available port on your host (e.g., 8081 to avoid conflicts).

## Docker Permissions

The `docker` CLI inside the backend container needs to talk to the Docker daemon via the socket. Inside the container, the process runs as root so this works automatically.

On your **host**, if you need to run `docker` commands without `sudo`, add your user to the `docker` group:

```bash
sudo usermod -aG docker $USER
```

Then log out and back in. This is a one-time setup.

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
- Verify the Docker socket is mounted: check that `-v /var/run/docker.sock:/var/run/docker.sock` is present
- Check Docker daemon is running: `docker info`
- Review sandbox logs: `docker-compose logs -f openclawssy | grep sandbox`
- List sandbox volumes: `docker volume ls | grep openclawssy_ws_`

**"permission denied" on Docker socket:**
- Inside the container this should not happen (runs as root)
- On the host, add your user to the `docker` group: `sudo usermod -aG docker $USER`

**Tool calls keep failing in loops:**
- The runner now auto-enters recovery mode after repeated failures and escalates with a guidance request after additional failures.
- Review the returned attempted steps/errors/output, then provide a corrective instruction.

**Verify network diagnostic tools are present:**
- `docker-compose exec openclawssy sh -lc 'nmap --version && dig +short example.com && ip -br a && ss -tulpen | head -n 5'`
