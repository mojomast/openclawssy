---
id: troubleshooting-integrations
title: Troubleshooting Integrations
category: Integrations
keywords: integrations troubleshooting discord telegram provider token env secret connectivity
related_topics: discord-bot-setup, secrets-guide, providers-and-models, faq
route_hints: /settings, /secrets, /help
---

# Troubleshooting Integrations

## Discord problems

- check token presence
- check Message Content intent
- check allowlists
- check command prefix

## Telegram problems

- check token presence
- check allowed users or chats
- confirm `telegram.enabled`

## Provider connectivity problems

- run `Test provider` to check endpoint reachability from the dashboard
- verify `base_url`
- verify `api_key_env`
- verify the underlying key or token is actually present in the running environment

## Secret-store vs environment confusion

Use the encrypted store for dashboard-managed workflows.
Use `*_env` fields only when your deployment injects secrets externally.
