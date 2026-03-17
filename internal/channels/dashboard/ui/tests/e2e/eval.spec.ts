import { expect, test, type Page, type Route } from "@playwright/test"

const defaultRuns = [
  {
    id: 42,
    suite: "basic",
    timestamp: "2026-03-14T19:00:00Z",
    total: 2,
    passed: 1,
    failed: 1,
    status: "fail",
    results: [
      {
        name: "case-pass",
        result: {
          passed: true,
          expected: "ok",
          actual: "ok",
          duration_ms: 11,
        },
      },
      {
        name: "case-regression",
        result: {
          passed: false,
          expected: "stable",
          actual: "changed",
          duration_ms: 13,
          error: "mismatch",
        },
      },
    ],
    metrics: {
      completion_rate: 0.5,
      tool_misuse_rate: 0.25,
      delegation_precision: 0.75,
      unnecessary_delegation_rate: 0,
      token_cost: 128,
      time_to_completion: 2400,
    },
    baseline: {
      available: true,
      timestamp: "2026-03-14T18:50:00Z",
      regressions: [
        {
          test_name: "case-regression",
          baseline: {
            passed: true,
            expected: "stable",
            actual: "stable",
            duration_ms: 10,
          },
          latest: {
            passed: false,
            expected: "stable",
            actual: "changed",
            duration_ms: 13,
            error: "mismatch",
          },
        },
      ],
    },
  },
  {
    id: 41,
    suite: "tool_choice",
    timestamp: "2026-03-14T18:40:00Z",
    total: 2,
    passed: 2,
    failed: 0,
    status: "pass",
    results: [
      {
        name: "choose-fs-read",
        result: {
          passed: true,
          expected: "tool:fs.read",
          actual: "tool:fs.read",
          duration_ms: 9,
        },
      },
      {
        name: "choose-web-search",
        result: {
          passed: true,
          expected: "tool:web.search",
          actual: "tool:web.search",
          duration_ms: 10,
        },
      },
    ],
    metrics: {
      completion_rate: 1,
      tool_misuse_rate: 0,
      delegation_precision: 1,
      unnecessary_delegation_rate: 0,
      token_cost: 90,
      time_to_completion: 1900,
    },
    baseline: {
      available: false,
      regressions: [],
    },
  },
]

type EvalMockOptions = {
  runs?: unknown[]
  failEvalRequests?: number
  evalEnabled?: boolean
}

async function installEvalMocks(page: Page, options: EvalMockOptions = {}): Promise<void> {
  let remainingFailures = options.failEvalRequests ?? 0
  const runs = options.runs ?? defaultRuns
  const evalEnabled = options.evalEnabled ?? true

  await page.route("**/*", async (route: Route) => {
    const request = route.request()
    const method = request.method()
    const url = new URL(request.url())
    const { pathname } = url

    const json = async (body: unknown, status = 200) => {
      await route.fulfill({
        status,
        contentType: "application/json; charset=utf-8",
        body: JSON.stringify(body),
      })
    }

    if (pathname === "/api/admin/status") {
      await json({ ok: true, model: { provider: "hatz", name: "glm-4.5" }, run_count: 1 })
      return
    }

    if (pathname === "/api/admin/control-plane/features") {
      await json({
        features: {
          instance_control: true,
          instance_agents: true,
          wizard: true,
          eval: evalEnabled,
        },
      })
      return
    }

    if (pathname === "/api/admin/eval/results" && method === "GET") {
      if (!evalEnabled) {
        await json({ error: { code: "feature.eval_disabled", message: "eval routes are disabled" } }, 403)
        return
      }
      if (remainingFailures > 0) {
        remainingFailures -= 1
        await json({ error: { message: "backend unavailable" } }, 500)
        return
      }

      await json({
        runs,
        count: runs.length,
      })
      return
    }

    if (pathname.startsWith("/api/") || pathname.startsWith("/v1/")) {
      await json({ ok: true })
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

test("Eval Results page shows history, expandable details, metrics, and red regressions", async ({ page }) => {
  await installEvalMocks(page)

  await page.goto("/dashboard#/help")
  await page.getByRole("link", { name: "Eval" }).click()

  await expect(page).toHaveURL(/#\/eval$/)
  await expect(page.getByRole("heading", { name: "Eval Results", level: 2 })).toBeVisible()
  await expect(page.getByTestId("eval-history-table")).toBeVisible()
  await expect(page.getByTestId("eval-run-row-42")).toContainText("basic")
  await expect(page.getByTestId("eval-run-row-41")).toContainText("tool_choice")

  const detailsPanel = page.getByTestId("eval-run-details-42")
  if (!(await detailsPanel.isVisible())) {
    await page.getByTestId("eval-run-toggle-42").click()
  }

  await expect(detailsPanel).toBeVisible()
  await expect(page.getByTestId("eval-case-row-case-pass")).toContainText("PASS")
  await expect(page.getByTestId("eval-case-row-case-regression")).toContainText("FAIL")

  await expect(page.getByTestId("eval-metric-completion-rate")).toContainText("50.0%")
  await expect(page.getByTestId("eval-metric-tool-misuse-rate")).toContainText("25.0%")
  await expect(page.getByTestId("eval-metric-delegation-precision")).toContainText("75.0%")
  await expect(page.getByTestId("eval-metric-token-cost")).toContainText("128")
  await expect(page.getByTestId("eval-metric-time-to-completion")).toContainText("2.4s")

  const regressionRow = page.getByTestId("eval-regression-row-case-regression")
  await expect(regressionRow).toBeVisible()
  await expect(regressionRow).toContainText("case-regression")
  await expect(regressionRow).toHaveClass(/text-red/)
})

test("Eval Results page shows empty state when no runs are returned", async ({ page }) => {
  await installEvalMocks(page, { runs: [] })

  await page.goto("/dashboard#/help")
  await page.getByRole("link", { name: "Eval" }).click()

  await expect(page).toHaveURL(/#\/eval$/)
  await expect(page.getByText("No eval results found.")).toBeVisible()
  await expect(page.getByTestId("eval-history-table")).toHaveCount(0)
})

test("Eval Results page surfaces API errors and refreshes successfully after retry", async ({ page }) => {
  await installEvalMocks(page, { failEvalRequests: 1 })

  await page.goto("/dashboard#/help")
  await page.getByRole("link", { name: "Eval" }).click()

  await expect(page.getByText("Failed to load eval results: backend unavailable")).toBeVisible()
  await page.getByRole("button", { name: "Retry" }).click()

  await expect(page.getByTestId("eval-history-table")).toBeVisible()
  await expect(page.getByTestId("eval-run-row-42")).toContainText("basic")
})

test("Eval Results page hides nav entry and shows disabled state when eval feature is off", async ({ page }) => {
  await installEvalMocks(page, { evalEnabled: false })

  await page.goto("/dashboard#/eval")

  await expect(page.getByRole("link", { name: "Eval" })).toHaveCount(0)
  await expect(page.getByTestId("eval-disabled-state")).toContainText("Eval disabled")
  await expect(page.getByTestId("eval-disabled-state")).toContainText("Eval is disabled")
})
