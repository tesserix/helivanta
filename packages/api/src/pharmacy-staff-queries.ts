"use client";
import { useQuery } from "@tanstack/react-query";
import { POLL_INTERVAL_MS } from "./client";
import type { EnrollmentRequestScope } from "./enrollment";
import {
  getPharmacyStaffRecord,
  listPharmacyStaffPage,
  listPharmacyStaffSourcesPage,
} from "./pharmacy-staff";

export function pharmacyStaffKey(scope: EnrollmentRequestScope, pharmacy: string): unknown[] {
  return ["workforce", "pharmacy-staff", scope.tenantId, scope.subject, pharmacy];
}

export function usePharmacyStaffSourcesPage(scope: EnrollmentRequestScope, cursor: string | null) {
  return useQuery({
    queryKey: ["workforce", "pharmacy-staff-sources", scope.tenantId, scope.subject, cursor],
    queryFn: ({ signal }) => listPharmacyStaffSourcesPage(scope, cursor, signal),
    gcTime: 0,
    staleTime: 0,
    retry: false,
    refetchInterval: POLL_INTERVAL_MS,
  });
}

export function usePharmacyStaffPage(
  scope: EnrollmentRequestScope,
  pharmacy: string,
  cursor: string | null,
) {
  return useQuery({
    queryKey: [...pharmacyStaffKey(scope, pharmacy), cursor],
    queryFn: ({ signal }) => listPharmacyStaffPage(scope, pharmacy, cursor, signal),
    gcTime: 0,
    staleTime: 0,
    retry: false,
    refetchInterval: POLL_INTERVAL_MS,
  });
}

/** Reads one record for editing; a new attempt rereads it, and its version guards the save. */
export function usePharmacyStaffRecord(
  scope: EnrollmentRequestScope,
  pharmacy: string,
  id: string,
  attempt: number,
  enabled: boolean,
) {
  return useQuery({
    queryKey: [...pharmacyStaffKey(scope, pharmacy), "record", id, attempt],
    queryFn: ({ signal }) => getPharmacyStaffRecord(scope, pharmacy, id, signal),
    enabled,
    gcTime: 0,
    staleTime: Infinity,
    retry: false,
    refetchOnWindowFocus: false,
  });
}
