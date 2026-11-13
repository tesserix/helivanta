"use client";

import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import {
  getGovernanceEmploymentSource,
  getGovernanceEmploymentDraft,
  listGovernanceEmploymentDrafts,
  searchGovernanceEmploymentTargets,
  type GovernanceEmploymentTargetScope,
  type GovernanceEmploymentPreparationScope,
} from "./employment-preparation";
import type { PersonReviewTargetSearchInput } from "./person-review-contract";

const privateRead = { gcTime: 0, staleTime: 0, retry: false, refetchOnWindowFocus: false } as const;
export const governanceEmploymentPreparationKey = (scope: GovernanceEmploymentTargetScope) => [
  "workforce",
  "governance-employment-preparation",
  scope.tenantId,
  scope.subject,
  scope.hospitalId,
];
export function useGovernanceEmploymentTargets(
  scope: GovernanceEmploymentTargetScope,
  input: PersonReviewTargetSearchInput | null,
  cursor: string | null,
  generation: number,
  enabled: boolean,
) {
  return useQuery({
    ...privateRead,
    queryKey: [...governanceEmploymentPreparationKey(scope), "targets", input, cursor, generation],
    queryFn: ({ signal }) => searchGovernanceEmploymentTargets(scope, input!, cursor, signal),
    enabled: input !== null && enabled,
  });
}
export function useGovernanceEmploymentSource(scope: GovernanceEmploymentPreparationScope) {
  return useQuery({
    ...privateRead,
    queryKey: [...governanceEmploymentPreparationKey(scope), scope.staffId, "source"],
    queryFn: ({ signal }) => getGovernanceEmploymentSource(scope, signal),
  });
}
export function useGovernanceEmploymentDrafts(scope: GovernanceEmploymentPreparationScope) {
  return useInfiniteQuery({
    ...privateRead,
    queryKey: [...governanceEmploymentPreparationKey(scope), scope.staffId, "periods"],
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) => listGovernanceEmploymentDrafts(scope, pageParam, signal),
    getNextPageParam: (page) => page.page.next_cursor ?? undefined,
  });
}
export function useGovernanceEmploymentDraft(
  scope: GovernanceEmploymentPreparationScope,
  id: string | undefined,
) {
  return useQuery({
    ...privateRead,
    queryKey: [...governanceEmploymentPreparationKey(scope), scope.staffId, "draft", id],
    queryFn: ({ signal }) => getGovernanceEmploymentDraft(scope, id!, signal),
    enabled: id !== undefined,
  });
}
export function useGovernanceEmploymentSponsors(scope: GovernanceEmploymentPreparationScope) {
  const targetScope = {
    tenantId: scope.tenantId,
    subject: scope.subject,
    hospitalId: scope.hospitalId,
  };
  return useInfiniteQuery({
    ...privateRead,
    queryKey: [...governanceEmploymentPreparationKey(scope), scope.staffId, "sponsors"],
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) =>
      searchGovernanceEmploymentTargets(targetScope, { employee_code: "" }, pageParam, signal),
    getNextPageParam: (page) => page.page.next_cursor ?? undefined,
  });
}
