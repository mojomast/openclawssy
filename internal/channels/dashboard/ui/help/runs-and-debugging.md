---
id: runs-and-debugging
title: Runs & Debugging
category: Debugging
keywords: runs debugging traces tool failures logs diagnostics errors retry run history
related_topics: scheduler-guide, providers-and-models, faq
route_hints: /runs, /chat, /help
---

# Runs & Debugging

## How runs work

Each request produces a run record with status, timestamps, model identity, and tool execution details.

## Where to look first

1. Open `Runs`
2. Filter to the failing status if needed
3. Open the run
4. Inspect:
   - run status
   - model identity
   - trace data
   - tool failures

## Common failure patterns

- invalid tool input
- missing provider key
- model or provider misconfiguration
- sandbox or shell restrictions
- network allowlist problems

## Debugging checklist

- confirm the global provider and model are correct
- confirm agent overrides are not changing the effective model unexpectedly
- confirm any required secrets exist
- inspect tool failures in the timeline
- compare current config with the run's captured model identity

## Where logs live

The dashboard focuses on run traces and tool results. For deeper runtime investigation, combine run details with server logs from your local or deployed Openclawssy process.
