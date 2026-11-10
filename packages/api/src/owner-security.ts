"use client";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  CustomerSecurityViewContract,
  CustomerSecurityInputContract,
  type CustomerSecurityView,
} from "@tesserix/onboarding-contracts/customer-signup";
import { apiFetch, POLL_INTERVAL_MS } from "./client";
import { enrollmentRequestOptions, type EnrollmentRequestScope } from "./enrollment";
export type { CustomerSecurityView };

export function useOwnerSecurity(scope: EnrollmentRequestScope) {
  const cache = useQueryClient();
  const key = ["owner-security", scope.tenantId, scope.subject];
  const validate = (value: unknown) => {
    const view = CustomerSecurityViewContract.parse(value);
    if (view.tenantId !== scope.tenantId || view.subject !== scope.subject)
      throw new Error("Workspace security response changed scope.");
    return view;
  };
  const query = useQuery({
    queryKey: key,
    gcTime: 0,
    staleTime: 0,
    retry: false,
    queryFn: async ({ signal }) =>
      validate(
        await apiFetch<unknown>("/iam/me/security", { ...enrollmentRequestOptions(scope), signal }),
      ),
    refetchInterval: (query) => (query.state.data?.state === "pending" ? POLL_INTERVAL_MS : false),
  });
  const reconcile = useMutation({
    mutationFn: async () => {
      if (!query.data) throw new Error("Refresh workspace security first.");
      const input = CustomerSecurityInputContract.parse({
        requestId: crypto.randomUUID(),
        expectedRevision: query.data.revision,
      });
      return validate(
        await apiFetch<unknown>("/iam/me/security", {
          ...enrollmentRequestOptions(scope),
          method: "POST",
          body: JSON.stringify(input),
        }),
      );
    },
    onSuccess: async (view) => {
      cache.setQueryData(key, view);
      await cache.invalidateQueries({ queryKey: key });
    },
  });
  return { query, reconcile };
}
