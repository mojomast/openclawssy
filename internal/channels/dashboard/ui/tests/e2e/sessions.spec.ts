import { expect, test, type Page, type Route } from "@playwright/test"

type MockSession = {
  session_id: string
  title?: string
  user_id?: string
  room_id?: string
  agent_id?: string
  channel?: string
  created_at?: string
  updated_at?: string
}

type MockSessionMessage = {
  role: string
  content: string
  ts: string
  run_id?: string
  tool_call_id?: string
  tool_name?: string
  message_id?: string
  status?: string
  instance_id?: string
  from_agent_id?: string
  to_agent_id?: string
  task_id?: string
  subject?: string
  source_session_id?: string
  related_run_id?: string
  note?: string
  error?: string
}

type SessionMockOptions = {
  sessions: MockSession[]
  messagesBySessionID?: Record<string, MockSessionMessage[]>
  listDelayMS?: number
  messageDelayMS?: number
  instanceAgentsEnabled?: boolean
}

type SessionMockState = {
  listQueries: Array<Record<string, string>>
  messageQueries: Array<{ sessionID: string; limit: string }>
  inboxDetailRequests: string[]
  inboxAckRequests: string[]
  inboxRunRequests: Array<{ path: string; body: string }>
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({
    status,
    contentType: "application/json; charset=utf-8",
    body: JSON.stringify(body),
  })
}

function makeSession(index: number): MockSession {
  return {
    session_id: `session_${String(index).padStart(2, "0")}`,
    title: `Session ${String(index).padStart(2, "0")}`,
    user_id: index % 2 === 0 ? "alice" : "bob",
    room_id: index % 3 === 0 ? "ops" : "qa",
    agent_id: "default",
    channel: "dashboard",
    created_at: `2026-03-14T10:${String(index).padStart(2, "0")}:00Z`,
    updated_at: `2026-03-14T12:${String(index).padStart(2, "0")}:00Z`,
  }
}

async function installSessionsMocks(page: Page, options: SessionMockOptions): Promise<SessionMockState> {
  const state: SessionMockState = {
    listQueries: [],
    messageQueries: [],
    inboxDetailRequests: [],
    inboxAckRequests: [],
    inboxRunRequests: [],
  }

  await page.route("**/*", async (route) => {
    const request = route.request()
    const method = request.method()
    const url = new URL(request.url())
    const { pathname, searchParams } = url

    if (pathname === "/api/admin/status") {
      await json(route, {
        ok: true,
        model: { provider: "hatz", name: "glm-4.5" },
        run_count: options.sessions.length,
      })
      return
    }

    if (pathname === "/api/admin/control-plane/features" && method === "GET") {
      await json(route, {
        features: {
          instance_control: true,
          instance_agents: options.instanceAgentsEnabled ?? true,
          wizard: true,
          eval: true,
        },
      })
      return
    }

    if (pathname === "/api/admin/chat/sessions" && method === "GET") {
      if ((options.listDelayMS || 0) > 0) {
        await new Promise((resolve) => setTimeout(resolve, options.listDelayMS))
      }

      state.listQueries.push(Object.fromEntries(searchParams.entries()))

      const limit = Number(searchParams.get("limit") || "25") || 25
      const offset = Number(searchParams.get("offset") || "0") || 0
      const pageSessions = options.sessions.slice(offset, offset + limit)

      await json(route, {
        sessions: pageSessions,
        total: options.sessions.length,
        limit,
        offset,
      })
      return
    }

    if (pathname.startsWith("/api/admin/chat/sessions/") && pathname.endsWith("/messages") && method === "GET") {
      if ((options.messageDelayMS || 0) > 0) {
        await new Promise((resolve) => setTimeout(resolve, options.messageDelayMS))
      }

      const sessionID = decodeURIComponent(pathname.replace("/api/admin/chat/sessions/", "").replace("/messages", ""))
      const limitText = String(searchParams.get("limit") || "200")
      const limit = Number(limitText) || 200

      state.messageQueries.push({ sessionID, limit: limitText })

      const allMessages = options.messagesBySessionID?.[sessionID] || []
      await json(route, {
        session_id: sessionID,
        messages: allMessages.slice(Math.max(0, allMessages.length - limit)),
      })
      return
    }

    if (pathname === "/api/admin/instances/lab/agents/implementer/inbox/msg_123" && method === "GET") {
      state.inboxDetailRequests.push(pathname)
      await json(route, {
        instance_id: "lab",
        agent_id: "implementer",
        message: {
          message_id: "msg_123",
          status: "acknowledged",
          instance_id: "lab",
          session_id: "inbox_session_1",
          source_session_id: "source_session_1",
          from_agent_id: "planner",
          to_agent_id: "implementer",
          task_id: "task_9",
          message: "implement the patch",
          related_run_id: "run_314",
          note: "dashboard acknowledged",
        },
      })
      return
    }

    if (pathname === "/api/admin/instances/lab/agents/implementer/inbox/msg_123/ack" && method === "POST") {
      state.inboxAckRequests.push(pathname)
      await json(route, {
        ok: true,
        message: {
          message_id: "msg_123",
          status: "acknowledged",
          instance_id: "lab",
          session_id: "inbox_session_1",
          source_session_id: "source_session_1",
          from_agent_id: "planner",
          to_agent_id: "implementer",
          task_id: "task_9",
          message: "implement the patch",
          related_run_id: "run_314",
          note: "dashboard acknowledged",
        },
      })
      return
    }

    if (pathname === "/api/admin/instances/lab/agents/implementer/inbox/msg_123/run" && method === "POST") {
      state.inboxRunRequests.push({ path: pathname, body: request.postData() || "" })
      await json(route, {
        ok: true,
        run_id: "run_315",
        status: "completed",
        message: {
          message_id: "msg_123",
          status: "completed",
          instance_id: "lab",
          session_id: "inbox_session_1",
          source_session_id: "source_session_1",
          from_agent_id: "planner",
          to_agent_id: "implementer",
          task_id: "task_9",
          message: "implement the patch",
          related_run_id: "run_315",
          note: "task completed",
        },
      })
      return
    }

    if (pathname.startsWith("/api/") || pathname.startsWith("/v1/")) {
      await json(route, { ok: true })
      return
    }

    await route.continue()
  })

  return state
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("openclawssy.dashboard.bearer", "e2e-token")
  })
})

test("session list supports search and sort options", async ({ page }) => {
  await installSessionsMocks(page, {
    sessions: [
      {
        session_id: "sess_b",
        title: "Beta Release",
        user_id: "alice",
        room_id: "ops-room",
        updated_at: "2026-03-14T12:10:00Z",
        created_at: "2026-03-14T09:10:00Z",
      },
      {
        session_id: "sess_a",
        title: "Alpha Notes",
        user_id: "qa-user",
        room_id: "qa-room",
        updated_at: "2026-03-14T12:05:00Z",
        created_at: "2026-03-14T09:05:00Z",
      },
      {
        session_id: "sess_c",
        title: "Gamma Sync",
        user_id: "other-user",
        room_id: "random",
        updated_at: "2026-03-14T12:00:00Z",
        created_at: "2026-03-14T09:00:00Z",
      },
    ],
  })

  await page.goto("/dashboard#/sessions")

  await expect(page.getByRole("heading", { name: "Sessions", level: 2 })).toBeVisible()
  await expect(page.locator("#sessions-sort option")).toHaveText([
    "Recently Updated",
    "Oldest",
    "Title A-Z",
    "Session ID A-Z",
  ])

  await page.getByLabel("Search").fill("qa-user")
  await expect(page.locator("[data-testid='sessions-table']")).toContainText("sess_a")
  await expect(page.locator("[data-testid='sessions-table']")).not.toContainText("sess_b")

  await page.getByLabel("Search").fill("")
  await page.getByLabel("Sort").selectOption("title")
  await expect(page.locator("[data-testid='sessions-table'] tbody tr").first()).toContainText("sess_a")

  await page.getByLabel("Sort").selectOption("id")
  await expect(page.locator("[data-testid='sessions-table'] tbody tr").first()).toContainText("sess_a")
})

test("page size selector and Prev/Next pagination update list queries and page meta", async ({ page }) => {
  const state = await installSessionsMocks(page, {
    sessions: Array.from({ length: 26 }, (_, index) => makeSession(index + 1)),
  })

  await page.goto("/dashboard#/sessions")

  await expect(page.getByText("1-25 of 26")).toBeVisible()

  await page.getByLabel("Page size").selectOption("10")
  await expect(page.getByText("1-10 of 26")).toBeVisible()
  await expect.poll(() => state.listQueries.at(-1)?.limit).toBe("10")
  await expect.poll(() => state.listQueries.at(-1)?.offset).toBe("0")

  await page.getByRole("button", { name: "Next" }).click()
  await expect(page.getByText("11-20 of 26")).toBeVisible()
  await expect.poll(() => state.listQueries.at(-1)?.offset).toBe("10")

  await page.getByRole("button", { name: "Next" }).click()
  await expect(page.getByText("21-26 of 26")).toBeVisible()

  await page.getByRole("button", { name: "Prev" }).click()
  await expect(page.getByText("11-20 of 26")).toBeVisible()
})

test("opening a session shows messages and tool events with collapsible args/output/error and inspect link", async ({ page }) => {
  const state = await installSessionsMocks(page, {
    sessions: [
      {
        session_id: "session_tool",
        title: "Tooling Session",
        user_id: "operator",
        room_id: "ops",
        updated_at: "2026-03-14T14:00:00Z",
        created_at: "2026-03-14T13:00:00Z",
      },
    ],
    messagesBySessionID: {
      session_tool: [
        {
          role: "user",
          content: "Please inspect the tool timeline",
          ts: "2026-03-14T14:00:01Z",
        },
        {
          role: "assistant",
          content: "Sure, loading tool events now.",
          ts: "2026-03-14T14:00:02Z",
        },
        {
          role: "tool",
          content: JSON.stringify({
            tool: "fs.read",
            id: "call_42",
            run_id: "run_777",
            summary: "Read README",
            arguments: { path: "README.md" },
            output: { bytes: 1200 },
            error: "permission denied",
          }),
          ts: "2026-03-14T14:00:03Z",
          tool_name: "fs.read",
          tool_call_id: "call_42",
          run_id: "run_777",
        },
      ],
    },
  })

  await page.goto("/dashboard#/sessions")
  await page.getByRole("button", { name: "Open" }).click()

  await expect(page.getByText("Session session_tool", { exact: false })).toBeVisible()
  await expect(page.getByTestId("sessions-message-stream")).toContainText("Please inspect the tool timeline")
  await expect(page.getByTestId("sessions-message-stream")).toContainText("Sure, loading tool events now.")
  await expect(page.getByTestId("sessions-message-stream")).toContainText("fs.read")

  const toolCard = page.locator("[data-testid='sessions-tool-event']").first()
  await toolCard.getByText("Args").click()
  await expect(toolCard).toContainText("README.md")
  await expect(toolCard).toContainText("Read README")
  await expect(toolCard).toContainText("permission denied")

  const inspectLink = toolCard.getByRole("link", { name: "Inspect Tool" })
  await expect(inspectLink).toHaveAttribute("href", /#\/runs\/run_777/)

  await page.getByLabel("Messages").selectOption("500")
  await expect.poll(() => state.messageQueries.at(-1)?.sessionID).toBe("session_tool")
  await expect.poll(() => state.messageQueries.at(-1)?.limit).toBe("500")
})

test("session detail renders lifecycle cards for system message events", async ({ page }) => {
  const state = await installSessionsMocks(page, {
    sessions: [
      {
        session_id: "session_lifecycle",
        title: "Lifecycle Session",
        user_id: "operator",
        room_id: "ops",
        updated_at: "2026-03-14T14:00:00Z",
        created_at: "2026-03-14T13:00:00Z",
      },
    ],
    messagesBySessionID: {
      session_lifecycle: [
        {
          role: "system",
          content: JSON.stringify({ message: "queued for execution" }),
          ts: "2026-03-14T14:00:03Z",
          message_id: "msg_123",
          status: "acknowledged",
          instance_id: "lab",
          from_agent_id: "planner",
          to_agent_id: "implementer",
          task_id: "task_9",
          subject: "handoff",
          source_session_id: "source_session_1",
          related_run_id: "run_314",
          note: "dashboard acknowledged",
        },
      ],
    },
  })

  await page.goto("/dashboard#/sessions")
  await page.getByRole("button", { name: "Open" }).click()

  const lifecycleCard = page.getByTestId("sessions-lifecycle-event").first()
  await expect(lifecycleCard).toContainText("Lifecycle")
  await expect(lifecycleCard).toContainText("acknowledged")
  await expect(lifecycleCard).toContainText("handoff")
  await expect(lifecycleCard).toContainText("Message: msg_123")
  await expect(lifecycleCard).toContainText("Instance: lab")
  await expect(lifecycleCard).toContainText("From: planner")
  await expect(lifecycleCard).toContainText("To: implementer")
  await expect(lifecycleCard).toContainText("Task: task_9")
  await expect(lifecycleCard).toContainText("Source session: source_session_1")
  await expect(lifecycleCard).toContainText("Run: run_314")
  await expect(lifecycleCard).toContainText("dashboard acknowledged")

  await lifecycleCard.getByRole("button", { name: "View inbox detail" }).click()
  await expect.poll(() => state.inboxDetailRequests.length).toBe(1)

  const inboxDetail = page.getByTestId("sessions-inbox-detail")
  await expect(inboxDetail).toContainText("Canonical inbox detail")
  await expect(inboxDetail).toContainText("Original message")
  await expect(inboxDetail).toContainText("implement the patch")
  await expect(inboxDetail).toContainText("Inbox session: inbox_session_1")

  await inboxDetail.getByRole("button", { name: "Acknowledge" }).click()
  await expect.poll(() => state.inboxAckRequests.length).toBe(1)
  await expect(inboxDetail).toContainText("dashboard acknowledged")

  await inboxDetail.getByRole("button", { name: "Run inbox task" }).click()
  await expect.poll(() => state.inboxRunRequests.length).toBe(1)
  await expect(inboxDetail).toContainText("completed")
  await expect(inboxDetail).toContainText("Run: run_315")
  await expect(inboxDetail).toContainText("task completed")
  await expect.poll(() => JSON.parse(state.inboxRunRequests[0]?.body || "{}").source).toBe("dashboard/sessions")
})

test("shows loading and empty states for sessions list and message detail", async ({ page }) => {
  await installSessionsMocks(page, {
    sessions: [
      {
        session_id: "session_empty_messages",
        title: "No Messages Yet",
        user_id: "nobody",
        room_id: "none",
        updated_at: "2026-03-14T15:00:00Z",
        created_at: "2026-03-14T14:00:00Z",
      },
    ],
    messagesBySessionID: {
      session_empty_messages: [],
    },
    listDelayMS: 200,
    messageDelayMS: 200,
  })

  await page.goto("/dashboard#/sessions")
  await expect(page.getByTestId("sessions-list-loading")).toBeVisible()

  await page.getByRole("button", { name: "Open" }).click()
  await expect(page.getByTestId("sessions-messages-loading")).toBeVisible()
  await expect(page.getByText("No messages in this session yet.")).toBeVisible()

  await page.getByLabel("Search").fill("does-not-exist")
  await expect(page.getByText("No sessions match the current search/filter.")).toBeVisible()
})

test("deep link /#/sessions?session={id} opens selected session", async ({ page }) => {
  const state = await installSessionsMocks(page, {
    sessions: [
      {
        session_id: "session_deep",
        title: "Deep Link Session",
        user_id: "alice",
        room_id: "ops",
        updated_at: "2026-03-14T16:00:00Z",
        created_at: "2026-03-14T15:00:00Z",
      },
    ],
    messagesBySessionID: {
      session_deep: [
        {
          role: "assistant",
          content: "Loaded by deep link",
          ts: "2026-03-14T16:00:01Z",
        },
      ],
    },
  })

  await page.goto("/dashboard#/sessions?session=session_deep")

  await expect(page.getByText("Session session_deep", { exact: false })).toBeVisible()
  await expect(page.getByTestId("sessions-message-stream")).toContainText("Loaded by deep link")
  await expect.poll(() => state.messageQueries.at(-1)?.sessionID).toBe("session_deep")
})

test("path deep-link /#/sessions/:sessionId stays synchronized and allows switching sessions", async ({ page }) => {
  const state = await installSessionsMocks(page, {
    sessions: [
      {
        session_id: "session_path",
        title: "Opened From Path",
        user_id: "alice",
        room_id: "ops",
        updated_at: "2026-03-14T17:00:00Z",
        created_at: "2026-03-14T16:00:00Z",
      },
      {
        session_id: "session_next",
        title: "Second Session",
        user_id: "bob",
        room_id: "qa",
        updated_at: "2026-03-14T17:01:00Z",
        created_at: "2026-03-14T16:01:00Z",
      },
    ],
    messagesBySessionID: {
      session_path: [
        {
          role: "assistant",
          content: "Loaded from path deep link",
          ts: "2026-03-14T17:00:01Z",
        },
      ],
      session_next: [
        {
          role: "assistant",
          content: "Switched to second session",
          ts: "2026-03-14T17:01:01Z",
        },
      ],
    },
  })

  await page.goto("/dashboard#/sessions/session_path")

  await expect(page.getByText("Session session_path", { exact: false })).toBeVisible()
  await expect(page.getByTestId("sessions-message-stream")).toContainText("Loaded from path deep link")
  await expect.poll(() => state.messageQueries.at(-1)?.sessionID).toBe("session_path")

  const nextSessionRow = page.locator("[data-testid='sessions-table'] tbody tr").filter({ hasText: "session_next" })
  await nextSessionRow.getByRole("button", { name: "Open" }).click()

  await expect(page.getByText("Session session_next", { exact: false })).toBeVisible()
  await expect(page.getByTestId("sessions-message-stream")).toContainText("Switched to second session")
  await expect(page).toHaveURL(/#\/sessions\/session_next\?session=session_next/)
  await expect.poll(() => state.messageQueries.at(-1)?.sessionID).toBe("session_next")
})

test("Sessions hides nav entry and shows disabled state when instance agents feature is off", async ({ page }) => {
  await installSessionsMocks(page, {
    sessions: [makeSession(1)],
    instanceAgentsEnabled: false,
  })

  await page.goto("/dashboard#/sessions")

  await expect(page.getByRole("link", { name: "Sessions" })).toHaveCount(0)
  await expect(page.getByRole("link", { name: "Monitor" })).toHaveCount(0)
  await expect(page.getByRole("link", { name: "Agent Contract" })).toHaveCount(0)
  await expect(page.getByTestId("sessions-disabled-state")).toContainText("Sessions disabled")
  await expect(page.getByTestId("sessions-disabled-state")).toContainText("Instance agent controls are disabled")
  await expect(page.getByLabel("Search")).toBeDisabled()
  await expect(page.getByLabel("Sort")).toBeDisabled()
  await expect(page.getByLabel("Page size")).toBeDisabled()
  await expect(page.getByRole("button", { name: "Prev" })).toBeDisabled()
  await expect(page.getByRole("button", { name: "Next" })).toBeDisabled()
  await expect(page.getByTestId("sessions-table")).toHaveCount(0)
  await expect(page.getByTestId("sessions-message-stream")).toHaveCount(0)
})
