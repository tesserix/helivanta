import { defineConfig } from "@playwright/test";

// Public, non-creating checks against the deployed service; no local servers.
export default defineConfig({
  testDir: "./tests",
  testMatch: ["customer-signup.spec.ts", "login-security-key-live.spec.ts"],
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: "list",
  use: { baseURL: "https://helivanta.app", headless: true },
});
