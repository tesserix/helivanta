import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../test/select";
import { Returns } from "./returns";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const saleId = "0199e000-0000-7000-8000-000000000030";
const quarantine = "0199e000-0000-7000-8000-000000000004";
const page = (data: unknown[]) => ({
  data,
  page: { has_more: false, next_cursor: null, total: data.length, offset: 0 },
});
const line = {
  line_number: 1,
  product_id: "0199e000-0000-7000-8000-000000000003",
  batch_id: "0199e000-0000-7000-8000-000000000005",
  location_id: "0199e000-0000-7000-8000-000000000006",
  lot_number: "TEST-LOT",
  expires_on: "2027-03-31",
  regulatory_class: "none",
  classification_code: "30049099",
  supply_nature: "taxable",
  rate_ppm: 120000,
  mrp_minor: 12500,
  pack_size: 10,
  quantity_units: 10,
  // 124.99: a line whose pro rata does not divide evenly.
  amount_minor: 12499,
  taxable_minor: 11159,
  cgst_minor: 670,
  sgst_minor: 670,
  prescription_id: null,
  prescription_line: null,
  list_amount_minor: 12500,
  discount_bps: 0,
  discount_minor: 0,
  returned_units: 4,
};
const sale = {
  id: saleId,
  invoice_number: "TS/2627/1",
  financial_year: "2627",
  sale_date: "2026-11-01",
  seller_legal_name: "TEST Main Pharmacy LLP",
  seller_address: "TEST 1 Market Road",
  seller_gstin: "27PQRST5678K2Z1",
  place_of_supply: "27",
  currency: "INR",
  total_minor: 12500,
  taxable_minor: 11160,
  cgst_minor: 670,
  sgst_minor: 670,
  actor_subject: "user-pharmacist",
  buyer_name: null,
  buyer_phone: null,
  discount_reason: null,
  discount_minor: 0,
  created_at: "2026-11-01T06:30:00Z",
  class_origin: "declared_by_outlet",
  nppa_ceiling_checked: false,
  lines: [line],
  tenders: [],
};

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function fixture() {
  const fetcher = vi.fn(async (url: string, init: RequestInit = {}) => {
    if (url.includes("/sales?")) return new Response(JSON.stringify(page([sale])));
    if (url.endsWith(`/sales/${saleId}`)) return new Response(JSON.stringify(sale));
    if (url.includes("/stock/locations"))
      return new Response(
        JSON.stringify(
          page([
            { id: quarantine, code: "TEST-Q", name: "TEST Quarantine", stock_status: "quarantine" },
            {
              id: "0199e000-0000-7000-8000-000000000007",
              code: "TEST-SHELF",
              name: "TEST Shelf",
              stock_status: "sellable",
            },
          ]),
        ),
      );
    if (url.includes("/credit-notes/") && init.method === "PUT")
      return new Response(
        JSON.stringify({
          id: "0199e000-0000-7000-8000-000000000050",
          sale_id: saleId,
          credit_note_number: "TS/2627/C1",
          financial_year: "2627",
          credit_note_date: "2026-11-02",
          invoice_number: "TS/2627/1",
          invoice_date: "2026-11-01",
          seller_legal_name: "TEST Main Pharmacy LLP",
          seller_address: "TEST 1 Market Road",
          seller_gstin: "27PQRST5678K2Z1",
          place_of_supply: "27",
          currency: "INR",
          total_minor: 3750,
          taxable_minor: 3348,
          cgst_minor: 201,
          sgst_minor: 201,
          reason: "TEST prescription changed",
          recipient_name: "TEST Ravi Kumar",
          recipient_address: "TEST 4 Station Road",
          actor_subject: "user-pharmacist",
          created_at: "2026-11-02T06:30:00Z",
          lines: [
            {
              line_number: 1,
              sale_line: 1,
              product_id: line.product_id,
              batch_id: line.batch_id,
              lot_number: "TEST-LOT",
              location_id: quarantine,
              classification_code: "30049099",
              rate_ppm: 120000,
              quantity_units: 3,
              amount_minor: 3750,
              taxable_minor: 3348,
              cgst_minor: 201,
              sgst_minor: 201,
            },
          ],
          refunds: [{ refund_number: 1, kind: "cash", amount_minor: 3750, reference: null }],
        }),
        { status: 201 },
      );
    return new Response(JSON.stringify({ message: `unexpected ${url}` }), { status: 500 });
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

describe("Returns", () => {
  it("issues a credit note into quarantine and prints its invoice reference", async () => {
    const fetcher = fixture();
    const { user } = renderWithProviders(<Returns scope={scope} canReturn />);
    await user.click(await screen.findByRole("button", { name: "Return from TS/2627/1" }));
    const units = await screen.findByLabelText("Units returned on line 1");
    await user.type(units, "7");
    await user.type(screen.getByLabelText("Refunded to"), "TEST Ravi Kumar");
    await user.type(screen.getByLabelText("Their address"), "TEST 4 Station Road");
    await user.type(screen.getByLabelText("Reason"), "TEST prescription changed");
    await user.click(screen.getByRole("button", { name: "Issue credit note" }));
    expect(await screen.findByText("At most 6 left to return")).toBeVisible();
    expect(screen.getByText("Choose the quarantine location")).toBeVisible();
    expect(fetcher.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(false);

    await chooseOption(user, screen.getByLabelText("Quarantine location"), /TEST-Q/);
    expect(screen.queryByRole("option", { name: /TEST-SHELF/ })).toBeNull();
    await user.clear(units);
    await user.type(units, "3");
    // 3 more after 4 of 10: taxable floor(78.113) - floor(44.636) = 33.48 and each
    // half floor(4.69) - floor(2.68) = 2.01, so 37.50; counted from zero it is 37.49.
    expect(screen.getByText("Refund INR 37.50")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Issue credit note" }));
    expect(await screen.findByRole("heading", { name: "Credit note TS/2627/C1" })).toBeVisible();
    expect(screen.getByText(/Against invoice TS\/2627\/1 of 2026-11-01/)).toBeVisible();
    expect(screen.getByText("Total credited INR 37.50")).toBeVisible();
    // Rule 53(1A)(f): an unregistered recipient's state, by name and code.
    expect(screen.getByText(/State Maharashtra \(27\)/)).toBeVisible();

    const put = fetcher.mock.calls.find(([, init]) => init?.method === "PUT");
    expect(JSON.parse(String(put?.[1]?.body))).toEqual({
      sale_id: saleId,
      lines: [{ sale_line: 1, quantity_units: 3, quarantine_location_id: quarantine }],
      refunds: [{ kind: "cash", amount_minor: 3750 }],
      reason: "TEST prescription changed",
      recipient: { name: "TEST Ravi Kumar", address: "TEST 4 Station Road" },
    });
  });

  it("lets a reader see invoices but not take a return", async () => {
    fixture();
    const { user } = renderWithProviders(<Returns scope={scope} canReturn={false} />);
    await user.click(await screen.findByRole("button", { name: "Return from TS/2627/1" }));
    expect(await screen.findByText(/Taking a return needs the pharmacist role/)).toBeVisible();
    expect(screen.queryByRole("button", { name: "Issue credit note" })).toBeNull();
  });

  it("reloads what is left to return after another counter took a return", async () => {
    let returnedSoFar = 4;
    const base = fixture();
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init: RequestInit = {}) => {
        if (url.endsWith(`/sales/${saleId}`))
          return new Response(
            JSON.stringify({ ...sale, lines: [{ ...line, returned_units: returnedSoFar }] }),
          );
        if (url.includes("/credit-notes/") && init.method === "PUT") {
          returnedSoFar = 9;
          return new Response(
            JSON.stringify({
              error: "not_returnable",
              message: "TEST-PCM-500: 1 of 10 units are left to return",
            }),
            { status: 409 },
          );
        }
        return base(url, init);
      }),
    );
    const { user } = renderWithProviders(<Returns scope={scope} canReturn />);
    await user.click(await screen.findByRole("button", { name: "Return from TS/2627/1" }));
    await user.type(await screen.findByLabelText("Units returned on line 1"), "3");
    await chooseOption(user, screen.getByLabelText("Quarantine location"), /TEST-Q/);
    await user.type(screen.getByLabelText("Refunded to"), "TEST Ravi Kumar");
    await user.type(screen.getByLabelText("Their address"), "TEST 4 Station Road");
    await user.type(screen.getByLabelText("Reason"), "TEST prescription changed");
    await user.click(screen.getByRole("button", { name: "Issue credit note" }));
    // The form reloads with what is now left, so the next attempt is current.
    // Line 1, batch TEST-LOT, 10 sold, now 1 left (it was 6).
    await waitFor(() => expect(screen.getAllByRole("row")[1]).toHaveTextContent(/^1TEST-LOT101/));
  });

  it("returns a sale billed to an admission to its account, with nothing paid out", async () => {
    const admitted = {
      ...sale,
      admission_id: "0199e000-0000-7000-8000-0000000000c2",
      admission_bed_code: "A-1",
    };
    const fetcher = fixture();
    const base = fetcher.getMockImplementation()!;
    fetcher.mockImplementation(async (url: string, init: RequestInit = {}) => {
      if (url.includes("/sales?")) return new Response(JSON.stringify(page([admitted])));
      if (url.endsWith(`/sales/${saleId}`)) return new Response(JSON.stringify(admitted));
      return base(url, init);
    });
    const { user } = renderWithProviders(<Returns scope={scope} canReturn />);
    await user.click(await screen.findByRole("button", { name: "Return from TS/2627/1" }));
    expect(
      await screen.findByText(/Returned to the admission account it was billed to \(bed A-1\)/),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText("Refund by")).toBeNull();
    expect(screen.queryByLabelText("Refund reference")).toBeNull();
    await user.type(screen.getByLabelText("Units returned on line 1"), "3");
    await chooseOption(user, screen.getByLabelText("Quarantine location"), /TEST-Q/);
    await user.type(screen.getByLabelText("Refunded to"), "TEST Ward 4");
    await user.type(screen.getByLabelText("Their address"), "TEST General Hospital");
    await user.type(screen.getByLabelText("Reason"), "TEST wrong bed");
    await user.click(screen.getByRole("button", { name: "Issue credit note" }));
    await waitFor(() => expect(fetcher.mock.calls.some(([, i]) => i?.method === "PUT")).toBe(true));
    const put = fetcher.mock.calls.find(([, i]) => i?.method === "PUT");
    expect(JSON.parse(String(put?.[1]?.body)).refunds).toEqual([
      { kind: "admission_account", amount_minor: 3750 },
    ]);
  });
});
