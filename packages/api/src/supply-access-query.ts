"use client";

import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { supplyKey } from "./supply";
import type { SupplyRegistryScope } from "./supply-registration";

export function useSupplyAccessRefresh({ tenantId, subject, partyId }: SupplyRegistryScope) {
  const client = useQueryClient();
  const clearAuthority = useCallback(() => {
    const identity = { tenantId, subject };
    const access = supplyKey(identity, "party", partyId, "access");
    void client.cancelQueries({ queryKey: access, exact: true }, { revert: false });
    client.setQueryData(access, { roles: [] });
    client.removeQueries({
      queryKey: supplyKey(identity, "party", partyId, "detail"),
      exact: true,
    });
  }, [client, tenantId, subject, partyId]);
  const refreshDirectory = useCallback(() => {
    const identity = { tenantId, subject };
    void client.invalidateQueries({ queryKey: supplyKey(identity, "parties") });
    void client.invalidateQueries({ queryKey: supplyKey(identity, "directory") });
  }, [client, tenantId, subject]);
  const changed = useCallback(
    (grantSubject: string, revoked: boolean) => {
      if (grantSubject === subject && revoked) clearAuthority();
      void client.invalidateQueries({
        queryKey: supplyKey({ tenantId, subject }, "party", partyId),
      });
      refreshDirectory();
    },
    [client, tenantId, subject, partyId, clearAuthority, refreshDirectory],
  );
  const denied = useCallback(() => {
    clearAuthority();
    void client.invalidateQueries({
      queryKey: supplyKey({ tenantId, subject }, "party", partyId, "access"),
      exact: true,
    });
    refreshDirectory();
  }, [client, tenantId, subject, partyId, clearAuthority, refreshDirectory]);
  return { changed, denied };
}
