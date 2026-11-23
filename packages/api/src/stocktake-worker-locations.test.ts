import { afterEach, expect, it, vi } from "vitest";
import * as api from "./index";
import type { StocktakeDutyLocationChoice } from "./operating-assignments-contract";

const scope = { tenantId: "018f8f5b-1111-7111-8111-111111111111", subject: "TEST-worker" };
const choice: StocktakeDutyLocationChoice = {
  owner_tenant_id: scope.tenantId,
  duty_id: "019a55c0-1111-7111-8111-111111111112",
  employer_id: "019a55c0-1111-7111-8111-111111111113",
  location_id: "019a55c0-1111-7111-8111-111111111114",
  employer_name: "TEST Independent Pharmacy",
  location_name: "TEST Main Outlet",
  location_code: "TEST-MAIN",
  time_zone: "Asia/Kolkata",
  country_code: "IN",
  template_key: "pharmacy.stocktake_counter.v1",
  template_label: "Outlet stocktake counter",
  assigned_permissions: ["pharmacy.stocktake.read", "pharmacy.stocktake.count"],
  snapshot_version: 2,
  authority_epoch: 1,
  expires_at: "2030-01-01T00:00:00Z",
};
const page = { data: [choice], page: { has_more: false, next_cursor: null } };
type ChoicePage = {
  data: StocktakeDutyLocationChoice[];
  page: { has_more: boolean; next_cursor: string | null };
};
function list() {
  expect(api).toHaveProperty("listStocktakeWorkerLocations");
  return Reflect.get(api, "listStocktakeWorkerLocations") as (
    scope: typeof scope,
    cursor?: string | null,
    signal?: AbortSignal,
  ) => Promise<ChoicePage>;
}
afterEach(() => vi.unstubAllGlobals());

it("discovers only the signed-in worker's named duty locations through Global API", async () => {
  const fetch = vi.fn().mockResolvedValue(Response.json(page));
  vi.stubGlobal("fetch", fetch);
  expect((await list()(scope)).data).toEqual([choice]);
  expect(fetch.mock.calls[0]?.[0]).toBe("/api/v1/iam/me/pharmacy-stocktake-locations?limit=20");
  const options = fetch.mock.calls[0]?.[1] as RequestInit;
  expect(options.cache).toBe("no-store");
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});

it("retains empty-page continuation and permits separate roles at one outlet", async () => {
  const reader = {
    ...choice,
    duty_id: "019a55c0-1111-7111-8111-111111111115",
    template_key: "pharmacy.stocktake_reader.v1",
    template_label: "Outlet stocktake reader",
    assigned_permissions: ["pharmacy.stocktake.read"],
  };
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(
      Response.json({ data: [], page: { has_more: true, next_cursor: "TEST-opaque-next" } }),
    )
    .mockResolvedValueOnce(
      Response.json({ data: [choice, reader], page: { has_more: false, next_cursor: null } }),
    );
  vi.stubGlobal("fetch", fetch);
  const first = await list()(scope);
  expect(first.data).toEqual([]);
  expect(first.page.has_more).toBe(true);
  expect((await list()(scope, first.page.next_cursor)).data).toEqual([choice, reader]);
  expect(fetch.mock.calls[1]?.[0]).toContain("cursor=TEST-opaque-next");
});

it.each([
  { ...page, data: [{ ...choice, owner_tenant_id: choice.employer_id }] },
  { ...page, data: [choice, choice] },
  { ...page, data: Array(21).fill(choice) },
  { ...page, data: [{ ...choice, template_key: "pharmacy.inventory_receiver.v1" }] },
  { ...page, data: [{ ...choice, assigned_permissions: ["pharmacy.stock.read"] }] },
  {
    ...page,
    data: [
      { ...choice, assigned_permissions: ["pharmacy.stocktake.read", "pharmacy.stocktake.review"] },
    ],
  },
  {
    ...page,
    data: [
      { ...choice, assigned_permissions: ["pharmacy.stocktake.read", "pharmacy.stocktake.read"] },
    ],
  },
  { ...page, data: [{ ...choice, employer_name: " " }] },
  { ...page, data: [{ ...choice, location_name: "" }] },
  { ...page, data: [{ ...choice, time_zone: "TEST/not-a-zone" }] },
  { ...page, data: [{ ...choice, legacy_manager: true }] },
  { ...page, page: { has_more: true, next_cursor: null } },
  { ...page, page: { has_more: false, next_cursor: "TEST-extra" } },
])("refuses unverifiable worker choices %#", async (value) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(value)));
  await expect(list()(scope)).rejects.toThrow();
});

it("refuses a repeated cursor instead of looping discovery", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        Response.json({ data: [], page: { has_more: true, next_cursor: "TEST-same" } }),
      ),
  );
  await expect(list()(scope, "TEST-same")).rejects.toThrow();
});

it("stops malformed or aborted discovery before transport", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  const controller = new AbortController();
  controller.abort();
  await expect(list()(scope, null, controller.signal)).rejects.toThrow();
  await expect(list()(scope, "")).rejects.toThrow();
  await expect(list()({ ...scope, tenantId: "TEST-not-a-tenant" })).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});
