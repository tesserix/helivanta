"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@tesserix/web";
import {
  fetchStocktakeLines,
  readStocktake,
  pharmacyKey,
  stocktakeKey,
  usePagedQuery,
  formatZonedDateTime,
  type PharmacyStocktakeScope,
  type PharmacyStocktakeLine,
} from "@helivanta/api";
import {
  AccountName,
  LoadMore,
  Panel,
  RequestFailure,
  firstError,
  useConfirmLeave,
} from "@helivanta/ui";
import { StockExport } from "./stock-export";
import { StocktakeCountEditor } from "./stocktake-count-editor";
import { StocktakeDecision } from "./stocktake-decision";

const sections = ["H1", "X", "NDPS", "ordinary", "unknown"] as const;
const sectionLabels = {
  H1: "Schedule H1 differences",
  X: "Schedule X differences",
  NDPS: "NDPS differences",
  ordinary: "Other declared differences",
  unknown: "Unknown-classification differences",
};
function section(line: PharmacyStocktakeLine) {
  if (line.ndps_status === "yes") return "NDPS";
  if (line.regulatory_class === "H1" || line.regulatory_class === "X") return line.regulatory_class;
  if (line.regulatory_class === null || line.ndps_status !== "no") return "unknown";
  return "ordinary";
}
const states = {
  counting: "Counting",
  submitted: "Awaiting independent review",
  posted: "Posted",
  rejected: "Rejected",
  cancelled: "Cancelled",
};

export function StocktakeDetail({
  scope,
  takeId,
  timeZone,
  canConfigure,
}: {
  scope: PharmacyStocktakeScope;
  takeId: string;
  timeZone: string;
  canConfigure: boolean;
}) {
  const client = useQueryClient();
  const leave = useConfirmLeave();
  const header = useQuery({
    queryKey: stocktakeKey(scope, "stocktake", takeId),
    queryFn: ({ signal }) => readStocktake(scope, takeId, signal),
    staleTime: 0,
  });
  const lines = usePagedQuery(
    stocktakeKey(scope, "stocktake-lines", takeId),
    (cursor, signal) => fetchStocktakeLines(scope, takeId, cursor, signal),
    { enabled: Boolean(header.data) && !header.error },
  );
  const [entry, setEntry] = useState<PharmacyStocktakeLine | "decision" | null>(null);
  const take = header.data;
  const refresh = () => {
    void client.invalidateQueries({ queryKey: pharmacyKey(scope) });
  };
  const complete = () => {
    setEntry(null);
    refresh();
  };
  if (header.error || lines.error)
    return (
      <RequestFailure
        message={
          header.error
            ? "The stocktake could not be loaded."
            : "Stocktake buckets could not be loaded. Editing is paused."
        }
        error={firstError(header.error, lines.error)}
        retry={refresh}
        recheck="Check stocktake access"
      />
    );
  if (!take) return <p role="status">Loading stocktake…</p>;
  const unavailable = header.isFetching || lines.isRefreshing;
  const editable = canConfigure && take.state === "counting" && !unavailable;
  return (
    <div className="flex flex-col gap-5">
      <Panel aria-label={`Stocktake ${take.number}`}>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="font-semibold">Stocktake {take.number}</h2>
            <p className="text-sm">{states[take.state]}</p>
          </div>
          <Button
            variant="outline"
            onClick={refresh}
            disabled={entry !== null || header.isFetching || lines.isRefreshing}
          >
            Refresh stocktake
          </Button>
        </div>
        <dl className="mt-4 grid gap-3 text-sm sm:grid-cols-2">
          <div>
            <dt className="text-muted-foreground">Started by</dt>
            <dd>
              <AccountName subject={take.started_by} />
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Snapshot</dt>
            <dd>{formatZonedDateTime(take.started_at, timeZone)}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Scope</dt>
            <dd>
              {take.selection === "all"
                ? "All products present at the snapshot"
                : `${take.product_ids.length} selected products`}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Progress</dt>
            <dd>
              {take.counted_rows} of {take.snapshot_rows} buckets counted · {take.difference_rows}{" "}
              differences
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Reason</dt>
            <dd>{take.reason}</dd>
          </div>
        </dl>
        <p className="mt-4 text-sm text-muted-foreground">
          Book quantities are frozen. Later movements remain in the live stock ledger. Uncounted
          buckets are not zero. Extra units must go to quarantine before reviewed posting.
        </p>
        <div className="mt-4">
          <StockExport
            key={`${take.id}:${take.version}`}
            scope={scope}
            stocktake={take}
            disabled={unavailable || entry !== null}
          />
        </div>
      </Panel>
      <Panel aria-label="Frozen stocktake buckets">
        <h3 className="font-semibold">Frozen stocktake buckets</h3>
        {lines.isPending ? (
          <p role="status">Loading counted buckets…</p>
        ) : (
          <>
            <div className="mt-3 overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead>
                  <tr>
                    <th className="p-2">Product / lot</th>
                    <th className="p-2">Location</th>
                    <th className="p-2">Frozen book</th>
                    <th className="p-2">Physical count</th>
                    <th className="p-2">Difference</th>
                    <th className="p-2">Counted by</th>
                    {canConfigure && <th className="p-2">Entry</th>}
                  </tr>
                </thead>
                <tbody>
                  {lines.items.map((line) => (
                    <tr key={line.id} className="border-b">
                      <td className="p-2">
                        {line.brand_name}
                        <span className="block text-muted-foreground">
                          {line.sku} · {line.lot_number} · expires {line.expires_on}
                        </span>
                      </td>
                      <td className="p-2">
                        {line.location_name} · {line.stock_status}
                      </td>
                      <td className="p-2 tabular-nums">
                        {line.book_units} {line.unit_of_issue}
                      </td>
                      <td className="p-2 tabular-nums">
                        {line.counted_units === null
                          ? "Not counted"
                          : `${line.counted_units} ${line.unit_of_issue}`}
                      </td>
                      <td className="p-2 tabular-nums">
                        {line.difference_units === null ? "Not established" : line.difference_units}
                      </td>
                      <td className="p-2">
                        {line.counted_by ? (
                          <AccountName subject={line.counted_by} />
                        ) : (
                          "Not recorded"
                        )}
                      </td>
                      {canConfigure && (
                        <td className="p-2">
                          <Button
                            variant="outline"
                            disabled={!editable || entry !== null}
                            onClick={() => setEntry(line)}
                          >
                            Record count · {line.lot_number}
                          </Button>
                        </td>
                      )}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <LoadMore
              hasMore={lines.hasMore}
              isLoading={lines.isFetchingMore}
              onClick={lines.loadMore}
              label="Load more stocktake buckets"
            />
          </>
        )}
      </Panel>
      {sections.map((key) => {
        const differences = lines.items.filter(
          (line) =>
            line.difference_units !== null && line.difference_units !== 0 && section(line) === key,
        );
        if (!differences.length) return null;
        return (
          <Panel key={key} aria-label={sectionLabels[key]}>
            <h3 className="font-semibold">{sectionLabels[key]}</h3>
            <p className="text-sm text-muted-foreground">
              Loaded differences only. The register download covers the full snapshot.
            </p>
            <ul className="mt-3 space-y-2 text-sm">
              {differences.map((line) => (
                <li key={line.id}>
                  {line.brand_name} · {line.lot_number} · {line.location_name}:{" "}
                  {line.difference_units} {line.unit_of_issue}. Schedule:{" "}
                  {line.regulatory_class ?? "Unknown"}; NDPS: {line.ndps_status}.
                </li>
              ))}
            </ul>
          </Panel>
        );
      })}
      {canConfigure &&
        (take.state === "counting" || take.state === "submitted") &&
        entry === null && (
          <Button variant="outline" disabled={unavailable} onClick={() => setEntry("decision")}>
            Review or close stocktake
          </Button>
        )}
      {entry !== null && (
        <Panel aria-label="Stocktake command">
          <Button variant="outline" onClick={() => leave(() => setEntry(null))}>
            Close entry
          </Button>
          <div className="mt-4">
            {entry === "decision" ? (
              <StocktakeDecision
                scope={scope}
                take={take}
                onSaved={complete}
                paused={unavailable || !canConfigure}
              />
            ) : (
              <StocktakeCountEditor
                scope={scope}
                take={take}
                line={entry}
                onSaved={complete}
                paused={unavailable || !canConfigure}
              />
            )}
          </div>
        </Panel>
      )}
    </div>
  );
}
