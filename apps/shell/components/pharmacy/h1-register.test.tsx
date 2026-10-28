import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { H1Register } from "./h1-register";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const entry = {
  id: "0199e000-0000-7000-8000-000000000070",
  supply_date: "2026-11-01",
  invoice_number: "TS/2627/1",
  sale_id: "0199e000-0000-7000-8000-000000000030",
  line_number: 1,
  prescription_id: "0199e000-0000-7000-8000-000000000040",
  prescribed_on: "2026-10-30",
  prescriber_name: "TEST Dr Asha Rao",
  prescriber_registration: "MMC/2011/04567",
  prescriber_address: "TEST 12 Clinic Lane",
  patient_name: "TEST Ravi Kumar",
  patient_address: "TEST 4 Station Road",
  product_id: "0199e000-0000-7000-8000-000000000003",
  sku: "TEST-H1-DRUG",
  brand_name: "TEST H1 Drug",
  generic_name: "TEST generic",
  lot_number: "TEST-LOT",
  expires_on: "2028-06-30",
  quantity_units: 10,
  returned_units: 3,
  pharmacist: "user-pharmacist",
};

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("H1Register", () => {
  it("shows each H1 supply for the month so far, with its returns", async () => {
    const fetcher = vi.fn(
      async (url: string) =>
        new Response(
          JSON.stringify({
            data: url.includes("from=2026-11-01&to=2026-11-15") ? [entry] : [],
            page: { has_more: false, next_cursor: null },
          }),
        ),
    );
    vi.stubGlobal("fetch", fetcher);
    renderWithProviders(<H1Register scope={scope} today="2026-11-15" />);
    const table = await screen.findByRole("table", { name: "H1 register" });
    expect(table).toHaveTextContent("TEST Dr Asha Rao (MMC/2011/04567), TEST 12 Clinic Lane");
    expect(table).toHaveTextContent("TEST Ravi Kumar, TEST 4 Station Road");
    expect(table).toHaveTextContent("TEST H1 Drug (TEST generic)");
    expect(screen.getByRole("cell", { name: "3" })).toBeVisible();
  });

  it("refuses a period over 366 days without asking the server", async () => {
    const fetcher = vi.fn(
      async () =>
        new Response(JSON.stringify({ data: [], page: { has_more: false, next_cursor: null } })),
    );
    vi.stubGlobal("fetch", fetcher);
    const { user } = renderWithProviders(<H1Register scope={scope} today="2026-11-15" />);
    await screen.findByText(/No Schedule H1 supplies from 2026-11-01 to 2026-11-15/);
    const calls = fetcher.mock.calls.length;
    const from = screen.getByLabelText("From");
    await user.clear(from);
    await user.type(from, "2025-01-01");
    await user.click(screen.getByRole("button", { name: "Show register" }));
    expect(await screen.findByText("A period is at most 366 days")).toBeVisible();
    expect(fetcher.mock.calls.length).toBe(calls);
  });
});
