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
  } = {},
) {
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
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
            lines: [line],
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

    await chooseOption(user, screen.getByLabelText("Paid by"), "UPI");
    await user.click(screen.getByRole("button", { name: "Post sale" }));
    expect(await screen.findByText("Enter the payment reference")).toBeVisible();
    expect(fetcher.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(false);
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
});
