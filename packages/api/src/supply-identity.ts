"use client";

import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "./client";
import { PartyProofContract } from "./organization-identity-contract";
import { supplyHeaders, supplyKey } from "./supply";
import type { SupplyRegistryScope } from "./supply-registration";

export function useSupplyPlatformIdentity(scope: SupplyRegistryScope, partyVersion: number) {
  return useQuery({
    queryKey: supplyKey(scope, "party", scope.partyId, "platform-identity", partyVersion),
    enabled: false,
    staleTime: 0,
    gcTime: 0,
    retry: false,
    queryFn: async ({ signal }) => {
      const parsed = PartyProofContract.safeParse(
        await apiFetch(`/supply/parties/${encodeURIComponent(scope.partyId)}/platform-identity`, {
          signal,
          cache: "no-store",
          headers: supplyHeaders(scope),
        }),
      );
      if (
        !parsed.success ||
        parsed.data.party_id !== scope.partyId ||
        parsed.data.party_version !== partyVersion ||
        parsed.data.identity.tenant_id !== scope.tenantId ||
        parsed.data.identity.state !== "active" ||
        parsed.data.trading_approved
      )
        throw new Error(
          "The organisation or workspace changed. Refresh the profile and try again.",
        );
      return parsed.data;
    },
  });
}
