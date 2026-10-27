import { expect, test, type Page } from "@playwright/test";
import { api, expectLabelsInNames, expectList, freshOutlet, uuidv7 } from "./support/pharmacy";

// #1303 E5: a concern opens its batch beside the list, and two concerns about
// one lot are one stop for Next. The URL holds the batch, so a reload keeps it.

async function concerns(page: Page) {
  const { outlet, stamp } = await freshOutlet(page);
  const product = uuidv7();
  await api(page, "PUT", `${outlet}/catalogue/products/${product}`, {
    expected_version: 0,
    sku: `TEST-SAF-${stamp}`,
    brand_name: `TEST Safety ${stamp}`,
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
  const lots: string[] = [];
  for (const n of [1, 2]) {
    const batch = uuidv7();
    await api(page, "PUT", `${outlet}/stock/batches/${batch}`, {
      product_id: product,
      lot_number: `TEST-SLOT-${stamp}-${n}`,
      expires_on: `2031-0${n}-28`,
      mrp_minor: 12500,
    });
    lots.push(batch);
  }
  // Oldest first: the list is newest first, so lot 1's two concerns lead it.
  for (const [batch, note] of [
    [lots[1], "lot 2 seal"],
    [lots[0], "lot 1 seal"],
    [lots[0], "lot 1 carton"],
  ] as const)
    await api(page, "PUT", `${outlet}/stock/safety-incidents/${uuidv7()}`, {
      batch_id: batch,
      kind: "packaging_damage",
      reason: `TEST ${note} damaged on arrival (${stamp}).`,
      occurred_at: new Date(Date.now() - 60_000).toISOString(),
    });
  return { stamp };
}

test("a concern opens its batch beside the list, and Next skips its second concern", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const { stamp } = await concerns(page);
  await page.goto("/pharmacy/safety");
  const opens = page.getByRole("button", { name: /^Open batch for / });
  await expect(opens).toHaveCount(3);
  await opens.first().click();
  const first = page.getByRole("heading", { name: `Lot TEST-SLOT-${stamp}-1` });
  await expect(first).toBeVisible();
  await expectLabelsInNames(page);
  // The concern's own row, in the list (its text is in the pane too).
  const list = (await opens.first().locator("xpath=ancestor::li").boundingBox())!;
  const pane = (await page.getByRole("region", { name: "Batch safety" }).boundingBox())!;
  expect(pane.x).toBeGreaterThanOrEqual(list.x + list.width);

  await page.getByRole("button", { name: "Next batch" }).click();
  const second = page.getByRole("heading", { name: `Lot TEST-SLOT-${stamp}-2` });
  await expect(second).toBeVisible();
  await expect(page.getByRole("button", { name: "Next batch" })).toBeDisabled();
  await page.reload();
  await expect(second).toBeVisible();

  // The batch tab's rows, with a batch open beside them.
  await page.goto("/pharmacy/safety?tab=batch");
  await page.getByRole("button", { name: `Open lot TEST-SLOT-${stamp}-1` }).click();
  await expect(first).toBeVisible();
  await expectLabelsInNames(page);
});

test.describe("on a phone", () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test("a batch takes the screen, and Back returns to the concerns", async ({ page }) => {
    const { stamp } = await concerns(page);
    await page.goto("/pharmacy/safety?tab=batch");
    await page.getByRole("button", { name: `Open lot TEST-SLOT-${stamp}-1` }).click();
    await expect(page.getByRole("heading", { name: `Lot TEST-SLOT-${stamp}-1` })).toBeVisible();
    await expect(page.getByRole("list", { name: "Batches at this outlet" })).toBeHidden();
    await page.goBack();
    await expectList(page, "/pharmacy/safety", { tab: "batch" });
    await expect(page.getByRole("list", { name: "Batches at this outlet" })).toBeVisible();
  });
});
