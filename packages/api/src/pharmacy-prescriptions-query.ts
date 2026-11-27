"use client";
import { useEffect, useState } from "react";
import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { apiFetch, POLL_INTERVAL_MS } from "./client";
import { pharmacyKey, type PharmacyScope } from "./pharmacy";
import {
  onTenantTransition,
  tenantTransitionPending,
  useTenantTransition,
} from "./tenant-transition";
import {
  prescriptionImageReceipt,
  prescriptionImageSchema,
  prescriptionImagesKey,
  prescriptionImagesRoot,
  readPrescriptionReviewContext,
} from "./pharmacy-prescriptions";

export function usePrescriptionReviewContext(
  scope: PharmacyScope & { subject: string },
  prescriptionId: string,
) {
  const switching = useTenantTransition();
  const [invalidated, setInvalidated] = useState(false);
  const client = useQueryClient();
  const { tenantId, subject, pharmacyId, outletId } = scope;
  const query = useQuery({
    queryKey: pharmacyKey(scope, "review-context", subject, prescriptionId),
    queryFn: ({ signal }) => readPrescriptionReviewContext(scope, prescriptionId, signal),
    enabled: !switching && !invalidated,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnWindowFocus: "always",
  });
  useEffect(
    () =>
      onTenantTransition(() => {
        if (!tenantTransitionPending()) return;
        setInvalidated(true);
        client.removeQueries({
          queryKey: pharmacyKey(
            { tenantId, pharmacyId, outletId },
            "review-context",
            subject,
            prescriptionId,
          ),
          exact: true,
        });
      }),
    [client, tenantId, subject, pharmacyId, outletId, prescriptionId],
  );
  const contextChanged = switching || invalidated;
  return {
    data: contextChanged || query.isFetching || query.isError ? undefined : query.data,
    contextChanged,
    isPending: query.isPending,
    isFetching: query.isFetching,
    isError: query.isError,
    error: query.error,
    refetch: query.refetch,
  };
}

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
