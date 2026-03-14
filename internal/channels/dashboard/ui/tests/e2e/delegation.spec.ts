import { expect, test, type Page, type Route } from "@playwright/test"

type MockRun = {
  id: string
  agent_id: string
  source: string
  status: string
  updated_at: string
  trace?: Record<string, unknown>
}

type DecisionRecord = {
  timestamp: string
  run_id: string
  agent_id: string
  record_type: string
  human_summary: string
  payload?: Record<string, unknown>
}

type DecisionNode = {
  run_id: string
  agent_id: string
  records: DecisionRecord[]
  subagents?: DecisionNode[]
}

type MockState = {
  config: Record<string, unknown>
  runs: MockRun[]
  runDetails: Record<string, Record<string, unknown>>
  decisionsByRunID: Record<string, DecisionNode>
  configPatchBodies: Record<string, unknown>[]
}

function createMockState(delegationMode = "suggest_only"): MockState {
  return {
    config: {
      agents: {
        delegation_mode: delegationMode,
        delegation_threshold: 3,
        delegation_cooldown_iterations: 12,
        auto_delegate: true,
      },
    },
    runs: [
      {
        id: "run_plan_alpha",
        agent_id: "default",
        source: "chat",
        status: "completed",
        updated_at: "2026-03-14T11:00:00Z",
      },
      {
        id: "run_plan_beta",
        agent_id: "default",
        source: "chat",
        status: "completed",
        updated_at: "2026-03-14T11:10:00Z",
      },
    ],
    runDetails: {
      run_plan_alpha: {
        id: "run_plan_alpha",
        agent_id: "default",
        source: "chat",
        status: "completed",
        updated_at: "2026-03-14T11:00:00Z",
        trace: {
          decomposition_plan: {
            delegation_mode: "suggest_only",
            trigger_reason: "complexity threshold exceeded",
            min_confidence: 0.72,
            avg_confidence: 0.84,
            all_roles_built_in: true,
            generated_at: "2026-03-14T11:00:01Z",
            tasks: [
              {
                task_id: "discover",
                description: "Read existing code and collect relevant files",
                assigned_role: "scout",
                confidence: 0.91,
                depends_on: [],
              },
              {
                task_id: "implement",
                description: "Implement UI updates",
                assigned_role: "implementer",
                confidence: 0.81,
                depends_on: ["discover"],
              },
              {
                task_id: "verify",
                description: "Run focused validations",
                assigned_role: "verifier",
                confidence: 0.78,
                depends_on: ["implement"],
              },
            ],
            dependency_dag: [
              { from_task_id: "discover", to_task_id: "implement" },
              { from_task_id: "implement", to_task_id: "verify" },
            ],
          },
        },
      },
      run_plan_beta: {
        id: "run_plan_beta",
        agent_id: "default",
        source: "chat",
        status: "completed",
        updated_at: "2026-03-14T11:10:00Z",
        trace: {
          decomposition_plan: {
            delegation_mode: "approve_plan",
            trigger_reason: "multi-step workflow",
            min_confidence: 0.76,
            avg_confidence: 0.82,
            all_roles_built_in: true,
            generated_at: "2026-03-14T11:10:01Z",
            tasks: [
              {
                task_id: "plan",
                description: "Draft implementation approach",
                assigned_role: "planner",
                confidence: 0.88,
                depends_on: [],
              },
              {
                task_id: "patch",
                description: "Apply code changes",
                assigned_role: "implementer",
                confidence: 0.79,
                depends_on: ["plan"],
              },
            ],
            dependency_dag: [{ from_task_id: "plan", to_task_id: "patch" }],
          },
        },
      },
    },
    decisionsByRunID: {
      run_plan_alpha: {
        run_id: "run_plan_alpha",
        agent_id: "default",
        records: [
          {
            timestamp: "2026-03-14T11:00:00Z",
            run_id: "run_plan_alpha",
            agent_id: "default",
            record_type: "goal_interpretation",
            human_summary: "Operator asked for delegation policy UI.",
          },
          {
            timestamp: "2026-03-14T11:00:08Z",
            run_id: "run_plan_alpha",
            agent_id: "default",
            record_type: "strategy_selection",
            human_summary: "Selected typed-role decomposition strategy.",
          },
        ],
        subagents: [
          {
            run_id: "run_child_alpha",
            agent_id: "implementer",
            records: [
              {
                timestamp: "2026-03-14T11:00:10Z",
                run_id: "run_child_alpha",
                agent_id: "implementer",
                record_type: "role_selection",
                human_summary: "Assigned implementer due to write-heavy task.",
              },
            ],
          },
        ],
      },
      run_plan_beta: {
        run_id: "run_plan_beta",
        agent_id: "default",
        records: [
          {
            timestamp: "2026-03-14T11:10:00Z",
            run_id: "run_plan_beta",
            agent_id: "default",
            record_type: "goal_interpretation",
            human_summary: "Operator requested run comparison tooling.",
          },
          {
            timestamp: "2026-03-14T11:10:09Z",
            run_id: "run_plan_beta",
            agent_id: "default",
            record_type: "strategy_selection",
            human_summary: "Picked approve-plan workflow for human gate.",
          },
        ],
      },
    },
    configPatchBodies: [],
  }
}

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: "application/json; charset=utf-8",
    body: JSON.stringify(body),
  })
}

async function installDelegationMocks(page: Page, state: MockState): Promise<void> {
  await page.route("**/*", async (route) => {
    const request = route.request()
    const method = request.method()
    const url = new URL(request.url())
    const { pathname, searchParams } = url

    if (pathname === "/api/admin/status") {
      await json(route, { ok: true, model: { provider: "hatz", name: "glm-4.5" }, run_count: state.runs.length })
      return
    }

    if (pathname === "/api/admin/config" && method === "GET") {
      await json(route, state.config)
      return
    }

    if (pathname === "/api/admin/config" && method === "PATCH") {
      const payload = JSON.parse(request.postData() || "{}") as Record<string, unknown>
      state.configPatchBodies.push(payload)

      const incomingAgents = (payload.agents || {}) as Record<string, unknown>
      const existingAgents = (state.config.agents || {}) as Record<string, unknown>
      state.config = {
        ...state.config,
        ...payload,
        agents: {
          ...existingAgents,
          ...incomingAgents,
        },
      }

      await json(route, { ok: true, config: state.config })
      return
    }

    if (pathname === "/v1/runs" && method === "GET") {
      const limit = Number(searchParams.get("limit") || "50") || 50
      const offset = Number(searchParams.get("offset") || "0") || 0
      const pageRuns = state.runs.slice(offset, offset + limit)
      await json(route, { runs: pageRuns, total: state.runs.length, limit, offset })
      return
    }

    if (pathname.startsWith("/v1/runs/") && method === "GET") {
      const runID = decodeURIComponent(pathname.replace("/v1/runs/", ""))
      const detail = state.runDetails[runID]
      if (!detail) {
        await json(route, { error: { code: "runs.not_found", message: "run not found" } }, 404)
        return
      }
      await json(route, detail)
      return
    }

    if (pathname.startsWith("/api/admin/debug/runs/") && pathname.endsWith("/trace") && method === "GET") {
      const runID = decodeURIComponent(pathname.replace("/api/admin/debug/runs/", "").replace("/trace", ""))
      const detail = state.runDetails[runID]
      const trace = (detail?.trace || {}) as Record<string, unknown>
      await json(route, { trace })
      return
    }

    if (pathname.startsWith("/api/admin/runs/") && pathname.endsWith("/decisions") && method === "GET") {
      const runID = decodeURIComponent(pathname.replace("/api/admin/runs/", "").replace("/decisions", ""))
      const payload = state.decisionsByRunID[runID]
      if (!payload) {
        await json(route, { error: { code: "decisions.not_found", message: "missing decisions" } }, 404)
        return
      }
      await json(route, payload)
      return
    }

    if (pathname.startsWith("/api/") || pathname.startsWith("/v1/")) {
      await json(route, { ok: true })
      return
    }

    await route.continue()
  })
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("openclawssy.dashboard.bearer", "e2e-token")
  })
})

test("Delegation policy editor shows mode/threshold/cooldown/toggle and saves through config PATCH", async ({ page }) => {
  const state = createMockState()
  await installDelegationMocks(page, state)

  await page.goto("/dashboard#/delegation")

  await expect(page.getByRole("heading", { name: "Delegation", level: 2 })).toBeVisible()
  await expect(page.getByTestId("delegation-mode-select")).toHaveValue("suggest_only")
  await expect(page.getByTestId("delegation-threshold-slider")).toHaveValue("3")
  await expect(page.getByTestId("delegation-cooldown-input")).toHaveValue("12")
  await expect(page.getByTestId("delegation-auto-toggle")).toBeChecked()

  await page.getByTestId("delegation-mode-select").selectOption("full_autonomous")
  const thresholdSlider = page.getByTestId("delegation-threshold-slider")
  await thresholdSlider.focus()
  await page.keyboard.press("ArrowRight")
  await page.keyboard.press("ArrowRight")
  await page.keyboard.press("ArrowRight")
  await expect(thresholdSlider).toHaveValue("6")
  await page.getByTestId("delegation-cooldown-input").fill("20")
  await page.getByTestId("delegation-auto-toggle").uncheck()

  await page.getByTestId("delegation-save-button").click()

  await expect.poll(() => state.configPatchBodies.length).toBe(1)
  const agents = (state.configPatchBodies[0].agents || {}) as Record<string, unknown>
  expect(agents.delegation_mode).toBe("full_autonomous")
  expect(agents.delegation_threshold).toBe(6)
  expect(agents.delegation_cooldown_iterations).toBe(20)
  expect(agents.auto_delegate).toBe(false)

  await expect(page.getByTestId("delegation-save-notice")).toContainText("saved")
})

for (const legacyMode of ["prompt_only", "tool_gated", "auto_execute"] as const) {
  test(`Delegation policy editor preserves legacy mode ${legacyMode} on save`, async ({ page }) => {
    const state = createMockState(legacyMode)
    await installDelegationMocks(page, state)

    await page.goto("/dashboard#/delegation")

    await expect(page.getByTestId("delegation-mode-select")).toHaveValue(legacyMode)

    await page.getByTestId("delegation-save-button").click()

    await expect.poll(() => state.configPatchBodies.length).toBe(1)
    const agents = (state.configPatchBodies[0].agents || {}) as Record<string, unknown>
    expect(agents.delegation_mode).toBe(legacyMode)

    await expect(page.getByTestId("delegation-save-notice")).toContainText("saved")
  })
}

test("Task graph preview renders nodes/edges with role and confidence badges and shows approve/reject actions", async ({ page }) => {
  const state = createMockState()
  await installDelegationMocks(page, state)

  await page.goto("/dashboard#/delegation")

  await page.getByTestId("delegation-run-select").selectOption("run_plan_alpha")

  await expect(page.getByTestId("task-graph-node-discover")).toContainText("scout")
  await expect(page.getByTestId("task-graph-node-implement")).toContainText("implementer")
  await expect(page.getByTestId("task-graph-node-verify")).toContainText("verifier")
  await expect(page.getByTestId("task-graph-node-discover")).toContainText("91%")
  await expect(page.getByTestId("task-graph-edge-discover-implement")).toHaveCount(1)

  await expect(page.getByTestId("task-graph-approve-button")).toBeVisible()
  await expect(page.getByTestId("task-graph-reject-button")).toBeVisible()

  await page.getByTestId("task-graph-approve-button").click()
  await expect(page.getByTestId("task-graph-action-notice")).toContainText("approved")

  await page.getByTestId("task-graph-reject-button").click()
  await expect(page.getByTestId("task-graph-action-notice")).toContainText("rejected")
})

test("Runs detail view opens Why this happened drawer with chronological decision records", async ({ page }) => {
  const state = createMockState()
  await installDelegationMocks(page, state)

  await page.goto("/dashboard#/runs")
  await page.getByRole("button", { name: "Open" }).first().click()

  await page.getByTestId("run-why-button").click()

  await expect(page.getByTestId("decision-drawer")).toBeVisible()
  await expect(page.getByTestId("decision-drawer")).toContainText("goal_interpretation")
  await expect(page.getByTestId("decision-drawer")).toContainText("strategy_selection")
  await expect(page.getByTestId("decision-drawer")).toContainText("Assigned implementer due to write-heavy task.")
})

test("Run comparison shows two runs side-by-side with divergence callout", async ({ page }) => {
  const state = createMockState()
  await installDelegationMocks(page, state)

  await page.goto("/dashboard#/delegation")

  await page.getByTestId("run-compare-left-select").selectOption("run_plan_alpha")
  await page.getByTestId("run-compare-right-select").selectOption("run_plan_beta")
  await page.getByTestId("run-compare-load").click()

  const leftPanel = page.getByTestId("run-compare-left-panel")
  const rightPanel = page.getByTestId("run-compare-right-panel")

  await expect(leftPanel).toContainText("run_plan_alpha")
  await expect(leftPanel).toContainText("Selected typed-role decomposition strategy.")

  await expect(rightPanel).toContainText("run_plan_beta")
  await expect(rightPanel).toContainText("Picked approve-plan workflow for human gate.")

  await expect(page.getByTestId("run-compare-divergence")).toContainText("Divergence")
})
