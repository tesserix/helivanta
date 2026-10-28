"use client";

import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Landmark } from "lucide-react";
import {
  apiFetch,
  countryProfile,
  pharmacyKey,
  pharmacyRoot,
  pharmacyTaxRegistrationInputSchema,
  taxScheme,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  validateTaxRegistration,
  type PharmacyOutlet,
  type PharmacyScope,
  type PharmacyTaxRegistration,
  type TaxScheme,
} from "@helivanta/api";
import { Field, FormSelectField, LoadMore, Panel, QueryState, useZodForm } from "@helivanta/ui";
import { useReportState, type FormState } from "@/lib/use-list-detail";

const registrationMessages = {
  format: "Enter the 15-character GSTIN.",
  checksum: "This GSTIN's last character does not match. Check it for a typing mistake.",
  jurisdiction: "This GSTIN is registered in a different state.",
} as const;

function registrationForm(scheme: TaxScheme, history: PharmacyTaxRegistration[]) {
  return pharmacyTaxRegistrationInputSchema
    .extend({ reason: z.string().trim().max(500) })
    .superRefine((value, ctx) => {
      const problem = validateTaxRegistration(scheme, value.jurisdiction_code, value.identifier);
      if (problem)
        ctx.addIssue({
          code: "custom",
          path: ["identifier"],
          message: registrationMessages[problem],
        });
      // Recording a second row for the same date replaces the first, so say why.
      if (!value.reason && history.some((row) => row.effective_from === value.effective_from))
        ctx.addIssue({
          code: "custom",
          path: ["reason"],
          message: "A registration already starts on this date. Say why you are correcting it.",
        });
    });
}

export function TaxRegistrationPanel({
  scope,
  outlet,
  today,
  onStateChange,
}: {
  scope: PharmacyScope;
  outlet: PharmacyOutlet;
  /** The outlet's own date: a registration cannot start before it. */
  today: string;
  /** An unsaved registration holds the outlet open (#1303 E6). */
  onStateChange?: FormState;
}) {
  const scheme = taxScheme(outlet.country_code);
  const key = pharmacyKey(scope, "tax-registrations");
  const history = useApiPagedQuery<PharmacyTaxRegistration>(
    key,
    `${pharmacyRoot(scope)}/tax-registrations`,
  );
  // Asked of the server: the row in force need not be on the first history page.
  const current = useApiQuery<{ registration: PharmacyTaxRegistration | null }>(
    [...key, "in-force"],
    `${pharmacyRoot(scope)}/tax-registrations/in-force`,
    { enabled: !!scheme },
  );
  if (!scheme)
    return (
      <Panel aria-labelledby="tax-registration-heading" className="mt-6">
        <h3 id="tax-registration-heading" className="font-semibold">
          Tax registration
        </h3>
        <p className="mt-2 text-sm text-muted-foreground">
          Tax setup is not available for{" "}
          {countryProfile(outlet.country_code)?.name ?? outlet.country_code} yet.
        </p>
      </Panel>
    );
  const jurisdictions = new Map<string, string>(scheme.jurisdictions.map((j) => [j.code, j.name]));
  const inForce = current.data?.registration;
  return (
    <Panel aria-labelledby="tax-registration-heading" className="mt-6">
      <h3 id="tax-registration-heading" className="font-semibold">
        Tax registration
      </h3>
      <p className="mt-1 text-sm text-muted-foreground">
        The {scheme.registration_kind.toUpperCase()} {outlet.name} sells under. Format checked, not
        verified with GSTN.
      </p>
      {inForce && (
        <Panel className="mt-4">
          <p className="text-sm text-muted-foreground">In force today</p>
          <p className="mt-1 font-medium tabular-nums">{inForce.identifier}</p>
          <p className="mt-1 text-sm text-muted-foreground">
            {jurisdictions.get(inForce.jurisdiction_code) ?? inForce.jurisdiction_code} · from{" "}
            {inForce.effective_from}
          </p>
        </Panel>
      )}
      <RegistrationForm
        key={history.items.length}
        scheme={scheme}
        history={history.items}
        today={today}
        onStateChange={onStateChange}
        submit={(values) =>
          apiFetch<PharmacyTaxRegistration>(`${pharmacyRoot(scope)}/tax-registrations`, {
            method: "POST",
            body: JSON.stringify(values),
          })
        }
        invalidate={key}
      />
      <h4 className="mt-6 mb-2 text-sm font-medium">History</h4>
      <QueryState
        query={history}
        label="tax registrations"
        empty={{
          icon: Landmark,
          title: "No registration yet",
          hint: "Record the outlet's GSTIN above.",
        }}
      >
        {(rows) => (
          <ul className="divide-y text-sm">
            {rows.map((row) => (
              <li key={row.id} className="flex flex-wrap items-center justify-between gap-3 py-3">
                <div className="min-w-0">
                  <p className="font-medium tabular-nums">{row.identifier}</p>
                  <p className="mt-0.5 text-muted-foreground">
                    {jurisdictions.get(row.jurisdiction_code) ?? row.jurisdiction_code} · from{" "}
                    {row.effective_from} · {row.recorded_by}
                    {row.reason ? ` · ${row.reason}` : ""}
                  </p>
                </div>
                {row.in_force ? (
                  <span className="rounded-full bg-muted px-3 py-1 text-xs font-medium">
                    In force
                  </span>
                ) : row.replaced ? (
                  <span className="rounded-full bg-muted px-3 py-1 text-xs text-muted-foreground">
                    Replaced
                  </span>
                ) : row.effective_from > today ? (
                  <span className="rounded-full bg-muted px-3 py-1 text-xs text-muted-foreground">
                    Upcoming
                  </span>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </QueryState>
      <LoadMore
        label="Load more registrations"
        hasMore={history.hasMore}
        isLoading={history.isFetchingMore}
        onClick={history.loadMore}
      />
    </Panel>
  );
}

type RegistrationValues = z.infer<ReturnType<typeof registrationForm>>;

function RegistrationForm({
  scheme,
  history,
  today,
  submit,
  invalidate,
  onStateChange,
}: {
  scheme: TaxScheme;
  history: PharmacyTaxRegistration[];
  today: string;
  submit: (values: Record<string, string>) => Promise<PharmacyTaxRegistration>;
  invalidate: unknown[];
  onStateChange?: FormState;
}) {
  const form = useZodForm(registrationForm(scheme, history), {
    jurisdiction_code: "",
    identifier: "",
    effective_from: today,
    reason: "",
  });
  const record = useApiMutation(submit, {
    successToast: "Registration recorded",
    invalidate: [invalidate],
    suppressErrorToast: true,
  });
  const errors = form.formState.errors;
  const onSubmit = (values: RegistrationValues) => {
    const { reason, ...rest } = values;
    record.mutate(reason ? { ...rest, reason } : rest);
  };
  useReportState(onStateChange, form.formState.isDirty, record.isPending);
  return (
    <form noValidate onSubmit={form.handleSubmit(onSubmit)} className="mt-5 space-y-4">
      <div className="grid gap-4 sm:grid-cols-3">
        <FormSelectField
          control={form.control}
          name="jurisdiction_code"
          id="tax-jurisdiction"
          label="State or union territory"
          options={scheme.jurisdictions.map((j) => ({
            value: j.code,
            label: `${j.code} ${j.name}`,
          }))}
        />
        <Field id="tax-identifier" label="GSTIN" error={errors.identifier?.message}>
          <Input
            id="tax-identifier"
            autoComplete="off"
            aria-invalid={!!errors.identifier}
            aria-describedby={errors.identifier ? "tax-identifier-error" : undefined}
            {...form.register("identifier")}
          />
        </Field>
        <Field id="tax-effective" label="In force from" error={errors.effective_from?.message}>
          <Input id="tax-effective" type="date" min={today} {...form.register("effective_from")} />
        </Field>
      </div>
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
        {record.isPending ? "Recording…" : "Record registration"}
      </Button>
    </form>
  );
}
