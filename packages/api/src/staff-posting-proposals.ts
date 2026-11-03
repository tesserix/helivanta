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
  staffPostingInstantMicros,
  type StaffPostingDraft,
  type StaffPostingProposal,
  type StaffPostingProposalInput,
  type StaffPostingDecisionInput,
} from "./staff-postings-contract";

export type StaffPostingProposalScope = Omit<StaffPostingScope, "access"> & { access: "scoped" };
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

function root(scope: StaffPostingProposalScope) {
  if (scope.access !== "scoped")
    throw new Error("Posting review requires an explicitly scoped workspace.");
  identifier.parse(scope.tenantId);
  z.string()
    .min(1)
    .max(200)
    .refine((value) => value === value.trim() && !/\p{Cc}/u.test(value))
    .parse(scope.subject);
  if (tenantTransitionPending()) throw new Error("Wait for your workspace to finish switching.");
  return `/workforce/hospitals/${identifier.parse(scope.hospitalId)}/posting-staff/${identifier.parse(scope.staffId)}/posting-proposals`;
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
    row.status !== (input.decision === "reject" ? "rejected" : "withdrawn") ||
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
  if (intent.decision === "approve")
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
