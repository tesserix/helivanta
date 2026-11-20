"use client";

import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  newPharmacyId,
  pharmacyStocktakeDecisionInputSchema,
  recordStocktakeDecision,
  readStocktakeDecision,
  type PharmacyScope,
  type PharmacyStocktake,
  type PharmacyStocktakeDecisionInput,
  type PharmacyStocktakeDecisionReceipt,
} from "@helivanta/api";
import {
  Controller,
  Field,
  SelectControl,
  RequestFailure,
  useLeaveGuard,
  useZodForm,
} from "@helivanta/ui";

const labels = {
  submit: "Submit for independent review",
  approve: "Approve and post differences",
  reject: "Reject stocktake",
  cancel: "Cancel stocktake",
} as const;
type Attempt = { command: string; input: PharmacyStocktakeDecisionInput };

export function StocktakeDecision({
  scope,
  take,
  onSaved,
  paused = false,
}: {
  scope: PharmacyScope;
  take: PharmacyStocktake;
  onSaved: () => void;
  paused?: boolean;
}) {
  const [basis] = useState(take);
  const allowed: PharmacyStocktakeDecisionInput["decision"][] = [];
  if (basis.state === "counting" && basis.counted_rows === basis.snapshot_rows)
    allowed.push("submit");
  if (basis.state === "submitted" && basis.review_relationship === "independent")
    allowed.push("approve", "reject");
  if (basis.state === "counting" || basis.state === "submitted") allowed.push("cancel");
  const schema = pharmacyStocktakeDecisionInputSchema
    .pick({ decision: true, reason: true })
    .extend({ attested: z.boolean() })
    .superRefine((value, ctx) => {
      if (!allowed.includes(value.decision))
        ctx.addIssue({
          code: "custom",
          path: ["decision"],
          message: "This decision is not available for the current count.",
        });
      if (value.decision === "approve" && basis.positive_difference_rows > 0 && !value.attested)
        ctx.addIssue({
          code: "custom",
          path: ["attested"],
          message: "Confirm the extra units have physically been moved to quarantine.",
        });
    })
    .transform(({ decision, reason }): PharmacyStocktakeDecisionInput => ({
      decision,
      reason,
      expected_version: basis.version,
      ...(decision === "approve"
        ? {
            extra_units_disposition:
              basis.positive_difference_rows > 0
                ? ("moved_to_quarantine" as const)
                : ("no_extra_units" as const),
          }
        : {}),
    }))
    .pipe(pharmacyStocktakeDecisionInputSchema);
  const form = useZodForm(schema, { decision: allowed[0], reason: "", attested: false });
  const [attempt, setAttempt] = useState<Attempt | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [saved, setSaved] = useState(false);
  const complete = (receipt: PharmacyStocktakeDecisionReceipt, value: Attempt) => {
    if (
      receipt.expected_version !== value.input.expected_version ||
      receipt.decision !== value.input.decision ||
      receipt.reason !== value.input.reason ||
      (receipt.extra_units_disposition ?? null) !== (value.input.extra_units_disposition ?? null)
    )
      throw new Error("The recovered decision does not match the original review.");
    setAttempt(null);
    setSaved(true);
    form.reset();
    onSaved();
  };
  const save = useMutation({
    mutationFn: async (value: Attempt) =>
      complete(await recordStocktakeDecision(scope, take.id, value.command, value.input), value),
    retry: false,
  });
  const recover = useMutation({
    mutationFn: async () => {
      if (!attempt) return;
      setNotFound(false);
      try {
        complete(await readStocktakeDecision(scope, take.id, attempt.command), attempt);
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
  const stale =
    take.version !== basis.version ||
    take.state !== basis.state ||
    take.review_relationship !== basis.review_relationship;
  const locked = paused || busy || attempt !== null || stale || saved;
  useLeaveGuard(
    !saved && (form.formState.isDirty || attempt !== null),
    `decision for stocktake ${take.number}`,
    busy,
  );
  if (allowed.length === 0) return null;
  return (
    <form
      noValidate
      className="flex max-w-xl flex-col gap-4"
      onSubmit={form.handleSubmit((input) => {
        if (locked) return;
        const next = { command: newPharmacyId(), input };
        setAttempt(next);
        setNotFound(false);
        save.mutate(next);
      })}
    >
      <h3 className="font-semibold">Stocktake decision</h3>
      <p className="text-sm">
        Approval posts all differences atomically. It requires an independent person who neither
        started nor counted this stocktake. Rejection and cancellation retain the evidence.
      </p>
      <Field
        id="stocktake-decision"
        label="Decision"
        error={form.formState.errors.decision?.message}
      >
        <Controller
          control={form.control}
          name="decision"
          render={({ field }) => (
            <SelectControl
              id="stocktake-decision"
              value={field.value}
              onValueChange={field.onChange}
              onBlur={field.onBlur}
              options={allowed.map((value) => ({ value, label: labels[value] }))}
              disabled={locked}
            />
          )}
        />
      </Field>
      {form.watch("decision") === "approve" && basis.positive_difference_rows > 0 && (
        <Field
          id="stocktake-attested"
          label="Extra-unit handling"
          error={form.formState.errors.attested?.message}
        >
          <label className="flex items-start gap-2 text-sm">
            <input
              id="stocktake-attested"
              type="checkbox"
              {...form.register("attested")}
              disabled={locked}
            />
            I have physically moved all extra units to the selected quarantine location.
          </label>
        </Field>
      )}
      <Field
        id="stocktake-decision-reason"
        label="Decision reason"
        error={form.formState.errors.reason?.message}
      >
        <Input
          id="stocktake-decision-reason"
          maxLength={500}
          {...form.register("reason")}
          disabled={locked}
        />
      </Field>
      {stale && (
        <p role="status">
          The stocktake changed. Reopen its latest revision before recording another decision.
        </p>
      )}
      {attempt && save.error && (
        <div role="alert" className="space-y-2 text-sm">
          <p>The decision could not be confirmed. Check its saved result before retrying.</p>
          <Button type="button" variant="outline" disabled={busy} onClick={() => recover.mutate()}>
            Check saved decision
          </Button>
          {notFound && (
            <>
              <p>No saved decision was found. Only the original decision can be retried.</p>
              <Button
                type="button"
                disabled={busy}
                onClick={() => {
                  setNotFound(false);
                  save.mutate(attempt);
                }}
              >
                Retry original decision
              </Button>
            </>
          )}
        </div>
      )}
      {recover.error && (
        <RequestFailure
          message="The saved decision could not be checked. Your original review is retained."
          error={recover.error}
          retry={() => recover.mutate()}
        />
      )}
      {saved && <p role="status">Stocktake decision recorded.</p>}
      <Button type="submit" disabled={locked}>
        {busy ? "Checking decision…" : "Record decision"}
      </Button>
    </form>
  );
}
