---
id: faq
title: FAQ
category: FAQ
keywords: faq questions common issues help troubleshooting provider token dashboard
related_topics: getting-started, secrets-guide, providers-and-models, discord-bot-setup, custom-dashboards, runs-and-debugging
route_hints: /help, /settings, /secrets, /dashboards
---

# FAQ

## Why does the Help Drawer stay open across tabs?

So you can keep instructions visible while working in the dashboard.

## Why can I see secret key names but not values?

Because the dashboard uses write-only secret ingestion. Presence is safe to show; values are not.

## What should I configure first?

Start with `Secrets`, then `Settings`, then `Agents`, then test with `Chat` and `Runs`.

## Why does validation fail even though I changed only one field?

Validation checks the effective merged config, so an existing invalid field elsewhere may still need attention.

## When should I use agent overrides?

Only when a specific agent truly needs a different model or token budget than the global default.

## What if a custom dashboard looks wrong after editing?

Use `Reset layout`, then rebuild it with the widgets you actually need.
