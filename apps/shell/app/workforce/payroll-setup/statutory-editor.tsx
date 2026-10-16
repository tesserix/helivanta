"use client";
import { useEffect, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  newStatutoryRuleForm,
  statutoryRevisionToForm,
  statutoryRuleFormSchema,
  statutoryRulesPath,
  toStatutoryRuleInput,
  useApiMutation,
  MONTH_NAMES,
  SALARY_ROUNDING_MODES,
  SALARY_ROUNDING_UNITS,
  STATUTORY_CONTRIBUTION_BASES,
  STATUTORY_KINDS,
  STATUTORY_WAGE_BASES,
  type StatutoryRule,
  type StatutoryRuleForm,
  type StatutoryRuleInput,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { Check } from "./payroll-workspace";
import { LwfFields, PtFields } from "./statutory-slabs";

const options = (labels: Record<string, string>) =>
  Object.entries(labels).map(([value, label]) => ({ value, label }));
const coverage = { covered: "Covered", not_covered: "Not covered" };

type PfField = keyof StatutoryRuleForm["pf"];
type EsiField = Exclude<keyof StatutoryRuleForm["esi"], "period_start_months">;

export function StatutoryEditor({
  hospital,
  base,
  kind,
  rule,
  onClose,
}: {
  hospital: string;
  base: string;
  kind: StatutoryRuleForm["kind"];
  rule?: StatutoryRule;
  onClose: () => void;
}) {
  const [id] = useState(() => rule?.id ?? crypto.randomUUID());
  const form = useZodForm(
    statutoryRuleFormSchema,
    rule?.current
      ? statutoryRevisionToForm(rule.current, rule.version)
      : newStatutoryRuleForm(kind, today()),
  );
  const save = useApiMutation(
    (input: StatutoryRuleInput) =>
      apiFetch<StatutoryRule>(`${statutoryRulesPath(base)}/${id}`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    {
      invalidate: [["workforce", "statutory-rules", hospital]],
      successToast: "Proposal submitted",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  useEffect(() => form.setFocus("name"), [form]);
  const errors = form.formState.errors;
  const value = form.watch();
  const set = (name: string, next: unknown) =>
    form.setValue(name as never, next as never, {
      shouldDirty: true,
      shouldValidate: form.formState.isSubmitted,
    });
  const input = (
    name: `pf.${PfField}` | `esi.${EsiField}` | "name" | "citation" | "reference",
    label: string,
    error: string | undefined,
    decimal = false,
  ) => {
    const fieldId = `statutory-${name.replace(".", "-")}`;
    return (
      <Field id={fieldId} label={label} error={error}>
        <input
          id={fieldId}
          className={inputClass}
          inputMode={decimal ? "decimal" : undefined}
          aria-invalid={!!error}
          {...form.register(name)}
        />
      </Field>
    );
  };
  const choose = (
    name: string,
    label: string,
    current: string,
    labels: Record<string, string>,
    error: string | undefined,
  ) => {
    const fieldId = `statutory-${name.replace(".", "-")}`;
    return (
      <Field id={fieldId} label={label} error={error}>
        <SelectControl
          id={fieldId}
          value={current}
          aria-invalid={!!error}
          placeholder="Choose from the cited source"
          onValueChange={(next) => set(name, next)}
          options={options(labels)}
        />
      </Field>
    );
  };
  const pf = errors.pf;
  const esi = errors.esi;
  const months = value.esi.period_start_months;
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label={`Propose ${STATUTORY_KINDS[kind]} rule`}
      onSubmit={form.handleSubmit((next) => save.mutate(toStatutoryRuleInput(next)))}
      className="space-y-6 border-b bg-muted/10 p-5"
    >
      <fieldset disabled={save.isPending} className="space-y-6">
        <div className="grid gap-5 sm:grid-cols-3">
          {input("name", "Name", errors.name?.message)}
          <Field
            id="statutory-effective"
            label="Effective date"
            error={errors.effective_on?.message}
          >
            <input
              id="statutory-effective"
              type="date"
              className={inputClass}
              aria-invalid={!!errors.effective_on}
              {...form.register("effective_on")}
            />
          </Field>
          {choose("coverage", "Coverage", value.coverage, coverage, errors.coverage?.message)}
          <div className="sm:col-span-2">
            {input("citation", "Notification or section relied on", errors.citation?.message)}
          </div>
          {input("reference", "Gazette number or link (optional)", errors.reference?.message)}
        </div>

        {value.coverage === "covered" && kind === "pf" && (
          <fieldset className="space-y-4 rounded-xl border p-4">
            <legend className="px-1 text-sm font-semibold">Provident Fund parameters</legend>
            <div className="grid gap-5 sm:grid-cols-3">
              {choose(
                "pf.wage_basis",
                "Wages",
                value.pf.wage_basis,
                STATUTORY_WAGE_BASES,
                pf?.wage_basis?.message,
              )}
              {choose(
                "pf.contribution_basis",
                "Contributions on",
                value.pf.contribution_basis,
                STATUTORY_CONTRIBUTION_BASES,
                pf?.contribution_basis?.message,
              )}
              {input("pf.wage_ceiling", "Wage ceiling (₹)", pf?.wage_ceiling?.message, true)}
              {input("pf.employee_rate", "Employee share (%)", pf?.employee_rate?.message, true)}
              {input("pf.employer_rate", "Employer share (%)", pf?.employer_rate?.message, true)}
              {input("pf.eps_rate", "Of which pension (%)", pf?.eps_rate?.message, true)}
              {input(
                "pf.eps_wage_ceiling",
                "Pension wage ceiling (₹)",
                pf?.eps_wage_ceiling?.message,
                true,
              )}
              {input("pf.edli_rate", "EDLI (%)", pf?.edli_rate?.message, true)}
              {input(
                "pf.edli_wage_ceiling",
                "EDLI wage ceiling (₹)",
                pf?.edli_wage_ceiling?.message,
                true,
              )}
              {input("pf.admin_rate", "Admin charges (%)", pf?.admin_rate?.message, true)}
              {input(
                "pf.admin_minimum",
                "Admin minimum per establishment (₹)",
                pf?.admin_minimum?.message,
                true,
              )}
              {choose(
                "pf.rounding_mode",
                "Rounding",
                value.pf.rounding_mode,
                SALARY_ROUNDING_MODES,
                pf?.rounding_mode?.message,
              )}
              {choose(
                "pf.rounding_unit",
                "Round to",
                value.pf.rounding_unit,
                SALARY_ROUNDING_UNITS,
                pf?.rounding_unit?.message,
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              The pension share is carved out of the employer share; the employer EPF share is what
              remains. The admin minimum applies per establishment when payroll runs.
            </p>
          </fieldset>
        )}

        {value.coverage === "covered" && kind === "esi" && (
          <fieldset className="space-y-4 rounded-xl border p-4">
            <legend className="px-1 text-sm font-semibold">ESI parameters</legend>
            <div className="grid gap-5 sm:grid-cols-3">
              {choose(
                "esi.wage_basis",
                "Wages",
                value.esi.wage_basis,
                STATUTORY_WAGE_BASES,
                esi?.wage_basis?.message,
              )}
              {input("esi.wage_ceiling", "Coverage ceiling (₹)", esi?.wage_ceiling?.message, true)}
              {input(
                "esi.disability_wage_ceiling",
                "Ceiling for persons with disability (₹)",
                esi?.disability_wage_ceiling?.message,
                true,
              )}
              {input("esi.employee_rate", "Employee share (%)", esi?.employee_rate?.message, true)}
              {input("esi.employer_rate", "Employer share (%)", esi?.employer_rate?.message, true)}
              {input(
                "esi.employee_exempt_daily",
                "Employee exempt at or below daily wage (₹, optional)",
                esi?.employee_exempt_daily?.message,
                true,
              )}
              {choose(
                "esi.rounding_mode",
                "Rounding",
                value.esi.rounding_mode,
                SALARY_ROUNDING_MODES,
                esi?.rounding_mode?.message,
              )}
              {choose(
                "esi.rounding_unit",
                "Round to",
                value.esi.rounding_unit,
                SALARY_ROUNDING_UNITS,
                esi?.rounding_unit?.message,
              )}
            </div>
            <div role="group" aria-labelledby="statutory-esi-periods" className="space-y-2">
              <p id="statutory-esi-periods" className="text-sm font-medium">
                Contribution periods start in
              </p>
              <div className="grid grid-cols-4 gap-2 sm:grid-cols-6">
                {MONTH_NAMES.map((label, index) => (
                  <Check
                    key={label}
                    label={label}
                    checked={months.includes(index + 1)}
                    onChange={(on) =>
                      set(
                        "esi.period_start_months",
                        on ? [...months, index + 1] : months.filter((m) => m !== index + 1),
                      )
                    }
                  />
                ))}
              </div>
              {esi?.period_start_months && (
                <p role="alert" className="text-sm text-destructive">
                  {esi.period_start_months.message}
                </p>
              )}
            </div>
          </fieldset>
        )}
        {value.coverage === "covered" && kind === "pt" && <PtFields form={form} />}
        {value.coverage === "covered" && kind === "lwf" && <LwfFields form={form} />}
        {value.coverage === "not_covered" && (kind === "pt" || kind === "lwf") && (
          <p className="text-xs text-muted-foreground">
            Record not covered where the state levies none, or where its professional tax is
            assessed half-yearly or yearly, which payroll runs will support. Cite the reason.
          </p>
        )}
        <p className="text-xs text-muted-foreground">
          Nothing is pre-filled. Enter each value from the notification you cite. Someone other than
          you must approve the proposal before it takes effect, and it cannot take effect before the
          revision in force.
        </p>
      </fieldset>
      {save.isError && (
        <SaveError message={`${save.error.message} Cancel and reload to see the current rule.`} />
      )}
      <div className="flex justify-end gap-2 border-t pt-5">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel proposal
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Submitting…" : "Submit for approval"}
        </Button>
      </div>
    </form>
  );
}
