import { expect, test, type Page, type Route } from "@playwright/test"

type SkillsApiState = {
  availableAgents: string[]
  installableCatalog: string[]
  installedSkills: string[]
  activatedByAgent: Record<string, string[]>
  getCount: number
  failNextGet: boolean
  failNextInstall: boolean
  failNextActivate: boolean
  failNextDeactivate: boolean
}

function normalizeName(value: unknown): string {
  if (value === null || value === undefined) {
    return ""
  }
  return String(value).trim().toLowerCase()
}

function unique(values: string[]): string[] {
  return Array.from(new Set(values.map((value) => normalizeName(value)).filter(Boolean))).sort((a, b) => a.localeCompare(b))
}

function buildSkillsPayload(state: SkillsApiState, requestedAgent: string) {
  const agentID = normalizeName(requestedAgent) || "default"
  const installed = unique(state.installedSkills)
  const activated = unique(state.activatedByAgent[agentID] || []).filter((name) => installed.includes(name))
  state.activatedByAgent[agentID] = activated

  return {
    agent_id: agentID,
    available_agents: state.availableAgents,
    installable: unique(state.installableCatalog).map((name) => ({
      name,
      installed: installed.includes(name),
    })),
    installed_skills: installed,
    activated_skills: activated,
  }
}

async function routeSkillsApi(page: Page, state: SkillsApiState) {
  await page.route("**/api/admin/skills**", async (route: Route) => {
    const url = new URL(route.request().url())
    const method = route.request().method()

    if (method === "GET") {
      state.getCount += 1
      if (state.failNextGet) {
        state.failNextGet = false
        await route.fulfill({ status: 500, body: "skills list failed" })
        return
      }

      const requestedAgent = normalizeName(url.searchParams.get("agent_id") || "default") || "default"
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(buildSkillsPayload(state, requestedAgent)),
      })
      return
    }

    if (method === "POST") {
      const payload = JSON.parse(route.request().postData() || "{}") as {
        action?: string
        name?: string
        agent_id?: string
      }

      const action = normalizeName(payload.action)
      const skillName = normalizeName(payload.name)
      const agentID = normalizeName(payload.agent_id) || "default"

      if (action === "install") {
        if (state.failNextInstall) {
          state.failNextInstall = false
          await route.fulfill({ status: 500, body: "install failed" })
          return
        }
        state.installedSkills = unique([...state.installedSkills, skillName])
      }

      if (action === "activate") {
        if (state.failNextActivate) {
          state.failNextActivate = false
          await route.fulfill({ status: 500, body: "activate failed" })
          return
        }

        if (!state.installedSkills.map((item) => normalizeName(item)).includes(skillName)) {
          await route.fulfill({ status: 400, body: `skill ${skillName} is not installed` })
          return
        }

        state.activatedByAgent[agentID] = unique([...(state.activatedByAgent[agentID] || []), skillName])
      }

      if (action === "deactivate") {
        if (state.failNextDeactivate) {
          state.failNextDeactivate = false
          await route.fulfill({ status: 500, body: "deactivate failed" })
          return
        }

        state.activatedByAgent[agentID] = unique((state.activatedByAgent[agentID] || []).filter((item) => normalizeName(item) !== skillName))
      }

      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(buildSkillsPayload(state, agentID)),
      })
      return
    }

    await route.continue()
  })
}

async function routeInstanceAgentsFeature(page: Page, enabled: boolean) {
  await page.route("**/api/admin/control-plane/features", async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        features: {
          instance_control: true,
          instance_agents: enabled,
          wizard: true,
          eval: true,
        },
      }),
    })
  })
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("openclawssy.dashboard.bearer", "e2e-token")
  })
})

test("renders agent selector, installable skills, and activation controls", async ({ page }) => {
  const state: SkillsApiState = {
    availableAgents: ["default", "ops"],
    installableCatalog: ["playwrite", "scrutiny", "tuistory"],
    installedSkills: ["playwrite"],
    activatedByAgent: { default: ["playwrite"] },
    getCount: 0,
    failNextGet: false,
    failNextInstall: false,
    failNextActivate: false,
    failNextDeactivate: false,
  }

  await routeSkillsApi(page, state)
  await routeInstanceAgentsFeature(page, true)
  await page.goto("/dashboard#/skills")

  await expect(page.getByRole("heading", { name: "Skills", level: 2 })).toBeVisible()
  await expect(page.getByLabel("Agent")).toBeVisible()
  await expect(page.getByRole("heading", { name: "Installable Skills" })).toBeVisible()
  await expect(page.locator("article", { hasText: "playwrite" })).toHaveCount(2)
  await expect(page.getByRole("button", { name: "Installed" })).toBeVisible()
  await expect(page.locator("article", { hasText: "scrutiny" }).getByRole("button", { name: "Install" })).toBeVisible()
  await expect(page.locator("article", { hasText: "tuistory" }).getByRole("button", { name: "Install" })).toBeVisible()
  await expect(page.getByRole("heading", { name: "Agent Activation" })).toBeVisible()
  await expect(page.getByRole("button", { name: "Deactivate" })).toBeVisible()
  await expect(page.getByText("Skills loaded.")).toBeVisible()
})

test("installs skills and activates/deactivates per selected agent", async ({ page }) => {
  const state: SkillsApiState = {
    availableAgents: ["default", "ops"],
    installableCatalog: ["playwrite", "scrutiny"],
    installedSkills: ["playwrite"],
    activatedByAgent: { default: [] },
    getCount: 0,
    failNextGet: false,
    failNextInstall: false,
    failNextActivate: false,
    failNextDeactivate: false,
  }

  await routeSkillsApi(page, state)
  await routeInstanceAgentsFeature(page, true)
  await page.goto("/dashboard#/skills")

  const installableSection = page.locator("section", { has: page.getByRole("heading", { name: "Installable Skills" }) })
  const activationSection = page.locator("section", { has: page.getByRole("heading", { name: "Agent Activation" }) })

  await installableSection.locator("article", { hasText: "scrutiny" }).getByRole("button", { name: "Install" }).click()
  await expect(page.getByText("Installed scrutiny.")).toBeVisible()

  const scrutinyRow = installableSection.locator("article", { hasText: "scrutiny" })
  await expect(scrutinyRow.getByRole("button", { name: "Installed" })).toBeVisible()

  const activationRow = activationSection.locator("article", { hasText: "scrutiny" })
  await activationRow.getByRole("button", { name: "Activate" }).click()
  await expect(page.getByText("Activated scrutiny for default.")).toBeVisible()
  await expect(activationRow.getByRole("button", { name: "Deactivate" })).toBeVisible()

  await activationRow.getByRole("button", { name: "Deactivate" }).click()
  await expect(page.getByText("Deactivated scrutiny for default.")).toBeVisible()
  await expect(activationRow.getByRole("button", { name: "Activate" })).toBeVisible()

  await page.getByLabel("Agent").selectOption("ops")
  await expect(activationRow.getByText("Not active for ops.")).toBeVisible()
})

test("reload button refetches and displays load/action errors", async ({ page }) => {
  const state: SkillsApiState = {
    availableAgents: ["default"],
    installableCatalog: ["playwrite", "scrutiny"],
    installedSkills: ["scrutiny"],
    activatedByAgent: { default: [] },
    getCount: 0,
    failNextGet: true,
    failNextInstall: true,
    failNextActivate: true,
    failNextDeactivate: false,
  }

  await routeSkillsApi(page, state)
  await routeInstanceAgentsFeature(page, true)
  await page.goto("/dashboard#/skills")

  await expect(page.getByText("Failed to load skills: skills list failed")).toBeVisible()

  const beforeReload = state.getCount
  await page.getByRole("button", { name: "Reload" }).click()
  await expect.poll(() => state.getCount).toBeGreaterThan(beforeReload)
  await expect(page.getByText("Skills loaded.")).toBeVisible()

  await page.locator("article", { hasText: "playwrite" }).getByRole("button", { name: "Install" }).click()
  await expect(page.getByText("Failed to install playwrite: install failed")).toBeVisible()

  await page.locator("article", { hasText: "scrutiny" }).getByRole("button", { name: "Activate" }).click()
  await expect(page.getByText("Failed to update scrutiny: activate failed")).toBeVisible()
})

test("hides skills nav and skips skills API work when instance agents are disabled", async ({ page }) => {
  const state: SkillsApiState = {
    availableAgents: ["default"],
    installableCatalog: ["playwrite", "scrutiny"],
    installedSkills: ["playwrite"],
    activatedByAgent: { default: ["playwrite"] },
    getCount: 0,
    failNextGet: false,
    failNextInstall: false,
    failNextActivate: false,
    failNextDeactivate: false,
  }

  await routeSkillsApi(page, state)
  await routeInstanceAgentsFeature(page, false)
  await page.goto("/dashboard#/skills")

  await expect(page.getByTestId("skills-disabled-state")).toContainText("Skills disabled")
  await expect(page.getByTestId("skills-disabled-state")).toContainText("Instance agent controls are disabled")
  await expect(page.getByRole("link", { name: "Skills" })).toHaveCount(0)
  await expect.poll(() => state.getCount).toBe(0)
  await expect(page.getByRole("button", { name: "Reload" })).toBeDisabled()
})
