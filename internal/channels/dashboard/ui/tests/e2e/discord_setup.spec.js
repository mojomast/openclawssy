import { expect, test } from "@playwright/test";

function json(route, body, status = 200) {
  return route.fulfill({
    status,
    contentType: "application/json; charset=utf-8",
    body: JSON.stringify(body),
  });
}

test("Discord setup shows token presence and updates after save", async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("openclawssy.dashboard.bearer", "e2e-token");
  });

  const state = {
    config: {
      server: { bind_address: "127.0.0.1", port: 8080 },
      workspace: { root: "/workspace" },
      engine: { max_concurrent_runs: 5 },
      output: { thinking_mode: "never", max_thinking_chars: 4096 },
      model: { provider: "openai", name: "gpt-4.1-mini", max_tokens: 1024 },
      chat: { enabled: true, default_agent_id: "default", allow_users: ["dashboard_user"], allow_rooms: [], rate_limit_per_min: 20, global_rate_limit_per_min: 120 },
      discord: { enabled: false, token_env: "DISCORD_BOT_TOKEN", default_agent_id: "default", command_prefix: "!ask", rate_limit_per_min: 20, allow_guilds: [], allow_channels: [], allow_users: [] },
      telegram: { enabled: false, token_env: "TELEGRAM_BOT_TOKEN", default_agent_id: "default", command_prefix: "", rate_limit_per_min: 20, allow_users: [], allow_chats: [] },
      sandbox: { active: false, provider: "none" },
      shell: { enable_exec: false, allowed_commands: [], default_timeout_ms: 120000, max_timeout_ms: 300000 },
      network: { allowed_domains: [] },
      scheduler: { max_concurrent_jobs: 10 },
      providers: { openai: {}, openrouter: {}, requesty: {}, hatz: {}, zai: {}, openai_compat: {} },
      agents: { enabled_agent_ids: ["default"], profiles: {} },
      secrets: {},
      memory: {},
    },
    secretKeys: [],
    postedNames: [],
  };

  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    const method = route.request().method();

    if (path === "/api/admin/status") {
      return json(route, { model: { provider: "openai", name: "gpt-4.1-mini" }, run_count: 1 });
    }
    if (path === "/api/admin/config" && method === "GET") {
      return json(route, state.config);
    }
    if (path === "/api/admin/config" && method === "POST") {
      state.config = JSON.parse(route.request().postData() || "{}");
      return json(route, { ok: true });
    }
    if (path === "/api/admin/secrets" && method === "GET") {
      return json(route, { keys: state.secretKeys });
    }
    if (path === "/api/admin/secrets" && method === "POST") {
      const body = JSON.parse(route.request().postData() || "{}");
      const key = String(body.name || "").trim();
      if (key) {
        state.postedNames.push(key);
        state.secretKeys = Array.from(new Set([...state.secretKeys, key]));
      }
      return json(route, { ok: true, stored: key });
    }
    if (path.startsWith("/api/admin/secrets/") && method === "DELETE") {
      const key = decodeURIComponent(path.slice("/api/admin/secrets/".length));
      state.secretKeys = state.secretKeys.filter((item) => item !== key);
      return json(route, { ok: true, deleted: key });
    }

    return route.continue();
  });

  await page.goto("/dashboard#/settings?category=chat");
  await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();
  await expect(page.getByText("Discord Setup")).toBeVisible();
  await expect(page.getByText("Discord token: Missing ❌")).toBeVisible();

  await page.getByLabel("Discord bot token").fill("discord-token-123");
  await page.getByRole("button", { name: "Save Token" }).click();

  await expect(page.getByText("Token stored (write-only).")).toBeVisible();
  await expect(page.getByText("Discord token: Present ✅")).toBeVisible();
  await expect.poll(() => state.postedNames).toEqual(["discord/bot_token"]);
});
