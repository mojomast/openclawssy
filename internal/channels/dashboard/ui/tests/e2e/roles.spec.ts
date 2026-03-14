import { expect, test, type Page, type Route } from "@playwright/test"

type RoleTemplate = {
  name: string
  description: string
  allowed_tools: string[]
  denied_tools: string[]
  max_iterations: number
  timeout_ms: number
  memory_access_scope: string
  writable_paths: string[]
  prompt_contract: string
  output_schema: Record<string, unknown>
  handoff_schema: Record<string, unknown>
  escalation_rules: string[]
  delegation_permissions: string[]
  is_builtin: boolean
}

type RolesState = {
  roles: RoleTemplate[]
  createRequests: string[]
  updateRequests: string[]
  deleteRequests: string[]
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

function createRolesState(): RolesState {
  return {
    roles: [
      {
        name: "scout",
        description: "Read-only discovery specialist.",
        allowed_tools: ["fs.list", "fs.read", "fs.search", "web.get"],
        denied_tools: [],
        max_iterations: 20,
        timeout_ms: 120000,
        memory_access_scope: "read_only",
        writable_paths: [],
        prompt_contract: "Collect evidence and summarize findings.",
        output_schema: { type: "object" },
        handoff_schema: { type: "object" },
        escalation_rules: ["Escalate when required sources are unavailable."],
        delegation_permissions: [],
        is_builtin: true,
      },
      {
        name: "planner",
        description: "Decomposition specialist.",
        allowed_tools: ["fs.read", "task.plan"],
        denied_tools: [],
        max_iterations: 30,
        timeout_ms: 120000,
        memory_access_scope: "summary",
        writable_paths: [],
        prompt_contract: "Break work into actionable nodes.",
        output_schema: { type: "object" },
        handoff_schema: { type: "object" },
        escalation_rules: ["Escalate on ambiguous goals."],
        delegation_permissions: ["implementer", "verifier"],
        is_builtin: true,
      },
      {
        name: "implementer",
        description: "Code and patch specialist.",
        allowed_tools: ["fs.read", "fs.edit", "shell.exec"],
        denied_tools: [],
        max_iterations: 100,
        timeout_ms: 120000,
        memory_access_scope: "workspace",
        writable_paths: ["workspace/**"],
        prompt_contract: "Apply minimal safe changes.",
        output_schema: { type: "object" },
        handoff_schema: { type: "object" },
        escalation_rules: ["Escalate on out-of-scope writes."],
        delegation_permissions: ["scout", "verifier"],
        is_builtin: true,
      },
      {
        name: "verifier",
        description: "Validation specialist.",
        allowed_tools: ["fs.read", "test.run"],
        denied_tools: [],
        max_iterations: 50,
        timeout_ms: 120000,
        memory_access_scope: "read_only",
        writable_paths: [],
        prompt_contract: "Run checks and report pass/fail.",
        output_schema: { type: "object" },
        handoff_schema: { type: "object" },
        escalation_rules: ["Escalate when test prerequisites are missing."],
        delegation_permissions: [],
        is_builtin: true,
      },
      {
        name: "reviewer",
        description: "Critique and risk specialist.",
        allowed_tools: ["fs.read", "decision.log"],
        denied_tools: [],
        max_iterations: 30,
        timeout_ms: 120000,
        memory_access_scope: "summary",
        writable_paths: [],
        prompt_contract: "Assess safety and maintainability.",
        output_schema: { type: "object" },
        handoff_schema: { type: "object" },
        escalation_rules: ["Escalate blocking production risks."],
        delegation_permissions: [],
        is_builtin: true,
      },
      {
        name: "operator",
        description: "Control-plane specialist.",
        allowed_tools: ["config.get", "config.set"],
        denied_tools: [],
        max_iterations: 10,
        timeout_ms: 120000,
        memory_access_scope: "none",
        writable_paths: [],
        prompt_contract: "Operate on approved config surfaces only.",
        output_schema: { type: "object" },
        handoff_schema: { type: "object" },
        escalation_rules: ["Escalate before high-impact policy edits."],
        delegation_permissions: [],
        is_builtin: true,
      },
      {
        name: "analyst",
        description: "Custom analysis role.",
        allowed_tools: ["fs.read", "fs.search"],
        denied_tools: ["shell.exec"],
        max_iterations: 12,
        timeout_ms: 90000,
        memory_access_scope: "read_only",
        writable_paths: ["workspace/reports/**"],
        prompt_contract: "Produce concise evidence-backed summaries.",
        output_schema: { type: "object", required: ["summary"] },
        handoff_schema: { type: "object", required: ["status"] },
        escalation_rules: ["Escalate when docs conflict."],
        delegation_permissions: ["scout"],
        is_builtin: false,
      },
    ],
    createRequests: [],
    updateRequests: [],
    deleteRequests: [],
  }
}

async function installRolesMocks(page: Page, state: RolesState): Promise<void> {
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

    if (pathname === "/api/admin/roles" && method === "GET") {
      await json({ roles: clone(state.roles), count: state.roles.length })
      return
    }

    if (pathname === "/api/admin/roles" && method === "POST") {
      const payload = JSON.parse(request.postData() || "{}") as RoleTemplate
      const normalizedName = String(payload.name || "").trim().toLowerCase()
      state.createRequests.push(normalizedName)

      const created: RoleTemplate = {
        ...payload,
        name: normalizedName,
        is_builtin: false,
      }
      state.roles = [...state.roles.filter((role) => role.name !== normalizedName), created]
      await json({ ok: true, role: clone(created) }, 201)
      return
    }

    const rolePathMatch = pathname.match(/^\/api\/admin\/roles\/([^/]+)$/)
    if (rolePathMatch) {
      const roleName = decodeURIComponent(rolePathMatch[1]).trim().toLowerCase()
      const target = state.roles.find((role) => role.name === roleName)

      if (method === "PUT") {
        if (!target) {
          await json({ error: { code: "roles.not_found", message: "role not found" } }, 404)
          return
        }
        if (target.is_builtin) {
          await json({ error: { code: "roles.builtin_immutable", message: "built-in role templates are immutable" } }, 403)
          return
        }
        const payload = JSON.parse(request.postData() || "{}") as RoleTemplate
        state.updateRequests.push(roleName)
        const updated: RoleTemplate = {
          ...payload,
          name: roleName,
          is_builtin: false,
        }
        state.roles = state.roles.map((role) => (role.name === roleName ? updated : role))
        await json({ ok: true, role: clone(updated) })
        return
      }

      if (method === "DELETE") {
        if (!target) {
          await json({ error: { code: "roles.not_found", message: "role not found" } }, 404)
          return
        }
        if (target.is_builtin) {
          await json({ error: { code: "roles.builtin_immutable", message: "built-in role templates are immutable" } }, 403)
          return
        }
        state.deleteRequests.push(roleName)
        state.roles = state.roles.filter((role) => role.name !== roleName)
        await json({ ok: true, deleted: roleName })
        return
      }
    }

    if (pathname.startsWith("/api/") || pathname.startsWith("/v1/")) {
      await json({ ok: true })
      return
    }

    await route.continue()
  })
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("openclawssy.dashboard.bearer", "e2e-token")
  })
})

test("Role Templates page lists built-in and custom roles with read-only built-ins", async ({ page }) => {
  const state = createRolesState()
  await installRolesMocks(page, state)

  await page.goto("/dashboard#/help")
  await page.getByRole("link", { name: "Role Templates" }).click()

  await expect(page).toHaveURL(/#\/roles$/)
  await expect(page.getByRole("heading", { name: "Role Templates", level: 2 })).toBeVisible()
  await expect(page.getByTestId("role-item-scout")).toBeVisible()
  await expect(page.getByTestId("role-item-analyst")).toBeVisible()

  await page.getByTestId("role-item-scout").click()
  await expect(page.getByTestId("role-selected-badge")).toContainText("Built-in")
  await expect(page.getByTestId("role-readonly-message")).toContainText("read-only")
})

test("Role Templates page creates and updates a custom role with full constraints form", async ({ page }) => {
  const state = createRolesState()
  await installRolesMocks(page, state)

  await page.goto("/dashboard#/roles")

  await page.getByTestId("create-role-name").fill("qa-specialist")
  await page.getByTestId("create-role-description").fill("Custom QA specialist")
  await page.getByTestId("create-role-allowed-tools").fill("fs.read, test.run")
  await page.getByTestId("create-role-denied-tools").fill("shell.exec")
  await page.getByTestId("create-role-max-iterations").fill("14")
  await page.getByTestId("create-role-timeout-ms").fill("60000")
  await page.getByTestId("create-role-memory-access-scope").fill("read_only")
  await page.getByTestId("create-role-writable-paths").fill("workspace/tests/**")
  await page.getByTestId("create-role-prompt-contract").fill("Run checks and summarize gaps.")
  await page.getByTestId("create-role-output-schema").fill('{"type":"object","required":["summary"]}')
  await page.getByTestId("create-role-handoff-schema").fill('{"type":"object","required":["status"]}')
  await page.getByTestId("create-role-escalation-rules").fill("Escalate on flaky tests")
  await page.getByTestId("create-role-delegation-permissions").fill("verifier")
  await page.getByTestId("create-role-submit").click()

  await expect.poll(() => state.createRequests.length).toBe(1)
  await expect(page.getByTestId("role-item-qa-specialist")).toBeVisible()

  await page.getByTestId("role-item-qa-specialist").click()
  await expect(page.getByTestId("role-selected-badge")).toContainText("Custom")

  await expect(page.getByTestId("edit-role-name")).toBeVisible()
  await expect(page.getByTestId("edit-role-allowed-tools")).toBeVisible()
  await expect(page.getByTestId("edit-role-denied-tools")).toBeVisible()
  await expect(page.getByTestId("edit-role-max-iterations")).toBeVisible()
  await expect(page.getByTestId("edit-role-timeout-ms")).toBeVisible()
  await expect(page.getByTestId("edit-role-memory-access-scope")).toBeVisible()
  await expect(page.getByTestId("edit-role-writable-paths")).toBeVisible()
  await expect(page.getByTestId("edit-role-prompt-contract")).toBeVisible()
  await expect(page.getByTestId("edit-role-output-schema")).toBeVisible()
  await expect(page.getByTestId("edit-role-handoff-schema")).toBeVisible()
  await expect(page.getByTestId("edit-role-escalation-rules")).toBeVisible()
  await expect(page.getByTestId("edit-role-delegation-permissions")).toBeVisible()

  await page.getByTestId("edit-role-description").fill("Updated QA specialist")
  await page.getByTestId("edit-role-submit").click()
  await expect.poll(() => state.updateRequests.length).toBe(1)
})

test("Role Templates page deletes custom role with confirmation dialog", async ({ page }) => {
  const state = createRolesState()
  await installRolesMocks(page, state)

  await page.goto("/dashboard#/roles")
  await page.getByTestId("role-item-analyst").click()

  await page.getByTestId("edit-role-delete").click()
  await expect(page.getByTestId("delete-role-confirm-dialog")).toBeVisible()
  await page.getByTestId("delete-role-confirm-submit").click()

  await expect.poll(() => state.deleteRequests.length).toBe(1)
  await expect(page.getByTestId("role-item-analyst")).toHaveCount(0)
})
