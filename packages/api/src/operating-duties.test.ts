import { afterEach, expect, it, vi } from "vitest";
import {
  prepareOperatingDuty,
  recoverOperatingDutyProposal,
  decideOperatingDuty,
  recoverOperatingDutyCommand,
  listOperatingDuties,
} from "./operating-duties";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-admin",
  employerKind: "supplier" as const,
  employerId: "019a55c0-1111-7111-8111-111111111112",
  staffId: "019a55c0-1111-7111-8111-111111111113",
};
const duty = "019a55c0-1111-7111-8111-111111111114",
  command = "019a55c0-1111-7111-8111-111111111115";
const input = {
  scope: {
    employer_kind: scope.employerKind,
    employer_id: scope.employerId,
    location_id: "019a55c0-1111-7111-8111-111111111116",
    expected_employer_version: 1,
    expected_location_version: 1,
  },
  identity: {
    staff_id: scope.staffId,
    staff_version: 1,
    link_id: "019a55c0-1111-7111-8111-111111111117",
    link_version: 2,
  },
  subject: "TEST-receiver",
  template_key: "supply.inventory_receiver.v1",
  reason: "TEST receiving duty",
  valid_from: "2026-11-07T00:00:00Z",
  expires_at: "2027-01-01T00:00:00Z",
};
const row = {
  owner_tenant_id: scope.tenantId,
  id: duty,
  input,
  status: "pending_review",
  version: 1,
  prepared_by: scope.subject,
  created_at: "2026-11-07T00:00:00Z",
  updated_at: "2026-11-07T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());
it("retains exact proposal intent and recovers using GET only", async () => {
  const request = vi.fn<typeof fetch>().mockImplementation(async () => Response.json(row));
  vi.stubGlobal("fetch", request);
  expect(await prepareOperatingDuty(scope, duty, input)).toEqual(row);
  expect(await recoverOperatingDutyProposal(scope, duty, input)).toEqual(row);
  expect(request.mock.calls.map(([url, init]) => [url, init?.method ?? "GET"])).toEqual([
    [`/api/v1/iam/operating-duties/${duty}`, "PUT"],
    [`/api/v1/iam/operating-duties/${duty}/proposal-receipt`, "GET"],
  ]);
  expect(new Headers(request.mock.calls[0]![1]?.headers).get("X-Helivanta-Expected-Subject")).toBe(
    scope.subject,
  );
});
it.each([
  { owner_tenant_id: "22222222-2222-4222-8222-222222222222" },
  { id: command },
  { prepared_by: "TEST-other" },
  { input: { ...input, subject: "TEST-other" } },
  { input: { ...input, scope: { ...input.scope, location_id: command } } },
  { input: { ...input, identity: { ...input.identity, link_version: 3 } } },
  { status: "active", version: 3 },
])("refuses a mismatched proposal recovery %j", async (change) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ ...row, ...change })));
  await expect(recoverOperatingDutyProposal(scope, duty, input)).rejects.toThrow();
});
it("checks exact review receipts without confusing activation with approval", async () => {
  const decision = {
    expected_version: 1,
    decision: "approve" as const,
    reason: "TEST independent review",
  };
  const receipt = {
    id: command,
    duty_id: duty,
    input: decision,
    result: { ...row, status: "approved_pending_activation", version: 2 },
    actor_subject: scope.subject,
    created_at: row.created_at,
  };
  const request = vi.fn<typeof fetch>().mockImplementation(async () => Response.json(receipt));
  vi.stubGlobal("fetch", request);
  expect(await decideOperatingDuty(scope, duty, command, decision)).toEqual(receipt);
  expect(await recoverOperatingDutyCommand(scope, duty, command, decision)).toEqual(receipt);
  expect(request.mock.calls[1]![1]?.method ?? "GET").toBe("GET");
  request.mockResolvedValue(
    Response.json({ ...receipt, result: { ...receipt.result, status: "active" } }),
  );
  await expect(recoverOperatingDutyCommand(scope, duty, command, decision)).rejects.toThrow();
});
it("rejects private cross-employer rows and inconsistent continuation", async () => {
  const request = vi
    .fn<typeof fetch>()
    .mockImplementation(async () =>
      Response.json({ data: [row], page: { has_more: false, next_cursor: null } }),
    );
  vi.stubGlobal("fetch", request);
  expect((await listOperatingDuties(scope, null)).data).toEqual([row]);
  expect(request.mock.calls[0]![0]).toBe(
    `/api/v1/iam/operating-employers/supplier/${scope.employerId}/staff/${scope.staffId}/duties?limit=25`,
  );
  request.mockResolvedValue(
    Response.json({
      data: [{ ...row, input: { ...input, identity: { ...input.identity, staff_id: command } } }],
      page: { has_more: false, next_cursor: null },
    }),
  );
  await expect(listOperatingDuties(scope, null)).rejects.toThrow();
  request.mockResolvedValue(
    Response.json({ data: [row, row], page: { has_more: false, next_cursor: null } }),
  );
  await expect(listOperatingDuties(scope, null)).rejects.toThrow();
  request.mockResolvedValue(
    Response.json({ data: [row], page: { has_more: true, next_cursor: null } }),
  );
  await expect(listOperatingDuties(scope, null)).rejects.toThrow();
});
