"use client";

import {
  supplyAccessLimitsInputSchema,
  supplyHeaders,
  supplyKey,
  useApiQuery,
  type SupplyRegistryScope,
} from "@helivanta/api";
import type { z } from "zod";
import { RequestFailure } from "../request-failure";

const limitsSchema = supplyAccessLimitsInputSchema
  .pick({ max_grant_hours: true, max_support_hours: true })
  .strip();

export function useAccessWindowPolicy(scope: SupplyRegistryScope, enabled = true) {
  const query = useApiQuery<unknown>(
    supplyKey(scope, "party", scope.partyId, "access-limits"),
    `/supply/parties/${scope.partyId}/access-limits`,
    {
      enabled,
      gcTime: 0,
      refetchOnWindowFocus: true,
      request: { headers: supplyHeaders(scope), cache: "no-store" },
    },
  );
  const parsed = limitsSchema.safeParse(query.data);
  return {
    data: !query.isError && parsed.success ? parsed.data : null,
    ready: !query.isFetching && !query.isError && parsed.success,
    pending: query.isPending || query.isFetching,
    refetch: query.refetch,
  };
}

export function AccessWindowPolicyStatus({
  policy,
}: {
  policy: ReturnType<typeof useAccessWindowPolicy>;
}) {
  if (policy.pending) return <p role="status">Checking current access duration limits…</p>;
  if (!policy.data)
    return (
      <RequestFailure
        message="Current access duration limits are unavailable. Retry before granting access."
        retry={policy.refetch}
      />
    );
  return null;
}

export function accessWindowLabel(hours: number) {
  return `${hours} ${hours === 1 ? "hour" : "hours"}`;
}

export function validateAccessWindow(
  value: { valid_from: string; expires_at: string },
  hours: number | undefined,
  ctx: z.RefinementCtx,
  path = "expires_at",
) {
  if (hours && Date.parse(value.expires_at) - Date.parse(value.valid_from) > hours * 3_600_000)
    ctx.addIssue({
      code: "custom",
      path: [path],
      message: `Access window must not exceed ${accessWindowLabel(hours)}.`,
    });
}
