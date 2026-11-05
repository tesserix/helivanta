import { screen, waitFor, within } from "@testing-library/react";
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
  upi_verified_minor: 0,
  card_minor: 0,
  cash_refund_minor: 0,
  upi_refund_minor: 0,
  card_refund_minor: 0,
  admission_account_minor: 0,
  admission_account_refund_minor: 0,
  service_charges_minor: 0,
  service_charges_taxable_minor: 0,
  service_charge_credits_minor: 0,
  service_charge_credits_taxable_minor: 0,
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
    upi_verified_minor: 68000,
    admission_account_minor: 4000,
    service_charges_minor: 5000,
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
    admission_account_refund_minor: 1000,
    service_charge_credits_minor: 2000,
  },
];

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function serve(rows: unknown[] | "fail" | "refuse", permissions: string[] = []) {
  const fetcher = vi.fn(async (url: string) =>
    url.includes("/iam/me/permissions")
      ? new Response(JSON.stringify({ data: permissions }))
      : // The sales report panels (#1060) have their own tests.
        !url.includes("/reports/daily")
        ? new Response(JSON.stringify({ data: [], page: { next_cursor: null, has_more: false } }))
        : rows === "fail"
          ? new Response(JSON.stringify({ error: "internal", message: "no" }), { status: 500 })
          : rows === "refuse"
            ? new Response(JSON.stringify({ error: "forbidden", message: "no" }), { status: 403 })
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
    // Service charges (#1388) are reported apart from medicine revenue.
    expect(first).toHaveTextContent("medicines INR 100.00, service charges INR 50.00");
    expect(first).toHaveTextContent(
      "Cash INR 125.00, UPI (verified) INR 680.00, UPI INR 25.00, Admission account INR 40.00",
    );
    const second = screen.getByRole("row", { name: /2026-11-02/ });
    expect(second).toHaveTextContent("1 for INR 50.00");
    expect(second).toHaveTextContent("INR -50.00");
    expect(second).toHaveTextContent("medicines INR 30.00, service charges INR 20.00");
    expect(second).toHaveTextContent("Cash INR 50.00");
    // Taken and refunded name only the kinds used.
    expect(within(first).getAllByRole("cell")[4]).toHaveTextContent(/^—$/);
    expect(within(second).getAllByRole("cell")[3]).toHaveTextContent(/^—$/);
    expect(within(second).getAllByRole("cell")[4]).toHaveTextContent(
      /^Cash INR 50.00, Admission account INR 10.00$/,
    );
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

  it("shows sales and stock only to staff who may read stock", async () => {
    serve(days, ["pharmacy.sale.read", "pharmacy.stock.read"]);
    const { unmount } = renderWithProviders(<DailyReport scope={scope} today="2026-11-15" />);
    expect(await screen.findByRole("heading", { name: "Sales and stock" })).toBeVisible();
    unmount();
    serve(days, ["pharmacy.sale.read"]);
    renderWithProviders(<DailyReport scope={scope} today="2026-11-15" />);
    await screen.findByRole("table", { name: "Daily sales" });
    expect(screen.getByRole("heading", { name: "Patient-wise sales" })).toBeVisible();
    expect(screen.queryByRole("heading", { name: "Sales and stock" })).toBeNull();
  });

  it("offers the patient lookup only to staff who may look patients up", async () => {
    serve(days, ["pharmacy.sale.read", "patient.pharmacy-lookup"]);
    const { unmount } = renderWithProviders(<DailyReport scope={scope} today="2026-11-15" />);
    expect(await screen.findByText(/No hospital has verified this pharmacy yet/)).toBeVisible();
    unmount();
    serve(days, ["pharmacy.sale.read"]);
    renderWithProviders(<DailyReport scope={scope} today="2026-11-15" />);
    await screen.findByRole("table", { name: "Daily sales" });
    expect(
      screen.getByText("Finding a patient needs patient lookup, which pharmacists hold."),
    ).toBeVisible();
    expect(screen.queryByText(/No hospital has verified this pharmacy yet/)).toBeNull();
  });

  it("reports a failed load with a retry", async () => {
    serve("fail");
    renderWithProviders(<DailyReport scope={scope} today="2026-11-15" />);
    expect(await screen.findByText("Could not load the daily report.")).toBeVisible();
    expect(screen.getByRole("button", { name: /try again|retry/i })).toBeVisible();
  });

  it("announces an empty period, and shows a refusal with no retry", async () => {
    serve([]);
    const { unmount } = renderWithProviders(<DailyReport scope={scope} today="2026-11-15" />);
    expect(
      await screen.findByText("No sales or returns from 2026-11-01 to 2026-11-15."),
    ).toHaveAttribute("role", "status");
    unmount();
    serve("refuse");
    renderWithProviders(<DailyReport scope={scope} today="2026-11-15" />);
    await waitFor(() =>
      expect(
        screen
          .getAllByRole("alert")
          .some((a) => a.textContent === "You don't have access to this outlet's reports."),
      ).toBe(true),
    );
    expect(screen.queryByText("Could not load the daily report.")).toBeNull();
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

describe("the daily report's CSV (#1060)", () => {
  it("downloads the period's days", async () => {
    localStorage.clear();
    vi.stubGlobal(
      "URL",
      Object.assign(class extends URL {}, {
        createObjectURL: vi.fn(() => "blob:daily"),
        revokeObjectURL: vi.fn(),
      }),
    );
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    const fetcher = serve(days);
    const json = fetcher.getMockImplementation()!;
    fetcher.mockImplementation(async (url: string) =>
      url.includes("/reports/daily.csv?")
        ? new Response("period_from\r\n", {
            headers: {
              "Content-Type": "text/csv; charset=utf-8",
              "Content-Disposition": `attachment; filename="pharmacy-daily-${scope.outletId}-2026-11-01-2026-11-15.csv"`,
            },
          })
        : json(url),
    );
    const { user } = renderWithProviders(<DailyReport scope={scope} today="2026-11-15" />);
    const region = screen.getByRole("status", { name: "Daily sales export" }).parentElement!;
    await user.click(within(region).getByRole("button", { name: "Download report CSV" }));
    expect(await within(region).findByText("CSV download started.")).toBeVisible();
    expect(
      fetcher.mock.calls.map(([url]) => String(url)).filter((u) => u.includes(".csv?")),
    ).toEqual([expect.stringContaining("/reports/daily.csv?from=2026-11-01&to=2026-11-15")]);
  });
});
