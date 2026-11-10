"use client";
import { useQueryClient } from "@tanstack/react-query";
import { useApiMutation, useApiQuery } from "./hooks";
import {
  CustomerSignupReceiptContract,
  startCustomerSignup,
  sendCustomerLink,
} from "./customer-signup";
const signupKey = ["customer-signup"];
export function useCustomerSignup() {
  const cache = useQueryClient();
  const progress = useApiQuery<unknown>(signupKey, "/auth/signup", {
    poll: (data) => {
      const parsed = CustomerSignupReceiptContract.safeParse(data);
      return (
        parsed.success && ["provisioning", "email_action_required"].includes(parsed.data.state)
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
  const resend = useApiMutation(() => sendCustomerLink("email"), { suppressErrorToast: true });
  return { progress, create, resend };
}
