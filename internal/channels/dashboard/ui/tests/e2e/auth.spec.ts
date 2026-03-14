import { expect, test } from "@playwright/test"

test("tokenless load shows in-app auth gate and retries requests with bearer header", async ({ page }) => {
  const authorizationHeaders: string[] = []

  await page.addInitScript(() => {
    window.localStorage.removeItem("openclawssy.dashboard.bearer")
  })

  await page.route("**/api/admin/workspace/entries**", async (route) => {
    authorizationHeaders.push(route.request().headers()["authorization"] || "")
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        workspace_root: "/tmp/workspace",
        path: ".",
        parent_path: "",
        entries: [],
      }),
    })
  })

  await page.goto("/dashboard#/workspace")

  await expect(page.getByRole("heading", { name: "Dashboard access token required" })).toBeVisible()
  await expect(page.getByText("Failed to load workspace entries", { exact: false })).toHaveCount(0)

  await page.getByLabel("Dashboard bearer token").fill("e2e-token")
  await page.getByRole("button", { name: "Save token and continue" }).click()

  await expect(page.getByRole("heading", { name: "Dashboard access token required" })).toHaveCount(0)
  await expect.poll(() => authorizationHeaders.length).toBe(1)
  expect(authorizationHeaders[0]).toBe("Bearer e2e-token")
  await expect(page.getByText("Loaded 0 item(s).")).toBeVisible()

  await page.getByRole("button", { name: "Refresh" }).click()
  await expect.poll(() => authorizationHeaders.length).toBe(2)
  expect(authorizationHeaders[1]).toBe("Bearer e2e-token")

  const storedToken = await page.evaluate(() => window.localStorage.getItem("openclawssy.dashboard.bearer"))
  expect(storedToken).toBe("e2e-token")
})
