import { expect, test } from "@playwright/test"

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("openclawssy.dashboard.bearer", "e2e-token")
  })
})

test("shows explicit loading and empty-state messaging when no runs exist", async ({ page }) => {
  await page.route("**/v1/runs?**", async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 250))
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        runs: [],
        total: 0,
        limit: 1,
        offset: 0,
      }),
    })
  })

  await page.goto("/dashboard#/runs")

  await expect(page.getByRole("heading", { name: "Runs", level: 2 })).toBeVisible()
  await expect(page.getByText("Loading...")).toBeVisible()
  await expect(page.getByText("No runs found. The React Runs view is still migrating.")).toBeVisible()
  await expect(page.getByText("(Page migration in progress)")).toHaveCount(0)
})

test("shows interim non-empty messaging when runs already exist", async ({ page }) => {
  await page.route("**/v1/runs?**", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        runs: [{ id: "run_1" }],
        total: 3,
        limit: 1,
        offset: 0,
      }),
    })
  })

  await page.goto("/dashboard#/runs")

  await expect(page.getByText("Detected 3 run(s). Full React run inspection is still migrating.")).toBeVisible()
  await expect(page.getByText("Use the legacy dashboard to filter, paginate, and inspect traces right now.")).toBeVisible()
})

test("shows retryable error state when runs request fails", async ({ page }) => {
  let requestCount = 0

  await page.route("**/v1/runs?**", async (route) => {
    requestCount += 1
    if (requestCount === 1) {
      await route.fulfill({ status: 500, body: "runs service unavailable" })
      return
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        runs: [],
        total: 0,
      }),
    })
  })

  await page.goto("/dashboard#/runs")

  await expect(page.getByText("Failed to load runs: runs service unavailable")).toBeVisible()
  await page.getByRole("button", { name: "Try Again" }).click()
  await expect(page.getByText("No runs found. The React Runs view is still migrating.")).toBeVisible()
})
