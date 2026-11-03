"use client";
import { useQuery } from "@tanstack/react-query";
import { listStaffPostingScopes, listStaffPostingTargets } from "./staff-posting-workspace";
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
