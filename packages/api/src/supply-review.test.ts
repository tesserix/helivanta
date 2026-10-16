import { afterEach, expect, it, vi } from "vitest";
import { recordSupplyReview, supplyReviewFormSchema } from "./supply-review";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-reviewer",
  partyId: "0199e000-0000-7000-8000-000000000001",
  licenseId: "0199e000-0000-7000-8000-000000000002",
};
const id = "0199e000-0000-7000-8000-000000000003";
const input = {
  expected_version: 1,
  expected_party_version: 1,
  expected_premise_version: 1,
  expected_license_version: 1,
  evidence_id: "0199e000-0000-7000-8000-000000000004",
  action: "verify" as const,
  reason: "TEST authority check",
  method: "authority_contact" as const,
  reference: "TEST-REFERENCE",
};
const receipt = {
  id,
  party_id: scope.partyId,
  license_id: scope.licenseId,
  premise_id: "0199e000-0000-7000-8000-000000000005",
  version: 2,
  party_version: 1,
  premise_version: 1,
  license_version: 1,
  evidence_id: input.evidence_id,
  action: input.action,
  status: "verified",
  reason: input.reason,
  method: input.method,
  reference: input.reference,
  actor_subject: scope.subject,
  created_at: "2026-10-16T00:00:00Z",
  updated_at: "2026-10-16T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());
it("rejects a saved decision for a different source revision despite matching command ID", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify({ ...receipt, party_version: 2 }))),
  );
  await expect(recordSupplyReview(scope, id, input, new AbortController().signal)).rejects.toThrow(
    "does not match",
  );
});
it("requires an authority reference and forbids treating a scan as verification", () => {
  expect(
    supplyReviewFormSchema.safeParse({ ...input, method: "not_applicable", reference: "" }).success,
  ).toBe(false);
  expect(supplyReviewFormSchema.safeParse({ ...input, method: "clamav" }).success).toBe(false);
  expect(supplyReviewFormSchema.safeParse(input).success).toBe(true);
});

it("binds the official client request to its expected signed-in subject", async () => {
  let headers: Headers | undefined;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      headers = new Headers(init?.headers);
      return new Response(JSON.stringify(receipt));
    }),
  );
  await recordSupplyReview(scope, id, input, new AbortController().signal);
  expect(headers?.get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});
