import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { DailyCollectionReport } from "./daily-collection";

const hospital = {
  id: "0199e000-0000-7000-8000-00000000a001",
  name: "TEST General",
  access_level: "manager" as const,
};
const money = { payments_minor: 20000, deposits_minor: 500000, refunds_minor: 100000 };
const day = (date: string) => ({
  hospital_id: hospital.id,
  date,
  timezone: "Asia/Kolkata",
  currency: "INR",
  final: false,
  shifts: [
    {
      shift_id: "s1",
      counter_code: "IPD-1",
      counter_name: "IPD desk",
      cashier: "user-clerk",
      status: "closed",
      opened_at: "2026-10-30T03:00:00Z",
      closed_at: "2026-10-30T11:00:00Z",
      opening_float_minor: 0,
      ...money,
      voided_count: 2,
      voided_minor: 5000,
      voided_refunds_minor: 50000,
      expected_minor: 420000,
      declared_minor: 419900,
      variance_minor: -100,
      authorised_by: "user-admin",
      authorisation_reason: "Counted short by one note",
      upi_payments_minor: 75000,
      upi_deposits_minor: 30000,
      picked_up_minor: 120000,
    },
    {
      shift_id: "s2",
      counter_code: "OPD-1",
      counter_name: "OPD desk",
      cashier: "user-clerk2",
      status: "open",
      opened_at: "2026-10-30T05:00:00Z",
      closed_at: null,
      opening_float_minor: null,
      payments_minor: null,
      deposits_minor: null,
      refunds_minor: null,
      voided_count: null,
      voided_minor: null,
      voided_refunds_minor: null,
      expected_minor: null,
      declared_minor: null,
      variance_minor: null,
      authorised_by: null,
      authorisation_reason: null,
      upi_payments_minor: null,
      upi_deposits_minor: null,
      picked_up_minor: null,
    },
  ],
  totals: {
    ...money,
    net_minor: 420000,
    shortage_minor: 100,
    excess_minor: 0,
    counted_shifts: 1,
    open_shifts: 1,
    pending_shifts: 0,
    voided_receipts: 2,
    voided_minor: 5000,
    voided_refunds_minor: 50000,
    upi_payments_minor: 75000,
    upi_deposits_minor: 30000,
    picked_up_minor: 120000,
  },
});
const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const csvBody = "date,status\r\n2026-10-30,closed\r\n";

async function sha256(text: string) {
  const bytes = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(text));
  return [...new Uint8Array(bytes)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

function server(opts: { perms?: string[]; timezone?: boolean; digest?: string } = {}) {
  const fetcher = vi.fn(async (url: string) => {
    if (url.includes("/iam/me/permissions"))
      return json(200, {
        subject: "user-admin",
        data: opts.perms ?? ["billing.account.read", "billing.collection.read"],
      });
    if (url.endsWith("/bed-charging"))
      return opts.timezone === false
        ? json(404, { error: "not_found", message: "not set up" })
        : json(200, { hospital_id: hospital.id, timezone: "Asia/Kolkata" });
    if (url.endsWith("/export.csv"))
      return new Response(csvBody, {
        status: 200,
        headers: {
          "Content-Type": "text/csv; charset=utf-8",
          "Content-Disposition": `attachment; filename="billing-collection-${hospital.id}-2026-10-31.csv"`,
          "X-Export-Row-Count": "2",
          "X-Export-Sha256": opts.digest ?? (await sha256(csvBody)),
        },
      });
    const m = /\/collections\/(\d{4}-\d{2}-\d{2})$/.exec(url);
    if (m) return json(200, day(m[1]!));
    return json(404, { error: "not_found", message: "not found" });
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
const urls = (f: ReturnType<typeof server>) => f.mock.calls.map(([url]) => String(url));

beforeEach(() => {
  // 20:00 UTC on 30 Oct is already 31 Oct in the hospital (IST).
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-10-30T20:00:00Z"));
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("DailyCollectionReport", () => {
  it("shows the hospital's today, each shift and the day's totals, with open shifts blind", async () => {
    const fetcher = server();
    renderWithProviders(<DailyCollectionReport hospital={hospital} />);
    const table = await screen.findByRole("table", { name: "Shifts" });
    expect(urls(fetcher).some((u) => u.endsWith("/collections/2026-10-31"))).toBe(true);
    const [, counted, open] = within(table).getAllByRole("row");
    expect(counted).toHaveTextContent("IPD-1");
    expect(counted).toHaveTextContent("INR 5,000.00");
    expect(counted).toHaveTextContent("2 · INR 50.00 (refunds INR 500.00)");
    expect(counted).toHaveTextContent("Short INR 1.00");
    expect(counted).toHaveTextContent("Authorised by user-admin: Counted short by one note");
    expect(open).toHaveTextContent("Open, not counted");
    expect(open).not.toHaveTextContent("INR");
    expect(screen.getByRole("status", { name: "Day status" })).toHaveTextContent(
      "Not final 1 open, 0 waiting for authorisation.",
    );
    const totals = screen.getByLabelText("Day totals");
    expect(totals).toHaveTextContent("Net cashINR 4,200.00");
    expect(totals).toHaveTextContent("ShortagesINR 1.00");
    // UPI apart from the drawer's cash.
    expect(counted).toHaveTextContent("INR 750.00 + deposits INR 300.00");
    expect(totals).toHaveTextContent("UPI paymentsINR 750.00");
    expect(totals).toHaveTextContent("UPI depositsINR 300.00");
    // Cash taken to the safe: a movement, not revenue.
    expect(counted).toHaveTextContent("INR 1,200.00");
    expect(totals).toHaveTextContent("Taken to the safeINR 1,200.00");
  });

  it("reads another day when one is chosen", async () => {
    const fetcher = server();
    const { user } = renderWithProviders(<DailyCollectionReport hospital={hospital} />);
    const input = await screen.findByLabelText("Day");
    await user.clear(input);
    await user.type(input, "2026-10-30");
    await user.click(screen.getByRole("button", { name: "Show day" }));
    await waitFor(() =>
      expect(urls(fetcher).some((u) => u.endsWith("/collections/2026-10-30"))).toBe(true),
    );
  });

  it("downloads the day's CSV once its digest matches", async () => {
    const create = vi.fn(() => "blob:collection");
    const revoke = vi.fn();
    vi.stubGlobal("URL", Object.assign(URL, { createObjectURL: create, revokeObjectURL: revoke }));
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    const fetcher = server();
    const { user } = renderWithProviders(<DailyCollectionReport hospital={hospital} />);
    await user.click(await screen.findByRole("button", { name: "Download CSV" }));
    expect(await screen.findByText("CSV download started: 2 shifts.")).toBeInTheDocument();
    expect(urls(fetcher).some((u) => u.endsWith("/collections/2026-10-31/export.csv"))).toBe(true);
    expect(click).toHaveBeenCalledOnce();
    expect(revoke).toHaveBeenCalledWith("blob:collection");
    click.mockRestore();
  });

  it("refuses a CSV whose bytes do not match the audited digest", async () => {
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    server({ digest: "0".repeat(64) });
    const { user } = renderWithProviders(<DailyCollectionReport hospital={hospital} />);
    await user.click(await screen.findByRole("button", { name: "Download CSV" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Could not download the collection CSV",
    );
    expect(click).not.toHaveBeenCalled();
    click.mockRestore();
  });

  it("asks for the hospital's timezone before any day can be read", async () => {
    const fetcher = server({ timezone: false });
    renderWithProviders(<DailyCollectionReport hospital={hospital} />);
    expect(
      await screen.findByText(/Set the hospital's timezone under bed charges/),
    ).toBeInTheDocument();
    expect(urls(fetcher).some((u) => u.includes("/collections/"))).toBe(false);
  });

  it("is not shown without the collection permission", async () => {
    const fetcher = server({ perms: ["billing.account.read"] });
    renderWithProviders(<DailyCollectionReport hospital={hospital} />);
    await waitFor(() =>
      expect(urls(fetcher).some((u) => u.includes("/iam/me/permissions"))).toBe(true),
    );
    expect(screen.queryByRole("heading", { name: "Collections" })).not.toBeInTheDocument();
    expect(urls(fetcher).some((u) => u.includes("/bed-charging"))).toBe(false);
  });
});
