# OpenClawRemoteUssy Integration

Openclawssy no longer embeds OpenClaw remote gateway code directly.

Remote connectivity is delegated to the standalone repository:

- `https://github.com/mojomast/openclawremoteussy`

## Pull from Openclawssy

```bash
openclawssy remote pull
```

Default pull target:

- `.openclawssy/external/openclawremoteussy`

## Build

```bash
go -C .openclawssy/external/openclawremoteussy build ./cmd/openclawremoteussy
```

## Configure Openclawssy

Set `.openclawssy/config.json`:

```json
{
  "openclaw": {
    "remote": {
      "enabled": true,
      "repository_url": "https://github.com/mojomast/openclawremoteussy.git",
      "binary_path": ".openclawssy/external/openclawremoteussy/openclawremoteussy",
      "ws_primary": "wss://your-gateway.example.com",
      "ws_fallback": "ws://your-fallback-gateway.example.net:18789",
      "session_key": "agent:main:main",
      "connect_timeout_ms": 10000,
      "request_timeout_ms": 15000,
      "poll_interval_ms": 1200,
      "poll_timeout_ms": 60000,
      "prefer_tailnet_wss": true
    }
  }
}
```

Store auth token in encrypted secrets under key:

- `openclaw/remote/auth_token`

## Use

```bash
openclawssy remote status
openclawssy remote send "What is up? Also, what model are you using?"
openclawssy remote history --limit 10
openclawssy remote reconnect
```

## Runtime behavior in `serve`

When `openclaw.remote.enabled=true`, `openclawssy serve` performs a startup probe by invoking:

- `openclawremoteussy status --healthcheck`

Startup continues even if probe fails, but a warning is logged.
