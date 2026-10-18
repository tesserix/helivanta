import { afterEach, expect, it, vi } from "vitest";
import {
  citableEvidence,
  credentialDecisionFormSchema,
  credentialRegisterPath,
  credentialReviewFormSchema,
  decideCredentialReview,
  proposeCredentialReview,
} from "./credentials";
import type { StaffDocument } from "./documents-fields";

const hospital = "11111111-1111-4111-8111-111111111111";
const staff = "22222222-2222-4222-8222-222222222222";
const credential = "33333333-3333-4333-8333-333333333333";
const review = "44444444-4444-4444-8444-444444444444";
const file = "55555555-5555-4555-8555-555555555555";
const scope = { hospitalId: hospital, staffId: staff };
const stamp = "2026-10-18T00:00:00Z";
const saved = {
  id: review,
  staff_id: staff,
  credential_id: credential,
  base_version: 0,
  status: "pending",
  type: "Synthetic registration",
  number: "SYN-1",
  issuer: "Synthetic Council",
  valid_from: "2026-01-01",
  valid_until: "2027-12-31",
  evidence_document_ids: [file],
  reason: "Checked the synthetic register",
  proposed_by_subject: "synthetic-hr",
  decision_note: "",
  created_at: stamp,
};
const proposal = {
  expected_version: 0,
  number: "SYN-1",
  valid_from: "2026-01-01",
  valid_until: "2027-12-31",
  evidence_document_ids: [file],
  reason: "Checked the synthetic register",
};
afterEach(() => vi.unstubAllGlobals());

it("needs evidence, a reason and an ordered period before asking the API", () => {
  const result = credentialReviewFormSchema.safeParse({
    ...proposal,
    valid_until: "2025-12-31",
    evidence_document_ids: [],
    reason: " ",
  });
  expect(result.success).toBe(false);
  const messages = Object.fromEntries(
    (result.error?.issues ?? []).map((i) => [i.path.join("."), i.message]),
  );
  expect(messages).toEqual({
    evidence_document_ids: "Choose at least one evidence file.",
    reason: "Say what you checked.",
    valid_until: "Expiry must be on or after the valid-from date.",
  });
  expect(credentialReviewFormSchema.safeParse(proposal).success).toBe(true);
});

it("needs a note only to reject", () => {
  expect(credentialDecisionFormSchema.safeParse({ decision: "approve", note: "" }).success).toBe(
    true,
  );
  const rejected = credentialDecisionFormSchema.safeParse({ decision: "reject", note: "  " });
  expect(rejected.error?.issues[0]?.message).toBe("Say why you reject it.");
});

it("offers only this credential's released or scanning evidence", () => {
  const base = {
    record_kind: "credential",
    record_id: credential,
    category: "credential_evidence",
    status: "available",
  };
  const files = [
    { ...base, id: "a" },
    { ...base, id: "b", status: "scan_pending" },
    { ...base, id: "c", status: "revoked" },
    { ...base, id: "d", record_id: review },
    { ...base, id: "e", record_kind: "qualification" },
  ] as unknown as StaffDocument[];
  expect(citableEvidence(files, credential).map((f) => f.id)).toEqual(["a", "b"]);
});

it("builds the register path only for a valid window", () => {
  expect(credentialRegisterPath(hospital, null)).toBe(
    `/workforce/hospitals/${hospital}/hr/credentials`,
  );
  expect(credentialRegisterPath(hospital, 30)).toBe(
    `/workforce/hospitals/${hospital}/hr/credentials?expiring_within=30`,
  );
  expect(() => credentialRegisterPath(hospital, 366)).toThrow();
});

it("proposes to the review id and refuses a receipt for another credential", async () => {
  const calls: { url: string; method?: string; body?: unknown }[] = [];
  let body: unknown = saved;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      calls.push({ url, method: init.method, body: JSON.parse(String(init.body)) });
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  await expect(proposeCredentialReview(scope, credential, review, proposal)).resolves.toEqual(
    saved,
  );
  expect(calls[0]).toEqual({
    url: `/api/v1/workforce/hospitals/${hospital}/staff/${staff}/credentials/${credential}/reviews/${review}`,
    method: "PUT",
    body: proposal,
  });
  body = { ...saved, credential_id: file };
  await expect(
    decideCredentialReview(scope, credential, review, {
      expected_version: 0,
      decision: "approve",
      note: "",
    }),
  ).rejects.toThrow("The saved review did not match this credential.");
  expect(calls[1]?.url).toMatch(/\/reviews\/[0-9a-f-]+\/decision$/);
});
