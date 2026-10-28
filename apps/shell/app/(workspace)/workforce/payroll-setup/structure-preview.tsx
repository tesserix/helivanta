"use client";
import { useEffect, useRef } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  formatPaise,
  halfDaysToDays,
  ppmToPercent,
  salaryPreviewFormSchema,
  salaryStructuresPath,
  toSalaryPreviewInput,
  useApiMutation,
  SALARY_HEAD_KINDS,
  type SalaryPreview,
  type SalaryPreviewInput,
  type SalaryStructure,
} from "@helivanta/api";
import { Field, useZodForm } from "@helivanta/ui";
import { inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { Fact } from "./payroll-workspace";

export function StructurePreview({
  base,
  structure,
  onClose,
}: {
  base: string;
  structure: SalaryStructure;
  onClose: () => void;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const form = useZodForm(salaryPreviewFormSchema, { on: today(), payable_days: "" });
  const preview = useApiMutation(
    (input: SalaryPreviewInput) =>
      apiFetch<SalaryPreview>(`${salaryStructuresPath(base)}/${structure.id}/preview`, {
        method: "POST",
        body: JSON.stringify(input),
      }),
    { suppressErrorToast: true },
  );
  const errors = form.formState.errors;
  const result = preview.data;
  return (
    <div role="region" aria-label={`${structure.name} preview`} className="border-t p-5">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          Preview {structure.name}
        </h4>
        <Button variant="ghost" onClick={onClose}>
          Close preview
        </Button>
      </div>
      <form
        noValidate
        autoComplete="off"
        aria-label="Preview inputs"
        onSubmit={form.handleSubmit((input) => preview.mutate(toSalaryPreviewInput(input)))}
        className="grid items-end gap-4 sm:grid-cols-[1fr_1fr_auto]"
      >
        <Field id="salary-preview-on" label="Month containing" error={errors.on?.message}>
          <input
            id="salary-preview-on"
            type="date"
            className={inputClass}
            aria-invalid={!!errors.on}
            {...form.register("on")}
          />
        </Field>
        <Field id="salary-preview-days" label="Payable days" error={errors.payable_days?.message}>
          <input
            id="salary-preview-days"
            inputMode="decimal"
            className={inputClass}
            aria-invalid={!!errors.payable_days}
            {...form.register("payable_days")}
          />
        </Field>
        <Button type="submit" disabled={preview.isPending}>
          {preview.isPending ? "Computing…" : "Compute preview"}
        </Button>
      </form>
      <p className="mt-2 text-xs text-muted-foreground">
        Uses the revisions in force on that date. Nothing is stored.
      </p>
      {preview.isError && <SaveError message={preview.error.message} />}
      {result && (
        <div className="mt-5 space-y-4">
          <table className="w-full text-sm">
            <caption className="sr-only">
              Lines for {halfDaysToDays(result.payable_half_days)} of {result.period_days} days
            </caption>
            <thead>
              <tr className="border-b text-left text-xs text-muted-foreground">
                <th className="py-2 font-medium">Head</th>
                <th className="py-2 font-medium">Kind</th>
                <th className="py-2 text-right font-medium">Amount</th>
              </tr>
            </thead>
            <tbody>
              {result.lines
                .filter((l) => l.shown)
                .map((l) => (
                  <tr key={l.head_id} className="border-b last:border-0">
                    <td className="py-2">
                      {l.payslip_label}
                      {!l.applicable && (
                        <span className="text-xs text-muted-foreground"> · not applicable</span>
                      )}
                    </td>
                    <td className="py-2">{SALARY_HEAD_KINDS[l.kind]}</td>
                    <td className="py-2 text-right tabular-nums">{formatPaise(l.amount_paise)}</td>
                  </tr>
                ))}
            </tbody>
          </table>
          <dl className="grid gap-4 text-sm sm:grid-cols-5">
            <Fact label="Gross" value={formatPaise(result.totals.gross_paise)} />
            <Fact label="Deductions" value={formatPaise(result.totals.deductions_paise)} />
            <Fact label="Net pay" value={formatPaise(result.totals.net_paise)} />
            <Fact
              label="Employer contributions"
              value={formatPaise(result.totals.employer_paise)}
            />
            <Fact label="CTC" value={formatPaise(result.totals.ctc_paise)} />
          </dl>
          {result.totals.negative_net && (
            <p role="alert" className="text-sm text-destructive">
              Deductions exceed gross for these days, so net pay is negative.
            </p>
          )}
          <div className="rounded-xl border bg-muted/30 p-4 text-sm">
            <p className="font-medium">Statutory wage test</p>
            <p className="mt-1 text-muted-foreground">
              Wages {formatPaise(result.wage_test.wages_paise)} · excluded{" "}
              {formatPaise(result.wage_test.excluded_paise)} (
              {ppmToPercent(result.wage_test.excluded_share_ppm)}% of{" "}
              {formatPaise(result.wage_test.remuneration_paise)}) · added back{" "}
              {formatPaise(result.wage_test.add_back_paise)} · statutory wages{" "}
              {formatPaise(result.wage_test.statutory_wages_paise)}
            </p>
          </div>
        </div>
      )}
    </div>
  );
}
