"use client";
import { useQuery } from "@tanstack/react-query";
import {
  fetchPharmacyTimeZone,
  pharmacyTimeZoneKey,
  type PharmacyCredentialOwner,
} from "./credentials";

export function usePharmacyTimeZone(owner: PharmacyCredentialOwner) {
  return useQuery({
    queryKey: pharmacyTimeZoneKey(owner),
    queryFn: ({ signal }) => fetchPharmacyTimeZone(owner, signal),
    gcTime: 0,
    refetchOnWindowFocus: false,
  });
}
