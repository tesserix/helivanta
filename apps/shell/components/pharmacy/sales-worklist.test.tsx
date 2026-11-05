import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders as renderProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { chooseOption } from "../../test/select";
import type { PharmacySaleSummary } from "@helivanta/api";
import { SaleStatement, SalesWorklist } from "./sales-worklist";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};

// A live address, as the app router has: the open sale is the URL.
const nav = vi.hoisted(() => ({ search: "", listeners: new Set<() => void>() }));
vi.mock("next/navigation", async () => {
  const { useSyncExternalStore } = await import("react");
  const subscribe = (fn: () => void) => {
    nav.listeners.add(fn);
    return () => nav.listeners.delete(fn);
  };
  const go = (method: "push" | "replace") => (href: string) =>
    window.history[method === "push" ? "pushState" : "replaceState"](null, "", href);
  return {
    usePathname: () => "/pharmacy/sales",
    useSearchParams: () => new URLSearchParams(useSyncExternalStore(subscribe, () => nav.search)),
    useRouter: () => ({ push: go("push"), replace: go("replace") }),
  };
});
function renderWithProviders(ui: Parameters<typeof renderProviders>[0]) {
  window.history.replaceState(null, "", `/pharmacy/sales?${nav.search}`);
  return renderProviders(<NavigationGuardProvider>{ui}</NavigationGuardProvider>);
}
beforeEach(() => {
  for (const method of ["pushState", "replaceState"] as const) {
    const original = window.history[method].bind(window.history);
    vi.spyOn(window.history, method).mockImplementation((data, unused, url) => {
      original(data, unused, url);
      nav.search = new URL(window.location.href).search.slice(1);
      nav.listeners.forEach((listener) => listener());
    });
  }
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  nav.search = "";
});

const row = (over: object) => ({
  id: "0199e000-0000-7000-8000-000000000201",
  posted_at: "2026-11-04T06:30:00Z",
  invoice_number: "PH/2627/1",
  sale_date: "2026-11-04",
  customer: "walk_in",
  admission_bed_code: null,
  linkage: "none",
  currency: "INR",
  total_minor: 7500,
  discount_minor: 0,
  units: 2,
  credited_minor: 0,
  net_minor: 7500,
  admission_outstanding_minor: 0,
  ...over,
});

const admitted = row({
  id: "0199e000-0000-7000-8000-000000000202",
  invoice_number: "PH/2627/2",
  customer: "admission",
  admission_bed_code: "B-7",
  linkage: "admission",
  total_minor: 2500,
  credited_minor: 1250,
  net_minor: 1250,
  admission_outstanding_minor: 1250,
});

const summary: PharmacySaleSummary = {
  id: admitted.id,
  invoice_number: "PH/2627/2",
  sale_date: "2026-11-04",
  customer: "admission",
  admission_bed_code: "B-7",
  linkage: "admission",
  currency: "INR",
  total_minor: 2500,
  taxable_minor: 2232,
  cgst_minor: 134,
  sgst_minor: 134,
  discount_minor: 0,
  lines: [
    {
      line_number: 1,
      product_id: "0199e000-0000-7000-8000-000000000101",
      sku: "TEST-PCM-500",
      brand_name: "TEST Paracetamol",
      lot_number: "TEST-LOT-1",
      quantity_units: 2,
      returned_units: 1,
      discount_minor: 0,
      amount_minor: 2500,
      credited_minor: 1250,
    },
  ],
  charges: [],
  payments: { admission_account: 2500 },
  credit_notes: [
    {
      id: "0199e000-0000-7000-8000-000000000301",
      credit_note_number: "PH/2627/C1",
      credit_note_date: "2026-11-05",
      total_minor: 1250,
      refunds: { admission_account: 1250 },
    },
  ],
  more_credit_notes: false,
  credited_minor: 1250,
  net_minor: 1250,
  collected_minor: 0,
  refunded_minor: 0,
  admission_billed_minor: 2500,
  admission_reversed_minor: 1250,
};

function serve(rows: unknown[] | "fail" | "refuse") {
  const fetcher = vi.fn<(url: string) => Promise<Response>>(async (url) =>
    url.includes("/summary")
      ? new Response(JSON.stringify(summary))
      : rows === "fail"
        ? new Response(JSON.stringify({ error: "internal", message: "no" }), { status: 500 })
        : rows === "refuse"
          ? new Response(JSON.stringify({ error: "forbidden", message: "no" }), { status: 403 })
          : new Response(
              JSON.stringify({ data: rows, page: { next_cursor: null, has_more: false } }),
            ),
  );
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

const worklistCalls = (fetcher: ReturnType<typeof serve>) =>
  fetcher.mock.calls.map(([url]) => String(url)).filter((u) => u.includes("/sales/worklist"));

describe("SalesWorklist", () => {
  it("lists today's sales with who they were for and what is left", async () => {
    const fetcher = serve([admitted, row({})]);
    renderWithProviders(<SalesWorklist scope={scope} today="2026-11-04" />);
    const list = await screen.findByRole("list", { name: "Sales" });
    expect(worklistCalls(fetcher)[0]).toContain("/sales/worklist?from=2026-11-04&to=2026-11-04");
    const [first, second] = within(list).getAllByRole("listitem");
    expect(first).toHaveTextContent("PH/2627/2 · 2026-11-04");
    expect(first).toHaveTextContent("Admission, bed B-7 · 2 units · INR 25.00");
    expect(first).toHaveTextContent(
      "Credited INR 12.50, net INR 12.50; on the admission account INR 12.50",
    );
    expect(second).toHaveTextContent("Walk-in");
    expect(second).toHaveTextContent("Nothing credited");
    expect(second).not.toHaveTextContent("admission account");
  });

  it("finds an invoice exactly and narrows by who the sale was for", async () => {
    const fetcher = serve([]);
    const { user } = renderWithProviders(<SalesWorklist scope={scope} today="2026-11-04" />);
    await screen.findByText("No sales from 2026-11-04 to 2026-11-04.");
    await user.type(screen.getByLabelText("Invoice number"), " ph/2627/9 ");
    await user.click(screen.getByRole("button", { name: "Show sales" }));
    expect(await screen.findByText("No sale with invoice PH/2627/9.")).toBeVisible();
    expect(worklistCalls(fetcher).at(-1)).toContain("/sales/worklist?invoice=PH%2F2627%2F9");
    expect(worklistCalls(fetcher).at(-1)).not.toContain("from=");
  });

  it("opens a sale's statement beside the list", async () => {
    serve([admitted]);
    const { user } = renderWithProviders(<SalesWorklist scope={scope} today="2026-11-04" />);
    await user.click(await screen.findByRole("button", { name: "Open invoice PH/2627/2" }));
    expect(await screen.findByRole("table", { name: "Lines and charges" })).toBeVisible();
    expect(screen.getByText("On the admission account").nextSibling).toHaveTextContent(
      "INR 12.50 (billed INR 25.00, reversed INR 12.50)",
    );
  });

  it("refuses an invoice number longer than any issued, without asking the server", async () => {
    const fetcher = serve([]);
    const { user } = renderWithProviders(<SalesWorklist scope={scope} today="2026-11-04" />);
    await screen.findByText("No sales from 2026-11-04 to 2026-11-04.");
    const calls = worklistCalls(fetcher).length;
    await user.type(screen.getByLabelText("Invoice number"), "ABCD/2627/123456789");
    await user.click(screen.getByRole("button", { name: "Show sales" }));
    expect(await screen.findByText("An invoice number is at most 16 characters")).toBeVisible();
    expect(worklistCalls(fetcher)).toHaveLength(calls);
  });

  it("steps through the new rows after the filter changes with a sale open", async () => {
    const third = row({ id: "0199e000-0000-7000-8000-000000000203", invoice_number: "PH/2627/3" });
    let calls = 0;
    // The second filter's rows arrive a moment later, as a real load does.
    vi.stubGlobal(
      "fetch",
      vi.fn<(url: string) => Promise<Response>>(async (url) => {
        if (url.includes("/summary")) return new Response(JSON.stringify(summary));
        calls += 1;
        if (calls > 1) await new Promise((r) => setTimeout(r, 30));
        return new Response(
          JSON.stringify({
            data: calls > 1 ? [admitted, third] : [admitted],
            page: { next_cursor: null, has_more: false },
          }),
        );
      }),
    );
    const { user } = renderWithProviders(<SalesWorklist scope={scope} today="2026-11-04" />);
    await user.click(await screen.findByRole("button", { name: "Open invoice PH/2627/2" }));
    await screen.findByRole("table", { name: "Lines and charges" });
    await user.clear(screen.getByLabelText("From"));
    await user.type(screen.getByLabelText("From"), "2026-11-01");
    await user.click(screen.getByRole("button", { name: "Show sales" }));
    await screen.findByRole("button", { name: "Open invoice PH/2627/3" });
    await waitFor(() => expect(screen.getByRole("button", { name: "Next sale" })).toBeEnabled());
  });

  it("shows a sale cited to an OPD visit and filters by what sales are linked to", async () => {
    const fetcher = serve([
      row({
        id: "0199e000-0000-7000-8000-000000000204",
        invoice_number: "PH/2627/4",
        customer: "patient",
        linkage: "opd",
      }),
    ]);
    const { user } = renderWithProviders(<SalesWorklist scope={scope} today="2026-11-04" />);
    const item = await screen.findByRole("listitem");
    expect(item).toHaveTextContent("Registered patient, for an OPD visit");
    await chooseOption(user, screen.getByRole("combobox", { name: "Linked to" }), "OPD visit");
    await user.click(screen.getByRole("button", { name: "Show sales" }));
    await waitFor(() => expect(worklistCalls(fetcher).at(-1)).toContain("&linkage=opd"));
  });

  it("reports a failed load", async () => {
    serve("fail");
    renderWithProviders(<SalesWorklist scope={scope} today="2026-11-04" />);
    expect(await screen.findByText("Could not load sales.")).toBeVisible();
  });

  it.each([403, 404])(
    "shows a sale refused with %i as not found, with no retry",
    async (status) => {
      vi.stubGlobal(
        "fetch",
        vi.fn<(url: string) => Promise<Response>>(async (url) =>
          url.includes("/summary")
            ? new Response(JSON.stringify({ error: "forbidden", message: "no" }), { status })
            : new Response(
                JSON.stringify({ data: [admitted], page: { next_cursor: null, has_more: false } }),
              ),
        ),
      );
      const { user } = renderWithProviders(<SalesWorklist scope={scope} today="2026-11-04" />);
      await user.click(await screen.findByRole("button", { name: "Open invoice PH/2627/2" }));
      expect(
        await screen.findByText("This sale was not found, or you do not have access."),
      ).toBeVisible();
      expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
    },
  );

  it("announces an empty period, and shows a refusal with no retry", async () => {
    serve([]);
    const { unmount } = renderWithProviders(<SalesWorklist scope={scope} today="2026-11-04" />);
    expect(await screen.findByText("No sales from 2026-11-04 to 2026-11-04.")).toHaveAttribute(
      "role",
      "status",
    );
    unmount();
    serve("refuse");
    renderWithProviders(<SalesWorklist scope={scope} today="2026-11-04" />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "You don't have access to this outlet's reports.",
    );
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
  });
});

describe("the sales worklist's CSV (#1060)", () => {
  it("downloads the period's sales, and offers no file of one invoice", async () => {
    vi.stubGlobal(
      "URL",
      Object.assign(class extends URL {}, {
        createObjectURL: vi.fn(() => "blob:worklist"),
        revokeObjectURL: vi.fn(),
      }),
    );
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    const fetcher = serve([row({})]);
    const json = fetcher.getMockImplementation()!;
    fetcher.mockImplementation(async (url: string) =>
      url.includes("/sales/worklist.csv?")
        ? new Response("period_from\r\n", {
            headers: {
              "Content-Type": "text/csv; charset=utf-8",
              "Content-Disposition": `attachment; filename="pharmacy-sales-worklist-${scope.outletId}-2026-11-04-2026-11-04.csv"`,
            },
          })
        : json(url),
    );
    const { user } = renderWithProviders(<SalesWorklist scope={scope} today="2026-11-04" />);
    expect(
      await screen.findByText("A download holding an admission sale is recorded."),
    ).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Download report CSV" }));
    expect(await screen.findByText("CSV download started.")).toBeVisible();
    expect(
      fetcher.mock.calls.map(([url]) => String(url)).filter((u) => u.includes(".csv?")),
    ).toEqual([expect.stringContaining("/sales/worklist.csv?from=2026-11-04&to=2026-11-04")]);

    await user.type(screen.getByLabelText("Invoice number"), "PH/2627/1");
    await user.click(screen.getByRole("button", { name: "Show sales" }));
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Download report CSV" })).toBeNull(),
    );
  });
});

describe("SaleStatement", () => {
  it("states the lines, payments, credit notes and balances", () => {
    renderProviders(<SaleStatement summary={summary} />);
    const line = screen.getByRole("row", { name: /TEST Paracetamol/ });
    expect(line).toHaveTextContent("TEST-PCM-500, lot TEST-LOT-1");
    expect(line).toHaveTextContent("21INR 25.00INR 12.50");
    expect(screen.getByText("Paid by").nextSibling).toHaveTextContent(
      "Admission account INR 25.00",
    );
    expect(screen.getByText("Net").nextSibling).toHaveTextContent("INR 12.50");
    expect(screen.getByText(/PH\/2627\/C1/)).toHaveTextContent(
      "PH/2627/C1 · 2026-11-05 · INR 12.50 refunded Admission account INR 12.50",
    );
    expect(screen.queryByText(/More credit notes/)).toBeNull();
  });

  it("says when more credit notes exist than are listed, and shows no admission line for a walk-in", () => {
    renderProviders(
      <SaleStatement
        summary={{
          ...summary,
          customer: "walk_in",
          admission_bed_code: null,
          more_credit_notes: true,
        }}
      />,
    );
    expect(
      screen.getByText("More credit notes than are listed here; the totals above count them all."),
    ).toBeVisible();
    expect(screen.queryByText("On the admission account")).toBeNull();
  });
});
