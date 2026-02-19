import { expect, test } from "@playwright/test";

function json(route, body, status = 200) {
  return route.fulfill({
    status,
    contentType: "application/json; charset=utf-8",
    body: JSON.stringify(body),
  });
}

// ─── Shared mock sandbox state ─────────────────────────────────────────────

function makeSandboxState() {
  return {
    provider: "docker",
    running: true,
    container_name: "openclawssy_agent_default",
    container_id: "abc123def456789",
    image: "ubuntu:24.04",
    workspace_path: "/workspace",
    volume_name: "openclawssy_ws_default",
    network_mode: "none",
    agent_id: "default",
    images: [
      { id: "sha256:abc123def456", repo: "ubuntu", tag: "24.04", size_mb: 78 },
      { id: "sha256:def789ghi012", repo: "python", tag: "3.12-slim", size_mb: 145 },
    ],
    volumes: [
      { name: "openclawssy_ws_default", driver: "local", mountpoint: "/var/lib/docker/volumes/openclawssy_ws_default/_data" },
    ],
    pullLog: [],
    deletedVolumes: [],
  };
}

// ─── beforeEach setup ──────────────────────────────────────────────────────

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("openclawssy.dashboard.bearer", "e2e-token");
  });

  const sb = makeSandboxState();

  await page.route("**/*", async (route) => {
    const reqUrl = new URL(route.request().url());
    const path = reqUrl.pathname;
    const method = route.request().method();

    // ── Base admin status (needed by layout) ──────────────────────────────
    if (path === "/api/admin/status") {
      return json(route, { model: { provider: "openai", name: "gpt-4.1-mini" }, run_count: 5 });
    }

    // ── Sandbox endpoints ─────────────────────────────────────────────────
    if (path === "/api/admin/sandbox/docker/status" && method === "GET") {
      return json(route, {
        agent_id: sb.agent_id,
        container_name: sb.container_name,
        container_id: sb.container_id,
        image: sb.image,
        running: sb.running,
        workspace_path: sb.workspace_path,
        volume_name: sb.volume_name,
        network_mode: sb.network_mode,
        provider: sb.provider,
      });
    }

    if (path === "/api/admin/sandbox/docker/create" && method === "POST") {
      sb.running = true;
      sb.container_id = "newcontainer1234";
      return json(route, { ok: true, agent_id: sb.agent_id });
    }

    if (path === "/api/admin/sandbox/docker/stop" && method === "POST") {
      sb.running = false;
      return json(route, { ok: true, agent_id: sb.agent_id });
    }

    if (path === "/api/admin/sandbox/docker/reset" && method === "POST") {
      sb.running = true;
      sb.container_id = "resetcontainer5678";
      return json(route, { ok: true, agent_id: sb.agent_id });
    }

    if (path === "/api/admin/sandbox/docker/pull" && method === "POST") {
      const body = JSON.parse(route.request().postData() || "{}");
      const image = String(body.image || "").trim();
      sb.pullLog.push(image);
      if (image) {
        const [repo, tag = "latest"] = image.split(":");
        sb.images.push({ id: `sha256:pulled${Date.now()}`, repo, tag, size_mb: 99 });
      }
      return json(route, { ok: true, image });
    }

    if (path === "/api/admin/sandbox/docker/images" && method === "GET") {
      return json(route, { images: sb.images });
    }

    if (path === "/api/admin/sandbox/docker/volumes" && method === "GET") {
      return json(route, { volumes: sb.volumes });
    }

    if (path === "/api/admin/sandbox/docker/volume" && method === "DELETE") {
      const body = JSON.parse(route.request().postData() || "{}");
      const name = String(body.name || "").trim();
      sb.deletedVolumes.push(name);
      sb.volumes = sb.volumes.filter((v) => v.name !== name);
      return json(route, { ok: true });
    }

    return route.continue();
  });
});

// ─── Helpers ───────────────────────────────────────────────────────────────

/**
 * Navigate to the sandbox page and wait for it to fully render.
 * This guards against the app-boot race where the initial "/chat" setState
 * fires a render concurrently with the hash-router's "/sandbox" setState.
 * Waiting for the "Sandbox Manager" heading ensures the sandbox page has
 * won the race before we proceed with assertions.
 */
async function gotoSandbox(page) {
  await page.goto("/dashboard#/sandbox");
  await expect(page.getByRole("heading", { name: "Sandbox Manager" })).toBeVisible();
}

// ─── Tests ─────────────────────────────────────────────────────────────────

test("Sandbox 1: page loads and shows Sandbox Manager heading", async ({ page }) => {
  await gotoSandbox(page);
  // Nav link and heading are both visible
  await expect(page.getByRole("link", { name: "Sandbox" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Sandbox Manager" })).toBeVisible();
});

test("Sandbox 2: nav link is present", async ({ page }) => {
  await page.goto("/dashboard#/chat");
  await expect(page.getByRole("link", { name: "Sandbox" })).toBeVisible();
});

test("Sandbox 3: status card shows container info for docker provider", async ({ page }) => {
  await gotoSandbox(page);
  // Wait for status to load
  await expect(page.getByTestId("status-card")).toBeVisible();
  // Container name appears in the status table
  await expect(page.getByRole("cell", { name: "openclawssy_agent_default" })).toBeVisible();
  // Image name appears - use the status table cell specifically
  await expect(page.getByRole("cell", { name: /ubuntu:24\.04/ }).first()).toBeVisible();
  // Running badge
  await expect(page.getByTestId("running-badge")).toBeVisible();
  await expect(page.getByTestId("running-badge")).toHaveText("running");
});

test("Sandbox 4: provider badge shows docker", async ({ page }) => {
  await gotoSandbox(page);
  await expect(page.getByTestId("provider-badge")).toBeVisible();
  await expect(page.getByTestId("provider-badge")).toHaveText("docker");
});

test("Sandbox 5: workspace banner shows active when running", async ({ page }) => {
  await gotoSandbox(page);
  await expect(page.getByTestId("workspace-banner-active")).toBeVisible();
  await expect(page.getByTestId("workspace-banner-active")).toContainText("/workspace");
});

test("Sandbox 6: refresh button reloads status", async ({ page }) => {
  await gotoSandbox(page);
  // Wait for initial load
  await expect(page.getByTestId("status-card")).toBeVisible();
  await expect(page.getByText("openclawssy_agent_default")).toBeVisible();

  // Click refresh
  await page.getByTestId("refresh-btn").click();
  // Status card should still show container info after refresh
  await expect(page.getByText("openclawssy_agent_default")).toBeVisible();
});

test("Sandbox 7: Create button shows success message", async ({ page }) => {
  await gotoSandbox(page);
  await expect(page.getByTestId("status-card")).toBeVisible();

  await page.getByTestId("create-btn").click();
  await expect(page.getByTestId("action-success")).toBeVisible();
  await expect(page.getByTestId("action-success")).toContainText("succeeded");
});

test("Sandbox 8: Stop button shows success message", async ({ page }) => {
  await gotoSandbox(page);
  await expect(page.getByTestId("status-card")).toBeVisible();

  await page.getByTestId("stop-btn").click();
  await expect(page.getByTestId("action-success")).toBeVisible();
  await expect(page.getByTestId("action-success")).toContainText("succeeded");
});

test("Sandbox 9: Reset button prompts confirm dialog and shows success", async ({ page }) => {
  await gotoSandbox(page);
  await expect(page.getByTestId("status-card")).toBeVisible();

  // Accept the confirm dialog
  page.once("dialog", (dialog) => {
    expect(dialog.message()).toContain("destroy all files");
    dialog.accept();
  });

  await page.getByTestId("reset-btn").click();
  await expect(page.getByTestId("action-success")).toBeVisible();
  await expect(page.getByTestId("action-success")).toContainText("succeeded");
});

test("Sandbox 10: Reset button cancel does nothing", async ({ page }) => {
  await gotoSandbox(page);
  await expect(page.getByTestId("status-card")).toBeVisible();

  // Dismiss the confirm dialog
  page.once("dialog", (dialog) => {
    dialog.dismiss();
  });

  await page.getByTestId("reset-btn").click();
  // No success message should appear
  await expect(page.getByTestId("action-success")).not.toBeVisible();
});

test("Sandbox 11: Pull image shows success", async ({ page }) => {
  await gotoSandbox(page);
  await page.getByTestId("pull-image-input").fill("alpine:3.19");
  await page.getByTestId("pull-btn").click();
  await expect(page.getByTestId("pull-success")).toBeVisible();
  await expect(page.getByTestId("pull-success")).toContainText("alpine:3.19");
});

test("Sandbox 12: Images list table renders rows", async ({ page }) => {
  await gotoSandbox(page);
  // Wait for images to load
  await expect(page.getByTestId("image-row").first()).toBeVisible();
  // ubuntu:24.04 should be visible in the images table (use exact cell match)
  await expect(page.getByTestId("image-row").filter({ hasText: "ubuntu:24.04" })).toBeVisible();
});

test("Sandbox 13: Volumes list table renders", async ({ page }) => {
  await gotoSandbox(page);
  await expect(page.getByTestId("volume-row").first()).toBeVisible();
  // The volume row should contain the volume name
  await expect(page.getByTestId("volume-row").filter({ hasText: "openclawssy_ws_default" })).toBeVisible();
});

test("Sandbox 14: Delete volume shows confirm dialog and removes row", async ({ page }) => {
  await gotoSandbox(page);
  // Wait for volumes to load
  await expect(page.getByTestId("volume-row").first()).toBeVisible();

  // Accept the confirm dialog
  page.once("dialog", (dialog) => {
    expect(dialog.message()).toContain("Delete volume");
    dialog.accept();
  });

  await page.getByTestId("delete-volume-btn").first().click();
  await expect(page.getByTestId("action-success")).toBeVisible();
  await expect(page.getByTestId("action-success")).toContainText("Deleted volume");
});

test("Sandbox 15: non-docker provider shows switch message", async ({ page }) => {
  // Register this route AFTER beforeEach sets up the general handler.
  // Playwright routes are LIFO, so this more-specific handler fires first.
  await page.route("**/api/admin/sandbox/docker/status**", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json; charset=utf-8",
      body: JSON.stringify({
        agent_id: "default",
        provider: "local",
        running: false,
        container_name: "",
        container_id: "",
        image: "",
        workspace_path: "/workspace",
        volume_name: "",
        network_mode: "",
      }),
    });
  });

  await gotoSandbox(page);
  // Wait for status to be loaded - non-docker notice should appear
  await expect(page.getByTestId("status-card")).toBeVisible();
  await expect(page.getByTestId("non-docker-notice")).toBeVisible();
  await expect(page.getByTestId("non-docker-notice")).toContainText("docker provider");
});
