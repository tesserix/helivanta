"use client";

import { useEffect, useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  apiFetch,
  countryConfiguration,
  useApiMutation,
  useApiQuery,
  useApiPagedQuery,
  usePermissions,
  pharmacyKey,
  pharmacyRoot,
  newPharmacyId,
  pharmacyPayableFormSchema,
  pharmacySettlementInputSchema,
  type PharmacyScope,
  type PharmacyPayableInput,
  type PharmacyPayableDetail,
  type PharmacySettlementInput,
  type PharmacySettlement,
  type PharmacyMovement,
} from "@helivanta/api";
import {
  Field,
  FormSelectField,
  SelectControl,
  useZodForm,
  useFieldArray,
  LoadMore,
  formatDateTime,
} from "@helivanta/ui";
import { CurrencyAmountField, formatPharmacyMoney } from "./currency-amount-field";
import { PharmacyFailure } from "./pharmacy-feedback";

const categories = [
  { value: "inventory", label: "Inventory purchase" },
  { value: "utilities", label: "Utilities" },
  { value: "rent", label: "Rent" },
  { value: "salary", label: "Salary" },
  { value: "transport", label: "Transport" },
  { value: "maintenance", label: "Maintenance" },
  { value: "professional", label: "Professional services" },
  { value: "other", label: "Other expense" },
];
const blankLine: PharmacyPayableInput["lines"][number] = {
  receipt_id: null,
  category: "other",
  description: "",
  net_minor: 0,
  tax_minor: 0,
  recoverable_tax_minor: 0,
  landed_minor: 0,
};
export function PayableForm({
  scope,
  defaultCurrency,
  onSaved,
  onCancel,
}: {
  scope: PharmacyScope;
  defaultCurrency: string;
  onSaved: (id: string) => void;
  onCancel: () => void;
}) {
  const [id] = useState(() => newPharmacyId());
  const form = useZodForm(pharmacyPayableFormSchema, {
    payee_name: "",
    payee_reference: "",
    document_reference: "",
    issued_on: "",
    due_on: "",
    currency: defaultCurrency,
    lines: [{ ...blankLine }],
  });
  const lines = useFieldArray({ control: form.control, name: "lines" });
  const currency = form.watch("currency");
  const save = useApiMutation(
    (values: PharmacyPayableInput) =>
      apiFetch<PharmacyPayableDetail>(`${pharmacyRoot(scope)}/accounts/payables/${id}`, {
        method: "PUT",
        body: JSON.stringify(values),
      }),
    {
      invalidate: [pharmacyKey(scope, "accounts")],
      successToast: "Bill or expense recorded",
      suppressErrorToast: true,
      onSuccess: (row) => onSaved(row.id),
    },
  );
  return (
    <form
      noValidate
      onSubmit={form.handleSubmit((values) => save.mutate(values))}
      className="space-y-6"
    >
      <div>
        <h3 className="text-lg font-semibold">Record a bill or expense</h3>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">
          Use the original supplier document and receipt references. Records are permanent;
          corrections use credits or reversals. Amounts are in the selected currency.
        </p>
      </div>
      <fieldset disabled={save.isPending} className="space-y-6">
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <Field
            id="payee-name"
            label="Payee name"
            error={form.formState.errors.payee_name?.message}
          >
            <Input id="payee-name" {...form.register("payee_name")} maxLength={200} />
          </Field>
          <Field
            id="payee-reference"
            label="Payee reference"
            error={form.formState.errors.payee_reference?.message}
          >
            <Input id="payee-reference" {...form.register("payee_reference")} maxLength={100} />
          </Field>
          <Field
            id="payable-reference"
            label="Document reference"
            error={form.formState.errors.document_reference?.message}
          >
            <Input
              id="payable-reference"
              {...form.register("document_reference")}
              maxLength={100}
            />
          </Field>
          <Field
            id="payable-issued"
            label="Document date"
            error={form.formState.errors.issued_on?.message}
          >
            <Input id="payable-issued" type="date" {...form.register("issued_on")} />
          </Field>
          <Field id="payable-due" label="Due date" error={form.formState.errors.due_on?.message}>
            <Input id="payable-due" type="date" {...form.register("due_on")} />
          </Field>
          <Field
            id="payable-currency"
            label="Currency"
            error={form.formState.errors.currency?.message}
          >
            <SelectControl
              id="payable-currency"
              value={currency}
              onValueChange={(value) => {
                form.setValue("currency", value);
                form.getValues("lines").forEach((_, i) => {
                  for (const field of [
                    "net_minor",
                    "tax_minor",
                    "recoverable_tax_minor",
                    "landed_minor",
                  ] as const)
                    form.setValue(`lines.${i}.${field}`, 0);
                });
              }}
              options={[
                ...countryConfiguration.countries.map((p) => p.currency),
                "USD",
                "AUD",
                "GBP",
                "EUR",
                "JPY",
                "KWD",
              ].map((value) => ({
                value,
                label: value,
              }))}
            />
          </Field>
        </div>
        <div className="space-y-5">
          {lines.fields.map((row, index) => (
            <fieldset key={row.id} className="space-y-4 rounded-xl border p-4">
              <legend className="px-2 text-sm font-medium">Line {index + 1}</legend>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field
                  id={`payable-category-${index}`}
                  label={`Category ${index + 1}`}
                  error={form.formState.errors.lines?.[index]?.category?.message}
                >
                  <SelectControl
                    id={`payable-category-${index}`}
                    value={form.watch(`lines.${index}.category`)}
                    onValueChange={(value) => {
                      form.setValue(
                        `lines.${index}.category`,
                        value as PharmacyPayableInput["lines"][number]["category"],
                        { shouldValidate: true },
                      );
                      if (value !== "inventory")
                        form.setValue(`lines.${index}.receipt_id`, null, { shouldValidate: true });
                    }}
                    options={categories}
                  />
                </Field>
                <Field
                  id={`payable-description-${index}`}
                  label={`Description ${index + 1}`}
                  error={form.formState.errors.lines?.[index]?.description?.message}
                >
                  <Input
                    id={`payable-description-${index}`}
                    {...form.register(`lines.${index}.description`)}
                    maxLength={500}
                  />
                </Field>
              </div>
              {form.watch(`lines.${index}.category`) === "inventory" && (
                <ReceiptPicker
                  scope={scope}
                  id={`payable-receipt-${index}`}
                  value={form.watch(`lines.${index}.receipt_id`) ?? ""}
                  onChange={(value) =>
                    form.setValue(`lines.${index}.receipt_id`, value, { shouldValidate: true })
                  }
                  error={form.formState.errors.lines?.[index]?.receipt_id?.message}
                />
              )}
              {form.watch(`lines.${index}.category`) !== "inventory" &&
                form.getValues(`lines.${index}.receipt_id`) && (
                  <p role="alert" className="text-sm text-destructive">
                    This line links inventory. Change back to Inventory purchase or remove and
                    re-add the line as an expense.
                  </p>
                )}
              <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
                {(
                  [
                    ["net_minor", "Net amount"],
                    ["tax_minor", "Tax"],
                    ["recoverable_tax_minor", "Recoverable tax"],
                    ["landed_minor", "Landed charges"],
                  ] as const
                ).map(([field, label]) => (
                  <CurrencyAmountField
                    key={`${row.id}:${field}:${currency}`}
                    id={`payable-${field}-${index}`}
                    label={`${label} ${index + 1}`}
                    currency={currency}
                    initialMinor={form.getValues(`lines.${index}.${field}`)}
                    onChange={(value) =>
                      form.setValue(`lines.${index}.${field}`, value, { shouldValidate: true })
                    }
                    error={form.formState.errors.lines?.[index]?.[field]?.message}
                  />
                ))}
              </div>
              <p className="text-xs leading-5 text-muted-foreground">
                Net amount is after discounts. Record tax and recoverable tax from the approved
                source document; landed charges include freight allocated to this line.
              </p>
              {lines.fields.length > 1 && (
                <Button type="button" variant="ghost" onClick={() => lines.remove(index)}>
                  Remove line {index + 1}
                </Button>
              )}
            </fieldset>
          ))}
        </div>
        {lines.fields.length < 100 && (
          <Button type="button" variant="outline" onClick={() => lines.append({ ...blankLine })}>
            Add line
          </Button>
        )}
        {form.formState.errors.lines?.message && (
          <p role="alert" className="text-sm text-destructive">
            {form.formState.errors.lines.message}
          </p>
        )}
        {save.isError && (
          <p role="alert" className="text-sm text-destructive">
            {save.error.message}
          </p>
        )}
        <div className="flex flex-wrap gap-3">
          <Button type="submit" disabled={save.isPending}>
            {save.isPending ? "Recording…" : "Save bill or expense"}
          </Button>
          <Button type="button" variant="outline" onClick={onCancel}>
            Cancel
          </Button>
        </div>
      </fieldset>
    </form>
  );
}
function ReceiptPicker({
  scope,
  id,
  value,
  onChange,
  error,
}: {
  scope: PharmacyScope;
  id: string;
  value: string;
  onChange: (id: string) => void;
  error?: string;
}) {
  const receipts = useApiPagedQuery<PharmacyMovement>(
    pharmacyKey(scope, "stock", "cost-receipts"),
    `${pharmacyRoot(scope)}/stock/movements`,
  );
  return (
    <div>
      <Field id={id} label="Inventory receipt" error={error}>
        <SelectControl
          id={id}
          value={value}
          onValueChange={onChange}
          placeholder="Select original receiving line"
          options={receipts.items
            .filter((row) => row.kind === "receipt")
            .map((row) => ({
              value: row.id,
              label: `${row.source_reference} · ${row.quantity_units} units`,
            }))}
          disabled={receipts.isPending}
        />
      </Field>
      {receipts.isError && (
        <PharmacyFailure message="Could not load receiving lines." retry={receipts.retry} />
      )}
      <LoadMore
        hasMore={receipts.hasMore}
        isLoading={receipts.isFetchingMore}
        onClick={receipts.loadMore}
      />
    </div>
  );
}
export function PayableHistory({
  scope,
  id,
  writable,
  onClose,
}: {
  scope: PharmacyScope;
  id: string;
  writable: boolean;
  onClose: () => void;
}) {
  const detail = useApiQuery<PharmacyPayableDetail>(
    pharmacyKey(scope, "accounts", "detail", id),
    `${pharmacyRoot(scope)}/accounts/payables/${id}`,
  );
  const history = useApiPagedQuery<PharmacySettlement>(
    pharmacyKey(scope, "accounts", "settlements", id),
    `${pharmacyRoot(scope)}/accounts/payables/${id}/settlements`,
  );
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    if (detail.data?.id) {
      heading.current?.scrollIntoView({ block: "start", behavior: "instant" });
      heading.current?.focus({ preventScroll: true });
    }
  }, [detail.data?.id]);
  if (detail.isError)
    return (
      <PharmacyFailure message="Could not load this financial document." retry={detail.refetch} />
    );
  if (!detail.data) return <p role="status">Loading document…</p>;
  const doc = detail.data;
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h3
            ref={heading}
            tabIndex={-1}
            className="scroll-mt-24 text-lg font-semibold outline-none"
          >
            {doc.document_reference}
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">
            {doc.payee_name} · {doc.payee_reference}
          </p>
        </div>
        <Button variant="outline" onClick={onClose}>
          Close document
        </Button>
      </div>
      <dl className="grid gap-4 rounded-xl border bg-muted/20 p-4 sm:grid-cols-2 lg:grid-cols-4">
        {(
          [
            ["Billed", doc.total_minor],
            ["Paid", doc.paid_minor],
            ["Credits", doc.credit_minor],
            ["Outstanding", doc.outstanding_minor],
          ] as const
        ).map(([label, value]) => (
          <div key={label}>
            <dt className="text-xs text-muted-foreground">{label}</dt>
            <dd className="mt-1 font-semibold tabular-nums">
              {formatPharmacyMoney(value, doc.currency)}
            </dd>
          </div>
        ))}
      </dl>
      <div className="text-xs leading-6 text-muted-foreground">
        Document date {doc.issued_on} · Due {doc.due_on}
        <br />
        Recorded by {doc.actor_subject} · {formatDateTime(doc.created_at)}
      </div>
      <ol className="divide-y rounded-xl border">
        {doc.lines.map((line, i) => (
          <li key={i} className="p-4">
            <p className="text-sm font-medium">{line.description}</p>
            <p className="mt-1 text-xs leading-6 text-muted-foreground">
              {line.category} · Net {formatPharmacyMoney(line.net_minor, doc.currency)} · Tax{" "}
              {formatPharmacyMoney(line.tax_minor, doc.currency)} · Recoverable tax{" "}
              {formatPharmacyMoney(line.recoverable_tax_minor, doc.currency)} · Landed{" "}
              {formatPharmacyMoney(line.landed_minor, doc.currency)}
            </p>
            {line.receipt_id && (
              <p className="mt-1 break-all text-xs text-muted-foreground">
                Receipt {line.receipt_id}
              </p>
            )}
          </li>
        ))}
      </ol>
      <div>
        <h4 className="mb-3 font-semibold">Payments, credits & reversals</h4>
        {history.isError ? (
          <PharmacyFailure message="Could not load settlement history." retry={history.retry} />
        ) : history.isPending ? (
          <p role="status">Loading settlements…</p>
        ) : !history.items.length ? (
          <p className="text-sm text-muted-foreground">No settlements recorded.</p>
        ) : (
          <ol className="divide-y rounded-xl border">
            {history.items.map((entry) => (
              <li key={entry.id} className="p-4">
                <div className="flex flex-wrap justify-between gap-3 text-sm">
                  <strong>
                    {entry.kind.replaceAll("_", " ")} ·{" "}
                    {formatPharmacyMoney(entry.amount_minor, entry.currency)}
                  </strong>
                  <span className="text-muted-foreground">{entry.occurred_on}</span>
                </div>
                <p className="mt-2 text-sm">
                  {entry.paid_by} → {doc.payee_name} · {entry.method}
                </p>
                <p className="mt-1 break-all text-xs text-muted-foreground">
                  {entry.source_reference} · Recorded by {entry.actor_subject} ·{" "}
                  {formatDateTime(entry.created_at)}
                </p>
                <p className="mt-2 text-xs text-muted-foreground">{entry.reason}</p>
                {entry.reversal_of && (
                  <p className="mt-1 break-all text-xs text-muted-foreground">
                    Reverses payment {entry.reversal_of}
                  </p>
                )}
              </li>
            ))}
          </ol>
        )}
        <LoadMore
          hasMore={history.hasMore}
          isLoading={history.isFetchingMore}
          onClick={history.loadMore}
        />
      </div>
      {writable && (
        <SettlementForm
          key={`${id}:${history.items.length}`}
          scope={scope}
          doc={doc}
          payments={history.items.filter(
            (row) =>
              row.kind === "payment" &&
              !history.items.some((reversal) => reversal.reversal_of === row.id),
          )}
        />
      )}
    </div>
  );
}
function SettlementForm({
  scope,
  doc,
  payments,
}: {
  scope: PharmacyScope;
  doc: PharmacyPayableDetail;
  payments: PharmacySettlement[];
}) {
  const { can } = usePermissions();
  const [id] = useState(() => newPharmacyId());
  const choices = [
    ...(can("pharmacy.accounts.payment") ? [{ value: "payment", label: "Record payment" }] : []),
    ...(can("pharmacy.accounts.adjust")
      ? [
          { value: "credit", label: "Credit note" },
          { value: "payment_reversal", label: "Reverse recorded payment" },
        ]
      : []),
  ];
  const form = useZodForm(pharmacySettlementInputSchema, {
    kind: can("pharmacy.accounts.payment") ? "payment" : "credit",
    currency: doc.currency,
    amount_minor: 0,
    paid_by: "",
    method: "bank",
    source_reference: "",
    occurred_on: "",
    reason: "",
    reversal_of: null,
  });
  const kind = form.watch("kind");
  const save = useApiMutation(
    (values: PharmacySettlementInput) =>
      apiFetch(
        `${pharmacyRoot(scope)}/accounts/payables/${doc.id}/${values.kind === "payment" ? "payments" : "adjustments"}/${id}`,
        { method: "PUT", body: JSON.stringify(values) },
      ),
    {
      invalidate: [pharmacyKey(scope, "accounts")],
      successToast: "Settlement recorded",
      suppressErrorToast: true,
    },
  );
  if (!choices.length) return null;
  return (
    <form
      noValidate
      onSubmit={form.handleSubmit((values) => save.mutate(values))}
      className="space-y-4 rounded-xl border p-4"
    >
      <h4 className="font-semibold">Record a settlement</h4>
      <p className="text-sm font-medium">
        Outstanding: {formatPharmacyMoney(doc.outstanding_minor, doc.currency)}
      </p>
      <p className="text-sm leading-6 text-muted-foreground">
        Record an external payment or approved credit. This form does not transfer money or verify
        bank settlement. Use a unique source reference from the original evidence.
      </p>
      <fieldset disabled={save.isPending} className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <Field id="settlement-kind" label="Record type">
          <SelectControl
            id="settlement-kind"
            value={kind}
            onValueChange={(value) => {
              form.setValue("kind", value as PharmacySettlementInput["kind"]);
              form.setValue("reversal_of", null);
              form.setValue("amount_minor", 0);
            }}
            options={choices}
          />
        </Field>
        <Field
          id="settlement-payer"
          label={kind === "payment" ? "Paid by" : "Responsible person"}
          error={form.formState.errors.paid_by?.message}
        >
          <Input id="settlement-payer" {...form.register("paid_by")} maxLength={200} />
        </Field>
        <FormSelectField
          control={form.control}
          name="method"
          id="settlement-method"
          label="Payment method"
          options={["bank", "cash", "card", "upi", "other"].map((value) => ({
            value,
            label: value === "upi" ? "UPI" : value[0].toUpperCase() + value.slice(1),
          }))}
        />
        <Field
          id="settlement-reference"
          label="Settlement reference"
          error={form.formState.errors.source_reference?.message}
        >
          <Input id="settlement-reference" {...form.register("source_reference")} maxLength={100} />
        </Field>
        <Field
          id="settlement-date"
          label="Settlement date"
          error={form.formState.errors.occurred_on?.message}
        >
          <Input id="settlement-date" type="date" {...form.register("occurred_on")} />
        </Field>
        <CurrencyAmountField
          key={kind + ":" + form.watch("reversal_of")}
          id="settlement-amount"
          label="Settlement amount"
          currency={doc.currency}
          initialMinor={form.getValues("amount_minor")}
          onChange={(value) => form.setValue("amount_minor", value, { shouldValidate: true })}
          error={form.formState.errors.amount_minor?.message}
        />
        {kind === "payment_reversal" && (
          <Field
            id="settlement-original"
            label="Original payment"
            error={form.formState.errors.reversal_of?.message}
          >
            <SelectControl
              id="settlement-original"
              value={form.watch("reversal_of") ?? ""}
              onValueChange={(value) => {
                form.setValue("reversal_of", value, { shouldValidate: true });
                form.setValue(
                  "amount_minor",
                  payments.find((p) => p.id === value)?.amount_minor ?? 0,
                );
              }}
              placeholder="Choose payment to reverse"
              options={payments.map((p) => ({
                value: p.id,
                label: `${p.source_reference} · ${formatPharmacyMoney(p.amount_minor, p.currency)}`,
              }))}
            />
          </Field>
        )}
        <div className="sm:col-span-2 lg:col-span-3">
          <Field
            id="settlement-reason"
            label="Reason / evidence note"
            error={form.formState.errors.reason?.message}
          >
            <Input id="settlement-reason" {...form.register("reason")} maxLength={500} />
          </Field>
        </div>
      </fieldset>
      {save.isError && (
        <p role="alert" className="text-sm text-destructive">
          {save.error.message}
        </p>
      )}
      <Button type="submit" disabled={save.isPending}>
        {save.isPending ? "Recording…" : "Save settlement"}
      </Button>
    </form>
  );
}
