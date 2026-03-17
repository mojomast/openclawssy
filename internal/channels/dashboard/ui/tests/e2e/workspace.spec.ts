import { expect, test } from "@playwright/test"

type WorkspaceEntry = {
  name: string
  path: string
  kind: "dir" | "file"
  size_bytes?: number
  modified_at?: string
  mime_type?: string
}

const entriesByPath: Record<string, { workspace_root: string; path: string; parent_path: string; entries: WorkspaceEntry[] }> = {
  ".": {
    workspace_root: "/workspace",
    path: ".",
    parent_path: "",
    entries: [
      {
        name: "docs",
        path: "docs",
        kind: "dir",
        modified_at: "2026-03-14T01:00:00Z",
      },
      {
        name: "README.md",
        path: "README.md",
        kind: "file",
        size_bytes: 2048,
        modified_at: "2026-03-14T01:01:00Z",
        mime_type: "text/markdown",
      },
      {
        name: "data.json",
        path: "data.json",
        kind: "file",
        size_bytes: 256,
        modified_at: "2026-03-14T01:02:00Z",
        mime_type: "application/json",
      },
    ],
  },
  docs: {
    workspace_root: "/workspace",
    path: "docs",
    parent_path: "",
    entries: [
      {
        name: "guide.txt",
        path: "docs/guide.txt",
        kind: "file",
        size_bytes: 20,
        modified_at: "2026-03-14T01:03:00Z",
        mime_type: "text/plain",
      },
    ],
  },
}

const filesByPath: Record<string, Record<string, unknown>> = {
  "README.md": {
    workspace_root: "/workspace",
    path: "README.md",
    name: "README.md",
    size_bytes: 2048,
    modified_at: "2026-03-14T01:01:00Z",
    mime_type: "text/markdown",
    is_text: true,
    truncated: false,
    preview_notice: "",
    content: "Welcome to workspace\nRead this first.",
  },
  "docs/guide.txt": {
    workspace_root: "/workspace",
    path: "docs/guide.txt",
    name: "guide.txt",
    size_bytes: 20,
    modified_at: "2026-03-14T01:03:00Z",
    mime_type: "text/plain",
    is_text: true,
    truncated: false,
    preview_notice: "",
    content: "Docs guide content",
  },
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("openclawssy.dashboard.bearer", "e2e-token")
  })
})

test("shows directory listing, filter, breadcrumbs, and file preview", async ({ page }) => {
  await page.route("**/api/admin/workspace/entries?**", async (route) => {
    const requestURL = new URL(route.request().url())
    const path = (requestURL.searchParams.get("path") || ".").trim() || "."
    const payload = entriesByPath[path]

    if (!payload) {
      await route.fulfill({ status: 404, body: "not found" })
      return
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ ...payload, workspace_mode: "docker" }),
    })
  })

  await page.route("**/api/admin/workspace/file?**", async (route) => {
    const requestURL = new URL(route.request().url())
    const path = (requestURL.searchParams.get("path") || "").trim()
    const payload = filesByPath[path]

    if (!payload) {
      await route.fulfill({ status: 404, body: "not found" })
      return
    }

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(payload),
    })
  })

  await page.goto("/dashboard#/workspace")

  await expect(page.getByRole("heading", { name: "Workspace", level: 2 })).toBeVisible()
  await expect(page.getByTestId("workspace-mode-summary")).toContainText("via docker workspace mode")
  await expect(page.getByText("Entries (3)")).toBeVisible()
  await expect(page.getByRole("button", { name: "workspace" })).toBeVisible()

  await page.getByRole("searchbox", { name: "Filter current folder" }).fill("read")
  await expect(page.getByRole("button", { name: /FILE README.md/ })).toBeVisible()
  await expect(page.getByRole("button", { name: /DIR docs/ })).toHaveCount(0)

  await page.getByRole("searchbox", { name: "Filter current folder" }).fill("")
  await page.getByRole("button", { name: /FILE README.md/ }).click()

  await expect(page.getByText("Path README.md")).toBeVisible()
  await expect(page.getByText("MIME text/markdown")).toBeVisible()
  await expect(page.getByText("Welcome to workspace")).toBeVisible()
})

test("clicking directories, breadcrumb segments, and Up navigates correctly", async ({ page }) => {
  await page.route("**/api/admin/workspace/entries?**", async (route) => {
    const requestURL = new URL(route.request().url())
    const path = (requestURL.searchParams.get("path") || ".").trim() || "."
    const payload = entriesByPath[path]
    if (!payload) {
      await route.fulfill({ status: 404, body: "not found" })
      return
    }
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ ...payload, workspace_mode: "docker" }) })
  })

  await page.route("**/api/admin/workspace/file?**", async (route) => {
    await route.fulfill({ status: 404, body: "not found" })
  })

  await page.goto("/dashboard#/workspace")

  await page.getByRole("button", { name: /DIR docs/ }).click()
  await expect(page.getByRole("button", { name: "docs" })).toBeVisible()
  await expect(page.getByText("Entries (1)")).toBeVisible()
  await expect(page.getByRole("button", { name: /FILE guide.txt/ })).toBeVisible()

  await page.getByRole("button", { name: "workspace" }).click()
  await expect(page.getByText("Entries (3)")).toBeVisible()
  await expect(page.getByRole("button", { name: /DIR docs/ })).toBeVisible()

  await page.getByRole("button", { name: /DIR docs/ }).click()
  await page.getByRole("button", { name: "Up" }).click()
  await expect(page.getByText("Entries (3)")).toBeVisible()
})

test("Refresh button reloads and auto-refresh polls every 4 seconds", async ({ page }) => {
  let entriesRequestCount = 0

  await page.route("**/api/admin/workspace/entries?**", async (route) => {
    entriesRequestCount += 1
    const requestURL = new URL(route.request().url())
    const path = (requestURL.searchParams.get("path") || ".").trim() || "."
    const payload = entriesByPath[path]
    if (!payload) {
      await route.fulfill({ status: 404, body: "not found" })
      return
    }
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ ...payload, workspace_mode: "docker" }) })
  })

  await page.route("**/api/admin/workspace/file?**", async (route) => {
    await route.fulfill({ status: 404, body: "not found" })
  })

  await page.goto("/dashboard#/workspace")
  await expect(page.getByText("Entries (3)")).toBeVisible()

  const baselineCount = entriesRequestCount
  await page.getByRole("button", { name: "Refresh" }).click()
  await expect.poll(() => entriesRequestCount).toBeGreaterThan(baselineCount)

  await page.getByRole("checkbox", { name: "Auto refresh" }).check()
  const autoRefreshBaseline = entriesRequestCount
  await page.waitForTimeout(4300)
  await expect.poll(() => entriesRequestCount).toBeGreaterThan(autoRefreshBaseline)

  await page.getByRole("checkbox", { name: "Auto refresh" }).uncheck()
  const stoppedCount = entriesRequestCount
  await page.waitForTimeout(4300)
  expect(entriesRequestCount).toBe(stoppedCount)
})
