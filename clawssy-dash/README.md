# clawssy-dash

A read-only web dashboard for visualizing artifacts, agents, runs, chat sessions, and files produced by the **openclawssy** AI agent system.

Built with a FastAPI backend and a single-file HTML/CSS/JS frontend, clawssy-dash provides a dark-themed, glass-morphism UI with animated gradients, a particle canvas, and an interactive D3 force graph -- all without ever mutating the underlying data.

---

## Quick Start

### Deploy with Docker (recommended)

```bash
cd /home/mojo/projects/openclawssy/clawssy-dash
docker compose up -d --build
```

The dashboard will be available at **http://100.72.41.9:9090** over Tailscale.

### Run locally (without Docker)

```bash
pip install -r requirements.txt
python app.py
```

The app listens on `0.0.0.0:8050` by default. You will need to set the environment variables described in [Configuration](#configuration) to point at valid data directories.

### View logs

```bash
docker logs -f clawssy-dash
```

---

## Architecture

```
clawssy-dash/
  app.py                 # FastAPI backend (~1200 lines, 12 endpoints, 18+ helpers)
  static/index.html      # Single-file SPA (~3291 lines, 6 tabs, ~43 JS functions)
  requirements.txt       # Python dependencies
  Dockerfile             # python:3.12-slim base image
  docker-compose.yml     # Container orchestration with read-only volume mounts
```

### Backend

`app.py` is a single-module FastAPI application. It walks two workspace directories and the openclawssy control plane, assembling file trees, cross-references, agent metadata, run histories, chat transcripts, and scheduler state into a set of JSON API responses. Tool-role chat messages contain JSON-encoded content that the backend parses server-side to extract `tool_name`, `summary`, `output`, and `error` fields.

The large `runs.json` file (284.5 MB) is intentionally skipped. Run data is read from individual agent run directories instead.

### Frontend

`static/index.html` is a self-contained single-page application with inline CSS and JavaScript. No build step is required. The UI features a dark theme with glass-morphism effects, animated gradients, and a particle canvas background. The D3 force graph is the centerpiece, with rich interactivity including search, zoom, neighbor highlighting, floating draggable panels, and collapsible tool-call details.

### Data Flow

All data sources are mounted **read-only**. The dashboard cannot modify any data.

| Source | Container Mount | Description |
|---|---|---|
| Docker volume `openclawssy_ws_default` | `/data/workspace` | Bot-created files from the sandboxed agent environment |
| Host path `~/projects/openclawssy/workspace` | `/data/host-workspace` | Richer workspace with ~122 bot-created files (ussyflow, ussystats, journal, hourly-apps, etc.) |
| Host path `~/projects/openclawssy/.openclawssy` | `/data/controlplane` | Agents (71), runs (980), chat sessions (102), scheduler jobs (10), config, policy |

Host workspace paths use a `host:` prefix to distinguish them from agent sandbox paths across all APIs.

---

## API Reference

All endpoints are read-only (`GET`).

| Endpoint | Description |
|---|---|
| `GET /` | Serves the SPA (`index.html`) |
| `GET /api/overview` | High-level counts (files, folders, agents, runs, chats) and recent writes from both workspaces |
| `GET /api/tree` | Nested file trees for both workspaces. Host workspace paths are prefixed with `host:` |
| `GET /api/file?path=<path>` | File content, metadata, and cross-references. Use `host:` prefix for host workspace files |
| `GET /api/file-neighbors?path=<path>` | Sibling files and cross-referenced files for a given path |
| `GET /api/graph?include_runs=<bool>` | Nodes and edges for the D3 force graph. Runs excluded by default for performance (~330 nodes without, ~1310 with) |
| `GET /api/timeline` | All workspace items sorted by modification time (descending) from both workspaces |
| `GET /api/agents` | Agent list with per-agent run and chat counts |
| `GET /api/agent/{name}` | Agent detail including all runs and chat sessions |
| `GET /api/scheduler` | Scheduler jobs sourced from `jobs.json` |
| `GET /api/chat/{session_id}/messages?limit=N` | Chat messages with parsed tool-call details (`tool_name`, `summary`, `output`, `error`) |
| `GET /api/provenance` | Container, mount, config, and system information |

---

## Frontend

The SPA is organized into six tabs:

### Overview

Provenance bar, animated stat counters, and a recent activity feed.

### Graph View

Full-viewport D3 force-directed graph with:
- Rich sidebar displaying file content, neighbors, and chat messages
- Floating, draggable panels
- Collapsible tool-call details
- Node labels toggle and runs toggle
- Search, zoom controls, and legend
- Neighbor highlight on hover

The graph defaults to excluding the 980 run nodes for navigability. A toggle in the UI adds them back (~1310 total nodes).

### Timeline

Vertical timeline sorted by modification time with category-coded indicator dots.

### Scheduler

Job cards showing schedule, status, and expandable prompt content.

### Artifacts

Three-column file inspector with dual workspace trees (agent sandbox and host), markdown and code rendering, and file metadata.

### Provenance

System information cards and a full agent roster with expandable per-agent run details.

---

## Configuration

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `WORKSPACE_ROOT` | `/data/workspace` | Path to the agent sandbox workspace inside the container |
| `HOST_WORKSPACE_ROOT` | `/data/host-workspace` | Path to the host bind-mount workspace inside the container |
| `CONTROLPLANE_ROOT` | `/data/controlplane` | Path to the `.openclawssy` control plane directory |

### Docker Compose

```yaml
services:
  clawssy-dash:
    build:
      context: .
    container_name: clawssy-dash
    ports:
      - "0.0.0.0:9090:8050"
    volumes:
      - openclawssy_ws_default:/data/workspace:ro
      - /home/mojo/projects/openclawssy/workspace:/data/host-workspace:ro
      - /home/mojo/projects/openclawssy/.openclawssy:/data/controlplane:ro
    environment:
      - WORKSPACE_ROOT=/data/workspace
      - HOST_WORKSPACE_ROOT=/data/host-workspace
      - CONTROLPLANE_ROOT=/data/controlplane
    restart: unless-stopped

volumes:
  openclawssy_ws_default:
    external: true
```

The container runs on `python:3.12-slim`, listens on port 8050 internally, and is mapped to port 9090 on the host.

### Excluded Directories

The backend skips the following directories when walking workspaces:

`.venv`, `venv`, `__pycache__`, `node_modules`, `.git`, `.mypy_cache`, `.pytest_cache`, `.ruff_cache`, `.tox`, `dist`, `build`, `.eggs`, `egg-info`

---

## Development

### Dependencies

```
fastapi==0.115.0
uvicorn[standard]==0.30.6
python-multipart==0.0.9
aiofiles==24.1.0
```

Install with:

```bash
pip install -r requirements.txt
```

### Running Locally

```bash
export WORKSPACE_ROOT=/path/to/workspace
export HOST_WORKSPACE_ROOT=/path/to/host-workspace
export CONTROLPLANE_ROOT=/path/to/.openclawssy
python app.py
```

The server starts on `0.0.0.0:8050`.

### Frontend Development

No build step is needed. The frontend is a single HTML file (`static/index.html`) with inline CSS and JavaScript. Edit the file and reload the browser.

### Key Design Decisions

- **Host prefix convention**: Host workspace paths use a `host:` prefix in all API requests and responses to disambiguate them from agent sandbox paths.
- **Skipped `runs.json`**: The monolithic `runs.json` (284.5 MB) is not loaded. Run data is read from individual agent run directories for better performance and memory usage.
- **Graph performance**: Run nodes (980) are excluded from the graph by default to keep the visualization navigable at ~330 nodes. A UI toggle enables the full ~1310-node view.
- **Server-side tool parsing**: Tool-role messages in chat transcripts contain JSON-encoded content. The backend parses this to extract structured fields (`tool_name`, `summary`, `output`, `error`) so the frontend can render them cleanly.
- **Strict read-only**: All Docker volume mounts use the `:ro` flag. The dashboard has no ability to modify any data source.

---

## License

Internal / Proprietary
