import { expect, test, type Page, type Route } from "@playwright/test"

type SandboxStatus = {
  agent_id: string
  running: boolean
  status: string
  container_name: string
  container_id: string
  image: string
  workspace_path: string
  volume_name: string
}

type SandboxImage = {
  id: string
  repository: string
  tag: string
  size: string
}

type SandboxVolume = {
  name: string
  driver: string
  mountpoint: string
}

type SandboxMockState = {
  status: SandboxStatus
  images: SandboxImage[]
  volumes: SandboxVolume[]
  createCalls: number
  stopCalls: number
  resetCalls: number
  pullCalls: string[]
  deleteCalls: string[]
  imagesCalls: number
  volumesCalls: number
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({
    status,
    contentType: "application/json; charset=utf-8",
    body: JSON.stringify(body),
  })
}

async function installSandboxMocks(page: Page): Promise<SandboxMockState> {
  const state: SandboxMockState = {
    status: {
      agent_id: "default",
      running: true,
      status: "running",
      container_name: "openclawssy_agent_default",
      container_id: "abc123def4567890",
      image: "ubuntu:24.04",
      workspace_path: "/workspace",
      volume_name: "openclawssy_ws_default",
    },
    images: [
      { id: "sha256:abc123def456", repository: "ubuntu", tag: "24.04", size: "78MB" },
      { id: "sha256:def789ghi012", repository: "python", tag: "3.12-slim", size: "145MB" },
    ],
    volumes: [
      {
        name: "openclawssy_ws_default",
        driver: "local",
        mountpoint: "/var/lib/docker/volumes/openclawssy_ws_default/_data",
      },
    ],
    createCalls: 0,
    stopCalls: 0,
    resetCalls: 0,
    pullCalls: [],
    deleteCalls: [],
    imagesCalls: 0,
    volumesCalls: 0,
  }

  await page.route("**/*", async (route) => {
    const request = route.request()
    const method = request.method()
    const url = new URL(request.url())
    const { pathname } = url

    if (pathname === "/api/admin/status") {
      await json(route, {
        ok: true,
        model: { provider: "hatz", name: "glm-4.5" },
        run_count: 2,
        runtime: {
          sandbox: { active: true, provider: "docker" },
          shell: { enable_exec: true },
        },
      })
      return
    }

    if (pathname === "/api/admin/control-plane/features" && method === "GET") {
      await json(route, {
        features: {
          instance_control: true,
          instance_agents: true,
          wizard: true,
          eval: true,
        },
      })
      return
    }

    if (pathname === "/api/admin/config" && method === "PATCH") {
      const payload = request.postDataJSON() as Record<string, any>
      const sandbox = payload.sandbox || {}
      const shell = payload.shell || {}
      state.status.status = sandbox.active ? state.status.status : "stopped"
      await json(route, {
        ok: true,
        sandbox,
        shell,
      })
      return
    }

    if (pathname === "/api/admin/sandbox/docker/status" && method === "GET") {
      await json(route, state.status)
      return
    }

    if (pathname === "/api/admin/sandbox/docker/create" && method === "POST") {
      state.createCalls += 1
      state.status.running = true
      state.status.status = "running"
      state.status.container_id = "create1234567890"
      await json(route, { ok: true })
      return
    }

    if (pathname === "/api/admin/sandbox/docker/stop" && method === "POST") {
      state.stopCalls += 1
      state.status.running = false
      state.status.status = "exited"
      await json(route, { ok: true })
      return
    }

    if (pathname === "/api/admin/sandbox/docker/reset" && method === "POST") {
      state.resetCalls += 1
      state.status.running = true
      state.status.status = "running"
      state.status.container_id = "reset1234567890"
      await json(route, { ok: true })
      return
    }

    if (pathname === "/api/admin/sandbox/docker/pull" && method === "POST") {
      const payload = request.postDataJSON() as Record<string, unknown>
      const image = String(payload.image || "").trim()
      state.pullCalls.push(image)
      if (image) {
        const [repo, tag = "latest"] = image.split(":")
        state.images.push({
          id: `sha256:pull${state.pullCalls.length}`,
          repository: repo || image,
          tag,
          size: "99MB",
        })
      }
      await json(route, { ok: true, image })
      return
    }

    if (pathname === "/api/admin/sandbox/docker/images" && method === "GET") {
      state.imagesCalls += 1
      await json(route, { images: state.images })
      return
    }

    if (pathname === "/api/admin/sandbox/docker/volumes" && method === "GET") {
      state.volumesCalls += 1
      await json(route, { volumes: state.volumes })
      return
    }

    if (pathname === "/api/admin/sandbox/docker/volume" && method === "DELETE") {
      const payload = request.postDataJSON() as Record<string, unknown>
      const name = String(payload.name || "").trim()
      state.deleteCalls.push(name)
      state.volumes = state.volumes.filter((item) => item.name !== name)
      await json(route, { ok: true })
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

async function gotoSandbox(page: Page) {
  await page.goto("/dashboard#/sandbox")
  await expect(page.getByRole("heading", { name: "Sandbox", level: 2 })).toBeVisible()
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("openclawssy.dashboard.bearer", "e2e-token")
  })
})

test("shows container status metadata and running badge", async ({ page }) => {
  await installSandboxMocks(page)
  await gotoSandbox(page)

  await expect(page.getByText("Container status")).toBeVisible()
  await expect(page.getByText("openclawssy_agent_default")).toBeVisible()
  await expect(page.getByRole("row", { name: /Container ID/ })).toContainText("abc123def456")
  await expect(page.getByRole("row", { name: /Status/ })).toContainText("running")
  await expect(page.getByRole("row", { name: /Image/ })).toContainText("ubuntu:24.04")
  await expect(page.getByRole("row", { name: /Workspace path/ })).toContainText("/workspace")
  await expect(page.getByRole("row", { name: /Volume name/ })).toContainText("openclawssy_ws_default")
  await expect(page.getByTestId("sandbox-running-badge")).toHaveText("running")
})

test("create, stop, and reset container actions work with reset confirmation", async ({ page }) => {
  const state = await installSandboxMocks(page)
  await gotoSandbox(page)

  await page.getByRole("button", { name: "Create container" }).click()
  await expect.poll(() => state.createCalls).toBe(1)
  await expect(page.getByText("Create container succeeded.")).toBeVisible()

  await page.getByRole("button", { name: "Stop container" }).click()
  await expect.poll(() => state.stopCalls).toBe(1)
  await expect(page.getByText("Stop container succeeded.")).toBeVisible()
  await expect(page.getByTestId("sandbox-running-badge")).toHaveText("stopped")

  page.once("dialog", (dialog) => {
    expect(dialog.message()).toContain("recreate the container")
    dialog.accept()
  })
  await page.getByRole("button", { name: "Reset container" }).click()
  await expect.poll(() => state.resetCalls).toBe(1)
  await expect(page.getByText("Reset container succeeded.")).toBeVisible()
})

test("workspace mode selector saves local/docker mode and shows runtime summary", async ({ page }) => {
  await installSandboxMocks(page)
  await gotoSandbox(page)

  await expect(page.getByTestId("sandbox-mode-summary")).toContainText("provider `docker`")
  await page.getByTestId("sandbox-mode-select").selectOption("local")
  await expect(page.getByText("Saved sandbox mode: local.")).toBeVisible()
})

test("pull image and refresh image table", async ({ page }) => {
  const state = await installSandboxMocks(page)
  await gotoSandbox(page)

  await expect(page.getByRole("table", { name: "Available images" })).toContainText("ubuntu:24.04")
  const initialImageCalls = state.imagesCalls

  await page.getByLabel("Image name").fill("alpine:3.19")
  await page.getByRole("button", { name: "Pull image" }).click()

  await expect.poll(() => state.pullCalls).toContain("alpine:3.19")
  await expect(page.getByText("Pulled image: alpine:3.19")).toBeVisible()
  await expect(page.getByRole("table", { name: "Available images" })).toContainText("alpine:3.19")
  await expect(page.getByRole("table", { name: "Available images" })).toContainText("99MB")

  await page.getByRole("button", { name: "Refresh images" }).click()
  await expect.poll(() => state.imagesCalls).toBeGreaterThan(initialImageCalls)
})

test("volume delete requires confirmation and refreshes table", async ({ page }) => {
  const state = await installSandboxMocks(page)
  await gotoSandbox(page)

  const volumesTable = page.getByRole("table", { name: "Docker volumes" })
  await expect(volumesTable).toContainText("openclawssy_ws_default")
  const initialVolumeCalls = state.volumesCalls

  page.once("dialog", (dialog) => dialog.dismiss())
  await page.getByRole("button", { name: "Delete volume openclawssy_ws_default" }).click()
  await expect.poll(() => state.deleteCalls.length).toBe(0)

  page.once("dialog", (dialog) => {
    expect(dialog.message()).toContain("Delete volume")
    dialog.accept()
  })
  await page.getByRole("button", { name: "Delete volume openclawssy_ws_default" }).click()
  await expect.poll(() => state.deleteCalls).toContain("openclawssy_ws_default")
  await expect(page.getByText("Deleted volume: openclawssy_ws_default")).toBeVisible()

  await page.getByRole("button", { name: "Refresh volumes" }).click()
  await expect.poll(() => state.volumesCalls).toBeGreaterThan(initialVolumeCalls)
  await expect(page.getByText("No volumes found.")).toBeVisible()
  await expect(volumesTable).toHaveCount(0)
})

test("advanced mount configuration section is collapsible", async ({ page }) => {
  await installSandboxMocks(page)
  await gotoSandbox(page)

  await expect(page.getByText("Mount configuration is display-only in this release.")).not.toBeVisible()
  await page.getByRole("button", { name: "Advanced mount configuration" }).click()
  await expect(page.getByText("Mount configuration is display-only in this release.")).toBeVisible()
})

test("shows error states for docker API failures", async ({ page }) => {
  await installSandboxMocks(page)

  await page.route("**/api/admin/sandbox/docker/status**", async (route) => {
    await json(route, { error: { message: "docker daemon unavailable" } }, 500)
  })
  await page.route("**/api/admin/sandbox/docker/images**", async (route) => {
    await json(route, { error: { message: "image listing failed" } }, 500)
  })
  await page.route("**/api/admin/sandbox/docker/volumes**", async (route) => {
    await json(route, { error: { message: "volume listing failed" } }, 500)
  })

  await gotoSandbox(page)

  await expect(page.getByText("Failed to load status", { exact: false })).toBeVisible()
  await expect(page.getByText("Failed to load images", { exact: false })).toBeVisible()
  await expect(page.getByText("Failed to load volumes", { exact: false })).toBeVisible()
})
