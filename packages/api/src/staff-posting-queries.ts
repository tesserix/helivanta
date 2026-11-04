"use client";
import { useQueries, useQuery } from "@tanstack/react-query";
import {
  listStaffPostingScopes,
  listStaffPostingTargets,
  listStaffPostingReviewScopes,
} from "./staff-posting-workspace";
import {
  getStaffPostingProposalContext,
  getStaffPostingSupervisionContext,
  getStaffPostingManagementReviewContext,
  getStaffPostingReviewSource,
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
export function useStaffPostingDrafts(
  scope: StaffPostingScope,
  cursor: string | null,
  busy = false,
) {
  return useQuery({
    ...options,
    refetchInterval: busy ? false : POLL_INTERVAL_MS,
    refetchOnWindowFocus: !busy,
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
export function useStaffPostingDepartmentChoices(
  scope: StaffPostingScope & { department?: string },
  cursor: string | null,
) {
  return useQuery({
    gcTime: 0,
    staleTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
    queryKey: [...staffPostingsKey(scope), "departments", scope.department ?? null, cursor],
    queryFn: ({ signal }) =>
      scope.access === "scoped"
        ? listPostingDepartmentChoices(scope, scope.hospitalId, cursor, signal, scope.department)
        : listOrganizationDepartments(scope, scope.hospitalId, cursor, signal),
  });
}

// Names come from each covered department's first choices page; an unnamed row keeps its reference.
export function useStaffPostingReviewDepartmentNames(
  scope: StaffPostingInboxScope,
  departments: readonly string[],
) {
  const hints: (string | undefined)[] = departments.length > 0 ? [...departments] : [undefined];
  return useQueries({
    queries: hints.map((department) => ({
      gcTime: 0,
      staleTime: 0,
      retry: false as const,
      refetchOnWindowFocus: false,
      queryKey: [
        "workforce",
        "posting-review-departments",
        scope.tenantId,
        scope.subject,
        scope.hospitalId,
        department ?? null,
      ],
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        listPostingDepartmentChoices(scope, scope.hospitalId, null, signal, department),
    })),
    combine: (results) =>
      new Map(
        results.flatMap(
          (result) => result.data?.data.map((row) => [row.id, row.name] as const) ?? [],
        ),
      ),
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
  busy = false,
) {
  return useQuery({
    ...options,
    refetchInterval: busy ? false : POLL_INTERVAL_MS,
    refetchOnWindowFocus: !busy,
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
  busy = false,
) {
  return useQuery({
    gcTime: 0,
    staleTime: 0,
    retry: false,
    refetchOnWindowFocus: !busy,
    queryKey: ["workforce", "posting-review-scopes", scope.tenantId, scope.subject, cursor],
    queryFn: ({ signal }) => listStaffPostingReviewScopes(scope, cursor, signal),
  });
}
export function useStaffPostingProposalInbox(
  scope: StaffPostingInboxScope,
  status: StaffPostingProposal["status"],
  cursor: string | null,
  busy = false,
) {
  return useQuery({
    gcTime: 0,
    staleTime: 0,
    retry: false,
    refetchOnWindowFocus: !busy,
    queryKey: [
      "workforce",
      "posting-inbox",
      scope.tenantId,
      scope.subject,
      scope.hospitalId,
      scope.department ?? null,
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
  busy = false,
) {
  return useQuery({
    gcTime: 0,
    staleTime: 0,
    retry: false,
    refetchOnWindowFocus: !busy,
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

export function useStaffPostingReviewSource(scope: StaffPostingProposalScope, busy = false) {
  return useQuery({
    gcTime: 0,
    staleTime: 0,
    retry: false,
    refetchOnWindowFocus: !busy,
    queryKey: [...staffPostingProposalsKey(scope), "review-source"],
    queryFn: ({ signal }) => getStaffPostingReviewSource(scope, signal),
  });
}

export function useStaffPostingSupervisionContext(
  scope: StaffPostingProposalScope,
  proposal: StaffPostingProposal,
  purpose: "review" | "submission",
  busy = false,
) {
  return useQuery({
    gcTime: 0,
    staleTime: 0,
    retry: false,
    refetchOnWindowFocus: !busy,
    queryKey: [
      ...staffPostingProposalsKey(scope),
      "supervision-context",
      proposal.id,
      proposal.version,
      proposal.revision_id,
      purpose,
    ],
    queryFn: ({ signal }) =>
      getStaffPostingSupervisionContext(
        scope,
        proposal.id,
        { purpose, expected_version: proposal.version, expected_revision_id: proposal.revision_id },
        signal,
      ),
  });
}

export function useStaffPostingManagementReviewContext(
  scope: StaffPostingProposalScope,
  proposal: StaffPostingProposal,
  enabled: boolean,
  busy = false,
) {
  return useQuery({
    gcTime: 0,
    staleTime: 0,
    retry: false,
    enabled,
    refetchOnWindowFocus: !busy,
    queryKey: [
      ...staffPostingProposalsKey(scope),
      "management-review-context",
      proposal.id,
      proposal.version,
      proposal.revision_id,
      "review",
    ],
    queryFn: ({ signal }) =>
      getStaffPostingManagementReviewContext(
        scope,
        proposal.id,
        {
          purpose: "review",
          expected_version: proposal.version,
          expected_revision_id: proposal.revision_id,
        },
        signal,
      ),
  });
}
