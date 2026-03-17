import { expect, test, type Page, type Route } from "@playwright/test"

type WizardState = {
  templateCalls: number
}

async function installWizardMocks(page: Page, options?: { wizardEnabled?: boolean }): Promise<WizardState> {
  const state: WizardState = {
    templateCalls: 0,
  }
  const wizardEnabled = options?.wizardEnabled ?? true

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
          instance_control: true,
          instance_agents: true,
          wizard: wizardEnabled,
          eval: true,
        },
      })
      return
    }

    if (pathname === "/api/admin/wizard/templates" && method === "GET") {
      state.templateCalls += 1
      await json({
        instance_templates: [
          { id: "blank", name: "Blank", description: "Start from the default safe configuration." },
          { id: "chat-assistant", name: "Chat Assistant", description: "Tune the default config for conversational use." },
          { id: "automation", name: "Automation", description: "Tune the default config for scheduled workflows." },
        ],
        agent_templates: [
          { id: "general", name: "General", description: "A general-purpose agent profile." },
          { id: "research", name: "Research", description: "A research-oriented profile." },
          { id: "operator", name: "Operator", description: "An operations-focused profile." },
        ],
      })
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

test("Wizard page lists instance and agent templates", async ({ page }) => {
  const state = await installWizardMocks(page)

  await page.goto("/dashboard#/wizard")

  await expect(page.getByRole("heading", { name: "Wizard", level: 2 })).toBeVisible()
  await expect(page.getByTestId("wizard-template-count")).toContainText("6 templates available")
  await expect(page.getByTestId("wizard-instance-template-blank")).toContainText("Blank")
  await expect(page.getByTestId("wizard-instance-template-chat-assistant")).toContainText("Chat Assistant")
  await expect(page.getByTestId("wizard-agent-template-research")).toContainText("Research")
  await expect(page.getByRole("link", { name: "Wizard" })).toBeVisible()
  await expect.poll(() => state.templateCalls).toBe(1)
})

test("Wizard page refreshes template catalog", async ({ page }) => {
  const state = await installWizardMocks(page)

  await page.goto("/dashboard#/wizard")
  await expect.poll(() => state.templateCalls).toBe(1)

  await page.getByRole("button", { name: "Refresh templates" }).click()
  await expect.poll(() => state.templateCalls).toBe(2)
})

test("Wizard nav hides and direct route shows disabled state when feature is off", async ({ page }) => {
  const state = await installWizardMocks(page, { wizardEnabled: false })

  await page.goto("/dashboard#/wizard")

  await expect(page.getByTestId("wizard-disabled-state")).toContainText("Wizard disabled")
  await expect(page.getByTestId("wizard-disabled-state")).toContainText("Guided instance and agent creation is disabled")
  await expect(page.getByRole("link", { name: "Wizard" })).toHaveCount(0)
  await expect.poll(() => state.templateCalls).toBe(0)
  await expect(page.getByRole("button", { name: "Refresh templates" })).toBeDisabled()
})
