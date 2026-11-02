import { expect, test } from "@playwright/test";
import type {
  PharmacyCounterPayment,
  PharmacyCreditNote,
  PharmacySale,
  PharmacyStockPosition,
} from "@helivanta/api";
import { choose, collectUpi, owner, provider, readyCounter } from "./support/pharmacy-counter";
import { uuidv7 } from "./support/pharmacy";

// Only this synthetic-provider spec trusts its ephemeral loopback certificate.
test.use({ ignoreHTTPSErrors: true });

test("a pharmacist uses verified UPI once, cannot switch customer, and returns stock to quarantine", async ({
  page,
  browser,
}) => {
  test.setTimeout(150_000);
  const fixture = await readyCounter(page, browser);
  const { cashier, outlet, product, sku, quarantine, subject } = fixture;
  try {
    const qr = await collectUpi(cashier, sku);
    const posted = cashier.waitForResponse(
      (r) => r.request().method() === "PUT" && r.url().includes(`${outlet}/sales/`),
      { timeout: 30_000 },
    );
    await cashier.getByRole("button", { name: "Post sale", exact: true }).click();
    const response = await posted;
    expect(response.status()).toBe(201);
    const sale = (await response.json()) as PharmacySale;
    expect(sale.total_minor).toBe(2500);
    expect(sale.actor_subject).toBe(subject);
    expect(sale.lines).toHaveLength(1);
    expect(sale.lines[0]!.quantity_units).toBe(2);
    expect(sale.tenders[0]!.kind).toBe("upi_verified");
    const paymentId = sale.tenders[0]!.payment_request_id!;
    expect(paymentId).toMatch(/^[0-9a-f-]{36}$/);
    await expect(
      cashier.getByRole("heading", { name: `Tax invoice ${sale.invoice_number}`, exact: true }),
    ).toBeVisible();
    const retained = await owner<PharmacySale>(cashier, "GET", `${outlet}/sales/${sale.id}`);
    expect(retained.invoice_number).toBe(sale.invoice_number);
    const payment = await owner<PharmacyCounterPayment>(
      cashier,
      "GET",
      `${outlet}/counter-payments/${paymentId}`,
    );
    expect(payment.status).toBe("paid");
    expect(payment.amount_minor).toBe(2500);
    expect(
      (
        await owner<{ sellable_units: number }>(
          cashier,
          "GET",
          `${outlet}/sales/availability?product_id=${product}`,
        )
      ).sellable_units,
    ).toBe(18);
    expect(
      (
        await owner<{ data: PharmacyCounterPayment[] }>(
          cashier,
          "GET",
          `${outlet}/counter-payments`,
        )
      ).data,
    ).toHaveLength(0);

    await cashier.goto("/pharmacy/returns");
    await cashier.reload();
    await cashier
      .getByRole("button", { name: `Return from ${sale.invoice_number}`, exact: true })
      .click();
    await cashier.getByLabel("Units returned on line 1", { exact: true }).fill("1");
    await choose(cashier, "Quarantine location", /TEST-Q/);
    await cashier.getByLabel("Refunded to", { exact: true }).fill("TEST Counter Buyer");
    await cashier.getByLabel("Their address", { exact: true }).fill("TEST Pune address");
    await cashier.getByLabel("Reason", { exact: true }).fill("TEST one unopened unit returned");
    await choose(cashier, "Refund by", "UPI");
    // The existing return command records an externally completed refund; it
    // never claims to execute a provider refund. Simulate that external act.
    const refund = await provider(cashier, "refund", { qr_id: qr, amount_minor: 1250 });
    await cashier.getByLabel("Refund reference", { exact: true }).fill(refund.id);
    const issued = cashier.waitForResponse(
      (r) => r.request().method() === "PUT" && r.url().includes(`${outlet}/credit-notes/`),
    );
    await cashier.getByRole("button", { name: "Issue credit note", exact: true }).click();
    const creditResponse = await issued;
    expect(creditResponse.status()).toBe(201);
    const credit = (await creditResponse.json()) as PharmacyCreditNote;
    expect(credit.sale_id).toBe(sale.id);
    expect(credit.invoice_number).toBe(sale.invoice_number);
    expect(credit.total_minor).toBe(1250);
    expect(credit.actor_subject).toBe(subject);
    expect(credit.lines[0]!.quantity_units).toBe(1);
    expect(credit.refunds[0]).toMatchObject({
      kind: "upi",
      amount_minor: 1250,
      reference: refund.id,
    });
    await expect(
      cashier.getByRole("heading", {
        name: `Credit note ${credit.credit_note_number}`,
        exact: true,
      }),
    ).toBeVisible();
    const replay = await owner<PharmacyCreditNote>(
      cashier,
      "PUT",
      `${outlet}/credit-notes/${credit.id}`,
      creditResponse.request().postDataJSON(),
    );
    expect(replay.id).toBe(credit.id);
    expect(replay.credit_note_number).toBe(credit.credit_note_number);
    const positions = await owner<{ data: PharmacyStockPosition[] }>(
      cashier,
      "GET",
      `${outlet}/stock/positions?product_id=${product}&balance=on_hand&as_of=${encodeURIComponent(new Date().toISOString())}`,
    );
    expect(positions.data.find((p) => p.location_id === quarantine)?.on_hand_units).toBe(1);
    expect(
      (
        await owner<{ sellable_units: number }>(
          cashier,
          "GET",
          `${outlet}/sales/availability?product_id=${product}`,
        )
      ).sellable_units,
    ).toBe(18);
    expect(
      (await owner<PharmacySale>(cashier, "GET", `${outlet}/sales/${sale.id}`)).lines[0]!
        .returned_units,
    ).toBe(1);
  } finally {
    await fixture.context.close();
  }
});

test("an unused paid UPI request is retained, externally refunded and cannot fund a sale", async ({
  page,
  browser,
}) => {
  test.setTimeout(150_000);
  const fixture = await readyCounter(page, browser);
  const { cashier, outlet, product, sku, subject } = fixture;
  try {
    const qr = await collectUpi(cashier, sku);
    const unused = await owner<{ data: PharmacyCounterPayment[] }>(
      cashier,
      "GET",
      `${outlet}/counter-payments`,
    );
    expect(unused.data).toHaveLength(1);
    const paymentId = unused.data[0]!.id;
    const external = await provider(cashier, "refund", { qr_id: qr, amount_minor: 2500 });
    await cashier.getByLabel("Completed refund reference", { exact: true }).fill(external.id);
    await cashier
      .getByLabel("Refund reason", { exact: true })
      .fill("TEST customer cancelled before a sale");
    await cashier
      .getByRole("button", { name: "Confirm the full refund was completed", exact: true })
      .click();
    await expect(cashier.getByText(/The completed external refund was recorded/)).toBeVisible();
    await expect(cashier.getByRole("combobox", { name: "Customer", exact: true })).toBeEnabled();
    await choose(cashier, "Customer", "Registered patient");
    await expect(cashier.getByRole("combobox", { name: "Customer", exact: true })).toContainText(
      "Registered patient",
    );
    const receipt = await owner<{ amount_minor: number; reference: string; recorded_by: string }>(
      cashier,
      "GET",
      `${outlet}/counter-payments/${paymentId}/refund`,
    );
    expect(receipt).toMatchObject({
      amount_minor: 2500,
      reference: external.id,
      recorded_by: subject,
    });
    expect(
      (
        await owner<PharmacyCounterPayment>(
          cashier,
          "GET",
          `${outlet}/counter-payments/${paymentId}`,
        )
      ).status,
    ).toBe("paid");
    expect(
      (
        await owner<{ data: PharmacyCounterPayment[] }>(
          cashier,
          "GET",
          `${outlet}/counter-payments`,
        )
      ).data,
    ).toHaveLength(0);
    const refused = await cashier.request.put(`/api/v1${outlet}/sales/${uuidv7()}`, {
      data: {
        lines: [{ product_id: product, quantity_units: 2 }],
        tenders: [{ kind: "upi_verified", amount_minor: 2500, payment_request_id: paymentId }],
      },
    });
    expect(refused.status()).toBe(409);
    expect((await refused.json()).error).toBe("not_sellable");
    expect(
      (
        await owner<{ sellable_units: number }>(
          cashier,
          "GET",
          `${outlet}/sales/availability?product_id=${product}`,
        )
      ).sellable_units,
    ).toBe(20);
    expect(
      (await owner<{ data: PharmacySale[] }>(cashier, "GET", `${outlet}/sales`)).data,
    ).toHaveLength(0);
    await cashier.reload();
    expect(
      (
        await owner<{ reference: string }>(
          cashier,
          "GET",
          `${outlet}/counter-payments/${paymentId}/refund`,
        )
      ).reference,
    ).toBe(external.id);
  } finally {
    await fixture.context.close();
  }
});
