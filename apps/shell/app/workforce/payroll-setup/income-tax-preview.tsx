"use client";
import { useEffect, useRef } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  formatPaise,
  incomeTaxPreviewFormSchema,
  incomeTaxPreviewPath,
  toIncomeTaxPreviewInput,
  useApiMutation,
  INCOME_TAX_RESULT_STATUSES,
  STATUTORY_KINDS,
  type IncomeTaxPreview as IncomeTaxPreviewResult,
  type IncomeTaxPreviewInput,
} from "@helivanta/api";
import { Field, useZodForm } from "@helivanta/ui";
import { inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";

export function IncomeTaxPreview({ base, onClose }: { base: string; onClose: () => void }) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const form = useZodForm(incomeTaxPreviewFormSchema, {
    on: today(),
    annual_income: "",
    age: "",
  });
  const preview = useApiMutation(
    (input: IncomeTaxPreviewInput) =>
      apiFetch<IncomeTaxPreviewResult>(incomeTaxPreviewPath(base), {
        method: "POST",
        body: JSON.stringify(input),
      }),
    { suppressErrorToast: true },
  );
  const errors = form.formState.errors;
  const result = preview.data;
  return (
    <div role="region" aria-label="Income-tax preview" className="border-t p-5">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          Preview income tax
        </h4>
        <Button variant="ghost" onClick={onClose}>
          Close preview
        </Button>
      </div>
      <form
        noValidate
        autoComplete="off"
        aria-label="Income-tax preview inputs"
        onSubmit={form.handleSubmit((input) => preview.mutate(toIncomeTaxPreviewInput(input)))}
        className="grid items-end gap-4 sm:grid-cols-[1fr_2fr_1fr_auto]"
      >
        <Field id="income-tax-preview-on" label="Tax year containing" error={errors.on?.message}>
          <input
            id="income-tax-preview-on"
            type="date"
            className={inputClass}
            aria-invalid={!!errors.on}
            {...form.register("on")}
          />
        </Field>
        <Field
          id="income-tax-preview-income"
          label="Annual salary (₹)"
          error={errors.annual_income?.message}
        >
          <input
            id="income-tax-preview-income"
            inputMode="decimal"
            className={inputClass}
            aria-invalid={!!errors.annual_income}
            {...form.register("annual_income")}
          />
        </Field>
        <Field id="income-tax-preview-age" label="Age (optional)" error={errors.age?.message}>
          <input
            id="income-tax-preview-age"
            inputMode="numeric"
            className={inputClass}
            aria-invalid={!!errors.age}
            {...form.register("age")}
          />
        </Field>
        <Button type="submit" disabled={preview.isPending}>
          {preview.isPending ? "Computing…" : "Compute tax"}
        </Button>
      </form>
      <p className="mt-2 text-xs text-muted-foreground">
        Annual liability under each regime&apos;s rule for that tax year, before any declared
        deductions or exemptions. Nothing is stored. Age is needed only where a regime&apos;s slabs
        differ by age.
      </p>
      {preview.isError && <SaveError message={preview.error.message} />}
      {result && (
        <div className="mt-5 grid gap-4 sm:grid-cols-2">
          {result.results.map((r) => (
            <div key={r.kind} className="rounded-xl border p-4 text-sm">
              <p className="font-medium">
                {STATUTORY_KINDS[r.kind]} · {INCOME_TAX_RESULT_STATUSES[r.status]}
              </p>
              {r.status === "applied" && (
                <table className="mt-3 w-full">
                  <caption className="sr-only">
                    {STATUTORY_KINDS[r.kind]} for tax year {r.tax_year}
                  </caption>
                  <tbody>
                    {(
                      [
                        ["Salary", r.income_paise],
                        ["Standard deduction", -r.standard_deduction_paise],
                        ["Taxable income", r.taxable_paise],
                        ["Tax on slabs", r.slab_tax_paise],
                        ["Rebate", -r.rebate_paise],
                        ["Surcharge", r.surcharge_paise],
                        ["Cess", r.cess_paise],
                      ] as const
                    ).map(([label, paise]) => (
                      <tr key={label} className="border-b">
                        <th scope="row" className="py-2 text-left font-normal">
                          {label}
                        </th>
                        <td className="py-2 text-right tabular-nums">
                          {paise < 0 ? `− ${formatPaise(-paise)}` : formatPaise(paise)}
                        </td>
                      </tr>
                    ))}
                    <tr>
                      <th scope="row" className="py-2 text-left font-semibold">
                        Tax for the year
                      </th>
                      <td className="py-2 text-right font-semibold tabular-nums">
                        {formatPaise(r.total_paise)}
                      </td>
                    </tr>
                  </tbody>
                </table>
              )}
              {r.status === "age_required" && (
                <p className="mt-2 text-xs text-muted-foreground">
                  This regime&apos;s slabs differ by age. Enter the age and compute again.
                </p>
              )}
              {r.status === "tax_year_missing" && (
                <p className="mt-2 text-xs text-muted-foreground">
                  The rule in force covers tax year {r.tax_year}. Propose a revision for this tax
                  year; earlier rates are never carried forward.
                </p>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
