import { afterEach, expect, it, vi } from "vitest";
import * as api from "./index";
import type { PharmacyStocktakeScope } from "./pharmacy-stocktakes";

const scope: PharmacyStocktakeScope = {
  tenantId: "018f8f5b-1111-7111-8111-111111111111",
  subject: "TEST-counter",
  pharmacyId: "019a55c0-1111-7111-8111-111111111112",
  outletId: "019a55c0-1111-7111-8111-111111111113",
  authority: "operating",
};
const product = {
  id: "019a55c0-1111-7111-8111-111111111114",
  outlet_id: scope.outletId,
  sku: "TEST-SKU",
  brand_name: "TEST Brand",
  generic_name: "TEST generic",
  dosage_form: "tablet",
  ingredients: [{ name: "TEST", strength: "500", unit: "mg" }],
  pack_size: 10,
  unit_of_issue: "tablet",
  currency: "INR",
  version: 2,
  created_at: "2026-11-20T00:00:00Z",
};
const location = {
  id: "019a55c0-1111-7111-8111-111111111115",
  outlet_id: scope.outletId,
  code: "TEST-Q",
  name: "TEST Quarantine",
  stock_status: "quarantine",
  storage_address: { zone_code: "", aisle_code: "", rack_code: "", shelf_code: "" },
  created_at: "2026-11-20T00:00:00Z",
};
const cases = [
  {
    name: "product",
    member: "fetchStocktakeProductChoices",
    suffix: "product-choices",
    row: product,
  },
  {
    name: "quarantine",
    member: "fetchStocktakeQuarantineChoices",
    suffix: "quarantine-choices",
    row: location,
  },
];
type Page = { data: unknown[]; page: { has_more: boolean; next_cursor: string | null } };
function load(member: string) {
  expect(api).toHaveProperty(member);
  return Reflect.get(api, member) as (
    account: PharmacyStocktakeScope,
    cursor: string | null,
    signal?: AbortSignal,
  ) => Promise<Page>;
}
afterEach(() => vi.unstubAllGlobals());

it.each(cases)(
  "loads named $name choices only through reviewed stocktake routes",
  async ({ member, suffix, row }) => {
    const fetch = vi
      .fn()
      .mockResolvedValue(
        Response.json({ data: [row], page: { has_more: true, next_cursor: "TEST-next" } }),
      );
    vi.stubGlobal("fetch", fetch);
    expect((await load(member)(scope, null)).data).toEqual([row]);
    expect(fetch.mock.calls[0]?.[0]).toBe(
      `/api/v1/pharmacy/pharmacies/${scope.pharmacyId}/operating-locations/${scope.outletId}/stocktakes/${suffix}?limit=100`,
    );
    const options = fetch.mock.calls[0]?.[1] as RequestInit;
    expect(options.cache).toBe("no-store");
    expect(new Headers(options.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
    expect(new Headers(options.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  },
);

it.each(cases)("refuses foreign, duplicate and broken $name pages", async ({ member, row }) => {
  for (const data of [[{ ...row, outlet_id: product.id }], [row, row], Array(201).fill(row)]) {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(Response.json({ data, page: { has_more: false, next_cursor: null } })),
    );
    await expect(load(member)(scope, null)).rejects.toThrow();
  }
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(Response.json({ data: [], page: { has_more: true, next_cursor: null } })),
  );
  await expect(load(member)(scope, null)).rejects.toThrow();
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        Response.json({ data: [], page: { has_more: true, next_cursor: "TEST-same" } }),
      ),
  );
  await expect(load(member)(scope, "TEST-same")).rejects.toThrow();
});

it("refuses sellable quarantine choices and editor-only product facts", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      Response.json({
        data: [{ ...location, stock_status: "sellable" }],
        page: { has_more: false, next_cursor: null },
      }),
    ),
  );
  await expect(load("fetchStocktakeQuarantineChoices")(scope, null)).rejects.toThrow();
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      Response.json({
        data: [{ ...product, mrp_minor: 100 }],
        page: { has_more: false, next_cursor: null },
      }),
    ),
  );
  await expect(load("fetchStocktakeProductChoices")(scope, null)).rejects.toThrow();
});

it.each(cases)("refuses legacy or aborted $name requests before transport", async ({ member }) => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(load(member)({ ...scope, authority: "legacy" }, null)).rejects.toThrow();
  await expect(load(member)({ ...scope, authority: undefined }, null)).rejects.toThrow();
  const controller = new AbortController();
  controller.abort();
  await expect(load(member)(scope, null, controller.signal)).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});
