import { expect, test, type Page, type Route } from "@playwright/test"

type ContractResponse = Record<string, unknown>

type DiffEntry = {
  field: string
  target_value: unknown
  base_value: unknown
  target_source: string
  base_source: string
}

type ContractState = {
  agents: string[]
  resolvedByAgent: Record<string, ContractResponse>
  config: Record<string, unknown>
  diffRequests: string[]
  rollbackSnapshots: Array<{ id: string; created_at: string; config: Record<string, unknown> }>
  restoreRequests: string[]
}

function deepClone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

function createResolvedContract(agentID: string, provider: string, source: string): ContractResponse {
  return {
    identity: {
      agent_id: agentID,
      display_name: agentID,
    },
    mission: {
      description: `Resolved runtime contract for agent ${agentID}`,
      goals: agentID === "reviewer" ? ["review quality"] : [],
    },
    system_prompt: {
      content: `Prompt for ${agentID}`,
      source: "agentdocs",
    },
    tool_policy: {
      allowed_tools: ["fs.read", "code.search"],
      denied_tools: [],
      default_deny: true,
    },
    delegation_policy: {
      mode: "tool_gated",
      threshold: 4,
      cooldown: 2,
      auto_delegate: true,
      agent_id: agentID,
      max_depth: 12,
    },
    memory_policy: {
      enabled: true,
      max_working_items: 200,
      max_prompt_tokens: 1200,
      auto_checkpoint: true,
      proactive: false,
      embeddings: true,
    },
    model_policy: {
      provider,
      model: provider === "openai" ? "gpt-4o-mini" : "glm-4.5",
      temperature: 0.2,
      max_tokens: 4096,
      timeout_ms: 90000,
    },
    sandbox_policy: {
      active: false,
      provider: "docker",
      docker: {
        image: "openclawssy/sandbox:latest",
        host: "",
        network_enabled: false,
        cpu_limit: 0,
        memory_limit_mb: 0,
        hardened: false,
        require_dedicated_daemon: false,
        allowed_images: [],
        pids_limit: 0,
        extra_env: [],
        mounts: [],
        pull_policy: "",
      },
    },
    observability_policy: {
      audit_enabled: true,
      trace_enabled: true,
      thinking_mode: "on_error",
    },
    inheritance: {
      source: {
        identity: "global",
        "identity.agent_id": "global",
        "identity.display_name": "global",
        mission: "global",
        "mission.description": "global",
        "mission.goals": agentID === "reviewer" ? "agent-profile" : "global",
        system_prompt: "global",
        "system_prompt.content": "global",
        "system_prompt.source": "global",
        tool_policy: "global",
        "tool_policy.allowed_tools": "global",
        "tool_policy.denied_tools": "global",
        "tool_policy.default_deny": "global",
        delegation_policy: "global",
        "delegation_policy.mode": "global",
        "delegation_policy.threshold": "global",
        "delegation_policy.cooldown": "global",
        "delegation_policy.auto_delegate": "global",
        "delegation_policy.agent_id": "global",
        "delegation_policy.max_depth": "global",
        memory_policy: "global",
        "memory_policy.enabled": "global",
        "memory_policy.max_working_items": "global",
        "memory_policy.max_prompt_tokens": "global",
        "memory_policy.auto_checkpoint": "global",
        "memory_policy.proactive": "global",
        "memory_policy.embeddings": "global",
        model_policy: source,
        "model_policy.provider": source,
        "model_policy.model": source,
        "model_policy.temperature": "global",
        "model_policy.max_tokens": "global",
        "model_policy.timeout_ms": source,
        sandbox_policy: "global",
        "sandbox_policy.active": "global",
        "sandbox_policy.provider": "global",
        "sandbox_policy.docker": "global",
        "sandbox_policy.docker.image": "global",
        "sandbox_policy.docker.host": "global",
        "sandbox_policy.docker.network_enabled": "global",
        "sandbox_policy.docker.cpu_limit": "global",
        "sandbox_policy.docker.memory_limit_mb": "global",
        "sandbox_policy.docker.hardened": "global",
        "sandbox_policy.docker.require_dedicated_daemon": "global",
        "sandbox_policy.docker.allowed_images": "global",
        "sandbox_policy.docker.pids_limit": "global",
        "sandbox_policy.docker.extra_env": "global",
        "sandbox_policy.docker.mounts": "global",
        "sandbox_policy.docker.pull_policy": "global",
        observability_policy: "global",
        "observability_policy.audit_enabled": "global",
        "observability_policy.trace_enabled": "global",
        "observability_policy.thinking_mode": "global",
      },
    },
  }
}

function computeDiff(target: ContractResponse, base: ContractResponse): DiffEntry[] {
  const targetFlat = flattenContract(target)
  const baseFlat = flattenContract(base)
  const sourceTarget = ((target.inheritance as { source?: Record<string, string> })?.source || {})
  const sourceBase = ((base.inheritance as { source?: Record<string, string> })?.source || {})

  const fields = Array.from(new Set([...Object.keys(targetFlat), ...Object.keys(baseFlat)])).sort()
  return fields
    .filter((field) => JSON.stringify(targetFlat[field]) !== JSON.stringify(baseFlat[field]))
    .map((field) => ({
      field,
      target_value: targetFlat[field],
      base_value: baseFlat[field],
      target_source: sourceTarget[field] || "global",
      base_source: sourceBase[field] || "global",
    }))
}

function flattenContract(contract: ContractResponse): Record<string, unknown> {
  const output: Record<string, unknown> = {}

  const walk = (value: unknown, prefix = "") => {
    if (Array.isArray(value)) {
      if (prefix) {
        output[prefix] = value
      }
      return
    }
    if (!value || typeof value !== "object") {
      if (prefix) {
        output[prefix] = value
      }
      return
    }

    const record = value as Record<string, unknown>
    for (const [key, next] of Object.entries(record)) {
      const path = prefix ? `${prefix}.${key}` : key
      if (path === "inheritance" || path.startsWith("inheritance.")) {
        continue
      }
      walk(next, path)
    }
  }

  walk(contract)
  return output
}

function createState(): ContractState {
  return {
    agents: ["default", "reviewer", "ops"],
    resolvedByAgent: {
      default: createResolvedContract("default", "hatz", "global"),
      reviewer: createResolvedContract("reviewer", "openai", "agent-profile"),
      ops: createResolvedContract("ops", "hatz", "subagent-override"),
    },
    config: {
      model: { provider: "hatz", name: "glm-4.5" },
      providers: {
        openai: { api_key: "openai-secret" },
      },
      discord: {
        token: "discord-secret",
      },
      agents: {
        profiles: {
          reviewer: { model: { provider: "openai", name: "gpt-4o-mini", timeout_ms: 90000 } },
        },
      },
    },
    diffRequests: [],
    rollbackSnapshots: [],
    restoreRequests: [],
  }
}

async function installContractMocks(page: Page, state: ContractState) {
  await page.route("**/*", async (route: Route) => {
    const request = route.request()
    const method = request.method()
    const url = new URL(request.url())
    const { pathname, searchParams } = url

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

    if (pathname === "/api/admin/agents" && method === "GET") {
      await json({ agents: state.agents, selected_agent: "default", active_agent: "default" })
      return
    }

    const contractMatch = pathname.match(/^\/api\/admin\/agents\/([^/]+)\/(resolved|diff)$/)
    if (contractMatch && method === "GET") {
      const agentID = decodeURIComponent(contractMatch[1])
      const action = contractMatch[2]
      const target = deepClone(state.resolvedByAgent[agentID] || state.resolvedByAgent.default)

      if (action === "resolved") {
        await json(target)
        return
      }

      const base = (searchParams.get("base") || "global").trim() || "global"
      state.diffRequests.push(`${agentID}:${base}`)

      const baseline =
        base === "global"
          ? deepClone(state.resolvedByAgent.default)
          : deepClone(state.resolvedByAgent[base] || state.resolvedByAgent.default)
      const differences = computeDiff(target, baseline)

      await json({
        agent_id: agentID,
        base,
        differences,
        count: differences.length,
        resolved: target,
        base_contract: baseline,
      })
      return
    }

    if (pathname === "/api/admin/config" && method === "GET") {
      await json(state.config)
      return
    }

    if (pathname === "/api/admin/config" && method === "PATCH") {
      const body = JSON.parse(request.postData() || "{}") as Record<string, unknown>
      state.config = deepClone(body)
      await json({ ok: true, config: body })
      return
    }

    const rollbackSaveMatch = pathname.match(/^\/api\/admin\/agents\/([^/]+)\/rollback-snapshot\/?$/)
    if (rollbackSaveMatch && method === "POST") {
      const snapshot = {
        id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        created_at: new Date().toISOString(),
        config: deepClone(state.config),
      }
      state.rollbackSnapshots = [snapshot, ...state.rollbackSnapshots].slice(0, 10)
      await json({ ok: true, snapshot: { id: snapshot.id, created_at: snapshot.created_at } })
      return
    }

    const rollbackRestoreMatch = pathname.match(/^\/api\/admin\/agents\/([^/]+)\/rollback-restore\/?$/)
    if (rollbackRestoreMatch && method === "POST") {
      const body = JSON.parse(request.postData() || "{}") as { snapshot_id?: string }
      const snapshotID = (body.snapshot_id || "").trim()
      const snapshot = state.rollbackSnapshots.find((item) => item.id === snapshotID)
      if (!snapshot) {
        await json({ error: { code: "contract.snapshot_not_found", message: "snapshot not found" } }, 404)
        return
      }
      state.restoreRequests.push(snapshotID)
      state.config = deepClone(snapshot.config)
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

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("openclawssy.dashboard.bearer", "e2e-token")
  })
})

test("Agent Contract page is reachable from sidebar and lists configured agents", async ({ page }) => {
  const state = createState()
  await installContractMocks(page, state)

  await page.goto("/dashboard#/help")
  await page.getByRole("link", { name: "Agent Contract" }).click()

  await expect(page).toHaveURL(/#\/agent-contract$/)
  await expect(page.getByRole("heading", { name: "Agent Contract", level: 2 })).toBeVisible()
  await expect(page.locator("#agent-contract-agent-selector option")).toHaveCount(3)
  await expect(page.locator("#agent-contract-agent-selector")).toHaveValue("default")
})

test("resolved contract view shows sectioned fields with source badges and raw toggle filters inherited fields", async ({ page }) => {
  const state = createState()
  await installContractMocks(page, state)

  await page.goto("/dashboard#/agent-contract")

  await expect(page.getByRole("heading", { name: "Resolved contract", level: 3 })).toBeVisible()
  await expect(page.getByRole("heading", { name: "Identity", level: 4 })).toBeVisible()
  await expect(page.getByRole("heading", { name: "Model Policy", level: 4 })).toBeVisible()

  await expect(page.locator("[data-testid='contract-field-model_policy.provider']")).toContainText("hatz")
  await expect(page.locator("[data-testid='contract-field-model_policy.provider']")).toContainText("Global")

  await page.locator("#agent-contract-agent-selector").selectOption("reviewer")
  await expect(page.locator("[data-testid='contract-field-model_policy.provider']")).toContainText("openai")
  await expect(page.locator("[data-testid='contract-field-model_policy.provider']")).toContainText("Agent Profile")

  await expect(page.locator("[data-testid='contract-field-memory_policy.enabled']")).toBeVisible()

  await page.getByRole("button", { name: "Raw" }).click()
  await expect(page.locator("[data-testid='contract-field-memory_policy.enabled']")).toHaveCount(0)
  await expect(page.locator("[data-testid='contract-field-model_policy.provider']")).toBeVisible()
})

test("diff view compares against global and another agent", async ({ page }) => {
  const state = createState()
  await installContractMocks(page, state)

  await page.goto("/dashboard#/agent-contract")
  await page.locator("#agent-contract-agent-selector").selectOption("reviewer")

  await page.getByRole("button", { name: "Diff" }).click()
  await expect(page.getByRole("heading", { name: "Diff view", level: 3 })).toBeVisible()

  await expect(page.locator("[data-testid='contract-diff-row-model_policy.provider']")).toContainText("openai")
  await expect(page.locator("[data-testid='contract-diff-row-model_policy.provider']")).toContainText("hatz")

  await page.locator("#agent-contract-diff-base").selectOption("ops")
  await expect(page.locator("[data-testid='contract-diff-row-model_policy.provider']")).toContainText("openai")
  await expect.poll(() => state.diffRequests.at(-1)).toBe("reviewer:ops")
})

test("rollback snapshots are server-backed and restore preserves secret fields", async ({ page }) => {
  const state = createState()
  await installContractMocks(page, state)
  const baselineConfig = deepClone(state.config)

  await page.goto("/dashboard#/agent-contract")
  await page.getByRole("button", { name: "Save rollback snapshot" }).click()

  await expect(page.getByText("Snapshot saved.")).toBeVisible()
  await expect(page.getByRole("button", { name: "Restore" })).toBeVisible()

  state.config = {
    model: { provider: "openai", name: "gpt-4o-mini" },
    providers: {
      openai: { api_key: "mutated-secret" },
    },
    discord: {
      token: "mutated-token",
    },
  }

  await page.getByRole("button", { name: "Restore" }).click()
  await expect.poll(() => state.restoreRequests.length).toBe(1)
  await expect(page.getByText("Rollback restored successfully.")).toBeVisible()
  expect(state.config).toEqual(baselineConfig)
})
