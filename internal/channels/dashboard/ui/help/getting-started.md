---
id: getting-started
title: Getting Started
category: Getting Started
keywords: dashboard overview onboarding tabs first steps quick tour help center
related_topics: discord-bot-setup, providers-and-models, secrets-guide, runs-and-debugging
route_hints: /chat, /runs, /settings, /help
---

# Getting Started

> [!INFO]
> The dashboard is your operator cockpit for configuring Openclawssy, running work, debugging failures, and monitoring integrations.

## Quick tour of the tabs

- `Chat` sends live prompts to the active agent.
- `Runs` shows recent run history, traces, and tool failures.
- `Settings` manages models, providers, agents, subagents, and runtime behavior.
- `Secrets` ingests keys safely without re-displaying values.
- `Scheduler` manages recurring jobs and one-shot automation.
- `Custom Dashboards` lets you build your own widget-based operator views.
- `Sandbox`, `Skills`, and `Docs` expose advanced runtime and prompt-management workflows.

## Recommended first setup steps

1. Open `Secrets` and add your provider API key.
2. Open `Settings` and configure the global provider and model.
3. Review `Agents` and per-agent overrides if you run multiple workflows.
4. Use `Chat` to send a first prompt.
5. Use `Runs` to confirm the model, tools, and output look right.

## When to use the Help Drawer

The Help Drawer is designed to stay open while you work. Use it when you want:

- route-specific tips without leaving the page
- setup checklists while editing settings
- troubleshooting steps next to a failing run
- quick links to deeper help topics

## First-setup checklist

```text
Secrets -> Settings -> Agents -> Chat -> Runs
```

## Common early mistakes

- missing provider API key or wrong env name
- enabling Discord or Telegram before storing a token
- setting a provider but leaving `model.name` blank
- creating agent overrides that accidentally conflict with global defaults

See related topics below for detailed setup flows.
