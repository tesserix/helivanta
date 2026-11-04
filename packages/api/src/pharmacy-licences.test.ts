import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiFetch, apiFetchResponse } from "./client";
import {
  downloadLicenceEvidence,
  prepareLicenceEvidence,
  recordPharmacyLicence,
  retryLicenceEvidence,
  submitLicenceEvidence,
  type PharmacyLicenceEvidence,
} from "./pharmacy-licences";

vi.mock("./client", () => ({ apiFetch: vi.fn(), apiFetchResponse: vi.fn() }));
const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "019a1b62-0000-7000-8000-000000000001",
  outletId: "019a1b62-0000-7000-8000-000000000002",
};
const licence = "019a1b62-0000-7000-8000-000000000003";
const fileID = "019a1b62-0000-7000-8000-000000000004";
const fields = {
  licence_kind: "retail" as const,
  form_label: "TEST Form 20",
  licence_number: "TEST-DL",
  issuing_authority: "TEST Controller",
  valid_from: "2026-01-01",
  valid_until: "2028-12-31",
  pharmacist_name: "TEST Pharmacist",
  pharmacist_registration: "TEST Registration",
};
const evidence: PharmacyLicenceEvidence = {
  id: fileID,
  outlet_id: scope.outletId,
  licence_id: licence,
  filename: "TEST.png",
  content_type: "image/png",
  size: 3,
  status: "available",
  scan_attempt: 1,
  uploaded_at: "2026-11-01T00:00:00Z",
  scanned_at: "2026-11-01T00:00:01Z",
  failure_code: null,
  uploader_subject: "TEST-author",
  expires_at: "2026-11-01T00:30:00Z",
  created_at: "2026-11-01T00:00:00Z",
  updated_at: "2026-11-01T00:00:01Z",
};
beforeEach(() => vi.clearAllMocks());

describe("private pharmacy licence commands", () => {
  it("rejects a declaration receipt for another outlet", async () => {
    vi.mocked(apiFetch).mockResolvedValue({
      ...fields,
      id: licence,
      outlet_id: scope.pharmacyId,
      entered_by: "TEST-author",
      created_at: "2026-11-01T00:00:00Z",
      decision: null,
      decision_reason: null,
      reviewed_by: null,
      reviewed_at: null,
      reviewed_evidence_file_id: null,
    });
    await expect(recordPharmacyLicence(scope, licence, fields)).rejects.toThrow(
      "does not belong to this outlet",
    );
  });

  it("does not send image bytes after a wrong-parent intent receipt", async () => {
    vi.mocked(apiFetch).mockResolvedValue({
      ...evidence,
      status: "upload_pending",
      licence_id: scope.pharmacyId,
    });
    await expect(
      submitLicenceEvidence(
        scope,
        licence,
        {
          id: fileID,
          file: new File(["PNG"], "TEST.png", { type: "image/png" }),
          input: {
            filename: "TEST.png",
            content_type: "image/png",
            size: 3,
            sha256: "a".repeat(64),
          },
        },
        new AbortController().signal,
      ),
    ).rejects.toThrow("does not belong to this licence");
    expect(apiFetch).toHaveBeenCalledTimes(1);
  });

  it("preserves a completed upload on an ambiguous retry without uploading bytes twice", async () => {
    vi.mocked(apiFetch).mockResolvedValue(evidence);
    const result = await submitLicenceEvidence(
      scope,
      licence,
      {
        id: fileID,
        file: new File(["PNG"], "TEST.png", { type: "image/png" }),
        input: { filename: "TEST.png", content_type: "image/png", size: 3, sha256: "a".repeat(64) },
      },
      new AbortController().signal,
    );
    expect(result.status).toBe("available");
    expect(apiFetch).toHaveBeenCalledTimes(1);
  });

  it("retries only the next retained scan attempt", async () => {
    vi.mocked(apiFetch).mockResolvedValue({ ...evidence, status: "scan_pending", scan_attempt: 3 });
    await retryLicenceEvidence(
      scope,
      { ...evidence, status: "scan_error", scan_attempt: 2 },
      new AbortController().signal,
    );
    expect(apiFetch).toHaveBeenCalledWith(
      expect.stringContaining(`/evidence/${fileID}/retry/3`),
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("refuses pending or rejected downloads before requesting disclosure", async () => {
    for (const status of ["scan_pending", "rejected"] as const) {
      await expect(
        downloadLicenceEvidence(scope, { ...evidence, status }, new AbortController().signal),
      ).rejects.toThrow("clean scan");
    }
    expect(apiFetchResponse).not.toHaveBeenCalled();
  });

  it("refuses a truncated binary download", async () => {
    vi.mocked(apiFetchResponse).mockResolvedValue(
      new Response(new Blob(["X"], { type: "image/png" })),
    );
    await expect(
      downloadLicenceEvidence(scope, evidence, new AbortController().signal),
    ).rejects.toThrow("does not match");
  });

  it("rejects unsafe filenames and unsupported content before reading a file", async () => {
    for (const file of [
      new File(["X"], "../TEST.png", { type: "image/png" }),
      new File(["X"], "TEST.pdf", { type: "application/pdf" }),
    ]) {
      await expect(
        prepareLicenceEvidence(file, fileID, new AbortController().signal),
      ).rejects.toThrow();
    }
    expect(apiFetch).not.toHaveBeenCalled();
  });

  it("does not send a command after its scope was aborted", async () => {
    const controller = new AbortController();
    controller.abort();
    await expect(
      prepareLicenceEvidence(
        new File(["PNG"], "TEST.png", { type: "image/png" }),
        fileID,
        controller.signal,
      ),
    ).rejects.toThrow();
    expect(apiFetch).not.toHaveBeenCalled();
  });
});
