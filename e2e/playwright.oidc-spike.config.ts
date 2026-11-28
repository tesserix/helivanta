import { defineConfig, devices } from "@playwright/test";

// #915 spike 2: the handshake cookie must survive the identity provider's
// cross-site 302 back to Helivanta in every engine we support. It needs no
// Helivanta stack: the spec runs two local servers on different sites.
export default defineConfig({
  testDir: "./oidc-spike",
  forbidOnly: true,
  retries: 0,
  timeout: 30_000,
  reporter: "list",
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] } },
    { name: "firefox", use: { ...devices["Desktop Firefox"] } },
    { name: "webkit", use: { ...devices["Desktop Safari"] } },
  ],
});
