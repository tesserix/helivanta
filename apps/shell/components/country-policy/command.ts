"use client";

import { useEffect, useRef } from "react";
import {
  countryPolicyKey,
  newCountryPolicyId,
  onTenantTransition,
  tenantTransitionPending,
  useApiMutation,
  type CountryPolicyIdentity,
  type CountryPolicyReceipt,
} from "@helivanta/api";

export function useCountryPolicyCommand<T extends { id: string }>(
  identity: CountryPolicyIdentity,
  send: (input: T, signal: AbortSignal) => Promise<CountryPolicyReceipt>,
  successToast: string,
  onSaved: (receipt: CountryPolicyReceipt) => void,
) {
  const command = useRef<{ fingerprint: string; input: T } | null>(null);
  const active = useRef<AbortController | null>(null);
  useEffect(() => {
    const unsubscribe = onTenantTransition(() => {
      if (tenantTransitionPending()) active.current?.abort();
    });
    return () => {
      unsubscribe();
      active.current?.abort();
    };
  }, []);
  return useApiMutation(
    async (input: T) => {
      if (tenantTransitionPending())
        throw new Error("Wait for your workspace to finish switching.");
      const fingerprint = JSON.stringify({ ...input, id: "" });
      if (!command.current || command.current.fingerprint !== fingerprint)
        command.current = { fingerprint, input: { ...input, id: newCountryPolicyId() } };
      const controller = new AbortController();
      active.current = controller;
      const receipt = await send(command.current.input, controller.signal);
      controller.signal.throwIfAborted();
      return receipt;
    },
    {
      invalidate: [countryPolicyKey(identity)],
      successToast,
      suppressErrorToast: true,
      onSuccess: onSaved,
    },
  );
}
