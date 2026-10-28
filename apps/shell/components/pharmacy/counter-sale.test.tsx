import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../test/select";
import { CounterSale } from "./counter-sale";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const productId = "0199e000-0000-7000-8000-000000000003";
const rxId = "0199e000-0000-7000-8000-000000000040";
const page = (data: unknown[]) => ({
  data,
  page: { has_more: false, next_cursor: null, total: data.length, offset: 0 },
});
const profile = {
  id: "0199e000-0000-7000-8000-000000000020",
  legal_name: "TEST Main Pharmacy LLP",
  address: "TEST 1 Market Road",
  invoice_prefix: "TS",
  recorded_by: "user-owner",
  created_at: "2026-11-01T06:30:00Z",
};
const line = {
  line_number: 1,
  product_id: productId,
  batch_id: "0199e000-0000-7000-8000-000000000005",
  location_id: "0199e000-0000-7000-8000-000000000004",
  lot_number: "TEST-LOT",
  expires_on: "2027-03-31",
  regulatory_class: "none",
  classification_code: "30049099",
  supply_nature: "taxable",
  rate_ppm: 120000,
  mrp_minor: 12500,
  pack_size: 10,
  quantity_units: 8,
  amount_minor: 10000,
  taxable_minor: 8928,
  cgst_minor: 536,
  sgst_minor: 536,
};
const totals = {
  currency: "INR",
  total_minor: 10000,
  taxable_minor: 8928,
  cgst_minor: 536,
  sgst_minor: 536,
  nppa_ceiling_checked: false,
};

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function fixture(
  options: {
    profile?: typeof profile | null;
    quote?: () => Response;
    sale?: (init: RequestInit) => Response;
    saveProfile?: (init: RequestInit) => Response;
    rxClass?: string;
    ceilingBps?: number;
    saveCeiling?: (init: RequestInit) => Response;
    saleLine?: typeof line & { prescription_id?: string; prescription_line?: number };
    /** The status every counter-payment read answers (#1386 C-4b). */
    counterPayment?: { status: string };
  } = {},
) {
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
    if (url.endsWith("/counter-payments")) return new Response(JSON.stringify(page([])));
    if (/\/counter-payments\/[0-9a-f-]{36}$/.test(url))
      return new Response(
        JSON.stringify({
          id: url.slice(-36),
          purpose: "counter_sale",
          amount_minor: 10000,
          currency: "INR",
          status: options.counterPayment?.status ?? "awaiting_payment",
          qr_image_url: "https://rzp.io/i/qr1",
          expires_at: new Date(Date.now() + 300_000).toISOString(),
          failure_reason: null,
          provider_payment_id: null,
          rrn: "123",
          paid_at: null,
          late: false,
          created_at: new Date().toISOString(),
        }),
        { status: init.method === "PUT" ? 201 : 200 },
      );
    if (url.includes("/invoice-profile/") && init.method === "PUT")
      return options.saveProfile?.(init) ?? new Response(JSON.stringify(profile), { status: 201 });
    if (url.endsWith("/invoice-profile"))
      return new Response(
        JSON.stringify({ profile: options.profile === undefined ? profile : options.profile }),
      );
    if (url.includes("/catalogue/products"))
      return new Response(
        JSON.stringify(
          page([
            {
              id: productId,
              sku: "TEST-PCM-500",
              brand_name: "TEST Paracetamol",
              status: "active",
            },
            {
              id: "0199e000-0000-7000-8000-000000000009",
              sku: "TEST-OLD",
              brand_name: "TEST Withdrawn",
              status: "withdrawn",
            },
          ]),
        ),
      );
    if (url.includes("/sales/availability"))
      return new Response(
        JSON.stringify({
          product_id: productId,
          sellable_units: 105,
          nearest_expiry: "2027-03-31",
          regulatory_class: options.rxClass ?? "none",
        }),
      );
    if (url.includes("/discount-ceiling/") && init.method === "PUT")
      return options.saveCeiling?.(init) ?? new Response(JSON.stringify({}), { status: 201 });
    if (url.endsWith("/discount-ceiling"))
      return new Response(
        JSON.stringify({
          ceiling:
            options.ceilingBps === undefined
              ? null
              : {
                  id: "0199e000-0000-7000-8000-000000000060",
                  max_bps: options.ceilingBps,
                  reason: "TEST owner decision",
                  recorded_by: "user-owner",
                  created_at: "2026-11-01T06:30:00Z",
                },
        }),
      );
    if (url.includes("/prescriptions/open-lines"))
      return new Response(
        JSON.stringify({
          lines: [
            {
              prescription_id: rxId,
              line_number: 1,
              prescribed_as: "Amoxicillin 500 mg",
              product_id: productId,
              quantity_units: 20,
              dispensings_allowed: 2,
              units_remaining: 20,
              dispensings_remaining: 2,
              prescribed_on: "2026-10-30",
              prescriber_name: "TEST Dr Asha Rao",
              patient_name: "TEST Ravi Kumar",
              addresses_on_file: true,
            },
          ],
        }),
      );
    if (url.endsWith("/sales/quote"))
      return options.quote?.() ?? new Response(JSON.stringify({ ...totals, lines: [line] }));
    if (/\/sales\/[0-9a-f-]{36}$/.test(url) && init.method === "PUT")
      return (
        options.sale?.(init) ??
        new Response(
          JSON.stringify({
            ...totals,
            id: "0199e000-0000-7000-8000-000000000030",
            invoice_number: "TS/2627/1",
            financial_year: "2627",
            sale_date: "2026-11-01",
            seller_legal_name: profile.legal_name,
            seller_address: profile.address,
            seller_gstin: "27PQRST5678K2Z1",
            place_of_supply: "27",
            actor_subject: "user-pharmacist",
            created_at: "2026-11-01T06:30:00Z",
            class_origin: "declared_by_outlet",
            lines: [options.saleLine ?? line],
            tenders: [
              {
                tender_number: 1,
                kind: "cash",
                amount_minor: 10000,
                tendered_minor: 20000,
                change_minor: 10000,
                reference: null,
              },
            ],
          }),
          { status: 201 },
        )
      );
    return new Response(JSON.stringify({ message: `unexpected ${url}` }), { status: 500 });
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

async function basketOfEight(user: ReturnType<typeof renderWithProviders>["user"]) {
  await chooseOption(user, await screen.findByLabelText("Product"), /TEST-PCM-500/);
  expect(await screen.findByText("105 units sellable, nearest expiry 2027-03-31")).toBeVisible();
  const units = screen.getByLabelText("Units");
  await user.clear(units);
  await user.type(units, "8");
  await user.click(screen.getByRole("button", { name: "Add to basket" }));
}

describe("CounterSale", () => {
  it("prices the basket on the server, takes cash and shows the invoice", async () => {
    const fetcher = fixture();
    const { user } = renderWithProviders(
      <CounterSale scope={scope} canSell canManageProfile={false} />,
    );
    expect(await screen.findByText(/Invoices print as TEST Main Pharmacy LLP/)).toBeVisible();
    expect(await screen.findByText(/NPPA ceiling prices are not checked/)).toBeVisible();
    await user.click(await screen.findByLabelText("Product"));
    expect(screen.queryByRole("option", { name: /TEST-OLD/ })).toBeNull();
    await user.keyboard("{Escape}");

    await basketOfEight(user);
    expect(
      within(screen.getByRole("list", { name: "Basket" })).getByText(
        "TEST-PCM-500 · TEST Paracetamol × 8",
      ),
    ).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Price basket" }));
    expect(await screen.findByText("INR 100.00", { selector: "dd" })).toBeVisible();
    expect(screen.getByText("TEST-LOT")).toBeVisible();

    const received = screen.getByLabelText("Amount received (INR)");
    await user.clear(received);
    await user.type(received, "200");
    await user.click(screen.getByRole("button", { name: "Post sale" }));
    expect(await screen.findByRole("heading", { name: "Tax invoice TS/2627/1" })).toBeVisible();
    expect(screen.getByText(/change INR 100.00/)).toBeVisible();

    const put = fetcher.mock.calls.find(
      ([url, init]) => /\/sales\/[0-9a-f-]{36}$/.test(url) && init?.method === "PUT",
    );
    expect(JSON.parse(String(put?.[1]?.body))).toEqual({
      lines: [{ product_id: productId, quantity_units: 8 }],
      tenders: [{ kind: "cash", amount_minor: 10000, tendered_minor: 20000 }],
    });
    await user.click(screen.getByRole("button", { name: "New sale" }));
    expect(await screen.findByRole("heading", { name: "Counter sale" })).toBeVisible();
  });

  it("refuses cash below the total and asks UPI for a reference", async () => {
    const fetcher = fixture();
    const { user } = renderWithProviders(
      <CounterSale scope={scope} canSell canManageProfile={false} />,
    );
    await basketOfEight(user);
    await user.click(screen.getByRole("button", { name: "Price basket" }));
    const received = await screen.findByLabelText("Amount received (INR)");
    await user.clear(received);
    await user.type(received, "50");
    await user.click(screen.getByRole("button", { name: "Post sale" }));
    expect(screen.getByText("Collect at least INR 100.00")).toBeVisible();
    expect(
      await screen.findByText("Enter a valid amount within the supported limit."),
    ).toBeVisible();

    await chooseOption(user, screen.getByLabelText("Paid by"), "UPI (reference only)");
    await user.click(screen.getByRole("button", { name: "Post sale" }));
    expect(await screen.findByText("Enter the payment reference")).toBeVisible();
    expect(fetcher.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(false);
  });

  it("posts verified UPI only once Razorpay has confirmed the payment", async () => {
    const counterPayment = { status: "awaiting_payment" };
    const fetcher = fixture({ counterPayment });
    const { user } = renderWithProviders(
      <CounterSale scope={scope} canSell canManageProfile={false} />,
    );
    await basketOfEight(user);
    await user.click(await screen.findByRole("button", { name: "Price basket" }));
    await chooseOption(user, await screen.findByLabelText("Paid by"), "UPI (verified)");
    await user.click(await screen.findByRole("button", { name: /Show UPI QR for/ }));
    expect(await screen.findByText(/Waiting for payment of/)).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Post sale" }));
    expect(await screen.findByText("Wait for the UPI payment to be confirmed")).toBeVisible();
    const salePut = () =>
      fetcher.mock.calls.filter(
        ([url, init]) => /\/sales\/[0-9a-f-]{36}$/.test(url) && init?.method === "PUT",
      );
    expect(salePut()).toHaveLength(0);

    counterPayment.status = "paid";
    expect(await screen.findByText(/^Paid .*RRN 123/, {}, { timeout: 6000 })).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Post sale" }));
    await waitFor(() => expect(salePut()).toHaveLength(1));
    const body = JSON.parse(String(salePut()[0][1]?.body));
    expect(body.tenders).toEqual([
      {
        kind: "upi_verified",
        amount_minor: 10000,
        payment_request_id: expect.stringMatching(/^[0-9a-f-]{36}$/),
      },
    ]);
  });

  it("shows the server's reason when a basket cannot be sold", async () => {
    fixture({
      quote: () =>
        new Response(
          JSON.stringify({
            error: "not_sellable",
            message:
              "TEST-PCM-500 is Schedule H and needs prescription evidence, which the counter does not record yet",
          }),
          { status: 409 },
        ),
    });
    const { user } = renderWithProviders(
      <CounterSale scope={scope} canSell canManageProfile={false} />,
    );
    await basketOfEight(user);
    await user.click(screen.getByRole("button", { name: "Price basket" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("needs prescription evidence");
    expect(screen.queryByRole("button", { name: "Post sale" })).toBeNull();
  });

  it("lets an owner record invoice details before anyone sells", async () => {
    const saves: string[] = [];
    fixture({
      profile: null,
      saveProfile: (init) => {
        saves.push(String(init.body));
        return new Response(JSON.stringify(profile), { status: 201 });
      },
    });
    const { user } = renderWithProviders(<CounterSale scope={scope} canSell canManageProfile />);
    expect(await screen.findByRole("heading", { name: "Invoice details" })).toBeVisible();
    expect(screen.queryByRole("heading", { name: "Counter sale" })).toBeNull();
    await user.type(screen.getByLabelText("Legal name"), "TEST Main Pharmacy LLP");
    await user.type(screen.getByLabelText("Address"), "TEST 1 Market Road");
    await user.type(screen.getByLabelText("Invoice prefix"), "ts");
    await user.click(screen.getByRole("button", { name: "Record invoice details" }));
    expect(await screen.findByText("1 to 4 capital letters or digits")).toBeVisible();
    await user.clear(screen.getByLabelText("Invoice prefix"));
    await user.type(screen.getByLabelText("Invoice prefix"), "TS");
    await user.click(screen.getByRole("button", { name: "Record invoice details" }));
    await waitFor(() => expect(saves).toHaveLength(1));
    expect(JSON.parse(saves[0])).toEqual({
      legal_name: "TEST Main Pharmacy LLP",
      address: "TEST 1 Market Road",
      invoice_prefix: "TS",
    });
  });

  it("tells a seller without invoice details whom to ask, and a non-seller that they cannot sell", async () => {
    fixture({ profile: null });
    const first = renderWithProviders(
      <CounterSale scope={scope} canSell canManageProfile={false} />,
    );
    expect(await screen.findByRole("heading", { name: "No invoice details yet" })).toBeVisible();
    expect(screen.queryByLabelText("Product")).toBeNull();
    first.unmount();

    fixture();
    renderWithProviders(<CounterSale scope={scope} canSell={false} canManageProfile={false} />);
    expect(await screen.findByText(/Selling here needs the pharmacist role/)).toBeVisible();
    expect(screen.queryByLabelText("Product")).toBeNull();
  });

  it("retries an unclear failure with the same sale id, never a new one", async () => {
    let attempts = 0;
    const fetcher = fixture({
      sale: () => {
        attempts++;
        return attempts === 1
          ? new Response(JSON.stringify({ error: "bad_gateway", message: "Upstream timed out" }), {
              status: 502,
            })
          : new Response(
              JSON.stringify({ error: "not_sellable", message: "not enough sellable stock" }),
              {
                status: 409,
              },
            );
      },
    });
    const { user } = renderWithProviders(
      <CounterSale scope={scope} canSell canManageProfile={false} />,
    );
    await basketOfEight(user);
    await user.click(screen.getByRole("button", { name: "Price basket" }));
    await user.click(await screen.findByRole("button", { name: "Post sale" }));
    expect(await screen.findByText(/The sale may have been posted/)).toBeVisible();
    expect(screen.queryByRole("button", { name: "Price the basket again" })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("button", { name: "Price the basket again" })).toBeVisible();
    const ids = fetcher.mock.calls
      .filter(([url, init]) => /\/sales\/[0-9a-f-]{36}$/.test(url) && init?.method === "PUT")
      .map(([url]) => url);
    expect(ids).toHaveLength(2);
    expect(ids[1]).toBe(ids[0]);
  });

  it("dispenses a Schedule H line only against a chosen prescription and reminds to endorse it", async () => {
    const rxLine = { ...line, regulatory_class: "H", prescription_id: rxId, prescription_line: 1 };
    const fetcher = fixture({
      rxClass: "H",
      saleLine: rxLine,
      quote: () => new Response(JSON.stringify({ ...totals, lines: [rxLine] })),
    });
    const { user } = renderWithProviders(
      <CounterSale scope={scope} canSell canManageProfile={false} />,
    );
    await chooseOption(user, await screen.findByLabelText("Product"), /TEST-PCM-500/);
    const units = screen.getByLabelText("Units");
    await user.clear(units);
    await user.type(units, "8");
    await user.click(screen.getByRole("button", { name: "Add to basket" }));
    expect(
      await screen.findByText("Schedule H: choose the prescription this is dispensed against"),
    ).toBeVisible();
    expect(screen.queryByRole("list", { name: "Basket" })).toBeNull();

    await chooseOption(
      user,
      screen.getByLabelText("Prescription (Schedule H)"),
      /TEST Ravi Kumar · TEST Dr Asha Rao · 2026-10-30 · 20 units, 2 dispensings left/,
    );
    await user.click(screen.getByRole("button", { name: "Add to basket" }));
    expect(await screen.findByText(/× 8 · on prescription/)).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Price basket" }));
    await user.click(await screen.findByRole("button", { name: "Post sale" }));
    expect(await screen.findByRole("heading", { name: "Tax invoice TS/2627/1" })).toBeVisible();
    expect(screen.getByRole("note")).toHaveTextContent(
      "Endorse the prescription: note TEST Main Pharmacy LLP, TEST 1 Market Road and 2026-11-01",
    );

    const cited = {
      product_id: productId,
      quantity_units: 8,
      prescription: { prescription_id: rxId, line_number: 1 },
    };
    const quote = fetcher.mock.calls.find(([url]) => String(url).endsWith("/sales/quote"));
    expect(JSON.parse(String(quote?.[1]?.body))).toEqual({ lines: [cited] });
    const put = fetcher.mock.calls.find(
      ([url, init]) => /\/sales\/[0-9a-f-]{36}$/.test(url) && init?.method === "PUT",
    );
    expect(JSON.parse(String(put?.[1]?.body)).lines).toEqual([cited]);
  });

  it("records a walk-in buyer only when given, and asks no endorsement for OTC", async () => {
    const fetcher = fixture();
    const { user } = renderWithProviders(
      <CounterSale scope={scope} canSell canManageProfile={false} />,
    );
    await basketOfEight(user);
    expect(screen.queryByLabelText(/Prescription/)).toBeNull();
    await user.click(screen.getByRole("button", { name: "Price basket" }));
    await screen.findByLabelText("Buyer name (optional)");
    await user.type(screen.getByLabelText("Buyer phone (optional)"), "12");
    await user.click(screen.getByRole("button", { name: "Post sale" }));
    expect(await screen.findByText("Use 6 to 15 digits, with an optional +")).toBeVisible();
    await user.clear(screen.getByLabelText("Buyer phone (optional)"));
    await user.type(screen.getByLabelText("Buyer name (optional)"), "TEST Walk In");
    await user.click(screen.getByRole("button", { name: "Post sale" }));
    expect(await screen.findByRole("heading", { name: "Tax invoice TS/2627/1" })).toBeVisible();
    expect(screen.queryByRole("note")).toBeNull();
    const put = fetcher.mock.calls.find(
      ([url, init]) => /\/sales\/[0-9a-f-]{36}$/.test(url) && init?.method === "PUT",
    );
    expect(JSON.parse(String(put?.[1]?.body)).buyer).toEqual({ name: "TEST Walk In" });
  });

  it("adds no line before the product's class is known", async () => {
    let releaseAvailability!: () => void;
    const gate = new Promise<void>((resolve) => {
      releaseAvailability = resolve;
    });
    const base = fixture({ rxClass: "H" });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init: RequestInit = {}) => {
        if (url.includes("/sales/availability")) await gate;
        return base(url, init);
      }),
    );
    const { user } = renderWithProviders(
      <CounterSale scope={scope} canSell canManageProfile={false} />,
    );
    await chooseOption(user, await screen.findByLabelText("Product"), /TEST-PCM-500/);
    expect(screen.getByRole("button", { name: "Add to basket" })).toBeDisabled();
    releaseAvailability();
    expect(await screen.findByLabelText("Prescription (Schedule H)")).toBeVisible();
    expect(screen.getByRole("button", { name: "Add to basket" })).toBeEnabled();
  });

  it("gives a holder's discount within the ceiling, with a reason, and shows it on the invoice", async () => {
    const discounted = {
      ...line,
      list_amount_minor: 10000,
      discount_bps: 1000,
      discount_minor: 1000,
      amount_minor: 9000,
    };
    const fetcher = fixture({
      ceilingBps: 1000,
      quote: () =>
        new Response(JSON.stringify({ ...totals, total_minor: 9000, lines: [discounted] })),
      sale: () =>
        new Response(
          JSON.stringify({
            ...totals,
            total_minor: 9000,
            id: "0199e000-0000-7000-8000-000000000030",
            invoice_number: "TS/2627/1",
            financial_year: "2627",
            sale_date: "2026-11-01",
            seller_legal_name: profile.legal_name,
            seller_address: profile.address,
            seller_gstin: "27PQRST5678K2Z1",
            place_of_supply: "27",
            actor_subject: "user-manager",
            created_at: "2026-11-01T06:30:00Z",
            class_origin: "declared_by_outlet",
            buyer_name: null,
            buyer_phone: null,
            discount_reason: "TEST senior citizen",
            discount_minor: 1000,
            lines: [discounted],
            tenders: [
              {
                tender_number: 1,
                kind: "upi",
                amount_minor: 9000,
                tendered_minor: null,
                change_minor: null,
                reference: "TEST-UPI",
              },
            ],
          }),
          { status: 201 },
        ),
    });
    const { user } = renderWithProviders(
      <CounterSale scope={scope} canSell canManageProfile={false} canDiscount />,
    );
    await chooseOption(user, await screen.findByLabelText("Product"), /TEST-PCM-500/);
    await screen.findByText("105 units sellable, nearest expiry 2027-03-31");
    const units = screen.getByLabelText("Units");
    await user.clear(units);
    await user.type(units, "8");
    const discount = await screen.findByLabelText("Discount % (up to 10%)");
    await user.type(discount, "12");
    await user.click(screen.getByRole("button", { name: "Add to basket" }));
    expect(await screen.findByText("This outlet's ceiling is 10%")).toBeVisible();
    await user.clear(discount);
    await user.type(discount, "10");
    await user.click(screen.getByRole("button", { name: "Add to basket" }));
    expect(await screen.findByText(/× 8 · 10% off/)).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Price basket" }));
    // Decision 5: each line shows its list amount and discount before payment.
    expect(await screen.findByRole("columnheader", { name: "Discount" })).toBeVisible();
    expect(screen.getByText("INR 100.00", { selector: "td" })).toBeVisible();
    expect(screen.getByText("10% · INR 10.00")).toBeVisible();
    await chooseOption(user, await screen.findByLabelText("Paid by"), "UPI (reference only)");
    await user.type(screen.getByLabelText("Payment reference"), "TEST-UPI");
    await user.click(screen.getByRole("button", { name: "Post sale" }));
    expect(
      await screen.findByText("Give the reason for the discount, at least 5 characters"),
    ).toBeVisible();
    expect(fetcher.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(false);
    await user.type(screen.getByLabelText("Reason for the discount"), "TEST senior citizen");
    await user.click(screen.getByRole("button", { name: "Post sale" }));
    expect(await screen.findByText(/Discount INR 10.00 · TEST senior citizen/)).toBeVisible();
    const put = fetcher.mock.calls.find(
      ([url, init]) => /\/sales\/[0-9a-f-]{36}$/.test(url) && init?.method === "PUT",
    );
    const body = JSON.parse(String(put?.[1]?.body));
    expect(body.lines).toEqual([{ product_id: productId, quantity_units: 8, discount_bps: 1000 }]);
    expect(body.discount_reason).toBe("TEST senior citizen");
  });

  it("offers no discount without the permission", async () => {
    fixture({ ceilingBps: 1000 });
    const { user } = renderWithProviders(
      <CounterSale scope={scope} canSell canManageProfile={false} />,
    );
    await basketOfEight(user);
    expect(screen.queryByLabelText(/Discount %/)).toBeNull();
    await user.click(screen.getByRole("button", { name: "Price basket" }));
    await screen.findByLabelText("Paid by");
    expect(screen.queryByLabelText("Reason for the discount")).toBeNull();
  });

  it("lets the owner record the ceiling, with a reason", async () => {
    const fetcher = fixture();
    const { user } = renderWithProviders(
      <CounterSale scope={scope} canSell={false} canManageProfile={false} canManageDiscount />,
    );
    expect(
      await screen.findByText(/None recorded, so no discount can be given at this outlet./),
    ).toBeVisible();
    await user.type(screen.getByLabelText("Ceiling %"), "100.5");
    await user.click(screen.getByRole("button", { name: "Record discount ceiling" }));
    expect(
      await screen.findAllByText("Enter a percentage from 0 to 100, at most two decimals"),
    ).not.toHaveLength(0);
    await user.clear(screen.getByLabelText("Ceiling %"));
    await user.type(screen.getByLabelText("Ceiling %"), "7.5");
    await user.click(screen.getByRole("button", { name: "Record discount ceiling" }));
    expect(await screen.findByText("Give the reason, at least 5 characters")).toBeVisible();
    await user.type(screen.getByLabelText("Reason"), "TEST loyalty scheme");
    await user.click(screen.getByRole("button", { name: "Record discount ceiling" }));
    expect(await screen.findByText("Discount ceiling recorded")).toBeVisible();
    const put = fetcher.mock.calls.find(
      ([url, init]) => String(url).includes("/discount-ceiling/") && init?.method === "PUT",
    );
    expect(JSON.parse(String(put?.[1]?.body))).toEqual({
      max_bps: 750,
      reason: "TEST loyalty scheme",
    });
  });
});
