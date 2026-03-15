import { expect, test, type Page } from "@playwright/test"

declare global {
  interface Window {
    __copiedSecretName?: string
    __confirmResult?: boolean
  }
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("openclawssy.dashboard.bearer", "e2e-token")
    window.__copiedSecretName = ""
    window.__confirmResult = true

    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async (value: string) => {
          window.__copiedSecretName = value
        },
      },
    })

    window.confirm = () => Boolean(window.__confirmResult)
  })
})

function keysPanel(page: Page) {
  return page.locator("article").filter({ has: page.getByRole("heading", { name: "Stored keys" }) }).first()
}

test("loads stored keys, filters by search, and supports Use key", async ({ page }) => {
  const state = {
    keys: ["discord/bot_token", "OPENAI_API_KEY", "ZAI_API_KEY"],
  }

  await page.route("**/api/admin/secrets**", async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    const method = route.request().method()

    if (path === "/api/admin/secrets" && method === "GET") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ keys: state.keys }),
      })
      return
    }

    await route.continue()
  })

  await page.goto("/dashboard#/secrets")
  const panel = keysPanel(page)

  await expect(page.getByRole("heading", { name: "Secrets", level: 2 })).toBeVisible()
  await expect(page.getByText("3 total")).toBeVisible()
  await expect(panel.locator("code", { hasText: "OPENAI_API_KEY" })).toBeVisible()

  await page.getByRole("searchbox", { name: "Search key names" }).fill("discord")
  await expect(panel.locator("code", { hasText: "discord/bot_token" })).toBeVisible()
  await expect(panel.locator("code", { hasText: "OPENAI_API_KEY" })).toHaveCount(0)

  await page.getByRole("button", { name: "Use key", exact: true }).first().click()
  await expect(page.getByLabel("Secret key name")).toHaveValue("PERPLEXITY_API_KEY")
})

test("copies key names and shows feedback", async ({ page }) => {
  await page.route("**/api/admin/secrets**", async (route) => {
    const url = new URL(route.request().url())
    if (url.pathname === "/api/admin/secrets" && route.request().method() === "GET") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ keys: ["OPENAI_API_KEY"] }),
      })
      return
    }
    await route.continue()
  })

  await page.goto("/dashboard#/secrets")
  const panel = keysPanel(page)
  await panel.getByRole("button", { name: "Copy name" }).click()

  await expect(page.getByText("Copied: OPENAI_API_KEY")).toBeVisible()

  const copied = await page.evaluate(() => window.__copiedSecretName || "")
  expect(copied).toBe("OPENAI_API_KEY")
})

test("stores secrets and shows validation and API error feedback", async ({ page }) => {
  const state = {
    keys: ["OPENAI_API_KEY"],
    failNextPost: true,
  }

  await page.route("**/api/admin/secrets**", async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    const method = route.request().method()

    if (path === "/api/admin/secrets" && method === "GET") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ keys: state.keys }),
      })
      return
    }

    if (path === "/api/admin/secrets" && method === "POST") {
      if (state.failNextPost) {
        state.failNextPost = false
        await route.fulfill({ status: 400, body: "value rejected" })
        return
      }

      const payload = JSON.parse(route.request().postData() || "{}")
      const key = String(payload.name || "").trim()
      if (key) {
        state.keys = Array.from(new Set([...state.keys, key]))
      }

      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ ok: true, stored: key }),
      })
      return
    }

    await route.continue()
  })

  await page.goto("/dashboard#/secrets")
  const panel = keysPanel(page)

  await page.getByRole("button", { name: "Store Secret" }).click()
  await expect(page.getByText("Name and value are required.")).toBeVisible()

  await page.getByLabel("Secret key name").fill("HATZ_API_KEY")
  await page.getByLabel("Secret value").fill("first-attempt")
  await page.getByRole("button", { name: "Store Secret" }).click()
  await expect(page.getByText("value rejected")).toBeVisible()

  await page.getByLabel("Secret value").fill("second-attempt")
  await page.getByRole("button", { name: "Store Secret" }).click()
  await expect(page.getByText("Stored key: HATZ_API_KEY")).toBeVisible()
  await expect(panel.locator("code", { hasText: "HATZ_API_KEY" })).toBeVisible()
  await expect(page.getByLabel("Secret value")).toHaveValue("")
})

test("deletes keys with confirmation and shows delete errors", async ({ page }) => {
  const state = {
    keys: ["discord/bot_token", "OPENAI_API_KEY"],
    failNextDelete: true,
  }

  await page.route("**/api/admin/secrets**", async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    const method = route.request().method()

    if (path === "/api/admin/secrets" && method === "GET") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ keys: state.keys }),
      })
      return
    }

    if (path.startsWith("/api/admin/secrets/") && method === "DELETE") {
      const key = decodeURIComponent(path.slice("/api/admin/secrets/".length))

      if (state.failNextDelete) {
        state.failNextDelete = false
        await route.fulfill({ status: 500, body: "delete failed" })
        return
      }

      state.keys = state.keys.filter((entry) => entry !== key)
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ ok: true, deleted: key }),
      })
      return
    }

    await route.continue()
  })

  await page.goto("/dashboard#/secrets")
  const panel = keysPanel(page)

  const discordRow = panel.locator("li", { hasText: "discord/bot_token" })
  await discordRow.getByRole("button", { name: "Delete key" }).click()
  await expect(page.getByText("delete failed")).toBeVisible()

  await page.evaluate(() => {
    window.__confirmResult = false
  })
  await discordRow.getByRole("button", { name: "Delete key" }).click()
  await expect(panel.locator("code", { hasText: "discord/bot_token" })).toBeVisible()

  await page.evaluate(() => {
    window.__confirmResult = true
  })
  await discordRow.getByRole("button", { name: "Delete key" }).click()

  await expect(page.getByText("Deleted key: discord/bot_token")).toBeVisible()
  await expect(panel.locator("code", { hasText: "discord/bot_token" })).toHaveCount(0)
})

test("shows loading error and retries list fetch", async ({ page }) => {
  const state = {
    failFirstGet: true,
  }

  await page.route("**/api/admin/secrets**", async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    const method = route.request().method()

    if (path === "/api/admin/secrets" && method === "GET") {
      if (state.failFirstGet) {
        state.failFirstGet = false
        await route.fulfill({ status: 500, body: "list failed" })
        return
      }

      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ keys: ["OPENAI_API_KEY"] }),
      })
      return
    }

    await route.continue()
  })

  await page.goto("/dashboard#/secrets")
  const panel = keysPanel(page)

  await expect(page.getByText("Failed to load keys: list failed")).toBeVisible()
  await page.getByRole("button", { name: "Retry" }).click()
  await expect(panel.locator("code", { hasText: "OPENAI_API_KEY" })).toBeVisible()
})
