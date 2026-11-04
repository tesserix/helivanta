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

it("retains an explicit hospital management intent without changing legacy submissions", () => {
  expect(StaffPostingProposalInputContract.parse(input)).toEqual(input);
  const supervision = { kind: "hospital_management" };
  expect(StaffPostingProposalInputContract.parse({ ...input, supervision })).toEqual({
    ...input,
    supervision,
  });
});

it("pins supervisor lookup to its canonical Staff path", () => {
  const supervision = {
    kind: "department_member",
    supervisor_staff_id: scope.staffId,
    supervisor_proposal_id: id(20),
    supervisor_revision_id: id(21),
  };
  expect(StaffPostingProposalInputContract.parse({ ...input, supervision })).toEqual({
    ...input,
    supervision,
  });
  const missingStaff = { ...supervision, supervisor_staff_id: undefined };
  expect(
    StaffPostingProposalInputContract.safeParse({ ...input, supervision: missingStaff }).success,
  ).toBe(false);
});

it("requires a complete Staff and supervisor reference only for non-root posting intent", () => {
  for (const kind of ["department_supervisor", "department_member"]) {
    const supervision = {
      kind,
      supervisor_staff_id: scope.staffId,
      supervisor_proposal_id: id(20),
      supervisor_revision_id: id(21),
    };
    expect(StaffPostingProposalInputContract.parse({ ...input, supervision })).toEqual({
      ...input,
      supervision,
    });
    for (const invalid of [
      { kind, supervisor_staff_id: scope.staffId },
      { kind, supervisor_staff_id: scope.staffId, supervisor_proposal_id: id(20) },
      { kind, supervisor_staff_id: scope.staffId, supervisor_revision_id: id(21) },
      { kind, supervisor_proposal_id: id(20), supervisor_revision_id: id(21) },
    ]) {
      expect(
        StaffPostingProposalInputContract.safeParse({ ...input, supervision: invalid }).success,
      ).toBe(false);
    }
  }
  for (const reference of [
    { supervisor_staff_id: scope.staffId },
    { supervisor_proposal_id: id(20) },
    { supervisor_revision_id: id(21) },
    { supervisor_proposal_id: id(20), supervisor_revision_id: id(21) },
  ]) {
    expect(
      StaffPostingProposalInputContract.safeParse({
        ...input,
        supervision: { kind: "hospital_management", ...reference },
      }).success,
    ).toBe(false);
  }
});

it("requires UUIDv7 supervisor references and rejects null or unrecognized supervision fields", () => {
  const supervision = {
    kind: "department_member",
    supervisor_staff_id: scope.staffId,
    supervisor_proposal_id: id(20),
    supervisor_revision_id: id(21),
  };
  for (const field of ["supervisor_proposal_id", "supervisor_revision_id"]) {
    for (const value of [scope.hospitalId, "00000000-0000-0000-0000-000000000000", null]) {
      expect(
        StaffPostingProposalInputContract.safeParse({
          ...input,
          supervision: { ...supervision, [field]: value },
        }).success,
      ).toBe(false);
    }
  }
  for (const invalid of [null, { ...supervision, grants_access: true }, { kind: "clinical" }]) {
    expect(
      StaffPostingProposalInputContract.safeParse({ ...input, supervision: invalid }).success,
    ).toBe(false);
  }
});

it("accepts hospital management review attestation only for approval", () => {
  const decision = {
    expected_version: 1,
    expected_revision_id: id(7),
    decision: "approve",
    reason: "Synthetic hospital management appointment review",
    sources_examined: true,
  };
  expect(StaffPostingDecisionInputContract.parse(decision)).toEqual(decision);
  expect(
    StaffPostingDecisionInputContract.parse({ ...decision, hospital_management_reviewed: true }),
  ).toEqual({ ...decision, hospital_management_reviewed: true });
  for (const action of ["reject", "withdraw"]) {
    expect(
      StaffPostingDecisionInputContract.safeParse({
        ...decision,
        decision: action,
        hospital_management_reviewed: true,
      }).success,
    ).toBe(false);
    expect(
      StaffPostingDecisionInputContract.parse({
        ...decision,
        decision: action,
        hospital_management_reviewed: false,
      }),
    ).toEqual({ ...decision, decision: action, hospital_management_reviewed: false });
  }
  expect(
    StaffPostingDecisionInputContract.safeParse({
      ...decision,
      hospital_management_reviewed: null,
    }).success,
  ).toBe(false);
});

it("discovers bounded hospital proposal rows with original account headers", async () => {
  const api = await import("./staff-posting-proposals");
  const staff = {
    id: scope.staffId,
    employee_code: "SYN-POSTING-QUEUE",
    display_name: "Synthetic queue worker",
  };
  const page = {
    data: [{ proposal: receipt, staff, is_submitter: false }],
    page: { has_more: false, next_cursor: null },
  };
  vi.mocked(apiFetch).mockResolvedValue(page);
  const controller = new AbortController();
  expect(
    await api.listStaffPostingProposalInbox(scope, "pending", null, controller.signal),
  ).toEqual(page);
  expect(apiFetch).toHaveBeenCalledWith(
    `/workforce/hospitals/${scope.hospitalId}/posting-proposal-inbox?status=pending&limit=8`,
    expect.objectContaining({
      cache: "no-store",
      signal: controller.signal,
      headers: expect.objectContaining({
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      }),
    }),
  );
});

it("rejects inbox rows outside their hospital, Staff relationship, status or page bound", async () => {
  const api = await import("./staff-posting-proposals");
  const item = {
    proposal: receipt,
    staff: { id: scope.staffId, employee_code: "SYN-QUEUE", display_name: "Synthetic worker" },
    is_submitter: false,
  };
  const page = { data: [item], page: { has_more: false, next_cursor: null } };
  for (const changed of [
    { ...page, data: [{ ...item, proposal: { ...receipt, hospital_id: id(90) } }] },
    { ...page, data: [{ ...item, staff: { ...item.staff, id: id(90) } }] },
    { ...page, data: [{ ...item, proposal: { ...receipt, status: "rejected" } }] },
    { ...page, data: [{ ...item, reason: "Private rationale must not be listed" }] },
    { ...page, data: [item, item] },
    {
      ...page,
      data: Array.from({ length: 9 }, (_, n) => ({
        ...item,
        proposal: { ...receipt, id: id(n + 100) },
      })),
    },
    { ...page, data: [], page: { has_more: true, next_cursor: "next" } },
    { ...page, page: { has_more: true, next_cursor: null } },
  ]) {
    vi.mocked(apiFetch).mockResolvedValue(changed);
    await expect(api.listStaffPostingProposalInbox(scope)).rejects.toThrow();
  }
  vi.mocked(apiFetch).mockResolvedValue({
    ...page,
    page: { has_more: true, next_cursor: "replayed" },
  });
  await expect(api.listStaffPostingProposalInbox(scope, "pending", "replayed")).rejects.toThrow();
});

it("discards inbox responses cancelled during transfer", async () => {
  const api = await import("./staff-posting-proposals");
  const controller = new AbortController();
  vi.mocked(apiFetch).mockImplementationOnce(async () => {
    controller.abort();
    return { data: [], page: { has_more: false, next_cursor: null } };
  });
  await expect(
    api.listStaffPostingProposalInbox(scope, "pending", null, controller.signal),
  ).rejects.toThrow();
});

it("releases only the requested exact protected proposal context", async () => {
  const api = await import("./staff-posting-proposals");
  const request = {
    purpose: "review" as const,
    expected_version: 1,
    expected_revision_id: receipt.revision_id,
  };
  const context = {
    purpose: "review",
    proposal: receipt,
    draft,
    draft_reason: "Synthetic retained preparation rationale",
    submission_reason: input.reason,
    decision_reason: null,
  };
  vi.mocked(apiFetch).mockResolvedValue(context);
  expect(await api.getStaffPostingProposalContext(scope, receipt.id, request)).toEqual(context);
  expect(apiFetch).toHaveBeenCalledWith(
    `/workforce/hospitals/${scope.hospitalId}/posting-staff/${scope.staffId}/posting-proposals/${receipt.id}/context?purpose=review&expected_version=1&expected_revision_id=${receipt.revision_id}`,
    expect.objectContaining({
      headers: expect.objectContaining({
        "X-Helivanta-Expected-Tenant": scope.tenantId,
        "X-Helivanta-Expected-Subject": scope.subject,
      }),
    }),
  );
});

it("requests retained supervision through an explicit protected context view", async () => {
  const api = await import("./staff-posting-proposals");
  const request = {
    purpose: "review" as const,
    expected_version: 1,
    expected_revision_id: receipt.revision_id,
  };
  const view = {
    context: {
      purpose: "review",
      proposal: receipt,
      draft,
      draft_reason: "Synthetic retained preparation rationale",
      submission_reason: input.reason,
      decision_reason: null,
    },
    supervision: { kind: "hospital_management" },
  };
  vi.mocked(apiFetch).mockResolvedValue(view);
  const controller = new AbortController();
  expect(
    await api.getStaffPostingSupervisionContext(scope, receipt.id, request, controller.signal),
  ).toEqual(view);
  expect(apiFetch).toHaveBeenCalledWith(
    `/workforce/hospitals/${scope.hospitalId}/posting-staff/${scope.staffId}/posting-proposals/${receipt.id}/context?purpose=review&expected_version=1&expected_revision_id=${receipt.revision_id}&supervision_context=v1`,
    expect.objectContaining({ cache: "no-store", signal: controller.signal }),
  );
});

it("requests an explicit current management review without widening the retained envelope", async () => {
  const api = await import("./staff-posting-proposals");
  const request = {
    purpose: "review" as const,
    expected_version: 1,
    expected_revision_id: receipt.revision_id,
  };
  const view = {
    context: {
      purpose: "review",
      proposal: receipt,
      draft,
      draft_reason: "Synthetic management source review",
      submission_reason: input.reason,
      decision_reason: null,
    },
    supervision: { kind: "hospital_management" },
  };
  vi.mocked(apiFetch).mockResolvedValue(view);
  const controller = new AbortController();
  expect(
    await api.getStaffPostingManagementReviewContext(scope, receipt.id, request, controller.signal),
  ).toEqual(view);
  expect(apiFetch).toHaveBeenCalledWith(
    `/workforce/hospitals/${scope.hospitalId}/posting-staff/${scope.staffId}/posting-proposals/${receipt.id}/context?purpose=review&expected_version=1&expected_revision_id=${receipt.revision_id}&supervision_context=v1&management_review=current`,
    expect.objectContaining({ cache: "no-store", signal: controller.signal }),
  );
});

it("cannot silently discard management readiness on an ordinary context getter", async () => {
  const api = await import("./staff-posting-proposals");
  const request = {
    purpose: "review" as const,
    expected_version: 1,
    expected_revision_id: receipt.revision_id,
    management_review: "current",
  };
  for (const getter of [
    api.getStaffPostingProposalContext,
    api.getStaffPostingSupervisionContext,
  ]) {
    vi.mocked(apiFetch).mockClear();
    await expect(getter(scope, receipt.id, request as never)).rejects.toThrow();
    expect(apiFetch).not.toHaveBeenCalled();
  }
});

it("refuses canceled, submission-purpose and unspecified management readiness", async () => {
  const api = await import("./staff-posting-proposals");
  const request = {
    purpose: "review" as const,
    expected_version: 1,
    expected_revision_id: receipt.revision_id,
  };
  await expect(
    api.getStaffPostingManagementReviewContext(scope, receipt.id, {
      ...request,
      purpose: "submission",
    } as never),
  ).rejects.toThrow();
  const controller = new AbortController();
  controller.abort();
  await expect(
    api.getStaffPostingManagementReviewContext(scope, receipt.id, request, controller.signal),
  ).rejects.toThrow();
  expect(apiFetch).not.toHaveBeenCalled();
  const context = {
    purpose: "review",
    proposal: receipt,
    draft,
    draft_reason: "Synthetic protected rationale",
    submission_reason: input.reason,
    decision_reason: null,
  };
  for (const supervision of [
    null,
    {
      kind: "department_member",
      supervisor_staff_id: scope.staffId,
      supervisor_proposal_id: receipt.id,
      supervisor_revision_id: receipt.revision_id,
    },
  ]) {
    vi.mocked(apiFetch).mockResolvedValue({ context, supervision });
    await expect(
      api.getStaffPostingManagementReviewContext(scope, receipt.id, request),
    ).rejects.toThrow("pending explicit hospital-management request");
  }
  const inflight = new AbortController();
  vi.mocked(apiFetch).mockImplementationOnce(async () => {
    inflight.abort();
    return { context, supervision: { kind: "hospital_management" } };
  });
  await expect(
    api.getStaffPostingManagementReviewContext(scope, receipt.id, request, inflight.signal),
  ).rejects.toThrow();
});

it("withholds protected supervision context when its caller cancels", async () => {
  const api = await import("./staff-posting-proposals");
  const request = {
    purpose: "review" as const,
    expected_version: 1,
    expected_revision_id: receipt.revision_id,
  };
  const cancelled = new AbortController();
  cancelled.abort();
  await expect(
    api.getStaffPostingSupervisionContext(scope, receipt.id, request, cancelled.signal),
  ).rejects.toThrow();
  expect(apiFetch).not.toHaveBeenCalled();
  const inflight = new AbortController();
  vi.mocked(apiFetch).mockImplementationOnce(async () => {
    inflight.abort();
    return {
      context: {
        purpose: "review",
        proposal: receipt,
        draft,
        draft_reason: "Synthetic preparation reason",
        submission_reason: input.reason,
        decision_reason: null,
      },
      supervision: { kind: "hospital_management" },
    };
  });
  await expect(
    api.getStaffPostingSupervisionContext(scope, receipt.id, request, inflight.signal),
  ).rejects.toThrow();
});

it("refuses supervision context with mismatched sources and keeps the legacy selector strict", async () => {
  const api = await import("./staff-posting-proposals");
  const request = {
    purpose: "review" as const,
    expected_version: 1,
    expected_revision_id: receipt.revision_id,
  };
  const context = {
    purpose: "review",
    proposal: receipt,
    draft,
    draft_reason: "Synthetic preparation reason",
    submission_reason: input.reason,
    decision_reason: null,
  };
  for (const supervision of [
    null,
    { kind: "hospital_management" },
    {
      kind: "department_member",
      supervisor_staff_id: scope.staffId,
      supervisor_proposal_id: receipt.id,
      supervisor_revision_id: receipt.revision_id,
    },
  ]) {
    vi.mocked(apiFetch).mockResolvedValue({ context, supervision });
    expect(
      (await api.getStaffPostingSupervisionContext(scope, receipt.id, request)).supervision,
    ).toEqual(supervision);
  }
  for (const view of [
    { context: { ...context, purpose: "submission" }, supervision: null },
    {
      context: {
        ...context,
        draft: { ...draft, staff_id: "00000000-0000-4000-8000-000000000001" },
      },
      supervision: null,
    },
    { context, supervision: { kind: "hospital_management", supervisor_staff_id: scope.staffId } },
    { context, supervision: { kind: "department_member" } },
    { context },
    { context, supervision: null, extra: true },
  ]) {
    vi.mocked(apiFetch).mockResolvedValue(view);
    await expect(
      api.getStaffPostingSupervisionContext(scope, receipt.id, request),
    ).rejects.toThrow();
  }
  vi.mocked(apiFetch).mockClear();
  await expect(
    api.getStaffPostingProposalContext(scope, receipt.id, {
      ...request,
      supervision_context: "v1",
    } as never),
  ).rejects.toThrow();
  expect(apiFetch).not.toHaveBeenCalled();
});

it("refuses private context with mismatched scope, purpose, immutable sources or reasons", async () => {
  const api = await import("./staff-posting-proposals");
  const request = {
    purpose: "review" as const,
    expected_version: 1,
    expected_revision_id: receipt.revision_id,
  };
  const context = {
    purpose: "review",
    proposal: receipt,
    draft,
    draft_reason: "Synthetic retained preparation",
    submission_reason: input.reason,
    decision_reason: null,
  };
  for (const change of [
    { purpose: "submission" },
    { proposal: { ...receipt, staff_id: id(90) } },
    { proposal: { ...receipt, revision_id: id(90) } },
    { proposal: { ...receipt, version: 2 } },
    { draft: { ...draft, revision_id: id(90) } },
    { draft: { ...draft, department_revision_id: id(90) } },
    { draft: { ...draft, starts_at: "2026-11-04T00:00:01Z" } },
    { draft_reason: "Synthetic\ninvalid reason" },
    { submission_reason: " Synthetic padded reason " },
    { decision_reason: "A pending proposal cannot contain a terminal reason" },
    { proposal: { ...receipt, input_hash: "private fields must not reach this view" } },
    { unknown: true },
  ]) {
    vi.mocked(apiFetch).mockResolvedValue({ ...context, ...change });
    await expect(api.getStaffPostingProposalContext(scope, receipt.id, request)).rejects.toThrow();
  }
});

it("recovers terminal rationale only for its exact current head", async () => {
  const api = await import("./staff-posting-proposals");
  const rejected = {
    ...receipt,
    revision_id: id(90),
    command_id: id(91),
    version: 2,
    status: "rejected",
  };
  const context = {
    purpose: "submission",
    proposal: rejected,
    draft,
    draft_reason: "Synthetic retained preparation",
    submission_reason: input.reason,
    decision_reason: "Synthetic independent correction",
  };
  vi.mocked(apiFetch).mockResolvedValue(context);
  expect(
    await api.getStaffPostingProposalContext(scope, receipt.id, {
      purpose: "submission",
      expected_version: 2,
      expected_revision_id: rejected.revision_id,
    }),
  ).toEqual(context);
});

it("measures retained rationale bounds in Unicode code points", async () => {
  const api = await import("./staff-posting-proposals");
  const reason = "😀".repeat(500);
  const rejected = {
    ...receipt,
    revision_id: id(90),
    command_id: id(91),
    version: 2,
    status: "rejected",
  };
  const request = {
    purpose: "submission" as const,
    expected_version: 2,
    expected_revision_id: rejected.revision_id,
  };
  const context = {
    purpose: "submission",
    proposal: rejected,
    draft,
    draft_reason: reason,
    submission_reason: reason,
    decision_reason: reason,
  };
  vi.mocked(apiFetch).mockResolvedValue(context);
  expect(await api.getStaffPostingProposalContext(scope, receipt.id, request)).toEqual(context);
  for (const field of ["draft_reason", "submission_reason", "decision_reason"] as const) {
    vi.mocked(apiFetch).mockResolvedValue({ ...context, [field]: reason + "😀" });
    await expect(api.getStaffPostingProposalContext(scope, receipt.id, request)).rejects.toThrow();
  }
});

it("rejects malformed context intent before requesting protected bytes", async () => {
  const api = await import("./staff-posting-proposals");
  const request = {
    purpose: "review",
    expected_version: 1,
    expected_revision_id: receipt.revision_id,
  };
  for (const change of [
    { purpose: "admin" },
    { expected_version: null },
    { expected_version: 0 },
    { expected_revision_id: scope.staffId },
    { unknown: true },
  ])
    await expect(
      api.getStaffPostingProposalContext(scope, receipt.id, { ...request, ...change } as never),
    ).rejects.toThrow();
  expect(apiFetch).not.toHaveBeenCalled();
});

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

it("retains an explicitly attested management approval and rejects a different terminal result", async () => {
  const api = await import("./staff-posting-proposals");
  const selected = StaffPostingProposalContract.parse(receipt);
  const decision = {
    expected_version: 1,
    expected_revision_id: id(7),
    decision: "approve" as const,
    reason: "Synthetic independent hospital management review",
    sources_examined: true,
    hospital_management_reviewed: true,
  };
  const approved = {
    ...receipt,
    revision_id: id(10),
    command_id: id(9),
    version: 2,
    status: "approved",
  };
  vi.mocked(apiFetch).mockResolvedValue(approved);
  expect(await api.decideStaffPostingProposal(scope, id(6), id(9), decision, selected)).toEqual(
    approved,
  );
  expect(await api.getStaffPostingDecisionReceipt(scope, id(6), id(9), decision, selected)).toEqual(
    approved,
  );
  for (const status of ["withdrawn", "rejected"]) {
    vi.mocked(apiFetch).mockResolvedValue({ ...approved, status });
    await expect(
      api.getStaffPostingDecisionReceipt(scope, id(6), id(9), decision, selected),
    ).rejects.toThrow();
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
