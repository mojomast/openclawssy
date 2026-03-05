---
id: providers-and-models
title: Providers & Models
category: Settings
keywords: providers models openai openrouter requesty zai generic agent overrides subagent defaults validation
related_topics: agent-overrides-and-subagents, secrets-guide, faq
route_hints: /settings, /help
---

# Providers & Models

## Supported providers

- `openai`
- `openrouter`
- `requesty`
- `zai`
- `generic`

## Global switching workflow

Use `Settings` -> `Model Provider`.

1. Choose the global `model.provider`
2. Set `model.name`
3. Adjust `temperature`
4. Adjust `max_tokens`
5. Review the provider endpoint card for the selected provider
6. Use `Test provider`
7. Click `Validate` before `Save Config`

## Per-agent overrides

Use `Settings` -> `Agents`.

- Leave provider or model blank to inherit from the global default
- Use explicit overrides only for agents that truly need specialization
- Use bulk actions carefully, especially in multi-agent setups

## Subagent defaults and overrides

Subagent controls let you tune delegated work safely:

- `allowed_tools`
- `timeout_ms`
- `thinking_mode`
- `delegation_mode`
- `max_tool_iterations`

Recommended best practices:

- keep subagent allowed tools narrower than the main agent
- set shorter timeouts for focused delegation tasks
- use inheritance unless you have a clear reason to diverge

## Plain-English validation errors

### Unsupported provider

You entered something other than `openai`, `openrouter`, `requesty`, `zai`, or `generic`.

### Missing model name

The provider is set, but `model.name` is empty.

### Invalid max_tokens

Global `model.max_tokens` must be between `1` and `20000`.

### Invalid profile override max_tokens

Per-agent `max_tokens` must be between `0` and `20000`. Use `0` or blank to inherit.

## Example configurations

```json
{
  "model": {
    "provider": "openrouter",
    "name": "openai/gpt-4.1-mini",
    "temperature": 0.2,
    "max_tokens": 4000
  }
}
```

```json
{
  "agents": {
    "profiles": {
      "research": {
        "model": {
          "provider": "zai",
          "name": "glm-4.7",
          "max_tokens": 8000
        }
      }
    }
  }
}
```
