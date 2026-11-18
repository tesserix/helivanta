import { defineConfig } from "@playwright/test";

const baseURL = process.env.HELIVANTA_ERROR_BROWSER_URL;
if (!baseURL || !["127.0.0.1", "localhost", "[::1]"].includes(new URL(baseURL).hostname)) {
  throw new Error("Frontend error fixtures require an explicit loopback production-Shell URL.");
}
if (!process.env.HELIVANTA_ERROR_WEB_LOG) {
  throw new Error("Frontend error acceptance requires the real Shell log file.");
}

export default defineConfig({
  testDir: "./frontend-errors",
  forbidOnly: true,
  retries: 0,
  workers: 1,
  timeout: 30_000,
  use: { baseURL, browserName: "chromium", trace: "retain-on-failure" },
});
