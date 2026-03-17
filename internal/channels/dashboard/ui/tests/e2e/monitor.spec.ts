import { expect, test, type Page, type Route } from "@playwright/test"

type MonitorMockState = {
  agentsCalls: number
  runsCalls: number
  refreshCalls: number
  startPayloads: Array<Record<string, unknown>>
  controlPayloads: Array<Record<string, unknown>>
  agentRequestURLs: string[]
}

type MonitorMockOptions = {
  agentPayloadForCall?: (callNumber: number) => unknown
  runsPayloadForCall?: (callNumber: number) => unknown
  instanceAgentsEnabled?: boolean
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({
    status,
    contentType: "application/json; charset=utf-8",
    body: JSON.stringify(body),
  })
}

function defaultAgentPayload() {
  return {
    agents: ["default", "ops"],
    agent_summaries: {
      default: {
        self_improvement_ready: true,
        activated_skills: ["triage", "verify"],
        model_provider: "hatz",
        model_name: "glm-4.5",
      },
      ops: {
        self_improvement_ready: false,
        activated_skills: ["review"],
        model_provider: "openai",
        model_name: "gpt-4.1-mini",
      },
    },
  }
}

function defaultRunsPayload() {
  return {
    runs: [
      {
        run_id: "run_main_1",
        instance_id: "lab",
        agent_id: "default",
        role: "main",
        status: "completed",
        task_id: "main-task-1",
        started_at: "2026-03-14T12:00:00Z",
      },
      {
        run_id: "run_sub_1",
        instance_id: "lab",
        agent_id: "default",
        role: "subagent",
        status: "running",
        task_id: "sub-task-1",
        started_at: "2026-03-14T12:01:00Z",
      },
      {
        run_id: "run_ops_1",
        instance_id: "lab",
        agent_id: "ops",
        role: "main",
        status: "running",
        task_id: "ops-task-1",
        started_at: "2026-03-14T12:02:00Z",
      },
    ],
    available_agents: ["default", "ops"],
  }
}

async function installMonitorMocks(page: Page, options?: MonitorMockOptions): Promise<MonitorMockState> {
  const state: MonitorMockState = {
    agentsCalls: 0,
    runsCalls: 0,
    refreshCalls: 0,
    startPayloads: [],
    controlPayloads: [],
    agentRequestURLs: [],
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

    if (pathname === "/api/admin/control-plane/features" && method === "GET") {
      await json(route, {
        features: {
          instance_control: true,
          instance_agents: options?.instanceAgentsEnabled ?? true,
          wizard: true,
          eval: true,
        },
      })
      return
    }

    if (pathname === "/api/admin/agents" && method === "GET") {
      state.agentsCalls += 1
      state.agentRequestURLs.push(request.url())
      const payload = options?.agentPayloadForCall?.(state.agentsCalls) || defaultAgentPayload()
      await json(route, payload)
      return
    }

    if (pathname === "/api/admin/instances/active" && method === "GET") {
      await json(route, { instance: { id: "lab", name: "Lab" } })
      return
    }

    if (pathname === "/api/admin/monitor/runs" && method === "GET") {
      state.runsCalls += 1
      state.refreshCalls += 1
      const payload = options?.runsPayloadForCall?.(state.runsCalls) || defaultRunsPayload()
      await json(route, payload)
      return
    }

    if (pathname === "/v1/runs" && method === "POST") {
      const payload = request.postDataJSON() as Record<string, unknown>
      state.startPayloads.push(payload)
      await json(route, { id: "run_started_from_ui" })
      return
    }

    if (pathname === "/api/admin/monitor/runs/control" && method === "POST") {
      const payload = request.postDataJSON() as Record<string, unknown>
      state.controlPayloads.push(payload)
      await json(route, { cancelled: true, tracked: true, run_id: String(payload.run_id || "") })
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

test("agent monitor shows cards, run table, and auto-polls every 2.5s", async ({ page }) => {
  const state = await installMonitorMocks(page, {
    runsPayloadForCall: (callNumber) => {
      if (callNumber >= 2) {
        return {
          runs: [
            {
              run_id: "run_main_1",
              instance_id: "lab",
              agent_id: "default",
              role: "main",
              status: "completed",
              task_id: "main-task-1",
              started_at: "2026-03-14T12:00:00Z",
            },
            {
              run_id: "run_sub_1",
              instance_id: "lab",
              agent_id: "default",
              role: "subagent",
              status: "completed",
              task_id: "sub-task-1",
              started_at: "2026-03-14T12:01:00Z",
            },
            {
              run_id: "run_poll_new",
              instance_id: "lab",
              agent_id: "ops",
              role: "main",
              status: "running",
              task_id: "ops-task-poll",
              started_at: "2026-03-14T12:03:00Z",
            },
          ],
          available_agents: ["default", "ops"],
        }
      }
      return defaultRunsPayload()
    },
  })

  await page.goto("/dashboard#/monitor")

  await expect(page.getByRole("heading", { name: "Agent Monitor", level: 2 })).toBeVisible()
  await expect(page.getByText("3 recent internal runs tracked", { exact: false })).toBeVisible()

  const defaultCard = page.locator("[data-testid='monitor-agent-card-default']")
  await expect(defaultCard).toContainText("main: 1 · subagent: 1")
  await expect(defaultCard).toContainText("active:")
  await expect(defaultCard).toContainText("self-improvement: ready")
  await expect(defaultCard).toContainText("skills: triage, verify")
  await expect(defaultCard).toContainText("model: hatz/glm-4.5")

  await expect(page.locator("[data-testid='monitor-runs-table']")).toContainText("run_main_1")
  await expect(page.locator("[data-testid='monitor-runs-table']")).toContainText("run_sub_1")
  await expect(page.locator("[data-testid='monitor-runs-table']")).toContainText("ops")

  await expect.poll(() => state.runsCalls, { timeout: 10000 }).toBeGreaterThan(1)
  await expect(page.locator("[data-testid='monitor-runs-table']")).toContainText("run_poll_new")
})

test("start run, stop active, per-row stop, thinking mode, and refresh-now interactions work", async ({ page }) => {
  const state = await installMonitorMocks(page)

  await page.goto("/dashboard#/monitor")

  await expect.poll(() => state.agentRequestURLs.some((url) => url.includes("instance_id=lab"))).toBeTruthy()

  await page.getByLabel("Launch prompt").fill("Investigate monitor behavior")
  await page.getByLabel("Thinking mode").selectOption("on_error")

  const opsCard = page.locator("[data-testid='monitor-agent-card-ops']")
  await opsCard.getByRole("button", { name: "Start" }).click()

  await expect.poll(() => state.startPayloads.length).toBe(1)
  expect(state.startPayloads[0]).toEqual({
    instance_id: "lab",
    agent_id: "ops",
    message: "Investigate monitor behavior",
    thinking_mode: "on_error",
  })

  const defaultCard = page.locator("[data-testid='monitor-agent-card-default']")
  await defaultCard.getByRole("button", { name: "Stop active" }).click()

  await expect.poll(() => state.controlPayloads.length).toBe(1)
  expect(state.controlPayloads[0]).toEqual({
    action: "cancel",
    instance_id: "lab",
    agent_id: "default",
    run_id: "run_sub_1",
  })

  const row = page.locator("[data-testid='monitor-run-row-lab-ops-run_ops_1']")
  await row.getByRole("button", { name: "Stop" }).click()

  await expect.poll(() => state.controlPayloads.length).toBe(2)
  expect(state.controlPayloads[1]).toEqual({
    action: "cancel",
    instance_id: "lab",
    agent_id: "ops",
    run_id: "run_ops_1",
  })

  const beforeRefresh = state.refreshCalls
  await page.getByRole("button", { name: "Refresh now" }).click()
  await expect.poll(() => state.refreshCalls).toBeGreaterThan(beforeRefresh)
})

test("Agent Monitor hides nav entry and shows disabled state when instance agents feature is off", async ({ page }) => {
  await installMonitorMocks(page, { instanceAgentsEnabled: false })

  await page.goto("/dashboard#/monitor")

  await expect(page.getByRole("link", { name: "Monitor" })).toHaveCount(0)
  await expect(page.getByRole("link", { name: "Agent Contract" })).toHaveCount(0)
  await expect(page.getByRole("link", { name: "Prompt Stack" })).toHaveCount(0)
  await expect(page.getByTestId("monitor-disabled-state")).toContainText("Agent Monitor disabled")
  await expect(page.getByTestId("monitor-disabled-state")).toContainText("Instance agent controls are disabled")
  await expect(page.getByLabel("Launch prompt")).toBeDisabled()
  await expect(page.getByLabel("Thinking mode")).toBeDisabled()
  await expect(page.getByRole("button", { name: "Refresh now" })).toBeDisabled()
  await expect(page.getByTestId("monitor-summary")).toHaveCount(0)
  await expect(page.locator("[data-testid^='monitor-agent-card-']")).toHaveCount(0)
  await expect(page.getByTestId("monitor-runs-table")).toHaveCount(0)
})
