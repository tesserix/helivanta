"use client";
import { useQuery } from "@tanstack/react-query";
import { POLL_INTERVAL_MS } from "./client";
import { fetchTaxEvidence, taxEvidenceKey, type TaxEvidenceScope } from "./payroll-evidence";

// Polls only while a file is still being scanned, so approval unblocks without a reload.
export function useTaxEvidence(scope: TaxEvidenceScope) {
  return useQuery({
    queryKey: taxEvidenceKey(scope),
    staleTime: 0,
    queryFn: ({ signal }) => fetchTaxEvidence(scope, signal),
    refetchInterval: (query) =>
      query.state.data?.evidence.some((file) => file.status === "scan_pending")
        ? POLL_INTERVAL_MS
        : false,
  });
}
