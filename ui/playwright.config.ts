import { defineConfig, devices } from "@playwright/test";

// End-to-end tests against the production build (`pnpm build:e2e`, which
// points the browser at the mock API) served by `next start`, with the mock
// API serving test/fixtures/api. Chromium only, a desktop and a phone project.
// Never run on the development VM: CI runs them (ui-e2e.yml).

const API_PORT = 8089;
const APP_PORT = 3100;
const CI = !!process.env.CI;

export default defineConfig({
  testDir: "e2e",
  fullyParallel: true,
  forbidOnly: CI,
  retries: CI ? 1 : 0,
  reporter: CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: `http://127.0.0.1:${APP_PORT}`,
    trace: "retain-on-failure",
  },
  projects: [
    {
      name: "desktop",
      use: { ...devices["Desktop Chrome"], viewport: { width: 1280, height: 800 } },
    },
    {
      name: "phone",
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 390, height: 844 },
        deviceScaleFactor: 3,
        isMobile: true,
        hasTouch: true,
      },
    },
  ],
  webServer: [
    {
      command: `node test/mock-api/server.mjs ${API_PORT}`,
      url: `http://127.0.0.1:${API_PORT}/api/v1/status`,
      reuseExistingServer: !CI,
    },
    {
      command: `pnpm start --hostname 127.0.0.1 --port ${APP_PORT}`,
      url: `http://127.0.0.1:${APP_PORT}`,
      env: { NEXT_PUBLIC_API_URL: `http://127.0.0.1:${API_PORT}/api`, API_URL: `http://127.0.0.1:${API_PORT}/api` },
      reuseExistingServer: !CI,
      timeout: 60_000,
    },
  ],
});
