# Dash Handoff

## Goal

Build a read-only dashboard that shows the agent workspace, all generated artifacts, and how items are linked together.

## What I found

- Running agent container: `openclawssy_agent_default`
- Agent workspace inside container: `/workspace`
- Host volume backing it: Docker volume `openclawssy_ws_default`
- App/control-plane container: `openclawssy-openclawssy-1`
- Control-plane bind mount: `/home/mojo/projects/openclawssy/.openclawssy` -> `/app/.openclawssy`
- App workspace bind mount: `/home/mojo/projects/openclawssy/workspace` -> `/app/workspace`

## Workspace inventory seen so far

Top-level `/workspace` contents:

- `final.txt`
- `test-isolation.txt`
- `hourly-apps/`
- `journal/`
- `test2/`
- `ussyflow/`
- `ussystats/`

Useful artifacts already confirmed:

- `ussystats/SPEC.md`
- `ussystats/ussystats.py`
- `test2/journal.txt`
- `test2/ussyring-report.md`
- `journal/entries/`
- `journal/summaries/`

## Important dashboard sources

Read these as the main data sources:

- Workspace tree under `/workspace`
- Markdown/text/code files in the workspace
- `.openclawssy/scheduler/jobs.json`
- `.openclawssy/agents/**/memory/chats/**/meta.json`
- `.openclawssy/agents/**/memory/chats/**/messages.jsonl`
- `.openclawssy/config/config.json`
- `.openclawssy/runs/runs.json`

## Link model to build

Show relationships using these edge types:

- `contains`: directory -> file/folder
- `references`: one file mentions another path or artifact name
- `generated_by`: artifact -> agent run / scheduler job / message
- `temporal`: older -> newer based on file timestamps
- `session_link`: chat session -> messages / inbox items
- `job_link`: scheduler job -> output/session/message target

## Recommended UX

1. Overview
   - counts of folders, files, journals, reports, scripts, and markdown docs
   - recent writes
   - active container/workspace path

2. Graph view
   - folder/file nodes
   - clickable backlinks and outbound links
   - cluster by project folder (`journal`, `ussystats`, `ussyflow`, `test2`, `hourly-apps`)

3. Timeline
   - newest files first
   - show write time, size, and inferred category

4. Artifact inspector
   - render markdown nicely
   - syntax-highlight code
   - show raw metadata side panel

5. Provenance panel
   - show discovered container, mount, and control-plane context
   - show which agent/session/job likely created or touched the item

## Implementation plan

1. Add a workspace indexer
   - walk `/workspace` read-only
   - collect path, type, size, mtime, parent folder
   - cap recursion or stream results for large trees

2. Add reference extraction
   - scan markdown for links and path mentions
   - scan code for imports and obvious file references
   - keep results heuristic and non-destructive

3. Add provenance enrichment
   - join workspace items to `.openclawssy` scheduler/chat/run data when names or timestamps match
   - keep this as best-effort metadata, not hard truth

4. Add graph-friendly API output
   - nodes: files, folders, jobs, sessions, messages, runs
   - edges: containment, references, chronology, provenance

5. Add a polished UI
   - tree sidebar
   - graph canvas
   - timeline panel
   - detail drawer

## Safety rules

- Do not delete, rename, or rewrite workspace files.
- Do not touch Docker state unless strictly needed for inspection.
- Treat all data as read-only unless the user explicitly asks for edits.
- Prefer listing and linking over mutating.

## Best next search targets

- `/workspace/journal/**`
- `/workspace/ussystats/**`
- `/workspace/test2/**`
- `/workspace/ussyflow/**`
- `/workspace/hourly-apps/**`
- `.openclawssy/scheduler/jobs.json`
- `.openclawssy/agents/**/memory/chats/**`

## Notes for the next agent

- The key container to inspect is `openclawssy_agent_default`.
- The agent workspace is `/workspace` and is separate from the repo checkout.
- The dashboard should present the workspace as a living artifact graph, not just a file browser.
- Most valuable first win: index everything under `/workspace`, then add backlinks and a timeline.
