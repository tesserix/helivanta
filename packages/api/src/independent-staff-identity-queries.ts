"use client";
import { useQuery } from "@tanstack/react-query";
import { POLL_INTERVAL_MS } from "./client";
import {
  listIndependentStaffLinksPage,
  type IndependentStaffScope,
} from "./independent-staff-identity";
export const independentStaffLinksKey = (scope: IndependentStaffScope) => [
  "workforce",
  "independent-links",
  scope.tenantId,
  scope.subject,
  scope.employerKind,
  scope.employerId,
  scope.staffId,
];
export function useIndependentStaffLinksPage(scope: IndependentStaffScope, cursor: string | null) {
  return useQuery({
    queryKey: [...independentStaffLinksKey(scope), cursor],
    queryFn: ({ signal }) => listIndependentStaffLinksPage(scope, cursor, signal),
    gcTime: 0,
    staleTime: 0,
    retry: false,
    refetchInterval: POLL_INTERVAL_MS,
  });
}
