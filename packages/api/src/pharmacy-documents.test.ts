import { afterEach, describe, expect, it, vi } from "vitest";
import { documentQueryKey } from "./document-queries";
import { readDocumentClassification } from "./document-classification";
import {
  documentPath,
  downloadStaffDocument,
  retryDocumentScan,
  revokeStaffDocument,
  submitDocumentUpload,
  type DocumentScope,
  type PharmacyDocumentScope,
} from "./documents";
import type { DocumentInput, PharmacyStaffDocument } from "./documents-fields";

const staffId = "11111111-1111-4111-8111-111111111111";
const pharmacyId = "0190a000-0000-7000-8000-000000000001";
const fileID = "55555555-5555-4555-8555-555555555555";
const scope: PharmacyDocumentScope = {
  subject: "synthetic-document-account",
  tenantId: "33333333-3333-4333-8333-333333333333",
  pharmacyId,
  staffId,
};
const input: DocumentInput = {
  record_kind: "credential",
  record_id: "22222222-2222-4222-8222-222222222222",
  category: "credential_evidence",
  filename: "synthetic-registration.png",
  content_type: "image/png",
  size: 1,
  sha256: "a".repeat(64),
};
const receipt: PharmacyStaffDocument = {
  ...input,
  id: fileID,
  pharmacy_id: pharmacyId,
  staff_id: staffId,
  record_version: 1,
  status: "upload_pending",
  scan_attempt: 1,
  uploaded_at: null,
  scanned_at: null,
  failure_code: null,
  created_at: "2026-11-08T00:00:00Z",
  updated_at: "2026-11-08T00:00:00Z",
  expires_at: "2026-11-08T00:30:00Z",
};
const upload = () => ({
  id: fileID,
  input,
  file: new File([new Uint8Array([1])], input.filename, { type: input.content_type }),
});
const signal = new AbortController().signal;
afterEach(() => vi.unstubAllGlobals());

describe("pharmacy Staff documents", () => {
  it("sends every file command to the pharmacy family with the account pinned and no intake scope", async () => {
    const requests: Array<{ url: string; init: RequestInit }> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init: RequestInit) => {
        requests.push({ url, init });
        const path = new URL(url, "https://example.invalid").pathname;
        if (path.endsWith("/content") && init.method !== "PUT")
          return new Response(new Uint8Array([1]), { headers: { "Content-Type": "image/png" } });
        return Response.json({
          ...receipt,
          status: path.endsWith("/revocation")
            ? "revoked"
            : path.endsWith("/content") || path.includes("/retries/")
              ? "scan_pending"
              : "upload_pending",
          scan_attempt: path.includes("/retries/") ? 2 : 1,
        });
      }),
    );
    expect((await submitDocumentUpload(scope, upload(), signal)).status).toBe("scan_pending");
    await retryDocumentScan(scope, fileID, 2, signal);
    expect((await revokeStaffDocument(scope, fileID, signal)).status).toBe("revoked");
    await downloadStaffDocument(scope, { ...receipt, status: "available" }, signal);
    const base = `/api/v1/workforce/pharmacies/${pharmacyId}/staff/${staffId}/documents/${fileID}`;
    expect(requests.map(({ url, init }) => `${init.method ?? "GET"} ${url}`)).toEqual([
      `PUT ${base}`,
      `PUT ${base}/content`,
      `PUT ${base}/retries/2`,
      `PUT ${base}/revocation`,
      `GET ${base}/content`,
    ]);
    for (const { init } of requests) {
      expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
      expect(new Headers(init.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
    }
  });

  it("refuses a receipt that names a hospital, another pharmacy or another Staff record", async () => {
    for (const body of [
      { ...receipt, pharmacy_id: "0190a000-0000-7000-8000-000000000002" },
      { ...receipt, staff_id: "66666666-6666-4666-8666-666666666666" },
      { ...receipt, hospital_id: pharmacyId },
      { ...receipt, pharmacy_id: undefined, hospital_id: pharmacyId },
    ]) {
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => Response.json({ ...body, status: "revoked" })),
      );
      await expect(revokeStaffDocument(scope, fileID, signal)).rejects.toThrow();
    }
  });

  it("refuses to download a file this pharmacy does not own without sending a request", async () => {
    const fetcher = vi.fn();
    vi.stubGlobal("fetch", fetcher);
    await expect(
      downloadStaffDocument(
        scope,
        { ...receipt, status: "available", pharmacy_id: "0190a000-0000-7000-8000-000000000002" },
        signal,
      ),
    ).rejects.toThrow("ownership");
    expect(fetcher).not.toHaveBeenCalled();
  });

  it("refuses a scope that names both or neither establishment, or a pharmacy with intake", () => {
    expect(documentPath(scope)).toBe(`/workforce/pharmacies/${pharmacyId}/staff/${staffId}`);
    for (const invalid of [
      { ...scope, hospitalId: "44444444-4444-4444-8444-444444444444" },
      { tenantId: scope.tenantId, subject: scope.subject, staffId },
      { ...scope, hrIntake: true },
      { ...scope, pharmacyId: "44444444-4444-4444-8444-444444444444" },
    ])
      expect(() => documentPath(invalid as unknown as DocumentScope)).toThrow();
  });

  it("keys the pharmacy cache apart from a hospital with the same identifier", () => {
    expect(documentQueryKey(scope)).not.toEqual(
      documentQueryKey({ ...scope, pharmacyId: undefined, hospitalId: pharmacyId }),
    );
    expect(documentQueryKey(scope)).toContain("pharmacy");
  });

  it("keeps classification and intake hospital-only at compile time", () => {
    // @ts-expect-error a pharmacy scope cannot carry HR intake
    const intake: PharmacyDocumentScope = { ...scope, hrIntake: true };
    // @ts-expect-error a pharmacy file has no classification route
    const classify = () => readDocumentClassification(scope, fileID, signal);
    expect([intake.hrIntake, typeof classify]).toEqual([true, "function"]);
  });
});
