import { afterEach, expect, it, vi } from "vitest";
import { listOperatingStaffChoices, operatingStaffChoicesKey } from "./operating-staff-choices";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-reviewer",
  employerKind: "pharmacy" as const,
  employerId: "019a55c0-1111-7111-8111-111111111112",
};
const choice = {
  staff_id: "11111111-1111-4111-8111-111111111112",
  display_name: "Synthetic staff",
  staff_version: 1,
  subject: "synthetic-staff",
  link_id: "019a55c0-1111-7111-8111-111111111113",
  link_version: 2,
};
const page = { data: [choice], page: { has_more: false, next_cursor: null } };
afterEach(() => vi.unstubAllGlobals());
it.each(["pharmacy", "supplier"] as const)(
  "loads exact %s Staff choices through the Global API without legacy directory requests",
  async (employerKind) => {
    const request = vi.fn<typeof fetch>().mockResolvedValue(Response.json(page));
    vi.stubGlobal("fetch", request);
    expect((await listOperatingStaffChoices({ ...scope, employerKind })).data).toEqual([choice]);
    expect(request).toHaveBeenCalledTimes(1);
    const [url, options] = request.mock.calls[0]!;
    expect(url).toBe(
      `/api/v1/workforce/operating-governance-employers/${employerKind}/${scope.employerId}/staff-choices?status=active&limit=20`,
    );
    expect(new Headers(options?.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
    expect(new Headers(options?.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  },
);
it.each([
  { ...page, data: [choice, choice] },
  { ...page, data: [{ ...choice, link_version: 1 }] },
  { ...page, data: [{ ...choice, staff_version: 0 }] },
  { ...page, data: [{ ...choice, source_grant_id: scope.employerId }] },
  { ...page, page: { has_more: true, next_cursor: null } },
  { ...page, page: { has_more: false, next_cursor: "foreign" } },
])("rejects unverified Staff choice payloads %j", async (value) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(value)));
  await expect(listOperatingStaffChoices(scope)).rejects.toThrow();
});
it("binds query keys to current account, tenant, employer kind and employer", () => {
  const key = operatingStaffChoicesKey(scope);
  for (const patch of [
    { tenantId: "22222222-2222-4222-8222-222222222222" },
    { subject: "another-reviewer" },
    { employerKind: "supplier" as const },
    { employerId: "019a55c0-1111-7111-8111-111111111114" },
  ])
    expect(operatingStaffChoicesKey({ ...scope, ...patch })).not.toEqual(key);
});
it("keeps an aborted request off the transport", async () => {
  const request = vi.fn<typeof fetch>();
  vi.stubGlobal("fetch", request);
  const abort = new AbortController();
  abort.abort();
  await expect(listOperatingStaffChoices(scope, null, abort.signal)).rejects.toThrow();
  expect(request).not.toHaveBeenCalled();
});
it("does not fall back after native source denial", async () => {
  const request = vi
    .fn<typeof fetch>()
    .mockResolvedValue(
      Response.json({ error: "not_found", message: "Unavailable" }, { status: 404 }),
    );
  vi.stubGlobal("fetch", request);
  await expect(listOperatingStaffChoices(scope)).rejects.toThrow();
  expect(request).toHaveBeenCalledTimes(1);
});
