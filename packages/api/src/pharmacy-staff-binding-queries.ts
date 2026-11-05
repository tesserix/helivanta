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

export function usePharmacyBindingHospitals(
  scope: StaffActorAccountScope,
  cursor: string | null,
  busy = false,
) {
  return useQuery({
    ...options,
    enabled: !busy,
    refetchOnWindowFocus: !busy,
    refetchOnReconnect: !busy,
    queryKey: [...accountKey(scope), "hospitals", cursor],
    queryFn: ({ signal }) => listStaffActorHospitalChoices(scope, cursor, signal),
  });
}
export function usePharmacyBindingOutlets(
  scope: PharmacyStaffBindingHospitalScope,
  cursor: string | null,
  purpose: "read" | "prepare" | "review" = "read",
  busy = false,
) {
  return useQuery({
    ...options,
    enabled: !busy,
    refetchOnWindowFocus: !busy,
    refetchOnReconnect: !busy,
    queryKey: [...accountKey(scope), scope.hospitalId, "outlets", purpose, cursor],
    queryFn: ({ signal }) => listPharmacyStaffBindingOutletChoices(scope, purpose, cursor, signal),
  });
}
export function usePharmacyBindingDepartments(
  scope: PharmacyStaffBindingHospitalScope,
  cursor: string | null,
  state: { busy?: boolean; enabled?: boolean } = {},
) {
  return useQuery({
    ...options,
    enabled: state.enabled ?? !state.busy,
    refetchOnWindowFocus: !state.busy,
    refetchOnReconnect: !state.busy,
    queryKey: [...accountKey(scope), scope.hospitalId, "departments", cursor],
    queryFn: ({ signal }) => listPostingDepartmentChoices(scope, scope.hospitalId, cursor, signal),
  });
}
export function usePharmacyBindings(
  scope: PharmacyStaffBindingScope,
  cursor: string | null,
  busy = false,
) {
  return useQuery({
    ...options,
    enabled: !busy,
    refetchOnWindowFocus: !busy,
    refetchOnReconnect: !busy,
    queryKey: [...outletKey(scope), "list", cursor],
    queryFn: ({ signal }) => listPharmacyStaffBindings(scope, cursor, signal),
  });
}
export function usePharmacyBinding(
  scope: PharmacyStaffBindingScope,
  binding: string,
  busy = false,
) {
  return useQuery({
    ...options,
    enabled: !busy,
    refetchOnWindowFocus: !busy,
    refetchOnReconnect: !busy,
    queryKey: [...outletKey(scope), "detail", binding],
    queryFn: ({ signal }) => getPharmacyStaffBinding(scope, binding, signal),
  });
}
export function usePharmacyBindingHistory(
  scope: PharmacyStaffBindingScope,
  binding: string,
  cursor: string | null,
  busy = false,
) {
  return useQuery({
    ...options,
    enabled: !busy,
    refetchOnWindowFocus: !busy,
    refetchOnReconnect: !busy,
    queryKey: [...outletKey(scope), "history", binding, cursor],
    queryFn: ({ signal }) => listPharmacyStaffBindingHistory(scope, binding, cursor, signal),
  });
}
