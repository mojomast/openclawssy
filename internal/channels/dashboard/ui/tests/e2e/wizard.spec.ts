import { expect, test, type Page, type Route } from "@playwright/test"

type WizardState = {
  templateCalls: number
  instancePlanCalls: Array<Record<string, unknown>>
  instanceCreateCalls: Array<Record<string, unknown>>
  duplicateInstanceIDs: Set<string>
  instancesCalls: number
  activeInstanceCalls: number
  instanceAgentListCalls: string[]
  agentPlanCalls: Array<Record<string, unknown>>
  agentCreateCalls: Array<Record<string, unknown>>
  agentsByInstance: Record<string, string[]>
}

async function installWizardMocks(page: Page, options?: { wizardEnabled?: boolean }): Promise<WizardState> {
  const state: WizardState = {
    templateCalls: 0,
    instancePlanCalls: [],
    instanceCreateCalls: [],
    duplicateInstanceIDs: new Set<string>(),
    instancesCalls: 0,
    activeInstanceCalls: 0,
    instanceAgentListCalls: [],
    agentPlanCalls: [],
    agentCreateCalls: [],
    agentsByInstance: {
      existing: ["operator"],
      "automation-ops": [],
    },
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

    if (pathname === "/api/admin/instances" && method === "GET") {
      state.instancesCalls += 1
      const dynamicInstances = Object.keys(state.agentsByInstance)
        .filter((id) => id !== "existing" && id !== "automation-ops")
        .map((id) => ({ id, name: id.split("-").map((part) => part.charAt(0).toUpperCase() + part.slice(1)).join(" ") }))
      await json({
        instances: [
          { id: "existing", name: "Existing" },
          { id: "automation-ops", name: "Automation Ops" },
          ...dynamicInstances,
        ],
        active_instance_id: "existing",
      })
      return
    }

    if (pathname === "/api/admin/instances/active" && method === "GET") {
      state.activeInstanceCalls += 1
      await json({ instance: { id: "existing", name: "Existing" } })
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

    if (pathname === "/api/admin/wizard/instances/plan" && method === "POST") {
      const payload = JSON.parse(request.postData() || "{}") as Record<string, unknown>
      state.instancePlanCalls.push(payload)
      const templateID = String(payload.template_id || "blank")
      const instanceID = String(payload.instance_id || "").trim()
      const modelProvider = String(payload.model_provider || "")
      const modelName = String(payload.model_name || "")
      const defaultAgentID = String(payload.default_agent_id || "")
      await json({
        plan: {
          instance: {
            id: instanceID,
            name: String(payload.name || instanceID),
            description: String(payload.description || ""),
            template: templateID,
            source: "wizard",
            config: {
              model: {
                provider: modelProvider || "openai",
                name: modelName || "gpt-4.1-mini",
              },
              chat: {
                default_agent_id: defaultAgentID || "default",
              },
            },
          },
          operations: [
            "create instance metadata",
            "persist instance config snapshot",
            ...(templateID === "chat-assistant" ? ["set chat channel default agent"] : []),
          ],
        },
      })
      return
    }

    if (pathname === "/api/admin/wizard/instances/create" && method === "POST") {
      const payload = JSON.parse(request.postData() || "{}") as Record<string, unknown>
      state.instanceCreateCalls.push(payload)
      const instanceID = String(payload.instance_id || "").trim()
      if (state.duplicateInstanceIDs.has(instanceID)) {
        await json({ error: { code: "instances.duplicate_id", message: "instance already exists" } }, 409)
        return
      }
      state.duplicateInstanceIDs.add(instanceID)
      if (!state.agentsByInstance[instanceID]) {
        state.agentsByInstance[instanceID] = []
      }
      await json({
        ok: true,
        instance: {
          id: instanceID,
          name: String(payload.name || instanceID),
          description: String(payload.description || ""),
          template: String(payload.template_id || "blank"),
          source: "wizard",
          config: {
            model: {
              provider: String(payload.model_provider || "openai"),
              name: String(payload.model_name || "gpt-4.1-mini"),
            },
          },
        },
      }, 201)
      return
    }

    const instanceAgentsMatch = pathname.match(/^\/api\/admin\/instances\/([^/]+)\/agents$/)
    if (instanceAgentsMatch && method === "GET") {
      const instanceID = decodeURIComponent(instanceAgentsMatch[1])
      state.instanceAgentListCalls.push(instanceID)
      await json({
        instance_id: instanceID,
        agents: (state.agentsByInstance[instanceID] || []).map((agentID) => ({ agent_id: agentID })),
      })
      return
    }

    if (pathname === "/api/admin/wizard/agents/plan" && method === "POST") {
      const payload = JSON.parse(request.postData() || "{}") as Record<string, unknown>
      state.agentPlanCalls.push(payload)
      await json({
        plan: {
          instance_id: String(payload.instance_id || ""),
          agent_id: String(payload.agent_id || "").trim(),
          template_id: String(payload.template_id || "general"),
          operations: ["normalize agent profile", "validate instance config with agent overrides"],
          profile: {
            enabled: Boolean(payload.enabled),
            self_improvement: Boolean(payload.self_improvement),
            model: {
              provider: String(payload.model_provider || "openai"),
              name: String(payload.model_name || "gpt-4.1-mini"),
              timeout_ms: Number(payload.model_timeout_ms || 0),
            },
          },
        },
      })
      return
    }

    if (pathname === "/api/admin/wizard/agents/create" && method === "POST") {
      const payload = JSON.parse(request.postData() || "{}") as Record<string, unknown>
      const instanceID = String(payload.instance_id || "")
      const agentID = String(payload.agent_id || "").trim()
      state.agentCreateCalls.push(payload)
      if ((state.agentsByInstance[instanceID] || []).includes(agentID)) {
        await json({ error: { code: "instances.agent_exists", message: "agent already exists" } }, 409)
        return
      }
      state.agentsByInstance[instanceID] = [...(state.agentsByInstance[instanceID] || []), agentID]
      await json({
        ok: true,
        instance_id: instanceID,
        agent: {
          agent_id: agentID,
          profile: {
            enabled: Boolean(payload.enabled),
            self_improvement: Boolean(payload.self_improvement),
            model: {
              provider: String(payload.model_provider || "openai"),
              name: String(payload.model_name || "gpt-4.1-mini"),
              timeout_ms: Number(payload.model_timeout_ms || 0),
            },
          },
        },
      }, 201)
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

test("Wizard previews an instance plan and shows config summary", async ({ page }) => {
  const state = await installWizardMocks(page)

  await page.goto("/dashboard#/wizard")
  await page.getByTestId("wizard-instance-template-chat-assistant").click()
  await page.getByTestId("wizard-instance-name").fill("Support Assistant")
  await page.getByTestId("wizard-instance-id").fill("support-assistant")
  await page.getByTestId("wizard-model-provider").fill("openrouter")
  await page.getByTestId("wizard-model-name").fill("moonshot/test")
  await page.getByTestId("wizard-default-agent-id").fill("assistant")
  await page.getByTestId("wizard-preview-instance").click()

  await expect.poll(() => state.instancePlanCalls.length).toBe(1)
  await expect(page.getByTestId("wizard-instance-plan-summary")).toContainText("support-assistant")
  await expect(page.getByTestId("wizard-instance-operations")).toContainText("create instance metadata")
  await expect(page.getByText('"provider": "openrouter"')).toBeVisible()
})

test("Wizard creates an instance and shows success state", async ({ page }) => {
  const state = await installWizardMocks(page)

  await page.goto("/dashboard#/wizard")
  await page.getByTestId("wizard-instance-name").fill("Automation Ops")
  await page.getByTestId("wizard-instance-id").fill("automation-ops")
  await page.getByTestId("wizard-model-provider").fill("openai")
  await page.getByTestId("wizard-model-name").fill("gpt-4.1-mini")
  await page.getByTestId("wizard-preview-instance").click()
  await expect.poll(() => state.instancePlanCalls.length).toBe(1)

  await page.getByTestId("wizard-create-instance").click()
  await expect.poll(() => state.instanceCreateCalls.length).toBe(1)
  await expect(page.getByTestId("wizard-instance-success")).toContainText("Created instance Automation Ops (automation-ops)")
})

test("Wizard surfaces duplicate instance create errors cleanly", async ({ page }) => {
  const state = await installWizardMocks(page)
  state.duplicateInstanceIDs.add("existing-instance")

  await page.goto("/dashboard#/wizard")
  await page.getByTestId("wizard-instance-id").fill("existing-instance")
  await page.getByTestId("wizard-instance-name").fill("Existing Instance")
  await page.getByTestId("wizard-create-instance").click()

  await expect.poll(() => state.instanceCreateCalls.length).toBe(1)
  await expect(page.getByText("Failed to create instance: instance already exists")).toBeVisible()
})

test("Wizard previews and creates an agent in an existing instance", async ({ page }) => {
  const state = await installWizardMocks(page)

  await page.goto("/dashboard#/wizard")
  await expect.poll(() => state.instancesCalls).toBe(1)
  await expect.poll(() => state.activeInstanceCalls).toBe(1)
  await expect.poll(() => state.instanceAgentListCalls.includes("existing")).toBeTruthy()

  await page.getByTestId("wizard-agent-template-research").click()
  await page.getByTestId("wizard-agent-id").fill("researcher")
  await page.getByTestId("wizard-agent-model-provider").fill("openai")
  await page.getByTestId("wizard-agent-model-name").fill("gpt-4.1-mini")
  await page.getByTestId("wizard-agent-timeout").fill("180000")
  await page.getByTestId("wizard-agent-self-improvement").check()
  await page.getByTestId("wizard-preview-agent").click()

  await expect.poll(() => state.agentPlanCalls.length).toBe(1)
  await expect(page.getByTestId("wizard-agent-plan-summary")).toContainText("researcher")
  await expect(page.getByTestId("wizard-agent-operations")).toContainText("normalize agent profile")
  await expect(page.getByText('"self_improvement": true')).toBeVisible()

  await page.getByTestId("wizard-create-agent").click()
  await expect.poll(() => state.agentCreateCalls.length).toBe(1)
  await expect(page.getByTestId("wizard-agent-success")).toContainText("Created agent researcher in existing")
})

test("Wizard can create an instance and then target it for agent creation in the same session", async ({ page }) => {
  const state = await installWizardMocks(page)

  await page.goto("/dashboard#/wizard")
  await page.getByTestId("wizard-instance-id").fill("session-flow")
  await page.getByTestId("wizard-instance-name").fill("Session Flow")
  await page.getByTestId("wizard-create-instance").click()

  await expect.poll(() => state.instanceCreateCalls.length).toBe(1)
  await expect(page.getByTestId("wizard-agent-instance")).toHaveValue("session-flow")

  await page.getByTestId("wizard-agent-id").fill("builder")
  await page.getByTestId("wizard-preview-agent").click()
  await expect.poll(() => state.agentPlanCalls.length).toBe(1)
})

test("Wizard disables instance actions when instance control is off", async ({ page }) => {
  const state = await installWizardMocks(page, { wizardEnabled: true })

  await page.route("**/api/admin/control-plane/features", async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json; charset=utf-8",
      body: JSON.stringify({
        features: {
          instance_control: false,
          instance_agents: true,
          wizard: true,
          eval: true,
        },
      }),
    })
  })

  await page.goto("/dashboard#/wizard")

  await expect(page.getByTestId("wizard-instance-disabled-state")).toContainText("Instance creation disabled")
  await expect(page.getByTestId("wizard-preview-instance")).toBeDisabled()
  await expect(page.getByTestId("wizard-create-instance")).toBeDisabled()
  await expect.poll(() => state.instancePlanCalls.length).toBe(0)
})

test("Wizard blocks duplicate agent creation in selected instance", async ({ page }) => {
  const state = await installWizardMocks(page)

  await page.goto("/dashboard#/wizard")
  await expect.poll(() => state.instanceAgentListCalls.includes("existing")).toBeTruthy()

  await page.getByTestId("wizard-agent-id").fill("operator")
  await expect(page.getByTestId("wizard-agent-duplicate-warning")).toContainText("already exists")
  await expect(page.getByTestId("wizard-create-agent")).toBeDisabled()
})
