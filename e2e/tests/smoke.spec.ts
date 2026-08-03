import { test, expect } from "@playwright/test";

test("login, dashboard, cross-zone OPD ping", async ({ page }) => {
  // Unauthenticated → redirected to login.
  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/);

  await page.getByLabel("Email").fill("test@hms.dev");
  await page.getByLabel("Password").fill("password123");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Departments" })).toBeVisible();

  // Cross the zone boundary: shell → medicore (hard navigation).
  await page.getByRole("link", { name: "OPD", exact: true }).first().click();
  await expect(page).toHaveURL(/\/medicore\/opd$/);
  await expect(page.getByRole("heading", { name: /OPD/ })).toBeVisible();

  // Round-trip through the Go API with the session cookie.
  await page.getByRole("button", { name: /Ping from OPD/ }).click();
  await expect(page.getByText("OPD ping").first()).toBeVisible();
});
