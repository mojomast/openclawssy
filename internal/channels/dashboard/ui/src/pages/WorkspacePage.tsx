import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { api } from "@/lib/api"

const WORKSPACE_AUTO_REFRESH_MS = 4000

type WorkspaceEntry = {
  name: string
  path: string
  kind: "dir" | "file"
  sizeBytes: number
  modifiedAt: string
  mimeType: string
}

type WorkspaceFile = {
  path: string
  name: string
  sizeBytes: number
  modifiedAt: string
  mimeType: string
  isText: boolean
  truncated: boolean
  previewNotice: string
  content: string
}

type WorkspaceEntriesResponse = {
  workspace_root?: string
  path?: string
  parent_path?: string
  entries?: WorkspaceEntryResponseItem[]
}

type WorkspaceEntryResponseItem = {
  name?: string
  path?: string
  kind?: string
  size_bytes?: number
  modified_at?: string
  mime_type?: string
}

type WorkspaceFileResponse = {
  path?: string
  name?: string
  size_bytes?: number
  modified_at?: string
  mime_type?: string
  is_text?: boolean
  truncated?: boolean
  preview_notice?: string
  content?: string
}

function asText(value: unknown): string {
  return String(value || "").trim()
}

function formatDateTime(value: string): string {
  if (!value) {
    return "-"
  }
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return "-"
  }
  return parsed.toLocaleString()
}

function formatBytes(value: number): string {
  const size = Number(value) || 0
  if (size < 1024) {
    return `${size} B`
  }
  if (size < 1024 * 1024) {
    return `${(size / 1024).toFixed(1)} KB`
  }
  return `${(size / (1024 * 1024)).toFixed(1)} MB`
}

function parentDirectory(pathValue: string): string {
  const clean = asText(pathValue)
  if (!clean || clean === ".") {
    return "."
  }
  const parts = clean.split("/").filter(Boolean)
  parts.pop()
  return parts.length ? parts.join("/") : "."
}

function normalizeEntry(entry: WorkspaceEntryResponseItem): WorkspaceEntry | null {
  if (!entry || typeof entry !== "object") {
    return null
  }
  return {
    name: asText(entry.name),
    path: asText(entry.path),
    kind: asText(entry.kind) === "dir" ? "dir" : "file",
    sizeBytes: Number(entry.size_bytes) || 0,
    modifiedAt: asText(entry.modified_at),
    mimeType: asText(entry.mime_type),
  }
}

export function WorkspacePage() {
  const [workspaceRoot, setWorkspaceRoot] = useState("")
  const [currentPath, setCurrentPath] = useState(".")
  const [parentPath, setParentPath] = useState("")
  const [entries, setEntries] = useState<WorkspaceEntry[]>([])
  const [selectedPath, setSelectedPath] = useState("")
  const [selectedFile, setSelectedFile] = useState<WorkspaceFile | null>(null)
  const [filterQuery, setFilterQuery] = useState("")
  const [autoRefresh, setAutoRefresh] = useState(false)
  const [loadingEntries, setLoadingEntries] = useState(false)
  const [loadingFile, setLoadingFile] = useState(false)
  const [statusText, setStatusText] = useState("")
  const [statusKind, setStatusKind] = useState<"" | "success" | "error">("")

  const currentPathRef = useRef(currentPath)
  const selectedPathRef = useRef(selectedPath)
  const loadingEntriesRef = useRef(false)
  const loadingFileRef = useRef(false)

  useEffect(() => {
    currentPathRef.current = currentPath
  }, [currentPath])

  useEffect(() => {
    selectedPathRef.current = selectedPath
  }, [selectedPath])

  const clearSelection = useCallback(() => {
    selectedPathRef.current = ""
    setSelectedPath("")
    setSelectedFile(null)
  }, [])

  const loadEntries = useCallback(async (pathValue: string = currentPathRef.current, options: { keepStatus?: boolean } = {}) => {
    if (loadingEntriesRef.current) {
      return
    }
    const { keepStatus = false } = options
    loadingEntriesRef.current = true
    setLoadingEntries(true)

    const nextPath = asText(pathValue) || "."
    if (!keepStatus) {
      setStatusKind("")
      setStatusText(`Loading ${nextPath}...`)
    }

    try {
      const payload = await api.get<WorkspaceEntriesResponse>(`/api/admin/workspace/entries?path=${encodeURIComponent(nextPath)}`)
      const normalizedEntries = Array.isArray(payload.entries)
        ? payload.entries.map((entry) => normalizeEntry(entry)).filter((entry): entry is WorkspaceEntry => Boolean(entry && entry.path))
        : []

      const resolvedPath = asText(payload.path) || "."
      currentPathRef.current = resolvedPath
      setWorkspaceRoot(asText(payload.workspace_root))
      setCurrentPath(resolvedPath)
      setParentPath(asText(payload.parent_path))
      setEntries(normalizedEntries)

      if (!keepStatus) {
        setStatusKind("success")
        setStatusText(`Loaded ${normalizedEntries.length} item(s).`)
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      setStatusKind("error")
      setStatusText(`Failed to load workspace entries: ${message}`)
    } finally {
      loadingEntriesRef.current = false
      setLoadingEntries(false)
    }
  }, [])

  const loadFile = useCallback(async (pathValue: string, options: { keepStatus?: boolean } = {}) => {
    if (loadingFileRef.current) {
      return
    }

    const nextPath = asText(pathValue)
    if (!nextPath) {
      return
    }

    const { keepStatus = false } = options
    loadingFileRef.current = true
    setLoadingFile(true)
    selectedPathRef.current = nextPath
    setSelectedPath(nextPath)

    if (!keepStatus) {
      setStatusKind("")
      setStatusText(`Opening ${nextPath}...`)
    }

    try {
      const payload = await api.get<WorkspaceFileResponse>(`/api/admin/workspace/file?path=${encodeURIComponent(nextPath)}`)
      const filePayload: WorkspaceFile = {
        path: asText(payload.path),
        name: asText(payload.name),
        sizeBytes: Number(payload.size_bytes) || 0,
        modifiedAt: asText(payload.modified_at),
        mimeType: asText(payload.mime_type),
        isText: Boolean(payload.is_text),
        truncated: Boolean(payload.truncated),
        previewNotice: asText(payload.preview_notice),
        content: typeof payload.content === "string" ? payload.content : "",
      }
      selectedPathRef.current = filePayload.path
      setSelectedPath(filePayload.path)
      setSelectedFile(filePayload)

      if (!keepStatus) {
        setStatusKind("success")
        setStatusText(`Opened ${filePayload.path}.`)
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      selectedPathRef.current = ""
      setSelectedPath("")
      setSelectedFile(null)
      if (!keepStatus) {
        setStatusKind("error")
        setStatusText(`Failed to open file: ${message}`)
      }
    } finally {
      loadingFileRef.current = false
      setLoadingFile(false)
    }
  }, [])

  const refreshWorkspace = useCallback(async (options: { silent?: boolean } = {}) => {
    const { silent = false } = options
    await loadEntries(currentPathRef.current, { keepStatus: silent })

    const selected = selectedPathRef.current
    if (!selected) {
      return
    }

    if (parentDirectory(selected) === currentPathRef.current) {
      await loadFile(selected, { keepStatus: true })
    }
  }, [loadEntries, loadFile])

  useEffect(() => {
    void refreshWorkspace()
  }, [refreshWorkspace])

  useEffect(() => {
    if (!autoRefresh) {
      return
    }
    const interval = window.setInterval(() => {
      void refreshWorkspace({ silent: true })
    }, WORKSPACE_AUTO_REFRESH_MS)
    return () => {
      window.clearInterval(interval)
    }
  }, [autoRefresh, refreshWorkspace])

  const filteredEntries = useMemo(() => {
    const query = asText(filterQuery).toLowerCase()
    if (!query) {
      return entries
    }
    return entries.filter((entry) => `${entry.name} ${entry.path} ${entry.kind}`.toLowerCase().includes(query))
  }, [entries, filterQuery])

  const breadcrumbs = useMemo(() => {
    const cleanPath = asText(currentPath)
    if (!cleanPath || cleanPath === ".") {
      return [] as Array<{ label: string; path: string }>
    }
    const parts = cleanPath.split("/").filter(Boolean)
    return parts.map((part, index) => ({
      label: part,
      path: parts.slice(0, index + 1).join("/"),
    }))
  }, [currentPath])

  const upTarget = currentPath === "." ? "" : parentPath || "."

  return (
    <div className="space-y-4 p-6" data-testid="workspace-page">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Workspace</h2>
        <p className="text-sm text-muted-foreground">
          Browse the active workspace, inspect folders, and preview text files without leaving the dashboard.
        </p>
      </div>

      <section className="rounded-lg border bg-card p-4 space-y-4">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div className="space-y-2">
            <h3 className="text-lg font-semibold">Workspace Browser</h3>
            <p className="text-sm text-muted-foreground">
              {workspaceRoot
                ? `Root: ${workspaceRoot}`
                : "Browse the active workspace tree and preview files safely from the dashboard."}
            </p>
            <nav className="flex flex-wrap items-center gap-1 text-sm text-muted-foreground" aria-label="Workspace breadcrumb">
              <Button
                type="button"
                variant="link"
                className="h-auto px-0 py-0"
                onClick={() => {
                  clearSelection()
                  void loadEntries(".")
                }}
              >
                workspace
              </Button>
              {breadcrumbs.map((crumb) => (
                <span key={crumb.path} className="inline-flex items-center gap-1">
                  <span>/</span>
                  <Button
                    type="button"
                    variant="link"
                    className="h-auto px-0 py-0"
                    onClick={() => {
                      clearSelection()
                      void loadEntries(crumb.path)
                    }}
                  >
                    {crumb.label}
                  </Button>
                </span>
              ))}
            </nav>
          </div>

          <div className="flex w-full flex-col gap-2 lg:max-w-xs">
            <Input
              type="search"
              placeholder="Filter current folder"
              aria-label="Filter current folder"
              value={filterQuery}
              onChange={(event) => setFilterQuery(event.target.value)}
            />

            <label className="inline-flex items-center gap-2 text-sm text-muted-foreground">
              <input
                type="checkbox"
                className="h-4 w-4"
                checked={autoRefresh}
                aria-label="Auto refresh"
                onChange={(event) => setAutoRefresh(event.target.checked)}
              />
              <span>Auto refresh ({WORKSPACE_AUTO_REFRESH_MS / 1000}s)</span>
            </label>

            <div className="flex gap-2">
              <Button
                type="button"
                variant="outline"
                disabled={loadingEntries || !upTarget}
                onClick={() => {
                  clearSelection()
                  void loadEntries(upTarget)
                }}
              >
                Up
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={loadingEntries || loadingFile}
                onClick={() => {
                  void refreshWorkspace()
                }}
              >
                {loadingEntries || loadingFile ? "Refreshing..." : "Refresh"}
              </Button>
            </div>
          </div>
        </div>

        <p
          className={`text-sm ${
            statusKind === "error"
              ? "text-destructive"
              : statusKind === "success"
                ? "text-emerald-600 dark:text-emerald-400"
                : "text-muted-foreground"
          }`}
        >
          {statusText || "Use the browser to inspect files and folders inside the active workspace."}
        </p>
      </section>

      <section className="grid gap-4 lg:grid-cols-2">
        <article className="rounded-lg border bg-card p-4 space-y-3">
          <div className="flex items-center justify-between gap-2">
            <h4 className="text-base font-semibold">Entries ({entries.length})</h4>
            <span className="text-xs text-muted-foreground">{asText(currentPath) || "."}</span>
          </div>

          {filteredEntries.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              {filterQuery ? "No entries match this filter." : "This folder is empty."}
            </p>
          ) : (
            <div className="space-y-2">
              {filteredEntries.map((entry) => (
                <Button
                  key={entry.path}
                  type="button"
                  variant={entry.path === selectedPath ? "default" : "ghost"}
                  className="h-auto w-full justify-start whitespace-normal px-3 py-2"
                  onClick={() => {
                    if (entry.kind === "dir") {
                      clearSelection()
                      void loadEntries(entry.path)
                      return
                    }
                    void loadFile(entry.path)
                  }}
                >
                  <span className="text-left">
                    <strong className="block">{entry.kind === "dir" ? "DIR" : "FILE"} {entry.name}</strong>
                    <span className="block text-xs text-muted-foreground">
                      {entry.kind === "dir"
                        ? formatDateTime(entry.modifiedAt)
                        : `${formatBytes(entry.sizeBytes)} · ${formatDateTime(entry.modifiedAt)}`}
                    </span>
                  </span>
                </Button>
              ))}
            </div>
          )}
        </article>

        <article className="rounded-lg border bg-card p-4 space-y-3">
          <h4 className="text-base font-semibold">Preview</h4>
          {!selectedFile ? (
            <p className="text-sm text-muted-foreground">Select a file to preview its contents.</p>
          ) : (
            <>
              <div className="space-y-1 text-sm">
                <p>Path {selectedFile.path}</p>
                <p>Size {formatBytes(selectedFile.sizeBytes)}</p>
                <p>MIME {selectedFile.mimeType || "-"}</p>
                <p>Modified {formatDateTime(selectedFile.modifiedAt)}</p>
              </div>

              {selectedFile.previewNotice && (
                <p className={`text-sm ${selectedFile.isText ? "text-muted-foreground" : "text-amber-600 dark:text-amber-400"}`}>
                  {selectedFile.previewNotice}
                </p>
              )}

              {selectedFile.isText && (
                <pre className="max-h-[420px] overflow-auto rounded-md border bg-muted/40 p-3 text-xs leading-relaxed">
                  {selectedFile.content}
                </pre>
              )}
            </>
          )}
        </article>
      </section>
    </div>
  )
}
