import { expect, test, type Page, type Route } from "@playwright/test"

const NAV_ITEMS: Array<{ label: string; path: string }> = [
  { label: "Help", path: "/help" },
  { label: "Workspace", path: "/workspace" },
  { label: "Secrets", path: "/secrets" },
  { label: "Docs", path: "/docs" },
  { label: "Skills", path: "/skills" },
  { label: "Chat", path: "/chat" },
  { label: "Settings", path: "/settings" },
  { label: "Runs", path: "/runs" },
  { label: "Sessions", path: "/sessions" },
  { label: "Monitor", path: "/monitor" },
  { label: "Scheduler", path: "/scheduler" },
  { label: "Sandbox", path: "/sandbox" },
  { label: "Dashboards", path: "/dashboards" },
  { label: "Agent Contract", path: "/agent-contract" },
  { label: "Prompt Stack", path: "/prompt-stack" },
  { label: "Role Templates", path: "/roles" },
  { label: "Delegation", path: "/delegation" },
  { label: "Eval", path: "/eval" },
]

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: "application/json; charset=utf-8",
    body: JSON.stringify(body),
  })
}

function minimalResolvedContract(agentID: string) {
  return {
    identity: { agent_id: agentID, display_name: agentID },
    mission: { description: "minimal" },
    system_prompt: { content: "minimal prompt", source: "prompt_stack" },
    tool_policy: { allowed_tools: ["fs.read"], denied_tools: [], default_deny: false },
    delegation_policy: {
      mode: "auto_execute",
      threshold: 1,
      cooldown: 1,
      auto_delegate: true,
      agent_id: "default",
      max_depth: 2,
      role_templates: [],
    },
    memory_policy: {
      enabled: true,
      max_working_items: 200,
      max_prompt_tokens: 64000,
      auto_checkpoint: true,
      proactive: false,
      embeddings: false,
    },
    model_policy: {
      provider: "hatz",
      model: "glm-4.5",
      temperature: 0,
      max_tokens: 0,
      timeout_ms: 120000,
    },
    sandbox_policy: {
      active: false,
      provider: "docker",
      docker: {
        image: "ubuntu:24.04",
        network_enabled: false,
        cpu_limit: 1,
        memory_limit_mb: 1024,
      },
    },
    observability_policy: { audit_enabled: true, trace_enabled: true, thinking_mode: "summary" },
    inheritance: {
      source: {
        identity: "global",
        mission: "global",
        system_prompt: "agent-profile",
        tool_policy: "global",
        delegation_policy: "global",
        memory_policy: "global",
        model_policy: "global",
        sandbox_policy: "global",
        observability_policy: "global",
      },
    },
  }
}

async function installMinimalDashboardMocks(page: Page) {
  await page.route("**/*", async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const { pathname } = url

    if (pathname === "/api/admin/status") {
      await json(route, {
        ok: true,
        model: { provider: "hatz", name: "glm-4.5" },
        run_count: 0,
      })
      return
    }

    if (pathname === "/api/admin/agents") {
      await json(route, { agents: ["default"], selected_agent: "default" })
      return
    }

    if (pathname === "/api/admin/config") {
      await json(route, {
        model: { provider: "hatz", name: "glm-4.5", timeout_ms: 120000 },
        agents: { enabled_agent_ids: ["default"], profiles: {} },
        dashboard: { enabled: true },
      })
      return
    }

    if (pathname.startsWith("/api/admin/agents/") && pathname.endsWith("/resolved")) {
      await json(route, minimalResolvedContract("default"))
      return
    }

    if (pathname.includes("/prompt-stack/")) {
      await json(route, { agent_id: "default", layers: [], assembled_prompt: "", differences: [] })
      return
    }

    if (pathname === "/api/admin/roles") {
      await json(route, { roles: [] })
      return
    }

    if (pathname === "/api/admin/eval/results") {
      await json(route, { suites: [], runs: [], metrics: {} })
      return
    }

    if (pathname === "/api/admin/workspace/entries") {
      await json(route, { workspace_root: "/tmp", path: ".", parent_path: "", entries: [] })
      return
    }

    if (pathname === "/api/admin/secrets") {
      await json(route, { secrets: [] })
      return
    }

    if (pathname === "/api/admin/skills") {
      await json(route, { skills: [] })
      return
    }

    if (pathname === "/api/admin/chat/sessions") {
      await json(route, { sessions: [], total: 0, limit: 25, offset: 0 })
      return
    }

    if (pathname === "/api/admin/monitor/runs") {
      await json(route, { runs: [] })
      return
    }

    if (pathname === "/api/admin/scheduler/jobs") {
      await json(route, { paused: false, jobs: [] })
      return
    }

    if (pathname === "/api/admin/dashboards") {
      await json(route, { dashboards: [] })
      return
    }

    if (pathname === "/v1/runs") {
      await json(route, { runs: [], total: 0, limit: 10, offset: 0 })
      return
    }

    if (pathname.startsWith("/api/") || pathname.startsWith("/v1/")) {
      await json(route, { ok: true })
      return
    }

    await route.continue()
  })
}

test("sidebar navigation reaches all 18 dashboard pages", async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("openclawssy.dashboard.bearer", "integration-token")
  })
  await installMinimalDashboardMocks(page)

  await page.goto("/dashboard#/help")

  for (const item of NAV_ITEMS) {
    await page.getByRole("link", { name: item.label }).first().click()
    await expect(page).toHaveURL(new RegExp(`#${item.path}(?:$|\\?)`))
  }
})

test("all dashboard routes are auth-gated when bearer token is missing", async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.removeItem("openclawssy.dashboard.bearer")
  })
  await installMinimalDashboardMocks(page)

  for (const item of NAV_ITEMS) {
    await page.goto(`/dashboard#${item.path}`)
    await expect(page.getByRole("heading", { name: "Dashboard access token required" })).toBeVisible()
  }
})
