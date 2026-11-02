import { afterEach, expect, it, vi } from "vitest";
import { listStaffDirectoryPage } from "./staff-directory";

const hospitalId = "11111111-1111-4111-8111-111111111111";
const scope = {
  tenantId: "22222222-2222-4222-8222-222222222222",
  subject: "synthetic-pharmacy-reader",
};
const staff = {
  id: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa",
  hospital_id: hospitalId,
  employee_code: "SYN-PHARM-001",
  display_name: "Synthetic Pharmacist",
  job_category: "pharmacist",
  employment_status: "active",
  created_at: "2026-11-02T00:00:00Z",
  updated_at: "2026-11-02T00:00:00Z",
};
const page = (data: unknown[] = [staff], next: string | null = null) => ({
  data,
  page: { has_more: next !== null, next_cursor: next },
});
afterEach(() => vi.unstubAllGlobals());

it("loads the canonical Staff page through Global API with the original workspace assertions", async () => {
  const fetcher = vi.fn().mockResolvedValue(Response.json(page([staff], "opaque+/=")));
  vi.stubGlobal("fetch", fetcher);
  const controller = new AbortController();
  expect(await listStaffDirectoryPage(scope, hospitalId, "previous+/=", controller.signal)).toEqual(
    page([staff], "opaque+/="),
  );
  const [url, init] = fetcher.mock.calls[0]!;
  expect(url).toBe(
    `/api/v1/workforce/hospitals/${hospitalId}/staff?limit=50&cursor=previous%2B%2F%3D`,
  );
  expect(init.cache).toBe("no-store");
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(init.signal).toBe(controller.signal);
});

it("validates hospital source choices without deriving Staff permission from a pharmacy role", async () => {
  const { listStaffHospitalsPage } = await import("./staff-directory");
  const source = { id: hospitalId, name: "Synthetic source hospital", access_level: "reader" };
  const fetcher = vi.fn().mockResolvedValue(Response.json(page([source])));
  vi.stubGlobal("fetch", fetcher);
  expect(await listStaffHospitalsPage(scope)).toEqual(page([source]));
  expect(fetcher.mock.calls[0]?.[0]).toBe("/api/v1/workforce/hospitals?limit=50");
});

it.each([
  ["foreign hospital", page([{ ...staff, hospital_id: scope.tenantId }])],
  ["duplicate canonical ID", page([staff, staff])],
  ["unknown category", page([{ ...staff, job_category: "licensed_by_label" }])],
  ["private field", page([{ ...staff, salary_minor: 500000 }])],
  ["invalid record ID", page([{ ...staff, id: "staff-1" }])],
  ["missing canonical source", page([{ ...staff, hospital_id: undefined }])],
  ["unbounded rows", page(Array.from({ length: 201 }, () => staff))],
  ["missing continuation", { ...page(), page: { has_more: true, next_cursor: null } }],
  ["unexpected continuation", { ...page(), page: { has_more: false, next_cursor: "unexpected" } }],
])("rejects %s instead of displaying unverified Staff", async (_label, value) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(value)));
  await expect(listStaffDirectoryPage(scope, hospitalId)).rejects.toThrow();
});

it.each(["not-a-hospital", "00000000-0000-0000-0000-000000000000"])(
  "rejects invalid hospital %s before requesting Staff",
  async (id) => {
    const fetcher = vi.fn();
    vi.stubGlobal("fetch", fetcher);
    await expect(listStaffDirectoryPage(scope, id)).rejects.toThrow();
    expect(fetcher).not.toHaveBeenCalled();
  },
);

it.each([
  [403, "forbidden"],
  [409, "session_changed"],
  [503, "authz_unavailable"],
] as const)("preserves owner refusal %s for the workspace cutoff", async (status, error) => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(Response.json({ error, message: "Synthetic owner refusal" }, { status })),
  );
  await expect(listStaffDirectoryPage(scope, hospitalId)).rejects.toMatchObject({
    status,
    code: error,
  });
});

it("preserves scoped intake requests and a genuine empty page", async () => {
  const fetcher = vi.fn().mockResolvedValue(Response.json(page([])));
  vi.stubGlobal("fetch", fetcher);
  expect(await listStaffDirectoryPage({ ...scope, hrIntake: true }, hospitalId)).toEqual(page([]));
  expect(fetcher.mock.calls[0]?.[0]).toContain("&hr_intake=v1");
});

it("matches canonical UUID scope independently of letter case", async () => {
  const id = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(Response.json(page([{ ...staff, hospital_id: id }]))),
  );
  const result = await listStaffDirectoryPage(scope, id.toUpperCase());
  expect(result.data[0]?.hospital_id).toBe(id);
});
