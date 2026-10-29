import { apiFetch } from "./client";
import { attachEmploymentEvidence, withdrawEmploymentEvidence } from "./employment-evidence";
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
  vi.mocked(apiFetch).mockResolvedValue(receipt);
  await expect(
    withdrawEmploymentEvidence(scope, id, {
      command_id: command,
      expected_version: 1,
      reason: "Synthetic withdrawal",
    }),
  ).rejects.toThrow("receipt");
});
