import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { StockOverview } from "./stock-overview";
import userEvent from "@testing-library/user-event";
import { chooseOption } from "../../test/select";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
};
const row = {
  id: "TEST-position",
  outlet_id: "TEST-outlet",
  outlet_name: "TEST Main",
  outlet_code: "TEST-MAIN",
  product_id: "TEST-product",
  brand_name: "TEST Medicine",
  sku: "TEST-SKU",
  unit_of_issue: "tablet",
  lot_number: "TEST-LOT",
  location_name: "TEST Receiving",
  stock_status: "quarantine",
  expires_on: "2026-11-30",
  days_to_expiry: 30,
  expiry_time_zone: "Asia/Kolkata",
  on_hand_units: 3,
  product_on_hand_units: "123",
  known_purchase_value_minor: "99999999999999999",
  currency: "INR",
  uncosted_units: "17",
};
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
function serve(handler: (url: URL) => Response) {
  const fetcher = vi.fn(async (path: string) => handler(new URL(path, "https://test.invalid")));
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
function page(data: unknown[], more = false, coverage = "restricted_outlets_excluded") {
  return new Response(
    JSON.stringify({ data, page: { has_more: more, next_cursor: more ? "TEST-next" : null } }),
    {
      headers: {
        "X-Pharmacy-Stock-Coverage": coverage,
        "X-Pharmacy-Stock-Valuation": JSON.stringify([
          {
            currency: "INR",
            known_purchase_value_minor: "99999999999999999",
            uncosted_products: "1",
          },
        ]),
      },
    },
  );
}
it("shows outlet scope, full product totals and explicit uncosted quantities without losing money precision", async () => {
  const fetcher = serve(() => page([row]));
  renderWithProviders(<StockOverview scope={scope} />);
  const table = await screen.findByRole("table", { name: "Stock across outlets" });
  expect(table).toHaveTextContent("TEST Main (TEST-MAIN)");
  expect(table).toHaveTextContent("3 tablet");
  expect(table).toHaveTextContent("123 tablet");
  expect(table).toHaveTextContent("INR 999,999,999,999,999.99 known");
  expect(table).toHaveTextContent("17 tablet uncosted");
  expect(screen.getByText(/Restricted outlets are excluded/)).toBeInTheDocument();
  const [path, init] = fetcher.mock.calls[0] as unknown as [string, RequestInit];
  expect(path).toContain(`/pharmacies/${scope.pharmacyId}/stock/positions?`);
  expect(new Headers(init?.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
});
it("paginates at one cutoff and resets the cursor when filters change", async () => {
  const fetcher = serve((url) =>
    page(
      [{ ...row, id: url.searchParams.has("cursor") ? "second" : "first" }],
      !url.searchParams.has("cursor"),
    ),
  );
  renderWithProviders(<StockOverview scope={scope} />);
  await screen.findByRole("table");
  fireEvent.click(screen.getByRole("button", { name: "Next page" }));
  await waitFor(() =>
    expect(fetcher.mock.calls.some(([path]) => String(path).includes("cursor=TEST-next"))).toBe(
      true,
    ),
  );
  await chooseOption(
    userEvent.setup(),
    screen.getByRole("combobox", { name: "Expiry" }),
    "Due within 30 days",
  );
  await waitFor(() => {
    const last = new URL(String(fetcher.mock.calls.at(-1)![0]), "https://test.invalid");
    expect(last.searchParams.get("expiry")).toBe("due_30");
    expect(last.searchParams.has("cursor")).toBe(false);
  });
  const cutoffs = fetcher.mock.calls.map(([path]) =>
    new URL(String(path), "https://test.invalid").searchParams.get("as_of"),
  );
  expect(new Set(cutoffs).size).toBe(1);
});
it("shows restricted coverage even when the matching page is empty", async () => {
  serve(() => page([]));
  renderWithProviders(<StockOverview scope={scope} />);
  expect(await screen.findByText(/No stock positions match/)).toBeInTheDocument();
  expect(screen.getByText(/Restricted outlets are excluded/)).toBeInTheDocument();
});
it.each(["fail", "missing-coverage"])(
  "fails closed on %s and never presents stale stock",
  async (kind) => {
    serve(() =>
      kind === "fail"
        ? new Response("{}", { status: 503 })
        : new Response(
            JSON.stringify({ data: [row], page: { has_more: false, next_cursor: null } }),
          ),
    );
    renderWithProviders(<StockOverview scope={scope} />);
    expect(await screen.findByText("Could not load stock across outlets.")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Download CSV" })).toBeDisabled();
  },
);
