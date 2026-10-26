import { expect, test, type Page } from "@playwright/test";
import { api, expectList, freshOutlet, uuidv7 } from "./support/pharmacy";

// #1303 E3/E4: a stock position opens beside the register, and Receiving
// shows the chosen lot beside its chooser. The URL names both, so a reload
// keeps them.

async function stocked(page: Page, lots = 3) {
  const { outlet, stamp } = await freshOutlet(page);
  const product = uuidv7();
  await api(page, "PUT", `${outlet}/catalogue/products/${product}`, {
    expected_version: 0,
    sku: `TEST-STK-${stamp}`,
    brand_name: `TEST Stock ${stamp}`,
    generic_name: "TEST paracetamol",
    dosage_form: "tablet",
    route: "oral",
    manufacturer: "TEST Manufacturer",
    marketing_company: "",
    ingredients: [{ name: "TEST paracetamol", strength: "500", unit: "mg" }],
    pack_size: 10,
    unit_of_issue: "tablet",
    currency: "INR",
    mrp_minor: 10500,
    status: "active",
  });
  const location = uuidv7();
  await api(page, "PUT", `${outlet}/stock/locations/${location}`, {
    code: "TEST-QA",
    name: "TEST Quarantine A",
  });
  for (let n = 1; n <= lots; n++) {
    const batch = uuidv7();
    await api(page, "PUT", `${outlet}/stock/batches/${batch}`, {
      product_id: product,
      lot_number: `TEST-LOT-${stamp}-${n}`,
      expires_on: `2031-0${n}-28`,
    });
    await api(page, "PUT", `${outlet}/stock/movements/${uuidv7()}`, {
      location_id: location,
      batch_id: batch,
      kind: "receipt",
      quantity_units: 10 * n,
      reason: "TEST received for the pane",
      source_reference: `TEST-GRN-${stamp}-${n}`,
    });
  }
  return { stamp, location, product };
}

for (const [label, size, rail] of [
  ["1366 rail expanded", { width: 1366, height: 768 }, "expanded"],
  ["1920", { width: 1920, height: 1080 }, "collapsed"],
] as const) {
  test(`a stock position opens beside the register at ${label}`, async ({ page }) => {
    await page.setViewportSize(size);
    const { stamp } = await stocked(page);
    await page.goto("/pharmacy/stock");
    if (rail === "expanded") await page.getByRole("button", { name: "Expand navigation" }).click();
    const inspects = page.getByRole("button", {
      name: new RegExp(`^Inspect TEST-STK-${stamp} lot`),
    });
    await expect(inspects).toHaveCount(3);
    const [first, second] = [
      (await inspects.nth(0).getAttribute("aria-label"))!,
      (await inspects.nth(1).getAttribute("aria-label"))!,
    ];
    const lotOf = (name: string) => name.match(/lot (\S+) at/)![1];
    const button = page.getByRole("button", { name: first, exact: true });
    const row = button.locator("xpath=ancestor::tr");
    // Bring the row into view first: a click scrolls to its target, and that
    // scroll is Playwright's, not the page moving the row.
    await button.scrollIntoViewIfNeeded();
    await expect(page.getByText(/loaded stock positions/)).toBeVisible();
    const scrolled = () => page.locator("[data-hms-scroll]").evaluate((node) => node.scrollTop);
    const before = { top: await scrolled(), y: (await row.boundingBox())!.y };
    await button.click();
    const pane = page.getByRole("region", { name: new RegExp(`${lotOf(first)} · TEST-QA$`) });
    await expect(pane).toBeVisible();
    await expect(pane.getByText("Closing stock", { exact: true })).toBeVisible();
    const list = (await row.boundingBox())!;
    expect((await pane.boundingBox())!.x).toBeGreaterThanOrEqual(list.x + list.width);
    expect(await scrolled()).toBe(before.top);
    test
      .info()
      .annotations.push({ type: "row shift", description: `${Math.round(list.y - before.y)}px` });
    expect(Math.abs(list.y - before.y)).toBeLessThanOrEqual(2);

    await page.getByRole("button", { name: "Next position" }).click();
    const next = page.getByRole("region", { name: new RegExp(`${lotOf(second)} · TEST-QA$`) });
    await expect(next).toBeVisible();
    await page.reload();
    await expect(next).toBeVisible();
  });
}

test.describe("on a phone", () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test("a position takes the screen, and Back returns to the register", async ({ page }) => {
    const { stamp } = await stocked(page, 1);
    await page.goto("/pharmacy/stock");
    await page.getByRole("button", { name: new RegExp(`^Inspect TEST-STK-${stamp} lot`) }).click();
    await expect(page.getByRole("region", { name: /^Stock reconciliation / })).toBeVisible();
    await expect(page.getByRole("table")).toBeHidden();
    await page.goBack();
    await expectList(page, "/pharmacy/stock", {});
    await expect(page.getByRole("table")).toBeVisible();
  });
});

test("receiving keeps the chosen lot through a reload", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const { stamp } = await stocked(page, 1);
  await page.goto("/pharmacy/receiving");
  await page.getByRole("combobox", { name: "Quarantine location" }).click();
  await page.getByRole("option", { name: /^TEST-QA ·/ }).click();
  await page.getByRole("combobox", { name: "Product" }).click();
  await page.getByRole("option", { name: `TEST-STK-${stamp} · TEST Stock ${stamp}` }).click();
  await page.getByRole("combobox", { name: "Manufacturer lot" }).click();
  await page.getByRole("option", { name: new RegExp(`^TEST-LOT-${stamp}-1`) }).click();
  const pane = page.getByRole("region", { name: `Lot TEST-LOT-${stamp}-1 at TEST-QA` });
  await expect(pane).toBeVisible();
  await expect(pane.getByText("TEST-GRN-" + stamp + "-1")).toBeVisible();
  await page.reload();
  await expect(pane).toBeVisible();
});
