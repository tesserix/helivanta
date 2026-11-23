import { afterEach, expect, it, vi } from "vitest";
import {
  fetchStocktakeLines,
  readStocktake,
  recordStocktakeCount,
  readStocktakeCount,
  recordStocktakeDecision,
  readStocktakeDecision,
  fetchStocktakes,
  startStocktake,
  recoverStocktakeStart,
  stocktakeKey,
  type PharmacyStocktakeScope,
} from "./index";

const scope = {
  subject: "TEST stocktake actor",
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const take = "0199e000-0000-7000-8000-000000000002";
const command = "0199e000-0000-7000-8000-000000000003";
const input = {
  selection: "all" as const,
  product_ids: [],
  quarantine_location_id: command,
  reason: "TEST count",
};
const receipt = {
  ...input,
  id: take,
  outlet_id: scope.outletId,
  number: 1,
  started_by: scope.subject,
  started_at: "2026-11-20T00:00:00Z",
  snapshot_rows: 1,
  counted_rows: 0,
  difference_rows: 0,
  positive_difference_rows: 0,
  review_relationship: "starter",
  version: 0,
  state: "counting",
};
const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200 });
afterEach(() => vi.unstubAllGlobals());

const observed = {
  expected_version: 0,
  counts: [{ line_id: command, counted_units: 0 }],
  reason: "TEST reviewed physical observation",
};
const counted = {
  ...observed,
  id: command,
  stocktake_id: take,
  outlet_id: scope.outletId,
  version: 1,
  state: "counting",
  recorded_by: scope.subject,
  recorded_at: receipt.started_at,
};
const submitted = {
  decision: "submit" as const,
  expected_version: 1,
  reason: "TEST reviewed submission",
};
const decided = {
  ...submitted,
  extra_units_disposition: null,
  id: command,
  stocktake_id: take,
  outlet_id: scope.outletId,
  version: 2,
  state: "submitted",
  adjustment_count: 0,
  recorded_by: scope.subject,
  recorded_at: receipt.started_at,
};
const cancelled = { ...submitted, decision: "cancel" as const };
it.each([
  {
    operation: "start",
    method: "PUT",
    suffix: `/${take}`,
    body: receipt,
    call: (s: typeof scope) => startStocktake(s, take, input),
  },
  {
    operation: "recover start",
    method: "GET",
    suffix: `/${take}`,
    body: receipt,
    call: (s: typeof scope) => recoverStocktakeStart(s, take, input),
  },
  {
    operation: "status",
    method: "GET",
    suffix: `/${take}`,
    body: receipt,
    call: (s: typeof scope) => readStocktake(s, take),
  },
  {
    operation: "history",
    method: "GET",
    suffix: "?",
    body: { data: [], page: { has_more: false, next_cursor: null } },
    call: (s: typeof scope) => fetchStocktakes(s, null),
  },
  {
    operation: "lines",
    method: "GET",
    suffix: `/${take}/lines?`,
    body: { data: [], page: { has_more: false, next_cursor: null } },
    call: (s: typeof scope) => fetchStocktakeLines(s, take, null),
  },
  {
    operation: "count",
    method: "PUT",
    suffix: `/${take}/counts/${command}`,
    body: counted,
    call: (s: typeof scope) => recordStocktakeCount(s, take, command, observed),
  },
  {
    operation: "count receipt",
    method: "GET",
    suffix: `/${take}/counts/${command}`,
    body: counted,
    call: (s: typeof scope) => readStocktakeCount(s, take, command),
  },
  {
    operation: "submit",
    method: "PUT",
    suffix: `/${take}/decisions/${command}`,
    body: decided,
    call: (s: typeof scope) => recordStocktakeDecision(s, take, command, submitted),
  },
  {
    operation: "decision receipt",
    method: "GET",
    suffix: `/${take}/decisions/${command}`,
    body: decided,
    call: (s: typeof scope) => readStocktakeDecision(s, take, command),
  },
  {
    operation: "cancel",
    method: "PUT",
    suffix: `/${take}/cancellations/${command}`,
    body: { ...decided, ...cancelled, state: "cancelled" },
    call: (s: typeof scope) => recordStocktakeDecision(s, take, command, cancelled),
  },
])("uses only reviewed routes for operating $operation", async ({ method, suffix, body, call }) => {
  const operating = { ...scope, authority: "operating" as const };
  const fetch = vi.fn().mockResolvedValue(json(body));
  vi.stubGlobal("fetch", fetch);
  await call(operating);
  expect(fetch.mock.calls[0]?.[0]).toBe(
    `/api/v1/pharmacy/pharmacies/${scope.pharmacyId}/operating-locations/${scope.outletId}/stocktakes${suffix}`,
  );
  const request = fetch.mock.calls[0]?.[1] as RequestInit;
  expect(request.method ?? "GET").toBe(method);
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(request.cache).toBe("no-store");
});

it("separates reviewed cache keys from legacy, other accounts and other outlets", () => {
  const operating = { ...scope, authority: "operating" as const };
  expect(stocktakeKey(operating, take)).not.toEqual(stocktakeKey(scope, take));
  expect(stocktakeKey(operating, take)).not.toEqual(
    stocktakeKey({ ...operating, subject: "TEST other account" }, take),
  );
  expect(stocktakeKey(operating, take)).not.toEqual(
    stocktakeKey({ ...operating, outletId: take }, take),
  );
});

it.each([null, "", "unknown"])(
  "refuses invalid authority %s before traffic or cache use",
  async (authority) => {
    const invalid = { ...scope, authority } as unknown as PharmacyStocktakeScope;
    const fetch = vi.fn().mockResolvedValue(json(receipt));
    vi.stubGlobal("fetch", fetch);
    await expect(readStocktake(invalid, take)).rejects.toThrow();
    expect(fetch).not.toHaveBeenCalled();
    expect(() => stocktakeKey(invalid, take)).toThrow();
  },
);

it.each(["start", "count", "decision"] as const)(
  "refuses a %s write receipt recorded by another account",
  async (kind) => {
    const count = {
      expected_version: 0,
      counts: [{ line_id: command, counted_units: 0 }],
      reason: "TEST zero",
    };
    const decision = { decision: "submit" as const, expected_version: 0, reason: "TEST complete" };
    const value =
      kind === "start"
        ? { ...receipt, started_by: "TEST another account" }
        : {
            ...(kind === "count" ? count : decision),
            id: command,
            stocktake_id: take,
            outlet_id: scope.outletId,
            version: 1,
            state: kind === "count" ? "counting" : "submitted",
            adjustment_count: 0,
            recorded_by: "TEST another account",
            recorded_at: receipt.started_at,
          };
    if (kind === "count") delete (value as Record<string, unknown>).adjustment_count;
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(json(value)));
    const write =
      kind === "start"
        ? startStocktake(scope, take, input)
        : kind === "count"
          ? recordStocktakeCount(scope, take, command, count)
          : recordStocktakeDecision(scope, take, command, decision);
    await expect(write).rejects.toThrow(/receipt/i);
  },
);

it("sends cancellation through the cancellation-only continuation", async () => {
  const input = {
    decision: "cancel" as const,
    expected_version: 0,
    reason: "TEST abandoned count",
  };
  const fetch = vi.fn().mockResolvedValue(
    json({
      ...input,
      extra_units_disposition: null,
      id: command,
      stocktake_id: take,
      outlet_id: scope.outletId,
      version: 1,
      state: "cancelled",
      adjustment_count: 0,
      recorded_by: scope.subject,
      recorded_at: receipt.started_at,
    }),
  );
  vi.stubGlobal("fetch", fetch);
  await recordStocktakeDecision(scope, take, command, input);
  expect(fetch.mock.calls[0]?.[0]).toContain(`/stocktakes/${take}/cancellations/${command}`);
});

it("recovers a matching original start from current status without writing or adopting another actor", async () => {
  const current = { ...receipt, state: "submitted", version: 2, counted_rows: 1 };
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(json(current))
    .mockResolvedValueOnce(json({ ...current, review_relationship: "independent" }))
    .mockResolvedValueOnce(json({ ...current, reason: "TEST other start" }));
  vi.stubGlobal("fetch", fetch);
  expect(await recoverStocktakeStart(scope, take, input)).toEqual(current);
  expect(fetch.mock.calls[0]?.[1]?.method).toBeUndefined();
  await expect(recoverStocktakeStart(scope, take, input)).rejects.toThrow(/start/i);
  await expect(recoverStocktakeStart(scope, take, input)).rejects.toThrow(/start/i);
});

it("recovers the immutable count receipt and refuses another command or version", async () => {
  const saved = {
    id: command,
    stocktake_id: take,
    outlet_id: scope.outletId,
    expected_version: 0,
    version: 1,
    state: "counting",
    counts: [{ line_id: command, counted_units: 0 }],
    reason: "TEST empty shelf",
    recorded_by: scope.subject,
    recorded_at: "2026-11-20T00:00:00Z",
  };
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(json(saved))
    .mockResolvedValueOnce(json({ ...saved, id: take }))
    .mockResolvedValueOnce(json({ ...saved, version: 3 }));
  vi.stubGlobal("fetch", fetch);
  expect(await readStocktakeCount(scope, take, command)).toEqual(saved);
  expect(fetch.mock.calls[0]?.[0]).toContain(`/stocktakes/${take}/counts/${command}`);
  const request = fetch.mock.calls[0]?.[1] as RequestInit;
  expect(request.cache).toBe("no-store");
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  await expect(readStocktakeCount(scope, take, command)).rejects.toThrow(/receipt/i);
  await expect(readStocktakeCount(scope, take, command)).rejects.toThrow(/receipt/i);
});

it("loads bounded stocktake pages and rejects wrong scope or a broken continuation", async () => {
  const summary = {
    ...input,
    id: take,
    outlet_id: scope.outletId,
    number: 1,
    started_by: scope.subject,
    started_at: "2026-11-20T00:00:00Z",
    snapshot_rows: 1,
    state: "counting",
    version: 0,
  };
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(
      json({ data: [summary], page: { has_more: true, next_cursor: "TEST-next" } }),
    )
    .mockResolvedValueOnce(
      json({
        data: [{ ...summary, outlet_id: command }],
        page: { has_more: false, next_cursor: null },
      }),
    )
    .mockResolvedValueOnce(json({ data: [], page: { has_more: true, next_cursor: null } }));
  vi.stubGlobal("fetch", fetch);
  expect((await fetchStocktakes(scope, null)).data).toEqual([summary]);
  await expect(fetchStocktakes(scope, "TEST-next")).rejects.toThrow(/outlet/i);
  await expect(fetchStocktakes(scope, null)).rejects.toThrow(/continuation/i);
  expect(fetch.mock.calls[1]?.[0]).toContain("cursor=TEST-next");
});

it("retains a review command and recovers its original receipt after later revisions", async () => {
  const input = { decision: "submit" as const, expected_version: 1, reason: "TEST all counted" };
  const saved = {
    ...input,
    extra_units_disposition: null,
    id: command,
    stocktake_id: take,
    outlet_id: scope.outletId,
    version: 2,
    state: "submitted",
    adjustment_count: 0,
    recorded_by: scope.subject,
    recorded_at: "2026-11-20T00:00:00Z",
  };
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(json(saved))
    .mockResolvedValueOnce(json(saved))
    .mockResolvedValueOnce(json({ ...saved, state: "posted" }));
  vi.stubGlobal("fetch", fetch);
  expect(await recordStocktakeDecision(scope, take, command, input)).toEqual(saved);
  expect(await readStocktakeDecision(scope, take, command)).toEqual(saved);
  expect(fetch.mock.calls[1]?.[0]).toContain(`/stocktakes/${take}/decisions/${command}`);
  await expect(recordStocktakeDecision(scope, take, command, input)).rejects.toThrow(/receipt/i);
});

it("reads current progress and refuses a foreign or inconsistent stocktake", async () => {
  const current = {
    ...receipt,
    counted_rows: 1,
    difference_rows: 1,
    version: 2,
    state: "submitted",
  };
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValueOnce(json(current))
      .mockResolvedValueOnce(json({ ...current, outlet_id: command }))
      .mockResolvedValueOnce(json({ ...current, counted_rows: 2 }))
      .mockResolvedValueOnce(json({ ...current, positive_difference_rows: 2 })),
  );
  expect(await readStocktake(scope, take)).toEqual(current);
  await expect(readStocktake(scope, take)).rejects.toThrow(/stocktake/i);
  await expect(readStocktake(scope, take)).rejects.toThrow(/progress/i);
  await expect(readStocktake(scope, take)).rejects.toThrow(/progress/i);
});

it("pins a start to its tenant, outlet, command and normalized intent", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(json(receipt))
    .mockResolvedValueOnce(json({ ...receipt, outlet_id: take }));
  vi.stubGlobal("fetch", fetch);
  expect(await startStocktake(scope, take, input)).toEqual(receipt);
  expect(fetch.mock.calls[0]?.[0]).toBe(
    `/api/v1/pharmacy/pharmacies/${scope.pharmacyId}/outlets/${scope.outletId}/stock/stocktakes/${take}`,
  );
  const request = fetch.mock.calls[0]?.[1] as RequestInit;
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(request.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(JSON.parse(request.body as string)).toEqual(input);
  await expect(startStocktake(scope, take, input)).rejects.toThrow(/receipt/i);
});

it("rejects a count receipt with another command or different physical quantities", async () => {
  const count = {
    expected_version: 0,
    counts: [{ line_id: command, counted_units: 0 }],
    reason: "TEST zero shelf",
  };
  const saved = {
    ...count,
    id: command,
    stocktake_id: take,
    outlet_id: scope.outletId,
    version: 1,
    state: "counting",
    recorded_by: scope.subject,
    recorded_at: "2026-11-20T00:00:00Z",
  };
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(json(saved))
    .mockResolvedValueOnce(json({ ...saved, id: take }))
    .mockResolvedValueOnce(json({ ...saved, counts: [{ line_id: command, counted_units: 1 }] }));
  vi.stubGlobal("fetch", fetch);
  expect(await recordStocktakeCount(scope, take, command, count)).toEqual(saved);
  expect(fetch.mock.calls[0]?.[0]).toContain(`/stocktakes/${take}/counts/${command}`);
  await expect(recordStocktakeCount(scope, take, command, count)).rejects.toThrow(/receipt/i);
  await expect(recordStocktakeCount(scope, take, command, count)).rejects.toThrow(/receipt/i);
  expect(count.counts[0]?.counted_units).toBe(0);
});

it("refuses a paged line from another stocktake", async () => {
  const line = {
    id: command,
    stocktake_id: take,
    product_id: take,
    product_version: 1,
    sku: "TEST",
    brand_name: "TEST Product",
    unit_of_issue: "tablet",
    batch_id: take,
    lot_number: "TEST-LOT",
    expires_on: "2030-01-01",
    location_id: command,
    location_code: "TEST-Q",
    location_name: "TEST Quarantine",
    stock_status: "quarantine",
    book_units: 10,
    movement_count: 1,
    regulatory_class: null,
    ndps_status: "unknown",
    schedule_declaration_id: null,
    counted_units: null,
    counted_by: null,
    count_reason: null,
    counted_at: null,
    difference_units: null,
    snapshotted_at: "2026-11-20T00:00:00Z",
  };
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValueOnce(json({ data: [line], page: { next_cursor: null, has_more: false } }))
      .mockResolvedValueOnce(
        json({
          data: [{ ...line, stocktake_id: command }],
          page: { next_cursor: null, has_more: false },
        }),
      ),
  );
  const page = await fetchStocktakeLines(scope, take, null);
  expect(page.data[0]?.book_units).toBe(10);
  expect(page.data[0]?.counted_units).toBeNull();
  await expect(fetchStocktakeLines(scope, take, null)).rejects.toThrow(/stocktake/i);
});

it("uses the exact hospital and department route for a hospital counter", async () => {
  const hospital = {
    ...scope,
    authority: "hospital",
    hospitalId: "0199e000-0000-7000-8000-000000000011",
    departmentId: "0199e000-0000-7000-8000-000000000012",
  } as unknown as PharmacyStocktakeScope;
  const fetch = vi.fn().mockResolvedValue(json(receipt));
  vi.stubGlobal("fetch", fetch);
  await startStocktake(hospital, take, input);
  expect(fetch.mock.calls[0]?.[0]).toBe(
    `/api/v1/pharmacy/hospitals/0199e000-0000-7000-8000-000000000011/departments/0199e000-0000-7000-8000-000000000012/pharmacies/${scope.pharmacyId}/outlets/${scope.outletId}/stocktakes/${take}`,
  );
});

it("separates hospital cache identity from other departments, hospitals and authority models", () => {
  const hospital: PharmacyStocktakeScope = {
    ...scope,
    authority: "hospital",
    hospitalId: command,
    departmentId: take,
  };
  for (const other of [
    scope,
    { ...scope, authority: "operating" as const },
    { ...hospital, hospitalId: take },
    { ...hospital, departmentId: command },
    { ...hospital, subject: "TEST other counter" },
  ])
    expect(stocktakeKey(hospital, take)).not.toEqual(stocktakeKey(other, take));
});

it.each([
  { authority: "hospital", hospitalId: take },
  { authority: "hospital", departmentId: take },
  { authority: "hospital", hospitalId: "", departmentId: take },
  { authority: "hospital", hospitalId: command, departmentId: "invalid" },
  { authority: "operating", hospitalId: command, departmentId: take },
  { hospitalId: command, departmentId: take },
])("refuses incomplete or ambiguous hospital scope before traffic: %j", async (value) => {
  const invalid = { ...scope, ...value } as PharmacyStocktakeScope;
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(readStocktake(invalid, take)).rejects.toThrow();
  expect(() => stocktakeKey(invalid, take)).toThrow();
  expect(fetch).not.toHaveBeenCalled();
});
