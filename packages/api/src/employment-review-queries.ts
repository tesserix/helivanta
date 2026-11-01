"use client";
import { listEmploymentPersonFindings } from "./employment-finding-selection";
import { useQuery } from "@tanstack/react-query";
import { listEmploymentReviewScopes } from "./employment-review-scopes";
import {
  listEmploymentReviewInbox,
  type EmploymentReviewHospitalWorkspace,
} from "./employment-review-browse";
import {
  employmentReviewsKey,
  getEmploymentReview,
  listEmploymentReviewHistory,
  type EmploymentReviewScope,
} from "./employment-reviews";
import type { EmploymentReviewSummary } from "./employment-review-contract";
const privateRead = { gcTime: 0, staleTime: 0, retry: false, refetchOnWindowFocus: false } as const;
export function useEmploymentReviewScopes(
  workspace: Pick<EmploymentReviewScope, "tenantId" | "subject">,
  cursor: string | null,
) {
  return useQuery({
    ...privateRead,
    queryKey: [
      "workforce",
      "employment-review-scopes",
      workspace.tenantId,
      workspace.subject,
      cursor,
    ],
    queryFn: ({ signal }) => listEmploymentReviewScopes(workspace, cursor, signal),
  });
}
export function useEmploymentReviewInbox(
  workspace: EmploymentReviewHospitalWorkspace,
  status: EmploymentReviewSummary["status"],
  cursor: string | null,
) {
  return useQuery({
    ...privateRead,
    queryKey: [
      "workforce",
      "employment-review-inbox",
      workspace.tenantId,
      workspace.subject,
      workspace.hospitalId,
      workspace.mode,
      status,
      cursor,
    ],
    queryFn: ({ signal }) => listEmploymentReviewInbox(workspace, status, cursor, signal),
  });
}
export function useEmploymentReviewDetail(scope: EmploymentReviewScope, id: string) {
  return useQuery({
    ...privateRead,
    queryKey: [...employmentReviewsKey(scope), id, "detail"],
    queryFn: ({ signal }) => getEmploymentReview(scope, id, signal),
  });
}
export function useEmploymentReviewHistory(
  scope: EmploymentReviewScope,
  id: string,
  cursor: string | null,
  enabled: boolean,
) {
  return useQuery({
    ...privateRead,
    queryKey: [...employmentReviewsKey(scope), id, "history", cursor],
    enabled,
    queryFn: ({ signal }) => listEmploymentReviewHistory(scope, id, cursor, signal),
  });
}

export function useEmploymentPersonFindings(
  scope: import("./employment-reviews").EmploymentReviewPreparationScope,
  basis: import("./employment-review-contract").EmploymentPersonFindingSearchInput,
  cursor: string | null,
) {
  return useQuery({
    ...privateRead,
    queryKey: [
      ...employmentReviewsKey(scope),
      "person-findings",
      basis.staff_version,
      basis.employment_epoch,
      cursor,
    ],
    queryFn: ({ signal }) => listEmploymentPersonFindings(scope, basis, cursor, signal),
  });
}
