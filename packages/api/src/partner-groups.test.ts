import { afterEach, expect, it, vi } from "vitest";
import { decidePartnerGroup, getPartnerGroupCommand, preparePartnerGroup } from "./partner-groups";
const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "reviewer" };
const id = "019ad451-1234-7123-8123-123456789012";
const command = "019ad451-1234-7123-8123-123456789013";
const input = {
  hospital_id: "22222222-2222-4222-8222-222222222222",
  name: "Receiving team",
  purpose: "receiving" as const,
  reason: "Synthetic team preparation",
  valid_from: "2026-11-06T09:00:00Z",
  expires_at: "2026-11-07T09:00:00Z",
};
const group = {
  ...input,
  id,
  status: "pending_scope",
  version: 1,
  proposed_by: scope.subject,
  membership_policy_version: 1,
  external_access_enabled: false,
  created_at: input.valid_from,
  updated_at: input.valid_from,
};
const decision = {
  operation: "approve" as const,
  expected_version: 2,
  reason: "Independent publication review",
};
const receipt = {
  id: command,
  group_id: id,
  ...decision,
  result_version: 3,
  actor_subject: scope.subject,
  actor_membership_epoch: 1,
  group: { ...group, status: "approved", version: 3 },
  created_at: input.valid_from,
};
afterEach(() => vi.unstubAllGlobals());
it("prepares through Global API with exact account and tenant preconditions", async () => {
  const request = vi.fn(async () => Response.json(group));
  vi.stubGlobal("fetch", request);
  expect(await preparePartnerGroup(scope, id, input)).toEqual(group);
  const [path, init] = request.mock.calls[0] as unknown as [string, RequestInit];
  expect(path).toBe(`/api/v1/iam/partner-groups/${id}`);
  expect(init.method).toBe("PUT");
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(JSON.parse(String(init.body))).toEqual(input);
});
it("refuses an owner response that mismatches the proposal or the current actor", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ ...group, proposed_by: "another-subject" })),
  );
  await expect(preparePartnerGroup(scope, id, input)).rejects.toThrow();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ ...group, hospital_id: scope.tenantId })),
  );
  await expect(preparePartnerGroup(scope, id, input)).rejects.toThrow();
});
it("recovers the exact command and refuses changed or mismatched decision evidence", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(receipt)),
  );
  expect(await decidePartnerGroup(scope, id, command, decision)).toEqual(receipt);
  expect(await getPartnerGroupCommand(scope, id, command)).toEqual(receipt);
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ ...receipt, expected_version: 1, result_version: 2 })),
  );
  await expect(decidePartnerGroup(scope, id, command, decision)).rejects.toThrow();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ ...receipt, actor_subject: "another-reviewer" })),
  );
  await expect(decidePartnerGroup(scope, id, command, decision)).rejects.toThrow();
});
