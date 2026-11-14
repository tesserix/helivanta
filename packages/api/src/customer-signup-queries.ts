"use client";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useApiMutation, useApiQuery } from "./hooks";
import {
  CustomerSignupReceiptContract,
  startCustomerSignup,
  getCustomerBusinessProfile,
  sendCustomerLink,
  newCustomerSignupReceipt,
} from "./customer-signup";
const signupKey = ["customer-signup"];
export function useCustomerSignup() {
  const cache = useQueryClient();
  const begin = useApiMutation(
    async () => {
      await cache.cancelQueries({ queryKey: signupKey });
      return newCustomerSignupReceipt();
    },
    {
      suppressErrorToast: true,
      onSuccess: async (receipt) => {
        await cache.cancelQueries({ queryKey: signupKey });
        cache.setQueryData(signupKey, receipt);
      },
    },
  );
  const progress = useApiQuery<unknown>(signupKey, "/auth/signup", {
    enabled: !begin.isPending,
    poll: (data) => {
      const parsed = CustomerSignupReceiptContract.safeParse(data);
      return (
        parsed.success &&
        ["provisioning", "email_action_required", "password_action_required"].includes(
          parsed.data.state,
        )
      );
    },
    gcTime: 0,
    staleTime: 0,
    request: { cache: "no-store" },
  });
  const create = useApiMutation(startCustomerSignup, {
    suppressErrorToast: true,
    onSuccess: (receipt) => cache.setQueryData(signupKey, receipt),
  });
  const resend = useApiMutation(
    () => {
      const current = CustomerSignupReceiptContract.safeParse(progress.data);
      return sendCustomerLink(
        current.success && current.data.state === "password_action_required" ? "password" : "email",
      );
    },
    { suppressErrorToast: true },
  );
  return { progress, create, resend, begin };
}

export function useCustomerBusinessProfile(
  tenantId: string,
  kind: "hospital" | "pharmacy",
  resourceId: string,
  enabled: boolean,
) {
  return useQuery({
    queryKey: ["customer-business-profile", tenantId, kind, resourceId],
    queryFn: ({ signal }) => getCustomerBusinessProfile(tenantId, kind, resourceId, signal),
    enabled,
    gcTime: 0,
    staleTime: 0,
  });
}
