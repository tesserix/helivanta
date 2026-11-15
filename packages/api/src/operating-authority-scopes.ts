import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { apiFetch } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import { OperatingGovernanceEmployerScopeContract } from "./staff-access-contract";
import type { OperatingDutyStaffScope } from "./operating-duties";
export type OperatingAuthorityScope = Pick<OperatingDutyStaffScope, "tenantId" | "subject">;
export { OperatingGovernanceEmployerScopeContract } from "./staff-access-contract";
export type { OperatingGovernanceEmployerScope } from "./staff-access-contract";
const cursorContract = z.string().min(1).max(2048);
const pageContract = z
  .object({
    data: OperatingGovernanceEmployerScopeContract.array().max(20),
    page: z.object({ has_more: z.boolean(), next_cursor: cursorContract.nullable() }).strict(),
  })
  .strict()
  .superRefine((value, context) => {
    if (
      value.page.has_more !== (value.page.next_cursor !== null) ||
      new Set(value.data.map((row) => `${row.employer_kind}:${row.employer_id}`)).size !==
        value.data.length ||
      value.data.some((row) => !row.can_manage && !row.can_review)
    )
      context.addIssue({
        code: "custom",
        message: "The current employer choices could not be verified.",
      });
  });
export const operatingAuthorityScopesKey = (scope: OperatingAuthorityScope) => [
  "workforce",
  "operating-authority-scopes",
  scope.tenantId,
  scope.subject,
];
export async function listOperatingAuthorityScopes(
  scope: OperatingAuthorityScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  signal?.throwIfAborted();
  const query = new URLSearchParams({ limit: "20" });
  if (cursor !== null) query.set("cursor", cursorContract.parse(cursor));
  const response = await apiFetch<unknown>(`/workforce/operating-authority-scopes?${query}`, {
    ...membershipRequestOptions(scope),
    signal,
  });
  signal?.throwIfAborted();
  return pageContract.parse(response);
}
export function useOperatingAuthorityScopes(
  scope: OperatingAuthorityScope,
  cursor: string | null,
  enabled = true,
) {
  return useQuery({
    queryKey: [...operatingAuthorityScopesKey(scope), cursor],
    queryFn: ({ signal }) => listOperatingAuthorityScopes(scope, cursor, signal),
    enabled,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnWindowFocus: true,
  });
}
