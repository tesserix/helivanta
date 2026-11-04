import { z } from "zod";
import { apiFetch } from "./client";
import { enrollmentRequestOptions } from "./enrollment";
import { tenantTransitionPending } from "./tenant-transition";
import type { StaffPostingScope } from "./staff-postings";
import {
  StaffPostingDraftContract,
  StaffPostingReviewSourceContract,
  StaffPostingProposalContract,
  StaffPostingProposalInputContract,
  StaffPostingDecisionInputContract,
  StaffPostingProposalContextRequestContract,
  StaffPostingProposalContextContract,
  StaffPostingSupervisionContextContract,
  StaffPostingChainReviewContextContract,
  StaffPostingInboxItemContract,
  StaffPostingSupervisorChoiceContract,
  StaffPostingCutoffContract,
  StaffPostingCutoffInputContract,
  staffPostingInstantMicros,
  type StaffPostingDraft,
  type StaffPostingProposal,
  type StaffPostingProposalInput,
  type StaffPostingDecisionInput,
  type StaffPostingProposalContextRequest,
  type StaffPostingCutoffInput,
} from "./staff-postings-contract";

export type StaffPostingProposalScope = Omit<StaffPostingScope, "access"> & { access: "scoped" };
export type StaffPostingInboxScope = Omit<StaffPostingProposalScope, "staffId">;
const identifier = z
  .string()
  .guid()
  .refine((value) => value !== "00000000-0000-0000-0000-000000000000")
  .transform((value) => value.toLowerCase());
const commandId = z.uuidv7().transform((value) => value.toLowerCase());
const cursorSchema = z.string().min(1).max(2048);
const pageSchema = z
  .object({
    data: z.array(StaffPostingProposalContract).max(50),
    page: z.object({ has_more: z.boolean(), next_cursor: cursorSchema.nullable() }).strict(),
  })
  .strict();
const same = (a: string, b: string) => identifier.parse(a) === identifier.parse(b);
export const staffPostingProposalsKey = (scope: StaffPostingProposalScope) => [
  "workforce",
  "posting-proposals",
  scope.access,
  scope.tenantId,
  scope.subject,
  scope.hospitalId,
  scope.staffId,
];

async function postingCutoffCommand(
  scope: StaffPostingProposalScope,
  proposal: string,
  command: string,
  input: StaffPostingCutoffInput,
  recover: boolean,
  signal?: AbortSignal,
) {
  const path = `${root(scope)}/${commandId.parse(proposal)}/cutoffs/commands/${commandId.parse(command)}`;
  const intent = StaffPostingCutoffInputContract.parse(input);
  const raw = await apiFetch(path, {
    ...enrollmentRequestOptions(scope),
    ...(recover ? {} : { method: "PUT", body: JSON.stringify(intent) }),
    signal,
  });
  const parsed = StaffPostingCutoffContract.safeParse(raw);
  if (!parsed.success)
    throw new Error(
      "The retained cutoff receipt could not be verified. Recover the exact command before retrying.",
    );
  const row = parsed.data;
  if (
    !matchesScope(row, scope) ||
    !same(row.id, command) ||
    !same(row.proposal_id, proposal) ||
    !same(row.expected_revision_id, intent.expected_revision_id) ||
    row.expected_cutoff_version !== intent.expected_cutoff_version ||
    row.mode !== intent.mode ||
    row.reason !== intent.reason ||
    (row.requested_cutoff_at === null
      ? null
      : staffPostingInstantMicros(row.requested_cutoff_at)) !==
      (intent.cutoff_at == null ? null : staffPostingInstantMicros(intent.cutoff_at))
  )
    throw new Error(
      "The retained cutoff receipt does not match this command. Recover the exact command before retrying.",
    );
  return row;
}

export function recordStaffPostingCutoff(
  scope: StaffPostingProposalScope,
  proposal: string,
  command: string,
  input: StaffPostingCutoffInput,
  signal?: AbortSignal,
) {
  return postingCutoffCommand(scope, proposal, command, input, false, signal);
}

export function getStaffPostingCutoffReceipt(
  scope: StaffPostingProposalScope,
  proposal: string,
  command: string,
  input: StaffPostingCutoffInput,
  signal?: AbortSignal,
) {
  return postingCutoffCommand(scope, proposal, command, input, true, signal);
}

function hospitalRoot(scope: StaffPostingInboxScope) {
  if (scope.access !== "scoped")
    throw new Error("Posting review requires an explicitly scoped workspace.");
  identifier.parse(scope.tenantId);
  z.string()
    .min(1)
    .max(200)
    .refine((value) => value === value.trim() && !/\p{Cc}/u.test(value))
    .parse(scope.subject);
  if (tenantTransitionPending()) throw new Error("Wait for your workspace to finish switching.");
  return `/workforce/hospitals/${identifier.parse(scope.hospitalId)}`;
}
function root(scope: StaffPostingProposalScope) {
  return `${hospitalRoot(scope)}/posting-staff/${identifier.parse(scope.staffId)}/posting-proposals`;
}
function matchesScope(
  row: { hospital_id: string; staff_id: string },
  scope: StaffPostingProposalScope,
) {
  return same(row.hospital_id, scope.hospitalId) && same(row.staff_id, scope.staffId);
}
function selectedDraft(
  scope: StaffPostingProposalScope,
  input: StaffPostingProposalInput,
  raw: StaffPostingDraft,
) {
  const draft = StaffPostingDraftContract.parse(raw);
  if (
    !matchesScope(draft, scope) ||
    !same(draft.id, input.draft_id) ||
    !same(draft.revision_id, input.draft_revision_id) ||
    draft.version !== input.draft_version ||
    draft.staff_version !== input.staff_version ||
    draft.employment_epoch !== input.employment_epoch
  )
    throw new Error("The selected draft does not match this submission. Reopen the current draft.");
  return draft;
}
function verifiedProposal(
  raw: unknown,
  scope: StaffPostingProposalScope,
  proposal?: string,
): StaffPostingProposal {
  const parsed = StaffPostingProposalContract.safeParse(raw);
  if (
    !parsed.success ||
    !matchesScope(parsed.data, scope) ||
    (proposal !== undefined && !same(parsed.data.id, proposal))
  )
    throw new Error(
      "The retained proposal receipt could not be verified. Recover the exact command before retrying.",
    );
  return parsed.data;
}
function verifiedSubmission(
  raw: unknown,
  scope: StaffPostingProposalScope,
  proposal: string,
  command: string,
  input: StaffPostingProposalInput,
  draft: StaffPostingDraft,
) {
  const row = verifiedProposal(raw, scope, proposal);
  if (
    !same(row.command_id, command) ||
    row.version !== 1 ||
    row.status !== "pending" ||
    !same(row.draft_id, input.draft_id) ||
    !same(row.draft_revision_id, input.draft_revision_id) ||
    row.draft_version !== input.draft_version ||
    !same(row.generation_id, input.generation_id) ||
    row.staff_version !== input.staff_version ||
    row.employment_epoch !== input.employment_epoch ||
    !same(row.department_id, draft.department_id) ||
    !same(row.department_revision_id, draft.department_revision_id) ||
    staffPostingInstantMicros(row.starts_at) !== staffPostingInstantMicros(draft.starts_at) ||
    (row.ends_at ? staffPostingInstantMicros(row.ends_at) : null) !==
      (draft.ends_at ? staffPostingInstantMicros(draft.ends_at) : null)
  )
    throw new Error(
      "The retained proposal receipt could not be verified. Recover the exact command before retrying.",
    );
  return row;
}

export async function submitStaffPostingProposal(
  scope: StaffPostingProposalScope,
  proposal: string,
  command: string,
  input: StaffPostingProposalInput,
  draft: StaffPostingDraft,
  signal?: AbortSignal,
) {
  const path = `${root(scope)}/${commandId.parse(proposal)}/commands/${commandId.parse(command)}`;
  const intent = StaffPostingProposalInputContract.parse(input),
    selected = selectedDraft(scope, intent, draft);
  const raw = await apiFetch(path, {
    ...enrollmentRequestOptions(scope),
    method: "PUT",
    body: JSON.stringify(intent),
    signal,
  });
  return verifiedSubmission(raw, scope, proposal, command, intent, selected);
}
export async function getStaffPostingProposalReceipt(
  scope: StaffPostingProposalScope,
  proposal: string,
  command: string,
  input: StaffPostingProposalInput,
  draft: StaffPostingDraft,
  signal?: AbortSignal,
) {
  const path = `${root(scope)}/${commandId.parse(proposal)}/commands/${commandId.parse(command)}`;
  const intent = StaffPostingProposalInputContract.parse(input),
    selected = selectedDraft(scope, intent, draft);
  return verifiedSubmission(
    await apiFetch(path, { ...enrollmentRequestOptions(scope), signal }),
    scope,
    proposal,
    command,
    intent,
    selected,
  );
}
export async function getStaffPostingProposal(
  scope: StaffPostingProposalScope,
  proposal: string,
  signal?: AbortSignal,
) {
  return verifiedProposal(
    await apiFetch(`${root(scope)}/${commandId.parse(proposal)}`, {
      ...enrollmentRequestOptions(scope),
      signal,
    }),
    scope,
    proposal,
  );
}

function verifiedDecision(
  raw: unknown,
  scope: StaffPostingProposalScope,
  proposal: string,
  command: string,
  input: StaffPostingDecisionInput,
  selected: StaffPostingProposal,
) {
  const row = verifiedProposal(raw, scope, proposal);
  const identifiers = [
    "draft_id",
    "draft_revision_id",
    "generation_id",
    "department_id",
    "department_revision_id",
  ] as const;
  const instants = ["starts_at", "eligibility_until", "created_at"] as const;
  if (
    !same(row.command_id, command) ||
    same(row.revision_id, selected.revision_id) ||
    row.version !== selected.version + 1 ||
    row.status !==
      ({ approve: "approved", reject: "rejected", withdraw: "withdrawn" } as const)[
        input.decision
      ] ||
    row.draft_version !== selected.draft_version ||
    row.staff_version !== selected.staff_version ||
    row.employment_epoch !== selected.employment_epoch ||
    identifiers.some((key) => !same(row[key], selected[key])) ||
    instants.some(
      (key) => staffPostingInstantMicros(row[key]) !== staffPostingInstantMicros(selected[key]),
    ) ||
    (row.ends_at ? staffPostingInstantMicros(row.ends_at) : null) !==
      (selected.ends_at ? staffPostingInstantMicros(selected.ends_at) : null) ||
    staffPostingInstantMicros(row.recorded_at)! < staffPostingInstantMicros(selected.recorded_at)!
  )
    throw new Error(
      "The retained posting decision receipt could not be verified. Recover the exact command before retrying.",
    );
  return row;
}

async function postingDecisionRequest(
  scope: StaffPostingProposalScope,
  proposal: string,
  command: string,
  input: StaffPostingDecisionInput,
  selected: StaffPostingProposal,
  method: "PUT" | "GET",
  signal?: AbortSignal,
  supervised = false,
) {
  const path = `${root(scope)}/${commandId.parse(proposal)}/decisions/${commandId.parse(command)}`;
  const intent = StaffPostingDecisionInputContract.parse(input);
  const pending = verifiedProposal(selected, scope, proposal);
  if (
    pending.status !== "pending" ||
    pending.version !== intent.expected_version ||
    !same(pending.revision_id, intent.expected_revision_id)
  )
    throw new Error("Choose the exact pending posting proposal revision before deciding.");
  if (
    supervised &&
    (intent.decision !== "approve" || intent.hospital_management_reviewed !== undefined)
  )
    throw new Error("A supervised approval cannot claim hospital management review.");
  if (!supervised && intent.decision === "approve" && intent.hospital_management_reviewed !== true)
    throw new Error("Posting approval requires the supervision policy.");
  const raw = await apiFetch(path, {
    ...enrollmentRequestOptions(scope),
    method,
    ...(method === "PUT" ? { body: JSON.stringify(intent) } : {}),
    signal,
  });
  return verifiedDecision(raw, scope, proposal, command, intent, pending);
}

export async function decideStaffPostingProposal(
  scope: StaffPostingProposalScope,
  proposal: string,
  command: string,
  input: StaffPostingDecisionInput,
  selected: StaffPostingProposal,
  signal?: AbortSignal,
) {
  return postingDecisionRequest(scope, proposal, command, input, selected, "PUT", signal);
}

export async function getStaffPostingDecisionReceipt(
  scope: StaffPostingProposalScope,
  proposal: string,
  command: string,
  input: StaffPostingDecisionInput,
  selected: StaffPostingProposal,
  signal?: AbortSignal,
) {
  return postingDecisionRequest(scope, proposal, command, input, selected, "GET", signal);
}
export async function listStaffPostingProposals(
  scope: StaffPostingProposalScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  return proposalPage(scope, undefined, cursor, signal);
}
export async function listStaffPostingProposalHistory(
  scope: StaffPostingProposalScope,
  proposal: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  return proposalPage(scope, commandId.parse(proposal), cursor, signal);
}
async function proposalPage(
  scope: StaffPostingProposalScope,
  proposal: string | undefined,
  cursor: string | null,
  signal?: AbortSignal,
) {
  const params = new URLSearchParams({ limit: "50" });
  if (cursor !== null) params.set("cursor", cursorSchema.parse(cursor));
  const page = pageSchema.parse(
    await apiFetch(`${root(scope)}${proposal ? `/${proposal}/history` : ""}?${params}`, {
      ...enrollmentRequestOptions(scope),
      signal,
    }),
  );
  page.data.forEach((row) => verifiedProposal(row, scope, proposal));
  if (
    new Set(page.data.map((row) => (proposal ? row.revision_id : row.id).toLowerCase())).size !==
      page.data.length ||
    page.page.has_more !== Boolean(page.page.next_cursor) ||
    (cursor !== null && page.page.next_cursor === cursor)
  )
    throw new Error("The Staff posting proposal page could not be verified.");
  return page;
}

export async function getStaffPostingReviewSource(
  scope: StaffPostingProposalScope,
  signal?: AbortSignal,
) {
  const parsed = StaffPostingReviewSourceContract.safeParse(
    await apiFetch(`${root(scope).replace(/\/posting-proposals$/, "")}/posting-review-source`, {
      ...enrollmentRequestOptions(scope),
      signal,
    }),
  );
  if (!parsed.success || !matchesScope(parsed.data, scope))
    throw new Error("The Staff posting review source could not be verified.");
  const source = parsed.data;
  return source;
}

type PostingContextSelection = Omit<
  StaffPostingProposalContextRequest,
  "supervision_context" | "management_review" | "chain_review"
>;

const legacyContextRequest = StaffPostingProposalContextRequestContract.omit({
  supervision_context: true,
  management_review: true,
  chain_review: true,
});

export async function getStaffPostingProposalContext(
  scope: StaffPostingProposalScope,
  proposal: string,
  rawRequest: PostingContextSelection,
  signal?: AbortSignal,
) {
  const request = legacyContextRequest.parse(rawRequest);
  const params = new URLSearchParams({
    purpose: request.purpose,
    expected_version: String(request.expected_version),
    expected_revision_id: commandId.parse(request.expected_revision_id),
  });
  const result = StaffPostingProposalContextContract.safeParse(
    await apiFetch(`${root(scope)}/${commandId.parse(proposal)}/context?${params}`, {
      ...enrollmentRequestOptions(scope),
      signal,
    }),
  );
  if (!result.success)
    throw new Error("The protected posting review context could not be verified.");
  const context = result.data;
  return verifiedPostingContext(context, scope, proposal, request);
}

export async function getStaffPostingSupervisionContext(
  scope: StaffPostingProposalScope,
  proposal: string,
  rawRequest: PostingContextSelection,
  signal?: AbortSignal,
) {
  return getPostingSupervisionView(
    scope,
    proposal,
    legacyContextRequest.parse(rawRequest),
    false,
    signal,
  );
}

export async function getStaffPostingManagementReviewContext(
  scope: StaffPostingProposalScope,
  proposal: string,
  rawRequest: PostingContextSelection & { purpose: "review" },
  signal?: AbortSignal,
) {
  const request = legacyContextRequest.extend({ purpose: z.literal("review") }).parse(rawRequest);
  const view = await getPostingSupervisionView(scope, proposal, request, true, signal);
  if (
    view.supervision?.kind !== "hospital_management" ||
    view.context.proposal.status !== "pending"
  )
    throw new Error(
      "Current management review requires a pending explicit hospital-management request.",
    );
  return view;
}

async function getPostingSupervisionView(
  scope: StaffPostingProposalScope,
  proposal: string,
  request: PostingContextSelection,
  management: boolean,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const params = new URLSearchParams({
    purpose: request.purpose,
    expected_version: String(request.expected_version),
    expected_revision_id: commandId.parse(request.expected_revision_id),
    supervision_context: "v1",
  });
  if (management) params.set("management_review", "current");
  const view = StaffPostingSupervisionContextContract.parse(
    await apiFetch(`${root(scope)}/${commandId.parse(proposal)}/context?${params}`, {
      ...enrollmentRequestOptions(scope),
      signal,
    }),
  );
  signal?.throwIfAborted();
  verifiedPostingContext(view.context, scope, proposal, request);
  return view;
}

function verifiedPostingContext(
  context: z.infer<typeof StaffPostingProposalContextContract>,
  scope: StaffPostingProposalScope,
  proposal: string,
  request: PostingContextSelection,
) {
  const row = verifiedProposal(context.proposal, scope, proposal);
  const selected = context.draft;
  if (
    context.purpose !== request.purpose ||
    row.version !== request.expected_version ||
    !same(row.revision_id, request.expected_revision_id) ||
    !matchesScope(selected, scope) ||
    !same(selected.id, row.draft_id) ||
    !same(selected.revision_id, row.draft_revision_id) ||
    selected.version !== row.draft_version ||
    selected.staff_version !== row.staff_version ||
    selected.employment_epoch !== row.employment_epoch ||
    !same(selected.department_id, row.department_id) ||
    !same(selected.department_revision_id, row.department_revision_id) ||
    staffPostingInstantMicros(selected.starts_at) !== staffPostingInstantMicros(row.starts_at) ||
    (selected.ends_at ? staffPostingInstantMicros(selected.ends_at) : null) !==
      (row.ends_at ? staffPostingInstantMicros(row.ends_at) : null) ||
    (row.status === "pending") !== (context.decision_reason === null) ||
    [context.draft_reason, context.submission_reason, context.decision_reason].some(
      (reason) => reason !== null && reason !== reason.trim(),
    )
  )
    throw new Error("The protected posting context does not match the selected proposal sources.");
  return context;
}

const inboxPageContract = z
  .object({
    data: z.array(StaffPostingInboxItemContract).max(8),
    page: z.object({ has_more: z.boolean(), next_cursor: cursorSchema.nullable() }).strict(),
  })
  .strict();

export async function listStaffPostingProposalInbox(
  scope: StaffPostingInboxScope,
  status: StaffPostingProposal["status"] = "pending",
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const selectedStatus = StaffPostingProposalContract.shape.status.parse(status);
  const query = new URLSearchParams({ status: selectedStatus, limit: "8" });
  if (cursor !== null) query.set("cursor", cursorSchema.parse(cursor));
  signal?.throwIfAborted();
  const raw = await apiFetch(`${hospitalRoot(scope)}/posting-proposal-inbox?${query}`, {
    ...enrollmentRequestOptions(scope),
    signal,
  });
  signal?.throwIfAborted();
  const page = inboxPageContract.parse(raw);
  if (
    page.page.has_more !== Boolean(page.page.next_cursor) ||
    (page.page.has_more && page.data.length === 0) ||
    (cursor !== null && page.page.next_cursor === cursor) ||
    new Set(page.data.map((item) => item.proposal.id.toLowerCase())).size !== page.data.length
  )
    throw new Error("The posting proposal inbox page could not be verified.");
  for (const [index, item] of page.data.entries()) {
    if (
      !same(item.proposal.hospital_id, scope.hospitalId) ||
      !same(item.staff.id, item.proposal.staff_id) ||
      item.proposal.status !== selectedStatus
    )
      throw new Error("The posting inbox does not match this hospital, Staff or status.");
    if (index > 0) {
      const previous = page.data[index - 1].proposal;
      const before = staffPostingInstantMicros(previous.created_at)!;
      const after = staffPostingInstantMicros(item.proposal.created_at)!;
      if (
        before < after ||
        (before === after && previous.id.toLowerCase() <= item.proposal.id.toLowerCase())
      )
        throw new Error("The posting proposal inbox order could not be verified.");
    }
  }
  return page;
}

const supervisorChoicePage = z
  .object({
    data: z.array(StaffPostingSupervisorChoiceContract).max(8),
    page: z.object({ has_more: z.boolean(), next_cursor: cursorSchema.nullable() }).strict(),
  })
  .strict();

export async function listStaffPostingSupervisorChoices(
  scope: StaffPostingInboxScope,
  purpose: "submission" | "review",
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const selected = z.enum(["submission", "review"]).parse(purpose);
  const query = new URLSearchParams({ purpose: selected, limit: "8" });
  if (cursor !== null) query.set("cursor", cursorSchema.parse(cursor));
  signal?.throwIfAborted();
  const raw = await apiFetch(`${hospitalRoot(scope)}/posting-supervisor-choices?${query}`, {
    ...enrollmentRequestOptions(scope),
    signal,
  });
  signal?.throwIfAborted();
  const page = supervisorChoicePage.parse(raw);
  if (
    page.page.has_more !== Boolean(page.page.next_cursor) ||
    (page.page.has_more && page.data.length === 0) ||
    (cursor !== null && page.page.next_cursor === cursor) ||
    new Set(page.data.map((row) => row.id.toLowerCase())).size !== page.data.length
  )
    throw new Error("The supervisor choice page could not be verified.");
  for (let index = 1; index < page.data.length; index++) {
    const before = page.data[index - 1],
      after = page.data[index];
    const previous = staffPostingInstantMicros(before.created_at),
      current = staffPostingInstantMicros(after.created_at);
    if (
      previous === null ||
      current === null ||
      previous < current ||
      (previous === current && before.id.toLowerCase() <= after.id.toLowerCase())
    )
      throw new Error("The supervisor choice page order could not be verified.");
  }

  for (const choice of page.data) {
    if (!same(choice.hospital_id, scope.hospitalId))
      throw new Error("The supervisor choices do not match this hospital.");
  }
  return page;
}

export async function decideStaffPostingSupervisedProposal(
  scope: StaffPostingProposalScope,
  proposal: string,
  command: string,
  input: StaffPostingDecisionInput,
  selected: StaffPostingProposal,
  signal?: AbortSignal,
) {
  return postingDecisionRequest(scope, proposal, command, input, selected, "PUT", signal, true);
}

export async function getStaffPostingSupervisedDecisionReceipt(
  scope: StaffPostingProposalScope,
  proposal: string,
  command: string,
  input: StaffPostingDecisionInput,
  selected: StaffPostingProposal,
  signal?: AbortSignal,
) {
  return postingDecisionRequest(scope, proposal, command, input, selected, "GET", signal, true);
}

export async function getStaffPostingChainReviewContext(
  scope: StaffPostingProposalScope,
  proposal: string,
  rawRequest: PostingContextSelection & { purpose: "review" },
  signal?: AbortSignal,
) {
  const request = legacyContextRequest.extend({ purpose: z.literal("review") }).parse(rawRequest);
  signal?.throwIfAborted();
  const params = new URLSearchParams({
    purpose: "review",
    expected_version: String(request.expected_version),
    expected_revision_id: request.expected_revision_id,
    supervision_context: "v1",
    chain_review: "current",
  });
  const view = StaffPostingChainReviewContextContract.parse(
    await apiFetch(`${root(scope)}/${commandId.parse(proposal)}/context?${params}`, {
      ...enrollmentRequestOptions(scope),
      signal,
    }),
  );
  signal?.throwIfAborted();
  verifiedPostingContext(view.context, scope, proposal, request);
  if (view.context.proposal.status !== "pending" || view.supervision.kind === "hospital_management")
    throw new Error("Choose a pending supervised posting for current chain review.");
  const seenStaff = new Set([scope.staffId.toLowerCase()]);
  const seenPostings = new Set([proposal.toLowerCase()]);
  let child: { starts_at: string; ends_at?: string | null } = view.context.proposal;
  let intent = view.supervision;
  let until = staffPostingInstantMicros(view.context.proposal.eligibility_until)!;
  if (child.ends_at != null && staffPostingInstantMicros(child.ends_at)! < until)
    until = staffPostingInstantMicros(child.ends_at)!;
  for (const [index, node] of view.chain.entries()) {
    const parent = node.posting;
    if (
      !same(parent.hospital_id, scope.hospitalId) ||
      !same(parent.staff.id, intent.supervisor_staff_id ?? "") ||
      !same(parent.id, intent.supervisor_proposal_id ?? "") ||
      !same(parent.revision_id, intent.supervisor_revision_id ?? "") ||
      seenStaff.has(parent.staff.id.toLowerCase()) ||
      seenPostings.has(parent.id.toLowerCase()) ||
      parent.kind !== node.supervision.kind ||
      staffPostingInstantMicros(child.starts_at)! < staffPostingInstantMicros(parent.starts_at)! ||
      (parent.ends_at != null &&
        (child.ends_at == null ||
          staffPostingInstantMicros(child.ends_at)! >
            staffPostingInstantMicros(parent.ends_at)!)) ||
      (index === view.chain.length - 1) !== (node.supervision.kind === "hospital_management")
    )
      throw new Error("The current supervisor chain is disconnected, repeated or out of scope.");
    seenStaff.add(parent.staff.id.toLowerCase());
    seenPostings.add(parent.id.toLowerCase());
    for (const deadline of [parent.eligibility_until, parent.ends_at])
      if (deadline != null && staffPostingInstantMicros(deadline)! < until)
        until = staffPostingInstantMicros(deadline)!;
    child = parent;
    intent = node.supervision;
  }
  const checked = staffPostingInstantMicros(view.checked_at),
    cutoff = staffPostingInstantMicros(view.eligibility_until);
  if (
    checked === null ||
    checked <= staffPostingInstantMicros("0001-01-01T00:00:00Z")! ||
    cutoff !== until ||
    checked >= until ||
    staffPostingInstantMicros(view.context.proposal.starts_at)! >= until
  )
    throw new Error("The checked supervisor chain cutoff could not be verified.");
  return view;
}
