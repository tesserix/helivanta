"use client";

import { useQuery } from "@tanstack/react-query";
import { listPersonReviewScopes } from "./person-review-scopes";
import {
  listPersonReviewInbox,
  searchPersonReviewTargets,
  type PersonReviewHospitalWorkspace,
} from "./person-review-browse";
import { getPersonReview, listPersonReviewHistory, type PersonReviewScope } from "./person-reviews";
import {
  searchPersonReviewEvidence,
  getPersonReviewPreparationSource,
} from "./person-review-selection";
import type {
  PersonReviewEvidenceSelectionInput,
  PersonReviewSummary,
} from "./person-review-contract";

const privateRead = { gcTime: 0, staleTime: 0, retry: false, refetchOnWindowFocus: false } as const;
export function usePersonReviewScopes(
  workspace: Pick<PersonReviewScope, "tenantId" | "subject">,
  cursor: string | null,
) {
  return useQuery({
    ...privateRead,
    queryKey: ["workforce", "person-review-scopes", workspace.tenantId, workspace.subject, cursor],
    queryFn: ({ signal }) => listPersonReviewScopes(workspace, cursor, signal),
  });
}

export function usePersonReviewInbox(
  workspace: PersonReviewHospitalWorkspace,
  status: PersonReviewSummary["status"],
  cursor: string | null,
) {
  return useQuery({
    ...privateRead,
    queryKey: [
      "workforce",
      "person-review-inbox",
      workspace.tenantId,
      workspace.subject,
      workspace.hospitalId,
      status,
      cursor,
    ],
    queryFn: ({ signal }) => listPersonReviewInbox(workspace, status, cursor, signal),
  });
}

export function usePersonReviewDetail(scope: PersonReviewScope, reviewId: string) {
  return useQuery({
    ...privateRead,
    queryKey: [
      "workforce",
      "person-review-detail",
      scope.tenantId,
      scope.subject,
      scope.hospitalId,
      scope.staffId,
      reviewId,
    ],
    queryFn: ({ signal }) => getPersonReview(scope, reviewId, signal),
  });
}

export function usePersonReviewTargets(
  workspace: PersonReviewHospitalWorkspace,
  employeeCode: string,
  cursor: string | null,
) {
  return useQuery({
    ...privateRead,
    queryKey: [
      "workforce",
      "person-review-targets",
      workspace.tenantId,
      workspace.subject,
      workspace.hospitalId,
      employeeCode,
      cursor,
    ],
    queryFn: ({ signal }) => searchPersonReviewTargets(workspace, employeeCode, cursor, signal),
  });
}
export function usePersonReviewPreparationSource(scope: PersonReviewScope) {
  return useQuery({
    ...privateRead,
    queryKey: [
      "workforce",
      "person-review-preparation-source",
      scope.tenantId,
      scope.subject,
      scope.hospitalId,
      scope.staffId,
    ],
    queryFn: ({ signal }) => getPersonReviewPreparationSource(scope, signal),
  });
}
export function usePersonReviewEvidenceOptions(
  scope: PersonReviewScope,
  source: PersonReviewEvidenceSelectionInput,
  cursor: string | null,
) {
  return useQuery({
    ...privateRead,
    queryKey: [
      "workforce",
      "person-review-evidence-selection",
      scope.tenantId,
      scope.subject,
      scope.hospitalId,
      scope.staffId,
      source.staff_version,
      source.employment_epoch,
      source.identity_hash,
      cursor,
    ],
    queryFn: ({ signal }) => searchPersonReviewEvidence(scope, source, cursor, signal),
  });
}

export function usePersonReviewHistory(
  scope: PersonReviewScope,
  reviewId: string,
  cursor: string | null,
  enabled: boolean,
) {
  return useQuery({
    ...privateRead,
    queryKey: [
      "workforce",
      "person-review-history",
      scope.tenantId,
      scope.subject,
      scope.hospitalId,
      scope.staffId,
      reviewId,
      cursor,
    ],
    enabled,
    queryFn: ({ signal }) => listPersonReviewHistory(scope, reviewId, cursor, signal),
  });
}
