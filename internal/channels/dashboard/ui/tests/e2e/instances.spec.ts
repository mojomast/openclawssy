import { expect, test, type Page, type Route } from "@playwright/test"

type InstancesState = {
  listCalls: number
  activateCalls: string[]
  activeInstanceID: string
}

async function installInstancesMocks(page: Page, options?: { instanceControlEnabled?: boolean }): Promise<InstancesState> {
  const state: InstancesState = {
    listCalls: 0,
    activateCalls: [],
    activeInstanceID: "alpha",
  }
  const instanceControlEnabled = options?.instanceControlEnabled ?? true

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

    if (pathname === "/api/admin/control-plane/features" && method === "GET") {
      await json({
        features: {
          instance_control: instanceControlEnabled,
          instance_agents: true,
          wizard: true,
          eval: true,
        },
      })
      return
    }

    if (pathname === "/api/admin/instances" && method === "GET") {
      state.listCalls += 1
      await json({
        active_instance_id: state.activeInstanceID,
        instances: [
          {
            id: "alpha",
            name: "Alpha",
            description: "Primary operator instance.",
            updated_at: "2026-03-17T12:00:00Z",
            model_provider: "openai",
            model_name: "gpt-4.1-mini",
            agent_count: 2,
            is_active: state.activeInstanceID === "alpha",
          },
          {
            id: "beta",
            name: "Beta",
            description: "Staging operator instance.",
            updated_at: "2026-03-17T12:10:00Z",
            model_provider: "openrouter",
            model_name: "moonshot/test",
            agent_count: 1,
            is_active: state.activeInstanceID === "beta",
          },
        ],
      })
      return
    }

    const activateMatch = pathname.match(/^\/api\/admin\/instances\/([^/]+)\/activate$/)
    if (activateMatch && method === "POST") {
      const instanceID = decodeURIComponent(activateMatch[1])
      state.activateCalls.push(instanceID)
      state.activeInstanceID = instanceID
      await json({ ok: true, active_instance_id: instanceID })
      return
    }

    if (pathname.startsWith("/api/") || pathname.startsWith("/v1/")) {
      await json({ ok: true })
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

test("Instances page lists canonical instances and active state", async ({ page }) => {
  const state = await installInstancesMocks(page)

  await page.goto("/dashboard#/instances")

  await expect(page.getByRole("heading", { name: "Instances", level: 2 })).toBeVisible()
  await expect(page.getByTestId("active-instance-card")).toContainText("Alpha")
  await expect(page.getByTestId("instance-item-alpha")).toContainText("Active")
  await expect(page.getByTestId("instance-item-beta")).toContainText("Beta")
  await expect(page.getByRole("link", { name: "Instances" })).toBeVisible()
  await expect.poll(() => state.listCalls).toBe(1)
})

test("Instances page activates another instance", async ({ page }) => {
  const state = await installInstancesMocks(page)

  await page.goto("/dashboard#/instances")
  await page.getByTestId("activate-instance-beta").click()

  await expect.poll(() => state.activateCalls).toEqual(["beta"])
  await expect(page.getByTestId("active-instance-card")).toContainText("Beta")
  await expect(page.getByText("Activated instance beta.")).toBeVisible()
})

test("Instances nav hides and direct route shows disabled state when instance control is off", async ({ page }) => {
  const state = await installInstancesMocks(page, { instanceControlEnabled: false })

  await page.goto("/dashboard#/instances")

  await expect(page.getByTestId("instances-disabled-state")).toContainText("Instance control disabled")
  await expect(page.getByRole("link", { name: "Instances" })).toHaveCount(0)
  await expect(page.getByRole("button", { name: "Refresh instances" })).toBeDisabled()
  await expect.poll(() => state.listCalls).toBe(0)
})
