import { expect, test } from "@playwright/test";
import { login, specAdmin } from "./support/login";

// Switching forgets the remembered account and ends the session, so the
// landing offers a fresh sign-in (whose card asks which account when one
// password opens several, #1751 D3) and the emailed-code finder.
test("switch account ends the source session and returns to a fresh sign-in", async ({ page }) => {
  await login(page, specAdmin());
  await page.getByRole("button", { name: "Switch account", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "Switch account?" })).toBeVisible();
  await page.getByRole("button", { name: "Sign out and switch" }).click();
  await expect(page.getByRole("button", { name: "Sign in", exact: true })).toBeVisible({
    timeout: 30_000,
  });
  await expect(page.getByRole("link", { name: "Find my accounts by email" })).toBeVisible();
  await expect(page.getByLabel("Workspace ID")).toHaveCount(0);
  const probe = await page.request.get("/api/v1/iam/me/permissions");
  expect(probe.status()).toBe(401);
});

test("cancelling account switching preserves the current authorised session", async ({ page }) => {
  await login(page, specAdmin());
  await page.getByRole("button", { name: "Switch account", exact: true }).click();
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).toBeVisible();
  const probe = await page.request.get("/api/v1/iam/me/permissions");
  expect(probe.status()).toBe(200);
});
