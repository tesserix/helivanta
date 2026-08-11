import { test, expect } from "@playwright/test";
import { login } from "./support/login";

test("seeded admin sees every zone", async ({ page }) => {
  await login(page);
  // Scoped to the zone rail (aria-label="Zones" in packages/ui/src/hms-shell.tsx):
  // the dashboard also renders an unfiltered card per zone with an
  // overlapping accessible name (e.g. "Lab Orders, samples & results"
  // contains "Lab"), so an unscoped query is ambiguous under Playwright's
  // default substring name matching.
  const zoneNav = page.getByRole("navigation", { name: "Zones" });
  for (const zone of ["MediCore", "Pharmacy", "Lab"]) {
    await expect(zoneNav.getByRole("link", { name: zone })).toBeVisible();
  }
});

test("login, OPD visit, pharmacy dispense, lab result", async ({ page }) => {
  const patient = `E2E Patient ${Date.now()}`;

  // Unauthenticated → redirected to login.
  await login(page);

  // Shell → medicore (hard navigation across the zone boundary).
  // The department card's accessible name includes its description text
  // ("OPD Outpatient registration & appointments"), so match by href instead
  // of an exact "OPD" name.
  await page.locator('a[href="/medicore/opd"]').first().click();
  await expect(page).toHaveURL(/\/medicore\/opd$/);
  // The medicore zone is a separate Next.js app reached via a hard
  // navigation; wait for its JS to finish loading/hydrating before
  // interacting, otherwise a click can race hydration and fall back to a
  // native (unhandled) form submit with stale/empty state.
  await page.waitForLoadState("networkidle");

  // Create the OPD visit.
  await page.getByLabel("Patient name").fill(patient);
  await page.getByRole("button", { name: "Create visit" }).click();
  await expect(page.getByText(patient).first()).toBeVisible();

  // Pharmacy zone: visit_created fans out asynchronously; the page
  // polls every 3s, so just wait for the patient to appear.
  await page.goto("/pharmacy");
  await expect(page.getByText(patient).first()).toBeVisible({
    timeout: 30_000,
  });
  await page
    .locator("li", { hasText: patient })
    .getByRole("button", { name: "Dispense" })
    .click();
  await expect(
    page.locator("li", { hasText: patient }).getByText(/Dispensed/),
  ).toBeVisible();

  // Lab zone: pending order for the same visit; record a result.
  await page.goto("/lab");
  await expect(page.getByText(patient).first()).toBeVisible({
    timeout: 30_000,
  });
  const orderRow = page.locator("li", { hasText: patient });
  await orderRow.getByLabel(`Result for ${patient}`).fill("WBC 6.1");
  await orderRow.getByRole("button", { name: "Save result" }).click();
  await expect(orderRow.getByText("Result: WBC 6.1")).toBeVisible();
});
