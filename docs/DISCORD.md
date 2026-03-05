# Discord Setup

Use this guide to create a Discord bot, store the token safely, and finish setup from the dashboard.

## 1. Create the application

1. Open the Discord Developer Portal: `https://discord.com/developers/applications`
2. Click `New Application`
3. Give the app a name and create it

## 2. Create the bot user

1. Open the `Bot` tab in the application
2. Click `Add Bot`
3. Under the bot token section, create or reset the token as needed
4. Copy the token immediately; Discord will not keep showing it forever

## 3. Enable required intents

This bot implementation requires the `Message Content Intent`.

In the `Bot` tab:

1. Find `Privileged Gateway Intents`
2. Enable `Message Content Intent`
3. Save changes in the portal

Without this intent, normal message commands will not work correctly.

## 4. Invite the bot to a server

1. Open the `OAuth2` -> `URL Generator` tab
2. In `Scopes`, select:
   - `bot`
3. In `Bot Permissions`, select the permissions your workflow needs
   - At minimum, the bot must be able to read messages and send messages
4. Copy the generated invite URL
5. Open the URL and authorize the bot into the target server

## 5. Store the token from the dashboard

1. Start Openclawssy and open the dashboard
2. Go to `Settings` -> `Chat/Discord/Telegram`
3. Find `Discord Setup`
4. Paste the token into the password-style field
5. Click `Save Token`

The dashboard stores the token in the encrypted secret store under the canonical key:

- `discord/bot_token`

The UI is write-only. After saving, the token value is not shown again.

## 6. Enable the Discord connector

Still in `Settings` -> `Discord`:

1. Enable `discord.enabled`
2. Confirm `discord.default_agent_id`
3. Set or review:
   - `discord.command_prefix`
   - `discord.allow_guilds`
   - `discord.allow_channels`
   - `discord.allow_users`
   - `discord.rate_limit_per_min`
4. Save config changes

The dashboard will show whether the Discord token is `Present` or `Missing`.

## 7. Understand `token_env` vs secrets store

- Recommended: store the token in the encrypted secret store through the dashboard using `discord/bot_token`
- Optional fallback: use `discord.token_env` if you want the process to read the token from an external environment variable such as `DISCORD_BOT_TOKEN`

If both exist, use the dashboard-managed secret as the primary operator workflow.

## 8. Rotate or delete the token

- To rotate: paste a new token into `Discord Setup` and click `Save Token` again
- To delete: use the `Delete stored token` action in `Discord Setup` or delete the key from the `Secrets` page

## Troubleshooting

- `Discord token: Missing`
  - Save a token in `Discord Setup`, or ensure the external env named by `discord.token_env` is present
- Bot does not respond to messages
  - Check `discord.enabled`
  - Confirm `Message Content Intent` is enabled
  - Confirm allowlists and command prefix settings
  - Verify the bot was invited with the needed permissions
