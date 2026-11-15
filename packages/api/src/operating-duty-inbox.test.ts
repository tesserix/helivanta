import { beforeEach, describe, expect, it, vi } from "vitest";
import { listOperatingDutyInbox } from "./operating-duty-inbox";

const { fetcher } = vi.hoisted(() => ({ fetcher: vi.fn() }));
vi.mock("./client", () => ({ apiFetch: fetcher }));
const scope = {
  tenantId: "00000000-0000-7000-8000-000000000001",
  subject: "owner-account",
  employerKind: "pharmacy" as const,
  employerId: "00000000-0000-7000-8000-000000000002",
  administration: "governance" as const,
};
const duty = {
  owner_tenant_id: scope.tenantId,
  id: "00000000-0000-7000-8000-000000000010",
  status: "revoked",
  version: 4,
  prepared_by: "former-preparer",
  created_at: "2026-11-01T00:00:00Z",
  updated_at: "2026-11-02T00:00:00Z",
  input: {
    scope: {
      employer_kind: scope.employerKind,
      employer_id: scope.employerId,
      location_id: "00000000-0000-7000-8000-000000000011",
      expected_employer_version: 1,
      expected_location_version: 1,
    },
    identity: {
      staff_id: "00000000-0000-7000-8000-000000000012",
      staff_version: 1,
      link_id: "00000000-0000-7000-8000-000000000013",
      link_version: 2,
    },
    subject: "former-account",
    template_key: "pharmacy.inventory_receiver.v1",
    reason: "TEST retained scope",
    valid_from: "2026-11-01T00:00:00Z",
    expires_at: "2026-11-02T00:00:00Z",
  },
};
beforeEach(() => {
  fetcher.mockReset();
});
const empty = () => ({ data: [], page: { has_more: false, next_cursor: null } });

describe("retained native operating duty inbox", () => {
  it("uses the bounded Global API route and exact current account without cache", async () => {
    fetcher.mockResolvedValue(empty());
    expect(await listOperatingDutyInbox(scope)).toEqual(empty());
    expect(fetcher).toHaveBeenCalledWith(
      `/iam/operating-governance-employers/pharmacy/${scope.employerId}/duty-inbox?status=all&limit=20`,
      expect.objectContaining({
        cache: "no-store",
        headers: {
          "X-Helivanta-Expected-Tenant": scope.tenantId,
          "X-Helivanta-Expected-Subject": scope.subject,
        },
      }),
    );
  });
  it("snapshots scope and encodes an opaque continuation", async () => {
    fetcher.mockResolvedValue(empty());
    const selected = { ...scope, employerKind: "supplier" as const };
    const pending = listOperatingDutyInbox(selected, "opaque+/=");
    selected.subject = "other-account";
    selected.employerId = "00000000-0000-7000-8000-000000000099";
    await pending;
    expect(fetcher.mock.calls[0]?.[0]).toContain(
      `/supplier/${scope.employerId}/duty-inbox?status=all&limit=20&cursor=opaque%2B%2F%3D`,
    );
    expect(fetcher.mock.calls[0]?.[1].headers["X-Helivanta-Expected-Subject"]).toBe(scope.subject);
  });
  it("refuses legacy admission and malformed employer scopes before fetching", async () => {
    for (const candidate of [
      { ...scope, administration: "legacy" },
      { ...scope, subject: "" },
      { ...scope, employerKind: "hospital" },
      { ...scope, employerId: "bad" },
      { ...scope, staffId: "an-unadvertised-filter" },
      { ...scope, subject: "invalid\0account" },
    ]) {
      await expect(listOperatingDutyInbox(candidate as typeof scope)).rejects.toThrow();
    }
    expect(fetcher).not.toHaveBeenCalled();
  });
  it("does not disguise source denial with an empty list or legacy retry", async () => {
    const refused = Object.assign(new Error("Current management source is no longer available"), {
      status: 404,
    });
    fetcher.mockRejectedValue(refused);
    await expect(listOperatingDutyInbox(scope)).rejects.toBe(refused);
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
  it.each([
    { data: [], page: { has_more: true, next_cursor: null } },
    { data: [], page: { has_more: false, next_cursor: "orphan" } },
    { data: [], page: { has_more: true, next_cursor: "empty-probe" } },
  ])("rejects inconsistent page evidence: %j", async (response) => {
    fetcher.mockResolvedValue(response);
    await expect(listOperatingDutyInbox(scope)).rejects.toThrow();
  });
  it("rejects already-cancelled account reads before disclosure", async () => {
    const controller = new AbortController();
    controller.abort();
    await expect(listOperatingDutyInbox(scope, null, controller.signal)).rejects.toThrow();
    expect(fetcher).not.toHaveBeenCalled();
  });
});

it("accepts canonical retained heads without claiming active Staff eligibility", async () => {
  fetcher.mockResolvedValue({ ...empty(), data: [duty] });
  expect((await listOperatingDutyInbox(scope)).data[0]?.status).toBe("revoked");
});
it.each([
  { ...duty, owner_tenant_id: "00000000-0000-7000-8000-000000000099" },
  { ...duty, input: { ...duty.input, scope: { ...duty.input.scope, employer_kind: "supplier" } } },
  {
    ...duty,
    input: {
      ...duty.input,
      scope: { ...duty.input.scope, employer_id: "00000000-0000-7000-8000-000000000099" },
    },
  },
])("rejects a head outside the selected owner scope", async (row) => {
  fetcher.mockResolvedValue({ ...empty(), data: [row] });
  await expect(listOperatingDutyInbox(scope)).rejects.toThrow();
});
it("rejects duplicate heads and underfilled continuation pages", async () => {
  for (const response of [
    { ...empty(), data: [duty, duty] },
    { data: [duty], page: { has_more: true, next_cursor: "opaque" } },
  ]) {
    fetcher.mockResolvedValue(response);
    await expect(listOperatingDutyInbox(scope)).rejects.toThrow();
  }
});
it("withholds results if account cancellation arrives during the read", async () => {
  const controller = new AbortController();
  fetcher.mockImplementation(async () => {
    controller.abort();
    return { ...empty(), data: [duty] };
  });
  await expect(listOperatingDutyInbox(scope, null, controller.signal)).rejects.toThrow();
});
