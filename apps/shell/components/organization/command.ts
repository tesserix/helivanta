"use client";
import { useEffect, useRef } from "react";
import {
  newOrganizationId,
  onTenantTransition,
  organizationKey,
  tenantTransitionPending,
  useApiMutation,
  type OrganizationScope,
} from "@helivanta/api";
export function useOrganizationCommand<T, R>(
  scope: OrganizationScope,
  send: (input: T, id: string, signal: AbortSignal) => Promise<R>,
  saved: (result: R) => void,
) {
  const command = useRef<{ fingerprint: string; id: string } | null>(null);
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
      const fingerprint = JSON.stringify(input);
      if (!command.current || command.current.fingerprint !== fingerprint)
        command.current = { fingerprint, id: newOrganizationId() };
      const controller = new AbortController();
      active.current = controller;
      const result = await send(input, command.current.id, controller.signal);
      controller.signal.throwIfAborted();
      return result;
    },
    {
      onSuccess: saved,
      suppressErrorToast: true,
      invalidate: [organizationKey(scope)],
      invalidateOnError: true,
    },
  );
}
