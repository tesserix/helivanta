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
const approved = {
  id: proposal,
  revision_id: input.expected_revision_id,
  command_id: "019a0000-0000-7000-8000-000000000004",
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  version: 2,
  status: "approved",
  grants_access: false,
  draft_id: "019a0000-0000-7000-8000-000000000005",
  draft_revision_id: "019a0000-0000-7000-8000-000000000006",
  draft_version: 1,
  generation_id: "019a0000-0000-7000-8000-000000000007",
  staff_version: 2,
  employment_epoch: 2,
  department_id: "019a0000-0000-7000-8000-000000000008",
  department_revision_id: "019a0000-0000-7000-8000-000000000009",
  starts_at: "2026-11-04T00:00:00Z",
  ends_at: null,
  eligibility_until: "2027-01-01T00:00:00Z",
  created_at: "2026-11-04T00:00:00Z",
  recorded_at: "2026-11-04T00:00:01Z",
} as const;
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

it("binds administrative context to the exact approved posting and rejects unrelated private cutoffs", async () => {
  const { getStaffPostingCutoffContext } = await import("./staff-posting-proposals");
  const context = { proposal: approved, cutoff: null, checked_at: "2026-11-05T00:00:01Z" };
  vi.mocked(apiFetch).mockResolvedValue(context);
  expect(await getStaffPostingCutoffContext(scope, approved)).toEqual(context);
  expect(apiFetch).toHaveBeenLastCalledWith(
    expect.stringContaining(
      `/${proposal}/cutoffs/context?expected_revision_id=${approved.revision_id}`,
    ),
    expect.objectContaining({ cache: "no-store" }),
  );
  const current = { ...context, cutoff: receipt };
  vi.mocked(apiFetch).mockResolvedValue(current);
  expect(await getStaffPostingCutoffContext(scope, approved)).toEqual(current);
  for (const change of [
    { proposal: { ...approved, hospital_id: scope.tenantId } },
    { proposal: { ...approved, revision_id: command } },
    { proposal: { ...approved, version: 3 } },
    { proposal: { ...approved, status: "pending" } },
    { cutoff: { ...receipt, staff_id: scope.hospitalId } },
    { cutoff: { ...receipt, expected_revision_id: command } },
    { checked_at: "2026-11-05T00:00:00Z" },
    { checked_at: "0001-01-01T00:00:00Z" },
  ]) {
    vi.mocked(apiFetch).mockResolvedValue({ ...current, ...change });
    await expect(getStaffPostingCutoffContext(scope, approved)).rejects.toThrow("cutoff context");
  }
});

it("bounds retained cutoff history and verifies scope, revision and continuation", async () => {
  const { listStaffPostingCutoffHistory } = await import("./staff-posting-proposals");
  const records = Array.from({ length: 8 }, (_, i) => {
    const version = 8 - i;
    const instant = `2026-12-${String(10 - version).padStart(2, "0")}T00:00:00Z`;
    return {
      ...receipt,
      id: `019a0000-0000-7000-8000-${String(10 + version).padStart(12, "0")}`,
      mode: "scheduled",
      version,
      expected_cutoff_version: version - 1,
      requested_cutoff_at: instant,
      cutoff_at: instant,
    };
  });
  const result = { data: records, page: { has_more: true, next_cursor: "synthetic-cutoff-next" } };
  vi.mocked(apiFetch).mockResolvedValue(result);
  expect(await listStaffPostingCutoffHistory(scope, approved)).toEqual(result);
  expect(apiFetch).toHaveBeenLastCalledWith(
    expect.stringContaining(
      `/${proposal}/cutoffs/history?expected_revision_id=${approved.revision_id}&limit=8`,
    ),
    expect.objectContaining({ cache: "no-store" }),
  );
  for (const changed of [
    { ...result, data: [] },
    { ...result, data: [receipt] },
    { ...result, data: [{ ...receipt, staff_id: scope.hospitalId }] },
    { ...result, data: [{ ...receipt, expected_revision_id: command }] },
    { ...result, data: [receipt, receipt] },
    { ...result, data: Array(9).fill(receipt) },
    { ...result, page: { has_more: false, next_cursor: "synthetic-cutoff-next" } },
    { ...result, page: { has_more: true, next_cursor: "synthetic-current" } },
  ]) {
    vi.mocked(apiFetch).mockResolvedValue(changed);
    await expect(
      listStaffPostingCutoffHistory(scope, approved, "synthetic-current"),
    ).rejects.toThrow();
  }
});
