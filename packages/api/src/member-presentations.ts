import { useQuery } from "@tanstack/react-query";
import { accountScopedRequest, type AccountRequestScope } from "./account-scoped-request";
import { ApiError } from "./client";
import {
  MemberPresentationRequestContract,
  MemberPresentationsContract,
  AccountPresentationsContract,
  type MemberPresentation,
  type AccountPresentation,
} from "./staff-access-contract";

export type { MemberPresentation, AccountPresentation } from "./staff-access-contract";
const batchSize = 20;

export function useDisplayedAccountPresentations(
  scope: AccountRequestScope | null,
  subjects: string[],
) {
  return useQuery({
    queryKey: ["iam", "displayed-account-presentations", scope?.tenantId, scope?.subject, subjects],
    queryFn: ({ signal }) => {
      if (!scope) throw new Error("Wait for the current account.");
      return readAccountPresentations(scope, subjects, signal);
    },
    enabled: !!scope && subjects.length > 0,
    gcTime: 0,
    staleTime: 0,
    retry: false,
    refetchInterval: 30_000,
  });
}

async function readBatch(
  scope: AccountRequestScope,
  batch: string[],
  signal: AbortSignal | undefined,
  accounts: boolean,
): Promise<MemberPresentation[]> {
  const body = MemberPresentationRequestContract.parse({ subjects: batch });
  const response = (accounts ? AccountPresentationsContract : MemberPresentationsContract).parse(
    await accountScopedRequest(
      scope,
      accounts ? "/iam/account-presentations" : "/iam/member-presentations",
      signal,
      { method: "POST", body },
    ),
  );
  const indexes = response.data.map((row) => batch.indexOf(row.subject));
  if (
    response.tenant_id !== scope.tenantId ||
    response.actor_subject !== scope.subject ||
    (!accounts && response.data.length !== batch.length) ||
    response.data.length > batch.length ||
    indexes.some(
      (index, position) => index < 0 || (position > 0 && index <= indexes[position - 1]),
    ) ||
    response.data.some(
      (row) =>
        row.available !== Boolean(row.name && row.email) ||
        new Set(row.roles.map((role) => role.key)).size !== row.roles.length ||
        (!row.available &&
          (row.name !== undefined || row.email !== undefined || row.roles.length === 0)),
    )
  )
    throw new Error("The current account details could not be verified.");
  return response.data;
}

export async function readMemberPresentations(
  scope: AccountRequestScope,
  subjects: string[],
  signal?: AbortSignal,
): Promise<MemberPresentation[]> {
  const wanted = [...new Set(subjects)].sort();
  const result: MemberPresentation[] = [];
  for (let offset = 0; offset < wanted.length; offset += batchSize)
    result.push(
      ...(await readBatch(scope, wanted.slice(offset, offset + batchSize), signal, false)),
    );
  return result;
}

export async function readAccountPresentations(
  scope: AccountRequestScope,
  subjects: string[],
  signal?: AbortSignal,
): Promise<AccountPresentation[]> {
  const wanted = [...new Set(subjects)].sort();
  const result: AccountPresentation[] = [];
  for (let offset = 0; offset < wanted.length; offset += batchSize) {
    try {
      result.push(
        ...(await readBatch(scope, wanted.slice(offset, offset + batchSize), signal, true)),
      );
    } catch (error) {
      if (!(error instanceof ApiError) || error.status !== 404) throw error;
    }
  }
  return result;
}
