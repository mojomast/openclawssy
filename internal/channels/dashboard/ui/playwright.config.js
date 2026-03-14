import { defineConfig, devices } from "@playwright/test";
import { existsSync } from "node:fs";

const playwrightFallbackLibDir = "/home/mojo/.cache/playwright-libs/root/usr/lib/x86_64-linux-gnu";

function prependEnvPath(pathToPrepend, currentValue) {
  const existing = String(currentValue || "")
    .split(":")
    .map((item) => item.trim())
    .filter(Boolean);
  if (existing.includes(pathToPrepend)) {
    return existing.join(":");
  }
  return [pathToPrepend, ...existing].join(":");
}

if (existsSync(playwrightFallbackLibDir)) {
  process.env.LD_LIBRARY_PATH = prependEnvPath(playwrightFallbackLibDir, process.env.LD_LIBRARY_PATH);
}

export default defineConfig({
  testDir: "./tests/e2e",
  timeout: 45000,
  expect: {
    timeout: 7000,
  },
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: "http://127.0.0.1:4173",
    trace: "on-first-retry",
  },
  webServer: {
    command: "node ./tests/e2e/server.mjs",
    url: "http://127.0.0.1:4173/dashboard",
    reuseExistingServer: !process.env.CI,
    timeout: 30000,
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
