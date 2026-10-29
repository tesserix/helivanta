import { apiFetch } from "./client";
import { saveEmploymentPeriodDraft } from "./employment-periods";
vi.mock("./client", () => ({ apiFetch: vi.fn() }));
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-admin",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
};
const id = "019a0000-0000-7000-8000-000000000001";
const input = {
  command_id: "019a0000-0000-7000-8000-000000000002",
  expected_version: 0,
  staff_version: 1,
  employment_epoch: 1,
  engagement_kind: "employee" as const,
  duration: "ongoing" as const,
  starts_at: "2026-11-01T00:00:00Z",
  contracting_party: "",
  sponsor_id: "",
  reason: "Synthetic onboarding",
  evidence_reference: "SYN-OFFER",
};
afterEach(() => vi.resetAllMocks());
it("pins the Staff workspace and verifies the owner's receipt", async () => {
  const { command_id: commandId, expected_version: expectedVersion, ...terms } = input;
  const receipt = {
    ...terms,
    id,
    revision_id: commandId,
    hospital_id: scope.hospitalId,
    staff_id: scope.staffId,
    version: expectedVersion + 1,
    status: "draft",
    proposed_by_subject: scope.subject,
    actor_subject: scope.subject,
    created_at: "2026-10-29T00:00:00Z",
    updated_at: "2026-10-29T00:00:00Z",
  };
  vi.mocked(apiFetch).mockResolvedValue(receipt);
  expect(await saveEmploymentPeriodDraft(scope, id, input)).toMatchObject({
    id,
    version: 1,
    status: "draft",
  });
  expect(apiFetch).toHaveBeenCalledWith(
    expect.stringContaining(`/staff/${scope.staffId}/employment-periods/${id}`),
    expect.objectContaining({
      cache: "no-store",
      headers: {
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      },
    }),
  );
  vi.mocked(apiFetch).mockResolvedValue({ ...receipt, staff_id: scope.hospitalId });
  await expect(saveEmploymentPeriodDraft(scope, id, input)).rejects.toThrow("receipt");
});
it("rejects incomplete contractor terms before transmitting", async () => {
  await expect(
    saveEmploymentPeriodDraft(scope, id, { ...input, engagement_kind: "agency_worker" }),
  ).rejects.toThrow();
  expect(apiFetch).not.toHaveBeenCalled();
});
