# Openclawssy Config Spec (v0.1)

Default config path: `.openclawssy/config.json`

## Setup Flow (Guided)
1. Run `openclawssy init -agent default`.
2. Edit `.openclawssy/config.json` and set `model.provider` + `model.name`.
3. Set the matching API key env var.
4. Verify with `openclawssy doctor -v`.
5. Optional: enable sandbox + shell exec, HTTPS dashboard, Discord bridge.

Default shipping profile:
- `model.provider`: `zai`
- `model.name`: `GLM-4.7`
- `model.max_tokens`: `20000`

## Provider Support
- `openai` (OpenAI endpoint)
- `openrouter`
- `requesty`
- `zai` (ZAI coding-plan compatible OpenAI-style endpoint)
- `generic` (any OpenAI-compatible base URL)

Provider API key env defaults:
- `openai` -> `OPENAI_API_KEY`
- `openrouter` -> `OPENROUTER_API_KEY`
- `requesty` -> `REQUESTY_API_KEY`
- `zai` -> `ZAI_API_KEY`
- `generic` -> `OPENAI_COMPAT_API_KEY`

## Runtime Schema

```json
{
  "network": {
    "enabled": false,
    "allowed_domains": []
  },
  "shell": {
    "enable_exec": false,
    "allowed_commands": []
  },
  "engine": {
    "max_concurrent_runs": 64
  },
  "scheduler": {
    "catch_up": true,
    "max_concurrent_jobs": 4
  },
  "sandbox": {
    "active": false,
    "provider": "none",
    "docker": {
      "image": "ubuntu:24.04",
      "network_enabled": false,
      "cpu_limit": 0,
      "memory_limit_mb": 0,
      "pull_policy": "if-not-present",
      "extra_env": [],
      "mounts": []
    }
  },
  "server": {
    "bind_address": "127.0.0.1",
    "port": 8080,
    "tls_enabled": false,
    "tls_cert_file": ".openclawssy/certs/server.crt",
    "tls_key_file": ".openclawssy/certs/server.key",
    "dashboard_enabled": true
  },
  "output": {
    "thinking_mode": "never",
    "max_thinking_chars": 4000
  },
  "workspace": {
    "root": "./workspace"
  },
  "model": {
    "provider": "openai",
    "name": "gpt-4o-mini",
    "temperature": 0.2,
    "max_tokens": 20000
  },
  "providers": {
    "openai": {
      "base_url": "https://api.openai.com/v1",
      "api_key_env": "OPENAI_API_KEY"
    },
    "openrouter": {
      "base_url": "https://openrouter.ai/api/v1",
      "api_key_env": "OPENROUTER_API_KEY"
    },
    "requesty": {
      "base_url": "https://router.requesty.ai/v1",
      "api_key_env": "REQUESTY_API_KEY"
    },
    "zai": {
      "base_url": "https://api.z.ai/api/coding/paas/v4",
      "api_key_env": "ZAI_API_KEY"
    },
    "generic": {
      "base_url": "",
      "api_key_env": "OPENAI_COMPAT_API_KEY"
    }
  },
  "agents": {
    "enabled_agent_ids": [],
    "allow_inter_agent_messaging": true,
    "allow_agent_model_overrides": true,
    "self_improvement_enabled": false,
    "profiles": {
      "default": {
        "enabled": true,
        "self_improvement": false,
        "model": {
          "provider": "openai",
          "name": "gpt-4o-mini",
          "max_tokens": 12000
        }
      }
    }
  },
  "chat": {
    "enabled": true,
    "default_agent_id": "default",
    "allow_users": [],
    "allow_rooms": [],
    "rate_limit_per_min": 20,
    "global_rate_limit_per_min": 120
  },
  "discord": {
    "enabled": false,
    "token_env": "DISCORD_BOT_TOKEN",
    "default_agent_id": "default",
    "allow_guilds": [],
    "allow_channels": [],
    "allow_users": [],
    "command_prefix": "!ask",
    "rate_limit_per_min": 20
  },
  "secrets": {
    "store_file": ".openclawssy/secrets.enc",
    "master_key_file": ".openclawssy/master.key"
  },
  "memory": {
    "enabled": false,
    "max_working_items": 200,
    "max_prompt_tokens": 1200,
    "auto_checkpoint": false,
    "proactive_enabled": true,
    "embeddings_enabled": false,
    "embedding_provider": "openrouter",
    "embedding_model": "text-embedding-3-small",
    "event_buffer_size": 256
  }
}
```

## Security Invariants
- Config is human-managed; agent tools do not get write access to `.openclawssy/`.
- Workspace write policy stays enforced after path and symlink resolution.
- `shell.exec` is enabled only when sandbox is active and provider is not `none`.
- `shell.allowed_commands` entries must be non-empty when provided.
- Supported sandbox providers: `none`, `local`, `docker`.
- When `provider=docker`, all agent fs.* tools and shell.exec run inside the container; no host filesystem access.
- HTTP APIs require bearer token.
- Chat queue accepts allowlisted senders only and enforces rate limits.
- Discord queue accepts allowlisted senders/channels/guilds and enforces rate limits.
- Secret values are write-only at API/UI surface; only key names are listed.
- Tool calls and run lifecycle events are always audited with redaction.

## Docker Sandbox Notes
- `sandbox.docker.image` — Docker image to use (default: `ubuntu:24.04`).
- `sandbox.docker.network_enabled` — enables bridge networking; default `false` (network=none).
- `sandbox.docker.cpu_limit` — fractional CPU limit (e.g. `0.5`); `0` means no limit.
- `sandbox.docker.memory_limit_mb` — memory limit in MB; `0` means no limit.
- `sandbox.docker.pull_policy` — `if-not-present` (default), `always`, or `never`.
- `sandbox.docker.extra_env` — non-secret env vars injected into the container. **Never put secrets here.**
- `sandbox.docker.mounts` — additional bind mounts (advanced; exposes host paths, use with caution).
- Container name is `openclawssy_agent_<agent_id>`; volume is `openclawssy_ws_<agent_id>`.
- Workspace inside the container is always `/workspace`.

## Model Runtime Notes
- `model.max_tokens` is validated in the range `1..20000`.
- Runtime enforces this cap on provider requests.
- Long chat history is compacted by runtime before context exhaustion.

## Agent Profiles and Control
- `agents.enabled_agent_ids` is an optional allowlist; when set, only listed agents can run.
- `agents.profiles.<agent_id>.enabled=false` disables that specific agent.
- `agents.allow_agent_model_overrides=true` allows `agents.profiles.<agent_id>.model` to override provider/model settings per agent.
- `agents.allow_inter_agent_messaging` toggles `agent.message.send` and `agent.message.inbox` workflows.
- `agents.self_improvement_enabled` gates prompt file mutation tools (`agent.prompt.update`).
- `agents.profiles.<agent_id>.self_improvement=true` must also be set for that agent before prompt mutation is allowed.

## Output Notes
- `output.thinking_mode` supports: `never`, `on_error`, `always`.
- Default is `never`.
- `output.max_thinking_chars` bounds persisted/returned thinking content.
- CLI `ask` supports per-call override: `openclawssy ask --thinking=always ...`.

## Concurrency and Scheduling Notes
- `engine.max_concurrent_runs` limits concurrent runtime executions.
- `scheduler.max_concurrent_jobs` limits scheduler worker concurrency per tick.
- `scheduler.catch_up=true` runs missed jobs once at startup check; `false` skips missed windows.

## Chat Rate Limiting
- `chat.rate_limit_per_min` applies to sender-scoped keys.
- `chat.global_rate_limit_per_min` applies process-wide across all chat senders.

## Memory Configuration
- `memory.enabled` toggles memory subsystem behavior globally.
- `memory.max_working_items` caps retained/retrieved working memory candidate set.
- `memory.max_prompt_tokens` bounds memory recall block size in prompt assembly.
- `memory.auto_checkpoint` enables default scheduler checkpoint wiring (`@every 6h`).
- `memory.proactive_enabled` enables proactive memory-triggered inter-agent message hooks.
- `memory.embeddings_enabled` enables embedding sync and semantic hybrid recall.
- `memory.embedding_provider` selects provider for embedding API calls (`openai|openrouter|requesty|zai|generic`).
- `memory.embedding_model` sets embedding model name for provider requests.
- `memory.event_buffer_size` controls async event ingestion queue capacity.

OpenRouter embeddings are supported through `providers.openrouter.base_url` + `OPENROUTER_API_KEY` (or `providers.openrouter.api_key`).
