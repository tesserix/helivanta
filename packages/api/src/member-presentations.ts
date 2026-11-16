import { accountScopedRequest, type AccountRequestScope } from "./account-scoped-request";
import {
  MemberPresentationRequestContract,
  MemberPresentationsContract,
  type MemberPresentation,
} from "./staff-access-contract";

export type { MemberPresentation } from "./staff-access-contract";

export async function readMemberPresentations(
  scope: AccountRequestScope,
  subjects: string[],
  signal?: AbortSignal,
): Promise<MemberPresentation[]> {
  const wanted = [...new Set(subjects)].sort();
  const result: MemberPresentation[] = [];
  for (let offset = 0; offset < wanted.length; offset += 20) {
    const batch = wanted.slice(offset, offset + 20);
    const body = MemberPresentationRequestContract.parse({ subjects: batch });
    const response = MemberPresentationsContract.parse(
      await accountScopedRequest(scope, "/iam/member-presentations", signal, {
        method: "POST",
        body,
      }),
    );
    if (
      response.tenant_id !== scope.tenantId ||
      response.actor_subject !== scope.subject ||
      response.data.length !== batch.length ||
      response.data.some(
        (row, index) =>
          row.subject !== batch[index] ||
          row.available !== Boolean(row.name && row.email) ||
          new Set(row.roles.map((role) => role.key)).size !== row.roles.length ||
          (!row.available && (row.name !== undefined || row.email !== undefined)),
      )
    )
      throw new Error("The current account details could not be verified.");
    result.push(...response.data);
  }
  return result;
}
