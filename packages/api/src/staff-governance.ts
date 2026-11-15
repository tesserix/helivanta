import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { accountScopedRequest, type AccountRequestScope } from "./account-scoped-request";
import {
  GovernanceGrantInputContract,
  GovernanceGrantReceiptContract,
  GovernanceOwnerNoticeContract,
  GovernanceGrantDecisionInputContract,
  GovernanceGrantDecisionReceiptContract,
  type GovernanceGrantInput,
  type GovernanceGrantDecisionInput,
} from "./staff-governance-contract";
import { staffPostingInstantMicros } from "./staff-postings-contract";

export * from "./staff-governance-contract";
export { v7 as newGovernanceCommandId } from "uuid";
export type GovernanceRequestScope = AccountRequestScope;

const commandId = z.uuidv7().refine((value) => value === value.toLowerCase());
const failure = () => new Error("The governance command response could not be verified.");

function grantPath(id: string) {
  return "/iam/governance-grants/" + commandId.parse(id);
}

function boundGrant(raw: unknown, scope: GovernanceRequestScope, id: string) {
  const receipt = GovernanceGrantReceiptContract.parse(raw);
  if (receipt.tenant_id !== scope.tenantId.toLowerCase() || receipt.id !== id) throw failure();
  return receipt;
}

function comparableInput(input: GovernanceGrantInput) {
  return JSON.stringify({
    ...input,
    scope_id: input.scope_id?.toLowerCase(),
    valid_from: staffPostingInstantMicros(input.valid_from)?.toString(),
    expires_at: staffPostingInstantMicros(input.expires_at)?.toString(),
  });
}

export async function readGovernanceGrant(
  scope: GovernanceRequestScope,
  id: string,
  signal?: AbortSignal,
) {
  return boundGrant(await accountScopedRequest(scope, grantPath(id), signal), scope, id);
}

export async function issueGovernanceGrant(
  scope: GovernanceRequestScope,
  id: string,
  input: GovernanceGrantInput,
  signal?: AbortSignal,
) {
  const body = GovernanceGrantInputContract.parse(input);
  const receipt = boundGrant(
    await accountScopedRequest(scope, grantPath(id), signal, { method: "PUT", body }),
    scope,
    id,
  );
  if (
    receipt.issued_by !== scope.subject ||
    receipt.version !== 1 ||
    !["pending_review", "provisional_active"].includes(receipt.status) ||
    comparableInput(receipt.input) !== comparableInput(body)
  )
    throw failure();
  return receipt;
}

export async function decideGovernanceGrant(
  scope: GovernanceRequestScope,
  id: string,
  command: string,
  input: GovernanceGrantDecisionInput,
  signal?: AbortSignal,
) {
  const path = grantPath(id) + "/decisions/" + commandId.parse(command);
  const body = GovernanceGrantDecisionInputContract.parse(input);
  const receipt = GovernanceGrantDecisionReceiptContract.parse(
    await accountScopedRequest(scope, path, signal, { method: "PUT", body }),
  );
  if (
    receipt.tenant_id !== scope.tenantId.toLowerCase() ||
    receipt.grant_id !== id ||
    receipt.id !== command ||
    receipt.actor_subject !== scope.subject ||
    receipt.result_version !== body.expected_version + 1 ||
    JSON.stringify(receipt.input) !== JSON.stringify(body)
  )
    throw failure();
  return receipt;
}

const noticeCursor = z.string().min(1).max(2048);
const noticePage = z
  .object({
    data: z.array(GovernanceOwnerNoticeContract).max(50),
    page: z.object({ has_more: z.boolean(), next_cursor: noticeCursor.nullable() }).strict(),
  })
  .strict();

export async function listGovernanceNotices(
  scope: GovernanceRequestScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ limit: "50" });
  if (cursor !== null) query.set("cursor", noticeCursor.parse(cursor));
  const result = noticePage.parse(
    await accountScopedRequest(scope, "/iam/governance-notices?" + query, signal),
  );
  if (
    result.data.some(
      (row) =>
        row.tenant_id !== scope.tenantId.toLowerCase() || row.recipient_subject !== scope.subject,
    ) ||
    new Set(result.data.map((row) => row.id)).size !== result.data.length ||
    result.page.has_more !== Boolean(result.page.next_cursor) ||
    (cursor !== null && result.page.next_cursor === cursor)
  )
    throw failure();
  return result;
}

export function useGovernanceNotices(scope: GovernanceRequestScope, cursor: string | null) {
  return useQuery({
    queryKey: ["governance-notices", scope.tenantId, scope.subject, cursor],
    queryFn: ({ signal }) => listGovernanceNotices(scope, cursor, signal),
    staleTime: 0,
    gcTime: 0,
    retry: false,
  });
}

export function useGovernanceGrant(scope: GovernanceRequestScope, id: string) {
  return useQuery({
    queryKey: ["governance-grant", scope.tenantId, scope.subject, id],
    queryFn: ({ signal }) => readGovernanceGrant(scope, id, signal),
    staleTime: 0,
    gcTime: 0,
    retry: false,
  });
}
