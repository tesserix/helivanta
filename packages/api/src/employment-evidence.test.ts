import { apiFetch } from "./client";
import {
  attachEmploymentEvidence,
  withdrawEmploymentEvidence,
  employmentEvidenceKey,
  eligibleEmploymentDocuments,
  employmentEvidenceDocumentsPath,
  ownedEmploymentEvidence,
} from "./employment-evidence";
vi.mock("./client", () => ({ apiFetch: vi.fn() }));
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-admin",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
  periodId: "019a0000-0000-7000-8000-000000000001",
  periodVersion: 1,
};
const id = "019a0000-0000-7000-8000-000000000002";
const command = "019a0000-0000-7000-8000-000000000003";
const file = "44444444-4444-4444-8444-444444444444";
const receipt = {
  id,
  revision_id: command,
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  period_id: scope.periodId,
  period_revision_id: scope.periodId,
  period_version: 1,
  version: 1,
  status: "attached",
  staff_version: 1,
  employment_epoch: 1,
  document_id: file,
  document_record_version: 1,
  filename: "SYN-contract.png",
  content_type: "image/png",
  size: 3,
  sha256: "a".repeat(64),
  source_generation: "1",
  released_generation: "2",
  scan_attempt: 1,
  scanned_at: "2026-10-29T00:00:00Z",
  proposed_by_subject: scope.subject,
  actor_subject: scope.subject,
  reason: "Synthetic contract",
  created_at: "2026-10-29T00:00:00Z",
  updated_at: "2026-10-29T00:00:00Z",
};
afterEach(() => vi.resetAllMocks());
it("pins the workspace and exact terms revision and validates attach receipt", async () => {
  vi.mocked(apiFetch).mockResolvedValue(receipt);
  expect(
    await attachEmploymentEvidence(scope, id, {
      command_id: command,
      document_id: file,
      reason: receipt.reason,
    }),
  ).toEqual(receipt);
  expect(apiFetch).toHaveBeenCalledWith(
    expect.stringContaining(`/employment-periods/${scope.periodId}/revisions/1/evidence/${id}`),
    expect.objectContaining({
      method: "PUT",
      cache: "no-store",
      headers: {
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      },
    }),
  );
  await attachEmploymentEvidence({ ...scope, hrIntake: true }, id, {
    command_id: command,
    document_id: file,
    reason: receipt.reason,
  });
  expect(vi.mocked(apiFetch).mock.lastCall?.[0]).toBe(
    `/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-periods/${scope.periodId}/revisions/1/evidence/${id}?hr_intake=v1`,
  );
  expect(employmentEvidenceKey({ ...scope, hrIntake: true })).not.toEqual(
    employmentEvidenceKey(scope),
  );
  vi.mocked(apiFetch).mockResolvedValue({ ...receipt, period_version: 2 });
  await expect(
    attachEmploymentEvidence(scope, id, {
      command_id: command,
      document_id: file,
      reason: receipt.reason,
    }),
  ).rejects.toThrow("receipt");
  vi.mocked(apiFetch).mockResolvedValue({ ...receipt, document_id: scope.staffId });
  await expect(
    attachEmploymentEvidence(scope, id, {
      command_id: command,
      document_id: file,
      reason: receipt.reason,
    }),
  ).rejects.toThrow("receipt");
});
it("accepts only the terminal withdrawal receipt at the expected version", async () => {
  vi.mocked(apiFetch).mockResolvedValue({ ...receipt, status: "withdrawn", version: 2 });
  expect(
    await withdrawEmploymentEvidence(scope, id, {
      command_id: command,
      expected_version: 1,
      reason: "Synthetic withdrawal",
    }),
  ).toMatchObject({ status: "withdrawn", version: 2 });
  await withdrawEmploymentEvidence({ ...scope, hrIntake: true }, id, {
    command_id: command,
    expected_version: 1,
    reason: "Synthetic withdrawal",
  });
  expect(vi.mocked(apiFetch).mock.lastCall?.[0]).toBe(
    `/workforce/hospitals/${scope.hospitalId}/staff/${scope.staffId}/employment-periods/${scope.periodId}/revisions/1/evidence/${id}/withdraw?hr_intake=v1`,
  );
  vi.mocked(apiFetch).mockResolvedValue(receipt);
  await expect(
    withdrawEmploymentEvidence(scope, id, {
      command_id: command,
      expected_version: 1,
      reason: "Synthetic withdrawal",
    }),
  ).rejects.toThrow("receipt");
});
describe("pharmacy employment evidence (ADR-0054 slice 4b)", () => {
  const pharmacyId = "0190a000-0000-7000-8000-000000000005";
  const { tenantId, subject, staffId, periodId, periodVersion } = scope;
  const pharmacy = { tenantId, subject, staffId, periodId, periodVersion, pharmacyId };
  const binding = Object.fromEntries(
    Object.entries(receipt).filter(([key]) => key !== "hospital_id"),
  );
  const pharmacyReceipt = { ...binding, pharmacy_id: pharmacyId };
  const attach = { command_id: command, document_id: file, reason: receipt.reason };
  it("attaches under the pharmacy family and accepts only its own receipt", async () => {
    vi.mocked(apiFetch).mockResolvedValueOnce(pharmacyReceipt);
    expect(await attachEmploymentEvidence(pharmacy, id, attach)).toEqual(pharmacyReceipt);
    expect(vi.mocked(apiFetch).mock.lastCall?.[0]).toBe(
      `/workforce/pharmacies/${pharmacyId}/staff/${scope.staffId}/employment-periods/${scope.periodId}/revisions/1/evidence/${id}`,
    );
    for (const other of [
      { ...binding, hospital_id: pharmacyId },
      { ...pharmacyReceipt, pharmacy_id: "0190a000-0000-7000-8000-000000000006" },
    ]) {
      vi.mocked(apiFetch).mockResolvedValueOnce(other);
      await expect(attachEmploymentEvidence(pharmacy, id, attach)).rejects.toThrow("receipt");
    }
  });
  it("withdraws under the pharmacy family", async () => {
    vi.mocked(apiFetch).mockResolvedValueOnce({
      ...pharmacyReceipt,
      status: "withdrawn",
      version: 2,
    });
    await withdrawEmploymentEvidence(pharmacy, id, {
      command_id: command,
      expected_version: 1,
      reason: "Synthetic withdrawal",
    });
    expect(vi.mocked(apiFetch).mock.lastCall?.[0]).toBe(
      `/workforce/pharmacies/${pharmacyId}/staff/${scope.staffId}/employment-periods/${scope.periodId}/revisions/1/evidence/${id}/withdraw`,
    );
  });
  it("keys a pharmacy apart from a hospital with the same identifier", () => {
    expect(employmentEvidenceKey(pharmacy)).not.toEqual(
      employmentEvidenceKey({ ...scope, hospitalId: pharmacyId }),
    );
  });
  it("reads only rows bound to the scoped terms revision", () => {
    expect(ownedEmploymentEvidence(pharmacy, [pharmacyReceipt])).toEqual([pharmacyReceipt]);
    expect(ownedEmploymentEvidence(scope, [receipt])).toEqual([receipt]);
    for (const rows of [
      [{ ...pharmacyReceipt, period_version: 2 }],
      [{ ...binding, hospital_id: pharmacyId }],
      [pharmacyReceipt, { ...pharmacyReceipt, staff_id: id }],
      "not rows",
    ])
      expect(ownedEmploymentEvidence(pharmacy, rows)).toBeUndefined();
    expect(ownedEmploymentEvidence(scope, [pharmacyReceipt])).toBeUndefined();
  });
  it("offers only the pharmacy's released employment files for the current Staff version", () => {
    expect(employmentEvidenceDocumentsPath(pharmacy)).toBe(
      `/workforce/pharmacies/${pharmacyId}/staff/${scope.staffId}/documents`,
    );
    const document = {
      id: file,
      pharmacy_id: pharmacyId,
      staff_id: scope.staffId,
      record_kind: "enrollment",
      record_id: scope.staffId,
      record_version: 1,
      category: "employment_evidence",
      filename: "SYN-contract.png",
      content_type: "image/png",
      size: 3,
      sha256: "a".repeat(64),
      status: "available",
      scan_attempt: 1,
      uploaded_at: "2026-10-29T00:00:00Z",
      scanned_at: "2026-10-29T00:00:00Z",
      failure_code: null,
      expires_at: "2026-10-30T00:00:00Z",
      created_at: "2026-10-29T00:00:00Z",
      updated_at: "2026-10-29T00:00:00Z",
    };
    const identity = { ...document, id: command, category: "identity_evidence" };
    const stale = { ...document, id, record_version: 2 };
    expect(eligibleEmploymentDocuments(pharmacy, 1, [document, identity, stale])).toEqual([
      document,
    ]);
    const unowned = Object.fromEntries(
      Object.entries(document).filter(([key]) => key !== "pharmacy_id"),
    );
    for (const rows of [
      [document, { ...document, pharmacy_id: "0190a000-0000-7000-8000-000000000006" }],
      [{ ...unowned, hospital_id: pharmacyId }],
      [{ ...document, staff_id: id }],
      "not rows",
    ])
      expect(eligibleEmploymentDocuments(pharmacy, 1, rows)).toBeUndefined();
  });
});
