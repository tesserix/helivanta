import { readFileSync } from "node:fs";
import { expect, test, type Locator, type Page } from "@playwright/test";
import type { PharmacyCreditNote, PharmacySale } from "@helivanta/api";
import { choose, owner, readyOutlet } from "./support/pharmacy-counter";
import { affiliatedPatient } from "./support/pharmacy-reports";
import { uuidv7 } from "./support/pharmacy";

// #1060: a pharmacist's sales read back the same on every report and in every
// file, and each report section shows its empty, invalid, failed and refused
// states (spec 2026-11-05-pharmacy-report-walkthrough-design.md).

const refused = "You don't have access to this outlet's reports.";
const inr = (minor: number) => `INR ${(minor / 100).toFixed(2)}`;
const rupees = (minor: number) => (minor / 100).toFixed(2);

async function sellCash(
  page: Page,
  outlet: string,
  sku: string,
  units: number,
  patient?: { hospitalId: string; mrn: string; name: string },
) {
  await page.goto("/pharmacy/counter");
  await choose(page, "Product", new RegExp(sku));
  await page.getByLabel("Units", { exact: true }).fill(String(units));
  await page.getByRole("button", { name: "Add to basket", exact: true }).click();
  await page.getByRole("button", { name: "Price basket", exact: true }).click();
  if (patient) {
    await choose(page, "Customer", "Registered patient");
    await choose(page, "Hospital", patient.hospitalId);
    await page.getByLabel("MRN", { exact: true }).fill(patient.mrn);
    await page.getByRole("button", { name: "Find patient", exact: true }).click();
    await expect(page.getByText(patient.name, { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "This is the patient", exact: true }).click();
  }
  await choose(page, "Paid by", "Cash");
  const posted = page.waitForResponse(
    (r) => r.request().method() === "PUT" && r.url().includes(`${outlet}/sales/`),
  );
  await page.getByRole("button", { name: "Post sale", exact: true }).click();
  const response = await posted;
  expect(response.status(), await response.text()).toBe(201);
  const sale = (await response.json()) as PharmacySale;
  await expect(
    page.getByRole("heading", { name: `Tax invoice ${sale.invoice_number}`, exact: true }),
  ).toBeVisible();
  return sale;
}

async function returnOneForCash(page: Page, outlet: string, invoice: string) {
  await page.goto("/pharmacy/returns");
  await page.getByRole("button", { name: `Return from ${invoice}`, exact: true }).click();
  await page.getByLabel("Units returned on line 1", { exact: true }).fill("1");
  await choose(page, "Quarantine location", /TEST-Q/);
  await page.getByLabel("Refunded to", { exact: true }).fill("TEST Counter Buyer");
  await page.getByLabel("Their address", { exact: true }).fill("TEST Pune address");
  await page.getByLabel("Reason", { exact: true }).fill("TEST one unopened unit returned");
  await choose(page, "Refund by", "Cash");
  const issued = page.waitForResponse(
    (r) => r.request().method() === "PUT" && r.url().includes(`${outlet}/credit-notes/`),
  );
  await page.getByRole("button", { name: "Issue credit note", exact: true }).click();
  const response = await issued;
  expect(response.status(), await response.text()).toBe(201);
  return (await response.json()) as PharmacyCreditNote;
}

/** RFC 4180 records of a file csvsafe wrote: byte-order mark, CRLF, quoted fields. */
function parseCsv(text: string): Record<string, string>[] {
  expect(text.charCodeAt(0), "the file starts with a byte-order mark").toBe(0xfeff);
  const records: string[][] = [];
  let field = "";
  let record: string[] = [];
  let quoted = false;
  for (let i = 1; i < text.length; i++) {
    const c = text[i]!;
    if (quoted) {
      if (c === '"' && text[i + 1] === '"') {
        field += '"';
        i++;
      } else if (c === '"') quoted = false;
      else field += c;
    } else if (c === '"') quoted = true;
    else if (c === ",") {
      record.push(field);
      field = "";
    } else if (c === "\r" && text[i + 1] === "\n") {
      record.push(field);
      records.push(record);
      field = "";
      record = [];
      i++;
    } else field += c;
  }
  expect(field, "the file ends with CRLF").toBe("");
  const [header, ...rows] = records;
  return rows.map((r) => Object.fromEntries(header!.map((h, i) => [h, r[i] ?? ""])));
}

/** The file a section's download button saves, with the server's own counts. */
async function download(page: Page, within: Locator, name: string) {
  const answered = page.waitForResponse((r) => r.url().includes(".csv?"));
  const saved = page.waitForEvent("download");
  await within.getByRole("button", { name: "Download report CSV", exact: true }).first().click();
  const [response, file] = await Promise.all([answered, saved]);
  expect(response.status()).toBe(200);
  const rows = parseCsv(readFileSync((await file.path())!, "utf8"));
  expect(Number(response.headers()["x-export-row-count"])).toBe(rows.length);
  expect(file.suggestedFilename()).toMatch(
    new RegExp(`^pharmacy-${name}-[0-9a-f-]{36}-\\d{4}-\\d{2}-\\d{2}-\\d{4}-\\d{2}-\\d{2}\\.csv$`),
  );
  return rows;
}

const region = (page: Page, name: string) => page.getByRole("region", { name, exact: true });

async function showPeriod(page: Page, from: string, to: string) {
  await page.getByLabel("From", { exact: true }).fill(from);
  await page.getByLabel("To", { exact: true }).fill(to);
  await page.getByRole("button", { name: "Show report", exact: true }).click();
}

test("a pharmacist's sales read back the same on every report and file", async ({
  page,
  browser,
}) => {
  test.setTimeout(240_000);
  const fixture = await readyOutlet(page, browser);
  const { cashier, outlet, product, sku, stamp } = fixture;
  try {
    const patient = await affiliatedPatient(page, cashier, outlet);
    // A product's category takes effect tomorrow at the earliest, so today's
    // sales sit in none: choosing one narrows them to nothing.
    const category = uuidv7();
    await owner(page, "PUT", `${outlet}/catalogue/categories/${category}`, {
      code: `TEST-CAT-${stamp}`,
      name: "TEST Report category",
    });
    const today = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Kolkata" }).format(new Date());

    const walkIn = await sellCash(cashier, outlet, sku, 3);
    const toPatient = await sellCash(cashier, outlet, sku, 2, patient);
    const credit = await returnOneForCash(cashier, outlet, walkIn.invoice_number);
    const sold = walkIn.total_minor + toPatient.total_minor;
    expect(credit.total_minor).toBeGreaterThan(0);

    await cashier.goto("/pharmacy/daily-report");
    const days = cashier.getByRole("table", { name: "Daily sales" });
    const day = days.getByRole("row", { name: new RegExp(today) });
    await expect(day).toContainText(`2 for ${inr(sold)}`);
    await expect(day).toContainText(`1 for ${inr(credit.total_minor)}`);
    await expect(day).toContainText(inr(sold - credit.total_minor));
    await expect(day).toContainText(`Cash ${inr(sold)}`);
    await expect(day).toContainText(`Cash ${inr(credit.total_minor)}`);
    const dailyFile = await download(cashier, cashier.locator("main"), "daily");
    expect(dailyFile).toHaveLength((await days.getByRole("row").count()) - 1);
    const [daily] = dailyFile;
    expect(daily).toMatchObject({
      day: today,
      sales: "2",
      sales_amount: rupees(sold),
      credit_notes: "1",
      credits: rupees(credit.total_minor),
      net: rupees(sold - credit.total_minor),
      cash: rupees(sold),
      cash_refunds: rupees(credit.total_minor),
      units_sold: "5",
      units_returned: "1",
    });

    const items = region(cashier, "Sales by item");
    const item = items.getByRole("row", { name: /TEST Counter Tablets/ });
    await expect(item).toContainText(`5 units for ${inr(sold)}`);
    await expect(item).toContainText(`1 units for ${inr(credit.total_minor)}`);
    const itemFiles = await download(cashier, items, "items");
    expect(itemFiles).toHaveLength(
      (await items.getByRole("table", { name: "Sales by item" }).getByRole("row").count()) - 1,
    );
    const [itemFile] = itemFiles;
    expect(itemFile).toMatchObject({
      product_id: product,
      units_sold: "5",
      sales: rupees(sold),
      units_returned: "1",
      credits: rupees(credit.total_minor),
      net_units: "4",
      net: rupees(sold - credit.total_minor),
    });
    await items.getByLabel("Lot number", { exact: true }).fill(`TEST-LOT-${stamp}`);
    const byLot = cashier.waitForResponse(
      (r) => r.url().includes("/reports/items?") && r.url().includes("lot_number="),
    );
    await items.getByRole("button", { name: "Apply lot", exact: true }).click();
    expect((await byLot).status()).toBe(200);
    await expect(item).toContainText(`5 units for ${inr(sold)}`);
    await items.getByRole("combobox", { name: "Category", exact: true }).click();
    await cashier.getByRole("option", { name: /TEST Report category/ }).click();
    await expect(items.getByRole("status").filter({ hasText: /^No items sold/ })).toBeVisible();
    await expect(item).toHaveCount(0);

    const receipts = region(cashier, "Receipts and refunds");
    await expect(receipts.getByRole("row")).toHaveCount(4);
    for (const doc of [walkIn, toPatient])
      await expect(
        receipts.getByRole("row", { name: new RegExp(doc.invoice_number) }),
      ).toContainText(inr(doc.total_minor));
    await expect(
      receipts.getByRole("row", { name: new RegExp(credit.credit_note_number) }),
    ).toContainText(inr(credit.total_minor));
    const receiptFile = await download(cashier, receipts, "receipts");
    expect(receiptFile.map((r) => [r.direction, r.document_number, r.amount]).sort()).toEqual(
      [
        ["receipt", walkIn.invoice_number, rupees(walkIn.total_minor)],
        ["receipt", toPatient.invoice_number, rupees(toPatient.total_minor)],
        ["refund", credit.credit_note_number, rupees(credit.total_minor)],
      ].sort(),
    );

    const patientSales = region(cashier, "Patient-wise sales");
    await patientSales.getByRole("combobox", { name: "Hospital", exact: true }).click();
    await cashier.getByRole("option", { name: patient.hospitalId, exact: true }).click();
    await patientSales.getByLabel("MRN", { exact: true }).fill(patient.mrn);
    await patientSales.getByRole("button", { name: "Find patient", exact: true }).click();
    await patientSales.getByRole("button", { name: "This is the patient", exact: true }).click();
    const lines = patientSales.getByRole("table", { name: `Purchases by ${patient.name}` });
    await expect(lines.getByRole("row")).toHaveCount(2);
    await expect(lines).toContainText(toPatient.invoice_number);
    await expect(lines).not.toContainText(walkIn.invoice_number);
    const patientFile = await download(cashier, patientSales, "patient-sales");
    expect(patientFile).toHaveLength(1);
    expect(patientFile[0]).toMatchObject({
      patient_id: patient.patientId,
      invoice_number: toPatient.invoice_number,
      quantity_units: "2",
      amount: rupees(toPatient.total_minor),
    });

    const stock = region(cashier, "Sales and stock");
    await expect(stock.getByRole("row", { name: /TEST Counter Tablets/ })).toBeVisible();
    const stockFiles = await download(cashier, stock, "stock-sales");
    expect(stockFiles).toHaveLength(
      (await stock.getByRole("table", { name: "Sales and stock" }).getByRole("row").count()) - 1,
    );
    const [stockFile] = stockFiles;
    // 20 received and shelved, 5 sold, 1 returned to quarantine.
    expect(stockFile).toMatchObject({
      product_id: product,
      units_sold: "5",
      units_returned: "1",
      on_hand_units: "16",
      sellable_units: "15",
    });

    await cashier.goto("/pharmacy/sales");
    const sales = cashier.getByRole("list", { name: "Sales" });
    await expect(sales.getByRole("listitem")).toHaveCount(2);
    await expect(sales.getByRole("listitem").first()).toContainText(toPatient.invoice_number);
    await expect(sales.getByRole("listitem").nth(1)).toContainText(
      `Credited ${inr(credit.total_minor)}, net ${inr(walkIn.total_minor - credit.total_minor)}`,
    );
    const worklistFile = await download(cashier, cashier.locator("main"), "sales-worklist");
    expect(worklistFile.map((r) => [r.invoice_number, r.customer, r.total, r.net])).toEqual([
      [
        toPatient.invoice_number,
        "patient",
        rupees(toPatient.total_minor),
        rupees(toPatient.total_minor),
      ],
      [
        walkIn.invoice_number,
        "walk_in",
        rupees(walkIn.total_minor),
        rupees(walkIn.total_minor - credit.total_minor),
      ],
    ]);
    await choose(cashier, "Sold to", "Walk-in");
    await cashier.getByRole("button", { name: "Show sales", exact: true }).click();
    await expect(sales.getByRole("listitem")).toHaveCount(1);
    await expect(sales).toContainText(walkIn.invoice_number);
    await choose(cashier, "Sold to", "Everyone");
    await cashier.getByLabel("Invoice number", { exact: true }).fill(walkIn.invoice_number);
    const byInvoice = cashier.waitForResponse(
      (r) => r.url().includes("/sales/worklist?") && r.url().includes("invoice="),
    );
    await cashier.getByRole("button", { name: "Show sales", exact: true }).click();
    expect((await byInvoice).status()).toBe(200);
    await expect(sales.getByRole("listitem")).toHaveCount(1);
    await expect(cashier.getByRole("button", { name: "Download report CSV" })).toHaveCount(0);
    await cashier
      .getByRole("button", { name: `Open invoice ${walkIn.invoice_number}`, exact: true })
      .click();
    await expect(cashier.getByRole("region", { name: "Credit notes" })).toContainText(
      credit.credit_note_number,
    );
  } finally {
    await fixture.context.close();
  }
});

test("each report section shows its empty, invalid, failed and refused states", async ({
  page,
  browser,
}) => {
  test.setTimeout(240_000);
  const fixture = await readyOutlet(page, browser);
  const { cashier, outlet, sku, subject } = fixture;
  try {
    const sale = await sellCash(cashier, outlet, sku, 1);
    const today = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Kolkata" }).format(new Date());
    await cashier.goto("/pharmacy/daily-report");
    const receipts = region(cashier, "Receipts and refunds");
    await expect(receipts).toContainText(sale.invoice_number);

    // Invalid periods are refused in the browser; no report is asked for.
    const asked: string[] = [];
    cashier.on("request", (r) => r.url().includes("/reports/") && asked.push(r.url()));
    await showPeriod(cashier, today, "2020-01-01");
    await expect(cashier.getByText("The last day is on or after the first")).toBeVisible();
    await showPeriod(cashier, "2020-01-01", today);
    await expect(cashier.getByText("A period is at most 366 days")).toBeVisible();
    expect(asked).toEqual([]);

    // A past period with nothing in it: every section says so, announced.
    await showPeriod(cashier, "2026-01-01", "2026-01-31");
    for (const text of [
      "No sales or returns from 2026-01-01 to 2026-01-31.",
      "No items sold or returned from 2026-01-01 to 2026-01-31.",
      "No receipts or refunds from 2026-01-01 to 2026-01-31.",
    ])
      await expect(cashier.getByRole("status").filter({ hasText: text })).toBeVisible();
    await expect(cashier.getByText(sale.invoice_number)).toHaveCount(0);

    // One section's request fails while the others answer: only it says so,
    // the sale it showed for the last period is gone, and its retry recovers it.
    const day = new Date(`${today}T00:00:00Z`);
    day.setUTCDate(day.getUTCDate() - 1);
    const yesterday = day.toISOString().slice(0, 10);
    await showPeriod(cashier, today, today);
    await expect(receipts).toContainText(sale.invoice_number);
    await cashier.route("**/reports/receipts?**", (route) => route.abort());
    await showPeriod(cashier, yesterday, today);
    await expect(receipts.getByRole("alert")).toContainText("Could not load receipts and refunds.");
    await expect(receipts.getByText(sale.invoice_number)).toHaveCount(0);
    await expect(
      region(cashier, "Sales by item").getByRole("row", { name: /TEST Counter Tablets/ }),
    ).toBeVisible();
    await expect(
      cashier
        .getByRole("table", { name: "Daily sales" })
        .getByRole("row", { name: new RegExp(today) }),
    ).toBeVisible();
    await cashier.unroute("**/reports/receipts?**");
    await receipts.getByRole("button", { name: "Try again", exact: true }).click();
    await expect(receipts).toContainText(sale.invoice_number);

    // The filters follow the caller's own grants, as the API enforces them.
    const me = await owner<{ data: string[] }>(cashier, "GET", "/iam/me/permissions");
    const stockRead = me.data.includes("pharmacy.stock.read");
    await expect(cashier.getByRole("combobox", { name: "Prescriber", exact: true })).toHaveCount(
      me.data.includes("pharmacy.prescription.read") ? 1 : 0,
    );
    await expect(cashier.getByRole("heading", { name: "Sales and stock" })).toHaveCount(
      stockRead ? 1 : 0,
    );

    // Access withdrawn while the page is open: every section answers with the
    // refusal, none offers a retry, and none keeps a row.
    // A period this page has never read, so no section answers from the
    // browser's five-second cache of an earlier read.
    day.setUTCDate(day.getUTCDate() - 1);
    const twoDaysAgo = day.toISOString().slice(0, 10);
    await owner(page, "DELETE", `${outlet}/access/${encodeURIComponent(subject)}`);
    await showPeriod(cashier, twoDaysAgo, today);
    const sections = [
      "Sales by item",
      "Receipts and refunds",
      ...(stockRead ? ["Sales and stock"] : []),
    ];
    for (const name of sections)
      await expect(region(cashier, name).getByText(refused).first()).toBeVisible();
    // The daily totals sit outside the named sections.
    await expect
      .poll(() => cashier.getByText(refused, { exact: true }).count())
      .toBeGreaterThan(sections.length);
    await expect(cashier.getByRole("table", { name: "Daily sales" })).toHaveCount(0);
    await expect(cashier.getByRole("button", { name: "Try again" })).toHaveCount(0);
    await expect(cashier.getByText(sale.invoice_number)).toHaveCount(0);
  } finally {
    await fixture.context.close();
  }
});
