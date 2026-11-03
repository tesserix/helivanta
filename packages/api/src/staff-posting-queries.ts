"use client";
import { useQuery } from "@tanstack/react-query";
import {
  listStaffPostingScopes,
  listStaffPostingTargets,
  listStaffPostingReviewScopes,
} from "./staff-posting-workspace";
import {
  getStaffPostingProposalContext,
  listStaffPostingProposalInbox,
  staffPostingProposalsKey,
  type StaffPostingInboxScope,
  type StaffPostingProposalScope,
} from "./staff-posting-proposals";
import type { StaffPostingProposal } from "./staff-postings-contract";
import { POLL_INTERVAL_MS } from "./client";
import {
  getStaffPostingDraftSource,
  listStaffPostingDrafts,
  listStaffPostingDraftHistory,
  staffPostingsKey,
  type StaffPostingScope,
} from "./staff-postings";
import {
  listOrganizationDepartments,
  listPostingDepartmentChoices,
} from "./organization-directory";
const options = {
  gcTime: 0,
  staleTime: 0,
  retry: false as const,
  refetchInterval: POLL_INTERVAL_MS,
};
export function useStaffPostingDraftSource(scope: StaffPostingScope) {
  return useQuery({
    ...options,
    queryKey: [...staffPostingsKey(scope), "source"],
    queryFn: ({ signal }) => getStaffPostingDraftSource(scope, signal),
  });
}
export function useStaffPostingDrafts(scope: StaffPostingScope, cursor: string | null) {
  return useQuery({
    ...options,
    queryKey: [...staffPostingsKey(scope), "list", cursor],
    queryFn: ({ signal }) => listStaffPostingDrafts(scope, cursor, signal),
  });
}
export function useStaffPostingHistory(
  scope: StaffPostingScope,
  draft: string | null,
  cursor: string | null,
) {
  return useQuery({
    ...options,
    enabled: draft !== null,
    queryKey: [...staffPostingsKey(scope), "history", draft, cursor],
    queryFn: ({ signal }) => listStaffPostingDraftHistory(scope, draft!, cursor, signal),
  });
}
export function useStaffPostingDepartmentChoices(scope: StaffPostingScope, cursor: string | null) {
  return useQuery({
    gcTime: 0,
    staleTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
    queryKey: [...staffPostingsKey(scope), "departments", cursor],
    queryFn: ({ signal }) =>
      scope.access === "scoped"
        ? listPostingDepartmentChoices(scope, scope.hospitalId, cursor, signal)
        : listOrganizationDepartments(scope, scope.hospitalId, cursor, signal),
  });
}

export function useStaffPostingScopes(
  scope: Pick<StaffPostingScope, "tenantId" | "subject">,
  cursor: string | null,
) {
  return useQuery({
    ...options,
    queryKey: ["workforce", "posting-scopes", scope.tenantId, scope.subject, cursor],
    queryFn: ({ signal }) => listStaffPostingScopes(scope, cursor, signal),
  });
}
export function useStaffPostingTargets(
  scope: Pick<StaffPostingScope, "tenantId" | "subject" | "hospitalId">,
  cursor: string | null,
) {
  return useQuery({
    ...options,
    queryKey: [
      "workforce",
      "posting-targets",
      scope.tenantId,
      scope.subject,
      scope.hospitalId,
      cursor,
    ],
    queryFn: ({ signal }) => listStaffPostingTargets(scope, cursor, signal),
  });
}

export function useStaffPostingReviewScopes(
  scope: Pick<StaffPostingScope, "tenantId" | "subject">,
  cursor: string | null,
) {
  return useQuery({
    gcTime: 0,
    staleTime: 0,
    retry: false,
    queryKey: ["workforce", "posting-review-scopes", scope.tenantId, scope.subject, cursor],
    queryFn: ({ signal }) => listStaffPostingReviewScopes(scope, cursor, signal),
  });
}
export function useStaffPostingProposalInbox(
  scope: StaffPostingInboxScope,
  status: StaffPostingProposal["status"],
  cursor: string | null,
) {
  return useQuery({
    gcTime: 0,
    staleTime: 0,
    retry: false,
    queryKey: [
      "workforce",
      "posting-inbox",
      scope.tenantId,
      scope.subject,
      scope.hospitalId,
      status,
      cursor,
    ],
    queryFn: ({ signal }) => listStaffPostingProposalInbox(scope, status, cursor, signal),
  });
}
export function useStaffPostingProposalContext(
  scope: StaffPostingProposalScope,
  proposal: StaffPostingProposal,
  purpose: "review" | "submission",
) {
  return useQuery({
    gcTime: 0,
    staleTime: 0,
    retry: false,
    queryKey: [
      ...staffPostingProposalsKey(scope),
      "context",
      proposal.id,
      proposal.version,
      proposal.revision_id,
      purpose,
    ],
    queryFn: ({ signal }) =>
      getStaffPostingProposalContext(
        scope,
        proposal.id,
        {
          purpose,
          expected_version: proposal.version,
          expected_revision_id: proposal.revision_id,
        },
        signal,
      ),
  });
}
