"use client";
import { useEffect, useRef } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  formatPaise,
  staffSalaryPreviewFormSchema,
  toStaffSalaryPreviewInput,
  useApiMutation,
  SALARY_HEAD_KINDS,
  STATUTORY_GENDERS,
  STATUTORY_KINDS,
  STATUTORY_RESULT_STATUSES,
  TAX_REGIMES,
  type StaffSalaryPreview as StaffSalaryPreviewResult,
  type StaffSalaryPreviewInput,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { Check, Fact } from "./payroll-workspace";
import { PeriodLine } from "./statutory-panel";

export function StaffPayPreview({ path, onClose }: { path: string; onClose: () => void }) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const form = useZodForm(staffSalaryPreviewFormSchema, {
    on: today(),
    payable_days: "",
    disability: false,
    gender: "",
  });
  const preview = useApiMutation(
    (input: StaffSalaryPreviewInput) =>
      apiFetch<StaffSalaryPreviewResult>(`${path}/salary-preview`, {
        method: "POST",
        body: JSON.stringify(input),
      }),
    { suppressErrorToast: true },
  );
  const errors = form.formState.errors;
  const value = form.watch();
  const result = preview.data;
  return (
    <div role="region" aria-label="Pay preview" className="border-t p-5">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          Preview pay
        </h4>
        <Button variant="ghost" onClick={onClose}>
          Close preview
        </Button>
      </div>
      <form
        noValidate
        autoComplete="off"
        aria-label="Pay preview inputs"
        onSubmit={form.handleSubmit((input) => preview.mutate(toStaffSalaryPreviewInput(input)))}
        className="grid items-end gap-4 sm:grid-cols-[1fr_1fr_1fr_auto]"
      >
        <Field id="staff-preview-on" label="Month containing" error={errors.on?.message}>
          <input
            id="staff-preview-on"
            type="date"
            className={inputClass}
            aria-invalid={!!errors.on}
            {...form.register("on")}
          />
        </Field>
        <Field id="staff-preview-days" label="Payable days" error={errors.payable_days?.message}>
          <input
            id="staff-preview-days"
            inputMode="decimal"
            className={inputClass}
            aria-invalid={!!errors.payable_days}
            {...form.register("payable_days")}
          />
        </Field>
        <Field id="staff-preview-gender" label="Gender (professional tax)">
          <SelectControl
            id="staff-preview-gender"
            value={value.gender}
            onValueChange={(next) =>
              form.setValue("gender", next as typeof value.gender, { shouldDirty: true })
            }
            options={[
              { value: "", label: "Not stated" },
              ...Object.entries(STATUTORY_GENDERS).map(([g, label]) => ({ value: g, label })),
            ]}
          />
        </Field>
        <Button type="submit" disabled={preview.isPending}>
          {preview.isPending ? "Computing…" : "Compute pay"}
        </Button>
        <div className="sm:col-span-4">
          <Check
            label="Employee is a person with disability (uses the higher ESI ceiling)"
            checked={value.disability}
            onChange={(on) => form.setValue("disability", on, { shouldDirty: true })}
          />
        </div>
      </form>
      <p className="mt-2 text-xs text-muted-foreground">
        Uses the approved pay, structure and statutory rules in force on that date. Nothing is
        stored.
      </p>
      {preview.isError && <SaveError message={preview.error.message} />}
      {result && (
        <div className="mt-5 space-y-4">
          <p className="text-sm">
            Tax year {result.tax_year}:{" "}
            {result.tax_regime ? (
              TAX_REGIMES[result.tax_regime]
            ) : (
              <span className="font-medium text-amber-900">
                no regime recorded. Record the employee&apos;s intimation before payroll deducts
                tax.
              </span>
            )}
          </p>
          <table className="w-full text-sm">
            <caption className="sr-only">Pay lines on {result.salary.on}</caption>
            <thead>
              <tr className="border-b text-left text-xs text-muted-foreground">
                <th className="py-2 font-medium">Head</th>
                <th className="py-2 font-medium">Kind</th>
                <th className="py-2 text-right font-medium">Amount</th>
              </tr>
            </thead>
            <tbody>
              {result.salary.lines
                .filter((l) => l.shown)
                .map((l) => (
                  <tr key={l.head_id} className="border-b last:border-0">
                    <td className="py-2">{l.payslip_label}</td>
                    <td className="py-2">{SALARY_HEAD_KINDS[l.kind]}</td>
                    <td className="py-2 text-right tabular-nums">{formatPaise(l.amount_paise)}</td>
                  </tr>
                ))}
            </tbody>
          </table>
          <ul className="space-y-1 text-sm">
            {result.results.map((r) => (
              <li key={r.kind}>
                {STATUTORY_KINDS[r.kind]} · {STATUTORY_RESULT_STATUSES[r.status]}
                {r.period && <PeriodLine period={r.period} />}
              </li>
            ))}
          </ul>
          <dl className="grid gap-4 text-sm sm:grid-cols-4">
            <Fact label="Gross" value={formatPaise(result.salary.totals.gross_paise)} />
            <Fact label="Employee contributions" value={formatPaise(result.employee_paise)} />
            <Fact label="Employer contributions" value={formatPaise(result.employer_paise)} />
            <Fact
              label="Net after contributions"
              value={formatPaise(result.net_after_statutory_paise)}
            />
          </dl>
        </div>
      )}
    </div>
  );
}
