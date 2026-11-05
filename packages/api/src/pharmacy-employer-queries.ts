"use client";
import { useQuery } from "@tanstack/react-query";
import { getPharmacyEmployer, pharmacyEmployerKey } from "./pharmacy-employers";

export function usePharmacyEmployer(tenantId: string, pharmacy: string, enabled: boolean) {
  return useQuery({
    gcTime: 0,
    staleTime: 0,
    retry: false,
    enabled,
    queryKey: pharmacyEmployerKey(tenantId, pharmacy),
    queryFn: ({ signal }) => getPharmacyEmployer(pharmacy, signal),
  });
}
