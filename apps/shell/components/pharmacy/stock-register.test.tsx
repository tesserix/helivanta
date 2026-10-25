import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders as render } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { chooseOption } from "../../test/select";
import { StockRegister } from "./stock-register";
import { formatZonedDateTime } from "@helivanta/api";
import { useState } from "react";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const location = "0199e000-0000-7000-8000-000000000004";
const batch = "0199e000-0000-7000-8000-000000000005";
const product = "0199e000-0000-7000-8000-000000000003";
const row = {
  id: "0199e000-0000-7000-8000-000000000006",
  created_at: "2026-10-17T10:00:00Z",
  location_id: location,
  batch_id: batch,
  product_id: product,
  location_code: "TEST-Q",
  location_name: "TEST Receiving",
  storage_address: {
    zone_code: "TEST-Z1",
    aisle_code: "",
    rack_code: "TEST-R1",
    shelf_code: "TEST-S1",
  },
  sku: "TEST-500",
  brand_name: "TEST capsule",
  unit_of_issue: "capsule",
  lot_number: "TEST-LOT",
  expires_on: "2026-11-11",
  stock_status: "quarantine",
  expiry_status: "unexpired",
  expiry_time_zone: "Asia/Kolkata",
  expiry_local_date: "2026-10-17",
  days_to_expiry: 25,
  expiry_band: "days_30",
  product_status: "active",
  regulatory_status: "pending_review",
  on_hand_units: 0,
  receipt_units: "12",
  correction_units: "-12",
  dispensable_units: 0,
};
const page = (data: unknown[], cursor: string | null = null) => ({
  data,
  page: { has_more: cursor !== null, next_cursor: cursor },
});
// A live address: the filters and the open position are the URL (#1303 E3).
const nav = vi.hoisted(() => ({
  search: "",
  listeners: new Set<() => void>(),
  calls: [] as { method: "push" | "replace"; href: string }[],
}));
vi.mock("next/navigation", async () => {
  const { useSyncExternalStore } = await import("react");
  const subscribe = (fn: () => void) => {
    nav.listeners.add(fn);
    return () => nav.listeners.delete(fn);
  };
  const go = (method: "push" | "replace") => (href: string) => {
    nav.calls.push({ method, href });
    nav.search = href.split("?")[1] ?? "";
    nav.listeners.forEach((fn) => fn());
  };
  return {
    usePathname: () => "/pharmacy/stock",
    useSearchParams: () => new URLSearchParams(useSyncExternalStore(subscribe, () => nav.search)),
    useRouter: () => ({ push: go("push"), replace: go("replace") }),
  };
});
// The register guards unsaved edits through the shell's provider.
const renderWithProviders = (ui: React.ReactElement) =>
  render(<NavigationGuardProvider>{ui}</NavigationGuardProvider>);
const reconciliationPane = () => screen.queryByRole("region", { name: /^Stock reconciliation/ });
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  nav.search = "";
  nav.calls.length = 0;
});

function fixture() {
  const fetcher = vi.fn(async (url: string) => {
    if (url.includes("/stock/locations"))
      return new Response(
        JSON.stringify(page([{ id: location, code: "TEST-Q", name: "TEST Receiving" }])),
      );
    if (url.includes("/catalogue/products"))
      return new Response(
        JSON.stringify(page([{ id: product, sku: "TEST-500", brand_name: "TEST capsule" }])),
      );
    if (url.includes("/stock/reconciliation")) {
      const query = new URL(url, "http://localhost").searchParams;
      return new Response(
        JSON.stringify({
          location_id: location,
          batch_id: batch,
          from: query.get("from") ?? "1970-01-01T00:00:00Z",
          as_of: query.get("as_of"),
          opening_units: 10,
          receipt_units: "2",
          correction_units: "-12",
          closing_units: 0,
          movement_count: 2,
        }),
      );
    }
    if (url.includes("/stock/movements")) return new Response(JSON.stringify(page([])));
    return new Response(JSON.stringify(page([row])));
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

it("downloads all matching stock using the current snapshot and expected tenant", async () => {
  vi.spyOn(Date, "now").mockReturnValue(Date.parse("2026-10-17T12:00:00Z"));
  const fallback = fixture().getMockImplementation()!;
  const fetcher = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>(async (url) => {
    if (url.includes("/stock/positions.csv?"))
      return new Response("sku,on_hand_units\r\nTEST-500,0\r\n", {
        headers: {
          "Content-Type": "text/csv; charset=utf-8",
          "Content-Disposition": 'attachment; filename="pharmacy-stock-report.csv"',
        },
      });
    return fallback(url);
  });
  vi.stubGlobal("fetch", fetcher);
  const create = vi.fn(() => "blob:test-stock-export");
  const revoke = vi.fn();
  vi.stubGlobal(
    "URL",
    Object.assign(class extends URL {}, { createObjectURL: create, revokeObjectURL: revoke }),
  );
  const download = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
  const { user } = renderWithProviders(<StockRegister scope={scope} timeZone="Asia/Kolkata" />);
  await screen.findByRole("table");
  await chooseOption(
    user,
    screen.getByRole("combobox", { name: "Filter stock location" }),
    /^TEST-Q · TEST Receiving/,
  );
  await chooseOption(
    user,
    screen.getByRole("combobox", { name: "Filter expiry at snapshot" }),
    "Due within 30 days (including today)",
  );
  await user.click(screen.getByRole("button", { name: "Download CSV" }));
  await waitFor(() => expect(download).toHaveBeenCalledOnce());
  const call = fetcher.mock.calls.find(([path]) => path.includes("/positions.csv?"))!;
  const params = new URL(call[0], "http://localhost").searchParams;
  expect(params.get("as_of")).toBe("2026-10-17T12:00:00.000Z");
  expect(params.get("location_id")).toBe(location);
  expect(params.get("expiry")).toBe("due_30");
  expect(params.has("cursor")).toBe(false);
  expect(new Headers(call[1]?.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(call[1]?.cache).toBe("no-store");
  expect(call[1]?.signal).toBeInstanceOf(AbortSignal);
  expect(create).toHaveBeenCalledOnce();
  expect(revoke).toHaveBeenCalledWith("blob:test-stock-export");
  expect(await screen.findByRole("status", { name: "Stock CSV export" })).toHaveTextContent(
    "CSV download started",
  );
});

it("retains zero stock and opens a focused reconciliation using one frozen snapshot", async () => {
  vi.spyOn(Date, "now").mockReturnValue(Date.parse("2026-10-17T12:00:00Z"));
  const fetcher = fixture();
  const { user } = renderWithProviders(<StockRegister scope={scope} />);
  const table = await screen.findByRole("table", {
    name: "Stock by location and manufacturer lot",
  });
  expect(within(table).getByText("TEST-LOT")).toBeInTheDocument();
  expect(within(table).getByText("Zone TEST-Z1 · Rack TEST-R1 · Shelf TEST-S1")).toBeVisible();
  expect(within(table).getByText("Base unit: capsule")).toBeVisible();
  expect(within(table).getByText("Expires in 25 days")).toBeVisible();
  expect(within(table).getByText("As of 2026-10-17 · Asia/Kolkata")).toBeVisible();
  expect(screen.getByRole("region", { name: "Scrollable stock register" })).toHaveAttribute(
    "tabindex",
    "0",
  );
  expect(within(table).getAllByText("0")).toHaveLength(2);
  await user.click(screen.getByRole("button", { name: "Inspect TEST-500 lot TEST-LOT at TEST-Q" }));
  expect(
    await screen.findByRole("heading", { name: "TEST-500 · TEST-LOT · TEST-Q" }),
  ).toHaveFocus();
  expect(await screen.findByText("Closing stock")).toBeInTheDocument();
  const positions = fetcher.mock.calls.find(([url]) => url.includes("/stock/positions"))![0];
  const reconciliation = fetcher.mock.calls.find(([url]) =>
    url.includes("/stock/reconciliation"),
  )![0];
  expect(new URL(reconciliation, "http://localhost").searchParams.get("as_of")).toBe(
    new URL(positions, "http://localhost").searchParams.get("as_of"),
  );
  expect(
    screen.queryByRole("button", { name: /release|reserve|dispense/i }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Close reconciliation" }));
  // Focus returns on the next frame, once the list has its full columns back.
  await waitFor(() =>
    expect(
      screen.getByRole("button", { name: "Inspect TEST-500 lot TEST-LOT at TEST-Q" }),
    ).toHaveFocus(),
  );
});

it("keeps filters and cursor pages on the same snapshot and replaces detail when filtering", async () => {
  const base = fixture();
  const fetcher = vi.fn(async (url: string) => {
    if (url.includes("/stock/positions"))
      return new Response(
        JSON.stringify(
          new URL(url, "http://localhost").searchParams.has("cursor")
            ? page([{ ...row, id: product, lot_number: "TEST-LOT-2" }])
            : page([row], "TEST cursor/+"),
        ),
      );
    return base(url);
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(<StockRegister scope={scope} />);
  await user.click(
    await screen.findByRole("button", { name: "Inspect TEST-500 lot TEST-LOT at TEST-Q" }),
  );
  await chooseOption(
    user,
    screen.getByLabelText("Filter stock location"),
    "TEST-Q · TEST Receiving · Address not recorded",
  );
  expect(reconciliationPane()).not.toBeInTheDocument();
  await chooseOption(
    user,
    screen.getByLabelText("Filter stock product"),
    "TEST-500 · TEST capsule",
  );
  await chooseOption(
    user,
    screen.getByLabelText("Filter expiry at snapshot"),
    "Due within 30 days (including today)",
  );
  await chooseOption(user, screen.getByLabelText("Filter stock balance"), "Positive stock on hand");
  await user.click(await screen.findByRole("button", { name: "Load more stock positions" }));
  expect(await screen.findByText("TEST-LOT-2")).toBeInTheDocument();
  const request = fetcher.mock.calls
    .map(([url]) => new URL(url, "http://localhost"))
    .find((url) => url.searchParams.has("cursor"))!;
  expect(Object.fromEntries(request.searchParams)).toMatchObject({
    location_id: location,
    product_id: product,
    expiry: "due_30",
    balance: "on_hand",
    cursor: "TEST cursor/+",
  });
  expect(request.searchParams.get("as_of")).toBe(
    new URL(
      fetcher.mock.calls.find(([url]) => url.includes("/stock/positions"))![0],
      "http://localhost",
    ).searchParams.get("as_of"),
  );
  expect(screen.getByText(/2 loaded stock positions/)).toBeInTheDocument();
});

it("applies UTC periods to reconciliation and ledger, rejects future starts, and clears the start", async () => {
  vi.spyOn(Date, "now").mockReturnValue(Date.parse("2026-10-17T12:00:00Z"));
  const fetcher = fixture();
  const { user } = renderWithProviders(<StockRegister scope={scope} />);
  await user.click(
    await screen.findByRole("button", { name: "Inspect TEST-500 lot TEST-LOT at TEST-Q" }),
  );
  await screen.findByText("Closing stock");
  const input = screen.getByLabelText("Period starts at (UTC, optional)");
  fireEvent.change(input, { target: { value: "2026-10-17T10:30" } });
  await user.click(screen.getByRole("button", { name: "Apply period" }));
  await waitFor(() =>
    expect(
      fetcher.mock.calls.some(
        ([url]) =>
          url.includes("/stock/movements") &&
          new URL(url, "http://localhost").searchParams.get("from") === "2026-10-17T10:30:00.000Z",
      ),
    ).toBe(true),
  );
  const detail = reconciliationPane()!;
  expect(
    within(detail).getByText(/Opening \+ receipts \+ corrections = closing stock/),
  ).toBeInTheDocument();
  const reads = fetcher.mock.calls.length;
  fireEvent.change(input, { target: { value: "2026-10-17T12:01" } });
  await user.click(screen.getByRole("button", { name: "Apply period" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("at or before the snapshot");
  expect(fetcher.mock.calls).toHaveLength(reads);
  fireEvent.change(input, { target: { value: "" } });
  await user.click(screen.getByRole("button", { name: "Apply period" }));
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.getByText(/All recorded history through/)).toBeInTheDocument();
});

it("refreshes the snapshot explicitly and hides prior rows and reconciliation after a failed refresh", async () => {
  const now = vi.spyOn(Date, "now").mockReturnValue(Date.parse("2026-10-17T12:00:00Z"));
  const base = fixture();
  let fail = false;
  const fetcher = vi.fn(async (url: string) =>
    fail && url.includes("/stock/positions")
      ? new Response(
          JSON.stringify({ error: "unavailable", message: "TEST snapshot unavailable" }),
          { status: 503 },
        )
      : base(url),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(<StockRegister scope={scope} />);
  await user.click(
    await screen.findByRole("button", { name: "Inspect TEST-500 lot TEST-LOT at TEST-Q" }),
  );
  await screen.findByText("Closing stock");
  now.mockReturnValue(Date.parse("2026-10-17T12:01:00Z"));
  fail = true;
  await user.click(screen.getByRole("button", { name: "Refresh snapshot" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Previously loaded rows are hidden");
  expect(screen.queryByRole("table")).not.toBeInTheDocument();
  expect(reconciliationPane()).not.toBeInTheDocument();
  expect(
    new URL(
      fetcher.mock.calls.filter(([url]) => url.includes("/stock/positions")).at(-1)![0],
      "http://localhost",
    ).searchParams.get("as_of"),
  ).toBe("2026-10-17T12:01:00.000Z");
  fail = false;
  await user.click(screen.getByRole("button", { name: "Try again" }));
  expect(await screen.findByRole("table")).toBeInTheDocument();
});

function ScopeHarness({ field }: { field: keyof typeof scope }) {
  const [current, setCurrent] = useState(scope);
  return (
    <>
      <StockRegister scope={current} />
      <button
        onClick={() => setCurrent({ ...scope, [field]: "0199e000-0000-7000-8000-000000000099" })}
      >
        Switch scope
      </button>
    </>
  );
}

it("renders exact cumulative decimal totals without losing units beyond Number safety", async () => {
  const base = fixture();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("/stock/positions"))
        return new Response(
          JSON.stringify(
            page([
              { ...row, receipt_units: "9007199254740993", correction_units: "-9007199254740993" },
            ]),
          ),
        );
      if (url.includes("/stock/reconciliation"))
        return new Response(
          JSON.stringify({
            location_id: location,
            batch_id: batch,
            from: "1970-01-01T00:00:00Z",
            as_of: new URL(url, "http://localhost").searchParams.get("as_of"),
            opening_units: 0,
            receipt_units: "9007199254740993",
            correction_units: "-9007199254740993",
            closing_units: 0,
            movement_count: 2,
          }),
        );
      return base(url);
    }),
  );
  const { user } = renderWithProviders(<StockRegister scope={scope} />);
  const exact = BigInt("9007199254740993").toLocaleString();
  expect(await screen.findByText(exact)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Inspect TEST-500 lot TEST-LOT at TEST-Q" }));
  // Beside an open position the rows drop the receipts column (#1303 D1); the
  // pane carries the exact figure.
  await waitFor(() => expect(within(reconciliationPane()!).getAllByText(exact)).toHaveLength(1));
  expect(screen.queryByText(BigInt("9007199254740992").toLocaleString())).not.toBeInTheDocument();
});

it("hides a previous period ledger and totals when the new reconciliation is denied", async () => {
  const base = fixture();
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method && init.method !== "GET") throw new Error("Read-only register mutated stock");
    if (
      url.includes("/stock/reconciliation") &&
      new URL(url, "http://localhost").searchParams.has("from")
    )
      return new Response(JSON.stringify({ error: "not_found", message: "Not found" }), {
        status: 404,
      });
    if (url.includes("/stock/movements"))
      return new Response(
        JSON.stringify(
          page([
            {
              id: product,
              source_reference: "TEST-PERIOD-LINE",
              kind: "receipt",
              quantity_units: 2,
              reason: "TEST received",
              actor_subject: "TEST-recorder",
              created_at: "2026-10-17T21:00:00.123456+11:00",
            },
          ]),
        ),
      );
    return base(url);
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(<StockRegister scope={scope} />);
  await user.click(
    await screen.findByRole("button", { name: "Inspect TEST-500 lot TEST-LOT at TEST-Q" }),
  );
  expect(await screen.findByText(/TEST-PERIOD-LINE/)).toBeInTheDocument();
  expect(
    screen.getByText(formatZonedDateTime("2026-10-17T21:00:00.123456+11:00", "UTC")),
  ).toHaveAttribute("datetime", "2026-10-17T21:00:00.123456+11:00");
  fireEvent.change(screen.getByLabelText("Period starts at (UTC, optional)"), {
    target: { value: "2020-01-01T00:00" },
  });
  await user.click(screen.getByRole("button", { name: "Apply period" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not reconcile");
  expect(screen.queryByText(/TEST-PERIOD-LINE/)).not.toBeInTheDocument();
  expect(screen.queryByText("Closing stock")).not.toBeInTheDocument();
  expect(fetcher.mock.calls.every(([, init]) => !init?.method || init.method === "GET")).toBe(true);
});

it("uses the persisted outlet zone for snapshots, period input and reloads", async () => {
  vi.spyOn(Date, "now").mockReturnValue(Date.parse("2026-10-17T12:00:00Z"));
  const fetcher = fixture();
  function ZoneHarness() {
    const [timeZone, setZone] = useState("Asia/Kolkata");
    return (
      <>
        <button onClick={() => setZone("Asia/Tashkent")}>Change outlet time zone</button>
        <StockRegister scope={scope} timeZone={timeZone} />
      </>
    );
  }
  const { user } = renderWithProviders(<ZoneHarness />);
  await user.click(
    await screen.findByRole("button", { name: "Inspect TEST-500 lot TEST-LOT at TEST-Q" }),
  );
  expect(
    screen.getByText(formatZonedDateTime("2026-10-17T12:00:00.000Z", "Asia/Kolkata")),
  ).toBeVisible();
  fireEvent.change(screen.getByLabelText("Period starts at (Asia/Kolkata, optional)"), {
    target: { value: "2026-10-17T10:30" },
  });
  await user.click(screen.getByRole("button", { name: "Apply period" }));
  await waitFor(() =>
    expect(
      fetcher.mock.calls.some(
        ([url]) =>
          new URL(url, "http://localhost").searchParams.get("from") === "2026-10-17T05:00:00.000Z",
      ),
    ).toBe(true),
  );
  await user.click(screen.getByRole("button", { name: "Change outlet time zone" }));
  expect(
    screen.queryByLabelText("Period starts at (Asia/Kolkata, optional)"),
  ).not.toBeInTheDocument();
  await user.click(
    await screen.findByRole("button", { name: "Inspect TEST-500 lot TEST-LOT at TEST-Q" }),
  );
  expect(screen.getByLabelText("Period starts at (Asia/Tashkent, optional)")).toHaveValue("");
});

it("refuses invalid outlet time zones before querying stock", () => {
  const fetcher = fixture();
  renderWithProviders(<StockRegister scope={scope} timeZone="Wrong/Zone" />);
  expect(screen.getByRole("alert")).toHaveTextContent("outlet time zone");
  expect(fetcher).not.toHaveBeenCalled();
});

it.each(["tenantId", "pharmacyId", "outletId"] as const)(
  "clears private rows and detail when %s changes to a denied scope",
  async (field) => {
    const base = fixture();
    const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
      const tenant = new Headers(init?.headers).get("X-Helivanta-Expected-Tenant");
      if (tenant !== scope.tenantId || url.includes("0199e000-0000-7000-8000-000000000099"))
        return new Response(JSON.stringify({ error: "not_found", message: "Not found" }), {
          status: 404,
        });
      return base(url);
    });
    vi.stubGlobal("fetch", fetcher);
    const { user } = renderWithProviders(<ScopeHarness field={field} />);
    await user.click(
      await screen.findByRole("button", { name: "Inspect TEST-500 lot TEST-LOT at TEST-Q" }),
    );
    await screen.findByText("Closing stock");
    await user.click(screen.getByRole("button", { name: "Switch scope" }));
    expect(await screen.findByText(/Could not load this stock snapshot/)).toBeInTheDocument();
    expect(screen.queryByText("TEST-LOT")).not.toBeInTheDocument();
    expect(reconciliationPane()).not.toBeInTheDocument();
  },
);

// #1303 E3: a stock position opens beside the register, named by its lot
// and location in the URL.
describe("a position opens beside the register", () => {
  const second = {
    ...row,
    id: "0199e000-0000-7000-8000-000000000007",
    batch_id: "0199e000-0000-7000-8000-000000000008",
    lot_number: "TEST-LOT-B",
  };
  const two = (rows = [row, second]) => {
    const base = fixture();
    const fetcher = vi.fn(async (url: string) => {
      const query = new URL(url, "http://localhost").searchParams;
      if (url.includes("/stock/positions?")) {
        const lot = query.get("batch_id");
        if (lot) return new Response(JSON.stringify(page(rows.filter((r) => r.batch_id === lot))));
        return new Response(JSON.stringify(page(rows)));
      }
      return base(url);
    });
    vi.stubGlobal("fetch", fetcher);
    return fetcher;
  };

  it("reopens the position in the URL, read by its lot and location only", async () => {
    nav.search = `expiry=due_30&open=${batch}&at=${location}`;
    const fetcher = two();
    renderWithProviders(<StockRegister scope={scope} />);
    const heading = await screen.findByRole("heading", { name: "TEST-500 · TEST-LOT · TEST-Q" });
    await waitFor(() => expect(heading).toHaveFocus());
    const read = fetcher.mock.calls
      .map(([url]) => new URL(url, "http://localhost"))
      .find((url) => url.searchParams.has("batch_id"))!;
    expect(Object.fromEntries(read.searchParams)).toEqual({
      as_of: read.searchParams.get("as_of"),
      batch_id: batch,
      location_id: location,
    });
  });

  it("says a position that is not found is not found, and can be closed", async () => {
    nav.search = `open=0199e000-0000-7000-8000-0000000000ff&at=${location}`;
    two();
    const { user } = renderWithProviders(<StockRegister scope={scope} />);
    expect(
      await screen.findByText("This record was not found, or you do not have access."),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Close reconciliation" }));
    expect(new URLSearchParams(nav.search).has("open")).toBe(false);
  });

  it("closes the position when a filter changes", async () => {
    nav.search = `open=${batch}&at=${location}`;
    two();
    const { user } = renderWithProviders(<StockRegister scope={scope} />);
    await screen.findByRole("heading", { name: "TEST-500 · TEST-LOT · TEST-Q" });
    await chooseOption(
      user,
      screen.getByLabelText("Filter stock balance"),
      "Positive stock on hand",
    );
    await waitFor(() => expect(new URLSearchParams(nav.search).get("balance")).toBe("on_hand"));
    expect(new URLSearchParams(nav.search).has("open")).toBe(false);
    expect(new URLSearchParams(nav.search).has("at")).toBe(false);
  });

  it("steps to the next position, and Refresh snapshot keeps it open", async () => {
    const now = vi.spyOn(Date, "now").mockReturnValue(Date.parse("2026-10-17T12:00:00Z"));
    let rows = [row, second];
    const base = fixture();
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        const lot = new URL(url, "http://localhost").searchParams.get("batch_id");
        if (url.includes("/stock/positions?"))
          return new Response(
            JSON.stringify(page(lot ? rows.filter((r) => r.batch_id === lot) : rows)),
          );
        return base(url);
      }),
    );
    const { user } = renderWithProviders(<StockRegister scope={scope} />);
    await user.click(
      await screen.findByRole("button", { name: "Inspect TEST-500 lot TEST-LOT at TEST-Q" }),
    );
    await user.click(screen.getByRole("button", { name: "Next position" }));
    expect(
      await screen.findByRole("heading", { name: "TEST-500 · TEST-LOT-B · TEST-Q" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Next position" })).toBeDisabled();
    // A third lot arrives; the refreshed rows put it after the open one.
    rows = [
      row,
      second,
      {
        ...second,
        id: "0199e000-0000-7000-8000-000000000009",
        batch_id: "0199e000-0000-7000-8000-00000000000a",
        lot_number: "TEST-LOT-C",
      },
    ];
    now.mockReturnValue(Date.parse("2026-10-17T12:05:00Z"));
    await user.click(screen.getByRole("button", { name: "Refresh snapshot" }));
    expect(
      await screen.findByRole("heading", { name: "TEST-500 · TEST-LOT-B · TEST-Q" }),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Next position" })).toBeEnabled(),
    );
  });

  it("does not ask before leaving over a typed period start: it is a query, not an edit", async () => {
    two();
    const { user } = renderWithProviders(<StockRegister scope={scope} />);
    await user.click(
      await screen.findByRole("button", { name: "Inspect TEST-500 lot TEST-LOT at TEST-Q" }),
    );
    fireEvent.change(await screen.findByLabelText("Period starts at (UTC, optional)"), {
      target: { value: "2026-10-17T10:30" },
    });
    await user.click(screen.getByRole("button", { name: "Next position" }));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(
      await screen.findByRole("heading", { name: "TEST-500 · TEST-LOT-B · TEST-Q" }),
    ).toBeInTheDocument();
  });
});
