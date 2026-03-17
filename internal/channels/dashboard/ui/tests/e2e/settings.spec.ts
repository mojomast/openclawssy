import { existsSync, readFileSync } from "node:fs"
import path from "node:path"
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
      await json({
        ok: true,
        model: { provider: "hatz", name: "glm-4.5" },
        run_count: 1,
        runtime: {
          server: { bind_address: "127.0.0.1", port: 8080 },
          workspace: { root: "/app/workspace" },
          sandbox: { active: true, provider: "docker" },
          shell: { enable_exec: true },
          output: { thinking_mode: "on_error" },
          engine: { max_concurrent_runs: 64 },
        },
      })
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

test("settings route is hardwired to React implementation and cannot delegate to legacy wrapper code", async () => {
  const appSource = readFileSync(path.resolve(process.cwd(), "src/App.tsx"), "utf8")
  const settingsSource = readFileSync(path.resolve(process.cwd(), "src/pages/SettingsPage.tsx"), "utf8")
  const legacySettingsPath = path.resolve(process.cwd(), "src/pages/settings.js")

  expect(appSource).toContain("from './pages/SettingsPage'")
  expect(appSource).toMatch(/<Route\s+path=\"settings\"\s+element=\{<SettingsPage\s*\/>\}\s*\/>/)
  expect(existsSync(legacySettingsPath)).toBeFalsy()

  expect(settingsSource).not.toMatch(/settings\.js/i)
  expect(settingsSource).not.toMatch(/settingsPage\.render/i)
  expect(settingsSource).not.toMatch(/disposeSettingsPage/i)
})

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

test("applies settings-specific layout styling so category navigation and controls remain visible", async ({ page }) => {
  const state = createState()
  await routeSettingsApi(page, state)

  await page.goto("/dashboard#/settings")

  const workspace = page.locator(".settings-workspace")
  await expect(workspace).toBeVisible()

  const workspaceDisplay = await workspace.evaluate((element) => window.getComputedStyle(element).display)
  expect(workspaceDisplay).toBe("grid")

  const categoryButtonStyles = await page.locator(".settings-category-button").first().evaluate((element) => {
    const styles = window.getComputedStyle(element)
    return {
      backgroundColor: styles.backgroundColor,
      borderRadius: styles.borderRadius,
      paddingTop: styles.paddingTop,
    }
  })

  expect(categoryButtonStyles.backgroundColor).not.toBe("rgba(0, 0, 0, 0)")
  expect(Number.parseFloat(categoryButtonStyles.borderRadius)).toBeGreaterThan(0)
  expect(Number.parseFloat(categoryButtonStyles.paddingTop)).toBeGreaterThan(0)
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

test("sandbox settings show effective runtime mode and use a constrained provider selector", async ({ page }) => {
  const state = createState()
  await routeSettingsApi(page, state)

  await page.goto("/dashboard#/settings?category=sandbox")

  await expect(page.locator("label.settings-field:has-text('Sandbox provider') select")).toHaveValue("docker")
  await expect(page.getByTestId("settings-runtime-sandbox-mode")).toContainText("`docker`")
  await expect(page.getByTestId("settings-runtime-sandbox-mode")).toContainText("sandbox enabled")

  await page.locator("label.settings-field:has-text('Sandbox provider') select").selectOption("local")
  await expect(page.getByTestId("settings-runtime-sandbox-override")).toBeVisible()
  await expect(page.locator(".settings-diff-table code", { hasText: "sandbox.provider" })).toBeVisible()
})

test("agents category renders profile editor model override controls and subagent defaults", async ({ page }) => {
  const state = createState()
  await routeSettingsApi(page, state)

  await page.goto("/dashboard#/settings?category=agents&profile=reviewer")

  await expect(page.getByText("Agent profile summary")).toBeVisible()
  await expect(page.getByText("Agent Profile Editor")).toBeVisible()
  await expect(page.getByRole("heading", { name: "Profile model override" })).toBeVisible()
  await expect(page.getByText("Subagent defaults")).toBeVisible()
  await expect(page.locator("label.settings-field:has-text('Profile agent') select")).toHaveValue("reviewer")
  await expect(page.locator("label.settings-field:has-text('Profile model provider') select")).toHaveValue("openai")
  await expect(page.locator("label.settings-field:has-text('Profile model name') input")).toHaveValue("gpt-4o-mini")

  await page.locator("label.settings-field:has-text('Profile model provider') select").selectOption("hatz")
  await expect(page.locator(".settings-diff-table code", { hasText: "agents.profiles.reviewer.model.provider" })).toBeVisible()

  await page.locator("label.settings-field:has-text('Profile model name') input").fill("glm-4.7")
  await expect(page.locator(".settings-diff-table code", { hasText: "agents.profiles.reviewer.model.name" })).toBeVisible()

  await page.locator("label.settings-field:has-text('Profile model max tokens') input").fill("8192")
  await expect(page.locator(".settings-diff-table code", { hasText: "agents.profiles.reviewer.model.max_tokens" })).toBeVisible()

  await page.locator("label.settings-field:has-text('Profile temperature') input").fill("0.3")
  await expect(page.locator(".settings-diff-table code", { hasText: "agents.profiles.reviewer.model.temperature" })).toBeVisible()

  await page.locator("label.settings-field:has-text('Profile provider timeout (ms)') input").fill("120000")
  await expect(page.locator(".settings-diff-table code", { hasText: "agents.profiles.reviewer.model.timeout_ms" })).toBeVisible()

  await page.getByRole("button", { name: "Clear profile model overrides" }).click()
  await expect(page.locator(".settings-diff-table code", { hasText: "agents.profiles.reviewer.model" })).toBeVisible()

  await expect(page.locator("label.settings-field:has-text('Subagent thinking mode') select")).toHaveValue("on_error")
  await expect(page.locator("label.settings-field:has-text('Subagent allowed tools (comma separated)') input")).toHaveValue("fs.read")

  await page.locator("label.settings-field:has-text('Subagent timeout ms') input").fill("60000")
  await expect(page.locator(".settings-diff-table code", { hasText: "agents.subagent_defaults.timeout_ms" })).toBeVisible()

  await page.locator("label.settings-field:has-text('Subagent thinking mode') select").selectOption("always")
  await expect(page.locator(".settings-diff-table code", { hasText: "agents.subagent_defaults.thinking_mode" })).toBeVisible()

  await page.locator("label.settings-field:has-text('Subagent allowed tools (comma separated)') input").fill("fs.read, code.search")
  await expect(page.locator(".settings-diff-table code", { hasText: "agents.subagent_defaults.allowed_tools" })).toBeVisible()
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

test("unsaved changes dialog appears on route navigation and supports stay, save, and discard flows", async ({ page }) => {
  const state = createState()
  await routeSettingsApi(page, state)

  await page.goto("/dashboard#/settings")
  await page.locator("label.settings-field:has-text('Server port') input").fill("9191")

  const navigationDialog = page.getByRole("dialog", { name: "Unsaved settings changes" })

  await page.getByRole("link", { name: "Chat" }).click()
  await expect(navigationDialog).toBeVisible()
  await expect(page).toHaveURL(/#\/settings/)

  await navigationDialog.getByRole("button", { name: "Stay on Settings" }).click()
  await expect(navigationDialog).toHaveCount(0)
  await expect(page).toHaveURL(/#\/settings/)
  expect(state.patchBodies.length).toBe(0)

  await page.getByRole("link", { name: "Chat" }).click()
  await expect(navigationDialog).toBeVisible()
  await navigationDialog.getByRole("button", { name: "Save and Continue" }).click()
  await expect.poll(() => state.patchBodies.length).toBe(1)
  await expect(page).toHaveURL(/#\/chat/)

  await page.goto("/dashboard#/settings")
  await page.locator("label.settings-field:has-text('Server port') input").fill("9292")

  await page.getByRole("link", { name: "Chat" }).click()
  await expect(navigationDialog).toBeVisible()
  await navigationDialog.getByRole("button", { name: "Discard changes" }).click()
  await expect.poll(() => state.patchBodies.length).toBe(1)
  await expect(page).toHaveURL(/#\/chat/)
})
