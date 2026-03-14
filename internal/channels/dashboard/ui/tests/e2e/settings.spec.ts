import { expect, test, type Page, type Route } from "@playwright/test"

type SettingsApiState = {
  config: Record<string, unknown>
  agents: string[]
  secretKeys: string[]
  configGetCount: number
  patchBodies: Array<Record<string, unknown>>
}

function deepClone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

function createBaseConfig(): Record<string, unknown> {
  return {
    server: { bind_address: "127.0.0.1", port: 8080 },
    workspace: { root: "./workspace" },
    output: { thinking_mode: "on_error", max_thinking_chars: 4000 },
    engine: { max_concurrent_runs: 32 },
    model: { provider: "hatz", name: "glm-4.5", temperature: 0.2, max_tokens: 4096, timeout_ms: 90000 },
    providers: {
      openai: { base_url: "", api_key_env: "OPENAI_API_KEY" },
      openrouter: { base_url: "", api_key_env: "OPENROUTER_API_KEY" },
      requesty: { base_url: "", api_key_env: "REQUESTY_API_KEY" },
      hatz: { base_url: "https://api.hatz.ai", api_key_env: "HATZ_API_KEY" },
      zai: { base_url: "", api_key_env: "ZAI_API_KEY" },
      generic: { base_url: "", api_key_env: "GENERIC_API_KEY" },
    },
    chat: {
      enabled: true,
      default_agent_id: "default",
      rate_limit_per_min: 30,
      global_rate_limit_per_min: 300,
      allow_users: ["dashboard_user"],
      allow_rooms: ["ops"],
    },
    discord: {
      enabled: false,
      default_agent_id: "default",
      token_env: "DISCORD_BOT_TOKEN",
      command_prefix: "!ask",
      rate_limit_per_min: 30,
      allow_guilds: [],
      allow_channels: [],
      allow_users: [],
    },
    telegram: {
      enabled: false,
      default_agent_id: "default",
      token_env: "TELEGRAM_BOT_TOKEN",
      command_prefix: "/ask",
      rate_limit_per_min: 30,
      allow_users: [],
      allow_chats: [],
    },
    agents: {
      allow_inter_agent_messaging: true,
      allow_agent_model_overrides: true,
      self_improvement_enabled: false,
      enabled_agent_ids: ["default", "reviewer"],
      profiles: {
        default: { enabled: true, self_improvement: false, model: {} },
        reviewer: { enabled: true, self_improvement: false, model: { provider: "openai", name: "gpt-4o-mini" } },
      },
      subagent_defaults: {
        allowed_tools: ["fs.read"],
        timeout_ms: 45000,
        max_tool_iterations: 12,
        thinking_mode: "on_error",
        delegation_mode: "tool_gated",
      },
      subagent_overrides: {
        reviewer: {
          allowed_tools: ["fs.read", "code.search"],
          timeout_ms: 30000,
          thinking_mode: "never",
        },
      },
    },
    memory: {
      enabled: true,
      max_working_items: 200,
      max_prompt_tokens: 1200,
      auto_checkpoint: true,
      proactive_enabled: false,
      event_buffer_size: 256,
      embeddings_enabled: true,
      embedding_provider: "openai",
      embedding_model: "text-embedding-3-small",
    },
    sandbox: { active: false, provider: "none" },
    shell: {
      enable_exec: false,
      default_timeout_ms: 120000,
      max_timeout_ms: 300000,
      allowed_commands: ["python3", "node"],
    },
    network: { enabled: false, allow_localhosts: false, allowed_domains: ["api.openai.com"] },
    scheduler: { catch_up: true, max_concurrent_jobs: 4 },
  }
}

async function routeSettingsApi(page: Page, state: SettingsApiState) {
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

    if (pathname === "/api/admin/agents" && method === "GET") {
      await json({ agents: state.agents })
      return
    }

    if (pathname === "/api/admin/config" && method === "GET") {
      state.configGetCount += 1
      await json(state.config)
      return
    }

    if (pathname === "/api/admin/config" && method === "PATCH") {
      const payload = JSON.parse(request.postData() || "{}") as Record<string, unknown>
      state.patchBodies.push(payload)
      state.config = deepClone(payload)
      await json({ ok: true })
      return
    }

    if (pathname === "/api/admin/config/validate" && method === "POST") {
      await json({ ok: true })
      return
    }

    if (pathname === "/api/admin/providers/test" && method === "POST") {
      await json({ ok: true, status_text: "Provider reachable" })
      return
    }

    if (pathname === "/api/admin/providers/models" && method === "GET") {
      await json({ models: ["glm-4.5", "glm-4.7"] })
      return
    }

    if (pathname === "/api/admin/secrets" && method === "GET") {
      await json({ keys: state.secretKeys })
      return
    }

    if (pathname === "/api/admin/secrets" && method === "POST") {
      const payload = JSON.parse(request.postData() || "{}") as { name?: string }
      const key = String(payload.name || "").trim()
      if (key && !state.secretKeys.includes(key)) {
        state.secretKeys.push(key)
      }
      await json({ ok: true })
      return
    }

    if (pathname.startsWith("/api/admin/secrets/") && method === "DELETE") {
      const key = decodeURIComponent(pathname.replace("/api/admin/secrets/", ""))
      state.secretKeys = state.secretKeys.filter((entry) => entry !== key)
      await json({ ok: true })
      return
    }

    if (pathname.startsWith("/api/") || pathname.startsWith("/v1/")) {
      await json({ ok: true })
      return
    }

    await route.continue()
  })
}

function createState(): SettingsApiState {
  return {
    config: createBaseConfig(),
    agents: ["default", "reviewer", "ops"],
    secretKeys: ["discord/bot_token"],
    configGetCount: 0,
    patchBodies: [],
  }
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("openclawssy.dashboard.bearer", "e2e-token")
  })
})

test("renders all settings categories, supports search filtering, and honors category/profile deep-link params", async ({ page }) => {
  const state = createState()
  await routeSettingsApi(page, state)

  await page.goto("/dashboard#/settings?category=model&profile=reviewer")

  await expect(page.getByRole("heading", { name: "Settings", level: 2 })).toBeVisible()
  await expect(page.getByRole("heading", { name: "Model Provider", level: 3 })).toBeVisible()

  const categoryButtons = page.locator(".settings-categories .settings-category-button")
  await expect(categoryButtons).toHaveCount(10)

  await page.getByRole("button", { name: /Agents/ }).click()
  await expect(page.getByRole("heading", { name: "Agents", level: 3 })).toBeVisible()
  await expect(page.locator("label.settings-field:has-text('Profile agent') select")).toHaveValue("reviewer")

  await page.getByPlaceholder("Search categories, fields, or values").fill("sandbox")
  await expect(page.getByRole("button", { name: /Sandbox\/Shell/ })).toBeVisible()
  await expect(page.getByRole("button", { name: /General/ })).toHaveCount(0)
})

test("editing fields updates the diff table and Save Config PATCHes with success feedback", async ({ page }) => {
  const state = createState()
  await routeSettingsApi(page, state)

  await page.goto("/dashboard#/settings")

  await page.locator("label.settings-field:has-text('Server port') input").fill("9090")
  await expect(page.getByText(/Diff before save \([1-9]\d* changed path/)).toBeVisible()

  await page.getByRole("button", { name: "Save Config" }).click()
  await expect.poll(() => state.patchBodies.length).toBeGreaterThan(0)
  await expect(page.locator("label.settings-field:has-text('Server port') input")).toHaveValue("9090")

  const lastPatch = state.patchBodies.at(-1) as Record<string, unknown>
  const server = lastPatch.server as { port?: number }
  expect(server.port).toBe(9090)
})

test("Reload Config refetches and restores server state", async ({ page }) => {
  const state = createState()
  await routeSettingsApi(page, state)

  await page.goto("/dashboard#/settings")
  await expect(page.locator("label.settings-field:has-text('Server port') input")).toHaveValue("8080")

  await page.locator("label.settings-field:has-text('Server port') input").fill("9191")
  const beforeReloadCount = state.configGetCount

  await page.getByRole("button", { name: "Reload" }).click()
  await expect.poll(() => state.configGetCount).toBeGreaterThan(beforeReloadCount)
  await expect(page.locator("label.settings-field:has-text('Server port') input")).toHaveValue("8080")
})

test("provider test and model discovery actions render connectivity and discovered-model results", async ({ page }) => {
  const state = createState()
  await routeSettingsApi(page, state)

  await page.goto("/dashboard#/settings?category=model")

  const hatzCard = page
    .locator("section.settings-section")
    .filter({ has: page.locator("h5.settings-subheading", { hasText: "hatz" }) })
    .first()

  await hatzCard.getByRole("button", { name: "Test provider" }).click()
  await expect(hatzCard.getByText("Provider reachable")).toBeVisible()

  await hatzCard.getByRole("button", { name: "Query models" }).click()
  await expect(hatzCard.getByText("glm-4.7")).toBeVisible()

  await hatzCard.getByRole("button", { name: "glm-4.7" }).click()
  await expect(page.locator("label.settings-field:has-text('Model name') select")).toHaveValue("glm-4.7")
})

test("agents category renders profile editor and subagent defaults with editable values", async ({ page }) => {
  const state = createState()
  await routeSettingsApi(page, state)

  await page.goto("/dashboard#/settings?category=agents&profile=reviewer")

  await expect(page.getByText("Agent profile summary")).toBeVisible()
  await expect(page.getByText("Agent Profile Editor")).toBeVisible()
  await expect(page.getByText("Subagent defaults")).toBeVisible()
  await expect(page.locator("label.settings-field:has-text('Profile agent') select")).toHaveValue("reviewer")

  await page.locator("label.settings-field:has-text('Subagent timeout ms') input").fill("60000")
  await expect(page.locator(".settings-diff-table code", { hasText: "agents.subagent_defaults.timeout_ms" })).toBeVisible()
})

test("advanced raw JSON editor applies valid JSON and surfaces parse errors for invalid JSON", async ({ page }) => {
  const state = createState()
  await routeSettingsApi(page, state)

  await page.goto("/dashboard#/settings?category=advanced")

  const rawEditor = page.locator("textarea.settings-raw-editor")
  await rawEditor.fill("{not-valid}")
  await page.getByRole("button", { name: "Apply JSON to Draft" }).click()
  await expect(page.getByText(/JSON parse error:/)).toBeVisible()

  const validDraft = createBaseConfig()
  const validServer = validDraft.server as { port?: number }
  validServer.port = 7777
  await rawEditor.fill(`${JSON.stringify(validDraft, null, 2)}\n`)
  await page.getByRole("button", { name: "Apply JSON to Draft" }).click()

  await expect(page.getByText(/Diff before save \([1-9]\d* changed path/)).toBeVisible()
  await expect(page.locator(".settings-diff-table code", { hasText: "server.port" })).toBeVisible()
})

test("unsaved changes warning appears on route navigation and blocks until discard is confirmed", async ({ page }) => {
  const state = createState()
  await routeSettingsApi(page, state)

  await page.goto("/dashboard#/settings")
  await page.locator("label.settings-field:has-text('Server port') input").fill("9191")

  page.once("dialog", async (dialog) => {
    expect(dialog.message()).toContain("unsaved settings changes")
    await dialog.dismiss()
  })
  await page.getByRole("link", { name: "Chat" }).click()
  await expect(page).toHaveURL(/#\/settings/)

  page.once("dialog", async (dialog) => {
    expect(dialog.message()).toContain("unsaved settings changes")
    await dialog.accept()
  })
  await page.getByRole("link", { name: "Chat" }).click()
  await expect(page).toHaveURL(/#\/chat/)
})
