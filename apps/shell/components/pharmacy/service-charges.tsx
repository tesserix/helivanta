"use client";

import { useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Truck } from "lucide-react";
import {
  apiFetch,
  newPharmacyId,
  pharmacyKey,
  pharmacyRoot,
  useApiMutation,
  useApiQuery,
  type PharmacyScope,
  type PharmacyServiceCharge,
  type PharmacyServiceChargeVersion,
} from "@helivanta/api";
import { Field, FormSelectField, Panel, RequestFailure, useZodForm } from "@helivanta/ui";
import { CurrencyAmountField, formatPharmacyMoney } from "./currency-amount-field";

// Service charges (#1388): each version declared by one person and approved
// by another. How a charge is taxed is the accountant's recorded decision.

function versionLabel(v: PharmacyServiceChargeVersion, currency: string): string {
  const tax =
    v.tax_treatment === "principal_supply"
      ? "taxed as the medicines"
      : `SAC ${v.sac_code}, ${v.supply_nature === "taxable" ? `${(v.rate_ppm ?? 0) / 10000}% GST` : v.supply_nature === "exempt" ? "exempt" : "nil rated"}`;
  return `${v.name} · ${formatPharmacyMoney(v.amount_minor, currency)} · ${v.channel === "ward_delivery" ? "ward delivery" : "counter"} · ${tax}${v.status === "withdrawn" ? " · withdrawn" : ""}`;
}

export function ServiceChargesSetup({
  scope,
  currency,
  today,
  canDeclare,
  canReview,
}: {
  scope: PharmacyScope;
  currency: string;
  today: string;
  canDeclare: boolean;
  canReview: boolean;
}) {
  const key = pharmacyKey(scope, "counter", "service-charges");
  const root = `${pharmacyRoot(scope)}/service-charges`;
  const charges = useApiQuery<{ items: PharmacyServiceCharge[] }>(key, root);
  const [adding, setAdding] = useState(false);
  return (
    <Panel>
      <section aria-labelledby="service-charges-heading">
        <h3 id="service-charges-heading" className="flex items-center gap-2 font-semibold">
          <Truck className="size-4" />
          Service charges
        </h3>
        <p className="mt-1 text-sm text-muted-foreground">
          A charge is billed as its own invoice line, never inside a medicine&apos;s price. Your
          accountant decides how each is taxed; a second person approves every change.
        </p>
        {charges.isError ? (
          <RequestFailure
            message="Could not load the service charges."
            retry={charges.refetch}
            error={charges.error}
          />
        ) : !charges.data ? (
          <p role="status" className="mt-3 text-sm text-muted-foreground">
            Loading service charges…
          </p>
        ) : (
          <ul className="mt-4 space-y-4">
            {charges.data.items.map((c) => (
              <ChargeRow
                key={c.id}
                charge={c}
                currency={currency}
                today={today}
                root={root}
                invalidate={[key]}
                canDeclare={canDeclare}
                canReview={canReview}
              />
            ))}
          </ul>
        )}
        {canDeclare &&
          (adding ? (
            <VersionForm
              root={root}
              currency={currency}
              today={today}
              invalidate={[key]}
              onDone={() => setAdding(false)}
            />
          ) : (
            <Button className="mt-4" variant="outline" onClick={() => setAdding(true)}>
              New service charge
            </Button>
          ))}
      </section>
    </Panel>
  );
}

function ChargeRow({
  charge,
  currency,
  today,
  root,
  invalidate,
  canDeclare,
  canReview,
}: {
  charge: PharmacyServiceCharge;
  currency: string;
  today: string;
  root: string;
  invalidate: unknown[][];
  canDeclare: boolean;
  canReview: boolean;
}) {
  const [editing, setEditing] = useState(false);
  const latest = charge.latest;
  return (
    <li className="rounded-md border p-3 text-sm">
      <p className="font-medium">{charge.code}</p>
      <p className="mt-0.5">
        {charge.in_force
          ? `Billed today: ${versionLabel(charge.in_force, currency)}`
          : "Not billed today"}
      </p>
      {latest && latest.decision !== "approved" && (
        <p className="mt-0.5 text-muted-foreground">
          Latest: {versionLabel(latest, currency)}, from {latest.effective_from} ·{" "}
          {latest.decision === null
            ? `awaiting review, declared by ${latest.declared_by}`
            : `rejected by ${latest.reviewed_by}: ${latest.decision_reason}`}
        </p>
      )}
      {canReview && latest && latest.decision === null && (
        <ReviewForm
          key={latest.id}
          version={latest}
          path={`${root}/${charge.id}`}
          invalidate={invalidate}
        />
      )}
      {canDeclare &&
        (editing ? (
          <VersionForm
            key={latest?.id ?? "none"}
            chargeId={charge.id}
            root={root}
            currency={currency}
            today={today}
            from={latest}
            code={charge.code}
            invalidate={invalidate}
            onDone={() => setEditing(false)}
          />
        ) : (
          <Button className="mt-2" variant="ghost" onClick={() => setEditing(true)}>
            Change {charge.code}
          </Button>
        ))}
    </li>
  );
}

const versionForm = z
  .object({
    code: z
      .string()
      .trim()
      .toUpperCase()
      .regex(/^[A-Z][A-Z0-9_]{1,19}$/, "2 to 20 capital letters, digits or underscores"),
    name: z.string().trim().min(1, "Enter the name printed on the invoice").max(80),
    channel: z.enum(["counter", "ward_delivery"]),
    amount_minor: z.number(),
    status: z.enum(["available", "withdrawn"]),
    tax_treatment: z.enum(["own_service", "principal_supply"]),
    sac_code: z.string().trim(),
    supply_nature: z.enum(["taxable", "nil_rated", "exempt"]),
    rate_percent: z.string().trim(),
    exemption_basis: z.string().trim().max(500),
    tax_basis: z
      .string()
      .trim()
      .min(5, "Say who decided the tax treatment and on what authority")
      .max(500),
    effective_from: z.string().date("Enter a date"),
  })
  .superRefine((v, ctx) => {
    if (!Number.isSafeInteger(v.amount_minor) || v.amount_minor < 1 || v.amount_minor > 100000000)
      ctx.addIssue({ code: "custom", path: ["amount_minor"], message: "Enter the amount charged" });
    if (v.tax_treatment === "principal_supply") return;
    if (!/^99\d{4}$/.test(v.sac_code))
      ctx.addIssue({ code: "custom", path: ["sac_code"], message: "A SAC is 99 and four digits" });
    if (v.supply_nature === "taxable" && !/^\d{1,3}(\.\d{1,2})?$/.test(v.rate_percent))
      ctx.addIssue({
        code: "custom",
        path: ["rate_percent"],
        message: "Enter the GST rate, at most two decimals",
      });
    if (v.supply_nature === "exempt" && !v.exemption_basis)
      ctx.addIssue({
        code: "custom",
        path: ["exemption_basis"],
        message: "Name the exemption notification and entry",
      });
  });

type VersionValues = z.infer<typeof versionForm>;

function body(v: VersionValues) {
  const { rate_percent, sac_code, supply_nature, exemption_basis, ...rest } = v;
  if (v.tax_treatment === "principal_supply") return rest;
  return {
    ...rest,
    sac_code,
    supply_nature,
    rate_ppm: supply_nature === "taxable" ? Math.round(Number(rate_percent) * 10000) : 0,
    ...(supply_nature === "exempt" ? { exemption_basis } : {}),
  };
}

function VersionForm({
  chargeId,
  root,
  currency,
  today,
  from,
  code,
  invalidate,
  onDone,
}: {
  /** Absent for a new charge, which takes a fresh id once. */
  chargeId?: string;
  root: string;
  currency: string;
  today: string;
  from?: PharmacyServiceChargeVersion | null;
  code?: string;
  invalidate: unknown[][];
  onDone: () => void;
}) {
  const form = useZodForm(versionForm, {
    code: code ?? "",
    name: from?.name ?? "",
    channel: from?.channel ?? "counter",
    amount_minor: from?.amount_minor ?? 0,
    status: from?.status ?? "available",
    tax_treatment: from?.tax_treatment ?? "principal_supply",
    sac_code: from?.sac_code ?? "",
    supply_nature: from?.supply_nature ?? "taxable",
    rate_percent: from?.rate_ppm ? String(from.rate_ppm / 10000) : "",
    exemption_basis: from?.exemption_basis ?? "",
    tax_basis: from?.tax_basis ?? "",
    effective_from: today,
  });
  // One id per charge and version, so a retried submit replays rather than records twice.
  const [charge] = useState(() => chargeId ?? newPharmacyId());
  const [id] = useState(() => newPharmacyId());
  const record = useApiMutation(
    (b: ReturnType<typeof body>) =>
      apiFetch<PharmacyServiceChargeVersion>(`${root}/${charge}/versions/${id}`, {
        method: "PUT",
        body: JSON.stringify(b),
      }),
    { successToast: "Version recorded", invalidate, onSuccess: onDone, suppressErrorToast: true },
  );
  const errors = form.formState.errors;
  const own = form.watch("tax_treatment") === "own_service";
  const nature = form.watch("supply_nature");
  return (
    <form
      noValidate
      onSubmit={form.handleSubmit((v) => record.mutate(body(v)))}
      className="mt-3 space-y-4 rounded-md border p-3"
    >
      <div className="grid gap-4 sm:grid-cols-3">
        <Field id="charge-code" label="Code" error={errors.code?.message}>
          <Input id="charge-code" readOnly={code !== undefined} {...form.register("code")} />
        </Field>
        <Field id="charge-name" label="Name on the invoice" error={errors.name?.message}>
          <Input id="charge-name" {...form.register("name")} />
        </Field>
        <CurrencyAmountField
          id="charge-amount"
          label="Amount, GST included"
          currency={currency}
          initialMinor={from?.amount_minor ?? 0}
          error={errors.amount_minor?.message}
          onChange={(minor) =>
            form.setValue("amount_minor", minor, { shouldDirty: true, shouldValidate: false })
          }
        />
        <FormSelectField
          control={form.control}
          name="channel"
          id="charge-channel"
          label="Applies to"
          options={[
            { value: "counter", label: "Counter sales" },
            { value: "ward_delivery", label: "Ward delivery (billed to an admission)" },
          ]}
        />
        <FormSelectField
          control={form.control}
          name="status"
          id="charge-status"
          label="Status"
          options={[
            { value: "available", label: "Available" },
            { value: "withdrawn", label: "Withdrawn" },
          ]}
        />
        <Field id="charge-from" label="In force from" error={errors.effective_from?.message}>
          <Input id="charge-from" type="date" {...form.register("effective_from")} />
        </Field>
        <FormSelectField
          control={form.control}
          name="tax_treatment"
          id="charge-treatment"
          label="Tax treatment"
          options={[
            { value: "principal_supply", label: "Taxed as the medicines (composite supply)" },
            { value: "own_service", label: "A service of its own (SAC and rate)" },
          ]}
        />
        {own && (
          <>
            <Field id="charge-sac" label="SAC" error={errors.sac_code?.message}>
              <Input id="charge-sac" inputMode="numeric" {...form.register("sac_code")} />
            </Field>
            <FormSelectField
              control={form.control}
              name="supply_nature"
              id="charge-nature"
              label="Supply"
              options={[
                { value: "taxable", label: "Taxable" },
                { value: "nil_rated", label: "Nil rated" },
                { value: "exempt", label: "Exempt" },
              ]}
            />
            {nature === "taxable" && (
              <Field id="charge-rate" label="GST rate %" error={errors.rate_percent?.message}>
                <Input id="charge-rate" inputMode="decimal" {...form.register("rate_percent")} />
              </Field>
            )}
            {nature === "exempt" && (
              <Field
                id="charge-exemption"
                label="Exemption basis"
                error={errors.exemption_basis?.message}
              >
                <Input id="charge-exemption" {...form.register("exemption_basis")} />
              </Field>
            )}
          </>
        )}
      </div>
      <Field id="charge-basis" label="Tax basis" error={errors.tax_basis?.message}>
        <Input id="charge-basis" {...form.register("tax_basis")} />
      </Field>
      {record.isError && (
        <p role="alert" className="text-sm text-destructive">
          {record.error.message}
        </p>
      )}
      <div className="flex gap-2">
        <Button type="submit" disabled={record.isPending}>
          {record.isPending ? "Recording…" : "Record version"}
        </Button>
        <Button type="button" variant="ghost" onClick={onDone}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

const reviewForm = z.object({
  reason: z.string().trim().min(1, "Say what you checked.").max(500),
});

function ReviewForm({
  version,
  path,
  invalidate,
}: {
  version: PharmacyServiceChargeVersion;
  path: string;
  invalidate: unknown[][];
}) {
  const form = useZodForm(reviewForm, { reason: "" });
  const review = useApiMutation(
    (b: { decision: "approved" | "rejected"; reason: string }) =>
      apiFetch<PharmacyServiceChargeVersion>(`${path}/versions/${version.id}/review`, {
        method: "POST",
        body: JSON.stringify(b),
      }),
    { successToast: "Review recorded", invalidate, suppressErrorToast: true },
  );
  const decide = (decision: "approved" | "rejected") =>
    form.handleSubmit(({ reason }) => review.mutate({ decision, reason }));
  const id = `charge-review-${version.id}`;
  return (
    <form noValidate onSubmit={decide("approved")} className="mt-3 space-y-3 rounded-md border p-3">
      <p>Tax basis: {version.tax_basis}</p>
      <Field id={id} label="What you checked" error={form.formState.errors.reason?.message}>
        <Input id={id} {...form.register("reason")} />
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
