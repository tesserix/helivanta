import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
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
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
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

it("retains zero stock and opens a focused reconciliation using one frozen snapshot", async () => {
  vi.spyOn(Date, "now").mockReturnValue(Date.parse("2026-10-17T12:00:00Z"));
  const fetcher = fixture();
  const { user } = renderWithProviders(<StockRegister scope={scope} />);
  const table = await screen.findByRole("table", {
    name: "Stock by location and manufacturer lot",
  });
  expect(within(table).getByText("TEST-LOT")).toBeInTheDocument();
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
  expect(
    screen.getByRole("button", { name: "Inspect TEST-500 lot TEST-LOT at TEST-Q" }),
  ).toHaveFocus();
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
    "TEST-Q · TEST Receiving",
  );
  expect(
    screen.queryByRole("region", { name: "Selected stock reconciliation" }),
  ).not.toBeInTheDocument();
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
  await user.click(await screen.findByRole("button", { name: "Load more" }));
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
  const detail = screen.getByRole("region", { name: "Selected stock reconciliation" });
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
  expect(
    screen.queryByRole("region", { name: "Selected stock reconciliation" }),
  ).not.toBeInTheDocument();
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
  await waitFor(() => expect(screen.getAllByText(exact)).toHaveLength(2));
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
        <button onClick={() => setZone("Asia/Karachi")}>Change outlet time zone</button>
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
  expect(screen.getByLabelText("Period starts at (Asia/Karachi, optional)")).toHaveValue("");
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
    expect(
      screen.queryByRole("region", { name: "Selected stock reconciliation" }),
    ).not.toBeInTheDocument();
  },
);
