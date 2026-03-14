import { expect, test, type Page, type Route } from "@playwright/test"

type SchedulerJob = {
  id: string
  agent_id: string
  schedule: string
  message: string
  enabled: boolean
  last_run?: string
}

type SchedulerMockState = {
  paused: boolean
  jobs: SchedulerJob[]
  listCalls: number
  addPayloads: Array<Record<string, unknown>>
  controlPayloads: Array<Record<string, unknown>>
  deleteIDs: string[]
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({
    status,
    contentType: "application/json; charset=utf-8",
    body: JSON.stringify(body),
  })
}

async function installSchedulerMocks(page: Page): Promise<SchedulerMockState> {
  const state: SchedulerMockState = {
    paused: false,
    jobs: [
      {
        id: "job_alpha",
        agent_id: "default",
        schedule: "@every 5m",
        message: "status ping",
        enabled: true,
        last_run: "2026-03-14T12:00:00Z",
      },
    ],
    listCalls: 0,
    addPayloads: [],
    controlPayloads: [],
    deleteIDs: [],
  }

  await page.route("**/*", async (route) => {
    const request = route.request()
    const method = request.method()
    const url = new URL(request.url())
    const { pathname } = url

    if (pathname === "/api/admin/status") {
      await json(route, { ok: true, model: { provider: "hatz", name: "glm-4.5" }, run_count: 3 })
      return
    }

    if (pathname === "/api/admin/scheduler/jobs" && method === "GET") {
      state.listCalls += 1
      await json(route, { paused: state.paused, jobs: state.jobs })
      return
    }

    if (pathname === "/api/admin/scheduler/jobs" && method === "POST") {
      const payload = request.postDataJSON() as Record<string, unknown>
      state.addPayloads.push(payload)
      const id = String(payload.id || `job_${state.jobs.length + 1}`).trim() || `job_${state.jobs.length + 1}`
      const job: SchedulerJob = {
        id,
        agent_id: String(payload.agent_id || "default").trim() || "default",
        schedule: String(payload.schedule || "@every 1m").trim(),
        message: String(payload.message || "").trim(),
        enabled: payload.enabled === undefined ? true : Boolean(payload.enabled),
      }
      state.jobs = [...state.jobs.filter((entry) => entry.id !== id), job]
      await json(route, { ok: true, id })
      return
    }

    if (pathname === "/api/admin/scheduler/control" && method === "POST") {
      const payload = request.postDataJSON() as Record<string, unknown>
      state.controlPayloads.push(payload)
      const action = String(payload.action || "").trim()
      const jobID = String(payload.job_id || "").trim()
      if (jobID) {
        state.jobs = state.jobs.map((job) => {
          if (job.id !== jobID) {
            return job
          }
          return { ...job, enabled: action === "resume" }
        })
      } else {
        state.paused = action === "pause"
      }
      await json(route, { ok: true })
      return
    }

    if (pathname.startsWith("/api/admin/scheduler/jobs/") && method === "DELETE") {
      const id = decodeURIComponent(pathname.split("/").pop() || "")
      state.deleteIDs.push(id)
      state.jobs = state.jobs.filter((job) => job.id !== id)
      await json(route, { ok: true, removed: id })
      return
    }

    if (pathname.startsWith("/api/") || pathname.startsWith("/v1/")) {
      await json(route, { ok: true })
      return
    }

    await route.continue()
  })

  return state
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("openclawssy.dashboard.bearer", "e2e-token")
  })
})

test("scheduler page shows global state, jobs table, and refresh", async ({ page }) => {
  const state = await installSchedulerMocks(page)

  await page.goto("/dashboard#/scheduler")

  await expect(page.getByRole("heading", { name: "Scheduler", level: 2 })).toBeVisible()
  await expect(page.getByText("Global scheduler state")).toBeVisible()
  await expect(page.getByText("Running").first()).toBeVisible()
  await expect(page.locator("table")).toContainText("job_alpha")
  await expect(page.locator("table")).toContainText("@every 5m")
  await expect(page.locator("table")).toContainText("Disable")

  const listCallsBeforeRefresh = state.listCalls
  await page.getByRole("button", { name: "Refresh jobs" }).click()
  await expect.poll(() => state.listCalls).toBeGreaterThan(listCallsBeforeRefresh)
})

test("pause and resume update scheduler global state with status feedback", async ({ page }) => {
  const state = await installSchedulerMocks(page)

  await page.goto("/dashboard#/scheduler")

  await page.getByRole("button", { name: "Pause scheduler" }).click()
  await expect.poll(() => state.controlPayloads.length).toBe(1)
  expect(state.controlPayloads[0]).toEqual({ action: "pause" })
  await expect(page.getByText("Scheduler paused globally.")).toBeVisible()
  await expect(page.getByText("Paused").first()).toBeVisible()

  await page.getByRole("button", { name: "Resume scheduler" }).click()
  await expect.poll(() => state.controlPayloads.length).toBe(2)
  expect(state.controlPayloads[1]).toEqual({ action: "resume" })
  await expect(page.getByText("Scheduler resumed globally.")).toBeVisible()
  await expect(page.getByText("Running").first()).toBeVisible()
})

test("add, enable/disable, and delete job operations work", async ({ page }) => {
  const state = await installSchedulerMocks(page)

  await page.goto("/dashboard#/scheduler")

  await page.getByLabel("Job ID (optional)").fill("job_manual")
  await page.getByLabel("agent_id").fill("ops")
  await page.getByLabel("schedule").fill("@every 10m")
  await page.getByLabel("message").fill("check queue")
  await page.getByLabel("enabled").uncheck()
  await page.getByRole("button", { name: "Add job" }).click()

  await expect.poll(() => state.addPayloads.length).toBe(1)
  expect(state.addPayloads[0]).toEqual({
    id: "job_manual",
    agent_id: "ops",
    schedule: "@every 10m",
    message: "check queue",
    enabled: false,
  })

  await expect(page.getByText("Added scheduler job: job_manual")).toBeVisible()
  await expect(page.locator("table")).toContainText("job_manual")
  await expect(page.locator("table")).toContainText("Disabled")

  const row = page.locator("tr", { hasText: "job_manual" })
  await row.getByRole("button", { name: "Enable" }).click()
  await expect.poll(() => state.controlPayloads.length).toBe(1)
  expect(state.controlPayloads[0]).toEqual({ action: "resume", job_id: "job_manual" })
  await expect(page.getByText("Enabled job: job_manual")).toBeVisible()

  await row.getByRole("button", { name: "Delete" }).click()
  await expect.poll(() => state.deleteIDs).toContain("job_manual")
  await expect(page.getByText("Deleted job: job_manual")).toBeVisible()
  await expect(page.locator("table")).not.toContainText("job_manual")
})

test("form validation prevents empty schedule/message submissions", async ({ page }) => {
  const state = await installSchedulerMocks(page)

  await page.goto("/dashboard#/scheduler")

  await page.getByRole("button", { name: "Add job" }).click()
  await expect(page.getByText("Schedule and message are required.")).toBeVisible()
  expect(state.addPayloads).toHaveLength(0)
})
