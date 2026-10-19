"use client";
import { useEffect, useRef } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  blankPreviewClaim,
  formatPaise,
  incomeTaxPreviewFormSchema,
  incomeTaxPreviewPath,
  toIncomeTaxPreviewInput,
  useApiMutation,
  HRA_CITIES,
  INCOME_TAX_CLAIM_STATUSES,
  INCOME_TAX_RESULT_STATUSES,
  STATUTORY_KINDS,
  type IncomeTaxPreview as IncomeTaxPreviewResult,
  type IncomeTaxPreviewInput,
} from "@helivanta/api";
import { Field, SelectControl, useFieldArray, useZodForm } from "@helivanta/ui";
import { inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";

export function IncomeTaxPreview({ base, onClose }: { base: string; onClose: () => void }) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const form = useZodForm(incomeTaxPreviewFormSchema, {
    on: today(),
    annual_income: "",
    age: "",
    claims: [],
    hra: { received: "", salary: "", rent_paid: "", city: "" },
  });
  const claims = useFieldArray({ control: form.control, name: "claims", keyName: "fieldKey" });
  const city = form.watch("hra.city");
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
  const amount = (
    id: string,
    label: string,
    name: Parameters<typeof form.register>[0],
    error?: string,
  ) => (
    <Field id={id} label={label} error={error}>
      <input
        id={id}
        inputMode="decimal"
        className={inputClass}
        aria-invalid={!!error}
        {...form.register(name)}
      />
    </Field>
  );
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
        <fieldset className="grid gap-4 sm:col-span-4 sm:grid-cols-4">
          <legend className="mb-2 text-sm font-medium">House rent allowance (optional)</legend>
          {amount(
            "income-tax-preview-hra-received",
            "HRA received in the year (₹)",
            "hra.received",
            errors.hra?.received?.message,
          )}
          {amount(
            "income-tax-preview-hra-salary",
            "Salary for HRA (₹)",
            "hra.salary",
            errors.hra?.salary?.message,
          )}
          {amount(
            "income-tax-preview-hra-rent",
            "Rent paid in the year (₹)",
            "hra.rent_paid",
            errors.hra?.rent_paid?.message,
          )}
          <Field id="income-tax-preview-hra-city" label="City" error={errors.hra?.city?.message}>
            <SelectControl
              id="income-tax-preview-hra-city"
              value={city}
              aria-invalid={!!errors.hra?.city}
              placeholder="Choose a city class"
              onValueChange={(next) =>
                form.setValue("hra.city", next, {
                  shouldDirty: true,
                  shouldValidate: form.formState.isSubmitted,
                })
              }
              options={Object.entries(HRA_CITIES).map(([value, label]) => ({ value, label }))}
            />
          </Field>
        </fieldset>
        <fieldset className="space-y-3 sm:col-span-4">
          <legend className="mb-2 text-sm font-medium">Claimed deductions (optional)</legend>
          {claims.fields.map((claim, index) => (
            <div key={claim.fieldKey} className="grid items-end gap-4 sm:grid-cols-[1fr_1fr_auto]">
              <Field
                id={`income-tax-preview-claim-${index}-code`}
                label={`Claim ${index + 1} code`}
                error={errors.claims?.[index]?.code?.message}
              >
                <input
                  id={`income-tax-preview-claim-${index}-code`}
                  className={inputClass}
                  aria-invalid={!!errors.claims?.[index]?.code}
                  {...form.register(`claims.${index}.code`)}
                />
              </Field>
              {amount(
                `income-tax-preview-claim-${index}-amount`,
                "Amount for the year (₹)",
                `claims.${index}.amount`,
                errors.claims?.[index]?.amount?.message,
              )}
              <Button
                type="button"
                variant="ghost"
                aria-label={`Remove claim ${index + 1}`}
                onClick={() => claims.remove(index)}
              >
                Remove
              </Button>
            </div>
          ))}
          <Button
            type="button"
            variant="outline"
            disabled={claims.fields.length >= 24}
            onClick={() => claims.append(blankPreviewClaim())}
          >
            Add claim
          </Button>
        </fieldset>
      </form>
      <p className="mt-2 text-xs text-muted-foreground">
        Annual liability under each regime&apos;s rule for that tax year, after the claims stated
        here. Each regime allows only the claim heads its rule lists, so the same figures may count
        under one and not the other. Nothing is stored. Age is needed only where a regime&apos;s
        slabs differ by age.
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
                        ["House rent exemption", -r.exemption_paise],
                        ["Standard deduction", -r.standard_deduction_paise],
                        ["Home loan interest", -r.house_property_loss_paise],
                        ["Deductions", -r.deductions_paise],
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
              {r.status === "applied" && r.claims.length > 0 && (
                <table className="mt-3 w-full text-xs">
                  <caption className="sr-only">Claims under {STATUTORY_KINDS[r.kind]}</caption>
                  <thead>
                    <tr className="border-b text-muted-foreground">
                      <th scope="col" className="py-1 text-left font-normal">
                        Claim
                      </th>
                      <th scope="col" className="py-1 text-right font-normal">
                        Claimed
                      </th>
                      <th scope="col" className="py-1 text-right font-normal">
                        Allowed
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {r.claims.map((c, i) => (
                      <tr key={`${c.code}-${i}`} className="border-b">
                        <th scope="row" className="py-1 text-left font-normal">
                          {c.code || "House rent"} · {INCOME_TAX_CLAIM_STATUSES[c.status]}
                        </th>
                        <td className="py-1 text-right tabular-nums">
                          {formatPaise(c.claimed_paise)}
                        </td>
                        <td className="py-1 text-right tabular-nums">
                          {formatPaise(c.allowed_paise)}
                        </td>
                      </tr>
                    ))}
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
