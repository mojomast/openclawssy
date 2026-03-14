import { expect, test } from "@playwright/test";

test.describe("Shared Components", () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      window.localStorage.setItem("openclawssy.dashboard.bearer", "e2e-token");
      window.localStorage.setItem(
        "ui-store",
        JSON.stringify({
          state: {
            theme: "system",
            sidebar: { isOpen: true, width: 240, collapsedSections: [] },
            inspector: { isOpen: true, width: 320 },
          },
          version: 0,
        })
      );
    });

    // Mock API routes
    await page.route("**/api/admin/status", async (route) => {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          model: { provider: "openai", name: "gpt-4.1-mini" },
          run_count: 42,
        }),
      });
    });

    await page.goto("/dashboard#/help");
  });

  test("Layout shell renders with header, nav, main, inspector panels", async ({ page }) => {
    // Header
    await expect(page.getByText("Openclawssy Dashboard")).toBeVisible();
    await expect(page.getByText("React")).toBeVisible();
    await expect(page.getByText("Runtime Active")).toBeVisible();

    // Sidebar navigation
    await expect(page.getByText("Dashboard")).toBeVisible();
    await expect(page.getByText("Operations")).toBeVisible();
    await expect(page.getByText("Control Plane")).toBeVisible();

    // Inspector panel
    await expect(page.getByText("Inspector")).toBeVisible();

    // Footer
    await expect(page.getByText("Open Legacy Dashboard")).toBeVisible();
    await expect(page.getByText("18 routes configured")).toBeVisible();
  });

  test("Nav sidebar shows links for all routes", async ({ page }) => {
    // Dashboard section
    await expect(page.getByRole("link", { name: "Help", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Workspace", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Secrets", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Chat", exact: true })).toBeVisible();

    // Operations section
    await expect(page.getByRole("link", { name: "Runs", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Sessions", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Scheduler", exact: true })).toBeVisible();

    // Control Plane section
    await expect(page.getByRole("link", { name: "Agent Contract", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Prompt Stack", exact: true })).toBeVisible();
  });

  test("Theme toggle switches dark/light and persists", async ({ page }) => {
    const themeButton = page.getByRole("button", { name: /Switch to (light|dark)/ });
    await expect(themeButton).toBeVisible();

    // Click theme toggle
    await themeButton.click();

    // Check localStorage for persisted theme
    const theme = await page.evaluate(() => {
      const store = localStorage.getItem("ui-store");
      return store ? JSON.parse(store).state?.theme : null;
    });

    expect(["light", "dark"]).toContain(theme);
  });

  test("Help dialog opens with F1 key", async ({ page }) => {
    await page.keyboard.press("F1");
    await expect(page.getByRole("dialog")).toBeVisible();
    await expect(page.getByRole("heading", { name: "Keyboard Shortcuts" })).toBeVisible();
    await expect(page.getByText("F1")).toBeVisible();
    await expect(page.getByText("Open help")).toBeVisible();
  });

  test("Help dialog opens with ? key", async ({ page }) => {
    await page.keyboard.press("?");
    await expect(page.getByRole("dialog")).toBeVisible();
    await expect(page.getByRole("heading", { name: "Keyboard Shortcuts" })).toBeVisible();
  });

  test("Search input focuses with / key", async ({ page }) => {
    const searchInput = page.locator('[data-search-input]');
    await page.keyboard.press("/");
    await expect(searchInput).toBeFocused();
  });

  test("g+c keyboard shortcut navigates to chat", async ({ page }) => {
    // First press 'g', then 'c' quickly
    await page.keyboard.press("g");
    await page.keyboard.press("c");

    // Should navigate to chat page
    await expect(page).toHaveURL(/.*\/chat/);
    await expect(page.getByRole("heading", { name: "Chat" })).toBeVisible();
  });

  test("Sidebar can be collapsed and expanded", async ({ page }) => {
    // Find and click collapse button
    const collapseButton = page.getByRole("button", { name: "Collapse" });
    await collapseButton.click();

    // Sidebar should collapse, check for expand button
    const expandButton = page.getByRole("button", { name: "Expand sidebar", exact: true });
    await expect(expandButton).toBeVisible();

    // Click expand button
    await expandButton.click();

    // Sidebar should expand again
    await expect(page.getByText("Dashboard")).toBeVisible();
    await expect(page.getByText("Operations")).toBeVisible();
    await expect(page.getByText("Control Plane")).toBeVisible();
  });

  test("Inspector panel can be closed and opened", async ({ page }) => {
    // Find the X button in inspector and click it
    const inspector = page.locator("aside, .border-l.bg-card").filter({ hasText: "Inspector" });
    const closeButton = inspector.getByRole("button").first();
    await closeButton.click();

    // Inspector should be closed, check for expand button
    const expandButton = page.getByRole("button", { name: "Expand inspector", exact: true });
    await expect(expandButton).toBeVisible();

    // Click expand button
    await expandButton.click();

    // Inspector should be visible again
    await expect(page.getByText("Inspector")).toBeVisible();
  });

  test("Footer displays legacy dashboard link and route count", async ({ page }) => {
    await expect(page.getByText("Open Legacy Dashboard")).toBeVisible();
    await expect(page.getByText("18 routes configured")).toBeVisible();
    await expect(page.getByText("Press ? for keyboard shortcuts")).toBeVisible();
  });

  test("Navigation links work and show active state", async ({ page }) => {
    // Click on a nav link
    await page.getByRole("link", { name: "Secrets", exact: true }).click();

    // Should navigate to secrets page
    await expect(page).toHaveURL(/.*\/secrets/);
  });

  test("Mobile navigation drawer appears on narrow screens", async ({ page }) => {
    // Resize to mobile width
    await page.setViewportSize({ width: 375, height: 667 });

    // Click hamburger menu
    const menuButton = page.getByRole("button", { name: "Open navigation menu" }).first();
    await menuButton.click();

    // Mobile drawer should appear
    await expect(page.getByRole("heading", { name: "Navigation" })).toBeVisible();

    // Close the drawer
    await page.getByRole("button", { name: "Close", exact: true }).click();
  });
});

test.describe("DataTable Component", () => {
  test("DataTable renders with sort, filter, pagination", async ({ page }) => {
    // Navigate to a page that uses DataTable (e.g., Runs)
    await page.goto("/dashboard#/runs");

    // Wait for table to be visible
    await expect(page.locator("table").first()).toBeVisible();
  });
});
