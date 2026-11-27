import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { apiFetch } from "./client";
import { beginTenantTransition, cancelTenantTransition } from "./tenant-transition";
import {
  readVerificationPolicyDraft,
  saveVerificationPolicyDraft,
  listVerificationPolicyDrafts,
} from "./pharmacy-verification-policies";
import { VerificationPolicyDraftInputContract } from "./pharmacy-verification-contract";
vi.mock("./client", () => ({ apiFetch: vi.fn() }));
const request = vi.mocked(apiFetch);
const scope = {
  tenantId: "0199e000-0000-7000-8000-000000000010",
  pharmacyId: "0199e000-0000-7000-8000-000000000011",
  outletId: "0199e000-0000-7000-8000-000000000012",
  subject: "synthetic-preparer",
};
const policy = "0199e000-0000-7000-8000-000000000013";
const revision = "0199e000-0000-7000-8000-000000000014";
const input = {
  expected_version: 0,
  label: "Synthetic draft",
  second_check_required: true,
  review_valid_for_seconds: 600,
  query_response_target_seconds: 1200,
  reason: "Synthetic proposal",
};
const receipt = () => ({
  id: revision,
  policy_id: policy,
  outlet_id: scope.outletId,
  version: 1,
  ...input,
  prepared_by: scope.subject,
  created_at: "2026-11-27T10:00:00Z",
  state: "draft",
  clinical_authority: "not_evaluated",
});
beforeEach(() => request.mockReset());
afterEach(() => cancelTenantTransition());
it("sends account-bound draft intent and validates its retained receipt", async () => {
  request.mockResolvedValue(receipt());
  await expect(saveVerificationPolicyDraft(scope, policy, revision, input)).resolves.toEqual(
    receipt(),
  );
  const [path, options] = request.mock.calls[0]!;
  expect(path).toContain(
    `/pharmacies/${scope.pharmacyId}/outlets/${scope.outletId}/verification-policy-drafts/${policy}/revisions/${revision}`,
  );
  expect(options?.method).toBe("PUT");
  expect(JSON.parse(options?.body as string)).toEqual(input);
  const headers = new Headers(options?.headers);
  expect(headers.get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(headers.get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});
it.each([
  { outlet_id: policy },
  { policy_id: revision },
  { id: policy },
  { version: 2 },
  { prepared_by: "another-person" },
  { state: "active" },
  { clinical_authority: "approved" },
  { label: "different intent" },
])("rejects mismatched write receipts %j", async (change) => {
  request.mockResolvedValue({ ...receipt(), ...change });
  await expect(saveVerificationPolicyDraft(scope, policy, revision, input)).rejects.toThrow();
});
it("allows another authorized editor to read retained drafts without claiming their authorship", async () => {
  request.mockResolvedValue({ ...receipt(), prepared_by: "another-editor" });
  await expect(readVerificationPolicyDraft(scope, policy, revision)).resolves.toMatchObject({
    prepared_by: "another-editor",
  });
});
it("validates every paginated revision and preserves the opaque next cursor", async () => {
  request.mockResolvedValue({
    data: [receipt()],
    page: { has_more: true, next_cursor: "opaque+/=" },
  });
  await expect(listVerificationPolicyDrafts(scope, "cursor+/=")).resolves.toMatchObject({
    page: { next_cursor: "opaque+/=" },
  });
  expect(request.mock.calls[0]![0]).toContain("cursor=cursor%2B%2F%3D");
  request.mockResolvedValue({
    data: [{ ...receipt(), outlet_id: policy }],
    page: { has_more: false, next_cursor: null },
  });
  await expect(listVerificationPolicyDrafts(scope)).rejects.toThrow();
  request.mockResolvedValue({ data: [receipt()], page: { has_more: true, next_cursor: null } });
  await expect(listVerificationPolicyDrafts(scope)).rejects.toThrow();
});
it.each([
  { clinical_authority: "approved" },
  { approver_subject: "someone" },
  { emergency_supply: true },
  { verified_person_id: policy },
  { expected_version: undefined },
  { second_check_required: undefined },
  { review_valid_for_seconds: 9223372037 },
])("rejects authority assertions or invalid drafts %j", async (change) => {
  expect(VerificationPolicyDraftInputContract.safeParse({ ...input, ...change }).success).toBe(
    false,
  );
  await expect(
    saveVerificationPolicyDraft(scope, policy, revision, { ...input, ...change } as typeof input),
  ).rejects.toThrow();
  expect(request).not.toHaveBeenCalled();
});
it("refuses requests during account transitions", async () => {
  beginTenantTransition();
  await expect(saveVerificationPolicyDraft(scope, policy, revision, input)).rejects.toThrow();
  expect(request).not.toHaveBeenCalled();
});
it("rejects a response arriving after an account transition", async () => {
  request.mockImplementation(async () => {
    beginTenantTransition();
    return receipt();
  });
  await expect(readVerificationPolicyDraft(scope, policy, revision)).rejects.toThrow();
});
