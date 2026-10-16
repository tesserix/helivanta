"use client";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { apiFetch, POLL_INTERVAL_MS } from "./client";
import { type SupplyEvidenceScope } from "./supply-evidence";
import {
  supplyReviewHeaders,
  supplyReviewKey,
  supplyReviewReceipt,
  supplyReviewReceiptSchema,
  supplyReviewRoot,
  supplyReviewStateSchema,
} from "./supply-review";

export function useSupplyReview(scope: SupplyEvidenceScope, enabled: boolean) {
  return useQuery({
    queryKey: supplyReviewKey(scope),
    enabled,
    staleTime: 0,
    gcTime: 0,
    refetchInterval: POLL_INTERVAL_MS,
    queryFn: async ({ signal }) => {
      const saved = supplyReviewStateSchema.parse(
        await apiFetch(supplyReviewRoot(scope).replace(/\/reviews$/, "/review"), {
          signal,
          cache: "no-store",
          headers: supplyReviewHeaders(scope),
        }),
      );
      if (saved.latest) supplyReviewReceipt(scope, saved.latest);
      return saved;
    },
  });
}
const pageSchema = z.object({
  data: z.array(supplyReviewReceiptSchema),
  page: z.object({ has_more: z.boolean(), next_cursor: z.string().nullable() }),
});
export function useSupplyReviewHistory(scope: SupplyEvidenceScope, enabled: boolean) {
  return useInfiniteQuery({
    queryKey: [...supplyReviewKey(scope), "history"],
    enabled,
    staleTime: 0,
    gcTime: 0,
    initialPageParam: null as string | null,
    queryFn: async ({ pageParam, signal }) => {
      const page = pageSchema.parse(
        await apiFetch(
          supplyReviewRoot(scope) + (pageParam ? `?cursor=${encodeURIComponent(pageParam)}` : ""),
          { signal, cache: "no-store", headers: supplyReviewHeaders(scope) },
        ),
      );
      page.data.forEach((row) => supplyReviewReceipt(scope, row));
      return page;
    },
    getNextPageParam: (page) => (page.page.has_more ? page.page.next_cursor : undefined),
  });
}
