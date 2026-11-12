import { expect, test, type Page } from "@playwright/test";
import { api, freshOutlet, uuidv7 } from "./support/pharmacy";

async function choose(page: Page, label: string, option: string | RegExp) {
  await page.getByRole("combobox", { name: label, exact: true }).click();
  await page.getByRole("option", { name: option, exact: typeof option === "string" }).click();
}
test("receipt invoice recovery, partial payment, credit and reversal retain one financial lineage", async ({
  page,
}) => {
  test.setTimeout(150_000);
  const { outlet, stamp } = await freshOutlet(page);
  const product = uuidv7(),
    batch = uuidv7(),
    location = uuidv7(),
    receipt = uuidv7();
  await api(page, "PUT", `${outlet}/catalogue/products/${product}`, {
    expected_version: 0,
    sku: `TEST-FIN-${stamp}`,
    brand_name: "TEST financial handoff item",
    generic_name: "TEST synthetic item",
    dosage_form: "tablet",
    route: "oral",
    manufacturer: "TEST Manufacturer",
    marketing_company: "",
    ingredients: [{ name: "TEST synthetic item", strength: "1", unit: "mg" }],
    pack_size: 10,
    unit_of_issue: "tablet",
    currency: "INR",
    mrp_minor: 12500,
    status: "active",
  });
  await api(page, "PUT", `${outlet}/stock/locations/${location}`, {
    code: "TEST-FIN-Q",
    name: "TEST financial quarantine",
  });
  await api(page, "PUT", `${outlet}/stock/batches/${batch}`, {
    product_id: product,
    lot_number: `TEST-FIN-${stamp}`,
    expires_on: "2099-12-31",
    mrp_minor: 12500,
  });
  await api(page, "PUT", `${outlet}/stock/movements/${receipt}`, {
    location_id: location,
    batch_id: batch,
    kind: "receipt",
    quantity_units: 20,
    reason: "TEST original physical acquisition",
    source_reference: `TEST-GRN-${stamp}`,
  });
  await page.goto("/pharmacy/accounts?tab=all");
  await page.getByRole("button", { name: "Record bill or expense" }).click();
  const invoice = `TEST-FIN-INVOICE-${stamp}`;
  await page.getByLabel("Payee name", { exact: true }).fill("TEST External Supplier");
  await page.getByLabel("Payee reference", { exact: true }).fill("TEST-EXTERNAL-SUPPLIER");
  await page.getByLabel("Document reference", { exact: true }).fill(invoice);
  await page.getByLabel("Document date", { exact: true }).fill("2026-11-09");
  await page.getByLabel("Due date", { exact: true }).fill("2026-11-10");
  await choose(page, "Category 1", "Inventory purchase");
  await choose(page, "Inventory receipt", new RegExp(`TEST-GRN-${stamp}`));
  await page
    .getByLabel("Description 1", { exact: true })
    .fill("TEST original supplier invoice acquisition");
  await page.getByLabel("Net amount 1 (INR)", { exact: true }).fill("100.00");
  await page.getByLabel("Tax 1 (INR)", { exact: true }).fill("18.00");
  let bill: string | undefined;
  let writes = 0;
  await page.route("**/accounts/payables/*", async (route) => {
    if (route.request().method() !== "PUT") {
      await route.continue();
      return;
    }
    writes++;
    const response = await route.fetch();
    expect(response.status()).toBe(200);
    bill = ((await response.json()) as { id: string }).id;
    await route.abort("failed");
  });
  await page.getByRole("button", { name: "Save bill or expense", exact: true }).click();
  await expect(page.getByText(/The bill outcome is uncertain/)).toBeVisible();
  await expect(page.getByLabel("Document reference", { exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Recover original bill", exact: true }).click();
  await expect(page.getByRole("region", { name: `Bill ${invoice}`, exact: true })).toBeVisible();
  expect(writes).toBe(1);
  await page.unroute("**/accounts/payables/*");
  expect(bill).toBeTruthy();
  const billRoot = `${outlet}/accounts/payables/${bill}`;
  const original = await api(page, "GET", billRoot);
  expect(original.total_minor).toBe(11800);
  expect((original.lines as { receipt_id: string }[])[0]!.receipt_id).toBe(receipt);
  const payment = `TEST-FIN-PAYMENT-${stamp}`;
  async function settle(kind: string, reference: string, amount: string, originalPayment?: RegExp) {
    await choose(page, "Record type", kind);
    await page
      .getByLabel(kind === "Record payment" ? "Paid by" : "Responsible person", { exact: true })
      .fill("TEST Finance operator");
    await page.getByLabel("Settlement reference", { exact: true }).fill(reference);
    await page.getByLabel("Settlement date", { exact: true }).fill("2026-11-09");
    if (originalPayment) await choose(page, "Original payment", originalPayment);
    await page.getByLabel("Settlement amount (INR)", { exact: true }).fill(amount);
    await page
      .getByLabel("Reason / evidence note", { exact: true })
      .fill("TEST external evidence only; no money transfer");
    const saved = page.waitForResponse(
      (r) => r.request().method() === "PUT" && r.url().includes(`${billRoot}/`),
    );
    await page.getByRole("button", { name: "Save settlement", exact: true }).click();
    expect((await saved).status()).toBe(200);
    await expect(page.getByText(new RegExp(reference)).first()).toBeVisible();
  }
  await settle("Record payment", payment, "30.00");
  expect((await api(page, "GET", billRoot)).outstanding_minor).toBe(8800);
  await settle("Credit note", `TEST-FIN-CREDIT-${stamp}`, "8.00");
  expect((await api(page, "GET", billRoot)).outstanding_minor).toBe(8000);
  await settle(
    "Reverse recorded payment",
    `TEST-FIN-REVERSAL-${stamp}`,
    "30.00",
    new RegExp(payment),
  );
  const final = await api(page, "GET", billRoot);
  expect(final.total_minor).toBe(11800);
  expect(final.paid_minor).toBe(0);
  expect(final.credit_minor).toBe(800);
  expect(final.outstanding_minor).toBe(11000);
  const history = await api(page, "GET", `${billRoot}/settlements`);
  expect(history.data as unknown[]).toHaveLength(3);
  await page.reload();
  await expect(page.getByRole("region", { name: `Bill ${invoice}`, exact: true })).toBeVisible();
});
