# clawssy-dash Handoff Document

This document is the complete reference for any coding agent continuing work on
the clawssy-dash project. It covers architecture, data sources, every endpoint,
every frontend function, design decisions, and future work.

---

## Table of Contents

- [What This Project Is](#what-this-project-is)
- [File Structure](#file-structure)
- [Architecture Overview](#architecture-overview)
- [Data Sources](#data-sources)
- [API Endpoints](#api-endpoints)
- [Backend Helper Functions](#backend-helper-functions)
- [Frontend JavaScript Functions](#frontend-javascript-functions)
- [CSS Sections](#css-sections)
- [Key Design Decisions](#key-design-decisions)
- [The openclawssy Go Codebase (Future Features)](#the-openclawssy-go-codebase-future-features)
- [What Could Be Built Next](#what-could-be-built-next)
- [Build and Deploy](#build-and-deploy)
- [Development Without Docker](#development-without-docker)
- [Important Notes for the Next Agent](#important-notes-for-the-next-agent)

---

## What This Project Is

clawssy-dash is a read-only web dashboard for the openclawssy AI agent system.
It visualizes artifacts, agents, runs, chat sessions, scheduler jobs, and files
produced by the bot. It is deployed as a Docker container at
`http://100.72.41.9:9090` via Tailscale.

---

## File Structure

```
clawssy-dash/
  app.py              -- FastAPI backend (1200 lines, 12 endpoints, 18+ helpers)
  static/index.html   -- Single-file SPA frontend (3291 lines, 6 tabs, ~43 JS functions, ~19 CSS sections)
  requirements.txt    -- fastapi==0.115.0, uvicorn[standard]==0.30.6, python-multipart==0.0.9, aiofiles==24.1.0
  Dockerfile          -- python:3.12-slim, exposes 8050
  docker-compose.yml  -- 3 read-only volume mounts, port 9090:8050
  README.md           -- Project README
  HANDOFF.md          -- This file
```

---

## Architecture Overview

### Backend

FastAPI application. All data is read from the filesystem on each request. There
is no database and no cache. Three mount points provide data: the agent sandbox
workspace, the host workspace, and the control plane directory.

### Frontend

Single HTML file with inline `<style>` and `<script>`. Uses D3.js (loaded from
CDN) for a force-directed graph visualization. The UI uses a dark theme with
glass-morphism card styling, animated gradient backgrounds, and a particle canvas
background layer.

### Docker

- Base image: `python:3.12-slim`
- Three volumes mounted read-only
- Internal port: 8050
- External port: 9090

---

## Data Sources

All three data sources are mounted read-only inside the container.

### 1. Agent Sandbox Workspace (`/data/workspace`)

- Docker volume: `openclawssy_ws_default`
- Contains approximately 6 files and 8 folders
- Currently sparse -- the `hourly-apps/` directory is empty
- This is the workspace visible to the agent inside its sandbox container

### 2. Host Bind-Mount Workspace (`/data/host-workspace`)

- Host path: `/home/mojo/projects/openclawssy/workspace`
- Approximately 122 meaningful files after excluding `.venv`, `venv`,
  `__pycache__`, and similar directories
- Contains: `ussyflow/`, `ussystats/`, `journal/`, `hourly-apps/`, `test2/`,
  and more
- This is the RICH workspace -- the primary source of interesting file data

### 3. Control Plane (`/data/controlplane`)

- Host path: `/home/mojo/projects/openclawssy/.openclawssy`
- Contains:
  - 71 agent directories under `agents/`
  - Each agent has `runs/run_*/meta.json` (980 total runs across all agents)
  - Each agent has `memory/chats/chat_*/meta.json` + `messages.jsonl` (102
    total chat sessions across all agents)
  - `scheduler/jobs.json` -- 10 scheduler jobs
  - `config.json` -- system configuration (model: `zai/GLM-5`)
  - `runs.json` -- 284.5 MB, intentionally SKIPPED (too large to load)

---

## API Endpoints

There are 12 endpoints defined in `app.py`.

### GET /

Serves `static/index.html`. If the file is not found, returns a fallback JSON
response listing all available endpoints.

### GET /api/overview

Returns a summary of the entire system.

**Response fields:**

- `file_count` -- files in agent workspace
- `folder_count` -- folders in agent workspace
- `host_file_count` -- files in host workspace
- `host_folder_count` -- folders in host workspace
- `agent_count` -- number of agents
- `run_count` -- total runs across all agents
- `chat_session_count` -- total chat sessions across all agents
- `recent_write_count` -- files modified in the last 24 hours
- `recent_writes` -- list of up to 20 recently modified files from both
  workspaces, each with: `path`, `name`, `size`, `mtime`, `category`,
  `workspace` (`"agent"` or `"host"`)

### GET /api/tree

Returns nested directory trees for both workspaces.

**Response fields:**

- `tree` -- nested dict for the agent workspace
- `host_tree` -- nested dict for the host workspace

Each tree node has: `name`, `type` (`"file"` or `"directory"`), `path`,
`children[]`. File nodes additionally have: `size`, `mtime`, `category`. Host
tree paths are prefixed with `host:`.

### GET /api/file?path=\<path\>

Returns detailed information about a single file or directory. Path traversal is
protected.

**Path resolution:** If `path` starts with `host:`, the prefix is stripped and
the path is resolved against the host workspace. Otherwise it resolves against
the agent workspace.

**Response fields (file):**

- `path`, `name`, `type`, `size`, `mtime`, `mtime_iso`, `category`
- `content` -- file text, up to 2 MB (truncated with a note if larger)
- `references[]` -- cross-references extracted from the file content

**Response fields (directory):**

- Returns a children listing instead of content

### GET /api/file-neighbors?path=\<path\>

Returns sibling files and cross-referenced files for a given path.

**Response fields:**

- `path`
- `neighbors[]` -- each with: `path`, `name`, `size` (if sibling), `category`,
  `relation` (`"sibling"` or `"reference"`)

Siblings are files in the same directory. References are extracted from the
file's content using regex-based cross-reference extraction.

### GET /api/graph?include_runs=\<bool\> (default: false)

Returns a node-edge graph of the entire system.

**Response fields:**

- `nodes[]`, `edges[]`, `node_count`, `edge_count`

**Node types:** `folder`, `file`, `agent`, `run`, `chat_session`

**Edge types:** `contains`, `references`, `generated_by`, `temporal`,
`session_link`

**Scale:**

- Without runs: approximately 330 nodes / 299 edges
- With runs: approximately 1310 nodes / 2241 edges

**Node ID formats:**

| Type           | ID format                      |
|----------------|-------------------------------|
| File (agent)   | `ws:<relative_path>`           |
| File (host)    | `host:<relative_path>`         |
| Agent          | `agent:<name>`                 |
| Run            | `run:<agent>/<run_id>`         |
| Chat session   | `chat:<agent>/<session_id>`    |

**Node fields (common):** `id`, `label`, `type`

**Node fields (type-specific):** `path`, `size`, `category`, `mtime`,
`mtime_iso`, `run_count`, `chat_session_count`, `completed_at`, `duration_ms`,
`model`, `status`, `ghost`

### GET /api/timeline

Returns all files sorted by modification time (newest first).

**Response fields:**

- `items[]` -- each with: `path`, `name`, `size`, `mtime`, `mtime_iso`, `type`,
  `category`, `workspace`
- `total` -- total item count

### GET /api/agents

Returns a list of all agents with summary counts.

**Response fields:**

- `agents[]` -- each with: `name`, `run_count`, `chat_session_count`
- Total count

### GET /api/agent/{name}

Returns detailed information about a single agent.

**Response fields:**

- `name`, `run_count`, `chat_session_count`
- `runs[]` -- each with: `run_id`, `agent`, `completed_at`, `duration_ms`,
  `model`, `provider`, `tool_call_count`, `status`, `instance_id`, `agent_id`,
  `parent_run_id`
- `chat_sessions[]` -- each with: `session_id`, `agent`, plus all fields from
  the session's `meta.json`

### GET /api/scheduler

Returns scheduler job data.

**Response fields:**

- `jobs[]`, `total`

Handles multiple JSON structures in `jobs.json`: a dict with a `"jobs"` key, a
flat dict of jobs, or a bare array. Normalizes field names across formats:
`id` becomes `name`, `agentID` becomes `agent_id`, `lastRun` becomes `last_run`,
`message` becomes `prompt`.

### GET /api/chat/{session_id}/messages?limit=N (default: 200, max: 1000)

Returns chat messages for a given session. Searches across all agent directories
to find the matching session.

**Response fields:**

- `session_id`, `agent_id`, `channel`, `title`, `created_at`, `updated_at`
- `messages[]` -- last N messages from `messages.jsonl`
- `total_messages`, `shown_messages`

**Tool message parsing:** Messages with a `tool` role have their `content` field
parsed as JSON to extract inner fields: `tool_name`, `tool_call_id`, `summary`,
`output`, `error`. Other messages return: `role`, `content`, `ts`, `run_id`.

### GET /api/provenance

Returns system and environment metadata.

**Response fields:**

- `container` -- `hostname`, `platform`, `python_version`, `pid`, `cwd`
- `mounts` -- root paths, accessibility flags, file counts for each mount
- `controlplane` -- safe config fields, `agent_count`, scheduler info,
  `policy_files`, `runs.json` size

---

## Backend Helper Functions

All helpers are defined in `app.py`.

| Function | Description |
|---|---|
| `_safe_stat(p)` | Returns `os.stat` result or `None` on error |
| `_safe_read_json(p)` | Parses a JSON file into a dict, returns `None` on error |
| `_safe_read_text(p, max_bytes=2MB)` | Reads file text with truncation; appends a note if truncated |
| `_relpath(p, root)` | Returns a POSIX-style relative path |
| `_classify_file(name)` | Categorizes a file by extension: `markdown`, `python`, `text`, `json`, `yaml`, `shell`, `html`, `css`, `javascript`, `image`, `other` |
| `_build_tree(root)` | Builds a nested directory tree dict for the agent workspace |
| `_build_host_tree()` | Builds a nested directory tree dict for the host workspace, with `host:` prefix on all paths |
| `_iter_agents()` | Scans all agent directories and yields agent info with run/chat counts |
| `_collect_runs(agent_dir)` | Collects all `meta.json` files from an agent's `runs/run_*/` directories |
| `_collect_chats(agent_dir)` | Collects all `meta.json` files from an agent's `memory/chats/chat_*/` directories |
| `_extract_references(filepath, root)` | Extracts cross-references from a file using regex patterns: markdown links, bare paths, Python imports, Python file path strings |
| `_walk_workspace()` | Walks the agent workspace, returning lists of (files, folders) |
| `_walk_host_workspace()` | Walks the host workspace, returning entries with `host:` prefix |

**Excluded directories constant:** `_EXCLUDED_DIRS` contains: `.venv`, `venv`,
`__pycache__`, `node_modules`, `.git`, `.mypy_cache`, `.pytest_cache`,
`.ruff_cache`, `.tox`, `dist`, `build`, `.eggs`, `egg-info`

---

## Frontend JavaScript Functions

There are approximately 43 JavaScript functions defined inline in
`static/index.html`.

### Utility Functions

| Function | Description |
|---|---|
| `formatBytes(bytes)` | Converts byte count to human-readable string (KB, MB, etc.) |
| `timeAgo(dateStr)` | Returns a relative time string ("3 hours ago", "2 days ago") |
| `formatDate(dateStr)` | Formats a date string for display |
| `fileIcon(name, type)` | Returns an appropriate icon character for a file or folder |
| `inferCategory(name, type)` | Infers file category from name/extension |
| `categoryColor(cat)` | Returns the CSS color associated with a file category |
| `fetchJSON(url)` | Wrapper around `fetch()` with JSON parsing |
| `animateCounter(el, target, duration)` | Animates a number counting up from 0 to target |
| `escapeHtml(str)` | Escapes HTML special characters to prevent XSS |

### Navigation Functions

| Function | Description |
|---|---|
| `moveIndicator(btn)` | Animates the tab indicator bar to the active tab button |
| `switchTab(name)` | Switches between the 6 main tabs |
| `loadPanelData(name)` | Loads data for the currently active tab |

### Fallback Generator Functions

These generate static sample data so the UI still renders when the API is
unreachable:

| Function | Description |
|---|---|
| `generateFallbackOverview()` | Static overview data |
| `generateFallbackGraph()` | Static graph nodes and edges |
| `generateFallbackTimeline()` | Static timeline items |
| `generateFallbackTree()` | Static directory tree |
| `generateFallbackAgents()` | Static agent list |
| `generateFallbackProvenance()` | Static provenance data |
| `generateFallbackFile(path)` | Static file content for a given path |

### Main Panel Loader Functions

| Function | Description |
|---|---|
| `loadOverview()` | Renders the overview tab with animated stat counters and recent activity feed |
| `loadGraph(includeRuns)` | Renders the D3 force-directed graph with full sidebar interaction |
| `loadTimeline()` | Renders the timeline view |
| `loadInspector()` | Renders the artifacts tab with dual directory trees and file viewer |
| `loadFile(path)` | Loads and renders file content into the artifacts panel |
| `loadProvenance()` | Renders provenance info and the agent roster |
| `loadScheduler()` | Renders scheduler job cards |

### Floating Panel Functions

| Function | Description |
|---|---|
| `openFloatingPanel(path)` | Creates a draggable floating file viewer panel |
| `loadFloatingPanelContent(path, panel)` | Fetches and renders file content inside a floating panel |
| `openFloatingChatPanel(sessionId)` | Creates a draggable floating chat viewer panel |
| `loadFloatingChatContent(sessionId, panel)` | Fetches and renders chat messages inside a floating panel |
| `makeDraggable(el, handle)` | Adds drag functionality to a floating panel element |

### Graph Sidebar Helper Functions

| Function | Description |
|---|---|
| `buildSidebarMetadata(d, edges)` | Builds a rich metadata view for the selected graph node |
| `loadSidebarFileContent(path, container)` | Loads file content into the sidebar |
| `loadSidebarNeighbors(path, container)` | Loads neighbor files into the sidebar |
| `renderChatMessage(msg)` | Renders an individual chat message with collapsible tool call details |
| `loadSidebarChat(sessionId, container)` | Loads a full chat session into the sidebar |

### Tree Rendering

| Function | Description |
|---|---|
| `renderTree(container, node, depth)` | Recursively renders a directory tree with expand/collapse |

### Other

| Function | Description |
|---|---|
| `togglePrompt(idx)` | Expands or collapses scheduler job prompt text |

---

## CSS Sections

There are approximately 19 CSS sections defined inline in `static/index.html`.

1. **Reset and Base** -- CSS custom properties (theme colors, fonts), box-sizing
   reset, body defaults
2. **Animated Gradient Background** -- Full-viewport shifting gradient animation
3. **Particle Canvas** -- Positioned canvas element for animated background
   particles
4. **Header / Nav** -- Top navigation bar with tab buttons and sliding indicator
5. **Main Content Area** -- Panel containers, tab visibility switching
6. **Glass Cards** -- Glass-morphism card style with backdrop blur and subtle
   borders
7. **Stat Counters** -- Animated number display cards for overview stats
8. **Activity Feed** -- Recent writes list styling
9. **Timeline** -- Vertical timeline with colored dots and connecting lines
10. **Scheduler Cards** -- Job display cards with status indicators
11. **Graph View** -- Full-viewport SVG container for the D3 graph
12. **Graph Sidebar** -- Right-side panel for graph node details
13. **Graph Controls** -- Button bar, toggles, and search input for the graph
14. **Graph Legend** -- Color-coded legend for node types
15. **Floating Panels** -- Absolutely positioned draggable file/chat viewer
    panels
16. **Artifacts / Inspector** -- Three-column layout: host tree, agent tree,
    file viewer
17. **File Viewer** -- Markdown and code content rendering styles
18. **Provenance** -- System information card layout
19. **Scrollbars / Misc** -- Custom scrollbar styling and miscellaneous
    utilities

---

## Key Design Decisions

### The `host:` prefix system

Host workspace files use a `host:` prefix in all paths returned by the API. The
frontend passes these prefixed paths directly to `/api/file?path=host:...`. The
backend checks for the prefix, strips it, and resolves the path against the host
workspace root. This convention is used consistently across every endpoint that
deals with file paths.

### No database or cache

Everything is read from the filesystem on each request. This keeps the dashboard
truly read-only and guarantees that displayed data is always current. The
trade-off is higher I/O per request, which is acceptable at the current scale.

### `runs.json` is intentionally skipped

At 284.5 MB, loading this file would kill performance. Run data is instead read
from individual `agents/*/runs/run_*/meta.json` files, which are small and fast
to read.

### Graph renders without runs by default

The default graph (~330 nodes) is navigable and interactive. Enabling runs
expands it to ~1310 nodes, which clutters the visualization. A toggle in the UI
allows the user to include runs when desired.

### Tool message parsing

Chat messages with a `tool` role store their `content` as a JSON-encoded string.
The backend parses this JSON to extract inner fields (`tool_name`,
`tool_call_id`, `summary`, `output`, `error`) so the frontend can render
structured, collapsible tool call detail blocks.

### Single HTML file for the frontend

No build tooling is required. All CSS and JS are inline. D3.js is loaded from a
CDN. This was a deliberate choice to keep deployment simple and avoid a frontend
build step.

### Opaque overlay backgrounds

Sidebar and floating panel backgrounds use `rgba(10,10,18,0.92)` to maintain
text readability over the animated gradient and particle background.

---

## The openclawssy Go Codebase (Future Features)

The main openclawssy application is a Go binary with extensive management APIs
that the dashboard could expose in future iterations. Understanding the Go
codebase is relevant for any feature that goes beyond read-only file browsing.

### Available Go APIs

- **50+ registered tools** in `internal/tools/`
- **60+ HTTP endpoints** in `internal/channels/dashboard/handler.go`

### Endpoint categories in the Go app

| Category | Description |
|---|---|
| Agent CRUD | Create, list, delete, configure agents |
| Scheduler management | Create, update, delete, toggle jobs |
| Run management | List, detail, cancel runs |
| Chat/memory management | Create sessions, send messages |
| Config management | Get and set system configuration |
| Secret management | Store and retrieve secrets |
| Policy management | Capabilities, path guards |
| Skill management | Register and list skills |
| Prompt stack management | Manage prompt stacks |
| Role management | Manage roles |
| Eval/testing endpoints | Evaluation and testing |
| Instance management | Manage instances |
| Wizard/setup endpoints | Setup wizards |
| Contract management | Manage contracts |

The Go app listens on its own port (typically 8080) in the
`openclawssy-openclawssy-1` container. If the dashboard evolves beyond
read-only, it could proxy requests to this API.

---

## What Could Be Built Next

### High Value Additions

1. **Real-time updates** -- WebSocket or SSE connection to detect new files and
   runs, auto-refreshing panels without manual reload.

2. **Search** -- Full-text search across all workspace files and chat messages.

3. **Agent management UI** -- Proxy requests to the Go app's agent CRUD
   endpoints to create and configure agents from the dashboard.

4. **Scheduler management** -- Create, edit, delete, and toggle scheduler jobs
   through the dashboard UI.

5. **Chat interface** -- Send messages to agents through the dashboard. This
   requires write access to the control plane.

6. **Run inspector** -- Detailed view of individual runs showing tool calls,
   timing breakdowns, and model usage statistics.

7. **Diff view** -- Show file changes over time using git history or
   mtime-based versioning.

### Medium Value

8. **Caching layer** -- Add Redis or in-memory cache for expensive operations
   such as agent scanning and graph building.

9. **Pagination** -- Timeline and agent list endpoints could use cursor-based or
   offset pagination for better performance at scale.

10. **File upload** -- Upload files to the workspace through the dashboard.

11. **Graph filtering** -- Filter the graph by date range, file type, agent, or
    project folder.

12. **Export** -- Export the graph as SVG or PNG, export the timeline as CSV.

### Polish

13. **Responsive design** -- Mobile-friendly layout adjustments.

14. **Keyboard shortcuts** -- Navigate tabs, trigger search, close panels with
    keyboard bindings.

15. **Breadcrumbs** -- Navigation breadcrumbs in the artifacts/inspector view.

16. **Dark/light theme toggle** -- The dashboard is currently dark-only.

17. **Error boundaries** -- Better error handling in the frontend when the API
    is down or returns unexpected data.

---

## Build and Deploy

### Docker (production)

```bash
# Build and start the container
cd /home/mojo/projects/openclawssy/clawssy-dash
docker compose up -d --build

# View logs
docker logs -f clawssy-dash

# Rebuild after changes
docker compose up -d --build

# Stop
docker compose down
```

The dashboard is accessible at `http://100.72.41.9:9090` via Tailscale.

### Docker details

- The Dockerfile uses `python:3.12-slim` as the base image
- Internal port is 8050
- External port is 9090 (mapped in `docker-compose.yml`)
- Three volumes are mounted read-only:
  - `openclawssy_ws_default` at `/data/workspace`
  - `/home/mojo/projects/openclawssy/workspace` at `/data/host-workspace`
  - `/home/mojo/projects/openclawssy/.openclawssy` at `/data/controlplane`

---

## Development Without Docker

```bash
pip install -r requirements.txt
export WORKSPACE_ROOT=/path/to/agent/workspace
export HOST_WORKSPACE_ROOT=/path/to/host/workspace
export CONTROLPLANE_ROOT=/path/to/.openclawssy
python app.py  # Starts on 0.0.0.0:8050
```

Replace the paths with the actual locations of your data directories.

---

## Important Notes for the Next Agent

- **Read-only is a hard constraint.** All volumes are mounted with `:ro`. Do not
  add write endpoints without an explicit user request to change this.

- **The `host:` prefix convention is load-bearing.** Both the backend and
  frontend depend on it for routing file requests to the correct workspace. Do
  not change this without updating both sides.

- **The frontend is a single file by design.** If it grows significantly beyond
  its current 3291 lines, consider splitting into separate JS and CSS files, but
  do not do this preemptively.

- **Fallback generators exist for offline resilience.** When the API is
  unreachable, the fallback functions generate static sample data so the UI
  still renders in a useful state. Maintain this pattern if adding new tabs.

- **D3 force simulation is tuned for ~330 nodes.** The current force
  parameters, charge strengths, and link distances are calibrated for the
  default graph size. If the data grows significantly, consider WebGL-based
  rendering (e.g., force-graph library) or node clustering.

- **Tool messages use collapsible elements.** Chat tool call messages render
  with `<details>/<summary>` HTML elements. Errors within tool calls are
  highlighted in red.

- **The graph sidebar is interactive.** Clicking a node in the graph loads file
  content, neighbor files, and chat messages into the right sidebar. Floating
  panels allow side-by-side comparison of multiple documents.

- **`runs.json` must not be loaded.** At 284.5 MB, it will cause memory and
  performance issues. Run data is sourced from individual per-run `meta.json`
  files instead.

- **The `_EXCLUDED_DIRS` set must be maintained.** Virtual environments, caches,
  and build artifacts are excluded from directory walks. If new exclusion
  patterns are needed, add them to this set rather than filtering elsewhere.
