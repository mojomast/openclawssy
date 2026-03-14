import { useCallback, useEffect, useState, type FormEvent } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { ApiError, api } from "@/lib/api"

const DEFAULT_AGENT_ID = "default"
const DEFAULT_PULL_IMAGE = "ubuntu:24.04"
const DEFAULT_MOUNT_CONFIG = "# Example:\n# /host/path:/container/path:ro\n"

type SandboxStatus = {
  agentID: string
  provider: string
  running: boolean
  containerName: string
  containerID: string
  image: string
  workspacePath: string
  volumeName: string
  networkMode: string
}

type SandboxImage = {
  id: string
  repo: string
  tag: string
  sizeMB: number | null
}

type SandboxVolume = {
  name: string
  driver: string
  mountpoint: string
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return null
  }
  return value as Record<string, unknown>
}

function asText(value: unknown): string {
  if (value === null || value === undefined) {
    return ""
  }
  return String(value)
}

function asBoolean(value: unknown): boolean {
  if (typeof value === "boolean") {
    return value
  }
  if (typeof value === "number") {
    return value !== 0
  }
  if (typeof value === "string") {
    const normalized = value.trim().toLowerCase()
    return normalized === "true" || normalized === "1" || normalized === "yes"
  }
  return false
}

function asNumber(value: unknown): number | null {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value
  }
  if (typeof value === "string") {
    const parsed = Number(value)
    if (Number.isFinite(parsed)) {
      return parsed
    }
  }
  return null
}

function extractErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (typeof error.details === "string" && error.details.trim().length > 0) {
      return error.details.trim()
    }

    const details = asRecord(error.details)
    const detailsMessage = asText(details?.message).trim()
    if (detailsMessage) {
      return detailsMessage
    }

    const nestedError = asRecord(details?.error)
    const nestedMessage = asText(nestedError?.message).trim()
    if (nestedMessage) {
      return nestedMessage
    }
  }

  if (error instanceof Error) {
    return error.message || "Unknown error"
  }

  return asText(error).trim() || "Unknown error"
}

function normalizeStatus(value: unknown): SandboxStatus {
  const raw = asRecord(value)
  return {
    agentID: asText(raw?.agent_id ?? raw?.agentID).trim() || DEFAULT_AGENT_ID,
    provider: asText(raw?.provider).trim() || "unknown",
    running: asBoolean(raw?.running),
    containerName: asText(raw?.container_name ?? raw?.containerName).trim(),
    containerID: asText(raw?.container_id ?? raw?.containerID).trim(),
    image: asText(raw?.image).trim(),
    workspacePath: asText(raw?.workspace_path ?? raw?.workspacePath).trim(),
    volumeName: asText(raw?.volume_name ?? raw?.volumeName).trim(),
    networkMode: asText(raw?.network_mode ?? raw?.networkMode).trim(),
  }
}

function normalizeImage(value: unknown): SandboxImage | null {
  const raw = asRecord(value)
  if (!raw) {
    return null
  }

  const id = asText(raw.id).trim()
  if (!id) {
    return null
  }

  return {
    id,
    repo: asText(raw.repo).trim(),
    tag: asText(raw.tag).trim(),
    sizeMB: asNumber(raw.size_mb ?? raw.sizeMB),
  }
}

function normalizeVolume(value: unknown): SandboxVolume | null {
  const raw = asRecord(value)
  if (!raw) {
    return null
  }

  const name = asText(raw.name).trim()
  if (!name) {
    return null
  }

  return {
    name,
    driver: asText(raw.driver).trim(),
    mountpoint: asText(raw.mountpoint).trim(),
  }
}

function toShortContainerID(value: string): string {
  return value.replace(/^sha256:/, "").slice(0, 12)
}

export function SandboxPage() {
  const [status, setStatus] = useState<SandboxStatus | null>(null)
  const [images, setImages] = useState<SandboxImage[] | null>(null)
  const [volumes, setVolumes] = useState<SandboxVolume[] | null>(null)

  const [statusLoading, setStatusLoading] = useState(false)
  const [imagesLoading, setImagesLoading] = useState(false)
  const [volumesLoading, setVolumesLoading] = useState(false)

  const [statusError, setStatusError] = useState("")
  const [imagesError, setImagesError] = useState("")
  const [volumesError, setVolumesError] = useState("")

  const [actionPending, setActionPending] = useState(false)
  const [actionError, setActionError] = useState("")
  const [actionSuccess, setActionSuccess] = useState("")

  const [pullImage, setPullImage] = useState(DEFAULT_PULL_IMAGE)
  const [pullPending, setPullPending] = useState(false)
  const [pullError, setPullError] = useState("")
  const [pullSuccess, setPullSuccess] = useState("")

  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [mountConfig, setMountConfig] = useState(DEFAULT_MOUNT_CONFIG)

  const loadStatus = useCallback(async () => {
    setStatusLoading(true)
    setStatusError("")
    try {
      const payload = await api.get<unknown>(
        `/api/admin/sandbox/docker/status?agent_id=${encodeURIComponent(DEFAULT_AGENT_ID)}`
      )
      setStatus(normalizeStatus(payload))
    } catch (error) {
      setStatusError(extractErrorMessage(error))
    } finally {
      setStatusLoading(false)
    }
  }, [])

  const loadImages = useCallback(async () => {
    setImagesLoading(true)
    setImagesError("")
    try {
      const payload = await api.get<{ images?: unknown }>("/api/admin/sandbox/docker/images")
      const normalized = Array.isArray(payload.images)
        ? payload.images.map(normalizeImage).filter((item): item is SandboxImage => item !== null)
        : []
      setImages(normalized)
    } catch (error) {
      setImagesError(extractErrorMessage(error))
    } finally {
      setImagesLoading(false)
    }
  }, [])

  const loadVolumes = useCallback(async () => {
    setVolumesLoading(true)
    setVolumesError("")
    try {
      const payload = await api.get<{ volumes?: unknown }>("/api/admin/sandbox/docker/volumes")
      const normalized = Array.isArray(payload.volumes)
        ? payload.volumes.map(normalizeVolume).filter((item): item is SandboxVolume => item !== null)
        : []
      setVolumes(normalized)
    } catch (error) {
      setVolumesError(extractErrorMessage(error))
    } finally {
      setVolumesLoading(false)
    }
  }, [])

  useEffect(() => {
    void Promise.allSettled([loadStatus(), loadImages(), loadVolumes()])
  }, [loadStatus, loadImages, loadVolumes])

  const runContainerAction = useCallback(
    async (label: string, action: () => Promise<void>) => {
      if (actionPending) {
        return
      }

      setActionPending(true)
      setActionError("")
      setActionSuccess("")

      try {
        await action()
        setActionSuccess(`${label} succeeded.`)
        await loadStatus()
      } catch (error) {
        setActionError(extractErrorMessage(error))
      } finally {
        setActionPending(false)
      }
    },
    [actionPending, loadStatus]
  )

  const onCreateContainer = useCallback(async () => {
    await runContainerAction("Create container", async () => {
      await api.post("/api/admin/sandbox/docker/create", { agent_id: DEFAULT_AGENT_ID })
    })
  }, [runContainerAction])

  const onStopContainer = useCallback(async () => {
    await runContainerAction("Stop container", async () => {
      await api.post("/api/admin/sandbox/docker/stop", { agent_id: DEFAULT_AGENT_ID })
    })
  }, [runContainerAction])

  const onResetContainer = useCallback(async () => {
    const confirmed = window.confirm("This will destroy all files in the container volume. Are you sure?")
    if (!confirmed) {
      return
    }

    await runContainerAction("Reset container", async () => {
      await api.post("/api/admin/sandbox/docker/reset", { agent_id: DEFAULT_AGENT_ID })
    })

    await loadVolumes()
  }, [loadVolumes, runContainerAction])

  const onSubmitPullImage = useCallback(
    async (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault()
      const image = pullImage.trim()
      if (!image) {
        setPullError("Image name is required.")
        setPullSuccess("")
        return
      }

      setPullPending(true)
      setPullError("")
      setPullSuccess("")

      try {
        await api.post("/api/admin/sandbox/docker/pull", { image })
        setPullSuccess(`Pulled image: ${image}`)
        await loadImages()
      } catch (error) {
        setPullError(extractErrorMessage(error))
      } finally {
        setPullPending(false)
      }
    },
    [loadImages, pullImage]
  )

  const onDeleteVolume = useCallback(
    async (name: string) => {
      const confirmed = window.confirm(`Delete volume "${name}"? This cannot be undone.`)
      if (!confirmed || actionPending) {
        return
      }

      setActionPending(true)
      setActionError("")
      setActionSuccess("")

      try {
        await api.request("/api/admin/sandbox/docker/volume", {
          method: "DELETE",
          body: { name },
        })
        setActionSuccess(`Deleted volume: ${name}`)
        await loadVolumes()
      } catch (error) {
        setActionError(extractErrorMessage(error))
      } finally {
        setActionPending(false)
      }
    },
    [actionPending, loadVolumes]
  )

  const nonDockerProvider = status && status.provider !== "docker"
  const runningBadgeText = status?.running ? "running" : "stopped"

  return (
    <div className="space-y-4 p-6" data-testid="sandbox-page">
      <div className="space-y-2">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="text-2xl font-semibold tracking-tight">Sandbox</h2>
          <Badge variant={status?.provider === "docker" ? "secondary" : "outline"}>{status?.provider || "unknown"}</Badge>
        </div>
        <p className="text-sm text-muted-foreground">
          Manage Docker sandbox containers, images, volumes, and mount configuration.
        </p>
      </div>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="text-base">Container status</CardTitle>
            <Button type="button" variant="outline" size="sm" onClick={() => void loadStatus()} disabled={statusLoading}>
              {statusLoading ? "Refreshing..." : "Refresh status"}
            </Button>
          </div>
          <CardDescription>Container metadata and runtime state for agent {DEFAULT_AGENT_ID}.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {statusError ? <p className="text-sm text-destructive">Failed to load status: {statusError}</p> : null}
          {statusLoading && !status ? <p className="text-sm text-muted-foreground">Loading status...</p> : null}
          {!statusLoading && !status && !statusError ? (
            <p className="text-sm text-muted-foreground">Status not loaded yet. Click refresh status.</p>
          ) : null}

          {status && !nonDockerProvider ? (
            <Table>
              <TableBody>
                <TableRow>
                  <TableHead>Container name</TableHead>
                  <TableCell>{status.containerName || "—"}</TableCell>
                </TableRow>
                <TableRow>
                  <TableHead>Container ID</TableHead>
                  <TableCell className="font-mono">{status.containerID ? toShortContainerID(status.containerID) : "—"}</TableCell>
                </TableRow>
                <TableRow>
                  <TableHead>Image</TableHead>
                  <TableCell>
                    <div className="flex flex-wrap items-center gap-2">
                      <span>{status.image || "—"}</span>
                      <Badge data-testid="sandbox-running-badge" variant={status.running ? "secondary" : "outline"}>
                        {runningBadgeText}
                      </Badge>
                    </div>
                  </TableCell>
                </TableRow>
                <TableRow>
                  <TableHead>Workspace path</TableHead>
                  <TableCell className="font-mono">{status.workspacePath || "—"}</TableCell>
                </TableRow>
                <TableRow>
                  <TableHead>Volume name</TableHead>
                  <TableCell className="font-mono">{status.volumeName || "—"}</TableCell>
                </TableRow>
                <TableRow>
                  <TableHead>Network mode</TableHead>
                  <TableCell>{status.networkMode || "—"}</TableCell>
                </TableRow>
              </TableBody>
            </Table>
          ) : null}

          {nonDockerProvider ? (
            <p className="text-sm text-yellow-700 dark:text-yellow-400">
              Switch sandbox provider to docker in Settings to use container lifecycle controls.
            </p>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Container actions</CardTitle>
          <CardDescription>Create, stop, or reset the sandbox container for this agent.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex flex-wrap gap-2">
            <Button type="button" onClick={() => void onCreateContainer()} disabled={actionPending}>
              {actionPending ? "Working..." : "Create container"}
            </Button>
            <Button type="button" variant="outline" onClick={() => void onStopContainer()} disabled={actionPending}>
              {actionPending ? "Working..." : "Stop container"}
            </Button>
            <Button type="button" variant="outline" onClick={() => void onResetContainer()} disabled={actionPending}>
              {actionPending ? "Working..." : "Reset container"}
            </Button>
          </div>

          <p className="text-sm text-muted-foreground">
            Reset destroys all files in the container volume and recreates it from scratch.
          </p>

          {actionError ? <p className="text-sm text-destructive">Action failed: {actionError}</p> : null}
          {actionSuccess ? <p className="text-sm text-muted-foreground">{actionSuccess}</p> : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Pull image</CardTitle>
          <CardDescription>Pull a Docker image by repository and tag (for example, alpine:3.19).</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <form className="flex flex-wrap items-end gap-2" onSubmit={(event) => void onSubmitPullImage(event)}>
            <label htmlFor="sandbox-pull-image" className="grow space-y-1 text-sm">
              <span>Image name</span>
              <Input
                id="sandbox-pull-image"
                value={pullImage}
                onChange={(event) => setPullImage(event.target.value)}
                placeholder={DEFAULT_PULL_IMAGE}
              />
            </label>
            <Button type="submit" disabled={pullPending}>{pullPending ? "Pulling..." : "Pull image"}</Button>
          </form>

          {pullError ? <p className="text-sm text-destructive">Failed to pull image: {pullError}</p> : null}
          {pullSuccess ? <p className="text-sm text-muted-foreground">{pullSuccess}</p> : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="text-base">Available images</CardTitle>
            <Button type="button" variant="outline" size="sm" onClick={() => void loadImages()} disabled={imagesLoading}>
              {imagesLoading ? "Refreshing..." : "Refresh images"}
            </Button>
          </div>
        </CardHeader>
        <CardContent className="space-y-3">
          {imagesError ? <p className="text-sm text-destructive">Failed to load images: {imagesError}</p> : null}
          {imagesLoading && !images ? <p className="text-sm text-muted-foreground">Loading images...</p> : null}
          {!imagesLoading && images && images.length === 0 ? (
            <p className="text-sm text-muted-foreground">No images found.</p>
          ) : null}

          {images && images.length > 0 ? (
            <Table aria-label="Available images">
              <TableHeader>
                <TableRow>
                  <TableHead>Repository:Tag</TableHead>
                  <TableHead>ID</TableHead>
                  <TableHead>Size (MB)</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {images.map((image) => {
                  const repoTag = `${image.repo || "unknown"}:${image.tag || "latest"}`
                  return (
                    <TableRow key={image.id}>
                      <TableCell>{repoTag}</TableCell>
                      <TableCell className="font-mono">{toShortContainerID(image.id)}</TableCell>
                      <TableCell>{image.sizeMB ?? "—"}</TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="text-base">Docker volumes</CardTitle>
            <Button type="button" variant="outline" size="sm" onClick={() => void loadVolumes()} disabled={volumesLoading}>
              {volumesLoading ? "Refreshing..." : "Refresh volumes"}
            </Button>
          </div>
        </CardHeader>
        <CardContent className="space-y-3">
          {volumesError ? <p className="text-sm text-destructive">Failed to load volumes: {volumesError}</p> : null}
          {volumesLoading && !volumes ? <p className="text-sm text-muted-foreground">Loading volumes...</p> : null}
          {!volumesLoading && volumes && volumes.length === 0 ? (
            <p className="text-sm text-muted-foreground">No volumes found.</p>
          ) : null}

          {volumes && volumes.length > 0 ? (
            <Table aria-label="Docker volumes">
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Driver</TableHead>
                  <TableHead>Mountpoint</TableHead>
                  <TableHead>Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {volumes.map((volume) => (
                  <TableRow key={volume.name}>
                    <TableCell className="font-mono">{volume.name}</TableCell>
                    <TableCell>{volume.driver || "—"}</TableCell>
                    <TableCell className="max-w-[28rem] truncate font-mono" title={volume.mountpoint || ""}>
                      {volume.mountpoint || "—"}
                    </TableCell>
                    <TableCell>
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        onClick={() => void onDeleteVolume(volume.name)}
                        disabled={actionPending}
                        aria-label={`Delete volume ${volume.name}`}
                      >
                        Delete
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="text-base">Advanced mount configuration</CardTitle>
            <Button type="button" variant="outline" size="sm" onClick={() => setAdvancedOpen((value) => !value)}>
              {advancedOpen ? "Hide advanced mount configuration" : "Advanced mount configuration"}
            </Button>
          </div>
        </CardHeader>
        {advancedOpen ? (
          <CardContent className="space-y-3">
            <p className="text-sm text-yellow-700 dark:text-yellow-400">
              Enabling custom mounts may expose host filesystem paths. Only configure this when you understand the security
              implications.
            </p>
            <label htmlFor="sandbox-mount-config" className="space-y-1 text-sm">
              <span>Mount specs (display only)</span>
              <textarea
                id="sandbox-mount-config"
                className="min-h-[120px] w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-sm shadow-sm outline-none focus-visible:ring-1 focus-visible:ring-ring"
                value={mountConfig}
                onChange={(event) => setMountConfig(event.target.value)}
                placeholder="/host/path:/container/path:ro"
              />
            </label>
            <p className="text-sm text-muted-foreground">Mount configuration is display-only in this release.</p>
          </CardContent>
        ) : null}
      </Card>
    </div>
  )
}
