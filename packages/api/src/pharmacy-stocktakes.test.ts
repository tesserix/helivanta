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
} from "./index";

const scope = {
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
  started_by: "TEST starter",
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
    recorded_by: "TEST counter",
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
  await expect(readStocktakeCount(scope, take, command)).rejects.toThrow(/receipt/i);
  await expect(readStocktakeCount(scope, take, command)).rejects.toThrow(/receipt/i);
});

it("loads bounded stocktake pages and rejects wrong scope or a broken continuation", async () => {
  const summary = {
    ...input,
    id: take,
    outlet_id: scope.outletId,
    number: 1,
    started_by: "TEST starter",
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
    recorded_by: "TEST starter",
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
    recorded_by: "TEST counter",
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
