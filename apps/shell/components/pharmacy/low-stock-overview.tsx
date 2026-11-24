"use client";

import { useQuery } from "@tanstack/react-query";
import {
  formatZonedDateTime,
  pharmacyKey,
  readLowStockOverview,
  type PharmacyScope,
} from "@helivanta/api";
import { GuardedLink, Panel, RefreshButton, RequestFailure } from "@helivanta/ui";

export function LowStockOverview({ scope }: { scope: PharmacyScope }) {
  const overview = useQuery({
    queryKey: pharmacyKey(scope, "low-stock-overview"),
    queryFn: ({ signal }) => readLowStockOverview(scope, signal),
    gcTime: 0,
    staleTime: 15_000,
    refetchOnWindowFocus: true,
    retry: false,
  });
  return (
    <Panel aria-label="Low-stock overview">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="font-semibold">Outlet stock alerts</h2>
        {!overview.isPending && !overview.error && (
          <RefreshButton
            refresh={overview.refetch}
            error={overview.error}
            busy={overview.isFetching}
          >
            Refresh low-stock overview
          </RefreshButton>
        )}
      </div>
      {overview.error ? (
        <RequestFailure
          message="The low-stock count could not be loaded."
          retry={overview.refetch}
          error={overview.error}
        />
      ) : overview.isPending ? (
        <p role="status">Loading low-stock count…</p>
      ) : overview.data ? (
        <div className="mt-3 space-y-2">
          <GuardedLink
            href={`/pharmacy/low-stock?pharmacy=${encodeURIComponent(scope.pharmacyId)}&outlet=${encodeURIComponent(scope.outletId)}`}
            className="flex items-baseline justify-between gap-3 rounded-md border p-3 transition-colors hover:bg-accent"
          >
            <span>Low-stock products</span>
            <span className="text-2xl font-semibold tabular-nums">{overview.data.low_stock}</span>
          </GuardedLink>
          <p className="text-xs text-muted-foreground">
            Observed {formatZonedDateTime(overview.data.observed_at, overview.data.time_zone)} ·{" "}
            {overview.data.time_zone}. Products without a configured minimum are excluded.
          </p>
        </div>
      ) : null}
    </Panel>
  );
}
