"use client";
import { useInfiniteQuery } from "@tanstack/react-query";
import { z } from "zod";
import { apiFetch, POLL_INTERVAL_MS } from "./client";
import type { PharmacyScope } from "./pharmacy";
import {
  prescriptionImageReceipt,
  prescriptionImageSchema,
  prescriptionImagesKey,
  prescriptionImagesRoot,
} from "./pharmacy-prescriptions";

const pageSchema = z.object({
  data: z.array(prescriptionImageSchema),
  page: z.object({ has_more: z.boolean(), next_cursor: z.string().nullable() }),
});

/** A prescription's pages, polled while any is being scanned. */
export function usePrescriptionImages(scope: PharmacyScope, prescriptionId: string) {
  return useInfiniteQuery({
    queryKey: prescriptionImagesKey(scope, prescriptionId),
    initialPageParam: null as string | null,
    staleTime: 0,
    gcTime: 0,
    queryFn: async ({ pageParam, signal }) => {
      const result = pageSchema.parse(
        await apiFetch(
          prescriptionImagesRoot(scope, prescriptionId) +
            (pageParam ? `?cursor=${encodeURIComponent(pageParam)}` : ""),
          { signal, cache: "no-store" },
        ),
      );
      result.data.forEach((image) => prescriptionImageReceipt(scope, prescriptionId, image));
      return result;
    },
    getNextPageParam: (page) => (page.page.has_more ? page.page.next_cursor : undefined),
    refetchInterval: (query) =>
      query.state.data?.pages.some((page) =>
        page.data.some((image) => image.status === "scan_pending"),
      )
        ? POLL_INTERVAL_MS
        : false,
  });
}
