import { defineConfig } from "@playwright/test";
const origin = process.env.HELIVANTA_BROWSER_ORIGIN;
if (!origin || !["localhost", "127.0.0.1"].includes(new URL(origin).hostname))
  throw new Error("HELIVANTA_BROWSER_ORIGIN must be a local running shell");
export default defineConfig({
  testDir: "./emergency-receiving",
  forbidOnly: true,
  retries: 0,
  timeout: 120000,
  use: { baseURL: origin, viewport: { width: 390, height: 844 } },
  reporter: "list",
});
