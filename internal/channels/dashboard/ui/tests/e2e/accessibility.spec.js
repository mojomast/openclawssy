import { expect, test } from "@playwright/test";

function json(route, body, status = 200) {
  return route.fulfill({
    status,
    contentType: "application/json; charset=utf-8",
    body: JSON.stringify(body),
  });
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("openclawssy.dashboard.bearer", "e2e-token");
  });

  await page.route("**/*", async (route) => {
    const reqUrl = new URL(route.request().url());
    const path = reqUrl.pathname;

    if (path === "/api/admin/status") {
      return json(route, {
        model: { provider: "openai", name: "gpt-4.1-mini" },
        run_count: 42,
      });
    }

    if (path.startsWith("/api/admin/agents")) {
      return json(route, {
        agents: ["default", "research"],
        active_agent: "default",
        selected_agent: "default",
        profile_context: { exists: false, enabled: true, self_improvement: false },
        agents_config: {
          allow_agent_model_overrides: true,
          allow_inter_agent_messaging: true,
          self_improvement_enabled: false,
        },
      });
    }

    return route.continue();
  });
});

test("Sidebar resizers support keyboard toggle (Enter)", async ({ page }) => {
  await page.goto("/dashboard");

  // Verify initial state: Nav pane is visible
  const nav = page.locator(".nav-pane");
  await expect(nav).toBeVisible();

  // Focus on the left resizer
  const leftResizer = page.getByLabel("Resize navigation pane");
  await leftResizer.focus();

  // Press Enter to collapse
  await page.keyboard.press("Enter");

  // Verify collapsed state
  // The .shell element should have .nav-collapsed class
  const shell = page.locator(".shell");
  await expect(shell).toHaveClass(/nav-collapsed/);

  // Press Enter again to expand
  await page.keyboard.press("Enter");

  // Verify expanded state
  await expect(shell).not.toHaveClass(/nav-collapsed/);
  await expect(nav).toBeVisible();
});
