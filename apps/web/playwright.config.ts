import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  timeout: 30_000,
  use: {
    baseURL: "http://127.0.0.1:15173",
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
  },
  webServer: [
    {
      command: "python3 ../../scripts/e2e-api.py",
      url: "http://127.0.0.1:13001/health",
      reuseExistingServer: false,
      timeout: 120_000,
    },
    {
      command: "pnpm dev --port 15173",
      url: "http://127.0.0.1:15173",
      env: { API_TARGET: "http://127.0.0.1:13001" },
      reuseExistingServer: false,
      timeout: 60_000,
    },
  ],
});
