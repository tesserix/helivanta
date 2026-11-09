"use client";

import { useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { ShieldCheck } from "lucide-react";
import {
  ApiError,
  apiFetch,
  formatZonedDateTime,
  newPharmacyId,
  pharmacyKey,
  pharmacyProductSchedulePath,
  useApiMutation,
  useApiQuery,
  type PharmacyProduct,
  type PharmacyScheduleDeclaration,
  type PharmacyScheduleState,
  type PharmacyScope,
} from "@helivanta/api";
import { Field, FormSelectField, RequestFailure, useZodForm } from "@helivanta/ui";
import { useReportState, type FormState } from "@/lib/use-list-detail";

// A product's schedule class (#1636): declared by one person from a source
// such as the pack label, approved by another. Release and sale need it.

/** The API returns at most this many declarations (`historyLimit`). */
const HISTORY_LIMIT = 50;

export function scheduleLabel(d: Pick<PharmacyScheduleDeclaration, "regulatory_class" | "ndps">) {
  const name = d.regulatory_class === "none" ? "Not scheduled" : `Schedule ${d.regulatory_class}`;
  return `${name} · ${d.ndps ? "NDPS" : "not NDPS"}`;
}

export function ProductScheduleSection({
  scope,
  product,
  timeZone,
  canDeclare,
  canReview,
  onStateChange,
  onReviewStateChange,
}: {
  scope: PharmacyScope;
  product: PharmacyProduct;
  timeZone: string;
  canDeclare: boolean;
  canReview: boolean;
  onStateChange?: FormState;
  onReviewStateChange?: FormState;
}) {
  const state = useApiQuery<PharmacyScheduleState>(
    pharmacyKey(scope, "product-schedule", product.id),
    pharmacyProductSchedulePath(scope, product.id),
  );
  const current = state.data;
  const inForce = current?.in_force;
  const path = pharmacyProductSchedulePath(scope, product.id);
  // A new class changes what the counter may sell, so its availability is reread too.
  const invalidate = [
    pharmacyKey(scope, "product-schedule", product.id),
    pharmacyKey(scope, "counter", "availability", product.id),
  ];
  // Only the latest declaration can take effect, so an older pending one is
  // never offered for review: approving it would change nothing.
  const newest = current?.history[0];
  const pending = newest?.decision === null ? newest : undefined;
  return (
    <section aria-labelledby="product-schedule-heading" className="mt-8 border-t pt-6">
      <h4 id="product-schedule-heading" className="mb-1 flex items-center gap-2 font-medium">
        <ShieldCheck className="size-4" />
        Schedule class
      </h4>
      {state.isError ? (
        <RequestFailure
          message="Could not load the schedule class."
          retry={state.refetch}
          error={state.error}
        />
      ) : !current ? (
        <p role="status" className="text-sm text-muted-foreground">
          Loading schedule class…
        </p>
      ) : (
        <div className="space-y-4">
          {inForce ? (
            <p className="text-sm text-muted-foreground">
              {`${scheduleLabel(inForce)}, approved ${formatZonedDateTime(inForce.reviewed_at ?? inForce.created_at, timeZone)}`}
            </p>
          ) : (
            <div className="space-y-1 text-sm">
              <p>{`No schedule class in force: ${current.reason}`}</p>
              <p className="text-muted-foreground">
                Until one is approved, this product cannot be released or sold.
              </p>
            </div>
          )}
          {inForce && (inForce.regulatory_class === "X" || inForce.ndps) && (
            <p className="text-sm text-muted-foreground">
              Schedule X and NDPS products cannot be released or sold until their controls exist.
            </p>
          )}
          {canReview && pending && (
            <ScheduleReviewForm
              key={pending.id}
              declaration={pending}
              path={path}
              invalidate={invalidate}
              onStateChange={onReviewStateChange}
              onStale={() => void state.refetch()}
            />
          )}
          {canDeclare && product.status !== "withdrawn" && (
            <ScheduleDeclarationForm
              key={current.history[0]?.id ?? "none"}
              path={path}
              invalidate={invalidate}
              onStateChange={onStateChange}
            />
          )}
          {current.history.length > 0 && (
            <ol className="space-y-2" aria-label="Schedule class history">
              {current.history.map((d) => (
                <li key={d.id} className="rounded-md bg-muted p-3 text-sm">
                  <p className="font-medium">{scheduleLabel(d)}</p>
                  <p className="mt-0.5 text-muted-foreground">
                    {d.source} · declared by {d.declared_by},{" "}
                    {formatZonedDateTime(d.created_at, timeZone)}
                  </p>
                  <p className="mt-0.5 text-xs">
                    {d.decision === null
                      ? d.id === pending?.id
                        ? "Awaiting review"
                        : "Superseded by a newer declaration"
                      : `${d.decision === "approved" ? "Approved" : "Rejected"} by ${d.reviewed_by}: ${d.decision_reason}`}
                  </p>
                </li>
              ))}
            </ol>
          )}
          {current.history.length >= HISTORY_LIMIT && (
            <p className="text-xs text-muted-foreground">
              Showing the latest {HISTORY_LIMIT} declarations.
            </p>
          )}
        </div>
      )}
    </section>
  );
}

const classes = ["none", "G", "H", "H1", "X"] as const;

// Class and NDPS start empty: a default would let an unread field record the
// least restrictive answer, "not scheduled, not NDPS". Fail closed.
const declarationSchema = z.object({
  regulatory_class: z
    .string()
    .refine((v) => (classes as readonly string[]).includes(v), "Choose the schedule class"),
  ndps: z.string().refine((v) => v === "yes" || v === "no", "Say whether it is under NDPS"),
  source: z.string().trim().min(1, "Name the source, such as the pack label").max(300),
});

function ScheduleDeclarationForm({
  path,
  invalidate,
  onStateChange,
}: {
  path: string;
  invalidate: unknown[][];
  onStateChange?: FormState;
}) {
  const form = useZodForm(declarationSchema, { regulatory_class: "", ndps: "", source: "" });
  // One id per declaration, so a retried submit replays rather than records twice.
  const [id] = useState(() => newPharmacyId());
  const record = useApiMutation(
    (v: z.output<typeof declarationSchema>) =>
      apiFetch<PharmacyScheduleDeclaration>(`${path}/${id}`, {
        method: "PUT",
        body: JSON.stringify({
          regulatory_class: v.regulatory_class,
          ndps: v.ndps === "yes",
          source: v.source,
        }),
      }),
    // A failure may still have committed: reread, so a recorded declaration
    // shows and remounts this form with a fresh id instead of a spent one.
    {
      successToast: "Schedule class declared",
      invalidate,
      invalidateOnError: true,
      suppressErrorToast: true,
    },
  );
  useReportState(onStateChange, form.formState.isDirty, record.isPending);
  const errors = form.formState.errors;
  return (
    <form
      noValidate
      onSubmit={form.handleSubmit((v) => record.mutate(v))}
      className="space-y-4"
      aria-label="Declare the product's schedule class"
    >
      <div className="grid gap-4 sm:grid-cols-3">
        <FormSelectField
          control={form.control}
          name="regulatory_class"
          id="schedule-class"
          label="Class"
          options={classes.map((c) => ({ value: c, label: c === "none" ? "Not scheduled" : c }))}
        />
        <FormSelectField
          control={form.control}
          name="ndps"
          id="schedule-ndps"
          label="NDPS"
          options={[
            { value: "yes", label: "Yes" },
            { value: "no", label: "No" },
          ]}
        />
        <Field id="schedule-source" label="Source" error={errors.source?.message}>
          <Input
            id="schedule-source"
            aria-invalid={!!errors.source}
            aria-describedby={errors.source ? "schedule-source-error" : undefined}
            {...form.register("source")}
          />
        </Field>
      </div>
      {record.isError && (
        <p role="alert" className="text-sm text-destructive">
          {record.error.message}
        </p>
      )}
      <Button type="submit" disabled={record.isPending}>
        {record.isPending ? "Declaring…" : "Declare schedule class"}
      </Button>
    </form>
  );
}

const reviewSchema = z.object({
  reason: z.string().trim().min(1, "Say what you checked.").max(500),
});

function ScheduleReviewForm({
  declaration,
  path,
  invalidate,
  onStateChange,
  onStale,
}: {
  declaration: PharmacyScheduleDeclaration;
  path: string;
  invalidate: unknown[][];
  onStateChange?: FormState;
  /** Someone else decided it first: reread rather than keep offering a stale review. */
  onStale: () => void;
}) {
  const form = useZodForm(reviewSchema, { reason: "" });
  const review = useApiMutation(
    (body: { decision: "approved" | "rejected"; reason: string }) =>
      apiFetch<PharmacyScheduleDeclaration>(`${path}/${declaration.id}/review`, {
        method: "POST",
        body: JSON.stringify(body),
      }),
    {
      successToast: "Review recorded",
      invalidate,
      suppressErrorToast: true,
      onError: (error) => {
        if (
          error instanceof ApiError &&
          (error.status === 404 || (error.status === 409 && error.code !== "four_eyes"))
        )
          onStale();
      },
    },
  );
  useReportState(onStateChange, form.formState.isDirty, review.isPending);
  const decide = (decision: "approved" | "rejected") =>
    form.handleSubmit(({ reason }) => review.mutate({ decision, reason }));
  return (
    <form noValidate onSubmit={decide("approved")} className="space-y-3 rounded-md border p-3">
      <p className="text-sm">
        Awaiting review: {scheduleLabel(declaration)}, from {declaration.source}, declared by{" "}
        {declaration.declared_by}.
      </p>
      <Field
        id="schedule-review-reason"
        label="What you checked"
        error={form.formState.errors.reason?.message}
      >
        <Input
          id="schedule-review-reason"
          aria-invalid={!!form.formState.errors.reason}
          aria-describedby={
            form.formState.errors.reason ? "schedule-review-reason-error" : undefined
          }
          {...form.register("reason")}
        />
      </Field>
      {review.isError && (
        <p role="alert" className="text-sm text-destructive">
          {review.error.message}
        </p>
      )}
      <div className="flex gap-2">
        <Button type="submit" disabled={review.isPending}>
          Approve
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={review.isPending}
          onClick={decide("rejected")}
        >
          Reject
        </Button>
      </div>
    </form>
  );
}
