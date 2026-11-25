import { expect, test } from "@playwright/test";
import { newPharmacyId, type PharmacySale, type PharmacyCreditNote } from "@helivanta/api";
import { choose, owner, readyOutlet } from "./support/pharmacy-counter";

test.use({ ignoreHTTPSErrors: true });

test("a Chandigarh seller posts local UTGST and interstate IGST with original-tax credits", async ({
  page,
  browser,
}) => {
  test.setTimeout(180_000);
  const fixture = await readyOutlet(page, browser);
  const { cashier, outlet, sku } = fixture;
  const today = new Intl.DateTimeFormat("en-CA", { timeZone: fixture.timeZone }).format(new Date());
  try {
    await owner(page, "PUT", `${outlet}/tax-registrations/${newPharmacyId()}`, {
      jurisdiction_code: "04",
      identifier: "04PQRST5678K2Z9",
      effective_from: today,
      reason: "TEST ONLY synthetic Chandigarh seller for all-India acceptance",
    });
    for (const recipient of ["Chandigarh", "Maharashtra"]) {
      const local = recipient === "Chandigarh";
      await cashier.goto("/pharmacy/counter");
      await choose(cashier, "Product", new RegExp(sku));
      await cashier.getByLabel("Units", { exact: true }).fill("10");
      await cashier.getByRole("button", { name: "Add to basket", exact: true }).click();
      await choose(cashier, "Recipient state", recipient);
      await cashier.getByRole("button", { name: "Price basket", exact: true }).click();
      await expect(
        cashier
          .locator("dt")
          .filter({ hasText: local ? /^UTGST$/ : /^IGST$/ })
          .locator("+ dd"),
      ).toHaveText(local ? "INR 6.70" : "INR 13.39");
      const posted = cashier.waitForResponse(
        (r) => r.request().method() === "PUT" && r.url().includes(`${outlet}/sales/`),
      );
      await cashier.getByRole("button", { name: "Post sale", exact: true }).click();
      const response = await posted;
      expect(response.status(), await response.text()).toBe(201);
      const sale = (await response.json()) as PharmacySale;
      expect(sale.sgst_minor).toBe(0);
      expect(sale.utgst_minor).toBe(local ? 670 : 0);
      expect(sale.cgst_minor).toBe(local ? 670 : 0);
      expect(sale.igst_minor).toBe(local ? 0 : 1339);
      await expect(
        cashier.getByText(`Place of supply ${recipient}`, { exact: false }),
      ).toBeVisible();
      await cashier.goto("/pharmacy/returns");
      await cashier
        .getByRole("button", { name: `Return from ${sale.invoice_number}`, exact: true })
        .click();
      await cashier.getByLabel("Units returned on line 1", { exact: true }).fill("10");
      await choose(cashier, "Quarantine location", /TEST-Q/);
      await cashier.getByLabel("Refunded to", { exact: true }).fill("TEST Synthetic Buyer");
      await cashier.getByLabel("Their address", { exact: true }).fill(`TEST ONLY ${recipient}`);
      await cashier
        .getByLabel("Reason", { exact: true })
        .fill("TEST synthetic full original-tax return");
      const issued = cashier.waitForResponse(
        (r) => r.request().method() === "PUT" && r.url().includes(`${outlet}/credit-notes/`),
      );
      await cashier.getByRole("button", { name: "Issue credit note", exact: true }).click();
      const creditResponse = await issued;
      expect(creditResponse.status(), await creditResponse.text()).toBe(201);
      const credit = (await creditResponse.json()) as PharmacyCreditNote;
      expect(credit.total_minor).toBe(sale.total_minor);
      expect(credit.utgst_minor).toBe(sale.utgst_minor);
      expect(credit.igst_minor).toBe(sale.igst_minor);
      expect(credit.sgst_minor).toBe(0);
      await expect(
        cashier.getByRole("heading", {
          name: `Credit note ${credit.credit_note_number}`,
          exact: true,
        }),
      ).toBeVisible();
    }
    const report = await owner<{
      days: {
        sales_utgst_minor: number;
        credits_utgst_minor: number;
        sales_igst_minor: number;
        credits_igst_minor: number;
      }[];
    }>(cashier, "GET", `${outlet}/reports/daily?from=${today}&to=${today}`);
    expect(report.days).toHaveLength(1);
    expect(report.days[0]).toMatchObject({
      sales_utgst_minor: 670,
      credits_utgst_minor: 670,
      sales_igst_minor: 1339,
      credits_igst_minor: 1339,
    });
  } finally {
    await fixture.context.close();
  }
});

test("an Odisha recipient gets an IGST invoice and a full original-tax credit", async ({
  page,
  browser,
}) => {
  test.setTimeout(150_000);
  const fixture = await readyOutlet(page, browser);
  const { cashier, outlet, sku } = fixture;
  try {
    await cashier.goto("/pharmacy/counter");
    await choose(cashier, "Product", new RegExp(sku));
    await cashier.getByLabel("Units", { exact: true }).fill("10");
    await cashier.getByRole("button", { name: "Add to basket", exact: true }).click();
    await choose(cashier, "Recipient state", "Odisha");
    await cashier.getByRole("button", { name: "Price basket", exact: true }).click();
    await expect(cashier.locator("dt").filter({ hasText: /^IGST$/ })).toBeVisible();
    await expect(cashier.locator("dd").filter({ hasText: /^INR 13.39$/ })).toBeVisible();
    const posted = cashier.waitForResponse(
      (r) => r.request().method() === "PUT" && r.url().includes(`${outlet}/sales/`),
    );
    await cashier.getByRole("button", { name: "Post sale", exact: true }).click();
    const response = await posted;
    expect(response.status()).toBe(201);
    const sale = (await response.json()) as PharmacySale;
    expect(sale.place_of_supply).toBe("21");
    expect(sale.igst_minor).toBe(1339);
    expect(sale.cgst_minor).toBe(0);
    expect(sale.sgst_minor).toBe(0);
    await expect(cashier.getByText(/Place of supply Odisha/)).toBeVisible();
    await cashier.goto("/pharmacy/returns");
    await cashier
      .getByRole("button", { name: `Return from ${sale.invoice_number}`, exact: true })
      .click();
    await cashier.getByLabel("Units returned on line 1", { exact: true }).fill("10");
    await choose(cashier, "Quarantine location", /TEST-Q/);
    await cashier.getByLabel("Refunded to", { exact: true }).fill("TEST Interstate Buyer");
    await cashier.getByLabel("Their address", { exact: true }).fill("TEST Bhubaneswar, Odisha");
    await cashier.getByLabel("Reason", { exact: true }).fill("TEST synthetic full return");
    const issued = cashier.waitForResponse(
      (r) => r.request().method() === "PUT" && r.url().includes(`${outlet}/credit-notes/`),
    );
    await cashier.getByRole("button", { name: "Issue credit note", exact: true }).click();
    const creditResponse = await issued;
    expect(creditResponse.status()).toBe(201);
    const credit = (await creditResponse.json()) as PharmacyCreditNote;
    expect(credit.igst_minor).toBe(sale.igst_minor);
    expect(credit.total_minor).toBe(sale.total_minor);
    expect(credit.cgst_minor).toBe(0);
    expect(credit.sgst_minor).toBe(0);
    await expect(
      cashier.getByRole("heading", {
        name: `Credit note ${credit.credit_note_number}`,
        exact: true,
      }),
    ).toBeVisible();
    const today = new Intl.DateTimeFormat("en-CA", { timeZone: fixture.timeZone }).format(
      new Date(),
    );
    const report = await owner<{
      days: { sales_igst_minor: number; credits_igst_minor: number }[];
    }>(cashier, "GET", `${outlet}/reports/daily?from=${today}&to=${today}`);
    expect(report.days).toHaveLength(1);
    expect(report.days[0]?.sales_igst_minor).toBe(1339);
    expect(report.days[0]?.credits_igst_minor).toBe(1339);
  } finally {
    await fixture.context.close();
  }
});
