import { z } from "zod";
import { isEnrollmentAccessChanged } from "./enrollment";
import { ApiError, apiFetch } from "./client";
import { personReviewRequest } from "./person-reviews";
import { staffAccessTemplates } from "./staff-access-templates";
import {
  AssignmentGovernanceHospitalScopeContract,
  AssignmentAccountPresentationContract,
  TemplateDefinitionContract,
  ProposalOutputContract,
  ProposalHistoryContract,
  ProposalDecisionRecordContract,
} from "./staff-access-contract";
import {
  staffAssignmentInputContract,
  staffAssignmentDecisionContract,
  type StaffAssignment,
  type StaffAssignmentInput,
  type StaffAssignmentDecision,
  type StaffAssignmentFamily,
  type StaffAssignmentScope,
} from "./staff-assignments";

export type GovernanceAssignmentScope = StaffAssignmentScope & { hospitalId: string };
export type GovernanceAssignmentHospital = z.infer<
  typeof AssignmentGovernanceHospitalScopeContract
>;
const cursorContract = z.string().min(1).max(2048);
const pageInfo = z
  .object({ has_more: z.boolean(), next_cursor: cursorContract.nullable() })
  .strict();
const hospitalsPage = z
  .object({ data: AssignmentGovernanceHospitalScopeContract.array().max(20), page: pageInfo })
  .strict();
const proposalsPage = z
  .object({ data: ProposalOutputContract.array().max(50), page: pageInfo })
  .strict();
const historyPage = z
  .object({ data: ProposalHistoryContract.array().max(50), page: pageInfo })
  .strict();
const decisionsPage = z
  .object({ data: ProposalDecisionRecordContract.array().max(50), page: pageInfo })
  .strict();
type PageOptions = { family?: StaffAssignmentFamily; cursor?: string | null; signal?: AbortSignal };

export const governanceAssignmentScopesKey = (scope: StaffAssignmentScope) => [
  "workforce",
  "assignment-authority-scopes",
  scope.tenantId,
  scope.subject,
];
export const governanceAssignmentsKey = (scope: GovernanceAssignmentScope) => [
  "iam",
  "governance-assignments",
  scope.tenantId,
  scope.subject,
  scope.hospitalId,
];

function root(scope: GovernanceAssignmentScope) {
  return `/iam/hospitals/${z.uuid().parse(scope.hospitalId)}/assignment-proposals`;
}
function query(limit: number, cursor?: string | null) {
  const parameters = new URLSearchParams({ limit: String(limit) });
  if (cursor !== undefined && cursor !== null)
    parameters.set("cursor", cursorContract.parse(cursor));
  return parameters;
}
function checkedPage<T extends { page: z.infer<typeof pageInfo> }>(page: T): T {
  if (page.page.has_more !== Boolean(page.page.next_cursor))
    throw new Error(
      "The assignment continuation could not be verified. Refresh before continuing.",
    );
  return page;
}
function checkedProposal(scope: GovernanceAssignmentScope, value: unknown, id?: string) {
  const result = ProposalOutputContract.parse(value);
  if (result.input.hospital_id !== scope.hospitalId || (id !== undefined && result.id !== id))
    throw new Error("The assignment does not match the selected hospital or request.");
  return result;
}

export async function listGovernanceAssignmentScopes(
  scope: StaffAssignmentScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const answer = await apiFetch<unknown>(
    `/workforce/assignment-authority-scopes?${query(20, cursor)}`,
    { ...personReviewRequest(scope), signal },
  );
  signal?.throwIfAborted();
  const page = checkedPage(hospitalsPage.parse(answer));
  if (page.data.some((row) => !row.can_manage && !row.can_review))
    throw new Error("The assignment hospital access could not be verified.");
  return page;
}

export async function listGovernanceAssignments(
  scope: GovernanceAssignmentScope,
  options: PageOptions = {},
) {
  options.signal?.throwIfAborted();
  const family = options.family ?? "leave";
  if (!staffAccessTemplates.some((template) => template.family === family))
    throw new Error("Choose a supported assignment family.");
  const parameters = query(50, options.cursor);
  parameters.set("family", family);
  const answer = await apiFetch<unknown>(`${root(scope)}?${parameters}`, {
    ...personReviewRequest(scope),
    signal: options.signal,
  });
  options.signal?.throwIfAborted();
  const page = checkedPage(proposalsPage.parse(answer));
  for (const row of page.data) {
    checkedProposal(scope, row);
    if (
      !staffAccessTemplates.some(
        (template) => template.key === row.input.template_key && template.family === family,
      )
    )
      throw new Error("The assignment family could not be verified.");
  }
  return page;
}

export async function getGovernanceAssignment(
  scope: GovernanceAssignmentScope,
  id: string,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const verifiedID = ProposalOutputContract.shape.id.parse(id);
  const answer = await apiFetch<unknown>(`${root(scope)}/${verifiedID}`, {
    ...personReviewRequest(scope),
    signal,
  });
  signal?.throwIfAborted();
  return checkedProposal(scope, answer, verifiedID);
}

export async function proposeGovernanceAssignment(
  scope: GovernanceAssignmentScope,
  id: string,
  input: StaffAssignmentInput,
) {
  const intent = staffAssignmentInputContract.parse(input);
  if (intent.hospital_id !== scope.hospitalId)
    throw new Error(
      "The assignment hospital changed. Select the current hospital before submitting.",
    );
  const verifiedID = ProposalOutputContract.shape.id.parse(id);
  const answer = await apiFetch<unknown>(`${root(scope)}/${verifiedID}`, {
    ...personReviewRequest(scope),
    method: "PUT",
    body: JSON.stringify(intent),
  });
  return checkedProposal(scope, answer, verifiedID);
}

export async function decideGovernanceAssignment(
  scope: GovernanceAssignmentScope,
  id: string,
  input: StaffAssignmentDecision,
) {
  const verifiedID = ProposalOutputContract.shape.id.parse(id);
  const intent = staffAssignmentDecisionContract.parse(input);
  const answer = await apiFetch<unknown>(`${root(scope)}/${verifiedID}/decision`, {
    ...personReviewRequest(scope),
    method: "POST",
    body: JSON.stringify(intent),
  });
  return checkedProposal(scope, answer, verifiedID);
}

export async function listGovernanceAssignmentHistory(
  scope: GovernanceAssignmentScope,
  id: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const verifiedID = ProposalOutputContract.shape.id.parse(id);
  const answer = await apiFetch<unknown>(
    `${root(scope)}/${verifiedID}/history?${query(50, cursor)}`,
    { ...personReviewRequest(scope), signal },
  );
  signal?.throwIfAborted();
  const page = checkedPage(historyPage.parse(answer));
  if (page.data.some((row) => row.id !== verifiedID))
    throw new Error("The assignment history does not match the selected request.");
  return page;
}

export async function listGovernanceAssignmentDecisions(
  scope: GovernanceAssignmentScope,
  id: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const verifiedID = ProposalOutputContract.shape.id.parse(id);
  const answer = await apiFetch<unknown>(
    `${root(scope)}/${verifiedID}/decisions?${query(50, cursor)}`,
    { ...personReviewRequest(scope), signal },
  );
  signal?.throwIfAborted();
  const page = checkedPage(decisionsPage.parse(answer));
  if (page.data.some((row) => row.proposal_id !== verifiedID))
    throw new Error("The assignment decisions do not match the selected request.");
  return page;
}

export async function listGovernanceAssignmentTemplates(
  scope: GovernanceAssignmentScope,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const answer = await apiFetch<unknown>(
    `/iam/hospitals/${z.uuid().parse(scope.hospitalId)}/assignment-templates`,
    { ...personReviewRequest(scope), signal },
  );
  signal?.throwIfAborted();
  const definitions = TemplateDefinitionContract.array()
    .length(staffAccessTemplates.length)
    .parse(answer);
  const expected = TemplateDefinitionContract.array().parse(staffAccessTemplates);
  const byKey = (a: { key: string }, b: { key: string }) => a.key.localeCompare(b.key);
  if (JSON.stringify(definitions.toSorted(byKey)) !== JSON.stringify(expected.toSorted(byKey)))
    throw new Error(
      "The assignment catalog changed. Refresh this workspace before preparing access.",
    );
  return definitions;
}

export async function getGovernanceAssignmentAccount(
  scope: GovernanceAssignmentScope,
  item: Pick<StaffAssignment, "id" | "input" | "version">,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  if (item.input.hospital_id !== scope.hospitalId)
    throw new Error("The selected hospital changed.");
  const answer = await apiFetch<unknown>(
    `${root(scope)}/${ProposalOutputContract.shape.id.parse(item.id)}/account`,
    {
      ...personReviewRequest(scope),
      signal,
    },
  );
  signal?.throwIfAborted();
  const account = AssignmentAccountPresentationContract.parse(answer);
  if (
    account.proposal_id !== item.id ||
    account.proposal_version !== item.version ||
    account.subject !== item.input.subject ||
    (account.available
      ? account.name === undefined || account.email === undefined
      : account.name !== undefined || account.email !== undefined)
  )
    throw new Error(
      "The selected assignment or account identity changed. Refresh before continuing.",
    );
  return account;
}

export function isGovernanceAssignmentAccessChanged(error: unknown): boolean {
  return (
    isEnrollmentAccessChanged(error) ||
    (error instanceof ApiError &&
      error.status === 409 &&
      ["tenant_changed", "assignment_session_context_required"].includes(error.code))
  );
}
