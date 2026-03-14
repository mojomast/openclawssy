import { type FormEvent, useCallback, useEffect, useMemo, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { ApiError, api } from "@/lib/api"

type SchedulerJob = {
  id: string
  agentID: string
  schedule: string
  message: string
  enabled: boolean
  lastRun: string
}

type SchedulerJobsResponse = {
  paused?: unknown
  jobs?: unknown
}

type AddJobResponse = {
  id?: unknown
}

type AddFormState = {
  id: string
  agentID: string
  schedule: string
  message: string
  enabled: boolean
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

function normalizeJob(value: unknown): SchedulerJob | null {
  const raw = asRecord(value)
  if (!raw) {
    return null
  }

  const id = asText(raw.id).trim()
  if (!id) {
    return null
  }

  const agentID = asText(raw.agentID ?? raw.agent_id).trim() || "default"
  return {
    id,
    agentID,
    schedule: asText(raw.schedule).trim(),
    message: asText(raw.message).trim(),
    enabled: asBoolean(raw.enabled),
    lastRun: asText(raw.lastRun ?? raw.last_run).trim(),
  }
}

function normalizeJobsPayload(payload: SchedulerJobsResponse): { paused: boolean; jobs: SchedulerJob[] } {
  const jobs = Array.isArray(payload.jobs)
    ? payload.jobs.map(normalizeJob).filter((job): job is SchedulerJob => job !== null)
    : []

  jobs.sort((left, right) => left.id.localeCompare(right.id))
  return {
    paused: asBoolean(payload.paused),
    jobs,
  }
}

function createDefaultAddForm(): AddFormState {
  return {
    id: "",
    agentID: "",
    schedule: "",
    message: "",
    enabled: true,
  }
}

export function SchedulerPage() {
  const [jobs, setJobs] = useState<SchedulerJob[]>([])
  const [paused, setPaused] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState("")

  const [notice, setNotice] = useState("")
  const [noticeKind, setNoticeKind] = useState<"success" | "error" | "">("")

  const [pendingActions, setPendingActions] = useState<Record<string, boolean>>({})
  const [addForm, setAddForm] = useState<AddFormState>(() => createDefaultAddForm())

  const setPending = useCallback((actionKey: string, isPending: boolean) => {
    setPendingActions((current) => {
      if (isPending) {
        if (current[actionKey]) {
          return current
        }
        return { ...current, [actionKey]: true }
      }
      if (!current[actionKey]) {
        return current
      }
      const next = { ...current }
      delete next[actionKey]
      return next
    })
  }, [])

  const isPending = useCallback(
    (actionKey: string) => {
      return Boolean(pendingActions[actionKey])
    },
    [pendingActions]
  )

  const loadJobs = useCallback(async (options?: { keepNotice?: boolean }) => {
    setLoading(true)
    setLoadError("")
    if (!options?.keepNotice) {
      setNotice("")
      setNoticeKind("")
    }

    try {
      const payload = await api.get<SchedulerJobsResponse>("/api/admin/scheduler/jobs")
      const normalized = normalizeJobsPayload(payload)
      setPaused(normalized.paused)
      setJobs(normalized.jobs)
    } catch (error) {
      setLoadError(extractErrorMessage(error))
    } finally {
      setLoading(false)
    }
  }, [])

  const runAction = useCallback(
    async (actionKey: string, task: () => Promise<void>) => {
      setPending(actionKey, true)
      setNotice("")
      setNoticeKind("")

      try {
        await task()
      } finally {
        setPending(actionKey, false)
      }
    },
    [setPending]
  )

  useEffect(() => {
    void loadJobs()
  }, [loadJobs])

  const jobCountLabel = useMemo(() => `${jobs.length} total`, [jobs.length])

  const onSubmitAddJob = useCallback(
    async (event: FormEvent<HTMLFormElement>) => {
      event.preventDefault()

      const id = asText(addForm.id).trim()
      const agentID = asText(addForm.agentID).trim()
      const schedule = asText(addForm.schedule).trim()
      const message = asText(addForm.message).trim()
      const enabled = Boolean(addForm.enabled)

      if (!schedule || !message) {
        setNoticeKind("error")
        setNotice("Schedule and message are required.")
        return
      }

      const body: Record<string, unknown> = {
        schedule,
        message,
        enabled,
      }

      if (id) {
        body.id = id
      }
      if (agentID) {
        body.agent_id = agentID
      }

      await runAction("add", async () => {
        try {
          const payload = await api.post<AddJobResponse>("/api/admin/scheduler/jobs", body)
          const createdID = asText(payload.id || id).trim()
          setNoticeKind("success")
          setNotice(createdID ? `Added scheduler job: ${createdID}` : "Added scheduler job.")
          setAddForm(createDefaultAddForm())
          await loadJobs({ keepNotice: true })
        } catch (error) {
          setNoticeKind("error")
          setNotice(`Failed to add job: ${extractErrorMessage(error)}`)
        }
      })
    },
    [addForm, loadJobs, runAction]
  )

  const setGlobalPaused = useCallback(
    async (nextPaused: boolean) => {
      const action = nextPaused ? "pause" : "resume"
      await runAction(`global:${action}`, async () => {
        try {
          await api.post("/api/admin/scheduler/control", { action })
          setNoticeKind("success")
          setNotice(nextPaused ? "Scheduler paused globally." : "Scheduler resumed globally.")
          await loadJobs({ keepNotice: true })
        } catch (error) {
          setNoticeKind("error")
          setNotice(`Failed to ${action} scheduler: ${extractErrorMessage(error)}`)
        }
      })
    },
    [loadJobs, runAction]
  )

  const setJobEnabled = useCallback(
    async (jobID: string, enabled: boolean) => {
      const action = enabled ? "resume" : "pause"
      await runAction(`job:${jobID}:${action}`, async () => {
        try {
          await api.post("/api/admin/scheduler/control", {
            action,
            job_id: jobID,
          })
          setNoticeKind("success")
          setNotice(enabled ? `Enabled job: ${jobID}` : `Disabled job: ${jobID}`)
          await loadJobs({ keepNotice: true })
        } catch (error) {
          setNoticeKind("error")
          setNotice(`Failed to update job ${jobID}: ${extractErrorMessage(error)}`)
        }
      })
    },
    [loadJobs, runAction]
  )

  const deleteJob = useCallback(
    async (jobID: string) => {
      await runAction(`delete:${jobID}`, async () => {
        try {
          await api.delete(`/api/admin/scheduler/jobs/${encodeURIComponent(jobID)}`)
          setNoticeKind("success")
          setNotice(`Deleted job: ${jobID}`)
          await loadJobs({ keepNotice: true })
        } catch (error) {
          setNoticeKind("error")
          setNotice(`Failed to delete job ${jobID}: ${extractErrorMessage(error)}`)
        }
      })
    },
    [loadJobs, runAction]
  )

  const globalActionPending = isPending("global:pause") || isPending("global:resume")

  return (
    <div className="space-y-4 p-6" data-testid="scheduler-page">
      <div className="space-y-2">
        <h2 className="text-2xl font-semibold tracking-tight">Scheduler</h2>
        <p className="text-sm text-muted-foreground">
          Manage scheduler jobs, pause or resume globally, and control enabled state per job.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Global scheduler state</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <Badge variant={paused ? "outline" : "secondary"}>{paused ? "Paused" : "Running"}</Badge>
            <div className="flex flex-wrap items-center gap-2">
              <Button type="button" variant="outline" onClick={() => void loadJobs({ keepNotice: true })} disabled={loading}>
                {loading ? "Refreshing..." : "Refresh jobs"}
              </Button>
              <Button
                type="button"
                onClick={() => void setGlobalPaused(!paused)}
                disabled={globalActionPending}
              >
                {globalActionPending ? "Saving..." : paused ? "Resume scheduler" : "Pause scheduler"}
              </Button>
            </div>
          </div>

          {notice ? (
            <p className={noticeKind === "error" ? "text-sm text-destructive" : "text-sm text-muted-foreground"}>{notice}</p>
          ) : null}
          {loadError ? <p className="text-sm text-destructive">Failed to load jobs: {loadError}</p> : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Add scheduler job</CardTitle>
          <p className="text-sm text-muted-foreground">
            Provide a duration schedule (for example @every 5m) or RFC3339 timestamp for one-shot jobs.
          </p>
        </CardHeader>
        <CardContent>
          <form className="space-y-4" onSubmit={(event) => void onSubmitAddJob(event)}>
            <div className="grid gap-3 md:grid-cols-2">
              <label htmlFor="scheduler-job-id" className="space-y-1 text-sm">
                <span>Job ID (optional)</span>
                <Input
                  id="scheduler-job-id"
                  placeholder="job_custom_id"
                  value={addForm.id}
                  onChange={(event) => setAddForm((current) => ({ ...current, id: event.target.value }))}
                />
              </label>

              <label htmlFor="scheduler-agent-id" className="space-y-1 text-sm">
                <span>agent_id</span>
                <Input
                  id="scheduler-agent-id"
                  placeholder="default"
                  value={addForm.agentID}
                  onChange={(event) => setAddForm((current) => ({ ...current, agentID: event.target.value }))}
                />
              </label>

              <label htmlFor="scheduler-schedule" className="space-y-1 text-sm">
                <span>schedule</span>
                <Input
                  id="scheduler-schedule"
                  placeholder="@every 5m"
                  value={addForm.schedule}
                  onChange={(event) => setAddForm((current) => ({ ...current, schedule: event.target.value }))}
                />
              </label>

              <label htmlFor="scheduler-message" className="space-y-1 text-sm">
                <span>message</span>
                <textarea
                  id="scheduler-message"
                  className="min-h-[88px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  placeholder="status ping"
                  value={addForm.message}
                  onChange={(event) => setAddForm((current) => ({ ...current, message: event.target.value }))}
                />
              </label>
            </div>

            <label htmlFor="scheduler-enabled" className="inline-flex items-center gap-2 text-sm">
              <input
                id="scheduler-enabled"
                type="checkbox"
                checked={addForm.enabled}
                onChange={(event) => setAddForm((current) => ({ ...current, enabled: event.target.checked }))}
              />
              <span>enabled</span>
            </label>

            <Button type="submit" disabled={isPending("add")}>{isPending("add") ? "Adding..." : "Add job"}</Button>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-2">
            <CardTitle className="text-base">Scheduled jobs</CardTitle>
            <p className="text-sm text-muted-foreground">{jobCountLabel}</p>
          </div>
        </CardHeader>
        <CardContent>
          {loading ? <p className="text-sm text-muted-foreground">Loading scheduler jobs...</p> : null}

          {!loading && jobs.length === 0 ? (
            <p className="text-sm text-muted-foreground">No jobs found. Add a job to start using scheduler automation.</p>
          ) : null}

          {!loading && jobs.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Job</TableHead>
                  <TableHead>Agent</TableHead>
                  <TableHead>Schedule</TableHead>
                  <TableHead>Message</TableHead>
                  <TableHead>Enabled</TableHead>
                  <TableHead>Last run</TableHead>
                  <TableHead>Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {jobs.map((job) => {
                  const toggleKey = `job:${job.id}:${job.enabled ? "pause" : "resume"}`
                  const deleteKey = `delete:${job.id}`

                  return (
                    <TableRow key={job.id}>
                      <TableCell>
                        <code>{job.id}</code>
                      </TableCell>
                      <TableCell>{job.agentID || "default"}</TableCell>
                      <TableCell>{job.schedule || "-"}</TableCell>
                      <TableCell>{job.message || "-"}</TableCell>
                      <TableCell>
                        <Badge variant={job.enabled ? "secondary" : "outline"}>{job.enabled ? "Enabled" : "Disabled"}</Badge>
                      </TableCell>
                      <TableCell>{job.lastRun || "Never"}</TableCell>
                      <TableCell>
                        <div className="flex flex-wrap items-center gap-2">
                          <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            disabled={isPending(toggleKey)}
                            onClick={() => void setJobEnabled(job.id, !job.enabled)}
                          >
                            {isPending(toggleKey) ? "Saving..." : job.enabled ? "Disable" : "Enable"}
                          </Button>
                          <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            disabled={isPending(deleteKey)}
                            onClick={() => void deleteJob(job.id)}
                          >
                            {isPending(deleteKey) ? "Deleting..." : "Delete"}
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          ) : null}
        </CardContent>
      </Card>
    </div>
  )
}
