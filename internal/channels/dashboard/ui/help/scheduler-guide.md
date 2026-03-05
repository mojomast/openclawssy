---
id: scheduler-guide
title: Scheduler
category: Scheduler
keywords: scheduler jobs catch_up recurring one-shot pause resume delete troubleshooting
related_topics: runs-and-debugging, custom-dashboards, faq
route_hints: /scheduler, /help
---

# Scheduler

## Jobs overview

The scheduler runs saved automation jobs on a recurring interval or one-shot timestamp.

## Important fields

- `schedule`
- `message`
- `agent_id`
- `enabled`

## What `catch_up` means

If catch-up is enabled, jobs missed while the service was down may run after the scheduler resumes.

## Troubleshooting checklist

- confirm the scheduler is globally running
- confirm the job itself is enabled
- confirm the target agent is valid
- confirm the message still makes sense with current config and policies
- inspect resulting runs in the Runs page

## Good operator habits

- start with low-frequency schedules
- verify one successful run before enabling many jobs
- use clear job ids and descriptive messages
