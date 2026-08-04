import { test, expect } from "@playwright/test";

test("login, OPD visit, pharmacy dispense, lab result", async ({ page }) => {
  const patient = `E2E Patient ${Date.now()}`;

  // Unauthenticated → redirected to login.
  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/);

  await page.getByLabel("Email").fill("test@hms.dev");
  await page.getByLabel("Password").fill("password123");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Departments" })).toBeVisible();

  // Shell → medicore (hard navigation across the zone boundary).
  await page.getByRole("link", { name: "OPD", exact: true }).first().click();
  await expect(page).toHaveURL(/\/medicore\/opd$/);

  // Create the OPD visit.
  await page.getByLabel("Patient name").fill(patient);
  await page.getByRole("button", { name: "Create visit" }).click();
  await expect(page.getByText(patient).first()).toBeVisible();

  // Pharmacy zone: visit_created fans out asynchronously; the page
  // polls every 3s, so just wait for the patient to appear.
  await page.goto("/pharmacy");
  await expect(page.getByText(patient).first()).toBeVisible({ timeout: 30_000 });
  await page
    .locator("li", { hasText: patient })
    .getByRole("button", { name: "Dispense" })
    .click();
  await expect(page.locator("li", { hasText: patient }).getByText(/Dispensed/)).toBeVisible();

  // Lab zone: pending order for the same visit; record a result.
  await page.goto("/lab");
  await expect(page.getByText(patient).first()).toBeVisible({ timeout: 30_000 });
  const orderRow = page.locator("li", { hasText: patient });
  await orderRow.getByLabel(`Result for ${patient}`).fill("WBC 6.1");
  await orderRow.getByRole("button", { name: "Save result" }).click();
  await expect(orderRow.getByText("Result: WBC 6.1")).toBeVisible();
});
