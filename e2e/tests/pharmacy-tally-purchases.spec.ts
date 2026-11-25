import { readFile } from "node:fs/promises";
import { expect, test } from "@playwright/test";
import { addBill, api, freshOutlet } from "./support/pharmacy";
import { choose } from "./support/pharmacy-counter";

test("source purchase evidence produces a repeatable Tally voucher", async ({ page }) => {
  test.setTimeout(150000);
  const { outlet, stamp } = await freshOutlet(page);
  const bill = await addBill(page, outlet, `TEST-TALLY-${stamp}`);
  const id = String(bill.id);
  await page.goto(`/pharmacy/accounts?tab=all&open=${id}`);
  await expect(page.getByText(/No purchase tax evidence recorded/)).toBeVisible();
  await page.getByLabel("Supplier GSTIN (leave blank if unregistered)").fill("07ABCDE1234F1Z2");
  await choose(page, "Supplier state / territory", "Delhi");
  await choose(page, "Place of supply on invoice", "Delhi");
  await page
    .getByLabel("Source / correction reason")
    .fill("TEST source invoice evidence; not a legal ITC decision");
  await page.getByLabel("GST rate (%)", { exact: true }).fill("18");
  await page.getByLabel("CGST (INR)", { exact: true }).fill("9.00");
  await page.getByLabel("SGST (INR)", { exact: true }).fill("9.00");
  await page.getByLabel("Recoverable CGST (INR)", { exact: true }).fill("9.00");
  await page.getByLabel("Recoverable SGST (INR)", { exact: true }).fill("9.00");
  const commands: string[] = [];
  await page.route("**/accounts/payables/*/purchase-tax/*", async (route) => {
    if (route.request().method() !== "PUT") {
      await route.continue();
      return;
    }
    commands.push(route.request().url());
    if (commands.length === 1) {
      const committed = await route.fetch();
      expect(committed.status()).toBe(200);
      await route.abort("failed");
    } else await route.continue();
  });
  await page.getByRole("button", { name: "Record purchase tax evidence", exact: true }).click();
  await expect(page.getByText(/Could not record purchase tax evidence/)).toBeVisible();
  await page.getByRole("button", { name: "Try again", exact: true }).click();
  await expect(page.getByRole("button", { name: "Record corrected evidence" })).toBeVisible();
  expect(commands).toHaveLength(2);
  expect(new Set(commands).size).toBe(1);
  await page.unroute("**/accounts/payables/*/purchase-tax/*");
  const source = await api(page, "GET", `${outlet}/accounts/payables/${id}`);
  expect(source.total_minor).toBe(11800);
  const retained = await api(page, "GET", `${outlet}/accounts/payables/${id}/purchase-tax`);
  expect(retained.evidence).toMatchObject({
    supplier_state: "07",
    place_of_supply: "07",
    lines: [{ cgst_minor: 900, sgst_minor: 900 }],
  });

  await page.goto("/pharmacy/accounts?tab=tally");
  await page.getByLabel("From", { exact: true }).fill("2026-01-01");
  await page.getByLabel("To", { exact: true }).fill("2026-01-31");
  await page.getByRole("button", { name: "Preview period", exact: true }).click();
  await expect(
    page.getByText("Supplier purchase bills", { exact: true }).locator(".."),
  ).toContainText("1");
  const names = {
    "Voucher type for supplier purchases": "Purchase",
    "Utilities purchases taxable at 18%": "TEST Utilities",
    "Recoverable input CGST on 18% supplies": "TEST Input CGST",
    "Recoverable input SGST on 18% supplies": "TEST Input SGST",
    "Supplier TEST-SUPPLIER (TEST Supplier)": "TEST Supplier Ledger",
  };
  for (const [label, name] of Object.entries(names))
    await page.getByLabel(label, { exact: true }).fill(name);
  await page.getByRole("button", { name: "Save ledger names", exact: true }).click();
  const download = async () => {
    const pending = page.waitForEvent("download");
    await page.getByRole("button", { name: "Download Tally XML", exact: true }).click();
    const file = await pending;
    const path = await file.path();
    expect(path).toBeTruthy();
    return readFile(path!, "utf8");
  };
  const xml = await download();
  expect(xml).toContain("<VOUCHERTYPENAME>Purchase</VOUCHERTYPENAME>");
  expect(xml).toContain(`<VOUCHERNUMBER>TEST-TALLY-${stamp}</VOUCHERNUMBER>`);
  expect(xml).toContain("<AMOUNT>-100.00</AMOUNT>");
  expect(xml.match(/<AMOUNT>-9.00<\/AMOUNT>/g)).toHaveLength(2);
  expect(xml).toContain("<AMOUNT>118.00</AMOUNT>");
  expect(await download()).toBe(xml);
});
