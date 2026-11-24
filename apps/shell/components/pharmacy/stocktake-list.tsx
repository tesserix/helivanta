"use client";

import { Button } from "@tesserix/web";
import {
  fetchStocktakes,
  formatZonedDateTime,
  stocktakeKey,
  usePagedQuery,
  type PharmacyStocktakeScope,
  type PharmacyStocktakeSummary,
} from "@helivanta/api";
import {
  AccountName,
  GuardedLink,
  LoadMore,
  Panel,
  RefreshButton,
  RequestFailure,
  useAccountPresentations,
} from "@helivanta/ui";

const stateLabels: Record<PharmacyStocktakeSummary["state"], string> = {
  counting: "Counting",
  submitted: "Awaiting independent review",
  posted: "Posted",
  rejected: "Rejected",
  cancelled: "Cancelled",
};

export function StocktakeList({
  scope,
  timeZone,
  onStart,
  onOpen,
  retainedOnly = onOpen !== undefined,
}: {
  scope: PharmacyStocktakeScope;
  timeZone: string;
  onStart?: () => void;
  onOpen?: (count: PharmacyStocktakeSummary) => void;
  retainedOnly?: boolean;
}) {
  const counts = usePagedQuery(stocktakeKey(scope, "stocktakes"), (cursor, signal) =>
    fetchStocktakes(scope, cursor, signal),
  );
  const people = useAccountPresentations(counts.items.map((count) => count.started_by));
  const open = counts.items.some(
    (count) =>
      count.source_invalidated !== true &&
      (count.state === "counting" || count.state === "submitted"),
  );
  const root = `/pharmacy/stock?pharmacy=${encodeURIComponent(scope.pharmacyId)}&outlet=${encodeURIComponent(scope.outletId)}&view=counts`;
  if (retainedOnly && !counts.isPending && !counts.error && counts.items.length === 0) return null;
  return (
    <Panel aria-label="Stocktakes">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="font-semibold">{retainedOnly ? "Retained stocktakes" : "Stocktakes"}</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            {retainedOnly
              ? "Inspect retained count status or cancel abandoned work. Starting, counting and approval require the Stock workspace."
              : "Freeze the book, record physical counts, then have an independent reviewer post the differences."}
          </p>
        </div>
        <div className="flex gap-2">
          <RefreshButton refresh={counts.retry} error={counts.error} busy={counts.isRefreshing}>
            Refresh stocktakes
          </RefreshButton>
          {onStart && (
            <Button
              onClick={onStart}
              disabled={counts.isPending || counts.isRefreshing || Boolean(counts.error) || open}
            >
              Start stocktake
            </Button>
          )}
        </div>
      </div>
      {counts.error ? (
        <RequestFailure
          message="Stocktakes could not be loaded."
          retry={counts.retry}
          error={counts.error}
        />
      ) : counts.isPending ? (
        <p role="status" className="mt-4">
          Loading stocktakes…
        </p>
      ) : counts.items.length === 0 ? (
        <p className="mt-4 text-sm">
          {retainedOnly
            ? "No retained stocktakes for this outlet."
            : "No stocktakes yet. Start a count when this outlet is ready."}
        </p>
      ) : (
        <>
          {open && (
            <p role="status" className="mt-4 text-sm">
              An open stocktake must be posted, rejected or cancelled before another can start.
            </p>
          )}
          <div className="mt-4 overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead>
                <tr className="border-b">
                  <th className="p-2">Reference</th>
                  <th className="p-2">State</th>
                  <th className="p-2">Started by</th>
                  <th className="p-2">Snapshot</th>
                  <th className="p-2">Frozen lines</th>
                  <th className="p-2">Inspect</th>
                </tr>
              </thead>
              <tbody>
                {counts.items.map((count) => (
                  <tr key={count.id} className="border-b">
                    <td className="p-2 font-medium">Stocktake {count.number}</td>
                    <td className="p-2">
                      {count.source_invalidated === true
                        ? "Source changed"
                        : stateLabels[count.state]}
                    </td>
                    <td className="p-2">
                      <AccountName subject={count.started_by} />
                      {people.get(count.started_by)?.available && (
                        <span
                          className="block text-xs text-muted-foreground"
                          aria-label="Current account roles"
                        >
                          {people
                            .get(count.started_by)
                            ?.roles.map((role) => role.label)
                            .join(", ")}
                        </span>
                      )}
                    </td>
                    <td className="p-2">{formatZonedDateTime(count.started_at, timeZone)}</td>
                    <td className="p-2 tabular-nums">{count.snapshot_rows}</td>
                    <td className="p-2">
                      {onOpen ? (
                        <Button variant="outline" onClick={() => onOpen(count)}>
                          {retainedOnly ? "Inspect" : "Open"} stocktake {count.number}
                        </Button>
                      ) : (
                        <GuardedLink
                          href={`${root}&count=${encodeURIComponent(count.id)}`}
                          className="underline underline-offset-4"
                        >
                          Open stocktake {count.number}
                        </GuardedLink>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
      <LoadMore
        hasMore={counts.hasMore}
        isLoading={counts.isFetchingMore}
        onClick={counts.loadMore}
        label="Load more stocktakes"
      />
    </Panel>
  );
}
