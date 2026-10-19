"use client";
import { useQuery } from "@tanstack/react-query";
import { POLL_INTERVAL_MS } from "./client";
import type { EvidenceClient, EvidenceScope } from "./workforce-evidence";

// Polls only while a file is still being scanned, so a decision unblocks without a reload.
export function useEvidence(client: EvidenceClient, scope: EvidenceScope) {
  return useQuery({
    queryKey: client.key(scope),
    staleTime: 0,
    queryFn: ({ signal }) => client.fetch(scope, signal),
    refetchInterval: (query) =>
      query.state.data?.evidence.some((file) => file.status === "scan_pending")
        ? POLL_INTERVAL_MS
        : false,
  });
}
