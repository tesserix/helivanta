"use client";
import { useQuery } from "@tanstack/react-query";
import { POLL_INTERVAL_MS } from "./client";
import {
  listOrganizationHospitals,
  listOrganizationDepartments,
  listOrganizationDepartmentRevisions,
  type OrganizationScope,
} from "./organization-directory";
export function organizationKey(scope: OrganizationScope) {
  return ["organization", scope.tenantId, scope.subject];
}
const options = {
  gcTime: 0,
  staleTime: 0,
  retry: false as const,
  refetchInterval: POLL_INTERVAL_MS,
};
export function useOrganizationHospitals(scope: OrganizationScope, cursor: string | null) {
  return useQuery({
    ...options,
    queryKey: [...organizationKey(scope), "hospitals", cursor],
    queryFn: ({ signal }) => listOrganizationHospitals(scope, cursor, signal),
  });
}
export function useOrganizationDepartments(
  scope: OrganizationScope,
  hospital: string,
  cursor: string | null,
) {
  return useQuery({
    ...options,
    queryKey: [...organizationKey(scope), "departments", hospital, cursor],
    queryFn: ({ signal }) => listOrganizationDepartments(scope, hospital, cursor, signal),
  });
}
export function useOrganizationDepartmentRevisions(
  scope: OrganizationScope,
  hospital: string,
  node: string,
  cursor: string | null,
) {
  return useQuery({
    ...options,
    queryKey: [...organizationKey(scope), "history", hospital, node, cursor],
    queryFn: ({ signal }) =>
      listOrganizationDepartmentRevisions(scope, hospital, node, cursor, signal),
  });
}
