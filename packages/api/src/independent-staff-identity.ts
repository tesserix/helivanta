import { z } from "zod";
import { apiFetch } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import {
  IndependentLinkInputContract,
  IndependentLinkDecisionInputContract,
  IndependentLinkProposalContract,
  IndependentLinkCommandContract,
  IndependentLinkViewContract,
  type IndependentLinkInput,
  type IndependentLinkDecisionInput,
  type IndependentIdentityScope,
} from "./workforce-independent-identity-contract";

export * from "./workforce-independent-identity-contract";
export { v7 as newIndependentStaffLinkId } from "uuid";
export type IndependentStaffScope = {
  tenantId: string;
  subject: string;
  employerKind: "pharmacy" | "supplier";
  employerId: string;
  staffId: string;
};
const staffId = z
  .uuid()
  .refine(
    (value) => value !== "00000000-0000-0000-0000-000000000000" && value === value.toLowerCase(),
  );
const id = z.uuid().regex(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
function linksBase(scope: IndependentStaffScope) {
  const family =
    z.enum(["pharmacy", "supplier"]).parse(scope.employerKind) === "pharmacy"
      ? "pharmacies"
      : "suppliers";
  return `/workforce/${family}/${id.parse(scope.employerId)}/staff/${staffId.parse(scope.staffId)}/account-links`;
}
const base = (scope: IndependentStaffScope, link: string) =>
  `${linksBase(scope)}/${id.parse(link)}`;
function sameScope(scope: IndependentStaffScope, result: IndependentIdentityScope) {
  return (
    result.owner_tenant_id === scope.tenantId &&
    result.employer_kind === scope.employerKind &&
    result.employer_id === scope.employerId &&
    result.staff_id === scope.staffId
  );
}
function exactProposal(
  scope: IndependentStaffScope,
  link: string,
  input: IndependentLinkInput,
  value: unknown,
) {
  const result = IndependentLinkProposalContract.parse(value);
  if (
    result.id !== link ||
    !sameScope(scope, result.scope) ||
    result.prepared_by !== scope.subject ||
    Object.entries(input).some(
      ([key, value]) => result.input[key as keyof IndependentLinkInput] !== value,
    )
  )
    throw new Error(
      "The account proposal does not match this Staff record, account or original intent.",
    );
  return result;
}
export async function proposeIndependentStaffLink(
  scope: IndependentStaffScope,
  link: string,
  input: IndependentLinkInput,
) {
  const body = IndependentLinkInputContract.parse(input);
  return exactProposal(
    scope,
    link,
    body,
    await apiFetch(base(scope, link), {
      ...membershipRequestOptions(scope),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
}
export async function recoverIndependentStaffProposal(
  scope: IndependentStaffScope,
  link: string,
  input: IndependentLinkInput,
) {
  return exactProposal(
    scope,
    link,
    IndependentLinkInputContract.parse(input),
    await apiFetch(`${base(scope, link)}/proposal-receipt`, membershipRequestOptions(scope)),
  );
}
export async function getIndependentStaffLink(
  scope: IndependentStaffScope,
  link: string,
  signal?: AbortSignal,
) {
  const result = IndependentLinkViewContract.parse(
    await apiFetch(base(scope, link), { ...membershipRequestOptions(scope), signal }),
  );
  if (
    !sameScope(scope, result.scope) ||
    result.link.id !== link ||
    result.link.staff_id !== scope.staffId
  )
    throw new Error("The account link does not match this Staff record.");
  return result;
}
function exactCommand(
  scope: IndependentStaffScope,
  link: string,
  command: string,
  input: IndependentLinkDecisionInput,
  value: unknown,
) {
  const result = IndependentLinkCommandContract.parse(value);
  const row = result.result.link;
  const next = { approve: "active", reject: "rejected", withdraw: "withdrawn", revoke: "revoked" }[
    input.decision
  ];
  if (
    result.id !== command ||
    result.link_id !== link ||
    !sameScope(scope, result.scope) ||
    !sameScope(scope, result.result.scope) ||
    result.actor_subject !== scope.subject ||
    Object.entries(input).some(
      ([key, value]) => result.input[key as keyof IndependentLinkDecisionInput] !== value,
    ) ||
    row.id !== link ||
    row.staff_id !== scope.staffId ||
    row.version !== input.expected_version + 1 ||
    row.status !== next ||
    row.actor_subject !== scope.subject ||
    row.reason !== input.reason ||
    row.updated_at !== result.created_at
  )
    throw new Error(
      "The account decision does not match this Staff record, account or original intent.",
    );
  return result;
}
export async function decideIndependentStaffLink(
  scope: IndependentStaffScope,
  link: string,
  command: string,
  input: IndependentLinkDecisionInput,
) {
  const body = IndependentLinkDecisionInputContract.parse(input);
  return exactCommand(
    scope,
    link,
    command,
    body,
    await apiFetch(`${base(scope, link)}/commands/${id.parse(command)}`, {
      ...membershipRequestOptions(scope),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
}
export async function recoverIndependentStaffCommand(
  scope: IndependentStaffScope,
  link: string,
  command: string,
  input: IndependentLinkDecisionInput,
) {
  return exactCommand(
    scope,
    link,
    command,
    IndependentLinkDecisionInputContract.parse(input),
    await apiFetch(
      `${base(scope, link)}/commands/${id.parse(command)}`,
      membershipRequestOptions(scope),
    ),
  );
}

const pageContract = z
  .object({
    data: z.array(IndependentLinkViewContract).max(25),
    page: z
      .object({ has_more: z.boolean(), next_cursor: z.string().min(1).max(4096).nullable() })
      .strict(),
  })
  .strict();
export async function listIndependentStaffLinksPage(
  scope: IndependentStaffScope,
  cursor: string | null,
  signal?: AbortSignal,
) {
  const parameters = new URLSearchParams({ limit: "25" });
  if (cursor !== null) parameters.set("cursor", z.string().min(1).max(4096).parse(cursor));
  const root = linksBase(scope);
  const result = pageContract.parse(
    await apiFetch(`${root}?${parameters}`, { ...membershipRequestOptions(scope), signal }),
  );
  if (
    result.page.has_more !== Boolean(result.page.next_cursor) ||
    (result.page.has_more && result.data.length === 0) ||
    new Set(result.data.map((row) => row.link.id)).size !== result.data.length ||
    result.data.some((row) => !sameScope(scope, row.scope) || row.link.staff_id !== scope.staffId)
  )
    throw new Error(
      "The Staff account-link page does not match this workspace or cannot be continued safely.",
    );
  return result;
}
