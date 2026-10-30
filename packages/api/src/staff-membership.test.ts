import { afterEach, expect, it, vi } from "vitest";
import { enrollStaffMember } from "./staff-membership";

const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-admin" };
const id = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const membership = "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb";
const input = {
  subject: "synthetic-reviewer",
  reason: "Enroll verified account without business access",
};
const receipt = {
  id,
  ...input,
  membership_id: membership,
  role_key: "staff_member",
  membership_epoch: 1,
  actor_subject: scope.subject,
  created_at: "2026-10-30T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());

it("enrolls only ordinary membership through an account-pinned Global API command", async () => {
  const fetch = vi.fn().mockResolvedValue(Response.json(receipt));
  vi.stubGlobal("fetch", fetch);
  expect(await enrollStaffMember(scope, id, input)).toEqual(receipt);
  const [url, init] = fetch.mock.calls[0]!;
  expect(url).toBe(`/api/v1/iam/membership-enrollments/${id}`);
  expect(init.method).toBe("PUT");
  expect(JSON.parse(init.body)).toEqual(input);
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(init.cache).toBe("no-store");
});
it("refuses malformed subjects, blank reasons and privileged input before transmission", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  for (const subject of ["", " ", "*", "a#b", "a:b", "a\0b", "a b", "a".repeat(201)])
    await expect(enrollStaffMember(scope, id, { ...input, subject })).rejects.toThrow();
  await expect(enrollStaffMember(scope, id, { ...input, reason: " " })).rejects.toThrow();
  await expect(
    enrollStaffMember(scope, id, { ...input, role_key: "tenant_admin" } as typeof input),
  ).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});
it("refuses mismatched receipt identity, actor, input, role and epoch", async () => {
  for (const change of [
    { id: membership },
    { subject: "different-target" },
    { reason: "different reason" },
    { actor_subject: "previous-admin" },
    { role_key: "doctor" },
    { membership_epoch: 2 },
    { membership_id: "not-an-id" },
  ]) {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ ...receipt, ...change })));
    await expect(enrollStaffMember(scope, id, input)).rejects.toThrow();
  }
});
