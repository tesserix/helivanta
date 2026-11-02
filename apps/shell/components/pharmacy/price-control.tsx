"use client";

import { useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Scale } from "lucide-react";
import {
  apiFetch,
  newPharmacyId,
  pharmacyKey,
  pharmacyPriceControlInputSchema,
  pharmacyRoot,
  useApiMutation,
  useApiQuery,
  type PharmacyPriceControl,
  type PharmacyPriceDeclaration,
  type PharmacyProduct,
  type PharmacyScope,
} from "@helivanta/api";
import { Field, FormSelectField, RequestFailure, useZodForm } from "@helivanta/ui";
import { useReportState, type FormState } from "@/lib/use-list-detail";
import { CurrencyAmountField, formatPharmacyMoney } from "./currency-amount-field";

// NPPA ceiling prices (#1371): declared by one person, approved by another.

function declarationLabel(row: PharmacyPriceDeclaration, currency: string): string {
  if (!row.controlled || row.ceiling_minor === null) return "Not price-controlled";
  return `NPPA ceiling ${formatPharmacyMoney(row.ceiling_minor, currency)} per unit + GST`;
}

const stateText: Record<PharmacyPriceControl["state"], string> = {
  undeclared: "Not declared",
  blocked: "Not sellable",
  in_force: "In force",
};

export function ProductPriceControlSection({
  scope,
  product,
  today,
  canDeclare,
  canReview,
  onStateChange,
  onReviewStateChange,
}: {
  scope: PharmacyScope;
  product: PharmacyProduct;
  today: string;
  canDeclare: boolean;
  canReview: boolean;
  /** The declaration and the review are separate unsaved forms (#1303 D5). */
  onStateChange?: FormState;
  onReviewStateChange?: FormState;
}) {
  const key = pharmacyKey(scope, "price-control", product.id);
  const path = `${pharmacyRoot(scope)}/catalogue/products/${product.id}/price-control`;
  const control = useApiQuery<PharmacyPriceControl>(key, path);
  return (
    <section aria-labelledby="price-control-heading" className="mt-8 border-t pt-6">
      <h4 id="price-control-heading" className="mb-1 flex items-center gap-2 font-medium">
        <Scale className="size-4" />
        Price control
      </h4>
      {control.isError ? (
        <RequestFailure message="Could not load the price control." retry={control.refetch} />
      ) : !control.data ? (
        <p role="status" className="text-sm text-muted-foreground">
          Loading price control…
        </p>
      ) : (
        <PriceControlBody
          data={control.data}
          product={product}
          today={today}
          path={path}
          invalidate={[key]}
          canDeclare={canDeclare}
          canReview={canReview}
          onStateChange={onStateChange}
          onReviewStateChange={onReviewStateChange}
        />
      )}
    </section>
  );
}

function PriceControlBody({
  data,
  product,
  today,
  path,
  invalidate,
  canDeclare,
  canReview,
  onStateChange,
  onReviewStateChange,
}: {
  data: PharmacyPriceControl;
  product: PharmacyProduct;
  today: string;
  path: string;
  invalidate: unknown[][];
  canDeclare: boolean;
  canReview: boolean;
  onStateChange?: FormState;
  onReviewStateChange?: FormState;
}) {
  const latest = data.history[0];
  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        {stateText[data.state]}
        {data.in_force ? `: ${declarationLabel(data.in_force, product.currency)}` : ""}
        {data.reason ? ` · ${data.reason}` : ""}
      </p>
      {canReview && latest && latest.decision === null && (
        <ReviewForm
          key={latest.id}
          declaration={latest}
          currency={product.currency}
          path={path}
          invalidate={invalidate}
          onStateChange={onReviewStateChange}
        />
      )}
      {canDeclare && product.status !== "withdrawn" && (
        <DeclarationForm
          key={data.history.length}
          currency={product.currency}
          today={today}
          path={path}
          invalidate={invalidate}
          onStateChange={onStateChange}
        />
      )}
      {data.history.length > 0 && (
        <ol className="space-y-2">
          {data.history.map((row) => (
            <li key={row.id} className="rounded-md bg-muted p-3 text-sm">
              <p className="font-medium">{declarationLabel(row, product.currency)}</p>
              <p className="mt-0.5 text-muted-foreground">
                from {row.effective_from}
                {row.order_reference ? ` · ${row.order_reference} of ${row.order_date}` : ""}
                {` · ${row.source} · declared by ${row.declared_by}`}
              </p>
              <p className="mt-0.5 text-xs">
                {row.decision === null
                  ? "Awaiting review"
                  : `${row.decision === "approved" ? "Approved" : "Rejected"} by ${row.reviewed_by}: ${row.decision_reason}`}
              </p>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}

const declarationForm = pharmacyPriceControlInputSchema
  .extend({
    ceiling_minor: z.number(),
    order_reference: z.string().trim().max(120),
    order_date: z.string(),
  })
  .superRefine((value, ctx) => {
    if (value.price_control !== "controlled") return;
    if (!Number.isSafeInteger(value.ceiling_minor) || value.ceiling_minor < 1)
      ctx.addIssue({
        code: "custom",
        path: ["ceiling_minor"],
        message: "Enter the ceiling per unit, before GST.",
      });
    if (!value.order_reference)
      ctx.addIssue({
        code: "custom",
        path: ["order_reference"],
        message: "Enter the NPPA order or notification number.",
      });
    if (!/^\d{4}-\d{2}-\d{2}$/.test(value.order_date))
      ctx.addIssue({ code: "custom", path: ["order_date"], message: "Enter the order's date." });
    else if (value.order_date > value.effective_from)
      ctx.addIssue({
        code: "custom",
        path: ["effective_from"],
        message: "A ceiling takes effect on or after its order's date.",
      });
  });

type DeclarationValues = z.infer<typeof declarationForm>;

function DeclarationForm({
  currency,
  today,
  path,
  invalidate,
  onStateChange,
}: {
  currency: string;
  today: string;
  path: string;
  invalidate: unknown[][];
  onStateChange?: FormState;
}) {
  const form = useZodForm(declarationForm, {
    price_control: "controlled",
    ceiling_minor: 0,
    order_reference: "",
    order_date: "",
    effective_from: today,
    source: "",
  });
  // One id per declaration, so a retried submit replays rather than records twice.
  const [id] = useState(() => newPharmacyId());
  const record = useApiMutation(
    (body: Record<string, unknown>) =>
      apiFetch<PharmacyPriceDeclaration>(`${path}/${id}`, {
        method: "PUT",
        body: JSON.stringify(body),
      }),
    { successToast: "Declaration recorded", invalidate, suppressErrorToast: true },
  );
  useReportState(onStateChange, form.formState.isDirty, record.isPending);
  const controlled = form.watch("price_control") === "controlled";
  const errors = form.formState.errors;
  const onSubmit = ({ ceiling_minor, order_reference, order_date, ...rest }: DeclarationValues) =>
    record.mutate(
      rest.price_control === "controlled"
        ? { ...rest, ceiling_minor, order_reference, order_date }
        : rest,
    );
  return (
    <form noValidate onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <FormSelectField
          control={form.control}
          name="price_control"
          id="price-kind"
          label="Scheduled formulation"
          options={[
            { value: "controlled", label: "Price-controlled (NPPA ceiling)" },
            { value: "not_controlled", label: "Not price-controlled" },
          ]}
        />
        <Field id="price-from" label="In force from" error={errors.effective_from?.message}>
          <Input id="price-from" type="date" {...form.register("effective_from")} />
        </Field>
      </div>
      {controlled && (
        <div className="grid gap-4 sm:grid-cols-3">
          <CurrencyAmountField
            id="price-ceiling"
            label="Ceiling per unit, before GST"
            currency={currency}
            initialMinor={0}
            error={errors.ceiling_minor?.message}
            onChange={(minor) =>
              form.setValue("ceiling_minor", minor, { shouldDirty: true, shouldValidate: false })
            }
          />
          <Field id="price-order" label="NPPA order" error={errors.order_reference?.message}>
            <Input
              id="price-order"
              aria-invalid={!!errors.order_reference}
              aria-describedby={errors.order_reference ? "price-order-error" : undefined}
              {...form.register("order_reference")}
            />
          </Field>
          <Field id="price-order-date" label="Order date" error={errors.order_date?.message}>
            <Input id="price-order-date" type="date" {...form.register("order_date")} />
          </Field>
        </div>
      )}
      <Field id="price-source" label="Source" error={errors.source?.message}>
        <Input
          id="price-source"
          aria-invalid={!!errors.source}
          aria-describedby={errors.source ? "price-source-error" : undefined}
          {...form.register("source")}
        />
      </Field>
      {record.isError && (
        <p role="alert" className="text-sm text-destructive">
          {record.error.message}
        </p>
      )}
      <Button type="submit" disabled={record.isPending}>
        {record.isPending ? "Recording…" : "Record declaration"}
      </Button>
    </form>
  );
}

const reviewForm = z.object({
  reason: z.string().trim().min(1, "Say what you checked.").max(500),
});

function ReviewForm({
  declaration,
  currency,
  path,
  invalidate,
  onStateChange,
}: {
  declaration: PharmacyPriceDeclaration;
  currency: string;
  path: string;
  invalidate: unknown[][];
  onStateChange?: FormState;
}) {
  const form = useZodForm(reviewForm, { reason: "" });
  const review = useApiMutation(
    (body: { decision: "approved" | "rejected"; reason: string }) =>
      apiFetch<PharmacyPriceDeclaration>(`${path}/${declaration.id}/review`, {
        method: "POST",
        body: JSON.stringify(body),
      }),
    { successToast: "Review recorded", invalidate, suppressErrorToast: true },
  );
  useReportState(onStateChange, form.formState.isDirty, review.isPending);
  const decide = (decision: "approved" | "rejected") =>
    form.handleSubmit(({ reason }) => review.mutate({ decision, reason }));
  return (
    <form noValidate onSubmit={decide("approved")} className="space-y-3 rounded-md border p-3">
      <p className="text-sm">
        Awaiting review: {declarationLabel(declaration, currency)}, from{" "}
        {declaration.effective_from}, declared by {declaration.declared_by}.
      </p>
      <Field
        id="price-review-reason"
        label="What you checked"
        error={form.formState.errors.reason?.message}
      >
        <Input
          id="price-review-reason"
          aria-invalid={!!form.formState.errors.reason}
          aria-describedby={form.formState.errors.reason ? "price-review-reason-error" : undefined}
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
