"use client";

import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { apiFetchResponse } from "./client";
import type { Page } from "./paged";
import type { PharmacyScope, PharmacyStockPosition } from "./pharmacy";
import {
  pharmacyStockOverviewFieldsSchema,
  pharmacyStockOverviewValuationFieldsSchema,
} from "./pharmacy-contract";

export type PharmacyStockOverviewScope = Pick<PharmacyScope, "tenantId" | "pharmacyId">;
export type PharmacyStockOverviewPosition = PharmacyStockPosition &
  z.infer<typeof pharmacyStockOverviewFieldsSchema>;
export type PharmacyStockCoverage = "all_accessible" | "restricted_outlets_excluded";

export function pharmacyStockOverviewPath(scope: PharmacyStockOverviewScope) {
  return `/pharmacy/pharmacies/${encodeURIComponent(scope.pharmacyId)}/stock/positions`;
}

/** Coverage comes from the SAME scoped snapshot as this page, including empty pages. */
export function usePharmacyStockOverview(
  scope: PharmacyStockOverviewScope,
  filters: string,
  cursor: string | null,
) {
  return useQuery({
    queryKey: ["pharmacy", scope.tenantId, scope.pharmacyId, "stock-overview", filters, cursor],
    gcTime: 0,
    refetchOnWindowFocus: true,
    queryFn: async ({ signal }) => {
      const query = new URLSearchParams(filters);
      query.set("limit", "25");
      if (cursor) query.set("cursor", cursor);
      const response = await apiFetchResponse(`${pharmacyStockOverviewPath(scope)}?${query}`, {
        headers: { "X-Helivanta-Expected-Tenant": scope.tenantId },
        cache: "no-store",
        signal,
      });
      const coverage = response.headers.get("X-Pharmacy-Stock-Coverage");
      if (coverage !== "all_accessible" && coverage !== "restricted_outlets_excluded")
        throw new Error("The server did not confirm outlet coverage. Reload the view.");
      const page = (await response.json()) as Page<PharmacyStockOverviewPosition>;
      const values = z
        .array(pharmacyStockOverviewValuationFieldsSchema)
        .max(20)
        .parse(JSON.parse(response.headers.get("X-Pharmacy-Stock-Valuation") ?? "null"));
      return { ...page, coverage, values };
    },
  });
}
