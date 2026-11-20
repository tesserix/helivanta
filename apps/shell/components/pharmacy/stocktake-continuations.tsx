"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@tesserix/web";
import {
  pharmacyKey,
  readStocktake,
  formatZonedDateTime,
  type PharmacyScope,
} from "@helivanta/api";
import { AccountName, Panel, RequestFailure, useConfirmLeave } from "@helivanta/ui";
import { StocktakeList } from "./stocktake-list";
import { StocktakeDecision } from "./stocktake-decision";

export function StocktakeContinuations({
  scope,
  timeZone,
  canCancel,
}: {
  scope: PharmacyScope;
  timeZone: string;
  canCancel: boolean;
}) {
  const client = useQueryClient();
  const leave = useConfirmLeave();
  const [selected, setSelected] = useState<string | null>(null);
  const [cancelling, setCancelling] = useState(false);
  const header = useQuery({
    queryKey: pharmacyKey(scope, "stocktake-continuation", selected),
    queryFn: ({ signal }) => readStocktake(scope, selected!, signal),
    enabled: selected !== null,
    staleTime: 0,
  });
  if (selected === null)
    return (
      <StocktakeList scope={scope} timeZone={timeZone} onOpen={(count) => setSelected(count.id)} />
    );
  const current = header.data;
  return (
    <Panel aria-label="Retained stocktake status">
      <Button
        variant="outline"
        onClick={() =>
          leave(() => {
            setCancelling(false);
            setSelected(null);
          })
        }
      >
        Back to retained stocktakes
      </Button>
      {header.error && (
        <RequestFailure
          message="The retained stocktake status is unavailable. New decisions are paused."
          error={header.error}
          retry={() => void header.refetch()}
        />
      )}
      {!current ? (
        !header.error && <p role="status">Loading retained stocktake…</p>
      ) : (
        <div className="mt-4 space-y-4">
          <h3 className="font-semibold">Stocktake {current.number}</h3>
          <p className="text-sm">
            Started by <AccountName subject={current.started_by} /> ·{" "}
            {formatZonedDateTime(current.started_at, timeZone)}
          </p>
          <p className="text-sm">
            {current.state === "submitted"
              ? "Awaiting independent review"
              : current.state === "counting"
                ? "Counting"
                : current.state === "posted"
                  ? "Posted"
                  : current.state === "cancelled"
                    ? "Cancelled"
                    : "Rejected"}
            . {current.counted_rows} of {current.snapshot_rows} buckets counted.
          </p>
          <p className="text-sm text-muted-foreground">
            This panel retains status and cancellation when Stock is unavailable. It cannot start
            counts, change observations, approve differences or export stock.
          </p>
          {cancelling ? (
            <StocktakeDecision
              scope={scope}
              take={current}
              cancelOnly
              paused={!canCancel || Boolean(header.error) || header.isFetching}
              onSaved={() => {
                setCancelling(false);
                void client.invalidateQueries({ queryKey: pharmacyKey(scope) });
              }}
            />
          ) : (
            canCancel &&
            (current.state === "counting" || current.state === "submitted") && (
              <Button
                variant="outline"
                disabled={Boolean(header.error) || header.isFetching}
                onClick={() => setCancelling(true)}
              >
                Cancel retained stocktake
              </Button>
            )
          )}
        </div>
      )}
    </Panel>
  );
}
