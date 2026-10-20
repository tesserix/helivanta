"use client";
import { useEffect, useRef } from "react";
import { Button } from "@tesserix/web";
import {
  benefitCoverageExportFormSchema,
  benefitPlansKey,
  benefitPlansPath,
  fetchBenefitCoverageCSV,
  onTenantTransition,
  tenantTransitionPending,
  useApiMutation,
  useApiPagedQuery,
  type BenefitCoverageExportForm,
  type BenefitPlan,
  type Hospital,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";

const rowsText = (rows: number) => `${rows} coverage ${rows === 1 ? "row" : "rows"}`;

export function CoverageExport({ hospital }: { hospital: Hospital }) {
  const plans = useApiPagedQuery<BenefitPlan>(
    benefitPlansKey(hospital.id),
    benefitPlansPath(hospital.id),
  );
  const form = useZodForm(benefitCoverageExportFormSchema, { as_of: today(), plan_id: "" });
  const controller = useRef<AbortController | null>(null);
  // A file that arrives after the hospital or workspace changed must never download.
  useEffect(() => {
    const unsubscribe = onTenantTransition(() => {
      if (tenantTransitionPending()) controller.current?.abort();
    });
    return () => {
      unsubscribe();
      controller.current?.abort();
    };
  }, []);
  const exporting = useApiMutation(
    async (filters: BenefitCoverageExportForm) => {
      if (tenantTransitionPending()) throw new DOMException("Workspace changed", "AbortError");
      const active = new AbortController();
      controller.current = active;
      const file = await fetchBenefitCoverageCSV(hospital.id, filters, active.signal);
      const url = URL.createObjectURL(file.blob);
      try {
        const link = document.createElement("a");
        link.href = url;
        link.download = file.filename;
        link.click();
      } finally {
        URL.revokeObjectURL(url);
      }
      return file;
    },
    { successToast: "Coverage exported", suppressErrorToast: true },
  );
  const failure = exporting.error?.name === "AbortError" ? null : exporting.error;
  const errors = form.formState.errors;
  return (
    <section
      aria-label="Coverage export"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="border-b p-5">
        <h3 className="font-semibold">Coverage export</h3>
        <p className="mt-1 text-xs text-muted-foreground">
          {hospital.name} · A CSV of who each plan covers on one date. Every export is recorded.
        </p>
      </div>
      <form
        noValidate
        autoComplete="off"
        aria-label="Export coverage"
        className="space-y-4 p-5"
        onSubmit={form.handleSubmit((filters) => exporting.mutate(filters))}
      >
        <fieldset disabled={exporting.isPending} className="grid gap-5 sm:grid-cols-2">
          <Field
            id="coverage-export-as-of"
            label="Coverage in force on"
            error={errors.as_of?.message}
          >
            <input
              id="coverage-export-as-of"
              type="date"
              className={inputClass}
              aria-invalid={!!errors.as_of}
              {...form.register("as_of")}
            />
          </Field>
          <Field id="coverage-export-plan" label="Plan" error={errors.plan_id?.message}>
            <SelectControl
              id="coverage-export-plan"
              value={form.watch("plan_id")}
              onValueChange={(next) => form.setValue("plan_id", next, { shouldValidate: true })}
              options={[
                { value: "", label: "All plans" },
                ...plans.items.map((p) => ({ value: p.id, label: `${p.name} · ${p.code}` })),
              ]}
            />
          </Field>
          {plans.hasMore && (
            <div className="sm:col-span-2">
              <Button
                type="button"
                variant="outline"
                disabled={plans.isFetchingMore}
                onClick={plans.loadMore}
              >
                Load more plans
              </Button>
            </div>
          )}
        </fieldset>
        {exporting.data && !exporting.isPending && (
          <p role="status" className="text-sm text-muted-foreground">
            {`${exporting.data.filename} · ${rowsText(exporting.data.rows)}`}
          </p>
        )}
        {failure && <SaveError message={failure.message} />}
        <div className="flex justify-end">
          <Button type="submit" disabled={exporting.isPending}>
            {exporting.isPending ? "Exporting…" : "Export coverage"}
          </Button>
        </div>
      </form>
    </section>
  );
}
