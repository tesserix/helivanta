"use client";

import { useQuery } from "@tanstack/react-query";
import { listStaffIntakeScopes } from "./staff-directory";
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
