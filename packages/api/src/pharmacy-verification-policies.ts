import { z } from "zod";
import { accountScopedRequest, type AccountRequestScope } from "./account-scoped-request";
import { pharmacyKey, type PharmacyScope } from "./pharmacy";
import { usePagedQuery } from "./paged";
import {
  VerificationPolicyDraftInputContract,
  VerificationPolicyDraftRevisionContract,
  type VerificationPolicyDraftInput,
} from "./pharmacy-verification-contract";

export type VerificationPolicyScope = PharmacyScope & AccountRequestScope;
export type {
  VerificationPolicyDraftInput,
  VerificationPolicyDraftRevision,
} from "./pharmacy-verification-contract";
export { VerificationPolicyDraftInputContract } from "./pharmacy-verification-contract";
const id = z.uuidv7().refine((value) => value === value.toLowerCase());
const pageContract = z
  .object({
    data: z.array(VerificationPolicyDraftRevisionContract).max(50),
    page: z.object({ next_cursor: z.string().min(1).nullable(), has_more: z.boolean() }).strict(),
  })
  .strict()
  .refine(
    (value) => value.page.has_more === (value.page.next_cursor !== null),
    "Invalid policy revision pagination.",
  );
function root(scope: VerificationPolicyScope) {
  return `/pharmacy/pharmacies/${id.parse(scope.pharmacyId)}/outlets/${id.parse(scope.outletId)}/verification-policy-drafts`;
}
function boundRevision(
  scope: VerificationPolicyScope,
  raw: unknown,
  policy?: string,
  revision?: string,
) {
  const row = VerificationPolicyDraftRevisionContract.parse(raw);
  if (
    row.outlet_id !== scope.outletId ||
    (policy !== undefined && row.policy_id !== policy) ||
    (revision !== undefined && row.id !== revision) ||
    row.version !== row.expected_version + 1
  ) {
    throw new Error("The policy draft response could not be verified. Refresh before continuing.");
  }
  return row;
}
export async function readVerificationPolicyDraft(
  scope: VerificationPolicyScope,
  policy: string,
  revision: string,
  signal?: AbortSignal,
) {
  const path = `${root(scope)}/${id.parse(policy)}/revisions/${id.parse(revision)}`;
  return boundRevision(scope, await accountScopedRequest(scope, path, signal), policy, revision);
}
export async function saveVerificationPolicyDraft(
  scope: VerificationPolicyScope,
  policy: string,
  revision: string,
  input: VerificationPolicyDraftInput,
  signal?: AbortSignal,
) {
  const intent = VerificationPolicyDraftInputContract.parse(input);
  const path = `${root(scope)}/${id.parse(policy)}/revisions/${id.parse(revision)}`;
  const row = boundRevision(
    scope,
    await accountScopedRequest(scope, path, signal, { method: "PUT", body: intent }),
    policy,
    revision,
  );
  if (
    row.prepared_by !== scope.subject ||
    Object.entries(intent).some(([field, value]) => row[field as keyof typeof row] !== value)
  ) {
    throw new Error(
      "The saved policy draft does not match this request. Recover the original revision before continuing.",
    );
  }
  return row;
}
export async function listVerificationPolicyDrafts(
  scope: VerificationPolicyScope,
  cursor?: string | null,
  policy?: string,
  signal?: AbortSignal,
) {
  const path = policy === undefined ? root(scope) : `${root(scope)}/${id.parse(policy)}/revisions`;
  const params = new URLSearchParams({ limit: "50" });
  if (cursor) params.set("cursor", cursor);
  const page = pageContract.parse(await accountScopedRequest(scope, `${path}?${params}`, signal));
  return { ...page, data: page.data.map((row) => boundRevision(scope, row, policy)) };
}
export function useVerificationPolicyDrafts(scope: VerificationPolicyScope, policy?: string) {
  return usePagedQuery(
    pharmacyKey(scope, "verification-policy-drafts", scope.subject, policy ?? "all"),
    (cursor, signal) => listVerificationPolicyDrafts(scope, cursor, policy, signal),
    { gcTime: 0, retry: false },
  );
}
