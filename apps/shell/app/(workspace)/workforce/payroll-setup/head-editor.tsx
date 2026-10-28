"use client";
import { useEffect, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  headKindRules,
  newSalaryHeadForm,
  salaryHeadFormSchema,
  salaryHeadToForm,
  toSalaryHeadInput,
  useApiMutation,
  MONTH_NAMES,
  SALARY_BASES,
  SALARY_HEAD_KINDS,
  SALARY_PRORATIONS,
  SALARY_ROUNDING_MODES,
  SALARY_ROUNDING_UNITS,
  SALARY_TAXABILITY,
  SALARY_WAGE_CLASSES,
  type SalaryHead,
  type SalaryHeadForm,
  type SalaryHeadInput,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { Check, Fact, headsKey, ReviewCheck } from "./payroll-workspace";

const options = (labels: Record<string, string>, keys?: readonly string[]) =>
  (keys ?? Object.keys(labels)).map((value) => ({ value, label: labels[value]! }));

export function HeadEditor({
  hospital,
  base,
  record,
  heads,
  onClose,
}: {
  hospital: string;
  base: string;
  record?: SalaryHead;
  heads: SalaryHead[];
  onClose: () => void;
}) {
  const [id] = useState(() => record?.id ?? crypto.randomUUID());
  const form = useZodForm(
    salaryHeadFormSchema,
    record ? salaryHeadToForm(record) : newSalaryHeadForm(today()),
  );
  const save = useApiMutation(
    (input: SalaryHeadInput) =>
      apiFetch<SalaryHead>(`${base}/hr/payroll/salary-heads/${id}`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    {
      invalidate: [headsKey(hospital), ["workforce", "salary-structures", hospital]],
      successToast: "Salary head saved",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  useEffect(() => form.setFocus(record ? "name" : "code"), [form, record]);
  const errors = form.formState.errors;
  const value = form.watch();
  const rules = headKindRules(value.kind);
  const set = <K extends keyof SalaryHeadForm>(name: K, next: SalaryHeadForm[K]) =>
    form.setValue(name, next as never, {
      shouldDirty: true,
      shouldValidate: form.formState.isSubmitted,
    });
  const toggle = <T,>(list: T[], item: T, on: boolean) =>
    on ? [...list, item] : list.filter((x) => x !== item);
  const text = (
    name:
      | "code"
      | "name"
      | "payslip_label"
      | "rate"
      | "proration_days"
      | "minimum_payable_days"
      | "min_amount"
      | "max_amount"
      | "tax_note",
    label: string,
    extra: { inputMode?: "decimal" | "numeric"; disabled?: boolean } = {},
  ) => (
    <Field id={`salary-head-${name}`} label={label} error={errors[name]?.message}>
      <input
        id={`salary-head-${name}`}
        className={inputClass}
        inputMode={extra.inputMode}
        disabled={extra.disabled}
        aria-invalid={!!errors[name]}
        {...form.register(name)}
      />
    </Field>
  );
  const choose = (
    name:
      | "kind"
      | "basis"
      | "proration"
      | "rounding_mode"
      | "rounding_unit_paise"
      | "taxability"
      | "wage_class",
    label: string,
    choices: { value: string; label: string }[],
    disabled?: boolean,
  ) => (
    <Field id={`salary-head-${name}`} label={label} error={errors[name]?.message}>
      <SelectControl
        id={`salary-head-${name}`}
        value={value[name]}
        disabled={disabled}
        onValueChange={(next) => set(name, next as never)}
        options={choices}
      />
    </Field>
  );
  // Only active earnings can be bases; a stored base stays listed so it can be removed.
  const others = heads.filter(
    (h) =>
      h.id !== id && ((h.kind === "earning" && h.active) || record?.base_head_ids.includes(h.id)),
  );
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label={record ? `Edit ${record.name}` : "Add salary head"}
      onSubmit={form.handleSubmit((input) => save.mutate(toSalaryHeadInput(input)))}
      className="space-y-6 border-b bg-muted/10 p-5"
    >
      <fieldset disabled={save.isPending} className="space-y-6">
        <div className="grid gap-5 sm:grid-cols-3">
          {choose("kind", "Kind", options(SALARY_HEAD_KINDS), !!record)}
          {text("code", "Code", { disabled: !!record })}
          {text("name", "Name")}
          {text("payslip_label", "Payslip label")}
          <Field
            id="salary-head-payslip_order"
            label="Payslip position"
            error={errors.payslip_order?.message}
          >
            <input
              id="salary-head-payslip_order"
              type="number"
              inputMode="numeric"
              className={inputClass}
              aria-invalid={!!errors.payslip_order}
              {...form.register("payslip_order", { valueAsNumber: true })}
            />
          </Field>
          <Field
            id="salary-head-effective"
            label="Effective date"
            error={errors.effective_on?.message}
          >
            <input
              id="salary-head-effective"
              type="date"
              className={inputClass}
              aria-invalid={!!errors.effective_on}
              {...form.register("effective_on")}
            />
          </Field>
        </div>
        {record && (
          <p className="text-xs text-muted-foreground">
            Kind and code are fixed once a head exists. Create a new head to change them.
          </p>
        )}

        <fieldset className="space-y-4 rounded-xl border p-4">
          <legend className="px-1 text-sm font-semibold">Calculation</legend>
          <div className="grid gap-5 sm:grid-cols-3">
            {choose("basis", "Basis", options(SALARY_BASES))}
            {value.basis === "percent" ? (
              text("rate", "Rate (%)", { inputMode: "decimal" })
            ) : (
              <>
                {choose("proration", "Proration", options(SALARY_PRORATIONS))}
                {value.proration === "fixed_days" &&
                  text("proration_days", "Days in a month", { inputMode: "numeric" })}
              </>
            )}
          </div>
          {value.basis === "percent" && (
            <div role="group" aria-labelledby="salary-head-bases" className="space-y-2">
              <p id="salary-head-bases" className="text-sm font-medium">
                Percentage of
              </p>
              {others.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  Add the heads this percentage is taken from first.
                </p>
              ) : (
                <div className="grid gap-2 sm:grid-cols-3">
                  {others.map((h) => (
                    <Check
                      key={h.id}
                      label={`${h.name} · ${h.code}`}
                      checked={value.base_head_ids.includes(h.id)}
                      onChange={(on) => set("base_head_ids", toggle(value.base_head_ids, h.id, on))}
                    />
                  ))}
                </div>
              )}
              {errors.base_head_ids && (
                <p role="alert" className="text-sm text-destructive">
                  {errors.base_head_ids.message}
                </p>
              )}
              <p className="text-xs text-muted-foreground">
                Percentages are taken from the bases after their own proration, so they are not
                prorated again.
              </p>
            </div>
          )}
          <div className="grid gap-5 sm:grid-cols-4">
            {choose("rounding_mode", "Rounding", options(SALARY_ROUNDING_MODES))}
            {choose("rounding_unit_paise", "Round to", options(SALARY_ROUNDING_UNITS))}
            {text("min_amount", "Floor (₹, optional)", { inputMode: "decimal" })}
            {text("max_amount", "Cap (₹, optional)", { inputMode: "decimal" })}
          </div>
          <div className="grid gap-5 sm:grid-cols-3">
            {text("minimum_payable_days", "Minimum payable days", { inputMode: "decimal" })}
          </div>
          <div role="group" aria-labelledby="salary-head-months" className="space-y-2">
            <p id="salary-head-months" className="text-sm font-medium">
              Paid in
            </p>
            <div className="grid grid-cols-4 gap-2 sm:grid-cols-6">
              {MONTH_NAMES.map((label, index) => (
                <Check
                  key={label}
                  label={label}
                  checked={value.months.includes(index + 1)}
                  onChange={(on) => set("months", toggle(value.months, index + 1, on))}
                />
              ))}
            </div>
            {errors.months && (
              <p role="alert" className="text-sm text-destructive">
                {errors.months.message}
              </p>
            )}
          </div>
        </fieldset>

        <fieldset className="space-y-4 rounded-xl border p-4">
          <legend className="px-1 text-sm font-semibold">Treatment</legend>
          <div className="flex flex-wrap gap-x-6 gap-y-2">
            <Check
              label="Counts toward gross"
              checked={rules.grossAllowed && value.in_gross}
              disabled={!rules.grossAllowed}
              onChange={(on) => set("in_gross", on)}
            />
            <Check
              label="Counts toward CTC"
              checked={rules.ctcAllowed && value.in_ctc}
              disabled={!rules.ctcAllowed}
              onChange={(on) => set("in_ctc", on)}
            />
            <Check
              label="Eligible for arrears"
              checked={value.arrears_eligible}
              onChange={(on) => set("arrears_eligible", on)}
            />
            <Check
              label="Show on payslip when zero"
              checked={value.show_when_zero}
              onChange={(on) => set("show_when_zero", on)}
            />
            <Check label="Active" checked={value.active} onChange={(on) => set("active", on)} />
          </div>
          {value.kind === "deduction" ? (
            <dl className="grid gap-4 text-sm sm:grid-cols-2">
              <Fact label="Income tax" value={SALARY_TAXABILITY.not_applicable} />
              <Fact label="Wage classification" value={SALARY_WAGE_CLASSES.outside} />
            </dl>
          ) : (
            <div className="grid gap-5 sm:grid-cols-3">
              {choose("taxability", "Income tax", options(SALARY_TAXABILITY, rules.taxability))}
              {value.taxability === "exempt" && text("tax_note", "Exemption basis")}
              {choose(
                "wage_class",
                "Wage classification",
                options(SALARY_WAGE_CLASSES, rules.wageClasses),
              )}
            </div>
          )}
          <p className="text-xs text-muted-foreground">
            Wage classification feeds the statutory wage test: excluded heads above half of total
            remuneration are added back to wages.
          </p>
        </fieldset>

        <ReviewCheck
          id="salary-head-review"
          error={errors.reviewed?.message}
          register={form.register("reviewed")}
        >
          I reviewed this head&apos;s rate, caps, tax treatment and wage classification
        </ReviewCheck>
      </fieldset>
      {save.isError && (
        <SaveError message={`${save.error.message} Cancel and reload to see the current head.`} />
      )}
      <div className="flex justify-end gap-2 border-t pt-5">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel changes
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Saving…" : "Save salary head"}
        </Button>
      </div>
    </form>
  );
}
