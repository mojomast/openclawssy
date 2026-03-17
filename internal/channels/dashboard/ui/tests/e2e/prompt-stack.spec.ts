import { expect, test, type Page, type Route } from "@playwright/test"

type LayerID =
  | "global_operator_policy"
  | "agent_identity"
  | "tool_safety_rules"
  | "delegation_policy"
  | "session_overlay"

type PromptLayer = {
  layer_id: LayerID
  content: string
  version: number
  updated_at: string
}

type PromptSnapshot = {
  version: number
  updated_at: string
  changed_layer: string
  layer_version: number
  layer_versions: Record<LayerID, number>
  stack: Record<LayerID, PromptLayer>
}

type PromptStackState = {
  instances: Array<{ id: string; name: string }>
  activeInstanceID: string
  agents: string[]
  selectedAgent: string
  contextWindow: number
  layersByID: Record<LayerID, PromptLayer>
  historyByLayer: Record<LayerID, PromptLayer[]>
  snapshots: PromptSnapshot[]
  saveRequests: Array<{ layerID: LayerID; content: string }>
  rollbackRequests: number[]
}

const LAYER_ORDER: LayerID[] = [
  "global_operator_policy",
  "agent_identity",
  "tool_safety_rules",
  "delegation_policy",
  "session_overlay",
]

function deepClone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

function nowISO(seed = 0): string {
  return new Date(Date.now() + seed * 1000).toISOString()
}

function assemblePrompt(layersByID: Record<LayerID, PromptLayer>): string {
  return LAYER_ORDER
    .map((layerID) => {
      const layer = layersByID[layerID]
      return `## ${layerID}\n${layer.content}`
    })
    .join("\n\n")
}

function estimateWordTokens(content: string): { wordCount: number; tokenCount: number } {
  const wordCount = content.trim() ? content.trim().split(/\s+/).length : 0
  return {
    wordCount,
    tokenCount: Math.ceil(wordCount * 1.3),
  }
}

function estimateStackTokens(layersByID: Record<LayerID, PromptLayer>) {
  const layers = LAYER_ORDER.map((layerID) => {
    const layer = layersByID[layerID]
    const estimate = estimateWordTokens(layer.content)
    return {
      layer_id: layer.layer_id,
      content: layer.content,
      version: layer.version,
      updated_at: layer.updated_at,
      word_count: estimate.wordCount,
      token_count: estimate.tokenCount,
    }
  })

  return {
    layers,
    totalTokens: layers.reduce((sum, layer) => sum + layer.token_count, 0),
  }
}

function diffPrompts(fromPrompt: string, toPrompt: string) {
  const fromLines = fromPrompt.split("\n")
  const toLines = toPrompt.split("\n")
  const lines: Array<{ type: string; content: string }> = []

  let i = 0
  let j = 0
  while (i < fromLines.length || j < toLines.length) {
    const from = fromLines[i]
    const to = toLines[j]
    if (from === to) {
      lines.push({ type: "unchanged", content: from || "" })
      i += 1
      j += 1
      continue
    }
    if (from !== undefined) {
      lines.push({ type: "removed", content: from })
      i += 1
    }
    if (to !== undefined) {
      lines.push({ type: "added", content: to })
      j += 1
    }
  }

  return lines
}

function createPromptStackState(): PromptStackState {
  const initialContent: Record<LayerID, string> = {
    global_operator_policy: "Operate safely and keep all actions auditable.",
    agent_identity: "You are the default implementation agent.",
    tool_safety_rules: "Allowed tools: fs.read, fs.search. Avoid destructive operations.",
    delegation_policy: "Delegate to implementer when code edits are required.",
    session_overlay: "Stop and return when completion criteria are satisfied.",
  }

  const layersByID = LAYER_ORDER.reduce<Record<LayerID, PromptLayer>>((acc, layerID, index) => {
    acc[layerID] = {
      layer_id: layerID,
      content: initialContent[layerID],
      version: 1,
      updated_at: nowISO(index + 1),
    }
    return acc
  }, {} as Record<LayerID, PromptLayer>)

  const historyByLayer = LAYER_ORDER.reduce<Record<LayerID, PromptLayer[]>>((acc, layerID) => {
    acc[layerID] = [deepClone(layersByID[layerID])]
    return acc
  }, {} as Record<LayerID, PromptLayer[]>)

  const snapshots: PromptSnapshot[] = LAYER_ORDER.map((changedLayer, index) => ({
    version: index + 1,
    updated_at: nowISO(index + 1),
    changed_layer: changedLayer,
    layer_version: 1,
    layer_versions: {
      global_operator_policy: layersByID.global_operator_policy.version,
      agent_identity: layersByID.agent_identity.version,
      tool_safety_rules: layersByID.tool_safety_rules.version,
      delegation_policy: layersByID.delegation_policy.version,
      session_overlay: layersByID.session_overlay.version,
    },
    stack: deepClone(layersByID),
  }))

  return {
    instances: [
      { id: "default", name: "Default" },
      { id: "lab", name: "Lab" },
    ],
    activeInstanceID: "lab",
    agents: ["default", "reviewer"],
    selectedAgent: "default",
    contextWindow: 24,
    layersByID,
    historyByLayer,
    snapshots,
    saveRequests: [],
    rollbackRequests: [],
  }
}

function appendSnapshot(state: PromptStackState, changedLayer: LayerID): void {
  const currentLayer = state.layersByID[changedLayer]
  state.snapshots.push({
    version: state.snapshots.length + 1,
    updated_at: currentLayer.updated_at,
    changed_layer: changedLayer,
    layer_version: currentLayer.version,
    layer_versions: {
      global_operator_policy: state.layersByID.global_operator_policy.version,
      agent_identity: state.layersByID.agent_identity.version,
      tool_safety_rules: state.layersByID.tool_safety_rules.version,
      delegation_policy: state.layersByID.delegation_policy.version,
      session_overlay: state.layersByID.session_overlay.version,
    },
    stack: deepClone(state.layersByID),
  })
}

async function installPromptStackMocks(page: Page, state: PromptStackState): Promise<void> {
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

    if (pathname === "/api/admin/config" && method === "GET") {
      await json({ model: { context_window: state.contextWindow } })
      return
    }

    if (pathname === "/api/admin/instances" && method === "GET") {
      await json({ instances: state.instances, active_instance_id: state.activeInstanceID })
      return
    }

    if (pathname === "/api/admin/instances/active" && method === "GET") {
      const instance = state.instances.find((item) => item.id === state.activeInstanceID) || state.instances[0]
      await json({ instance })
      return
    }

    if (pathname === "/api/admin/agents" && method === "GET") {
      await json({ agents: state.agents, selected_agent: state.selectedAgent })
      return
    }

    const instanceAgentsMatch = pathname.match(/^\/api\/admin\/instances\/([^/]+)\/agents$/)
    if (instanceAgentsMatch && method === "GET") {
      await json({
        instance_id: decodeURIComponent(instanceAgentsMatch[1]),
        agents: state.agents.map((agentID) => ({ agent_id: agentID })),
      })
      return
    }

    const promptStackMatch = pathname.match(/^\/api\/admin\/instances\/([^/]+)\/agents\/([^/]+)\/prompt-stack(?:\/([^/]+))?$/)
    if (promptStackMatch) {
      const action = promptStackMatch[3] || ""

      if (!action && method === "GET") {
        await json({
          agent_id: state.selectedAgent,
          layers: LAYER_ORDER.map((layerID) => deepClone(state.layersByID[layerID])),
        })
        return
      }

      if (method === "PUT" && LAYER_ORDER.includes(action as LayerID)) {
        const layerID = action as LayerID
        const body = JSON.parse(request.postData() || "{}") as { content?: string }
        const nextContent = String(body.content || "")
        const previous = state.layersByID[layerID]
        const updated: PromptLayer = {
          layer_id: layerID,
          content: nextContent,
          version: previous.version + 1,
          updated_at: nowISO(state.snapshots.length + 1),
        }
        state.layersByID[layerID] = updated
        state.historyByLayer[layerID].push(deepClone(updated))
        state.saveRequests.push({ layerID, content: nextContent })
        appendSnapshot(state, layerID)

        await json({
          ok: true,
          updated_layer: deepClone(updated),
          layers: LAYER_ORDER.map((id) => deepClone(state.layersByID[id])),
        })
        return
      }

      if (action === "preview" && method === "GET") {
        const estimate = estimateStackTokens(state.layersByID)
        await json({
          agent_id: state.selectedAgent,
          layers: estimate.layers,
          assembled_prompt: assemblePrompt(state.layersByID),
          total_tokens: estimate.totalTokens,
          estimation_method: "word_count_x1.3",
        })
        return
      }

      if (action === "history" && method === "GET") {
        await json({
          agent_id: state.selectedAgent,
          layers: deepClone(state.historyByLayer),
          versions: state.snapshots.map((snapshot) => ({
            version: snapshot.version,
            updated_at: snapshot.updated_at,
            changed_layer: snapshot.changed_layer,
            layer_version: snapshot.layer_version,
            layer_versions: snapshot.layer_versions,
          })),
          count: state.snapshots.length,
        })
        return
      }

      if (action === "diff" && method === "GET") {
        const v1 = Number.parseInt(searchParams.get("v1") || "", 10)
        const v2 = Number.parseInt(searchParams.get("v2") || "", 10)
        const from = state.snapshots.find((snapshot) => snapshot.version === v1)
        const to = state.snapshots.find((snapshot) => snapshot.version === v2)
        if (!from || !to) {
          await json({ error: { code: "promptstack.version_not_found", message: "version not found" } }, 404)
          return
        }
        const fromPrompt = assemblePrompt(from.stack)
        const toPrompt = assemblePrompt(to.stack)
        const lines = diffPrompts(fromPrompt, toPrompt)
        await json({
          agent_id: state.selectedAgent,
          from_version: v1,
          to_version: v2,
          from_prompt: fromPrompt,
          to_prompt: toPrompt,
          diff: {
            layer_id: "all_layers",
            from_version: v1,
            to_version: v2,
            lines,
            unified_diff: `--- version ${v1}\n+++ version ${v2}`,
          },
        })
        return
      }

      if (action === "rollback" && method === "POST") {
        const body = JSON.parse(request.postData() || "{}") as { version?: number }
        const version = Number(body.version || 0)
        const snapshot = state.snapshots.find((item) => item.version === version)
        if (!snapshot) {
          await json({ error: { code: "promptstack.version_not_found", message: "requested version not found" } }, 404)
          return
        }
        state.rollbackRequests.push(version)
        state.layersByID = deepClone(snapshot.stack)
        await json({
          ok: true,
          version,
          layers: LAYER_ORDER.map((layerID) => deepClone(state.layersByID[layerID])),
        })
        return
      }

      if (action === "lint" && method === "POST") {
        const issues = [
          {
            severity: "warning",
            description: "Vague delegation language in delegation_policy: \"maybe delegate to implementer\" lacks an explicit trigger.",
            layer_id: "delegation_policy",
            suggested_fix: "Specify concrete delegation triggers (for example complexity threshold).",
          },
          {
            severity: "info",
            description: "Missing success criteria: prompt stack does not define how completion is measured.",
            layer_id: "all_layers",
            suggested_fix: "Add explicit completion criteria.",
          },
        ]
        await json({ agent_id: state.selectedAgent, issues, count: issues.length })
        return
      }

      if (action === "test" && method === "POST") {
        const checks = [
          {
            name: "termination_rules",
            passed: true,
            explanation: "PASS: Prompt includes explicit stop/return conditions.",
          },
          {
            name: "allowed_tools_mentioned",
            passed: true,
            explanation: "PASS: Prompt mentions allowed tools or tool allowlist directives.",
          },
          {
            name: "delegation_instructions_present",
            passed: false,
            explanation: "FAIL: Prompt contains delegation instructions for subagent handoff/routing.",
          },
        ]
        await json({
          agent_id: state.selectedAgent,
          passed: false,
          checks,
        })
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

test("renders all prompt layers with merged preview and overflow warning", async ({ page }) => {
  const state = createPromptStackState()
  await installPromptStackMocks(page, state)

  await page.goto("/dashboard#/prompt-stack")

  await expect(page.getByRole("heading", { name: "Prompt Stack", level: 2 })).toBeVisible()
  await expect(page.locator("#prompt-stack-instance-selector")).toHaveValue("lab")
  await expect(page.locator("#prompt-stack-agent-selector")).toHaveValue("default")

  for (const layerID of LAYER_ORDER) {
    await expect(page.getByTestId(`prompt-stack-tab-${layerID}`)).toBeVisible()
  }

  await expect(page.getByTestId("prompt-stack-editor-highlight")).toBeVisible()
  await expect(page.getByTestId("prompt-stack-preview-highlight")).toBeVisible()
  await expect(page.locator('[data-testid="prompt-stack-preview-highlight"] [data-syntax-line="heading"]').first()).toBeVisible()

  await expect(page.getByTestId("prompt-stack-editor")).toContainText("Operate safely and keep all actions auditable.")
  await expect(page.getByTestId("prompt-stack-preview")).toContainText("## global_operator_policy")
  await expect(page.getByTestId("prompt-stack-total-tokens")).toContainText("Total tokens")
  await expect(page.getByTestId("prompt-stack-overflow-warning")).toBeVisible()
  await expect(page.getByTestId("prompt-stack-version-list").locator("li")).toHaveCount(5)
})

test("saving a layer updates preview and version history", async ({ page }) => {
  const state = createPromptStackState()
  await installPromptStackMocks(page, state)

  await page.goto("/dashboard#/prompt-stack")

  await page.getByTestId("prompt-stack-tab-agent_identity").click()
  await page.getByTestId("prompt-stack-editor").fill(
    "You are the reviewer agent.\nSuccess criteria: all prompt checks pass."
  )
  await page.getByTestId("prompt-stack-save-layer").click()

  await expect.poll(() => state.saveRequests.length).toBe(1)
  await expect(page.getByTestId("prompt-stack-save-notice")).toContainText("Saved")
  await expect(page.getByTestId("prompt-stack-preview")).toContainText("You are the reviewer agent")
  await expect(page.getByTestId("prompt-stack-version-list").locator("li")).toHaveCount(6)
})

test("diff, rollback, lint, and structural tests are visible and actionable", async ({ page }) => {
  const state = createPromptStackState()
  await installPromptStackMocks(page, state)

  await page.goto("/dashboard#/prompt-stack")

  await page.getByTestId("prompt-stack-tab-agent_identity").click()
  await page.getByTestId("prompt-stack-editor").fill("Identity version one")
  await page.getByTestId("prompt-stack-save-layer").click()
  await page.getByTestId("prompt-stack-editor").fill("Identity version two")
  await page.getByTestId("prompt-stack-save-layer").click()

  await expect.poll(() => state.saveRequests.length).toBe(2)
  await expect(page.getByTestId("prompt-stack-version-list").locator("li")).toHaveCount(7)

  await page.locator("#prompt-stack-diff-from").selectOption("6")
  await page.locator("#prompt-stack-diff-to").selectOption("7")
  await page.getByTestId("prompt-stack-load-diff").click()

  await expect(page.getByTestId("prompt-stack-diff")).toContainText("Identity version one")
  await expect(page.getByTestId("prompt-stack-diff")).toContainText("Identity version two")

  await page.locator("#prompt-stack-rollback-version").selectOption("6")
  await page.getByTestId("prompt-stack-rollback").click()

  await expect.poll(() => state.rollbackRequests.length).toBe(1)
  await expect(page.getByTestId("prompt-stack-preview")).toContainText("Identity version one")

  await page.getByTestId("prompt-stack-run-lint").click()
  await expect(page.getByTestId("prompt-stack-lint-results")).toContainText("Vague delegation language")
  await expect(page.getByTestId("prompt-stack-lint-results")).toContainText("delegation_policy")

  await page.getByTestId("prompt-stack-run-test").click()
  await expect(page.getByTestId("prompt-stack-test-results")).toContainText("termination_rules")
  await expect(page.getByTestId("prompt-stack-test-results")).toContainText("delegation_instructions_present")
})
