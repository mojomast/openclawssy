import { expect, test } from "@playwright/test"

declare global {
  interface Window {
    __copiedHelpLink?: string
  }
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("openclawssy.dashboard.bearer", "e2e-token")
    window.__copiedHelpLink = ""
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async (value: string) => {
          window.__copiedHelpLink = value
        },
      },
    })
  })

  await page.route("**/api/admin/status", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        model: { provider: "openai", name: "gpt-4.1-mini" },
        run_count: 42,
      }),
    })
  })
})

test("renders grouped topics and topic content with toc + related chips", async ({ page }) => {
  await page.goto("/dashboard#/help")

  const sidebar = page.locator("aside").first()

  await expect(page.getByRole("heading", { name: "Help Center" })).toBeVisible()
  await expect(page.getByRole("heading", { name: "🚀 Getting Started" })).toBeVisible()
  await expect(sidebar.getByRole("button", { name: "Getting Started" })).toBeVisible()

  await expect(page.getByText("Getting Started / Getting Started")).toBeVisible()
  await expect(page.getByRole("heading", { name: "On this page" })).toBeVisible()
  await expect(page.getByRole("button", { name: "Quick tour of the tabs" })).toBeVisible()

  await expect(page.getByRole("heading", { name: "Related topics" })).toBeVisible()
  await page.locator("article").getByRole("button", { name: "Runs & Debugging" }).click()
  await expect(page.locator("article").getByRole("heading", { name: "Runs & Debugging", level: 3 })).toBeVisible()
  await expect(page.getByText("Debugging / Runs & Debugging")).toBeVisible()
})

test("search filters topics and highlights matching text", async ({ page }) => {
  await page.goto("/dashboard#/help")

  const sidebar = page.locator("aside").first()

  const searchInput = page.getByRole("searchbox", { name: "Search help topics" })
  await searchInput.fill("discord")

  await expect(sidebar.getByRole("button", { name: "Discord Bot Setup" })).toBeVisible()

  const highlightedMark = sidebar.locator("button:has-text('Discord') mark")
  await expect(highlightedMark).toContainText(/discord/i)

  await searchInput.fill("runs debugging")
  await expect(sidebar.getByRole("button", { name: "Runs & Debugging" })).toBeVisible()
})

test("deep link topic query selects topic on load", async ({ page }) => {
  await page.goto("/dashboard#/help?topic=faq")

  await expect(page.locator("article").getByRole("heading", { name: "FAQ", level: 3 })).toBeVisible()
  await expect(page.getByText("FAQ / FAQ")).toBeVisible()

  await page.goto("/dashboard#/help?topic=secrets")
  await expect(page.locator("article").getByRole("heading", { name: "Secrets", exact: true }).first()).toBeVisible()
  await expect(page.getByText("Secrets / Secrets")).toBeVisible()
})

test("copy link copies topic deep link and shows toast", async ({ page }) => {
  await page.goto("/dashboard#/help?topic=scheduler-guide")

  await expect(page.locator("article").getByRole("heading", { name: "Scheduler", level: 3 })).toBeVisible()
  await page.getByRole("button", { name: "Copy link to topic" }).click()

  await expect(page.getByText("Topic link copied.")).toBeVisible()

  const copied = await page.evaluate(() => window.__copiedHelpLink || "")
  expect(copied).toContain("#/help?topic=scheduler-guide")
})

test("copy link shows deterministic fallback when clipboard permission is unavailable", async ({ page }) => {
  await page.goto("/dashboard#/help?topic=scheduler")

  await page.evaluate(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async () => {
          throw new Error("NotAllowedError")
        },
      },
    })
    Object.defineProperty(document, "execCommand", {
      configurable: true,
      value: () => false,
    })
  })

  await expect(page.locator("article").getByRole("heading", { name: "Scheduler", exact: true }).first()).toBeVisible()
  await page.getByRole("button", { name: "Copy link to topic" }).click()
  await expect(page.getByText(/Clipboard unavailable\./)).toBeVisible()
})
