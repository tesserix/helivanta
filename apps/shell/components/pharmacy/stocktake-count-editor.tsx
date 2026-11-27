"use client";

import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { z } from "@/lib/csp-zod";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  newPharmacyId,
  pharmacyStocktakeCountInputSchema,
  pharmacyStocktakeCountEntrySchema,
  recordStocktakeCount,
  readStocktakeCount,
  type PharmacyStocktakeScope,
  type PharmacyStocktake,
  type PharmacyStocktakeLine,
  type PharmacyStocktakeCountInput,
  type PharmacyStocktakeCountReceipt,
} from "@helivanta/api";
import { Field, RequestFailure, useLeaveGuard, useZodForm } from "@helivanta/ui";
import {
  retainStocktakeCommand,
  forgetStocktakeCommand,
  StocktakeRecoveryError,
} from "./stocktake-recovery";

const schema = pharmacyStocktakeCountInputSchema.extend({
  counts: z
    .array(
      pharmacyStocktakeCountEntrySchema.extend({
        counted_units: z
          .string()
          .regex(
            /^(0|[1-9][0-9]*)$/,
            "Enter a whole physical count, including zero for an empty shelf.",
          )
          .transform(Number)
          .pipe(pharmacyStocktakeCountEntrySchema.shape.counted_units),
      }),
    )
    .length(1),
});
type Attempt = { command: string; input: PharmacyStocktakeCountInput };

export function StocktakeCountEditor({
  scope,
  take,
  line,
  onSaved,
  paused = false,
}: {
  scope: PharmacyStocktakeScope;
  take: PharmacyStocktake;
  line: PharmacyStocktakeLine;
  onSaved: () => void;
  paused?: boolean;
}) {
  const [version] = useState(take.version);
  const form = useZodForm(schema, {
    expected_version: version,
    counts: [{ line_id: line.id, counted_units: "" }],
    reason: "",
  });
  const [attempt, setAttempt] = useState<Attempt | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [saved, setSaved] = useState(false);
  const complete = (receipt: PharmacyStocktakeCountReceipt, submitted: Attempt) => {
    if (
      receipt.recorded_by !== scope.subject ||
      receipt.expected_version !== submitted.input.expected_version ||
      receipt.reason !== submitted.input.reason ||
      receipt.counts.length !== 1 ||
      receipt.counts[0]?.line_id !== line.id ||
      receipt.counts[0]?.counted_units !== submitted.input.counts[0]?.counted_units
    )
      throw new Error("The recovered count does not match the original observation.");
    forgetStocktakeCommand(scope, {
      kind: "count",
      stocktakeId: take.id,
      commandId: submitted.command,
    });
    setAttempt(null);
    setSaved(true);
    form.reset();
    onSaved();
  };
  const save = useMutation({
    mutationFn: async (value: Attempt) => {
      retainStocktakeCommand(scope, {
        kind: "count",
        stocktakeId: take.id,
        commandId: value.command,
      });
      complete(await recordStocktakeCount(scope, take.id, value.command, value.input), value);
    },
    retry: false,
  });
  const recover = useMutation({
    mutationFn: async () => {
      if (!attempt) return;
      setNotFound(false);
      try {
        complete(await readStocktakeCount(scope, take.id, attempt.command), attempt);
      } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
          setNotFound(true);
          return;
        }
        throw error;
      }
    },
    retry: false,
  });
  const busy = save.isPending || recover.isPending;
  const retentionFailed = save.error instanceof StocktakeRecoveryError;
  const stale = paused || take.version !== version || take.state !== "counting";
  useLeaveGuard(
    !saved && (form.formState.isDirty || attempt !== null),
    `physical count for ${line.brand_name}`,
    busy,
  );
  return (
    <form
      noValidate
      className="flex max-w-xl flex-col gap-4"
      onSubmit={form.handleSubmit((input) => {
        if (attempt || busy || stale || saved) return;
        const next = { command: newPharmacyId(), input };
        setAttempt(next);
        setNotFound(false);
        save.mutate(next);
      })}
    >
      <h3 className="font-semibold">Count {line.brand_name}</h3>
      <p className="text-sm">
        Lot {line.lot_number} · {line.location_name}. Frozen book: {line.book_units}{" "}
        {line.unit_of_issue}.
      </p>
      <p className="text-sm">
        Previously recorded:{" "}
        {line.counted_units === null
          ? "Not counted"
          : `${line.counted_units} ${line.unit_of_issue}`}
        . Leave the entry blank until you physically count it.
      </p>
      <Field
        id="stocktake-quantity"
        label={`Physical count (${line.unit_of_issue})`}
        error={form.formState.errors.counts?.[0]?.counted_units?.message}
      >
        <Input
          id="stocktake-quantity"
          inputMode="numeric"
          {...form.register("counts.0.counted_units")}
          disabled={busy || attempt !== null || stale || saved}
        />
      </Field>
      <Field
        id="stocktake-count-reason"
        label="Count reason"
        error={form.formState.errors.reason?.message}
      >
        <Input
          id="stocktake-count-reason"
          {...form.register("reason")}
          disabled={busy || attempt !== null || stale || saved}
        />
      </Field>
      {stale && (
        <p role="status">
          The stocktake changed. Close this entry and inspect the latest count before continuing.
        </p>
      )}
      {attempt && save.error && (
        <div role="alert" className="space-y-2 text-sm">
          <p>
            {retentionFailed
              ? "No count request was sent because its recovery reference could not be retained. Check browser storage or resolve pending results before retrying."
              : "The save could not be confirmed. Your original count is retained. Check its result before retrying."}
          </p>
          {!retentionFailed && (
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={() => recover.mutate()}
            >
              Check saved count
            </Button>
          )}
          {(notFound || retentionFailed) && (
            <>
              <p>
                {retentionFailed
                  ? "Retry the same count after recovery storage is available."
                  : "No saved command was found. You can retry the identical count; a stale revision will be refused."}
              </p>
              <Button
                type="button"
                disabled={busy}
                onClick={() => {
                  setNotFound(false);
                  save.mutate(attempt);
                }}
              >
                Retry original count
              </Button>
            </>
          )}
        </div>
      )}
      {recover.error && (
        <RequestFailure
          message="The saved count could not be checked. Keep this entry open."
          error={recover.error}
          retry={() => recover.mutate()}
        />
      )}
      {saved && <p role="status">Physical count saved.</p>}
      <Button type="submit" disabled={busy || attempt !== null || stale || saved}>
        {busy ? "Checking count…" : "Save physical count"}
      </Button>
    </form>
  );
}
