---
id: custom-dashboards
title: Custom Dashboards
category: Dashboards
keywords: custom dashboards widgets drag resize save rename layout localStorage persistence
related_topics: getting-started, agent-overrides-and-subagents, faq
route_hints: /dashboards, /help
---

# Custom Dashboards

## What they are

Custom Dashboards let you build your own operator workspace from reusable widgets.

## Supported actions

- create multiple dashboards
- rename dashboards
- reorder dashboards
- duplicate dashboards
- delete dashboards
- add widgets from a searchable registry
- drag widgets by the header
- resize widgets from the bottom-right corner

## Saving behavior

- changes are cached in localStorage for instant UX
- changes are also persisted on the server so they survive browser or device changes

## Available widgets

- Runs: Recent
- Scheduler: Jobs
- Runtime Status
- Chat: Quick prompt
- Secrets: Presence summary
- Settings: Model summary + Agent overrides summary
- Discord/Telegram status

## Keyboard support

- arrow keys nudge a focused widget
- `Delete` removes a focused widget

## Resetting layouts

Use `Reset layout` from the dashboard toolbar to clear all widgets from the current custom dashboard.

## Future-facing note

Layout sharing/export is not a dedicated workflow yet, but server-backed persistence already makes dashboards portable across browser sessions.
