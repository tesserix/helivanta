"use client";

import { useQuery } from "@tanstack/react-query";
import {
  listStaffIntakeScopes,
  listStaffDirectoryPage,
  listStaffHospitalsPage,
} from "./staff-directory";
import { POLL_INTERVAL_MS } from "./client";
import { staffIntakeScopeKey } from "./enrollment";
import type { EnrollmentRequestScope } from "./enrollment";

export function useStaffIntakeScopes(scope: EnrollmentRequestScope, cursor: string | null) {
  return useQuery({
    queryKey: ["workforce", "hr-intake-scopes", scope.tenantId, scope.subject, cursor],
    queryFn: ({ signal }) => listStaffIntakeScopes(scope, cursor, signal),
    gcTime: 0,
    staleTime: 0,
    retry: false,
  });
}

export function useStaffDirectoryPage(
  scope: EnrollmentRequestScope,
  hospitalId: string,
  cursor: string | null,
) {
  return useQuery({
    queryKey: [
      "workforce",
      "canonical-staff-directory",
      scope.tenantId,
      scope.subject,
      staffIntakeScopeKey(scope),
      hospitalId,
      cursor,
    ],
    queryFn: ({ signal }) => listStaffDirectoryPage(scope, hospitalId, cursor, signal),
    gcTime: 0,
    staleTime: 0,
    retry: false,
    refetchInterval: POLL_INTERVAL_MS,
  });
}

export function useStaffHospitalsPage(scope: EnrollmentRequestScope, cursor: string | null) {
  return useQuery({
    queryKey: ["workforce", "canonical-staff-hospitals", scope.tenantId, scope.subject, cursor],
    queryFn: ({ signal }) => listStaffHospitalsPage(scope, cursor, signal),
    gcTime: 0,
    staleTime: 0,
    retry: false,
    refetchInterval: POLL_INTERVAL_MS,
  });
}
