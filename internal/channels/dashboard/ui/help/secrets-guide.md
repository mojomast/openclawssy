---
id: secrets-guide
title: Secrets
category: Secrets
keywords: secrets write-only encrypted rotate delete token env discord provider api key
related_topics: discord-bot-setup, providers-and-models, faq
route_hints: /secrets, /settings, /help
---

# Secrets

## What secrets are

Secrets are sensitive values such as provider API keys and bot tokens.

## Security guarantees

- secrets are ingested through write-only flows
- values are never re-displayed in the dashboard UI
- secret presence can be shown, but secret values must never be shown

## Common patterns

- `discord/bot_token`
- provider API key env references such as `OPENAI_API_KEY`
- external token environment names via `*_token_env`

## Add, rotate, and delete

### Add

1. Open `Secrets`
2. Enter the key name
3. Enter the value
4. Click the store button

### Rotate

Save the same key again with the new value.

### Delete

Use `Delete key` on the Secrets page or the dedicated token delete action in guided setup flows.

## Environment variable vs encrypted store

- the encrypted store is the recommended dashboard-managed workflow
- `token_env` and `api_key_env` are fallbacks for externally managed process environments

## Troubleshooting

### Enabled but missing token

- store the token in the encrypted store
- or confirm the external environment variable named in config actually exists in the running process

### Permission problems

- ensure the runtime can read the encrypted store file and master key file

### Rotated token but still failing

- confirm the old token is no longer cached externally
- confirm the connector was restarted or reloaded if required by your deployment
