import { afterEach, expect, it, vi } from "vitest";
import { withStaffIntakeScope } from "./enrollment";
import { listStaffIntakeScopes } from "./staff-directory";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-hr-intake",
  hrIntake: true,
};
const hospital = "22222222-2222-4222-8222-222222222222";
afterEach(() => vi.unstubAllGlobals());

it("adds the canonical intake selector after other projections and preserves the cursor", () => {
  const path = "/workforce/hospitals/example/staff?lifecycle=withdrawal-v1&cursor=opaque%2B%2F%3D";
  const selected = withStaffIntakeScope(path, scope);
  const query = new URL(selected, "https://example.invalid").searchParams;
  expect(query.get("hr_intake")).toBe("v1");
  expect(query.get("lifecycle")).toBe("withdrawal-v1");
  expect(query.get("cursor")).toBe("opaque+/=");
  expect(withStaffIntakeScope(selected, scope)).toBe(selected);
  expect(withStaffIntakeScope(path, { ...scope, hrIntake: false })).toBe(path);
  expect(withStaffIntakeScope(path, { tenantId: scope.tenantId, subject: scope.subject })).toBe(
    path,
  );
});

it.each(["hr_intake=unknown", "hr_intake=v1&hr_intake=v1"])(
  "rejects a conflicting intake projection instead of silently changing %s",
  (query) =>
    expect(() =>
      withStaffIntakeScope(`/workforce/hospitals/example/staff?${query}`, scope),
    ).toThrow(),
);

it("reads bounded scopes with the original workspace and preserves empty-page continuation", async () => {
  const result = { data: [], page: { has_more: true, next_cursor: "next+/=" } };
  const fetch = vi.fn().mockResolvedValue(Response.json(result));
  vi.stubGlobal("fetch", fetch);
  expect(await listStaffIntakeScopes(scope, "prior+/=")).toEqual(result);
  const [url, options] = fetch.mock.calls[0]!;
  const query = new URL(url, "https://example.invalid").searchParams;
  expect(url).toContain("/api/v1/workforce/hr-intake-scopes?");
  expect(query.get("limit")).toBe("20");
  expect(query.get("cursor")).toBe("prior+/=");
  expect(options.cache).toBe("no-store");
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});

it.each([
  {
    data: [
      {
        hospital_id: hospital,
        name: "Synthetic Hospital",
        can_prepare_staff: false,
        can_manage_staff_documents: true,
      },
    ],
    page: { has_more: false, next_cursor: null },
  },
  {
    data: [
      {
        hospital_id: hospital,
        name: "Synthetic Hospital",
        can_prepare_staff: true,
        can_manage_staff_documents: false,
      },
    ],
    page: { has_more: false, next_cursor: null },
  },
  {
    data: [{ hospital_id: hospital, name: "Synthetic Hospital", can_prepare_staff: true }],
    page: { has_more: false, next_cursor: null },
  },
  { data: [], page: { has_more: true, next_cursor: null } },
  { data: [], page: { has_more: false, next_cursor: "unexpected" } },
])("rejects invalid capability or pagination responses", async (result) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(result)));
  await expect(listStaffIntakeScopes(scope, null)).rejects.toThrow();
});
