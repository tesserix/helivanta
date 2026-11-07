"use client";
import { useQuery } from "@tanstack/react-query";
import { POLL_INTERVAL_MS } from "./client";
import type { EnrollmentRequestScope } from "./enrollment";
import {
  getSupplierStaffRecord,
  listSupplierStaffPage,
  listSupplierStaffSourcesPage,
} from "./supplier-staff";

export function supplierStaffKey(scope: EnrollmentRequestScope, supplier: string): unknown[] {
  return ["workforce", "supplier-staff", scope.tenantId, scope.subject, supplier];
}

export function useSupplierStaffSourcesPage(scope: EnrollmentRequestScope, cursor: string | null) {
  return useQuery({
    queryKey: ["workforce", "supplier-staff-sources", scope.tenantId, scope.subject, cursor],
    queryFn: ({ signal }) => listSupplierStaffSourcesPage(scope, cursor, signal),
    gcTime: 0,
    staleTime: 0,
    retry: false,
    refetchInterval: POLL_INTERVAL_MS,
  });
}

export function useSupplierStaffPage(
  scope: EnrollmentRequestScope,
  supplier: string,
  cursor: string | null,
) {
  return useQuery({
    queryKey: [...supplierStaffKey(scope, supplier), cursor],
    queryFn: ({ signal }) => listSupplierStaffPage(scope, supplier, cursor, signal),
    gcTime: 0,
    staleTime: 0,
    retry: false,
    refetchInterval: POLL_INTERVAL_MS,
  });
}

/** Reads one record for editing; a new attempt rereads it, and its version guards the save. */
export function useSupplierStaffRecord(
  scope: EnrollmentRequestScope,
  supplier: string,
  id: string,
  attempt: number,
  enabled: boolean,
) {
  return useQuery({
    queryKey: [...supplierStaffKey(scope, supplier), "record", id, attempt],
    queryFn: ({ signal }) => getSupplierStaffRecord(scope, supplier, id, signal),
    enabled,
    gcTime: 0,
    staleTime: Infinity,
    retry: false,
    refetchOnWindowFocus: false,
  });
}
