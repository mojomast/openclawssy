import { expect, test, type Page, type Route } from "@playwright/test"

const DOC_ORDER = ["SOUL.md", "RULES.md", "TOOLS.md", "SPECPLAN.md", "DEVPLAN.md", "HANDOFF.md", "HEARTBEAT.md"]

type DocStore = Record<string, string>

type DocsApiState = {
  availableAgents: string[]
  docsByAgent: Record<string, DocStore>
  getCount: number
  failNextGet: boolean
  failNextPost: boolean
}

function createDefaultDocStore(agentID: string): DocStore {
  return {
    "SOUL.md": `# SOUL\n${agentID} soul`,
    "RULES.md": `# RULES\n${agentID} rules`,
    "TOOLS.md": `# TOOLS\n${agentID} tools`,
    "SPECPLAN.md": `# SPECPLAN\n${agentID} specplan`,
    "DEVPLAN.md": `# DEVPLAN\n${agentID} devplan`,
    "HANDOFF.md": `# HANDOFF\n${agentID} handoff`,
  }
}

function buildDocsPayload(store: DocStore) {
  return DOC_ORDER.map((name) => {
    const resolvedName = name === "HEARTBEAT.md" ? "HANDOFF.md" : name
    const aliasFor = name === "HEARTBEAT.md" ? "HANDOFF.md" : ""
    const content = String(store[resolvedName] || "")
    const exists = Object.prototype.hasOwnProperty.call(store, resolvedName)
    return {
      name,
      resolved_name: resolvedName,
      alias_for: aliasFor,
      content,
      exists,
    }
  })
}

async function routeDocsApi(page: Page, state: DocsApiState) {
  await page.route("**/api/admin/agent/docs**", async (route: Route) => {
    const url = new URL(route.request().url())
    const method = route.request().method()

    if (method === "GET") {
      state.getCount += 1
      if (state.failNextGet) {
        state.failNextGet = false
        await route.fulfill({ status: 500, body: "load failed" })
        return
      }

      const requestedAgent = (url.searchParams.get("agent_id") || "default").trim() || "default"
      const docsStore = state.docsByAgent[requestedAgent] || createDefaultDocStore(requestedAgent)
      state.docsByAgent[requestedAgent] = docsStore

      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          agent_id: requestedAgent,
          available_agents: state.availableAgents,
          documents: buildDocsPayload(docsStore),
        }),
      })
      return
    }

    if (method === "POST") {
      if (state.failNextPost) {
        state.failNextPost = false
        await route.fulfill({ status: 500, body: "write failed" })
        return
      }

      const payload = JSON.parse(route.request().postData() || "{}") as {
        agent_id?: string
        name?: string
        content?: string
      }
      const agentID = String(payload.agent_id || "default").trim() || "default"
      const requestedName = String(payload.name || "").trim()
      const resolvedName = requestedName === "HEARTBEAT.md" ? "HANDOFF.md" : requestedName
      const docsStore = state.docsByAgent[agentID] || createDefaultDocStore(agentID)
      docsStore[resolvedName] = String(payload.content || "")
      state.docsByAgent[agentID] = docsStore

      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ ok: true }),
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

test("loads docs with agent/document selectors and editor", async ({ page }) => {
  const state: DocsApiState = {
    availableAgents: ["default", "ops"],
    docsByAgent: {
      default: createDefaultDocStore("default"),
      ops: createDefaultDocStore("ops"),
    },
    getCount: 0,
    failNextGet: false,
    failNextPost: false,
  }

  await routeDocsApi(page, state)
  await routeInstanceAgentsFeature(page, true)

  await page.goto("/dashboard#/docs")

  await expect(page.getByRole("heading", { name: "Docs", level: 2 })).toBeVisible()
  await expect(page.getByLabel("Agent")).toBeVisible()
  await expect(page.locator("#docs-document")).toBeVisible()
  await expect(page.getByLabel("Document content")).toContainText("default soul")
  await expect(page.getByText("Docs loaded.")).toBeVisible()
})

test("shows unsaved indicator and saves selected document", async ({ page }) => {
  const state: DocsApiState = {
    availableAgents: ["default", "ops"],
    docsByAgent: {
      default: createDefaultDocStore("default"),
      ops: createDefaultDocStore("ops"),
    },
    getCount: 0,
    failNextGet: false,
    failNextPost: false,
  }

  await routeDocsApi(page, state)
  await routeInstanceAgentsFeature(page, true)

  await page.goto("/dashboard#/docs")
  const editor = page.getByLabel("Document content")

  await editor.fill("# SOUL\nupdated from test")
  await expect(page.locator("#docs-document option", { hasText: "SOUL.md *" })).toHaveCount(1)

  await page.getByRole("button", { name: "Save selected doc" }).click()
  await expect(page.getByText("Saved SOUL.md for default.")).toBeVisible()
  await expect(page.locator("#docs-document option", { hasText: "SOUL.md *" })).toHaveCount(0)
})

test("reload button and switching agent refetch docs", async ({ page }) => {
  const state: DocsApiState = {
    availableAgents: ["default", "ops"],
    docsByAgent: {
      default: createDefaultDocStore("default"),
      ops: createDefaultDocStore("ops"),
    },
    getCount: 0,
    failNextGet: false,
    failNextPost: false,
  }

  await routeDocsApi(page, state)
  await routeInstanceAgentsFeature(page, true)

  await page.goto("/dashboard#/docs")
  await expect(page.getByLabel("Document content")).toContainText("default soul")

  const initialGetCount = state.getCount
  await page.getByRole("button", { name: "Reload docs" }).click()
  await expect.poll(() => state.getCount).toBeGreaterThan(initialGetCount)

  const beforeSwitchCount = state.getCount
  await page.getByLabel("Agent").selectOption("ops")
  await expect.poll(() => state.getCount).toBeGreaterThan(beforeSwitchCount)
  await expect(page.getByLabel("Document content")).toContainText("ops soul")
})

test("shows load and save error status messages", async ({ page }) => {
  const state: DocsApiState = {
    availableAgents: ["default", "ops"],
    docsByAgent: {
      default: createDefaultDocStore("default"),
      ops: createDefaultDocStore("ops"),
    },
    getCount: 0,
    failNextGet: true,
    failNextPost: true,
  }

  await routeDocsApi(page, state)
  await routeInstanceAgentsFeature(page, true)

  await page.goto("/dashboard#/docs")
  await expect(page.getByText("Failed to load docs: load failed")).toBeVisible()

  await page.getByRole("button", { name: "Reload docs" }).click()
  await expect(page.getByText("Docs loaded.")).toBeVisible()

  await page.getByLabel("Document content").fill("# SOUL\nwrite error")
  await page.getByRole("button", { name: "Save selected doc" }).click()
  await expect(page.getByText("Failed to save SOUL.md: write failed")).toBeVisible()
})

test("hides docs nav and skips docs API work when instance agents are disabled", async ({ page }) => {
  const state: DocsApiState = {
    availableAgents: ["default"],
    docsByAgent: {
      default: createDefaultDocStore("default"),
    },
    getCount: 0,
    failNextGet: false,
    failNextPost: false,
  }

  await routeDocsApi(page, state)
  await routeInstanceAgentsFeature(page, false)

  await page.goto("/dashboard#/docs")

  await expect(page.getByTestId("docs-disabled-state")).toContainText("Docs disabled")
  await expect(page.getByTestId("docs-disabled-state")).toContainText("Instance agent controls are disabled")
  await expect(page.getByRole("link", { name: "Docs" })).toHaveCount(0)
  await expect.poll(() => state.getCount).toBe(0)
  await expect(page.getByRole("button", { name: "Reload docs" })).toBeDisabled()
  await expect(page.getByRole("button", { name: "Save selected doc" })).toBeDisabled()
})
