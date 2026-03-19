"""
openclawssy artifact dashboard – FastAPI backend.

Serves a read-only view of the agent workspace and control-plane metadata
produced by the openclawssy AI agent system.

Data sources (both mounted read-only in the container):
  /data/workspace      – agent workspace volume (WORKSPACE_ROOT)
  /data/controlplane   – .openclawssy control-plane directory (CONTROLPLANE_ROOT)

Listens on port 8050.  CORS enabled for all origins.
"""

from __future__ import annotations

import json
import logging
import os
import platform
import re
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from fastapi import FastAPI, HTTPException, Query, Request
from fastapi import Path as PathParam
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import FileResponse, JSONResponse
from fastapi.staticfiles import StaticFiles

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

WORKSPACE_ROOT = Path(os.environ.get("WORKSPACE_ROOT", "/data/workspace"))
HOST_WORKSPACE_ROOT = Path(os.environ.get("HOST_WORKSPACE_ROOT", "/data/host-workspace"))
CONTROLPLANE_ROOT = Path(os.environ.get("CONTROLPLANE_ROOT", "/data/controlplane"))
STATIC_DIR = Path("/app/static")

logger = logging.getLogger("clawssy-dash")
logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")

# Directories to exclude from workspace walking and tree building.
# These are tooling artifacts, not bot-created content.
_EXCLUDED_DIRS: set[str] = {
    ".venv", "venv", "__pycache__", "node_modules", ".git",
    ".mypy_cache", ".pytest_cache", ".ruff_cache", ".tox",
    "dist", "build", ".eggs", "egg-info",
}

# ---------------------------------------------------------------------------
# App setup
# ---------------------------------------------------------------------------

app = FastAPI(
    title="openclawssy artifact dashboard",
    description="Read-only dashboard for openclawssy workspace and control-plane artifacts.",
    version="1.0.0",
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# ---------------------------------------------------------------------------
# Helpers – filesystem indexing
# ---------------------------------------------------------------------------


def _safe_stat(p: Path) -> os.stat_result | None:
    """Return stat for *p* or None if inaccessible."""
    try:
        return p.stat()
    except (OSError, PermissionError):
        return None


def _safe_read_json(p: Path) -> dict[str, Any] | None:
    """Return parsed JSON dict from *p*, or None on any failure."""
    try:
        text = p.read_text(encoding="utf-8", errors="replace")
        data = json.loads(text)
        return data if isinstance(data, dict) else None
    except Exception:
        return None


def _safe_read_text(p: Path, max_bytes: int = 2 * 1024 * 1024) -> str | None:
    """Return text content of *p* (up to *max_bytes*), or None."""
    try:
        size = p.stat().st_size
        if size > max_bytes:
            with p.open("r", encoding="utf-8", errors="replace") as fh:
                return fh.read(max_bytes) + f"\n\n… [truncated at {max_bytes} bytes, total {size}]"
        return p.read_text(encoding="utf-8", errors="replace")
    except Exception:
        return None


def _relpath(p: Path, root: Path) -> str:
    """Return the POSIX relative path of *p* under *root*."""
    try:
        return p.relative_to(root).as_posix()
    except ValueError:
        return p.as_posix()


def _classify_file(name: str) -> str:
    """Return a human-readable category for the file."""
    lower = name.lower()
    if lower.endswith((".md", ".markdown")):
        return "markdown"
    if lower.endswith(".py"):
        return "python"
    if lower.endswith(".txt"):
        return "text"
    if lower.endswith(".json"):
        return "json"
    if lower.endswith((".yaml", ".yml")):
        return "yaml"
    if lower.endswith((".sh", ".bash")):
        return "shell"
    if lower.endswith((".html", ".htm")):
        return "html"
    if lower.endswith((".css",)):
        return "css"
    if lower.endswith((".js", ".ts", ".jsx", ".tsx")):
        return "javascript"
    if lower.endswith((".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp")):
        return "image"
    return "other"


# ---------------------------------------------------------------------------
# Helpers – workspace tree building
# ---------------------------------------------------------------------------


def _build_tree(root: Path) -> dict[str, Any]:
    """Build a nested dict representing the workspace file tree."""
    if not root.is_dir():
        return {"name": root.name, "type": "directory", "children": [], "path": ""}

    def _walk(dirpath: Path, rel: str) -> dict[str, Any]:
        node: dict[str, Any] = {
            "name": dirpath.name or str(root),
            "type": "directory",
            "path": rel,
            "children": [],
        }
        try:
            entries = sorted(dirpath.iterdir(), key=lambda e: (not e.is_dir(), e.name.lower()))
        except (OSError, PermissionError):
            return node

        for entry in entries:
            if entry.is_dir() and entry.name in _EXCLUDED_DIRS:
                continue
            entry_rel = f"{rel}/{entry.name}" if rel else entry.name
            if entry.is_dir():
                node["children"].append(_walk(entry, entry_rel))
            else:
                st = _safe_stat(entry)
                node["children"].append({
                    "name": entry.name,
                    "type": "file",
                    "path": entry_rel,
                    "size": st.st_size if st else 0,
                    "mtime": st.st_mtime if st else 0,
                    "category": _classify_file(entry.name),
                })
        return node

    return _walk(root, "")


def _build_host_tree() -> dict[str, Any]:
    """Build a nested dict representing the host workspace file tree.

    All path values are prefixed with ``host:`` so they can be passed
    directly to ``/api/file?path=…`` without client-side munging.
    """
    tree = _build_tree(HOST_WORKSPACE_ROOT)
    tree["name"] = "host-workspace"

    def _prefix_paths(node: dict[str, Any]) -> None:
        p = node.get("path", "")
        if p and not p.startswith("host:"):
            node["path"] = f"host:{p}"
        for child in node.get("children", []):
            _prefix_paths(child)

    _prefix_paths(tree)
    return tree


# ---------------------------------------------------------------------------
# Helpers – control-plane agent scanning
# ---------------------------------------------------------------------------


def _iter_agents() -> list[dict[str, Any]]:
    """Return metadata for every agent under the control-plane agents/ dir."""
    agents_dir = CONTROLPLANE_ROOT / "agents"
    if not agents_dir.is_dir():
        return []

    result: list[dict[str, Any]] = []
    try:
        agent_names = sorted(d.name for d in agents_dir.iterdir() if d.is_dir())
    except (OSError, PermissionError):
        return []

    for name in agent_names:
        agent_dir = agents_dir / name
        runs = _collect_runs(agent_dir)
        chats = _collect_chats(agent_dir)
        result.append({
            "name": name,
            "run_count": len(runs),
            "chat_session_count": len(chats),
            "runs": runs,
            "chat_sessions": chats,
        })
    return result


def _collect_runs(agent_dir: Path) -> list[dict[str, Any]]:
    """Collect run metadata from agent_dir/runs/run_*/meta.json."""
    runs_dir = agent_dir / "runs"
    if not runs_dir.is_dir():
        return []
    result: list[dict[str, Any]] = []
    try:
        run_dirs = sorted(runs_dir.iterdir(), key=lambda d: d.name)
    except (OSError, PermissionError):
        return []

    for rd in run_dirs:
        if not rd.is_dir():
            continue
        meta_path = rd / "meta.json"
        meta = _safe_read_json(meta_path)
        entry: dict[str, Any] = {
            "run_id": rd.name,
            "agent": agent_dir.name,
        }
        if meta:
            entry.update({
                "completed_at": meta.get("completed_at"),
                "duration_ms": meta.get("duration_ms"),
                "model": meta.get("model"),
                "provider": meta.get("provider"),
                "tool_call_count": meta.get("tool_call_count"),
                "status": meta.get("status"),
                "instance_id": meta.get("instance_id"),
                "agent_id": meta.get("agent_id"),
                "parent_run_id": meta.get("parent_run_id"),
            })
        result.append(entry)
    return result


def _collect_chats(agent_dir: Path) -> list[dict[str, Any]]:
    """Collect chat session metadata from agent_dir/memory/chats/chat_*/meta.json."""
    chats_dir = agent_dir / "memory" / "chats"
    if not chats_dir.is_dir():
        return []
    result: list[dict[str, Any]] = []
    try:
        chat_dirs = sorted(chats_dir.iterdir(), key=lambda d: d.name)
    except (OSError, PermissionError):
        return []

    for cd in chat_dirs:
        if not cd.is_dir():
            continue
        meta_path = cd / "meta.json"
        meta = _safe_read_json(meta_path)
        entry: dict[str, Any] = {
            "session_id": cd.name,
            "agent": agent_dir.name,
        }
        if meta:
            entry.update(meta)
        result.append(entry)
    return result


# ---------------------------------------------------------------------------
# Helpers – cross-reference / graph extraction
# ---------------------------------------------------------------------------

# Markdown link: [text](path)
_RE_MD_LINK = re.compile(r"\[([^\]]*)\]\(([^)]+)\)")

# Bare path mentions: /workspace/... or ./... (at least 2 path segments)
_RE_BARE_PATH = re.compile(r"(?:(?:/workspace|\.)/[\w./_-]+)")

# Python import: import foo  /  from foo import bar
_RE_PY_IMPORT = re.compile(r"^\s*(?:from\s+([\w.]+)\s+import|import\s+([\w.]+))", re.MULTILINE)

# Python file path strings: open("..."), Path("..."), or bare quoted paths
_RE_PY_PATH = re.compile(r"""(?:open|Path)\s*\(\s*['"]([^'"]+)['"]\s*\)|['"]((?:\./|/)\S+?)['"]""")


def _extract_references(filepath: Path, root: Path) -> list[dict[str, str]]:
    """Extract cross-references from a file to other files/paths."""
    refs: list[dict[str, str]] = []
    text = _safe_read_text(filepath, max_bytes=512 * 1024)
    if not text:
        return refs

    source_rel = _relpath(filepath, root)
    name_lower = filepath.name.lower()

    seen: set[str] = set()

    def _add(target: str, kind: str) -> None:
        # Normalize target: strip anchors, query strings
        target = target.split("#")[0].split("?")[0].strip()
        if not target or target in seen:
            return
        # Skip URLs
        if target.startswith(("http://", "https://", "mailto:", "ftp://")):
            return
        seen.add(target)
        refs.append({"source": source_rel, "target": target, "type": kind})

    if name_lower.endswith((".md", ".markdown")):
        for match in _RE_MD_LINK.finditer(text):
            _add(match.group(2), "md_link")
        for match in _RE_BARE_PATH.finditer(text):
            _add(match.group(0), "path_mention")

    elif name_lower.endswith(".py"):
        for match in _RE_PY_IMPORT.finditer(text):
            module = match.group(1) or match.group(2)
            if module:
                _add(module.replace(".", "/") + ".py", "import")
        for match in _RE_PY_PATH.finditer(text):
            path_str = match.group(1) or match.group(2)
            if path_str:
                _add(path_str, "file_ref")
    else:
        # Generic: look for bare path mentions
        for match in _RE_BARE_PATH.finditer(text):
            _add(match.group(0), "path_mention")

    return refs


# ---------------------------------------------------------------------------
# Helpers – workspace walking utilities
# ---------------------------------------------------------------------------


def _walk_workspace() -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    """Walk the workspace and return (files, folders) metadata lists."""
    files: list[dict[str, Any]] = []
    folders: list[dict[str, Any]] = []

    if not WORKSPACE_ROOT.is_dir():
        return files, folders

    for dirpath_str, dirnames, filenames in os.walk(str(WORKSPACE_ROOT)):
        dirpath = Path(dirpath_str)
        rel = _relpath(dirpath, WORKSPACE_ROOT)
        if rel != ".":
            st = _safe_stat(dirpath)
            folders.append({
                "path": rel,
                "name": dirpath.name,
                "mtime": st.st_mtime if st else 0,
            })

        # Sort for determinism and skip excluded directories
        dirnames[:] = sorted(d for d in dirnames if d not in _EXCLUDED_DIRS)
        filenames.sort()

        for fn in filenames:
            fp = dirpath / fn
            st = _safe_stat(fp)
            frel = _relpath(fp, WORKSPACE_ROOT)
            files.append({
                "path": frel,
                "name": fn,
                "size": st.st_size if st else 0,
                "mtime": st.st_mtime if st else 0,
                "category": _classify_file(fn),
            })

    return files, folders


def _walk_host_workspace() -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    """Walk the host workspace and return (files, folders) with host: prefixed paths."""
    files: list[dict[str, Any]] = []
    folders: list[dict[str, Any]] = []

    if not HOST_WORKSPACE_ROOT.is_dir():
        return files, folders

    for dirpath_str, dirnames, filenames in os.walk(str(HOST_WORKSPACE_ROOT)):
        dirpath = Path(dirpath_str)
        rel = _relpath(dirpath, HOST_WORKSPACE_ROOT)
        if rel != ".":
            st = _safe_stat(dirpath)
            folders.append({
                "path": f"host:{rel}",
                "name": dirpath.name,
                "mtime": st.st_mtime if st else 0,
            })

        # Sort for determinism and skip excluded directories
        dirnames[:] = sorted(d for d in dirnames if d not in _EXCLUDED_DIRS)
        filenames.sort()

        for fn in filenames:
            fp = dirpath / fn
            st = _safe_stat(fp)
            frel = _relpath(fp, HOST_WORKSPACE_ROOT)
            files.append({
                "path": f"host:{frel}",
                "name": fn,
                "size": st.st_size if st else 0,
                "mtime": st.st_mtime if st else 0,
                "category": _classify_file(fn),
            })

    return files, folders


# ---------------------------------------------------------------------------
# API Routes
# ---------------------------------------------------------------------------


@app.get("/api/overview")
def api_overview() -> JSONResponse:
    """High-level counts: files, folders, agents, runs, chat sessions, recent writes."""
    files, folders = _walk_workspace()
    host_files, host_folders = _walk_host_workspace()
    agents = _iter_agents()

    total_runs = sum(a["run_count"] for a in agents)
    total_chats = sum(a["chat_session_count"] for a in agents)

    # Recent writes: files modified in the last 24 hours, from both workspaces
    cutoff = time.time() - 86400

    agent_recent = [dict(f, workspace="agent") for f in files if f["mtime"] >= cutoff]
    host_recent = [dict(f, workspace="host") for f in host_files if f["mtime"] >= cutoff]
    combined_recent = sorted(agent_recent + host_recent, key=lambda f: f["mtime"], reverse=True)[:20]

    return JSONResponse({
        "file_count": len(files),
        "folder_count": len(folders),
        "host_file_count": len(host_files),
        "host_folder_count": len(host_folders),
        "agent_count": len(agents),
        "run_count": total_runs,
        "chat_session_count": total_chats,
        "recent_write_count": len(combined_recent),
        "recent_writes": combined_recent,
        "workspace_root": str(WORKSPACE_ROOT),
        "host_workspace_root": str(HOST_WORKSPACE_ROOT),
        "controlplane_root": str(CONTROLPLANE_ROOT),
    })


@app.get("/api/tree")
def api_tree() -> JSONResponse:
    """Full nested file tree of both workspaces."""
    tree = _build_tree(WORKSPACE_ROOT)
    host_tree = _build_host_tree()
    return JSONResponse({"tree": tree, "host_tree": host_tree})


@app.get("/api/file")
def api_file(path: str = Query(..., description="Relative path within workspace (prefix with host: for host workspace)")) -> JSONResponse:
    """Return content + metadata for a single workspace file."""
    # Determine which workspace to resolve against
    if path.startswith("host:"):
        raw_path = path[len("host:"):]
        root = HOST_WORKSPACE_ROOT
    else:
        raw_path = path
        root = WORKSPACE_ROOT

    # Sanitize: prevent path traversal
    clean = Path(raw_path)
    if clean.is_absolute():
        # Strip leading slash so it becomes relative
        clean = Path(str(clean).lstrip("/"))

    # Resolve against the appropriate workspace root and ensure it stays within
    target = (root / clean).resolve()
    root_resolved = root.resolve()

    if not str(target).startswith(str(root_resolved)):
        raise HTTPException(status_code=403, detail="Path traversal denied.")

    if not target.exists():
        raise HTTPException(status_code=404, detail=f"File not found: {path}")

    if target.is_dir():
        # Return directory listing
        children: list[dict[str, Any]] = []
        try:
            for entry in sorted(target.iterdir(), key=lambda e: (not e.is_dir(), e.name.lower())):
                st = _safe_stat(entry)
                children.append({
                    "name": entry.name,
                    "type": "directory" if entry.is_dir() else "file",
                    "path": _relpath(entry, root),
                    "size": st.st_size if st and not entry.is_dir() else 0,
                    "mtime": st.st_mtime if st else 0,
                })
        except (OSError, PermissionError):
            pass
        return JSONResponse({
            "path": _relpath(target, root),
            "type": "directory",
            "children": children,
        })

    st = _safe_stat(target)
    content = _safe_read_text(target)

    return JSONResponse({
        "path": _relpath(target, root),
        "name": target.name,
        "type": "file",
        "size": st.st_size if st else 0,
        "mtime": st.st_mtime if st else 0,
        "mtime_iso": datetime.fromtimestamp(st.st_mtime, tz=timezone.utc).isoformat() if st else None,
        "category": _classify_file(target.name),
        "content": content,
        "references": _extract_references(target, root),
    })


@app.get("/api/file-neighbors")
def api_file_neighbors(path: str = Query(..., description="Relative path (prefix with host: for host workspace)")) -> JSONResponse:
    """Return files related to the given path: same directory siblings + cross-ref targets."""
    if path.startswith("host:"):
        raw_path = path[len("host:"):]
        root = HOST_WORKSPACE_ROOT
        prefix = "host:"
    else:
        raw_path = path
        root = WORKSPACE_ROOT
        prefix = ""

    clean = Path(raw_path)
    if clean.is_absolute():
        clean = Path(str(clean).lstrip("/"))
    target = (root / clean).resolve()
    root_resolved = root.resolve()
    if not str(target).startswith(str(root_resolved)):
        raise HTTPException(status_code=403, detail="Path traversal denied.")

    neighbors: list[dict[str, Any]] = []
    seen: set[str] = set()

    # 1. Sibling files (same directory)
    parent = target.parent
    if parent.is_dir():
        try:
            for entry in sorted(parent.iterdir(), key=lambda e: e.name.lower()):
                if entry.is_file() and entry != target and entry.name not in _EXCLUDED_DIRS:
                    rel = prefix + _relpath(entry, root)
                    if rel not in seen:
                        seen.add(rel)
                        st = _safe_stat(entry)
                        neighbors.append({
                            "path": rel,
                            "name": entry.name,
                            "size": st.st_size if st else 0,
                            "category": _classify_file(entry.name),
                            "relation": "sibling",
                        })
        except (OSError, PermissionError):
            pass

    # 2. Cross-referenced files
    if target.is_file():
        refs = _extract_references(target, root)
        for ref in refs:
            ref_path = prefix + ref["target"].lstrip("./")
            if ref_path not in seen:
                seen.add(ref_path)
                neighbors.append({
                    "path": ref_path,
                    "name": ref["target"].split("/")[-1],
                    "category": _classify_file(ref["target"].split("/")[-1]),
                    "relation": "reference",
                })

    return JSONResponse({"path": path, "neighbors": neighbors})


@app.get("/api/graph")
def api_graph(
    include_runs: bool = Query(False, description="Include run nodes (971+). Off by default for performance."),
) -> JSONResponse:
    """
    Return nodes and edges for a graph visualization.

    Node types: folder, file, agent, run, chat_session
    Edge types: contains, references, generated_by, temporal, session_link

    By default run nodes (and generated_by / temporal edges) are excluded
    to keep the graph navigable.  Pass ?include_runs=true to get them.
    """
    nodes: list[dict[str, Any]] = []
    edges: list[dict[str, Any]] = []
    node_ids: set[str] = set()

    def _add_node(nid: str, label: str, ntype: str, **extra: Any) -> None:
        if nid in node_ids:
            return
        node_ids.add(nid)
        node = {"id": nid, "label": label, "type": ntype}
        node.update(extra)
        nodes.append(node)

    # --- Workspace nodes + containment edges ---

    files, folders = _walk_workspace()

    # Root node
    _add_node("ws:", "workspace", "folder")

    for folder in folders:
        fid = f"ws:{folder['path']}"
        _add_node(fid, folder["name"], "folder", path=folder["path"])

        # Containment edge to parent
        parent_path = str(Path(folder["path"]).parent)
        parent_id = "ws:" if parent_path == "." else f"ws:{parent_path}"
        edges.append({"source": parent_id, "target": fid, "type": "contains"})

    for f in files:
        fid = f"ws:{f['path']}"
        _add_node(fid, f["name"], "file", path=f["path"], size=f["size"], category=f["category"],
                  mtime=f.get("mtime"),
                  mtime_iso=datetime.fromtimestamp(f["mtime"], tz=timezone.utc).isoformat() if f.get("mtime") else None)

        parent_path = str(Path(f["path"]).parent)
        parent_id = "ws:" if parent_path == "." else f"ws:{parent_path}"
        edges.append({"source": parent_id, "target": fid, "type": "contains"})

    # --- Host workspace nodes + containment edges ---

    host_files, host_folders = _walk_host_workspace()

    # Host root node
    _add_node("host:", "host-workspace", "folder")

    for folder in host_folders:
        # folder["path"] is already prefixed with "host:"
        fid = folder["path"]
        _add_node(fid, folder["name"], "folder", path=folder["path"])

        # Containment edge to parent – strip "host:" prefix, get parent, re-prefix
        raw_path = folder["path"][len("host:"):]
        parent_path = str(Path(raw_path).parent)
        parent_id = "host:" if parent_path == "." else f"host:{parent_path}"
        edges.append({"source": parent_id, "target": fid, "type": "contains"})

    for f in host_files:
        fid = f["path"]  # already "host:..." prefixed
        _add_node(fid, f["name"], "file", path=f["path"], size=f["size"], category=f["category"],
                  mtime=f.get("mtime"),
                  mtime_iso=datetime.fromtimestamp(f["mtime"], tz=timezone.utc).isoformat() if f.get("mtime") else None)

        raw_path = f["path"][len("host:"):]
        parent_path = str(Path(raw_path).parent)
        parent_id = "host:" if parent_path == "." else f"host:{parent_path}"
        edges.append({"source": parent_id, "target": fid, "type": "contains"})

    # --- Cross-reference edges ---

    for f in files:
        fp = WORKSPACE_ROOT / f["path"]
        refs = _extract_references(fp, WORKSPACE_ROOT)
        for ref in refs:
            source_id = f"ws:{ref['source']}"
            # Try to resolve target to an existing workspace node
            target_path = ref["target"]
            # Normalize: strip leading ./ or /workspace/
            target_clean = target_path.lstrip("./")
            if target_clean.startswith("workspace/"):
                target_clean = target_clean[len("workspace/"):]
            target_id = f"ws:{target_clean}"
            if target_id in node_ids:
                edges.append({
                    "source": source_id,
                    "target": target_id,
                    "type": "references",
                    "ref_type": ref["type"],
                })
            else:
                # Only create ghost nodes for path-like references, not bare imports
                if ref["type"] != "import":
                    _add_node(target_id, target_clean, "file", path=target_clean, ghost=True)
                    edges.append({
                        "source": source_id,
                        "target": target_id,
                        "type": "references",
                        "ref_type": ref["type"],
                    })

    # --- Cross-reference edges from host workspace ---

    for f in host_files:
        raw_path = f["path"][len("host:"):]
        fp = HOST_WORKSPACE_ROOT / raw_path
        refs = _extract_references(fp, HOST_WORKSPACE_ROOT)
        for ref in refs:
            source_id = f"host:{ref['source']}"
            target_path = ref["target"]
            target_clean = target_path.lstrip("./")
            if target_clean.startswith("workspace/"):
                target_clean = target_clean[len("workspace/"):]
            # Try host workspace first, then agent workspace
            host_target_id = f"host:{target_clean}"
            ws_target_id = f"ws:{target_clean}"
            if host_target_id in node_ids:
                edges.append({
                    "source": source_id,
                    "target": host_target_id,
                    "type": "references",
                    "ref_type": ref["type"],
                })
            elif ws_target_id in node_ids:
                edges.append({
                    "source": source_id,
                    "target": ws_target_id,
                    "type": "references",
                    "ref_type": ref["type"],
                })
            else:
                # Only create ghost nodes for path-like references, not bare imports
                if ref["type"] != "import":
                    _add_node(host_target_id, target_clean, "file", path=f"host:{target_clean}", ghost=True)
                    edges.append({
                        "source": source_id,
                        "target": host_target_id,
                        "type": "references",
                        "ref_type": ref["type"],
                    })

    # --- Agent / run / chat nodes and edges ---

    agents = _iter_agents()
    for agent in agents:
        agent_id = f"agent:{agent['name']}"
        _add_node(agent_id, agent["name"], "agent",
                  run_count=agent["run_count"],
                  chat_session_count=agent["chat_session_count"])

        if include_runs:
            prev_run_id: str | None = None
            for run in agent["runs"]:
                rid = f"run:{agent['name']}/{run['run_id']}"
                _add_node(rid, run["run_id"], "run",
                          agent=agent["name"],
                          completed_at=run.get("completed_at"),
                          duration_ms=run.get("duration_ms"),
                          model=run.get("model"),
                          status=run.get("status"))
                edges.append({"source": agent_id, "target": rid, "type": "generated_by"})

                # Temporal edge between consecutive runs
                if prev_run_id:
                    edges.append({"source": prev_run_id, "target": rid, "type": "temporal"})
                prev_run_id = rid

        for chat in agent["chat_sessions"]:
            cid = f"chat:{agent['name']}/{chat['session_id']}"
            _add_node(cid, chat["session_id"], "chat_session", agent=agent["name"])
            edges.append({"source": agent_id, "target": cid, "type": "session_link"})

    return JSONResponse({
        "nodes": nodes,
        "edges": edges,
        "node_count": len(nodes),
        "edge_count": len(edges),
    })


@app.get("/api/timeline")
def api_timeline() -> JSONResponse:
    """All workspace items sorted by mtime descending."""
    files, folders = _walk_workspace()
    host_files, host_folders = _walk_host_workspace()

    items: list[dict[str, Any]] = []
    for f in files:
        items.append({
            "path": f["path"],
            "name": f["name"],
            "size": f["size"],
            "mtime": f["mtime"],
            "mtime_iso": datetime.fromtimestamp(f["mtime"], tz=timezone.utc).isoformat() if f["mtime"] else None,
            "type": "file",
            "category": f["category"],
            "workspace": "agent",
        })
    for d in folders:
        items.append({
            "path": d["path"],
            "name": d["name"],
            "size": 0,
            "mtime": d["mtime"],
            "mtime_iso": datetime.fromtimestamp(d["mtime"], tz=timezone.utc).isoformat() if d["mtime"] else None,
            "type": "directory",
            "category": "folder",
            "workspace": "agent",
        })
    for f in host_files:
        items.append({
            "path": f["path"],
            "name": f["name"],
            "size": f["size"],
            "mtime": f["mtime"],
            "mtime_iso": datetime.fromtimestamp(f["mtime"], tz=timezone.utc).isoformat() if f["mtime"] else None,
            "type": "file",
            "category": f["category"],
            "workspace": "host",
        })
    for d in host_folders:
        items.append({
            "path": d["path"],
            "name": d["name"],
            "size": 0,
            "mtime": d["mtime"],
            "mtime_iso": datetime.fromtimestamp(d["mtime"], tz=timezone.utc).isoformat() if d["mtime"] else None,
            "type": "directory",
            "category": "folder",
            "workspace": "host",
        })

    items.sort(key=lambda x: x["mtime"], reverse=True)

    return JSONResponse({
        "items": items,
        "total": len(items),
    })


@app.get("/api/agents")
def api_agents() -> JSONResponse:
    """List all agents with run counts and chat session counts."""
    agents = _iter_agents()
    summary = [
        {
            "name": a["name"],
            "run_count": a["run_count"],
            "chat_session_count": a["chat_session_count"],
        }
        for a in agents
    ]
    return JSONResponse({
        "agents": summary,
        "total": len(summary),
    })


@app.get("/api/agent/{name}")
def api_agent_detail(name: str) -> JSONResponse:
    """Detail for a specific agent including all runs and chat sessions."""
    agents_dir = CONTROLPLANE_ROOT / "agents" / name
    if not agents_dir.is_dir():
        raise HTTPException(status_code=404, detail=f"Agent not found: {name}")

    runs = _collect_runs(agents_dir)
    chats = _collect_chats(agents_dir)

    return JSONResponse({
        "name": name,
        "run_count": len(runs),
        "chat_session_count": len(chats),
        "runs": runs,
        "chat_sessions": chats,
    })


@app.get("/api/scheduler")
def api_scheduler() -> JSONResponse:
    """Return scheduler jobs from the control-plane scheduler/jobs.json."""
    jobs_path = CONTROLPLANE_ROOT / "scheduler" / "jobs.json"
    raw = _safe_read_json(jobs_path)

    jobs: list[dict[str, Any]] = []
    if raw is not None:
        if isinstance(raw, dict):
            # Structure A: {"jobs": [list of job dicts]}
            if "jobs" in raw and isinstance(raw["jobs"], list):
                for item in raw["jobs"]:
                    if isinstance(item, dict):
                        entry = dict(item)
                        # Normalize field names for the frontend
                        if "id" in entry and "name" not in entry:
                            entry["name"] = entry["id"]
                        if "agentID" in entry and "agent_id" not in entry:
                            entry["agent_id"] = entry["agentID"]
                        if "lastRun" in entry and "last_run" not in entry:
                            entry["last_run"] = entry["lastRun"]
                        if "message" in entry and "prompt" not in entry:
                            entry["prompt"] = entry["message"]
                        jobs.append(entry)
            else:
                # Structure B: {job_name: {job_config}, ...}
                for name, value in raw.items():
                    if isinstance(value, dict):
                        entry = dict(value)
                        entry["name"] = name
                        jobs.append(entry)
    # _safe_read_json only returns dict or None, so handle bare JSON array
    if not jobs and jobs_path.is_file():
        try:
            text = jobs_path.read_text(encoding="utf-8", errors="replace")
            data = json.loads(text)
            if isinstance(data, list):
                for item in data:
                    if isinstance(item, dict):
                        jobs.append(item)
        except Exception:
            pass

    return JSONResponse({
        "jobs": jobs,
        "total": len(jobs),
    })


@app.get("/api/chat/{session_id}/messages")
def api_chat_messages(
    session_id: str = PathParam(..., description="Chat session ID"),
    limit: int = Query(200, ge=1, le=1000, description="Max messages to return"),
) -> JSONResponse:
    """Return messages for a specific chat session, searched across all agents."""
    agents_dir = CONTROLPLANE_ROOT / "agents"
    if not agents_dir.is_dir():
        raise HTTPException(status_code=404, detail=f"Chat session not found: {session_id}")

    # Search all agent directories for a chat dir matching session_id
    chat_dir: Path | None = None
    agent_id: str = ""
    try:
        agent_entries = sorted(agents_dir.iterdir())
    except (OSError, PermissionError):
        raise HTTPException(status_code=404, detail=f"Chat session not found: {session_id}")

    for agent_entry in agent_entries:
        if not agent_entry.is_dir():
            continue
        candidate = agent_entry / "memory" / "chats" / session_id
        if candidate.is_dir():
            chat_dir = candidate
            agent_id = agent_entry.name
            break

    if chat_dir is None:
        raise HTTPException(status_code=404, detail=f"Chat session not found: {session_id}")

    # Read session metadata from meta.json
    meta = _safe_read_json(chat_dir / "meta.json") or {}

    # Read and parse messages from messages.jsonl
    messages_path = chat_dir / "messages.jsonl"
    all_messages: list[dict[str, Any]] = []
    total_count = 0

    try:
        with messages_path.open("r", encoding="utf-8", errors="replace") as fh:
            for line in fh:
                line = line.strip()
                if not line:
                    continue
                try:
                    msg = json.loads(line)
                except (json.JSONDecodeError, ValueError):
                    continue
                if not isinstance(msg, dict):
                    continue
                total_count += 1
                role = msg.get("role", "")
                if role == "tool":
                    entry: dict[str, Any] = {
                        "role": "tool",
                        "tool_name": msg.get("tool_name", ""),
                        "tool_call_id": msg.get("tool_call_id", ""),
                        "summary": "",
                        "output": "",
                        "error": "",
                    }
                    if "ts" in msg:
                        entry["ts"] = msg["ts"]
                    if "run_id" in msg:
                        entry["run_id"] = msg["run_id"]
                    # Parse the content JSON for tool details
                    try:
                        inner = json.loads(msg.get("content", "{}"))
                        entry["summary"] = inner.get("summary", "")
                        entry["output"] = inner.get("output", "")
                        entry["error"] = inner.get("error", "")
                        if not entry["tool_name"]:
                            entry["tool_name"] = inner.get("tool", "")
                    except (json.JSONDecodeError, ValueError):
                        entry["output"] = msg.get("content", "")
                else:
                    entry = {
                        "role": role,
                        "content": msg.get("content", ""),
                    }
                    if "ts" in msg:
                        entry["ts"] = msg["ts"]
                    if "run_id" in msg:
                        entry["run_id"] = msg["run_id"]
                all_messages.append(entry)
    except PermissionError:
        # Many chat files are root-owned; return what we can
        pass
    except FileNotFoundError:
        pass

    # Apply limit: return the last N messages (most recent last)
    shown = all_messages[-limit:]

    return JSONResponse({
        "session_id": session_id,
        "agent_id": agent_id,
        "channel": meta.get("channel", ""),
        "title": meta.get("title", ""),
        "created_at": meta.get("created_at", ""),
        "updated_at": meta.get("updated_at", ""),
        "messages": shown,
        "total_messages": total_count,
        "shown_messages": len(shown),
    })


@app.get("/api/provenance")
def api_provenance() -> JSONResponse:
    """Container info, mount info, and control-plane context."""
    # System / container info
    container_info: dict[str, Any] = {
        "hostname": platform.node(),
        "platform": platform.platform(),
        "python_version": platform.python_version(),
        "pid": os.getpid(),
        "cwd": os.getcwd(),
    }

    # Mount info: check if workspace and controlplane are accessible
    ws_accessible = WORKSPACE_ROOT.is_dir()
    cp_accessible = CONTROLPLANE_ROOT.is_dir()
    host_ws_accessible = HOST_WORKSPACE_ROOT.is_dir()

    mount_info: dict[str, Any] = {
        "workspace_root": str(WORKSPACE_ROOT),
        "workspace_accessible": ws_accessible,
        "host_workspace_root": str(HOST_WORKSPACE_ROOT),
        "host_workspace_accessible": host_ws_accessible,
        "controlplane_root": str(CONTROLPLANE_ROOT),
        "controlplane_accessible": cp_accessible,
    }

    # Try to get workspace file count quickly
    if ws_accessible:
        file_count = 0
        for _, _, fnames in os.walk(str(WORKSPACE_ROOT)):
            file_count += len(fnames)
        mount_info["workspace_file_count"] = file_count

    # Try to get host workspace file count quickly
    if host_ws_accessible:
        host_file_count = 0
        for _, _, fnames in os.walk(str(HOST_WORKSPACE_ROOT)):
            host_file_count += len(fnames)
        mount_info["host_workspace_file_count"] = host_file_count

    # Control-plane context
    cp_context: dict[str, Any] = {}
    config_path = CONTROLPLANE_ROOT / "config.json"
    config = _safe_read_json(config_path)
    if config:
        # Expose non-secret config fields
        safe_keys = [
            "model", "provider", "sandbox", "workspace",
            "channels", "default_agent", "instance_id",
        ]
        cp_context["config"] = {k: config[k] for k in safe_keys if k in config}

    # Count agents
    agents_dir = CONTROLPLANE_ROOT / "agents"
    if agents_dir.is_dir():
        try:
            cp_context["agent_count"] = sum(1 for d in agents_dir.iterdir() if d.is_dir())
        except (OSError, PermissionError):
            cp_context["agent_count"] = 0

    # Scheduler info
    scheduler_dir = CONTROLPLANE_ROOT / "scheduler"
    if scheduler_dir.is_dir():
        jobs_path = scheduler_dir / "jobs.json"
        jobs = _safe_read_json(jobs_path)
        if jobs:
            cp_context["scheduler"] = {"jobs_loaded": True, "job_count": len(jobs) if isinstance(jobs, dict) else 0}
        else:
            cp_context["scheduler"] = {"jobs_loaded": False}

    # Policy info
    policy_dir = CONTROLPLANE_ROOT / "policy"
    if policy_dir.is_dir():
        try:
            policy_files = [f.name for f in policy_dir.iterdir() if f.is_file()]
            cp_context["policy_files"] = sorted(policy_files)
        except (OSError, PermissionError):
            cp_context["policy_files"] = []

    # Check for runs.json existence but do NOT read it
    runs_json = CONTROLPLANE_ROOT / "runs.json"
    if runs_json.exists():
        st = _safe_stat(runs_json)
        cp_context["runs_json"] = {
            "exists": True,
            "size_bytes": st.st_size if st else 0,
            "size_mb": round(st.st_size / (1024 * 1024), 1) if st else 0,
            "note": "Skipped reading – file too large for dashboard ingestion.",
        }

    return JSONResponse({
        "container": container_info,
        "mounts": mount_info,
        "controlplane": cp_context,
        "generated_at": datetime.now(tz=timezone.utc).isoformat(),
    })


# ---------------------------------------------------------------------------
# Static file serving + SPA fallback
# ---------------------------------------------------------------------------

# Mount static files if the directory exists
if STATIC_DIR.is_dir():
    app.mount("/static", StaticFiles(directory=str(STATIC_DIR)), name="static")


@app.get("/", response_model=None)
def root() -> FileResponse | JSONResponse:
    """Serve index.html from the static directory, or a fallback JSON message."""
    index = STATIC_DIR / "index.html"
    if index.is_file():
        return FileResponse(str(index), media_type="text/html")
    # Fallback: return a helpful JSON response
    return JSONResponse({
        "status": "ok",
        "message": "openclawssy artifact dashboard API is running.",
        "hint": "Place index.html in /app/static to enable the web UI.",
        "endpoints": [
            "/api/overview",
            "/api/tree",
            "/api/file?path=<relative-path>",
            "/api/file-neighbors?path=<relative-path>",
            "/api/graph",
            "/api/timeline",
            "/api/agents",
            "/api/agent/{name}",
            "/api/scheduler",
            "/api/chat/{session_id}/messages",
            "/api/provenance",
        ],
    })


# ---------------------------------------------------------------------------
# Entrypoint
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    import uvicorn

    uvicorn.run(
        "app:app",
        host="0.0.0.0",
        port=8050,
        log_level="info",
        access_log=True,
    )
