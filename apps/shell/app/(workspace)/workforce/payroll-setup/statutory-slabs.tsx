"use client";
import { Button } from "@tesserix/web";
import {
  blankLwfSlab,
  blankPtSlab,
  formatPaise,
  ptAnnualTotals,
  MONTH_NAMES,
  PT_CADENCES,
  SALARY_ROUNDING_MODES,
  SALARY_ROUNDING_UNITS,
  STATUTORY_GENDERS,
  STATUTORY_WAGE_BASES,
  type StatutoryRuleForm,
} from "@helivanta/api";
import { Field, SelectControl, useFieldArray, type UseFormReturn } from "@helivanta/ui";
import { inputClass } from "../roster-policy/policy-ui";
import { Check } from "./payroll-workspace";

export type Form = UseFormReturn<StatutoryRuleForm>;
type Kind = "pt" | "lwf";
type Gender = keyof typeof STATUTORY_GENDERS;

const options = (labels: Record<string, string>) =>
  Object.entries(labels).map(([value, label]) => ({ value, label }));

export function useSet(form: Form) {
  return (name: string, next: unknown) =>
    form.setValue(name as never, next as never, {
      shouldDirty: true,
      shouldValidate: form.formState.isSubmitted,
    });
}

export function Text({
  form,
  name,
  label,
  error,
}: {
  form: Form;
  name: string;
  label: string;
  error: string | undefined;
}) {
  const id = `statutory-${name.replaceAll(".", "-")}`;
  return (
    <Field id={id} label={label} error={error}>
      <input
        id={id}
        className={inputClass}
        inputMode={/(state_code|tax_year)$/.test(name) ? undefined : "decimal"}
        aria-invalid={!!error}
        {...form.register(name as never)}
      />
    </Field>
  );
}

export function Choose({
  form,
  name,
  label,
  current,
  labels,
  error,
}: {
  form: Form;
  name: string;
  label: string;
  current: string;
  labels: Record<string, string>;
  error: string | undefined;
}) {
  const set = useSet(form);
  const id = `statutory-${name.replaceAll(".", "-")}`;
  return (
    <Field id={id} label={label} error={error}>
      <SelectControl
        id={id}
        value={current}
        aria-invalid={!!error}
        placeholder="Choose from the cited source"
        onValueChange={(next) => set(name, next)}
        options={options(labels)}
      />
    </Field>
  );
}

function Toggles<T extends string | number>({
  label,
  id,
  choices,
  chosen,
  onChange,
  error,
}: {
  label: string;
  id: string;
  choices: [T, string][];
  chosen: T[];
  onChange: (next: T[]) => void;
  error: string | undefined;
}) {
  return (
    <div role="group" aria-labelledby={id} className="space-y-2">
      <p id={id} className="text-sm font-medium">
        {label}
      </p>
      <div className="grid grid-cols-4 gap-2 sm:grid-cols-6">
        {choices.map(([value, text]) => (
          <Check
            key={String(value)}
            label={text}
            checked={chosen.includes(value)}
            onChange={(on) => onChange(on ? [...chosen, value] : chosen.filter((c) => c !== value))}
          />
        ))}
      </div>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}

const months = MONTH_NAMES.map((m, i): [number, string] => [i + 1, m]);

export function TableError({ message }: { message: string | undefined }) {
  return message ? (
    <p role="alert" className="text-sm text-destructive">
      {message}
    </p>
  ) : null;
}

// The state header shared by PT and LWF; the state code is recorded for review, not used in the calculation.
function StateFields({ form, kind }: { form: Form; kind: Kind }) {
  const errors = form.formState.errors[kind];
  return (
    <>
      <Text
        form={form}
        name={`${kind}.state_code`}
        label="State code (for example IN-MH)"
        error={errors?.state_code?.message}
      />
      <Choose
        form={form}
        name={`${kind}.wage_basis`}
        label="Wages"
        current={form.watch(`${kind}.wage_basis`)}
        labels={STATUTORY_WAGE_BASES}
        error={errors?.wage_basis?.message}
      />
    </>
  );
}

export function PtFields({ form }: { form: Form }) {
  const set = useSet(form);
  const slabs = useFieldArray({ control: form.control, name: "pt.slabs", keyName: "fieldKey" });
  const errors = form.formState.errors.pt;
  const value = form.watch("pt.slabs");
  const cadence = form.watch("pt.cadence");
  const periodic = cadence === "half_yearly" || cadence === "annual";
  const wages = { half_yearly: "Half-yearly", annual: "Annual" }[cadence] ?? "Monthly";
  const totals = ptAnnualTotals(value);
  const genders = Object.entries(STATUTORY_GENDERS) as [Gender, string][];
  return (
    <fieldset className="space-y-4 rounded-xl border p-4">
      <legend className="px-1 text-sm font-semibold">Professional tax parameters</legend>
      <div className="grid gap-5 sm:grid-cols-3">
        <StateFields form={form} kind="pt" />
        <Choose
          form={form}
          name="pt.cadence"
          label="Charged on"
          current={cadence}
          labels={PT_CADENCES}
          error={errors?.cadence?.message}
        />
      </div>
      {periodic && (
        <Toggles
          id="statutory-pt-periods"
          label={cadence === "annual" ? "The year starts in" : "Half-years start in"}
          choices={months}
          chosen={form.watch("pt.period_start_months")}
          onChange={(next) => set("pt.period_start_months", next)}
          error={errors?.period_start_months?.message}
        />
      )}
      {slabs.fields.map((slab, index) => {
        const slabErrors = errors?.slabs?.[index];
        const row = value[index];
        return (
          <fieldset key={slab.fieldKey} className="space-y-3 rounded-lg border p-3">
            <legend className="px-1 text-sm font-medium">Slab {index + 1}</legend>
            <div className="grid items-end gap-4 sm:grid-cols-[1fr_1fr_1fr_auto]">
              <Text
                form={form}
                name={`pt.slabs.${index}.from`}
                label={`${wages} wages from (₹)`}
                error={slabErrors?.from?.message}
              />
              <Text
                form={form}
                name={`pt.slabs.${index}.to`}
                label="Up to, excluding (₹, blank for no limit)"
                error={slabErrors?.to?.message}
              />
              <Text
                form={form}
                name={`pt.slabs.${index}.amount`}
                label={periodic ? "Tax for the period (₹)" : "Tax per month (₹)"}
                error={slabErrors?.amount?.message}
              />
              <Button
                type="button"
                variant="ghost"
                aria-label={`Remove slab ${index + 1}`}
                disabled={slabs.fields.length === 1}
                onClick={() => slabs.remove(index)}
              >
                Remove
              </Button>
            </div>
            <Toggles
              id={`statutory-pt-slab-${index}-months`}
              label={
                periodic
                  ? `Month slab ${index + 1} is charged in`
                  : `Months slab ${index + 1} applies in`
              }
              choices={months}
              chosen={row?.months ?? []}
              onChange={(next) => set(`pt.slabs.${index}.months`, next)}
              error={slabErrors?.months?.message}
            />
            <Toggles
              id={`statutory-pt-slab-${index}-genders`}
              label={`Slab ${index + 1} applies to`}
              choices={genders}
              chosen={row?.genders ?? []}
              onChange={(next) => set(`pt.slabs.${index}.genders`, next)}
              error={slabErrors?.genders?.message}
            />
          </fieldset>
        );
      })}
      <TableError message={errors?.slabs?.root?.message ?? errors?.slabs?.message} />
      <Button
        type="button"
        variant="outline"
        disabled={slabs.fields.length >= 96}
        onClick={() => slabs.append(blankPtSlab())}
      >
        Add slab
      </Button>
      {totals && (
        <p className="text-sm" aria-live="polite">
          Highest yearly tax:{" "}
          {genders
            .map(([g, label]) => `${label.toLowerCase()} ${formatPaise(totals[g])}`)
            .join(" · ")}
        </p>
      )}
      <p className="text-xs text-muted-foreground">
        In every month a slab names, the slabs for each gender must run from ₹0 without gaps or
        overlaps, ending in one with no limit. Enter an exempt band as a ₹0 slab. A month no slab
        names has no professional tax. Check the yearly total against the ceiling in Article 276 of
        the Constitution.
      </p>
      {periodic && (
        <p className="text-xs text-muted-foreground">
          Slabs apply to the period&apos;s wages, and each period is charged once, in the one month
          its slabs name. Previews project the period&apos;s wages from the month they compute; the
          payroll run uses the wages actually paid.
        </p>
      )}
    </fieldset>
  );
}

export function LwfFields({ form }: { form: Form }) {
  const set = useSet(form);
  const slabs = useFieldArray({ control: form.control, name: "lwf.slabs", keyName: "fieldKey" });
  const errors = form.formState.errors.lwf;
  const lwf = form.watch("lwf");
  return (
    <fieldset className="space-y-4 rounded-xl border p-4">
      <legend className="px-1 text-sm font-semibold">Labour welfare fund parameters</legend>
      <div className="grid gap-5 sm:grid-cols-3">
        <StateFields form={form} kind="lwf" />
        <Choose
          form={form}
          name="lwf.rounding_mode"
          label="Rounding"
          current={lwf.rounding_mode}
          labels={SALARY_ROUNDING_MODES}
          error={errors?.rounding_mode?.message}
        />
        <Choose
          form={form}
          name="lwf.rounding_unit"
          label="Round to"
          current={lwf.rounding_unit}
          labels={SALARY_ROUNDING_UNITS}
          error={errors?.rounding_unit?.message}
        />
      </div>
      <Toggles
        id="statutory-lwf-months"
        label="Deducted in"
        choices={months}
        chosen={lwf.deduction_months}
        onChange={(next) => set("lwf.deduction_months", next)}
        error={errors?.deduction_months?.message}
      />
      {slabs.fields.map((slab, index) => {
        const slabErrors = errors?.slabs?.[index];
        return (
          <fieldset key={slab.fieldKey} className="space-y-3 rounded-lg border p-3">
            <legend className="px-1 text-sm font-medium">Slab {index + 1}</legend>
            <div className="grid items-end gap-4 sm:grid-cols-[1fr_1fr_auto]">
              <Text
                form={form}
                name={`lwf.slabs.${index}.from`}
                label="Wages from (₹)"
                error={slabErrors?.from?.message}
              />
              <Text
                form={form}
                name={`lwf.slabs.${index}.to`}
                label="Up to, excluding (₹, blank for no limit)"
                error={slabErrors?.to?.message}
              />
              <Button
                type="button"
                variant="ghost"
                aria-label={`Remove slab ${index + 1}`}
                disabled={slabs.fields.length === 1}
                onClick={() => slabs.remove(index)}
              >
                Remove
              </Button>
            </div>
            {(["employee", "employer"] as const).map((party) => (
              <div key={party} className="grid gap-4 sm:grid-cols-3">
                <Text
                  form={form}
                  name={`lwf.slabs.${index}.${party}_fixed`}
                  label={`${party === "employee" ? "Employee" : "Employer"} fixed (₹)`}
                  error={slabErrors?.[`${party}_fixed`]?.message}
                />
                <Text
                  form={form}
                  name={`lwf.slabs.${index}.${party}_rate`}
                  label={`${party === "employee" ? "Employee" : "Employer"} rate (%)`}
                  error={slabErrors?.[`${party}_rate`]?.message}
                />
                <Text
                  form={form}
                  name={`lwf.slabs.${index}.${party}_cap`}
                  label={`${party === "employee" ? "Employee" : "Employer"} cap (₹, optional)`}
                  error={slabErrors?.[`${party}_cap`]?.message}
                />
              </div>
            ))}
          </fieldset>
        );
      })}
      <TableError message={errors?.slabs?.root?.message ?? errors?.slabs?.message} />
      <Button
        type="button"
        variant="outline"
        disabled={slabs.fields.length >= 24}
        onClick={() => slabs.append(blankLwfSlab())}
      >
        Add slab
      </Button>
      <p className="text-xs text-muted-foreground">
        Each share is the fixed amount plus the rate on wages, rounded, then limited to the cap.
        Enter 0 for a part the state does not use. The slabs must run from ₹0 without gaps or
        overlaps, ending in one with no limit.
      </p>
    </fieldset>
  );
}
