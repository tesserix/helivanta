import { afterEach, expect, it, vi } from "vitest";
import { listPharmacyStaffBindingOutletChoices } from "./pharmacy-staff-bindings";
import { beginTenantTransition, cancelTenantTransition } from "./tenant-transition";

const id = (n: number) => "019a0000-0000-7000-8000-" + String(n).padStart(12, "0");
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-choice-reader",
  hospitalId: "22222222-2222-4222-8222-222222222222",
};
const choice = {
  pharmacy_id: id(1),
  outlet_id: id(2),
  pharmacy_name: "Synthetic pharmacy",
  outlet_name: "Synthetic outlet",
};
const page = (data: unknown[], cursor: string | null = null) => ({
  data,
  page: { has_more: cursor !== null, next_cursor: cursor },
});
afterEach(() => {
  vi.unstubAllGlobals();
  cancelTenantTransition();
});

it("reads canonical outlet names under an exact hospital and preparation purpose", async () => {
  const fetch = vi.fn().mockResolvedValue(Response.json(page([choice])));
  vi.stubGlobal("fetch", fetch);
  expect(await listPharmacyStaffBindingOutletChoices(scope, "prepare", "opaque+/=")).toEqual(
    page([choice]),
  );
  const [url, options] = fetch.mock.calls[0]!;
  const parsed = new URL(url, "https://example.invalid");
  expect(parsed.pathname).toBe(
    "/api/v1/pharmacy/hospitals/" + scope.hospitalId + "/staff-binding-outlet-choices",
  );
  expect(parsed.searchParams.get("purpose")).toBe("prepare");
  expect(parsed.searchParams.get("limit")).toBe("10");
  expect(parsed.searchParams.get("cursor")).toBe("opaque+/=");
  expect(options.cache).toBe("no-store");
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});

it.each([
  page([choice, choice]),
  page([{ ...choice, outlet_id: choice.outlet_id.toUpperCase() }, choice]),
  page([{ ...choice, staff_id: id(3) }]),
  page([{ ...choice, can_dispense: true }]),
  page([{ ...choice, pharmacy_id: scope.hospitalId }]),
  page(Array.from({ length: 11 }, () => choice)),
  { data: [], page: { has_more: true, next_cursor: null } },
  { data: [], page: { has_more: false, next_cursor: "extra" } },
  page([choice], "previous"),
])("withholds malformed or overbroad outlet choices: %j", async (response) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(response)));
  await expect(listPharmacyStaffBindingOutletChoices(scope, "read", "previous")).rejects.toThrow();
});
it.each(["read", "prepare", "review"] as const)(
  "keeps the exact %s purpose in the target request",
  async (purpose) => {
    const fetch = vi.fn().mockResolvedValue(Response.json(page([])));
    vi.stubGlobal("fetch", fetch);
    expect(await listPharmacyStaffBindingOutletChoices(scope, purpose)).toEqual(page([]));
    expect(
      new URL(fetch.mock.calls[0]![0], "https://example.invalid").searchParams.get("purpose"),
    ).toBe(purpose);
  },
);
it("discards late outlet choices across an account/workspace transition", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      beginTenantTransition();
      cancelTenantTransition();
      return Response.json(page([choice]));
    }),
  );
  await expect(listPharmacyStaffBindingOutletChoices(scope, "read")).rejects.toThrow();
});
it("rejects an unsupported purpose without transmitting a request", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(
    listPharmacyStaffBindingOutletChoices(scope, "operating" as "read"),
  ).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});
