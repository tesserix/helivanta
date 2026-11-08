import { afterEach, expect, it, vi } from "vitest";
import {
  listSupplierStaffSourcesPage,
  listSupplierStaffPage,
  getSupplierStaffRecord,
  saveSupplierStaffRecord,
  registerSupplierEmployer,
  getSupplierEmployer,
} from "./supplier-staff";
const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "TEST-owner" };
const supplier = "019a55c0-1111-7111-8111-111111111111";
const id = "019a55c0-1111-7111-8111-111111111112";
const input = {
  employee_code: "TEST-1",
  display_name: "TEST Operator",
  job_category: "other",
  employment_status: "active",
} as const;
const row = {
  id,
  supplier_id: supplier,
  ...input,
  created_at: "2026-11-07T00:00:00Z",
  updated_at: "2026-11-07T00:00:00Z",
};
const record = { ...row, version: 1, directory_context_version: "v1" };
const respond = (value: unknown) => {
  const request = vi.fn<typeof fetch>().mockImplementation(async () => Response.json(value));
  vi.stubGlobal("fetch", request);
  return request;
};
afterEach(() => vi.unstubAllGlobals());
it("lists only typed supplier Staff sources and refuses mixed employer rows", async () => {
  const source = {
    supplier_id: supplier,
    name: "TEST Distributor",
    access_level: "manager",
    created_at: row.created_at,
  };
  const request = respond({ data: [source], page: { has_more: false, next_cursor: null } });
  expect((await listSupplierStaffSourcesPage(scope)).data).toEqual([source]);
  expect(request.mock.calls[0]![0]).toBe("/api/v1/workforce/suppliers?limit=50");
  respond({
    data: [{ ...source, hospital_id: supplier }],
    page: { has_more: false, next_cursor: null },
  });
  await expect(listSupplierStaffSourcesPage(scope)).rejects.toThrow();
});
it("checks exact supplier and record on reads and saves", async () => {
  const request = respond(record);
  expect(await getSupplierStaffRecord(scope, supplier, id)).toEqual(record);
  expect(await saveSupplierStaffRecord(scope, supplier, id, input, 0)).toEqual(record);
  const init = request.mock.calls[1]![1]!;
  expect(JSON.parse(String(init.body))).toEqual({
    ...input,
    expected_version: 0,
    directory_context_version: "v1",
  });
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  respond({ ...record, supplier_id: id });
  await expect(getSupplierStaffRecord(scope, supplier, id)).rejects.toThrow();
  respond({ ...record, display_name: "TEST someone else" });
  await expect(saveSupplierStaffRecord(scope, supplier, id, input, 0)).rejects.toThrow();
});
it("refuses duplicate and cross-supplier directory pages", async () => {
  respond({ data: [row], page: { has_more: false, next_cursor: null } });
  expect((await listSupplierStaffPage(scope, supplier)).data).toEqual([row]);
  respond({ data: [row, row], page: { has_more: false, next_cursor: null } });
  await expect(listSupplierStaffPage(scope, supplier)).rejects.toThrow();
  respond({ data: [{ ...row, supplier_id: id }], page: { has_more: false, next_cursor: null } });
  await expect(listSupplierStaffPage(scope, supplier)).rejects.toThrow();
});
it("registers an employer from owner facts without sending invented Staff authority", async () => {
  const employer = {
    supplier_id: supplier,
    name: "TEST Distributor",
    source_version: 1,
    created_at: row.created_at,
  };
  const request = respond(employer);
  expect(await registerSupplierEmployer(scope, supplier)).toEqual(employer);
  expect(request.mock.calls[0]![1]).toMatchObject({ method: "PUT", body: "{}", cache: "no-store" });
  respond({ ...employer, supplier_id: id });
  await expect(registerSupplierEmployer(scope, supplier)).rejects.toThrow();
});

it("recovers an exact supplier employer registration with GET", async () => {
  const result = {
    supplier_id: supplier,
    name: "TEST Distributor",
    source_version: 1,
    created_at: row.created_at,
  };
  const request = respond(result);
  expect(await getSupplierEmployer(scope, supplier)).toEqual(result);
  expect(request.mock.calls[0]![1]?.method ?? "GET").toBe("GET");
  respond({ ...result, supplier_id: id });
  await expect(getSupplierEmployer(scope, supplier)).rejects.toThrow();
});
