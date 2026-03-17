import { expect, test, type Page, type Route } from "@playwright/test"

type MockRun = {
  id: string
  instance_id?: string
  agent_id: string
  parent_run_id?: string
  session_id?: string
  source: string
  status: string
  updated_at: string
  provider?: string
  model?: string
  artifact_path?: string
  decomposition_plan?: Record<string, unknown>
}

type RunsMockOptions = {
  runs: MockRun[]
  detailsByID?: Record<string, unknown>
  traceByID?: Record<string, unknown>
  listDelayMS?: number
  detailDelayMS?: number
}

type RunsMockState = {
  listQueries: Array<Record<string, string>>
  detailRequests: Array<{ runID: string; query: Record<string, string> }>
  traceRequests: Array<{ runID: string; query: Record<string, string> }>
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({
    status,
    contentType: "application/json; charset=utf-8",
    body: JSON.stringify(body),
  })
}

function makeRun(index: number): MockRun {
  return {
    id: `run_${String(index).padStart(2, "0")}`,
    agent_id: index % 2 === 0 ? "agent-a" : "agent-b",
    source: index % 3 === 0 ? "chat" : "cron",
    status: "completed",
    updated_at: `2026-03-14T10:${String(index).padStart(2, "0")}:00Z`,
    provider: "hatz",
    model: "glm-4.5",
  }
}

async function installRunsMocks(page: Page, options: RunsMockOptions): Promise<RunsMockState> {
  const state: RunsMockState = {
    listQueries: [],
    detailRequests: [],
    traceRequests: [],
  }

  await page.route("**/*", async (route) => {
    const request = route.request()
    const method = request.method()
    const url = new URL(request.url())
    const { pathname, searchParams } = url

    if (pathname === "/api/admin/status") {
      await json(route, { model: { provider: "hatz", name: "glm-4.5" }, run_count: options.runs.length })
      return
    }

    if (pathname === "/v1/runs" && method === "GET") {
      if ((options.listDelayMS || 0) > 0) {
        await new Promise((resolve) => setTimeout(resolve, options.listDelayMS))
      }

      const queryRecord = Object.fromEntries(searchParams.entries())
      state.listQueries.push(queryRecord)

      const statusFilter = String(searchParams.get("status") || "").trim().toLowerCase()
      const agentFilter = String(searchParams.get("agent_id") || "").trim()
      const sourceFilter = String(searchParams.get("source") || "").trim()
      const limit = Number(searchParams.get("limit") || "10") || 10
      const offset = Number(searchParams.get("offset") || "0") || 0

      const filtered = options.runs.filter((run) => {
        if (statusFilter && run.status.toLowerCase() !== statusFilter) {
          return false
        }
        if (agentFilter && run.agent_id !== agentFilter) {
          return false
        }
        if (sourceFilter && run.source !== sourceFilter) {
          return false
        }
        return true
      })

      const pageRuns = filtered.slice(offset, offset + limit)
      await json(route, {
        runs: pageRuns,
        total: filtered.length,
        limit,
        offset,
      })
      return
    }

    if (pathname.startsWith("/v1/runs/") && method === "GET") {
      if ((options.detailDelayMS || 0) > 0) {
        await new Promise((resolve) => setTimeout(resolve, options.detailDelayMS))
      }

      const runID = decodeURIComponent(pathname.replace("/v1/runs/", ""))
      state.detailRequests.push({ runID, query: Object.fromEntries(searchParams.entries()) })

      const detail = options.detailsByID?.[runID]
      if (detail) {
        await json(route, detail)
        return
      }

      const baseRun = options.runs.find((run) => run.id === runID)
      if (!baseRun) {
        await json(route, { error: { code: "runs.not_found", message: "run not found" } }, 404)
        return
      }

      await json(route, baseRun)
      return
    }

    if (pathname.startsWith("/api/admin/debug/runs/") && pathname.endsWith("/trace") && method === "GET") {
      const runID = decodeURIComponent(pathname.replace("/api/admin/debug/runs/", "").replace("/trace", ""))
      state.traceRequests.push({ runID, query: Object.fromEntries(searchParams.entries()) })
      const trace = options.traceByID?.[runID]
      if (trace) {
        await json(route, trace)
        return
      }
      await json(route, { error: { code: "trace.not_found", message: "trace unavailable" } }, 404)
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

test("run list supports status/agent/source filtering with expected query params", async ({ page }) => {
  const state = await installRunsMocks(page, {
    runs: [
      {
        id: "run_a",
        agent_id: "agent-a",
        source: "chat",
        status: "queued",
        updated_at: "2026-03-14T10:00:00Z",
      },
      {
        id: "run_b",
        agent_id: "agent-b",
        source: "chat",
        status: "failed",
        updated_at: "2026-03-14T10:01:00Z",
      },
      {
        id: "run_c",
        agent_id: "agent-b",
        source: "cron",
        status: "running",
        updated_at: "2026-03-14T10:02:00Z",
      },
    ],
  })

  await page.goto("/dashboard#/runs")

  await expect(page.getByRole("heading", { name: "Runs", level: 2 })).toBeVisible()
  await expect(page.locator("#runs-status option")).toHaveText([
    "All",
    "Queued",
    "Running",
    "Completed",
    "Failed",
    "Canceled",
  ])

  await page.getByLabel("Status").selectOption("failed")
  await page.getByLabel("Agent filter").fill("agent-b")
  await page.getByLabel("Source filter").fill("chat")
  await page.getByRole("button", { name: "Apply filters" }).click()

  await expect(page.getByText("1-1 of 1")).toBeVisible()
  await expect(page.locator("[data-testid='runs-table']")).toContainText("run_b")
  await expect(page.locator("[data-testid='runs-table']")).not.toContainText("run_a")
  await expect(page.locator("[data-testid='runs-table']")).not.toContainText("run_c")

  await expect.poll(() => state.listQueries.at(-1)?.status).toBe("failed")
  await expect.poll(() => state.listQueries.at(-1)?.agent_id).toBe("agent-b")
  await expect.poll(() => state.listQueries.at(-1)?.source).toBe("chat")
})

test("page size selector and Prev/Next pagination update list and page meta", async ({ page }) => {
  const runs = Array.from({ length: 26 }, (_, index) => makeRun(index + 1))
  const state = await installRunsMocks(page, { runs })

  await page.goto("/dashboard#/runs")

  await expect(page.getByText("1-10 of 26")).toBeVisible()
  await page.getByRole("button", { name: "Next" }).click()
  await expect(page.getByText("11-20 of 26")).toBeVisible()

  await page.getByRole("button", { name: "Next" }).click()
  await expect(page.getByText("21-26 of 26")).toBeVisible()

  await page.getByRole("button", { name: "Prev" }).click()
  await expect(page.getByText("11-20 of 26")).toBeVisible()

  await page.getByLabel("Page size").selectOption("25")
  await expect(page.getByText("1-25 of 26")).toBeVisible()
  await expect.poll(() => state.listQueries.at(-1)?.limit).toBe("25")
  await expect.poll(() => state.listQueries.at(-1)?.offset).toBe("0")
})

test("opening a run shows summary, payload JSON, model steps, timeline, and clickable tool inspection", async ({ page }) => {
  await installRunsMocks(page, {
    runs: [
      {
        id: "run_1",
        instance_id: "lab",
        agent_id: "default",
        parent_run_id: "run_parent",
        session_id: "session-123",
        source: "chat",
        status: "failed",
        updated_at: "2026-03-14T12:10:00Z",
        provider: "hatz",
        model: "glm-4.5",
        artifact_path: "artifacts/run_1",
        decomposition_plan: { delegation_mode: "approve_plan", tasks: [{ task_id: "task-1" }, { task_id: "task-2" }] },
      },
    ],
    detailsByID: {
      run_1: {
        id: "run_1",
        instance_id: "lab",
        agent_id: "default",
        parent_run_id: "run_parent",
        session_id: "session-123",
        source: "chat",
        status: "failed",
        updated_at: "2026-03-14T12:10:00Z",
        provider: "hatz",
        model: "glm-4.5",
        artifact_path: "artifacts/run_1",
        decomposition_plan: { delegation_mode: "approve_plan", tasks: [{ task_id: "task-1" }, { task_id: "task-2" }] },
      },
    },
    traceByID: {
      run_1: {
        trace: {
          run_id: "run_1",
          model_inputs: [
            { iteration: 1, prompt_length: 120, history_injected: false, message: "Inspect repository structure" },
            { iteration: 2, prompt_length: 245, history_injected: true, message: "Attempt to apply edits" },
          ],
          tool_execution_results: [
            {
              tool: "fs.edit",
              tool_call_id: "tool-1",
              duration_ms: 31,
              arguments: JSON.stringify({ path: "README.md" }),
              output: "",
              error: "missing edits",
            },
            {
              tool: "fs.edit",
              tool_call_id: "tool-2",
              duration_ms: 29,
              arguments: JSON.stringify({ path: "README.md" }),
              output: "",
              error: "missing edits",
            },
            {
              tool: "fs.read",
              tool_call_id: "tool-3",
              duration_ms: 12,
              arguments: JSON.stringify({ path: "README.md" }),
              output: "file content",
              error: "",
            },
          ],
        },
      },
    },
  })

  await page.goto("/dashboard#/runs")
  await page.getByRole("button", { name: "Open" }).click()

  await expect(page).toHaveURL(/#\/runs\/run_1$/)
  await expect(page.getByText("hatz / glm-4.5")).toBeVisible()
  await expect(page.locator("[data-testid='run-identity-summary']")).toContainText("lab")
  await expect(page.locator("[data-testid='run-identity-summary']")).toContainText("run_parent")
  await expect(page.locator("[data-testid='run-identity-summary']")).toContainText("session-123")
  await expect(page.locator("[data-testid='run-identity-summary']")).toContainText("artifacts/run_1")
  await expect(page.locator("[data-testid='run-identity-summary']")).toContainText("approve_plan - 2 tasks")
  await expect(page.getByText("Full payload")).toBeVisible()
  await expect(page.locator("[data-testid='run-payload-json'] pre")).toContainText('"id": "run_1"')

  await expect(page.getByRole("heading", { name: "Model Steps", level: 3 })).toBeVisible()
  await expect(page.getByText("#1 · prompt 120 chars")).toBeVisible()

  await expect(page.getByRole("heading", { name: "Tool Calls", level: 3 })).toBeVisible()
  await expect(page.getByText("2 repeated failures · fs.edit · missing edits")).toBeVisible()
  await page.getByText("2 repeated failures · fs.edit · missing edits").click()
  await page.locator("[data-testid='tool-timeline'] button", { hasText: "fs.edit" }).first().click()

  const inspectionPanel = page.locator("[data-testid='tool-inspection-panel']")
  await expect(inspectionPanel).toContainText("Tool inspection")
  await expect(inspectionPanel).toContainText("fs.edit")
  await expect(inspectionPanel).toContainText("failed")
  await expect(inspectionPanel).toContainText("missing edits")
})

test("deep link /#/runs/{id} loads run detail and shows loading states", async ({ page }) => {
  const state = await installRunsMocks(page, {
    runs: [
      {
        id: "run_deep",
        agent_id: "default",
        source: "chat",
        status: "completed",
        updated_at: "2026-03-14T13:00:00Z",
      },
    ],
    detailsByID: {
      run_deep: {
        id: "run_deep",
        agent_id: "default",
        source: "chat",
        status: "completed",
        updated_at: "2026-03-14T13:00:00Z",
      },
    },
    traceByID: {
      run_deep: {
        trace: {
          run_id: "run_deep",
          model_inputs: [],
          tool_execution_results: [],
        },
      },
    },
    listDelayMS: 250,
    detailDelayMS: 250,
  })

  await page.goto("/dashboard#/runs/run_deep")

  await expect(page.locator("[data-testid='runs-list-loading']")).toBeVisible()
  await expect(page.locator("[data-testid='run-detail-loading']")).toBeVisible()

  await expect(page.getByText("run_deep")).toBeVisible()
  await expect.poll(() => state.detailRequests.map((entry) => entry.runID)).toContain("run_deep")
  await expect.poll(() => state.traceRequests.map((entry) => entry.runID)).toContain("run_deep")
})

test("deep link /#/runs?run={id} loads run detail from hash params", async ({ page }) => {
  const state = await installRunsMocks(page, {
    runs: [
      {
        id: "run_query",
        agent_id: "default",
        source: "chat",
        status: "completed",
        updated_at: "2026-03-14T13:30:00Z",
      },
    ],
    detailsByID: {
      run_query: {
        id: "run_query",
        agent_id: "default",
        source: "chat",
        status: "completed",
        updated_at: "2026-03-14T13:30:00Z",
      },
    },
    traceByID: {
      run_query: {
        trace: {
          run_id: "run_query",
          model_inputs: [],
          tool_execution_results: [],
        },
      },
    },
  })

  await page.goto("/dashboard#/runs?run=run_query")

	await expect(page.locator("[data-testid='run-detail-panel']")).toContainText("run_query")
  await expect.poll(() => state.detailRequests.map((entry) => entry.runID)).toContain("run_query")
  await expect.poll(() => state.traceRequests.map((entry) => entry.runID)).toContain("run_query")
})

test("run detail threads composite identity into detail and trace requests", async ({ page }) => {
  const state = await installRunsMocks(page, {
    runs: [
      {
        id: "run_dup",
        instance_id: "instance-a",
        agent_id: "agent-7",
        source: "chat",
        status: "completed",
        updated_at: "2026-03-14T13:40:00Z",
      },
    ],
    detailsByID: {
      run_dup: {
        id: "run_dup",
        instance_id: "instance-a",
        agent_id: "agent-7",
        source: "chat",
        status: "completed",
        updated_at: "2026-03-14T13:40:00Z",
      },
    },
    traceByID: {
      run_dup: {
        trace: {
          run_id: "run_dup",
          model_inputs: [],
          tool_execution_results: [],
        },
      },
    },
  })

  await page.goto("/dashboard#/runs")
  await page.getByRole("button", { name: "Open" }).click()

  await expect.poll(() => state.detailRequests.at(-1)?.query.instance_id).toBe("instance-a")
  await expect.poll(() => state.detailRequests.at(-1)?.query.agent_id).toBe("agent-7")
  await expect.poll(() => state.traceRequests.at(-1)?.query.instance_id).toBe("instance-a")
  await expect.poll(() => state.traceRequests.at(-1)?.query.agent_id).toBe("agent-7")
})
