"use client";
import { useQuery } from "@tanstack/react-query";
import type { EnrollmentRequestScope } from "./enrollment";
import { usePagedQuery } from "./paged";
import { getPharmacyEnrollment } from "./pharmacy-enrollment";
import { listPharmacyStaffPage } from "./pharmacy-staff";

export function pharmacyEnrollmentKey(
  scope: EnrollmentRequestScope,
  pharmacy: string,
  id: string,
): unknown[] {
  return [
    "workforce",
    "pharmacy-enrollment",
    scope.tenantId,
    scope.subject,
    pharmacy.toLowerCase(),
    id.toLowerCase(),
  ];
}

/** Reads one enrollment for editing; a new attempt rereads it, and its version guards the save. */
export function usePharmacyEnrollment(
  scope: EnrollmentRequestScope,
  pharmacy: string,
  id: string,
  attempt: number,
) {
  return useQuery({
    queryKey: [...pharmacyEnrollmentKey(scope, pharmacy, id), attempt],
    queryFn: ({ signal }) => getPharmacyEnrollment(scope, pharmacy, id, signal),
    gcTime: 0,
    staleTime: Infinity,
    retry: false,
    refetchOnWindowFocus: false,
  });
}

/** Pages this pharmacy's own Staff directory for the supervisor choice, each page validated as the pharmacy's. */
export function usePharmacySupervisorChoices(scope: EnrollmentRequestScope, pharmacy: string) {
  return usePagedQuery(
    ["workforce", "pharmacy-supervisors", scope.tenantId, scope.subject, pharmacy.toLowerCase()],
    (cursor, signal) => listPharmacyStaffPage(scope, pharmacy, cursor, signal),
    { gcTime: 0, retry: false },
  );
}
