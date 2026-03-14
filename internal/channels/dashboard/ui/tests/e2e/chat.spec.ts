import { expect, test, type Page } from "@playwright/test"

const RESUME_INTERRUPTED_RUN_MESSAGE =
  "Resume your previous response from the exact interruption point. Continue from the cutoff without repeating already-completed text unless needed for coherence."

type RouteState = {
  selectedAgent: string
  chatPosts: Array<Record<string, unknown>>
  cancelCalls: string[]
  streamRequests: Record<string, number>
}

function sseFrame(event: { id?: number; event?: string; data: unknown }): string {
  const lines: string[] = []
  if (typeof event.id === "number") {
    lines.push(`id: ${event.id}`)
  }
  if (event.event) {
    lines.push(`event: ${event.event}`)
  }
  lines.push(`data: ${JSON.stringify(event.data)}`)
  return `${lines.join("\n")}\n\n`
}

async function installChatRoutes(
  page: Page,
  options: {
    initialMessages?: Array<Record<string, unknown>>
    onStreamRequest?: (runID: string, attempt: number) => { status: number; body?: string }
  } = {}
): Promise<RouteState> {
  const state: RouteState = {
    selectedAgent: "default",
    chatPosts: [],
    cancelCalls: [],
    streamRequests: {},
  }

  const sessionID = "sess_chat_1"
  const messages = options.initialMessages ?? []

  await page.route("**/*", async (route) => {
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
      await json({ ok: true })
      return
    }

    if (pathname === "/api/admin/agents" && method === "GET") {
      await json({
        agents: ["default", "research"],
        active_agent: state.selectedAgent,
        selected_agent: state.selectedAgent,
        profile_context: {
          exists: state.selectedAgent === "research",
          enabled: true,
          self_improvement: false,
          model_provider: state.selectedAgent === "research" ? "openai" : "",
          model_name: state.selectedAgent === "research" ? "gpt-4.1-mini" : "",
          model_max_tokens: 0,
          model_timeout_ms: 0,
        },
        agents_config: {
          allow_agent_model_overrides: true,
          allow_inter_agent_messaging: true,
          self_improvement_enabled: false,
        },
      })
      return
    }

    if (pathname === "/api/admin/agents" && method === "POST") {
      const payload = JSON.parse(request.postData() || "{}")
      const nextAgent = String(payload.agent_id || "").trim() || "default"
      state.selectedAgent = nextAgent
      await json({
        agents: ["default", "research"],
        active_agent: nextAgent,
        selected_agent: nextAgent,
        profile_context: {
          exists: nextAgent === "research",
          enabled: true,
          self_improvement: false,
          model_provider: nextAgent === "research" ? "openai" : "",
          model_name: nextAgent === "research" ? "gpt-4.1-mini" : "",
          model_max_tokens: 0,
          model_timeout_ms: 0,
        },
        agents_config: {
          allow_agent_model_overrides: true,
          allow_inter_agent_messaging: true,
          self_improvement_enabled: false,
        },
      })
      return
    }

    if (pathname === "/api/admin/chat/sessions" && method === "GET") {
      await json({
        sessions: [
          {
            session_id: sessionID,
            updated_at: "2026-03-14T00:00:00Z",
          },
        ],
        total: 1,
        limit: 1,
        offset: 0,
      })
      return
    }

    if (pathname === `/api/admin/chat/sessions/${sessionID}/messages` && method === "GET") {
      await json({ session_id: sessionID, messages })
      return
    }

    if (pathname === "/v1/chat/messages" && method === "POST") {
      const payload = JSON.parse(request.postData() || "{}")
      state.chatPosts.push(payload)
      await json({ id: "run_chat_1", status: "running", session_id: sessionID })
      return
    }

    if (pathname === "/v1/runs/run_chat_1" && method === "GET") {
      await json({
        id: "run_chat_1",
        status: "running",
        session_id: sessionID,
        updated_at: "2026-03-14T00:00:01Z",
      })
      return
    }

    if (pathname === "/v1/runs/run_chat_1/cancel" && method === "POST") {
      state.cancelCalls.push("run_chat_1")
      await json({ id: "run_chat_1", status: "canceling", cancelled: true })
      return
    }

    if (pathname === "/api/admin/monitor/runs/control" && method === "POST") {
      await json({ cancelled: true })
      return
    }

    if (pathname === "/v1/runs/events/run_chat_1" && method === "GET") {
      state.streamRequests.run_chat_1 = (state.streamRequests.run_chat_1 || 0) + 1
      const attempt = state.streamRequests.run_chat_1
      const streamResponse = options.onStreamRequest?.("run_chat_1", attempt)
      if (streamResponse) {
        await route.fulfill({
          status: streamResponse.status,
          contentType: "text/event-stream",
          body: streamResponse.body || "",
        })
        return
      }

      const body =
        sseFrame({
          id: 1,
          event: "status",
          data: {
            id: 1,
            type: "status",
            run_id: "run_chat_1",
            ts: "2026-03-14T00:00:01Z",
            data: { status: "running", session_id: sessionID },
          },
        }) +
        sseFrame({
          id: 2,
          event: "model_text",
          data: {
            id: 2,
            type: "model_text",
            run_id: "run_chat_1",
            ts: "2026-03-14T00:00:02Z",
            data: { text: "Hello world", partial: true },
          },
        }) +
        sseFrame({
          id: 3,
          event: "completed",
          data: {
            id: 3,
            type: "completed",
            run_id: "run_chat_1",
            ts: "2026-03-14T00:00:03Z",
            data: { status: "completed", output: "Hello world" },
          },
        })

      await route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body,
      })
      return
    }

    await route.continue()
  })

  return state
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("openclawssy.dashboard.bearer", "e2e-token")
    window.localStorage.setItem(
      "dashboard.chat_layout.p1",
      JSON.stringify({ activityPaneWidth: 368, showToolActivityInTranscript: true })
    )
  })
})

test("shows empty state and prevents empty/whitespace submissions", async ({ page }) => {
  await installChatRoutes(page)
  await page.goto("/dashboard#/chat")

  await expect(page.getByRole("heading", { name: "Chat", level: 2 })).toBeVisible()
  await expect(page.getByText("Send a message to begin.")).toBeVisible()

  const sendButton = page.getByRole("button", { name: "Send" })
  await expect(sendButton).toBeDisabled()

  await page.getByLabel("Message composer").fill("   ")
  await expect(sendButton).toBeDisabled()

  await page.getByLabel("Message composer").fill("Hello")
  await expect(sendButton).toBeEnabled()
})

test("sends a message, streams model_text SSE events, and finalizes on completed", async ({ page }) => {
  const state = await installChatRoutes(page)
  await page.goto("/dashboard#/chat")

  await page.getByLabel("Message composer").fill("Explain the migration")
  await page.getByRole("button", { name: "Send" }).click()

  await expect(page.getByText("Explain the migration")).toBeVisible()
  await expect(page.getByText("Hello world")).toBeVisible()
  await expect(page.getByText("Run: run_chat_1")).toBeVisible()

  expect(state.chatPosts).toHaveLength(1)
  expect(state.chatPosts[0]?.message).toBe("Explain the migration")
})

test("renders inline tool timeline cards with expand/collapse and supports timeline toggle", async ({ page }) => {
  const body =
    sseFrame({
      id: 1,
      event: "status",
      data: {
        id: 1,
        type: "status",
        run_id: "run_chat_1",
        ts: "2026-03-14T00:00:01Z",
        data: { status: "running", session_id: "sess_chat_1" },
      },
    }) +
    sseFrame({
      id: 2,
      event: "tool_end",
      data: {
        id: 2,
        type: "tool_end",
        run_id: "run_chat_1",
        ts: "2026-03-14T00:00:02Z",
        data: {
          tool: "fs.search",
          tool_call_id: "tool_1",
          duration_ms: 44,
          arguments: { path: "src" },
          output: { matches: 3 },
          error: "",
        },
      },
    }) +
    sseFrame({
      id: 3,
      event: "completed",
      data: {
        id: 3,
        type: "completed",
        run_id: "run_chat_1",
        ts: "2026-03-14T00:00:03Z",
        data: { status: "completed", output: "done" },
      },
    })

  await installChatRoutes(page, {
    onStreamRequest: () => ({ status: 200, body }),
  })

  await page.goto("/dashboard#/chat")
  await page.getByLabel("Message composer").fill("find tools")
  await page.getByRole("button", { name: "Send" }).click()

  const transcript = page.getByTestId("chat-transcript")
  await expect(transcript.getByTestId("transcript-tool-card")).toHaveCount(1)

  await transcript.getByRole("button", { name: /Expand tool fs.search/ }).click()
  await expect(transcript.getByText("Arguments")).toBeVisible()
  await expect(transcript.getByText("Output")).toBeVisible()

  await page.getByRole("button", { name: "Tool timeline: on" }).click()
  await expect(page.getByRole("button", { name: "Tool timeline: off" })).toBeVisible()
  await expect(transcript.getByTestId("transcript-tool-card")).toHaveCount(0)
})

test("agent picker switches active agent and subsequent send uses the selected agent", async ({ page }) => {
  const state = await installChatRoutes(page)
  await page.goto("/dashboard#/chat")

  await page.getByLabel("Agent picker").selectOption("research")
  await expect(page.getByText("Selected: research")).toBeVisible()

  await page.getByLabel("Message composer").fill("Use research agent")
  await page.getByRole("button", { name: "Send" }).click()

  expect(state.chatPosts).toHaveLength(1)
  expect(state.chatPosts[0]?.agent_id).toBe("research")
})

test("shows SSE connection error indicator with retry and resume button for interrupted responses", async ({ page }) => {
  const state = await installChatRoutes(page, {
    initialMessages: [
      {
        role: "assistant",
        content:
          "Connection dropped. Send `continue` to resume from the cutoff point without repeating everything.",
        ts: "2026-03-14T00:00:00Z",
      },
    ],
    onStreamRequest: (_runID, attempt) => {
      if (attempt === 1) {
        return { status: 500, body: "stream unavailable" }
      }

      const body =
        sseFrame({
          id: 10,
          event: "status",
          data: {
            id: 10,
            type: "status",
            run_id: "run_chat_1",
            ts: "2026-03-14T00:00:10Z",
            data: { status: "running", session_id: "sess_chat_1" },
          },
        }) +
        sseFrame({
          id: 11,
          event: "completed",
          data: {
            id: 11,
            type: "completed",
            run_id: "run_chat_1",
            ts: "2026-03-14T00:00:11Z",
            data: { status: "completed", output: "Recovered response" },
          },
        })
      return { status: 200, body }
    },
  })

  await page.goto("/dashboard#/chat")

  await expect(page.getByRole("button", { name: "Resume interrupted run" }).first()).toBeVisible()
  await page.getByRole("button", { name: "Resume interrupted run" }).first().click()

  await expect.poll(() => state.streamRequests.run_chat_1 || 0).toBeGreaterThanOrEqual(1)
  await expect(page.getByText("SSE connection error")).toBeVisible()

  await page.getByRole("button", { name: "Retry stream" }).click()
  await expect.poll(() => state.streamRequests.run_chat_1 || 0).toBeGreaterThanOrEqual(2)
  await expect(page.getByText("Recovered response")).toBeVisible()

  expect(state.chatPosts.at(-1)?.message).toBe(RESUME_INTERRUPTED_RUN_MESSAGE)
})
