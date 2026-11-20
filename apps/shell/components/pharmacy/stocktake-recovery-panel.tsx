"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button } from "@tesserix/web";
import {
  ApiError,
  pharmacyKey,
  readStocktake,
  readStocktakeCount,
  readStocktakeDecision,
  formatZonedDateTime,
  type PharmacyStocktakeScope,
} from "@helivanta/api";
import { AccountName, Panel, RequestFailure } from "@helivanta/ui";
import {
  forgetStocktakeCommand,
  StocktakeRecoveryError,
  useStocktakeRecovery,
  type StocktakeRecoveryMarker,
} from "./stocktake-recovery";

export function StocktakeRecoveryPanel({
  scope,
  timeZone,
}: {
  scope: PharmacyStocktakeScope;
  timeZone: string;
}) {
  const recovery = useStocktakeRecovery(scope);
  if (recovery.unavailable)
    return (
      <Panel aria-label="Stocktake recovery unavailable">
        <p role="alert">
          Recovery references could not be loaded. New stocktake commands are paused until browser
          storage is available.
        </p>
        <Button variant="outline" onClick={recovery.refresh}>
          Check browser recovery storage
        </Button>
      </Panel>
    );
  if (!recovery.markers.length) return null;
  return (
    <Panel aria-label="Pending stocktake outcomes">
      <h2 className="font-semibold">Pending stocktake outcomes</h2>
      <p className="mt-1 text-sm text-muted-foreground">
        Check the original saved result before continuing. This panel reads retained evidence and
        sends no new stock commands.
      </p>
      <div className="mt-4 space-y-4">
        {recovery.markers.map((marker, index) => (
          <RecoveryResult
            key={`${scope.subject}:${marker.kind}:${marker.stocktakeId}:${marker.commandId}`}
            scope={scope}
            timeZone={timeZone}
            marker={marker}
            number={index + 1}
          />
        ))}
      </div>
    </Panel>
  );
}

async function originalResult(scope: PharmacyStocktakeScope, marker: StocktakeRecoveryMarker) {
  if (marker.kind === "start") {
    const value = await readStocktake(scope, marker.stocktakeId);
    if (value.started_by !== scope.subject || value.review_relationship !== "starter")
      throw new Error("The original starter does not match this account.");
    const states = {
      counting: "Counting",
      submitted: "Awaiting independent review",
      posted: "Posted",
      cancelled: "Cancelled",
      rejected: "Rejected",
    };
    return {
      message: `The original start exists as Stocktake ${value.number}. Current status: ${states[value.state]}.`,
      subject: value.started_by,
      at: value.started_at,
    };
  }
  if (marker.kind === "count") {
    const value = await readStocktakeCount(scope, marker.stocktakeId, marker.commandId);
    if (value.recorded_by !== scope.subject)
      throw new Error("The original counter does not match this account.");
    return {
      message: `Original physical count was recorded for ${value.counts.length} bucket observations.`,
      subject: value.recorded_by,
      at: value.recorded_at,
    };
  }
  const value = await readStocktakeDecision(scope, marker.stocktakeId, marker.commandId);
  if (value.recorded_by !== scope.subject)
    throw new Error("The original decision does not match this account.");
  const decisions = {
    submit: "Submitted for independent review",
    approve: "Approved and posted differences",
    reject: "Rejected",
    cancel: "Cancelled",
  };
  return {
    message: `Original decision was recorded: ${decisions[value.decision]}.`,
    subject: value.recorded_by,
    at: value.recorded_at,
  };
}

function RecoveryResult({
  scope,
  marker,
  timeZone,
  number,
}: {
  scope: PharmacyStocktakeScope;
  marker: StocktakeRecoveryMarker;
  timeZone: string;
  number: number;
}) {
  const client = useQueryClient();
  const check = useMutation({ mutationFn: () => originalResult(scope, marker), retry: false });
  const acknowledge = useMutation({
    mutationFn: async () => {
      await client.invalidateQueries({ queryKey: pharmacyKey(scope) });
      if (!forgetStocktakeCommand(scope, marker)) throw new StocktakeRecoveryError();
    },
    retry: false,
  });
  const missing = check.error instanceof ApiError && check.error.status === 404;
  const confirmed = check.isSuccess && check.data;
  const busy = check.isPending || acknowledge.isPending;
  return (
    <Panel aria-label={`Pending stocktake result ${number}`} className="space-y-3">
      <h3 className="font-medium">
        Pending {marker.kind === "start" ? "start" : marker.kind === "count" ? "count" : "decision"}{" "}
        result {number}
      </h3>
      <Button variant="outline" disabled={busy} onClick={() => check.mutate()}>
        Check original {marker.kind} result
      </Button>
      {confirmed && (
        <>
          <p role="status" className="text-sm">
            {confirmed.message}
          </p>
          <p className="text-sm">
            <AccountName subject={confirmed.subject} /> ·{" "}
            {formatZonedDateTime(confirmed.at, timeZone)}
          </p>
          <Button disabled={busy} onClick={() => acknowledge.mutate()}>
            Acknowledge saved result
          </Button>
        </>
      )}
      {missing ? (
        <>
          <p role="alert" className="text-sm">
            No saved result was found. The original request may still finish. Check again, or
            explicitly dismiss this reference and refresh current counts before further work.
          </p>
          <Button variant="outline" disabled={busy} onClick={() => acknowledge.mutate()}>
            Dismiss missing reference
          </Button>
        </>
      ) : (
        check.error && (
          <RequestFailure
            message="The original result could not be verified for this account."
            error={check.error}
            retry={() => check.mutate()}
          />
        )
      )}
      {acknowledge.error && (
        <RequestFailure
          message="The recovery reference could not be cleared. The saved stock outcome is unchanged."
          error={acknowledge.error}
          retry={() => acknowledge.mutate()}
        />
      )}
    </Panel>
  );
}
