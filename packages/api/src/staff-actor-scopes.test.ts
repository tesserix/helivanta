import { afterEach, expect, it, vi } from "vitest";
import { beginTenantTransition, cancelTenantTransition } from "./tenant-transition";
import { listStaffActorHospitalChoices } from "./staff-actor-scopes";

const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-actor" };
const choice = {
  hospital_id: "22222222-2222-4222-8222-222222222222",
  name: "Synthetic Staff hospital",
};
afterEach(() => {
  vi.unstubAllGlobals();
  cancelTenantTransition();
});
it("reads only canonical hospital metadata with explicit account intent", async () => {
  const response = { data: [choice], page: { has_more: false, next_cursor: null } };
  const fetch = vi.fn().mockResolvedValue(Response.json(response));
  vi.stubGlobal("fetch", fetch);
  expect(await listStaffActorHospitalChoices(scope)).toEqual(response);
  const [url, options] = fetch.mock.calls[0]!;
  expect(url).toBe("/api/v1/workforce/staff-actor-scopes?limit=20");
  expect(options.cache).toBe("no-store");
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});

it("retains an empty filtered page and its opaque continuation", async () => {
  const response = { data: [], page: { has_more: true, next_cursor: "next+/=" } };
  const fetch = vi.fn().mockResolvedValue(Response.json(response));
  vi.stubGlobal("fetch", fetch);
  expect(await listStaffActorHospitalChoices(scope, "previous+/=")).toEqual(response);
  expect(
    new URL(fetch.mock.calls[0]![0], "https://example.invalid").searchParams.get("cursor"),
  ).toBe("previous+/=");
});
it.each([
  { data: [choice, choice], page: { has_more: false, next_cursor: null } },
  {
    data: [{ ...choice, staff_id: choice.hospital_id }],
    page: { has_more: false, next_cursor: null },
  },
  { data: Array.from({ length: 21 }, () => choice), page: { has_more: false, next_cursor: null } },
  { data: [choice], page: { has_more: true, next_cursor: null } },
  { data: [choice], page: { has_more: false, next_cursor: "extra" } },
  { data: [choice], page: { has_more: true, next_cursor: "previous" } },
  {
    data: [{ ...choice, hospital_id: "00000000-0000-0000-0000-000000000000" }],
    page: { has_more: false, next_cursor: null },
  },
])("withholds malformed or overbroad hospital discovery: %j", async (response) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(response)));
  await expect(listStaffActorHospitalChoices(scope, "previous")).rejects.toThrow();
});

it("discards late hospital choices even if the transport ignores an aborted signal", async () => {
  const abort = new AbortController();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      abort.abort();
      return Response.json({ data: [choice], page: { has_more: false, next_cursor: null } });
    }),
  );
  await expect(listStaffActorHospitalChoices(scope, null, abort.signal)).rejects.toThrow();
});
it("discards late hospital choices across a bounced workspace transition", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      beginTenantTransition();
      cancelTenantTransition();
      return Response.json({ data: [choice], page: { has_more: false, next_cursor: null } });
    }),
  );
  await expect(listStaffActorHospitalChoices(scope)).rejects.toThrow();
});
it.each([{ tenantId: "guessed" }, { subject: "changed\nheader" }])(
  "rejects invalid account scope before transmission: %j",
  async (change) => {
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    await expect(listStaffActorHospitalChoices({ ...scope, ...change })).rejects.toThrow();
    expect(fetch).not.toHaveBeenCalled();
  },
);
