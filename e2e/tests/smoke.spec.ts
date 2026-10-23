import { test, expect } from "@playwright/test";
import { login } from "./support/login";
import { DISPENSED, waitingFor } from "./support/queue";

test("seeded admin sees every zone", async ({ page }) => {
  await login(page);
  // Scoped to the zone rail (aria-label="Zones" in packages/ui/src/hms-shell.tsx):
  // the dashboard (apps/shell/app/page.tsx) also renders a card per
  // visible zone, and for this admin every zone is visible there too —
  // its Lab card's accessible name ("Lab Orders, samples & results")
  // contains "Lab" as a substring, so an unscoped query is ambiguous
  // under Playwright's default substring name matching even though both
  // surfaces now apply the same visibleZones(can) permission filter.
  const zoneNav = page.getByRole("navigation", { name: "Zones" });
  for (const zone of ["MediCore", "PharmaConnect", "Lab"]) {
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
  await page.goto(waitingFor(patient));
  await expect(page.getByText(patient).first()).toBeVisible({
    timeout: 30_000,
  });
  const row = page.locator("li", { hasText: patient });
  await row.getByLabel("Medication").fill("Amoxicillin 250mg");
  await row.getByRole("button", { name: "Dispense" }).click();
  // Dispensing moves the row from Waiting to the Dispensed tab (#1295).
  await expect(page.locator("li", { hasText: patient })).toHaveCount(0);
  await page.goto(DISPENSED);
  await expect(
    page.locator("li", { hasText: patient }).getByText("Amoxicillin 250mg"),
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
