import { expect, test } from "@playwright/test";
import { login, specAdmin, specPharmacist } from "./support/login";
import { uuidv7 } from "./support/pharmacy";
import { choose, owner, readyOutlet } from "./support/pharmacy-counter";
import { affiliatedPatient } from "./support/pharmacy-reports";
import { waitingFor } from "./support/queue";

// The cross-zone journey with authorization actually enforced: an admin
// creates a visit that reaches lab, and a
// differently-privileged pharmacist sees only what pharmacist holds
// permission for — in the sidebar, on the dashboard, and when navigating
// directly to a route the UI would otherwise hide the action on.

test("admin creates a visit and it lands in lab, not the retired dispense queue", async ({
  page,
}) => {
  await login(page, specAdmin());

  const patient = `Journey Patient ${Date.now()}`;
  await page.locator('a[href="/medicore/opd"]').first().click();
  await expect(page).toHaveURL(/\/medicore\/opd$/);
  await page.waitForLoadState("networkidle");

  await page.getByLabel("Patient name").fill(patient);
  await page.getByRole("button", { name: "Create visit" }).click();
  await expect(page.getByText(patient).first()).toBeVisible();

  // visit_created reaches lab through JetStream. Pharmacy no longer
  // consumes it (#1680): the free-text dispense queue it fed is retired.
  await page.goto("/lab");
  await expect(page.getByText(patient).first()).toBeVisible({
    timeout: 30_000,
  });
  await page.goto(waitingFor(patient));
  await expect(page.getByText("No waiting patient matches")).toBeVisible();
});

test("pharmacist sees only the pharmacy zone, in the sidebar and on the dashboard", async ({
  page,
}) => {
  await login(page, specPharmacist());

  // Scoped to the zone rail (aria-label="Zones") — see smoke.spec.ts for
  // why an unscoped query is ambiguous even though the dashboard's cards
  // (apps/shell/app/page.tsx) apply the same visibleZones(can) filter.
  const zoneNav = page.getByRole("navigation", { name: "Zones" });
  await expect(zoneNav.getByRole("link", { name: "PharmaConnect" })).toBeVisible();
  await expect(zoneNav.getByRole("link", { name: "MediCore" })).toHaveCount(0);
  await expect(zoneNav.getByRole("link", { name: "Lab" })).toHaveCount(0);

  // The dashboard is the first thing every user sees and must honour the
  // same "only doors that will open" contract (packages/ui/src/zones.ts)
  // as the sidebar — scoped by href since the dashboard's card titles are
  // per-page ("OPD"/"IPD"), not the zone name.
  const dashboard = page.getByRole("main");
  const shortcuts = dashboard.getByRole("navigation", { name: "Department shortcuts" });
  await expect(shortcuts.locator('a[href="/pharmacy"]')).toBeVisible();
  await expect(dashboard.locator('a[href^="/medicore"]')).toHaveCount(0);
  await expect(dashboard.locator('a[href="/lab"]')).toHaveCount(0);
});

test("pharmacist cannot create a visit even by navigating directly", async ({ page }) => {
  await login(page, specPharmacist());
  await page.goto("/medicore/opd");
  await page.waitForLoadState("networkidle");

  // The UI hides the action entirely (Can permission="medicore.visit.create"
  // in apps/shell/components/medicore/visit-panel.tsx); the API is the real
  // enforcement point, so the form must be absent rather than merely
  // disabled.
  await expect(page.getByRole("button", { name: "Create visit" })).toHaveCount(0);
});

// #1680, replacing #976's free-text dispense: a pharmacist serves an OPD
// patient with a counter sale citing the encounter admin opened, and the sale
// moves stock and is recorded as an OPD sale.
test("pharmacist sells against an OPD encounter opened by admin", async ({ page, browser }) => {
  test.setTimeout(240_000);
  const fixture = await readyOutlet(page, browser);
  const { cashier, outlet, product, sku } = fixture;
  try {
    const patient = await affiliatedPatient(page, cashier, outlet);
    const administrator = await owner<{ subject: string }>(page, "GET", "/iam/me/permissions");
    const clinical = `/medicore/hospitals/${patient.hospitalId}`;
    await owner(page, "PUT", `${clinical}/access/${encodeURIComponent(administrator.subject)}`);
    // Opening is idempotent under its id; retry until patientmaster's
    // registration has reached MediCore.
    const encounter = uuidv7();
    await expect
      .poll(
        async () =>
          (
            await page.request.put(`/api/v1${clinical}/encounters/${encounter}`, {
              data: {
                patient_id: patient.patientId,
                consult_mode: "in_person",
                visit_type: "first",
              },
            })
          ).status(),
        { timeout: 30_000, message: "the registered patient reaches MediCore" },
      )
      .toBeLessThan(300);
    await expect
      .poll(
        async () => {
          const visits = await owner<{ data: { encounter_id: string }[] }>(
            cashier,
            "GET",
            `${outlet}/opd-encounters?patient_id=${patient.patientId}&hospital_id=${patient.hospitalId}`,
          );
          return visits.data.some((v) => v.encounter_id === encounter);
        },
        { timeout: 30_000, message: "encounter_opened.v1 reaches the pharmacy" },
      )
      .toBe(true);
    const availability = async () =>
      (
        await owner<{ sellable_units: number }>(
          cashier,
          "GET",
          `${outlet}/sales/availability?product_id=${product}`,
        )
      ).sellable_units;
    const before = await availability();

    await cashier.goto("/pharmacy/counter");
    await choose(cashier, "Product", new RegExp(sku));
    await cashier.getByLabel("Units", { exact: true }).fill("2");
    await cashier.getByRole("button", { name: "Add to basket", exact: true }).click();
    await cashier.getByRole("button", { name: "Price basket", exact: true }).click();
    await choose(cashier, "Customer", "Registered patient");
    // #1637: the hospital shows by name once announced, by id until then.
    await choose(cashier, "Hospital", new RegExp(`${patient.hospitalName}|${patient.hospitalId}`));
    await cashier.getByLabel("MRN", { exact: true }).fill(patient.mrn);
    await cashier.getByRole("button", { name: "Find patient", exact: true }).click();
    await expect(cashier.getByText(patient.name, { exact: true })).toBeVisible();
    await cashier.getByRole("button", { name: "This is the patient", exact: true }).click();
    await choose(cashier, "For the OPD visit", /^Visit opened/);
    await choose(cashier, "Paid by", "Cash");
    const posted = cashier.waitForResponse(
      (r) => r.request().method() === "PUT" && r.url().includes(`${outlet}/sales/`),
    );
    await cashier.getByRole("button", { name: "Post sale", exact: true }).click();
    const response = await posted;
    expect(response.status(), await response.text()).toBe(201);
    const sale = (await response.json()) as { id: string; encounter_id: string | null };
    expect(sale.encounter_id).toBe(encounter);

    expect(await availability()).toBe(before - 2);
    const summary = await owner<{ linkage: string }>(
      cashier,
      "GET",
      `${outlet}/sales/${sale.id}/summary`,
    );
    expect(summary.linkage).toBe("opd");
  } finally {
    await fixture.context.close();
  }
});
