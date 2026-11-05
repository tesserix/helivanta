"use client";

import { useQuery } from "@tanstack/react-query";
import { listStaffActorHospitalChoices, type StaffActorAccountScope } from "./staff-actor-scopes";
import { listPostingDepartmentChoices } from "./organization-directory";
import {
  getPharmacyStaffBinding,
  listPharmacyStaffBindings,
  listPharmacyStaffBindingHistory,
  listPharmacyStaffBindingOutletChoices,
  type PharmacyStaffBindingScope,
  type PharmacyStaffBindingHospitalScope,
} from "./pharmacy-staff-bindings";

const options = { gcTime: 0, staleTime: 0, retry: false, refetchOnWindowFocus: true } as const;
const accountKey = (scope: StaffActorAccountScope) => [
  "pharmacy",
  "staff-relationships",
  scope.tenantId,
  scope.subject,
];
const outletKey = (scope: PharmacyStaffBindingScope) => [
  ...accountKey(scope),
  scope.hospitalId,
  scope.pharmacyId,
  scope.outletId,
];

export function usePharmacyBindingHospitals(scope: StaffActorAccountScope, cursor: string | null) {
  return useQuery({
    ...options,
    queryKey: [...accountKey(scope), "hospitals", cursor],
    queryFn: ({ signal }) => listStaffActorHospitalChoices(scope, cursor, signal),
  });
}
export function usePharmacyBindingOutlets(
  scope: PharmacyStaffBindingHospitalScope,
  cursor: string | null,
) {
  return useQuery({
    ...options,
    queryKey: [...accountKey(scope), scope.hospitalId, "outlets", "read", cursor],
    queryFn: ({ signal }) => listPharmacyStaffBindingOutletChoices(scope, "read", cursor, signal),
  });
}
export function usePharmacyBindingDepartments(
  scope: PharmacyStaffBindingHospitalScope,
  cursor: string | null,
) {
  return useQuery({
    ...options,
    queryKey: [...accountKey(scope), scope.hospitalId, "departments", cursor],
    queryFn: ({ signal }) => listPostingDepartmentChoices(scope, scope.hospitalId, cursor, signal),
  });
}
export function usePharmacyBindings(scope: PharmacyStaffBindingScope, cursor: string | null) {
  return useQuery({
    ...options,
    queryKey: [...outletKey(scope), "list", cursor],
    queryFn: ({ signal }) => listPharmacyStaffBindings(scope, cursor, signal),
  });
}
export function usePharmacyBinding(scope: PharmacyStaffBindingScope, binding: string) {
  return useQuery({
    ...options,
    queryKey: [...outletKey(scope), "detail", binding],
    queryFn: ({ signal }) => getPharmacyStaffBinding(scope, binding, signal),
  });
}
export function usePharmacyBindingHistory(
  scope: PharmacyStaffBindingScope,
  binding: string,
  cursor: string | null,
) {
  return useQuery({
    ...options,
    queryKey: [...outletKey(scope), "history", binding, cursor],
    queryFn: ({ signal }) => listPharmacyStaffBindingHistory(scope, binding, cursor, signal),
  });
}
