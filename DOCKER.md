# Docker Setup Guide

## Quick Start

Run Openclawssy with sandbox isolation in a single command:

```bash
docker run \
  -e ZAI_API_KEY=<your_key> \
  -v ~/.openclawssy:/app/.openclawssy \
  -p 8080:8080 \
  ghcr.io/mojomast/openclawssy:latest \
  serve --token change-me --sandbox-active --sandbox-provider local
```

The container itself IS the sandbox. Agent commands run inside the container with filesystem isolation enforced by the `local` provider -- no Docker socket, no child containers, no Docker-in-Docker. The container boundary provides the isolation.

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

   The container provides sandbox isolation by default. Agent commands run inside the container -- not on the host, not in nested child containers.

4. **Access the dashboard:**
   - Local: http://localhost:8081/dashboard
   - Tailscale: http://<tailscale-ip>:8081/dashboard (from any device on your tailnet)
   - Enter your bearer token (from `.env` or default: `change-me`)
   - Start chatting with the bot!

### Features

- **Chat Interface**: Built-in chat UI in the dashboard
- **ZAI Integration**: Pre-configured for GLM-4.7 Coding Plan
- **Container Sandbox**: The Docker container IS the sandbox -- no Docker-in-Docker
- **Secure Setup**: API key validation on startup
- **Persistent Storage**: Configuration stored in host-mounted volume
- **Shell-ready runtime image**: Includes `bash`, `python3`/`pip`, `node`/`npm`, `git`, `curl`, `wget`, `jq`, and common GNU utilities
- **Network diagnostics included**: `nmap`, `dig`/`nslookup`, `ip`, `ss`, `netstat`, `traceroute`, `tcpdump`, `mtr`, `nc`, `socat`, and related tools
- **Installer-script compatibility**: Includes `openrc` tools (`rc-update`) so common curl-piped installers on Alpine fail less often
- **Long-run progress UX**: Dashboard chat keeps polling with elapsed time + tool progress instead of stalling on manual refresh prompts
- **Failure escalation flow**: After repeated tool failures, the agent shifts to recovery mode and then asks for user guidance with attempted steps/errors/output

## Sandbox Architecture

### Container-as-Sandbox (Docker Deployment)

When you deploy Openclawssy via Docker, the container itself provides the isolation:

- **No Docker socket mount** -- the container cannot access the host Docker daemon
- **No child containers** -- there is no Docker-in-Docker or sibling container spawning
- **No `docker-cli`** -- the runtime image does not include Docker CLI tools
- The `local` sandbox provider runs agent commands directly inside the container
- Filesystem operations are confined to the container's workspace
- The container boundary (namespaces, cgroups) is the isolation boundary

This is the simplest and most secure Docker deployment model.

### Docker Provider (Native Host Deployment)

If you run the openclawssy binary directly on your host (not in a container), you can use the `docker` sandbox provider to run agent commands inside isolated Docker containers:

```bash
./bin/openclawssy serve --token change-me --sandbox-active --sandbox-provider docker
```

This requires Docker to be installed on the host. The binary communicates with the Docker daemon to create per-agent sandbox containers. This is the correct use case for the `docker` provider.

### Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `SANDBOX_ACTIVE` | `true` | Enable sandbox isolation |
| `SANDBOX_PROVIDER` | `local` | Sandbox provider (`local` for Docker deployment, `docker` for native host) |

Docker-specific settings (only relevant when using `--sandbox-provider docker` on native host):

| Setting | Default | Description |
|---------|---------|-------------|
| `sandbox.docker.image` | `ubuntu:24.04` | Base image for agent containers |
| `sandbox.docker.cpu_limit` | `1.0` | CPU limit per agent container |
| `sandbox.docker.memory_limit_mb` | `2048` | Memory limit in MB per agent container |
| `sandbox.docker.network_enabled` | `false` | Enable network access in sandbox containers |

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

**Tool calls keep failing in loops:**
- The runner now auto-enters recovery mode after repeated failures and escalates with a guidance request after additional failures.
- Intermittent success does not immediately clear recovery mode; escalation still occurs if failure patterns continue.
- Review the returned attempted steps/errors/output, then provide a corrective instruction (for example capabilities, auth, or alternate approach).

**Curl installer script fails with `rc-update: not found`:**
- Rebuild with updated image that includes `openrc`: `docker-compose build --no-cache openclawssy && docker-compose up -d`
- Retry installer command after rebuild.

**Verify network diagnostic tools are present:**
- `docker-compose exec openclawssy sh -lc 'nmap --version && dig +short example.com && ip -br a && ss -tulpen | head -n 5'`
