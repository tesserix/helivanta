import { afterEach, expect, it, vi } from "vitest";
import {
  fetchTaxEvidence,
  prepareTaxEvidence,
  submitTaxEvidence,
  taxEvidenceFileSchema,
  taxEvidenceReceipt,
  type TaxEvidenceScope,
} from "./payroll-evidence";

afterEach(() => vi.unstubAllGlobals());

const staff = "22222222-2222-4222-8222-222222222222";
const revision = "77777777-7777-4777-8777-777777777777";
const fileId = "88888888-8888-4888-8888-888888888888";
const stamp = "2026-10-16T00:00:00Z";
const scope: TaxEvidenceScope = {
  declarationPath: `/workforce/hospitals/h/hr/payroll/staff/${staff}/tax-declarations/2026-27`,
  staffId: staff,
  revisionId: revision,
};
const saved = (over: object = {}) => ({
  id: fileId,
  hospital_id: "11111111-1111-4111-8111-111111111111",
  staff_id: staff,
  record_id: revision,
  record_kind: "tax_declaration",
  record_version: 2,
  category: "tax_declaration_evidence",
  filename: "synthetic-receipt.png",
  content_type: "image/png",
  size: 4,
  sha256: "a".repeat(64),
  status: "upload_pending",
  scan_attempt: 1,
  uploaded_at: null,
  scanned_at: null,
  failure_code: null,
  expires_at: stamp,
  created_at: stamp,
  updated_at: stamp,
  ...over,
});

it("accepts only evidence of this staff member's proposal", () => {
  expect(taxEvidenceReceipt(scope, saved()).id).toBe(fileId);
  expect(() => taxEvidenceReceipt(scope, saved({ record_id: fileId }))).toThrow(/ownership/);
  expect(() =>
    taxEvidenceReceipt(scope, saved({ staff_id: "33333333-3333-4333-8333-333333333333" })),
  ).toThrow(/ownership/);
  expect(() => taxEvidenceReceipt(scope, saved({ record_kind: "enrollment" }))).toThrow();
  expect(() => taxEvidenceReceipt(scope, saved(), revision)).toThrow(/ownership/);
});

it("refuses a listing that carries another proposal's file", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(
          JSON.stringify({ evidence: [saved({ record_id: fileId })], uploads_available: true }),
        ),
    ),
  );
  await expect(fetchTaxEvidence(scope, new AbortController().signal)).rejects.toThrow(/ownership/);
});

it("checks the chosen file and stops when the saved intent differs", async () => {
  expect(taxEvidenceFileSchema.safeParse({ file: null }).success).toBe(false);
  const text = new File(["x"], "notes.txt", { type: "text/plain" });
  expect(taxEvidenceFileSchema.safeParse({ file: text }).error?.issues[0]?.message).toBe(
    "Choose a PNG or JPEG image.",
  );
  const image = new File([new Uint8Array([137, 80, 78, 71])], "synthetic-receipt.png", {
    type: "image/png",
  });
  expect(taxEvidenceFileSchema.safeParse({ file: image }).success).toBe(true);
  const upload = await prepareTaxEvidence(scope, fileId, image, new AbortController().signal);
  expect(upload.input).toMatchObject({ revision_id: revision, size: 4 });
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      calls.push(url);
      return new Response(JSON.stringify(saved({ sha256: "b".repeat(64) })), { status: 201 });
    }),
  );
  await expect(submitTaxEvidence(scope, upload, new AbortController().signal)).rejects.toThrow(
    /does not match/,
  );
  expect(calls).toHaveLength(1);
});
