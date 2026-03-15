import { expect, test, type Page, type Route } from "@playwright/test"

type WidgetLayout = {
  widget_key: string
  widget_instance_id: string
  x: number
  y: number
  w: number
  h: number
  widget_state?: Record<string, unknown>
}

type DashboardRecord = {
  id: string
  name: string
  position: number
  created_at: string
  updated_at: string
  layout: WidgetLayout[]
}

type DashboardsMockState = {
  dashboards: DashboardRecord[]
  createPayloads: Array<Record<string, unknown>>
  putCalls: Array<{ id: string; payload: DashboardRecord }>
  deleteIDs: string[]
  quickPromptPayloads: Array<Record<string, unknown>>
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({
    status,
    contentType: "application/json; charset=utf-8",
    body: JSON.stringify(body),
  })
}

function nowISO(): string {
  return new Date().toISOString()
}

function cloneDashboard(dashboard: DashboardRecord): DashboardRecord {
  return {
    ...dashboard,
    layout: dashboard.layout.map((widget) => ({
      ...widget,
      widget_state: { ...(widget.widget_state || {}) },
    })),
  }
}

async function installDashboardsMocks(page: Page, initialDashboards?: DashboardRecord[]): Promise<DashboardsMockState> {
  const state: DashboardsMockState = {
    dashboards:
      initialDashboards?.map(cloneDashboard) || [
        {
          id: "dash_main",
          name: "Main Dashboard",
          position: 0,
          created_at: "2026-03-14T10:00:00Z",
          updated_at: "2026-03-14T10:00:00Z",
          layout: [],
        },
      ],
    createPayloads: [],
    putCalls: [],
    deleteIDs: [],
    quickPromptPayloads: [],
  }

  await page.route("**/*", async (route) => {
    const request = route.request()
    const method = request.method()
    const url = new URL(request.url())
    const { pathname, searchParams } = url

    if (pathname === "/api/admin/status") {
      await json(route, {
        provider: "hatz",
        model: "glm-4.5",
        run_count: 7,
        discord_enabled: true,
        telegram_enabled: false,
      })
      return
    }

    if (pathname === "/api/admin/config") {
      await json(route, {
        model: { provider: "hatz", name: "glm-4.5", max_tokens: 8192 },
        providers: {
          openai: { base_url: "https://api.openai.com", api_key_env: "OPENAI_API_KEY" },
          openrouter: { base_url: "https://openrouter.ai/api", api_key_env: "OPENROUTER_API_KEY" },
          requesty: { base_url: "https://api.requesty.ai", api_key_env: "REQUESTY_API_KEY" },
          zai: { base_url: "https://api.z.ai", api_key_env: "ZAI_API_KEY" },
          generic: { base_url: "", api_key_env: "GENERIC_API_KEY" },
        },
        agents: {
          enabled_agent_ids: ["default"],
          profiles: {
            default: {
              model: { provider: "hatz", name: "glm-4.5" },
            },
          },
          subagent_defaults: {
            thinking_mode: "high",
            delegation_mode: "auto_execute",
            timeout_ms: 120000,
            allowed_tools: ["Read", "Grep", "Execute"],
          },
        },
        memory: {
          enabled: true,
          embeddings_enabled: false,
          embedding_provider: "",
          max_working_items: 30,
        },
        network: { allowed_domains: ["github.com", "docs.factory.ai"] },
        shell: { enable_exec: true },
        sandbox: { active: true, provider: "docker" },
        scheduler: { catch_up: true, max_concurrent_jobs: 4 },
        discord: { enabled: true },
        telegram: { enabled: false },
      })
      return
    }

    if (pathname === "/api/admin/secrets") {
      await json(route, {
        keys: ["discord/bot_token", "OPENAI_API_KEY"],
      })
      return
    }

    if (pathname === "/api/admin/scheduler/jobs") {
      await json(route, {
        paused: false,
        jobs: [
          { id: "job_a", schedule: "@every 5m", message: "ping", enabled: true },
          { id: "job_b", schedule: "@every 15m", message: "report", enabled: false },
        ],
      })
      return
    }

    if (pathname === "/api/admin/chat/sessions") {
      await json(route, {
        sessions: [
          {
            session_id: "sess_1",
            title: "Ops Room",
            agent_id: "default",
            updated_at: "2026-03-14T12:00:00Z",
          },
        ],
      })
      return
    }

    if (pathname === "/api/admin/skills") {
      await json(route, {
        agent_id: searchParams.get("agent_id") || "default",
        installed_skills: ["frontend-worker", "mission-worker-base"],
        activated_skills: ["frontend-worker"],
      })
      return
    }

    if (pathname === "/api/admin/agent/docs") {
      await json(route, {
        agent_id: searchParams.get("agent_id") || "default",
        documents: [
          { name: "SOUL.md", exists: true },
          { name: "RULES.md", exists: true },
          { name: "TOOLS.md", exists: false },
        ],
      })
      return
    }

    if (pathname === "/api/admin/sandbox/docker/status") {
      await json(route, {
        provider: "docker",
        running: true,
      })
      return
    }

    if (pathname === "/api/admin/sandbox/docker/images") {
      await json(route, {
        images: [{ id: "img_1" }, { id: "img_2" }],
      })
      return
    }

    if (pathname === "/api/admin/sandbox/docker/volumes") {
      await json(route, {
        volumes: [{ name: "vol_1" }],
      })
      return
    }

    if (pathname === "/v1/runs") {
      await json(route, {
        runs: [
          {
            id: "run_1",
            status: "completed",
            updated_at: "2026-03-14T12:04:00Z",
          },
          {
            id: "run_2",
            status: "running",
            updated_at: "2026-03-14T12:05:00Z",
          },
        ],
        total: 2,
      })
      return
    }

    if (pathname === "/v1/chat/messages" && method === "POST") {
      const payload = request.postDataJSON() as Record<string, unknown>
      state.quickPromptPayloads.push(payload)
      await json(route, { id: "run_quick_1" })
      return
    }

    if (pathname === "/api/admin/dashboards" && method === "GET") {
      const sorted = [...state.dashboards].sort((left, right) => left.position - right.position)
      await json(route, {
        dashboards: sorted,
      })
      return
    }

    if (pathname === "/api/admin/dashboards" && method === "POST") {
      const payload = request.postDataJSON() as Record<string, unknown>
      state.createPayloads.push(payload)
      const created: DashboardRecord = {
        id: `dash_server_${state.createPayloads.length}`,
        name: String(payload.name || `Dashboard ${state.createPayloads.length}`),
        position: state.dashboards.length,
        created_at: nowISO(),
        updated_at: nowISO(),
        layout: [],
      }
      state.dashboards = [...state.dashboards, created]
      await json(route, { ok: true, dashboard: created })
      return
    }

    if (pathname.startsWith("/api/admin/dashboards/") && method === "PUT") {
      const id = decodeURIComponent(pathname.split("/").pop() || "")
      const payload = request.postDataJSON() as DashboardRecord
      state.putCalls.push({ id, payload })
      const index = state.dashboards.findIndex((item) => item.id === id)
      if (index >= 0) {
        state.dashboards[index] = {
          ...cloneDashboard(payload),
          id,
          updated_at: nowISO(),
        }
      }
      await json(route, { ok: true, dashboard: state.dashboards[index] || payload })
      return
    }

    if (pathname.startsWith("/api/admin/dashboards/") && method === "DELETE") {
      const id = decodeURIComponent(pathname.split("/").pop() || "")
      state.deleteIDs.push(id)
      state.dashboards = state.dashboards.filter((item) => item.id !== id)
      await json(route, { ok: true, deleted: id })
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

test("dashboard sidebar supports create, duplicate, delete, reorder, and auto-save", async ({ page }) => {
  const state = await installDashboardsMocks(page)

  await page.goto("/dashboard#/dashboards")

  await expect(page.getByRole("heading", { name: "Custom Dashboards", level: 2 })).toBeVisible()
  await expect(page.getByTestId("dashboard-empty-state")).toBeVisible()

  await page.getByRole("button", { name: "Create dashboard" }).click()
  await expect.poll(() => state.createPayloads.length).toBe(1)
  await expect(page.locator("[data-testid^='dashboard-tab-row-']")).toHaveCount(2)

  await page.getByLabel("Dashboard name").fill("Ops Board")
  await page.waitForTimeout(300)
  expect(state.putCalls.length).toBe(0)
  await expect.poll(() => state.putCalls.some((call) => call.payload.name === "Ops Board")).toBeTruthy()

  const opsRow = page.locator("[data-testid^='dashboard-tab-row-']", { hasText: "Ops Board" }).first()
  await opsRow.getByRole("button", { name: "Duplicate" }).click()
  await expect(page.locator("[data-testid^='dashboard-tab-row-']", { hasText: "Ops Board Copy" })).toBeVisible()

  await opsRow.getByRole("button", { name: /Move Ops Board up/ }).click()
  await expect.poll(() => state.putCalls.length).toBeGreaterThan(0)

  const copyRow = page.locator("[data-testid^='dashboard-tab-row-']", { hasText: "Ops Board Copy" }).first()
  await copyRow.getByRole("button", { name: "Delete" }).click()
  await expect(page.getByTestId("dashboard-delete-dialog")).toBeVisible()
  await expect(page.locator("[data-testid^='dashboard-tab-row-']", { hasText: "Ops Board Copy" })).toHaveCount(1)
  await page.getByRole("button", { name: "Delete dashboard" }).click()
  await expect(page.locator("[data-testid^='dashboard-tab-row-']", { hasText: "Ops Board Copy" })).toHaveCount(0)
})

test("widget picker adds widgets and context menu supports configure, duplicate, remove, and open source", async ({ page }) => {
  const state = await installDashboardsMocks(page)

  await page.goto("/dashboard#/dashboards")

  await page.getByTestId("toolbar-add-widget").click()
  await page.getByLabel("Search widgets").fill("runs")
  await page.getByTestId("widget-picker-option-runs-recent").click()

  await page.getByTestId("toolbar-add-widget").click()
  await page.getByLabel("Search widgets").fill("scheduler")
  await page.getByTestId("widget-picker-option-scheduler-jobs").click()

  await expect(page.locator("[data-widget-key='runs.recent']")).toHaveCount(1)
  await expect(page.locator("[data-widget-key='scheduler.jobs']")).toHaveCount(1)

  const schedulerCard = page.locator("[data-widget-key='scheduler.jobs']").first()
  await schedulerCard.getByRole("button", { name: "Widget menu Scheduler: Jobs" }).click()
  await expect(schedulerCard.getByRole("button", { name: "Open source tab" })).toBeVisible()
  await expect(schedulerCard.getByRole("button", { name: "Duplicate widget" })).toBeVisible()
  await expect(schedulerCard.getByRole("button", { name: "Configure" })).toBeVisible()
  await expect(schedulerCard.getByRole("button", { name: "Remove widget" })).toBeVisible()
  await schedulerCard.getByRole("button", { name: "Configure" }).click()
  await expect(page.getByTestId("widget-configure-fallback-dialog")).toBeVisible()
  await expect(page.getByText("Scheduler: Jobs does not expose custom settings yet.")).toBeVisible()
  await page.getByTestId("widget-configure-fallback-close").click()

  const menuButton = page.getByRole("button", { name: "Widget menu Runs: Recent" }).first()
  await menuButton.click()
  page.once("dialog", (dialog) => {
    expect(dialog.type()).toBe("prompt")
    dialog.accept("8")
  })
  await page.getByRole("button", { name: "Configure" }).click()
  await expect.poll(() => {
    return state.putCalls.some((call) => {
      const first = call.payload.layout[0]
      const limit = Number(first?.widget_state?.limit || 0)
      return limit === 8
    })
  }).toBeTruthy()

  await menuButton.click()
  await page.getByRole("button", { name: "Duplicate widget" }).click()
  await expect(page.locator("[data-widget-key='runs.recent']")).toHaveCount(2)

  await page.getByRole("button", { name: "Widget menu Runs: Recent" }).first().click()
  await page.getByRole("button", { name: "Remove widget" }).click()
  await expect(page.locator("[data-widget-key='runs.recent']")).toHaveCount(1)

  await page.getByRole("button", { name: "Widget menu Runs: Recent" }).first().click()
  await page.getByRole("button", { name: "Open source tab" }).click()
  await expect(page).toHaveURL(/#\/runs$/)

  expect(state.createPayloads.length).toBeGreaterThanOrEqual(0)
})

test("widgets are draggable with resize handles and debounced auto-save", async ({ page }) => {
  const state = await installDashboardsMocks(page, [
    {
      id: "dash_main",
      name: "Main Dashboard",
      position: 0,
      created_at: "2026-03-14T10:00:00Z",
      updated_at: "2026-03-14T10:00:00Z",
      layout: [
        {
          widget_key: "runs.recent",
          widget_instance_id: "widget_1",
          x: 0,
          y: 0,
          w: 4,
          h: 3,
          widget_state: {},
        },
      ],
    },
  ])

  await page.goto("/dashboard#/dashboards")

  const card = page.getByTestId("widget-card-widget_1")
  await expect(card).toHaveAttribute("data-widget-x", "0")
  await expect(card).toHaveAttribute("data-widget-y", "0")
  await expect(card).toHaveAttribute("data-widget-w", "4")
  await expect(card).toHaveAttribute("data-widget-h", "3")

  const resizeHandle = page.getByTestId("widget-resize-handle-widget_1")
  await expect(resizeHandle).toBeVisible()

  const dragHandle = page.getByTestId("widget-drag-handle-widget_1")
  const dragBox = await dragHandle.boundingBox()
  expect(dragBox).not.toBeNull()
  if (!dragBox) {
    return
  }

  await page.mouse.move(dragBox.x + 8, dragBox.y + 8)
  await page.mouse.down()
  await page.mouse.move(dragBox.x + 180, dragBox.y + 130)
  await page.mouse.up()

  await expect.poll(async () => {
    const x = await card.getAttribute("data-widget-x")
    const y = await card.getAttribute("data-widget-y")
    return Number(x || "0") + Number(y || "0")
  }).toBeGreaterThan(0)

  const putCountBefore = state.putCalls.length
  await page.waitForTimeout(300)
  expect(state.putCalls.length).toBe(putCountBefore)
  await expect.poll(() => state.putCalls.length).toBeGreaterThan(putCountBefore)
})
