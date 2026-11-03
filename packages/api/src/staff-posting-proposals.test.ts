import { apiFetch } from "./client";
import {
  StaffPostingProposalInputContract,
  StaffPostingDecisionInputContract,
  StaffPostingProposalContract,
} from "./staff-postings-contract";

vi.mock("./client", () => ({ apiFetch: vi.fn() }));

const scope = {
  access: "scoped" as const,
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-submitter",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
};
const id = (n: number) => `019a0000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const input = {
  expected_version: 0,
  draft_id: id(1),
  draft_revision_id: id(2),
  draft_version: 3,
  generation_id: id(3),
  staff_version: 2,
  employment_epoch: 2,
  reason: "Synthetic current-source proposal",
  sources_examined: true,
};
const draft = {
  id: id(1),
  revision_id: id(2),
  version: 3,
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  staff_version: 2,
  employment_epoch: 2,
  department_id: id(4),
  department_revision_id: id(5),
  starts_at: "2026-11-04T00:00:00Z",
  ends_at: "2026-11-05T00:00:00Z",
  status: "draft" as const,
  grants_access: false,
  created_at: "2026-11-03T00:00:00Z",
  recorded_at: "2026-11-03T01:00:00Z",
};
const receipt = {
  id: id(6),
  revision_id: id(7),
  command_id: id(8),
  version: 1,
  status: "pending",
  grants_access: false,
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  draft_id: input.draft_id,
  draft_revision_id: input.draft_revision_id,
  draft_version: input.draft_version,
  generation_id: input.generation_id,
  staff_version: 2,
  employment_epoch: 2,
  department_id: draft.department_id,
  department_revision_id: draft.department_revision_id,
  starts_at: draft.starts_at,
  ends_at: draft.ends_at,
  eligibility_until: "2026-11-06T00:00:00Z",
  created_at: "2026-11-03T02:00:00Z",
  recorded_at: "2026-11-03T02:00:00Z",
};
afterEach(() => vi.resetAllMocks());

it("requires exact pending revision, source confirmation and a control-free decision reason", () => {
  const decision = {
    expected_version: 1,
    expected_revision_id: id(7),
    decision: "reject",
    reason: "Synthetic independent correction",
    sources_examined: true,
  };
  expect(StaffPostingDecisionInputContract.safeParse(decision).success).toBe(true);
  for (const change of [
    { sources_examined: false },
    { expected_revision_id: scope.hospitalId },
    { reason: "Synthetic\ninvalid control" },
  ])
    expect(StaffPostingDecisionInputContract.safeParse({ ...decision, ...change }).success).toBe(
      false,
    );
});

it("retains the exact non-operating decision command and verifies its immutable source pins", async () => {
  const api = await import("./staff-posting-proposals");
  const selected = StaffPostingProposalContract.parse(receipt);
  const decision = {
    expected_version: 1,
    expected_revision_id: id(7),
    decision: "reject" as const,
    reason: "Synthetic independent correction",
    sources_examined: true,
  };
  const rejected = {
    ...receipt,
    revision_id: id(10),
    command_id: id(9),
    version: 2,
    status: "rejected",
  };
  vi.mocked(apiFetch).mockResolvedValue(rejected);
  expect(await api.decideStaffPostingProposal(scope, id(6), id(9), decision, selected)).toEqual(
    rejected,
  );
  expect(apiFetch).toHaveBeenCalledWith(
    `/workforce/hospitals/${scope.hospitalId}/posting-staff/${scope.staffId}/posting-proposals/${id(6)}/decisions/${id(9)}`,
    expect.objectContaining({
      method: "PUT",
      body: JSON.stringify(decision),
      cache: "no-store",
      headers: {
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      },
    }),
  );
  expect(await api.getStaffPostingDecisionReceipt(scope, id(6), id(9), decision, selected)).toEqual(
    rejected,
  );
  for (const change of [
    { generation_id: id(30) },
    { status: "approved" },
    { grants_access: true },
    { version: 3 },
    { command_id: id(31) },
    { starts_at: "2026-11-04T00:00:00.000001Z" },
  ]) {
    vi.mocked(apiFetch).mockResolvedValue({ ...rejected, ...change });
    await expect(
      api.getStaffPostingDecisionReceipt(scope, id(6), id(9), decision, selected),
    ).rejects.toThrow("could not be verified");
  }
});

it("refuses changed pending source, false confirmation and unsupported approval before sending", async () => {
  const api = await import("./staff-posting-proposals");
  const selected = StaffPostingProposalContract.parse(receipt);
  const decision = {
    expected_version: 1,
    expected_revision_id: id(7),
    decision: "reject" as const,
    reason: "Synthetic independent correction",
    sources_examined: true,
  };
  for (const change of [
    { expected_version: 2 },
    { expected_revision_id: id(30) },
    { sources_examined: false },
    { decision: "approve" as const },
  ])
    await expect(
      api.decideStaffPostingProposal(scope, id(6), id(9), { ...decision, ...change }, selected),
    ).rejects.toThrow();
  expect(apiFetch).not.toHaveBeenCalled();
});

it("requires exact reviewed sources, confirmation and a valid reason", () => {
  expect(StaffPostingProposalInputContract.safeParse(input).success).toBe(true);
  for (const change of [
    { expected_version: 1 },
    { draft_id: scope.hospitalId },
    { generation_id: scope.hospitalId },
    { draft_version: 0 },
    { sources_examined: false },
    { reason: "Synthetic\ncontrol" },
    { extra: "not allowed" },
  ])
    expect(StaffPostingProposalInputContract.safeParse({ ...input, ...change }).success).toBe(
      false,
    );
});

it("submits through the scoped Global API path with retained identity and verifies the selected draft", async () => {
  const { submitStaffPostingProposal } = await import("./staff-posting-proposals");
  vi.mocked(apiFetch).mockResolvedValue(receipt);
  const controller = new AbortController();
  expect(
    await submitStaffPostingProposal(scope, id(6), id(8), input, draft, controller.signal),
  ).toEqual(receipt);
  expect(apiFetch).toHaveBeenCalledWith(
    `/workforce/hospitals/${scope.hospitalId}/posting-staff/${scope.staffId}/posting-proposals/${id(6)}/commands/${id(8)}`,
    expect.objectContaining({
      method: "PUT",
      body: JSON.stringify(input),
      signal: controller.signal,
      cache: "no-store",
      headers: {
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      },
    }),
  );
  for (const change of [
    { command_id: id(9) },
    { staff_id: scope.hospitalId },
    { id: id(9) },
    { draft_revision_id: id(9) },
    { draft_version: 4 },
    { generation_id: id(9) },
    { employment_epoch: 3 },
    { status: "approved" },
    { version: 2 },
    { grants_access: true },
    { department_revision_id: id(9) },
    { starts_at: "2026-11-04T00:00:00.000001Z" },
    { ends_at: "2026-11-05T00:00:00.000001Z" },
    { eligibility_until: draft.starts_at },
    { reason: "private data" },
  ]) {
    vi.mocked(apiFetch).mockResolvedValue({ ...receipt, ...change });
    await expect(submitStaffPostingProposal(scope, id(6), id(8), input, draft)).rejects.toThrow(
      "receipt",
    );
  }
});

it("recovers the same immutable command without creating another submission", async () => {
  const { getStaffPostingProposalReceipt } = await import("./staff-posting-proposals");
  vi.mocked(apiFetch).mockResolvedValue(receipt);
  expect(await getStaffPostingProposalReceipt(scope, id(6), id(8), input, draft)).toEqual(receipt);
  expect(apiFetch).toHaveBeenCalledExactlyOnceWith(
    expect.stringContaining(`/commands/${id(8)}`),
    expect.not.objectContaining({ method: "PUT" }),
  );
});

it("refuses legacy routing, changed selected drafts and malformed intent before transmission", async () => {
  const { submitStaffPostingProposal } = await import("./staff-posting-proposals");
  await expect(
    submitStaffPostingProposal({ ...scope, access: "legacy" } as never, id(6), id(8), input, draft),
  ).rejects.toThrow("scoped");
  await expect(
    submitStaffPostingProposal(scope, id(6), id(8), input, { ...draft, revision_id: id(9) }),
  ).rejects.toThrow("draft");
  await expect(
    submitStaffPostingProposal(scope, id(6), id(8), { ...input, sources_examined: false }, draft),
  ).rejects.toThrow();
  expect(apiFetch).not.toHaveBeenCalled();
});

it("keeps proposal history bounded and separate from draft caches", async () => {
  const { listStaffPostingProposalHistory, staffPostingProposalsKey } =
    await import("./staff-posting-proposals");
  const page = { data: [receipt], page: { has_more: false, next_cursor: null } };
  vi.mocked(apiFetch).mockResolvedValue(page);
  expect(await listStaffPostingProposalHistory(scope, id(6))).toEqual(page);
  expect(apiFetch).toHaveBeenLastCalledWith(
    expect.stringContaining(`/posting-proposals/${id(6)}/history?limit=50`),
    expect.objectContaining({ cache: "no-store" }),
  );
  for (const invalid of [
    { ...page, data: [{ ...receipt, hospital_id: scope.staffId }] },
    { ...page, data: [receipt, receipt] },
    { ...page, page: { has_more: true, next_cursor: null } },
    { ...page, page: { has_more: true, next_cursor: "same-cursor" } },
  ]) {
    vi.mocked(apiFetch).mockResolvedValue(invalid);
    await expect(listStaffPostingProposalHistory(scope, id(6), "same-cursor")).rejects.toThrow();
  }
  expect(staffPostingProposalsKey(scope)).not.toEqual(
    staffPostingProposalsKey({ ...scope, subject: "another-account" }),
  );
  expect(staffPostingProposalsKey(scope)).toContain("posting-proposals");
});

it("verifies the current employment readiness projection without accepting another Staff source", async () => {
  const { getStaffPostingReviewSource } = await import("./staff-posting-proposals");
  const source = {
    hospital_id: scope.hospitalId,
    staff_id: scope.staffId,
    staff_version: 2,
    employment_epoch: 2,
    generation_id: id(3),
    generation_state: "current",
    starts_at: "2026-11-03T00:00:00Z",
    ends_at: null,
    eligibility_until: "2026-11-06T00:00:00Z",
    checked_at: "2026-11-03T02:00:00Z",
  };
  vi.mocked(apiFetch).mockResolvedValue(source);
  expect(await getStaffPostingReviewSource(scope)).toEqual(source);
  vi.mocked(apiFetch).mockResolvedValue({ ...source, staff_id: scope.hospitalId });
  await expect(getStaffPostingReviewSource(scope)).rejects.toThrow("source");
  for (const change of [
    { generation_id: null },
    { starts_at: "0001-01-01T00:00:00Z" },
    { eligibility_until: source.checked_at },
    { generation_state: "not_started" },
    { generation_state: "no_current_generation" },
    { checked_at: "0001-01-01T00:00:00Z" },
  ]) {
    vi.mocked(apiFetch).mockResolvedValue({ ...source, ...change });
    await expect(getStaffPostingReviewSource(scope)).rejects.toThrow("source");
  }
  const noGeneration = {
    ...source,
    generation_id: null,
    generation_state: "no_current_generation",
    starts_at: null,
    ends_at: null,
    eligibility_until: null,
  };
  vi.mocked(apiFetch).mockResolvedValue(noGeneration);
  expect(await getStaffPostingReviewSource(scope)).toEqual(noGeneration);
});

it("permits an open-ended placement while keeping its reviewed source cutoff distinct", async () => {
  const { submitStaffPostingProposal } = await import("./staff-posting-proposals");
  const ongoing = { ...receipt, ends_at: null };
  vi.mocked(apiFetch).mockResolvedValue(ongoing);
  expect(
    await submitStaffPostingProposal(scope, id(6), id(8), input, { ...draft, ends_at: null }),
  ).toEqual(ongoing);
  const longPlacement = { ...receipt, ends_at: "2027-11-05T00:00:00Z" };
  vi.mocked(apiFetch).mockResolvedValue(longPlacement);
  expect(
    await submitStaffPostingProposal(scope, id(6), id(8), input, {
      ...draft,
      ends_at: longPlacement.ends_at,
    }),
  ).toEqual(longPlacement);
});
