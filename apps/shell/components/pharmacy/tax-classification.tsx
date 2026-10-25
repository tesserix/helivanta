"use client";

import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Receipt } from "lucide-react";
import {
  apiFetch,
  formatRatePercent,
  pharmacyKey,
  pharmacyRoot,
  pharmacyTaxClassificationInputSchema,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  type PharmacyProduct,
  type PharmacyProductTax,
  type PharmacyScope,
  type PharmacyTaxClassification,
  type TaxScheme,
} from "@helivanta/api";
import { Field, FormSelectField, LoadMore, QueryState, useZodForm } from "@helivanta/ui";

export function taxLabel(tax: PharmacyProductTax | null | undefined, name = "HSN"): string {
  if (!tax) return "Not classified";
  if (tax.supply_nature === "nil_rated") return "Nil-rated";
  if (tax.supply_nature === "exempt") return "Exempt";
  return `${name} ${tax.classification_code} · ${formatRatePercent(tax.rate_ppm)}%`;
}

export function TaxBadge({
  tax,
  name,
}: {
  tax: PharmacyProductTax | null | undefined;
  name?: string;
}) {
  return (
    <span
      className={
        tax
          ? "rounded-full bg-muted px-3 py-1 text-xs font-medium tabular-nums"
          : "rounded-full bg-(--hms-amber-tint) px-3 py-1 text-xs font-medium text-(--hms-amber)"
      }
    >
      {taxLabel(tax, name)}
    </span>
  );
}

function classificationForm(scheme: TaxScheme, history: PharmacyTaxClassification[]) {
  const code = new RegExp(scheme.classification.pattern);
  return pharmacyTaxClassificationInputSchema
    .extend({
      exemption_basis: z.string().trim().max(500),
      reason: z.string().trim().max(500),
    })
    .superRefine((value, ctx) => {
      if (!code.test(value.classification_code.trim().toUpperCase()))
        ctx.addIssue({
          code: "custom",
          path: ["classification_code"],
          message: `Enter a 4, 6 or 8 digit ${scheme.classification.name} code.`,
        });
      const zero = Number(value.rate_percent) === 0;
      if (value.supply_nature === "taxable" && zero)
        ctx.addIssue({
          code: "custom",
          path: ["rate_percent"],
          message: "A taxable product needs a rate above 0.",
        });
      if (value.supply_nature !== "taxable" && !zero)
        ctx.addIssue({
          code: "custom",
          path: ["rate_percent"],
          message: "Nil-rated and exempt products carry no rate.",
        });
      if (value.supply_nature === "exempt" && !value.exemption_basis)
        ctx.addIssue({
          code: "custom",
          path: ["exemption_basis"],
          message: "Record the notification or basis for the exemption.",
        });
      if (!value.reason && history.some((row) => row.effective_from === value.effective_from))
        ctx.addIssue({
          code: "custom",
          path: ["reason"],
          message: "A classification already starts on this date. Say why you are correcting it.",
        });
    });
}

type ClassificationValues = z.infer<ReturnType<typeof classificationForm>>;

export function ProductTaxSection({
  scope,
  product,
  scheme,
  canManage,
  today,
}: {
  scope: PharmacyScope;
  product: PharmacyProduct;
  scheme: TaxScheme | null;
  canManage: boolean;
  today: string;
}) {
  const key = pharmacyKey(scope, "tax-classifications", product.id);
  const history = useApiPagedQuery<PharmacyTaxClassification>(
    key,
    `${pharmacyRoot(scope)}/catalogue/products/${product.id}/tax-classifications`,
  );
  const name = scheme?.classification.name;
  // The product record carries the classification in force; the history's
  // first page need not hold it, and the selected copy goes stale.
  const current = useApiQuery<PharmacyProduct>(
    pharmacyKey(scope, "product", product.id),
    `${pharmacyRoot(scope)}/catalogue/products/${product.id}`,
  );
  const inForce = current.data ? current.data.tax : product.tax;
  return (
    <section aria-labelledby="product-tax-heading" className="mt-8 border-t pt-6">
      <h4 id="product-tax-heading" className="mb-1 flex items-center gap-2 font-medium">
        <Receipt className="size-4" />
        Tax
      </h4>
      <p className="mb-4 text-sm text-muted-foreground">
        In force today: <TaxBadge tax={inForce} name={name} />
      </p>
      {!scheme ? (
        <p className="text-sm text-muted-foreground">
          Tax setup is not available for this country yet.
        </p>
      ) : (
        canManage &&
        product.status !== "withdrawn" && (
          <ClassificationForm
            key={history.items.length}
            scheme={scheme}
            history={history.items}
            today={today}
            submit={(values) =>
              apiFetch<PharmacyTaxClassification>(
                `${pharmacyRoot(scope)}/catalogue/products/${product.id}/tax-classifications`,
                { method: "POST", body: JSON.stringify(values) },
              )
            }
            invalidate={[
              key,
              pharmacyKey(scope, "catalogue"),
              pharmacyKey(scope, "product", product.id),
            ]}
          />
        )
      )}
      <QueryState
        query={history}
        label="tax classifications"
        empty={{
          icon: Receipt,
          title: "Not classified",
          hint: "No tax classification has been recorded.",
        }}
      >
        {(rows) => (
          <ol className="mt-4 space-y-2">
            {rows.map((row) => (
              <li
                key={row.id}
                className="flex flex-wrap items-center justify-between gap-3 rounded-md bg-muted p-3 text-sm"
              >
                <div className="min-w-0">
                  <p className="font-medium">{taxLabel(row, name)}</p>
                  <p className="mt-0.5 text-muted-foreground">
                    from {row.effective_from} · {row.recorded_by}
                    {row.exemption_basis ? ` · ${row.exemption_basis}` : ""}
                    {row.reason ? ` · ${row.reason}` : ""}
                  </p>
                </div>
                {row.in_force ? (
                  <span className="text-xs font-medium">In force</span>
                ) : row.replaced ? (
                  <span className="text-xs text-muted-foreground">Replaced</span>
                ) : row.effective_from > today ? (
                  <span className="text-xs text-muted-foreground">Upcoming</span>
                ) : null}
              </li>
            ))}
          </ol>
        )}
      </QueryState>
      <LoadMore
        label="Load more classifications"
        hasMore={history.hasMore}
        isLoading={history.isFetchingMore}
        onClick={history.loadMore}
      />
    </section>
  );
}

function ClassificationForm({
  scheme,
  history,
  today,
  submit,
  invalidate,
}: {
  scheme: TaxScheme;
  history: PharmacyTaxClassification[];
  today: string;
  submit: (values: Record<string, string>) => Promise<PharmacyTaxClassification>;
  invalidate: unknown[][];
}) {
  const form = useZodForm(classificationForm(scheme, history), {
    classification_code: "",
    supply_nature: "taxable",
    rate_percent: "",
    exemption_basis: "",
    effective_from: today,
    reason: "",
  });
  const record = useApiMutation(submit, {
    successToast: "Classification recorded",
    invalidate,
    suppressErrorToast: true,
  });
  const nature = form.watch("supply_nature");
  const errors = form.formState.errors;
  const onSubmit = ({ exemption_basis, reason, ...rest }: ClassificationValues) =>
    record.mutate({
      ...rest,
      ...(exemption_basis ? { exemption_basis } : {}),
      ...(reason ? { reason } : {}),
    });
  return (
    <form noValidate onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Field
          id="tax-code"
          label={`${scheme.classification.name} code`}
          error={errors.classification_code?.message}
        >
          <Input
            id="tax-code"
            inputMode="numeric"
            aria-invalid={!!errors.classification_code}
            aria-describedby={errors.classification_code ? "tax-code-error" : undefined}
            {...form.register("classification_code")}
          />
        </Field>
        <FormSelectField
          control={form.control}
          name="supply_nature"
          id="tax-nature"
          label="Supply"
          options={[
            { value: "taxable", label: "Taxable" },
            { value: "nil_rated", label: "Nil-rated" },
            { value: "exempt", label: "Exempt" },
          ]}
          onValueChange={(value) => {
            if (value !== "taxable") form.setValue("rate_percent", "0");
            if (value !== "exempt") form.setValue("exemption_basis", "");
          }}
        />
        <Field id="tax-rate" label="Rate (%)" error={errors.rate_percent?.message}>
          <Input
            id="tax-rate"
            inputMode="decimal"
            readOnly={nature !== "taxable"}
            aria-invalid={!!errors.rate_percent}
            aria-describedby={errors.rate_percent ? "tax-rate-error" : undefined}
            {...form.register("rate_percent")}
          />
        </Field>
        <Field id="tax-from" label="In force from" error={errors.effective_from?.message}>
          <Input id="tax-from" type="date" min={today} {...form.register("effective_from")} />
        </Field>
      </div>
      {nature === "exempt" && (
        <Field id="tax-basis" label="Exemption basis" error={errors.exemption_basis?.message}>
          <Input
            id="tax-basis"
            aria-invalid={!!errors.exemption_basis}
            aria-describedby={errors.exemption_basis ? "tax-basis-error" : undefined}
            {...form.register("exemption_basis")}
          />
        </Field>
      )}
      <Field
        id="tax-reason"
        label="Reason for a correction (optional)"
        error={errors.reason?.message}
      >
        <Input
          id="tax-reason"
          aria-invalid={!!errors.reason}
          aria-describedby={errors.reason ? "tax-reason-error" : undefined}
          {...form.register("reason")}
        />
      </Field>
      {record.isError && (
        <p role="alert" className="text-sm text-destructive">
          {record.error.message}
        </p>
      )}
      <Button type="submit" disabled={record.isPending}>
        {record.isPending ? "Recording…" : "Record classification"}
      </Button>
    </form>
  );
}
