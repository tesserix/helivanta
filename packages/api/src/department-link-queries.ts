"use client";
import { useQuery } from "@tanstack/react-query";
import { POLL_INTERVAL_MS } from "./client";
import { listDepartmentLinks } from "./department-links";
import { listOrganizationDepartments, type OrganizationScope } from "./organization-directory";

const options = {
  gcTime: 0,
  staleTime: 0,
  retry: false as const,
  refetchInterval: POLL_INTERVAL_MS,
};
export function useDepartmentLinkHistory(
  scope: OrganizationScope,
  hospital: string,
  master: string,
  cursor: string | null,
) {
  return useQuery({
    ...options,
    queryKey: ["department-handoff", scope.tenantId, scope.subject, hospital, master, cursor],
    queryFn: ({ signal }) => listDepartmentLinks(scope, hospital, master, cursor, signal),
  });
}
export function useDepartmentLinkChoices(
  scope: OrganizationScope,
  hospital: string,
  cursor: string | null,
  enabled: boolean,
) {
  return useQuery({
    ...options,
    queryKey: ["organization", scope.tenantId, scope.subject, "departments", hospital, cursor],
    queryFn: ({ signal }) => listOrganizationDepartments(scope, hospital, cursor, signal),
    enabled,
  });
}
