import { afterEach, expect, it, vi } from "vitest";
import * as api from "./index";
import type { HospitalStocktakeOutletChoice, HospitalStocktakeScopeChoice } from "./index";

const account = { tenantId: "018f8f5b-1111-7111-8111-111111111111", subject: "TEST-worker" };
const scope = {
  ...account,
  hospitalId: "22222222-2222-4222-8222-222222222222",
  departmentId: "019a55c0-1111-7111-8111-111111111112",
};
const assignment: HospitalStocktakeScopeChoice = {
  owner_tenant_id: account.tenantId,
  id: "019a55c0-1111-7111-8111-111111111113",
  hospital_id: scope.hospitalId,
  department_id: scope.departmentId,
  hospital_name: "TEST Hospital",
  department_name: "TEST Pharmacy department",
  template_key: "pharmacy.department_stocktake_counter.v1",
  template_label: "Hospital pharmacy stocktake counter",
  assigned_permissions: [
    "pharmacy.stocktake.read",
    "pharmacy.stocktake.count",
    "workforce.stocktake_actor.use",
  ],
  snapshot_version: 1,
  authority_epoch: 1,
  expires_at: "2030-01-01T00:00:00Z",
};
const outlet: HospitalStocktakeOutletChoice = {
  owner_tenant_id: account.tenantId,
  hospital_id: scope.hospitalId,
  department_id: scope.departmentId,
  department_revision_id: "019a55c0-1111-7111-8111-111111111114",
  pharmacy_id: "019a55c0-1111-7111-8111-111111111115",
  outlet_id: "019a55c0-1111-7111-8111-111111111116",
  binding_id: "019a55c0-1111-7111-8111-111111111117",
  binding_version: 2,
  pharmacy_name: "TEST Hospital pharmacy",
  outlet_name: "TEST Main outlet",
  outlet_code: "TEST-MAIN",
  country_code: "IN",
  time_zone: "Asia/Kolkata",
};
const page = (data: unknown[], cursor: string | null = null) => ({
  data,
  page: { has_more: cursor !== null, next_cursor: cursor },
});
function client(name: string) {
  expect(api).toHaveProperty(name);
  return Reflect.get(api, name) as (
    account: typeof scope,
    cursor?: string | null,
    signal?: AbortSignal,
  ) => Promise<{ data: unknown[]; page: { has_more: boolean; next_cursor: string | null } }>;
}
afterEach(() => vi.unstubAllGlobals());

it("reads named hospital assignments and bound outlets through their Global API owners", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(Response.json(page([assignment])))
    .mockResolvedValueOnce(Response.json(page([outlet])));
  vi.stubGlobal("fetch", fetch);
  expect((await client("listHospitalStocktakeScopes")(scope)).data).toEqual([assignment]);
  expect((await client("listHospitalStocktakeOutlets")(scope)).data).toEqual([outlet]);
  expect(fetch.mock.calls.map(([url]) => url)).toEqual([
    "/api/v1/iam/me/hospital-pharmacy-stocktake-scopes?limit=20",
    `/api/v1/pharmacy/hospitals/${scope.hospitalId}/departments/${scope.departmentId}/stocktake-outlet-choices?limit=20`,
  ]);
  for (const [, raw] of fetch.mock.calls) {
    const options = raw as RequestInit;
    expect(options.cache).toBe("no-store");
    expect(new Headers(options.headers).get("X-Helivanta-Expected-Tenant")).toBe(account.tenantId);
    expect(new Headers(options.headers).get("X-Helivanta-Expected-Subject")).toBe(account.subject);
  }
});

it("retains empty-page continuation without dropping separate roles at one department", async () => {
  const reader = {
    ...assignment,
    id: "019a55c0-1111-7111-8111-111111111118",
    template_key: "pharmacy.department_stocktake_reader.v1",
    template_label: "Hospital pharmacy stocktake reader",
    assigned_permissions: ["pharmacy.stocktake.read", "workforce.stocktake_actor.use"],
  };
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(Response.json(page([], "TEST-next")))
    .mockResolvedValueOnce(Response.json(page([assignment, reader])));
  vi.stubGlobal("fetch", fetch);
  const first = await client("listHospitalStocktakeScopes")(scope);
  expect(first.data).toEqual([]);
  expect(first.page.has_more).toBe(true);
  expect((await client("listHospitalStocktakeScopes")(scope, first.page.next_cursor)).data).toEqual(
    [assignment, reader],
  );
  expect(fetch.mock.calls[1]?.[0]).toContain("cursor=TEST-next");
});

it.each([
  { ...assignment, owner_tenant_id: scope.hospitalId },
  { ...assignment, template_key: "workforce.leave_reviewer.v1" },
  { ...assignment, template_label: assignment.id },
  {
    ...assignment,
    assigned_permissions: [
      "pharmacy.stocktake.read",
      "pharmacy.stocktake.review",
      "workforce.stocktake_actor.use",
    ],
  },
  {
    ...assignment,
    assigned_permissions: [
      "pharmacy.stocktake.read",
      "pharmacy.stocktake.read",
      "workforce.stocktake_actor.use",
    ],
  },
  { ...assignment, staff_id: assignment.id },
])(
  "rejects a scope outside the account or exact reviewed role before exposing labels",
  async (bad) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(page([bad]))));
    await expect(client("listHospitalStocktakeScopes")(scope)).rejects.toThrow();
  },
);

it.each([
  { ...outlet, owner_tenant_id: scope.hospitalId },
  { ...outlet, hospital_id: outlet.pharmacy_id },
  { ...outlet, department_id: outlet.outlet_id },
  { ...outlet, time_zone: "TEST/not-a-zone" },
  { ...outlet, country_code: "US" },
  { ...outlet, staff_id: assignment.id },
])(
  "rejects a bound outlet outside the selected owner scope or canonical projection",
  async (bad) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(page([bad]))));
    await expect(client("listHospitalStocktakeOutlets")(scope)).rejects.toThrow();
  },
);

it.each(["listHospitalStocktakeScopes", "listHospitalStocktakeOutlets"])(
  "rejects duplicate or overlarge private pages: %s",
  async (name) => {
    const row = name.endsWith("Scopes") ? assignment : outlet;
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(Response.json(page([row, row])))
      .mockResolvedValueOnce(Response.json(page(Array(21).fill(row))));
    vi.stubGlobal("fetch", fetch);
    await expect(client(name)(scope)).rejects.toThrow();
    await expect(client(name)(scope)).rejects.toThrow();
  },
);

it("validates hospital coordinates before cache admission or traffic", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(
    client("listHospitalStocktakeOutlets")({ ...scope, hospitalId: "../other" }),
  ).rejects.toThrow();
  expect(api).toHaveProperty("hospitalStocktakeOutletsKey");
  const key = Reflect.get(api, "hospitalStocktakeOutletsKey") as (
    account: typeof scope,
  ) => readonly unknown[];
  expect(() => key({ ...scope, departmentId: "../other" })).toThrow();
  expect(key(scope)).not.toEqual(key({ ...scope, departmentId: outlet.outlet_id }));
  expect(key(scope)).not.toEqual(key({ ...scope, subject: "TEST-other" }));
  expect(fetch).not.toHaveBeenCalled();
});

it.each(["listHospitalStocktakeScopes", "listHospitalStocktakeOutlets"])(
  "retains filtered-page progress and rejects a repeated private cursor: %s",
  async (name) => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(Response.json(page([], "TEST-next")))
      .mockResolvedValueOnce(Response.json(page([], "TEST-next")));
    vi.stubGlobal("fetch", fetch);
    const first = await client(name)(scope);
    expect(first.data).toEqual([]);
    expect(first.page.next_cursor).toBe("TEST-next");
    await expect(client(name)(scope, first.page.next_cursor)).rejects.toThrow();
  },
);
it("rejects two approved bindings for the same physical outlet", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(Response.json(page([outlet, { ...outlet, binding_id: assignment.id }]))),
  );
  await expect(client("listHospitalStocktakeOutlets")(scope)).rejects.toThrow();
});
it.each(["listHospitalStocktakeScopes", "listHospitalStocktakeOutlets"])(
  "does not send an aborted account request: %s",
  async (name) => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    const abort = new AbortController();
    abort.abort();
    await expect(client(name)(scope, null, abort.signal)).rejects.toThrow();
    expect(fetch).not.toHaveBeenCalled();
  },
);

const departmentChoice = {
  owner_tenant_id: account.tenantId,
  hospital_id: scope.hospitalId,
  scope_department_id: scope.departmentId,
  department_id: outlet.outlet_id,
  department_revision_id: outlet.department_revision_id,
  department_name: "TEST Child pharmacy unit",
  department_code: "TEST-CHILD",
};
it("discovers named descendant departments through Global API with account preconditions", async () => {
  const fetch = vi.fn().mockResolvedValue(Response.json(page([departmentChoice])));
  vi.stubGlobal("fetch", fetch);
  expect((await client("listHospitalStocktakeDepartments")(scope)).data).toEqual([departmentChoice]);
  expect(fetch.mock.calls[0]?.[0]).toBe(
    `/api/v1/pharmacy/hospitals/${scope.hospitalId}/departments/${scope.departmentId}/stocktake-department-choices?limit=20`,
  );
  const options = fetch.mock.calls[0]?.[1] as RequestInit;
  expect(options.cache).toBe("no-store");
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Subject")).toBe(account.subject);
});
it.each([
  { ...departmentChoice, owner_tenant_id: scope.hospitalId },
  { ...departmentChoice, hospital_id: outlet.pharmacy_id },
  { ...departmentChoice, scope_department_id: outlet.outlet_id },
  { ...departmentChoice, staff_id: assignment.id },
])("rejects an unverified descendant projection before displaying names", async (bad) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(page([bad]))));
  await expect(client("listHospitalStocktakeDepartments")(scope)).rejects.toThrow();
});
it("keeps descendant cache keys separate by account and assignment root", () => {
  expect(api).toHaveProperty("hospitalStocktakeDepartmentsKey");
  const key = Reflect.get(api, "hospitalStocktakeDepartmentsKey") as (value: typeof scope) => readonly unknown[];
  expect(key(scope)).not.toEqual(key({ ...scope, subject: "TEST-other" }));
  expect(key(scope)).not.toEqual(key({ ...scope, departmentId: outlet.outlet_id }));
});
it("rejects duplicate descendant choices and non-progressing cursors", async () => {
  const fetch = vi.fn()
    .mockResolvedValueOnce(Response.json(page([departmentChoice, departmentChoice])))
    .mockResolvedValueOnce(Response.json(page([], "TEST-next")))
    .mockResolvedValueOnce(Response.json(page([], "TEST-next")));
  vi.stubGlobal("fetch", fetch);
  await expect(client("listHospitalStocktakeDepartments")(scope)).rejects.toThrow();
  const first = await client("listHospitalStocktakeDepartments")(scope);
  expect(first.page.next_cursor).toBe("TEST-next");
  await expect(client("listHospitalStocktakeDepartments")(scope, first.page.next_cursor)).rejects.toThrow();
});
