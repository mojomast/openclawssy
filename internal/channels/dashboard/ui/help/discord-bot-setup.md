---
id: discord-bot-setup
title: Discord Bot Setup
category: Integrations
keywords: discord bot setup token intents invite permissions developer portal message content
related_topics: secrets-guide, providers-and-models, faq, troubleshooting-integrations
route_hints: /settings, /secrets, /help
---

# Discord Bot Setup

## Create the Discord application

1. Open the Discord Developer Portal: `https://discord.com/developers/applications`
2. Click `New Application`
3. Enter a name and create the application

## Add the bot user

1. Open the `Bot` tab
2. Click `Add Bot`
3. Create or reset the bot token
4. Copy the token immediately

> [!WARNING]
> After a reset, old tokens stop working immediately. If your bot suddenly stops responding, update the stored token in the dashboard.

## Enable required privileged intents

This bot implementation requires the `Message Content Intent`.

In the `Bot` tab:

1. Find `Privileged Gateway Intents`
2. Enable `Message Content Intent`
3. Save changes

## Invite the bot to a server

1. Open `OAuth2` -> `URL Generator`
2. Select the `bot` scope
3. Choose permissions that allow the bot to read and send messages
4. Copy the generated invite URL
5. Open the URL and add the bot to your server

## Complete setup in the dashboard

1. Open `Settings`
2. Go to the `Discord` section and use `Discord Setup`
3. Paste the bot token and click `Save Token`
4. Confirm the token status changes to `Present`
5. Enable `discord.enabled`
6. Set:
   - `discord.default_agent_id`
   - `discord.command_prefix`
   - `discord.allow_guilds`
   - `discord.allow_channels`
   - `discord.allow_users`
   - `discord.rate_limit_per_min`
7. Save config changes

## Recommended permissions and safeguards

- keep allowlists narrow at first
- use a clear command prefix such as `!ask`
- verify rate limits before inviting the bot to busy servers

## Troubleshooting

### Bot ignores messages

- confirm `discord.enabled=true`
- confirm `Message Content Intent` is enabled
- confirm the bot was invited to the server
- confirm the stored token has not been rotated elsewhere

### Commands do not trigger

- check the configured `discord.command_prefix`
- verify allowlists are not blocking the guild, channel, or user

### Token shows missing

- store the token in the dashboard-managed secret `discord/bot_token`
- or verify the external env named by `discord.token_env` exists

### Bot worked before, then stopped

- the token may have been reset
- save the new token in `Discord Setup`
