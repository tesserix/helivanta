import { screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { DailyReport } from "./daily-report";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const zero = {
  sales: 0,
  sales_minor: 0,
  sales_taxable_minor: 0,
  sales_cgst_minor: 0,
  sales_sgst_minor: 0,
  discount_minor: 0,
  units_sold: 0,
  credit_notes: 0,
  credits_minor: 0,
  credits_taxable_minor: 0,
  credits_cgst_minor: 0,
  credits_sgst_minor: 0,
  units_returned: 0,
  net_minor: 0,
  cash_minor: 0,
  upi_minor: 0,
  card_minor: 0,
  cash_refund_minor: 0,
  upi_refund_minor: 0,
  card_refund_minor: 0,
};
const days = [
  {
    ...zero,
    day: "2026-11-01",
    currency: "INR",
    sales: 2,
    sales_minor: 15000,
    sales_taxable_minor: 14286,
    sales_cgst_minor: 357,
    sales_sgst_minor: 357,
    units_sold: 12,
    net_minor: 15000,
    cash_minor: 12500,
    upi_minor: 2500,
  },
  {
    ...zero,
    day: "2026-11-02",
    currency: "INR",
    credit_notes: 1,
    credits_minor: 5000,
    units_returned: 4,
    net_minor: -5000,
    cash_refund_minor: 5000,
  },
];

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function serve(rows: unknown[] | "fail") {
  const fetcher = vi.fn(async (url: string) =>
    rows === "fail"
      ? new Response(JSON.stringify({ error: "internal", message: "no" }), { status: 500 })
      : new Response(
          JSON.stringify({
            from: "2026-11-01",
            to: "2026-11-15",
            days: url.includes("from=2026-11-01&to=2026-11-15") ? rows : [],
          }),
        ),
  );
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

describe("DailyReport", () => {
  it("shows each day's sales, returns, net and payments for the month so far", async () => {
    serve(days);
    renderWithProviders(<DailyReport scope={scope} today="2026-11-15" />);
    const table = await screen.findByRole("table", { name: "Daily sales" });
    const first = screen.getByRole("row", { name: /2026-11-01/ });
    expect(first).toHaveTextContent("2 for INR 150.00");
    expect(first).toHaveTextContent("12 units");
    expect(first).toHaveTextContent("Cash INR 125.00, UPI INR 25.00");
    const second = screen.getByRole("row", { name: /2026-11-02/ });
    expect(second).toHaveTextContent("1 for INR 50.00");
    expect(second).toHaveTextContent("INR -50.00");
    expect(second).toHaveTextContent("Cash INR 50.00");
    // Taken and refunded name only the kinds used.
    expect(within(first).getAllByRole("cell")[4]).toHaveTextContent(/^—$/);
    expect(within(second).getAllByRole("cell")[3]).toHaveTextContent(/^—$/);
    expect(within(second).getAllByRole("cell")[4]).toHaveTextContent(/^Cash INR 50.00$/);
    expect(table).toBeVisible();
  });

  it("says when nothing was sold or returned instead of showing zeros", async () => {
    serve([]);
    renderWithProviders(<DailyReport scope={scope} today="2026-11-15" />);
    expect(
      await screen.findByText("No sales or returns from 2026-11-01 to 2026-11-15."),
    ).toBeVisible();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("reports a failed load with a retry", async () => {
    serve("fail");
    renderWithProviders(<DailyReport scope={scope} today="2026-11-15" />);
    expect(await screen.findByText("Could not load the daily report.")).toBeVisible();
    expect(screen.getByRole("button", { name: /try again|retry/i })).toBeVisible();
  });

  it("refuses a period over 366 days without asking the server", async () => {
    const fetcher = serve([]);
    const { user } = renderWithProviders(<DailyReport scope={scope} today="2026-11-15" />);
    await screen.findByText(/No sales or returns/);
    const calls = fetcher.mock.calls.length;
    const from = screen.getByLabelText("From");
    await user.clear(from);
    await user.type(from, "2025-01-01");
    await user.click(screen.getByRole("button", { name: "Show report" }));
    expect(await screen.findByText("A period is at most 366 days")).toBeVisible();
    expect(fetcher.mock.calls.length).toBe(calls);
  });
});
