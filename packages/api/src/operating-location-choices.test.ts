import { afterEach, expect, it, vi } from "vitest";
import {
  listOperatingLocationChoices,
  operatingLocationChoicesKey,
} from "./operating-location-choices";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-preparer",
  employerKind: "pharmacy" as const,
  employerId: "019a55c0-1111-7111-8111-111111111112",
};
const location = {
  owner_tenant_id: scope.tenantId,
  employer_kind: scope.employerKind,
  employer_id: scope.employerId,
  location_id: "019a55c0-1111-7111-8111-111111111113",
  code: "SYN-OUTLET",
  name: "Synthetic outlet",
  country_code: "IN",
  employer_version: 1,
  location_version: 1,
};
const page = { data: [location], page: { has_more: false, next_cursor: null } };
afterEach(() => vi.unstubAllGlobals());
it.each(["pharmacy", "supplier"] as const)(
  "loads exact %s locations through the native IAM destination",
  async (employerKind) => {
    const result = { ...page, data: [{ ...location, employer_kind: employerKind }] };
    const request = vi.fn<typeof fetch>().mockResolvedValue(Response.json(result));
    vi.stubGlobal("fetch", request);
    expect((await listOperatingLocationChoices({ ...scope, employerKind })).data).toEqual(
      result.data,
    );
    expect(request).toHaveBeenCalledTimes(1);
    const [url, options] = request.mock.calls[0]!;
    expect(url).toBe(
      `/api/v1/iam/operating-governance-employers/${employerKind}/${scope.employerId}/location-choices?country=IN&limit=20`,
    );
    expect(new Headers(options?.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
    expect(new Headers(options?.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  },
);
it.each([
  { ...page, data: [{ ...location, owner_tenant_id: "22222222-2222-4222-8222-222222222222" }] },
  { ...page, data: [{ ...location, employer_kind: "supplier" }] },
  { ...page, data: [{ ...location, employer_id: "019a55c0-1111-7111-8111-111111111114" }] },
  { ...page, data: [{ ...location, location_version: 0 }] },
  { ...page, data: [{ ...location, country_code: "PK" }] },
  { ...page, data: [{ ...location, source_grant_id: scope.employerId }] },
  { ...page, data: [location, location] },
  { ...page, page: { has_more: true, next_cursor: null } },
  { ...page, data: [], page: { has_more: true, next_cursor: "unverified" } },
])("refuses unverifiable location choices %j", async (value) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(value)));
  await expect(listOperatingLocationChoices(scope)).rejects.toThrow();
});
it("separates employer and account query keys", () => {
  for (const patch of [
    { subject: "another-preparer" },
    { tenantId: "22222222-2222-4222-8222-222222222222" },
    { employerKind: "supplier" as const },
    { employerId: "019a55c0-1111-7111-8111-111111111114" },
  ])
    expect(operatingLocationChoicesKey({ ...scope, ...patch })).not.toEqual(
      operatingLocationChoicesKey(scope),
    );
});
it("does not use legacy catalogues after native source refusal", async () => {
  const request = vi
    .fn<typeof fetch>()
    .mockResolvedValue(
      Response.json(
        { error: "authorization_unavailable", message: "Unavailable" },
        { status: 503 },
      ),
    );
  vi.stubGlobal("fetch", request);
  await expect(listOperatingLocationChoices(scope)).rejects.toThrow();
  expect(request).toHaveBeenCalledTimes(1);
});
it("cancels before any transport", async () => {
  const request = vi.fn<typeof fetch>();
  vi.stubGlobal("fetch", request);
  const abort = new AbortController();
  abort.abort();
  await expect(listOperatingLocationChoices(scope, null, abort.signal)).rejects.toThrow();
  expect(request).not.toHaveBeenCalled();
});
