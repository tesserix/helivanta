"use client";
import { useInfiniteQuery } from "@tanstack/react-query";
import { z } from "zod";
import { apiFetch, POLL_INTERVAL_MS } from "./client";
import {
  supplyEvidenceSchema,
  supplyEvidenceRoot,
  supplyEvidenceKey,
  supplyEvidenceHeaders,
  supplyEvidenceReceipt,
  type SupplyEvidenceScope,
} from "./supply-evidence";
const pageSchema = z.object({
  data: z.array(supplyEvidenceSchema),
  page: z.object({ has_more: z.boolean(), next_cursor: z.string().nullable() }),
});
export function useSupplyEvidence(scope: SupplyEvidenceScope, enabled: boolean) {
  return useInfiniteQuery({
    queryKey: supplyEvidenceKey(scope),
    enabled,
    initialPageParam: null as string | null,
    staleTime: 0,
    gcTime: 0,
    queryFn: async ({ pageParam, signal }) => {
      const result = pageSchema.parse(
        await apiFetch(
          supplyEvidenceRoot(scope) + (pageParam ? `?cursor=${encodeURIComponent(pageParam)}` : ""),
          { signal, cache: "no-store", headers: supplyEvidenceHeaders(scope) },
        ),
      );
      result.data.forEach((file) => supplyEvidenceReceipt(scope, file));
      return result;
    },
    getNextPageParam: (page) => (page.page.has_more ? page.page.next_cursor : undefined),
    refetchInterval: (query) =>
      query.state.data?.pages.some((page) =>
        page.data.some(
          (file) =>
            file.status === "scan_pending" ||
            (file.status === "scan_error" && Date.now() - Date.parse(file.updated_at) < 5 * 60_000),
        ),
      )
        ? POLL_INTERVAL_MS
        : false,
  });
}
