---
id: agent-overrides-and-subagents
title: Agents, Overrides & Subagents
category: Settings
keywords: agents profiles inherit overrides subagent defaults subagent overrides delegation tools timeout thinking mode
related_topics: providers-and-models, runs-and-debugging, custom-dashboards
route_hints: /settings, /dashboards, /help
---

# Agents, Overrides & Subagents

## Agent profiles

Agent profiles let you tune behavior for named agents without changing the global default.

You can configure:

- enabled state
- self-improvement flag
- model provider
- model name
- max tokens

## Inheritance rules

- blank provider or model name means `inherit global`
- blank or `0` max tokens means `inherit global`

## Bulk actions

Bulk update helps when you need to move many profiles to the same provider/model quickly.

Use it when:

- standardizing after changing providers
- rolling out a new default model family
- testing the same model across many agents

## Subagent defaults

Subagent defaults apply to delegated runs unless overridden per target agent.

Tune these carefully:

- smaller tool sets reduce risk
- shorter timeouts reduce hanging delegation runs
- lower iteration caps reduce tool loops

## Subagent overrides

Use per-agent subagent overrides only when that target agent needs different restrictions.

Example reasons:

- a research subagent needs `http.request`
- a code-only subagent needs filesystem tools but not scheduler tools

## Guardrail tips

> [!SUCCESS]
> Start with inheritance and add overrides only after observing a real need.

> [!WARNING]
> Very wide allowed tool lists or long iteration budgets make delegation harder to reason about.
