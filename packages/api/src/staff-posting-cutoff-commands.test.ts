import { apiFetch } from "./client";

vi.mock("./client", () => ({ apiFetch: vi.fn() }));

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-cutoff-administrator",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
  access: "scoped" as const,
};
const command = "019a0000-0000-7000-8000-000000000001";
const proposal = "019a0000-0000-7000-8000-000000000002";
const input = {
  expected_revision_id: "019a0000-0000-7000-8000-000000000003",
  expected_cutoff_version: 0,
  mode: "immediate" as const,
  reason: "Synthetic responsibility ending",
  sources_examined: true,
};
const receipt = {
  id: command,
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  proposal_id: proposal,
  ...input,
  requested_cutoff_at: null,
  version: 1,
  cutoff_at: "2026-11-05T00:00:00.123456Z",
  recorded_at: "2026-11-05T00:00:00.123456Z",
};
afterEach(() => vi.resetAllMocks());

it("verifies the exact cutoff command and intent on writes and receipt recovery", async () => {
  const { recordStaffPostingCutoff, getStaffPostingCutoffReceipt } =
    await import("./staff-posting-proposals");
  for (const save of [recordStaffPostingCutoff, getStaffPostingCutoffReceipt]) {
    vi.mocked(apiFetch).mockResolvedValue(receipt);
    expect(await save(scope, proposal, command, input)).toEqual(receipt);
    expect(apiFetch).toHaveBeenLastCalledWith(
      `/workforce/hospitals/${scope.hospitalId}/posting-staff/${scope.staffId}/posting-proposals/${proposal}/cutoffs/commands/${command}`,
      expect.objectContaining({
        cache: "no-store",
        headers: {
          "X-Helivanta-Expected-Tenant": scope.tenantId,
          "X-Helivanta-Expected-Subject": scope.subject,
        },
      }),
    );
    for (const change of [
      { id: proposal },
      { proposal_id: command },
      { hospital_id: scope.tenantId },
      { staff_id: scope.hospitalId },
      { expected_revision_id: command },
      { expected_cutoff_version: 1, version: 2 },
      { reason: "Different intent" },
    ]) {
      vi.mocked(apiFetch).mockResolvedValue({ ...receipt, ...change });
      await expect(save(scope, proposal, command, input)).rejects.toThrow("cutoff receipt");
    }
  }
});
