import { afterEach, expect, it, vi } from "vitest";
import { submitMembershipCommand } from "./membership-commands";

const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-admin" };
const id = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const input = {
  operation: "remove" as const,
  basis_id: "22222222-2222-4222-8222-222222222222",
  subject: "synthetic-target",
  role_key: "staff_member",
  expected_membership_epoch: 1,
  reason: "Remove this exact reviewed membership",
};
const receipt = {
  id,
  ...input,
  result_membership_id: input.basis_id,
  result_membership_epoch: 2,
  actor_subject: scope.subject,
  created_at: "2026-10-30T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());

it("pins the command, account and tenant through Global API", async () => {
  const fetch = vi.fn().mockResolvedValue(Response.json(receipt));
  vi.stubGlobal("fetch", fetch);
  expect(await submitMembershipCommand(scope, id, input)).toEqual(receipt);
  const [url, init] = fetch.mock.calls[0]!;
  expect(url).toBe(`/api/v1/iam/membership-commands/${id}`);
  expect(init.method).toBe("PUT");
  expect(init.cache).toBe("no-store");
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(JSON.parse(init.body)).toEqual(input);
});
it("rejects another command's accepted receipt", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(Response.json({ ...receipt, id: "019aaaaa-bbbb-7aaa-8aaa-aaaaaaaaaaaa" })),
  );
  await expect(submitMembershipCommand(scope, id, input)).rejects.toThrow();
});
it("refuses blank reasons and recovery of privileged roles before sending", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(submitMembershipCommand(scope, id, { ...input, reason: " " })).rejects.toThrow();
  await expect(
    submitMembershipCommand(scope, id, { ...input, operation: "recover", role_key: "doctor" }),
  ).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});
