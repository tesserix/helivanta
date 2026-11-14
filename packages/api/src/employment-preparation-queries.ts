"use client";

import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import {
  getGovernanceEmploymentSource,
  getGovernanceEmploymentDraft,
  listGovernanceEmploymentDrafts,
  listGovernanceEmploymentHistory,
  searchGovernanceEmploymentTargets,
  type GovernanceEmploymentTargetScope,
  type GovernanceEmploymentPreparationScope,
} from "./employment-preparation";
import type { PersonReviewTargetSearchInput } from "./person-review-contract";

import {
  listGovernanceEmploymentDocuments,
  type GovernanceEmploymentDocumentScope,
} from "./employment-document-preparation";

import {
  listGovernanceEmploymentEvidence,
  listGovernanceEmploymentEvidenceHistory,
  type GovernanceEmploymentBindingScope,
} from "./employment-binding-preparation";

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

export const governanceEmploymentDocumentKey = (scope: GovernanceEmploymentDocumentScope) => [
  ...governanceEmploymentPreparationKey({
    tenantId: scope.tenantId,
    subject: scope.subject,
    hospitalId: scope.hospitalId,
  }),
  scope.staffId,
  "documents",
  scope.staffVersion,
  scope.employmentEpoch,
];
export function useGovernanceEmploymentDocuments(
  scope: GovernanceEmploymentDocumentScope,
  enabled = true,
) {
  return useInfiniteQuery({
    ...privateRead,
    queryKey: governanceEmploymentDocumentKey(scope),
    enabled,
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) => listGovernanceEmploymentDocuments(scope, pageParam, signal),
    getNextPageParam: (page) => page.page.next_cursor ?? undefined,
  });
}

export const governanceEmploymentBindingKey = (scope: GovernanceEmploymentBindingScope) => [
  ...governanceEmploymentDocumentKey(scope),
  "terms-evidence",
  scope.periodId,
  scope.periodRevisionId,
  scope.periodVersion,
  scope.periodStaffVersion,
  scope.periodEmploymentEpoch,
];
export function useGovernanceEmploymentEvidence(scope: GovernanceEmploymentBindingScope) {
  return useInfiniteQuery({
    ...privateRead,
    queryKey: governanceEmploymentBindingKey(scope),
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) => listGovernanceEmploymentEvidence(scope, pageParam, signal),
    getNextPageParam: (page) => page.page.next_cursor ?? undefined,
  });
}
export function useGovernanceEmploymentEvidenceHistory(
  scope: GovernanceEmploymentBindingScope,
  id: string,
) {
  return useInfiniteQuery({
    ...privateRead,
    queryKey: [...governanceEmploymentBindingKey(scope), "history", id],
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) =>
      listGovernanceEmploymentEvidenceHistory(scope, id, pageParam, signal),
    getNextPageParam: (page) => page.page.next_cursor ?? undefined,
  });
}

export function useGovernanceEmploymentHistory(
  scope: GovernanceEmploymentPreparationScope,
  id: string,
) {
  return useInfiniteQuery({
    ...privateRead,
    queryKey: [...governanceEmploymentPreparationKey(scope), scope.staffId, "terms-history", id],
    initialPageParam: null as string | null,
    queryFn: ({ pageParam, signal }) =>
      listGovernanceEmploymentHistory(scope, id, pageParam, signal),
    getNextPageParam: (page) => page.page.next_cursor ?? undefined,
  });
}
