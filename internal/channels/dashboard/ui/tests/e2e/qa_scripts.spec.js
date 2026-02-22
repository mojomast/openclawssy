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

  const state = {
    schedulerPaused: false,
    jobs: [],
    secrets: ["discord/bot_token"],
  };

  await page.route("**/*", async (route) => {
    const reqUrl = new URL(route.request().url());
    const path = reqUrl.pathname;
    const method = route.request().method();

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

    if (path === "/v1/runs" && method === "GET") {
      return json(route, {
        runs: [
          {
            id: "run_qa_1",
            agent_id: "default",
            status: "failed",
            updated_at: "2026-02-18T12:00:00Z",
            provider: "openai",
            model: "gpt-4.1-mini",
          },
        ],
        total: 1,
        limit: 10,
        offset: 0,
      });
    }

    if (path === "/v1/runs/run_qa_1" && method === "GET") {
      return json(route, {
        id: "run_qa_1",
        status: "failed",
        updated_at: "2026-02-18T12:00:00Z",
        provider: "openai",
        model: "gpt-4.1-mini",
        trace: {
          run_id: "run_qa_1",
          tool_execution_results: [
            {
              tool: "fs.edit",
              tool_call_id: "tool-1",
              duration_ms: 30,
              arguments: JSON.stringify({ path: "README.md" }),
              output: "",
              error: "missing argument: edits",
            },
          ],
        },
      });
    }

    if (path === "/api/admin/debug/runs/run_qa_1/trace" && method === "GET") {
      return json(route, {
        trace: {
          run_id: "run_qa_1",
          tool_execution_results: [
            {
              tool: "fs.edit",
              tool_call_id: "tool-1",
              duration_ms: 30,
              arguments: JSON.stringify({ path: "README.md" }),
              output: "",
              error: "missing argument: edits",
            },
          ],
        },
      });
    }

    if (path === "/api/admin/chat/sessions" && method === "GET") {
      return json(route, {
        sessions: [
          {
            session_id: "sess_1",
            title: "qa session",
            user_id: "dashboard_user",
            room_id: "dashboard",
            channel: "dashboard",
            agent_id: "default",
            updated_at: "2026-02-18T12:00:00Z",
            created_at: "2026-02-18T11:59:00Z",
          },
        ],
        total: 1,
        limit: 25,
        offset: 0,
      });
    }

    if (path.startsWith("/api/admin/chat/sessions/") && path.endsWith("/messages") && method === "GET") {
      return json(route, {
        session_id: "sess_1",
        messages: [
          { role: "user", content: "help", ts: "2026-02-18T12:00:00Z" },
          {
            role: "tool",
            tool_name: "python.exec",
            tool_call_id: "tool_py_1",
            run_id: "run_qa_1",
            ts: "2026-02-18T12:00:01Z",
            content: JSON.stringify({
              error: "externally-managed-environment",
              summary: "pip install failed",
            }),
          },
        ],
      });
    }

    if (path === "/api/admin/secrets" && method === "GET") {
      return json(route, { keys: state.secrets });
    }

    if (path === "/api/admin/secrets" && method === "POST") {
      const body = JSON.parse(route.request().postData() || "{}");
      const key = String(body.name || "").trim();
      if (key && !state.secrets.includes(key)) {
        state.secrets.push(key);
      }
      return json(route, { ok: true });
    }

    if (path === "/api/admin/scheduler/jobs" && method === "GET") {
      return json(route, { paused: state.schedulerPaused, jobs: state.jobs });
    }

    if (path === "/api/admin/scheduler/jobs" && method === "POST") {
      const body = JSON.parse(route.request().postData() || "{}");
      const id = String(body.id || "job_1").trim() || "job_1";
      const job = {
        id,
        agent_id: String(body.agent_id || "default"),
        schedule: String(body.schedule || "@every 5m"),
        message: String(body.message || "status ping"),
        enabled: body.enabled === undefined ? true : Boolean(body.enabled),
      };
      state.jobs = [...state.jobs.filter((item) => item.id !== id), job];
      return json(route, { ok: true, id });
    }

    if (path.startsWith("/api/admin/scheduler/jobs/") && method === "DELETE") {
      const id = decodeURIComponent(path.split("/").pop() || "");
      state.jobs = state.jobs.filter((item) => item.id !== id);
      return json(route, { ok: true });
    }

    if (path === "/api/admin/scheduler/control" && method === "POST") {
      const body = JSON.parse(route.request().postData() || "{}");
      const action = String(body.action || "").trim();
      const jobID = String(body.job_id || "").trim();
      if (jobID) {
        state.jobs = state.jobs.map((item) => {
          if (item.id !== jobID) {
            return item;
          }
          return { ...item, enabled: action === "resume" };
        });
      } else {
        state.schedulerPaused = action === "pause";
      }
      return json(route, { ok: true });
    }

    if (path === "/v1/chat/messages" && method === "POST") {
      return json(route, {
        id: "run_chat_1",
        status: "running",
        session_id: "sess_1",
        response: "queued",
      });
    }

    if (path === "/v1/runs/run_chat_1" && method === "GET") {
      return json(route, {
        id: "run_chat_1",
        status: "completed",
        session_id: "sess_1",
        output: "done",
        updated_at: "2026-02-18T12:00:03Z",
      });
    }

    return route.continue();
  });
});

test("Script 1: tool failure visibility and schema/fix guidance", async ({ page }) => {
  await page.goto("/dashboard#/runs");
  await page.getByRole("button", { name: "Open" }).click();

  await page.getByText("fs.edit").first().click();
  await page.getByRole("button", { name: "Schema" }).click();
  await expect(page.getByText("Required fields")).toBeVisible();
  await expect(page.getByText("edits (missing)")).toBeVisible();
  await expect(page.getByText("Detected fs.edit missing edits[]")).toBeVisible();

  await page.getByRole("button", { name: "Fixes" }).click();
  await expect(page.getByText("Tool input is invalid. Open Schema inspector")).toBeVisible();
  await expect(page.getByRole("button", { name: "Open Schema Inspector" })).toBeVisible();
});

test("Script 2: python dependency guidance with Python Env helper", async ({ page }) => {
  await page.goto("/dashboard#/chat");
  await page.getByRole("button", { name: "Fixes" }).click();
  await expect(page.getByText("Use a project virtual environment and install packages there.")).toBeVisible();
  await page.getByRole("button", { name: "Open Python Env" }).first().click();

  await expect(page.getByRole("heading", { name: "Python Env" })).toBeVisible();
  await expect(page.getByLabel("Virtual env path")).toBeVisible();
  await expect(page.getByText("python3 -m venv .venv")).toBeVisible();
});

test("Script 3: secrets workflow and key visibility", async ({ page }) => {
  await page.goto("/dashboard#/secrets");
  await expect(page.getByRole("heading", { name: "Secrets" })).toBeVisible();
  await expect(page.getByText("PERPLEXITY_API_KEY")).toBeVisible();

  await page.getByLabel("Secret key name").fill("PERPLEXITY_API_KEY");
  await page.getByLabel("Secret value").fill("token-123");
  await page.getByRole("button", { name: "Store Secret" }).click();
  await expect(page.getByText("Stored key: PERPLEXITY_API_KEY")).toBeVisible();
  await expect(page.getByText("PERPLEXITY_API_KEY").first()).toBeVisible();
});

test("Script 4: scheduler create, disable, pause, delete", async ({ page }) => {
  await page.goto("/dashboard#/scheduler");
  await expect(page.getByRole("heading", { name: "Scheduler", exact: true })).toBeVisible();

  await page.getByLabel("schedule").fill("@every 5m");
  await page.getByLabel("message").fill("status ping");
  await page.getByRole("button", { name: "Add job" }).click();
  await expect(page.getByText("Added scheduler job: job_1")).toBeVisible();

  await page.getByRole("button", { name: "Disable" }).click();
  await expect(page.getByText("Disabled job: job_1")).toBeVisible();

  await page.getByRole("button", { name: "Pause scheduler" }).click();
  await expect(page.getByText("Scheduler paused globally.")).toBeVisible();

  await page.getByRole("button", { name: "Delete" }).click();
  await expect(page.getByRole("button", { name: "Confirm delete" })).toBeVisible();
  await page.getByRole("button", { name: "Confirm delete" }).click();
  await expect(page.getByText("Deleted job: job_1")).toBeVisible();
});
