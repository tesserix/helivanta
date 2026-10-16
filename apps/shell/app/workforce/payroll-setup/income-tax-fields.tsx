"use client";
import { Button } from "@tesserix/web";
import {
  blankIncomeTaxBand,
  blankSurcharge,
  blankTaxSlab,
  INCOME_TAX_REBATES,
  SALARY_ROUNDING_MODES,
  SALARY_ROUNDING_UNITS,
} from "@helivanta/api";
import { useFieldArray } from "@helivanta/ui";
import { Choose, TableError, Text, type Form } from "./statutory-slabs";

function SlabTable({ form, band }: { form: Form; band: number }) {
  const name = `income_tax.age_bands.${band}.slabs` as const;
  const slabs = useFieldArray({ control: form.control, name, keyName: "fieldKey" });
  const errors = form.formState.errors.income_tax?.age_bands?.[band]?.slabs;
  return (
    <div className="space-y-3">
      {slabs.fields.map((slab, index) => (
        <div key={slab.fieldKey} className="grid items-end gap-4 sm:grid-cols-[1fr_1fr_1fr_auto]">
          <Text
            form={form}
            name={`${name}.${index}.from`}
            label={`Slab ${index + 1} taxable income from (₹)`}
            error={errors?.[index]?.from?.message}
          />
          <Text
            form={form}
            name={`${name}.${index}.to`}
            label="Up to (₹, blank for no limit)"
            error={errors?.[index]?.to?.message}
          />
          <Text
            form={form}
            name={`${name}.${index}.rate`}
            label="Rate (%)"
            error={errors?.[index]?.rate?.message}
          />
          <Button
            type="button"
            variant="ghost"
            aria-label={`Remove slab ${index + 1} of band ${band + 1}`}
            disabled={slabs.fields.length === 1}
            onClick={() => slabs.remove(index)}
          >
            Remove
          </Button>
        </div>
      ))}
      <TableError message={errors?.root?.message ?? errors?.message} />
      <Button
        type="button"
        variant="outline"
        disabled={slabs.fields.length >= 16}
        onClick={() => slabs.append(blankTaxSlab())}
      >
        Add slab
      </Button>
    </div>
  );
}

export function IncomeTaxFields({ form }: { form: Form }) {
  const bands = useFieldArray({
    control: form.control,
    name: "income_tax.age_bands",
    keyName: "fieldKey",
  });
  const surcharges = useFieldArray({
    control: form.control,
    name: "income_tax.surcharges",
    keyName: "fieldKey",
  });
  const errors = form.formState.errors.income_tax;
  const it = form.watch("income_tax");
  const rebated = it.rebate === "relief" || it.rebate === "cliff";
  return (
    <fieldset className="space-y-4 rounded-xl border p-4">
      <legend className="px-1 text-sm font-semibold">Income-tax parameters</legend>
      <div className="grid gap-5 sm:grid-cols-3">
        <Text
          form={form}
          name="income_tax.tax_year"
          label="Tax year (for example 2026-27)"
          error={errors?.tax_year?.message}
        />
        <Text
          form={form}
          name="income_tax.standard_deduction"
          label="Standard deduction (₹)"
          error={errors?.standard_deduction?.message}
        />
        <Text
          form={form}
          name="income_tax.cess_rate"
          label="Health and education cess (%)"
          error={errors?.cess_rate?.message}
        />
        <Choose
          form={form}
          name="income_tax.taxable_rounding_mode"
          label="Round taxable income"
          current={it.taxable_rounding_mode}
          labels={SALARY_ROUNDING_MODES}
          error={errors?.taxable_rounding_mode?.message}
        />
        <Choose
          form={form}
          name="income_tax.taxable_rounding_unit"
          label="Taxable income to"
          current={it.taxable_rounding_unit}
          labels={SALARY_ROUNDING_UNITS}
          error={errors?.taxable_rounding_unit?.message}
        />
        <div className="hidden sm:block" />
        <Choose
          form={form}
          name="income_tax.tax_rounding_mode"
          label="Round tax payable"
          current={it.tax_rounding_mode}
          labels={SALARY_ROUNDING_MODES}
          error={errors?.tax_rounding_mode?.message}
        />
        <Choose
          form={form}
          name="income_tax.tax_rounding_unit"
          label="Tax payable to"
          current={it.tax_rounding_unit}
          labels={SALARY_ROUNDING_UNITS}
          error={errors?.tax_rounding_unit?.message}
        />
      </div>

      {bands.fields.map((band, index) => (
        <fieldset key={band.fieldKey} className="space-y-3 rounded-lg border p-3">
          <legend className="px-1 text-sm font-medium">Age band {index + 1}</legend>
          <div className="grid items-end gap-4 sm:grid-cols-[1fr_auto]">
            <Text
              form={form}
              name={`income_tax.age_bands.${index}.min_age`}
              label="Applies from age"
              error={errors?.age_bands?.[index]?.min_age?.message}
            />
            <Button
              type="button"
              variant="ghost"
              aria-label={`Remove age band ${index + 1}`}
              disabled={bands.fields.length === 1}
              onClick={() => bands.remove(index)}
            >
              Remove band
            </Button>
          </div>
          <SlabTable form={form} band={index} />
        </fieldset>
      ))}
      <TableError message={errors?.age_bands?.root?.message ?? errors?.age_bands?.message} />
      <Button
        type="button"
        variant="outline"
        disabled={bands.fields.length >= 4}
        onClick={() => bands.append(blankIncomeTaxBand())}
      >
        Add age band
      </Button>

      <div className="grid gap-5 sm:grid-cols-3">
        <Choose
          form={form}
          name="income_tax.rebate"
          label="Rebate"
          current={it.rebate}
          labels={INCOME_TAX_REBATES}
          error={errors?.rebate?.message}
        />
        {rebated && (
          <>
            <Text
              form={form}
              name="income_tax.rebate_income_limit"
              label="Taxable income up to (₹)"
              error={errors?.rebate_income_limit?.message}
            />
            <Text
              form={form}
              name="income_tax.rebate_max"
              label="Rebate at most (₹)"
              error={errors?.rebate_max?.message}
            />
          </>
        )}
      </div>

      <div className="space-y-3">
        <p className="text-sm font-medium">Surcharge</p>
        {surcharges.fields.map((band, index) => (
          <div key={band.fieldKey} className="grid items-end gap-4 sm:grid-cols-[1fr_1fr_auto]">
            <Text
              form={form}
              name={`income_tax.surcharges.${index}.above`}
              label={`Band ${index + 1} on taxable income above (₹)`}
              error={errors?.surcharges?.[index]?.above?.message}
            />
            <Text
              form={form}
              name={`income_tax.surcharges.${index}.rate`}
              label="Rate on tax (%)"
              error={errors?.surcharges?.[index]?.rate?.message}
            />
            <Button
              type="button"
              variant="ghost"
              aria-label={`Remove surcharge band ${index + 1}`}
              onClick={() => surcharges.remove(index)}
            >
              Remove
            </Button>
          </div>
        ))}
        <TableError message={errors?.surcharges?.root?.message ?? errors?.surcharges?.message} />
        <Button
          type="button"
          variant="outline"
          disabled={surcharges.fields.length >= 8}
          onClick={() => surcharges.append(blankSurcharge())}
        >
          Add surcharge band
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        One rule covers one tax year, 1 April to 31 March, and its effective date must fall in it; a
        new year needs a new revision. Tax is the slabs on taxable income after the standard
        deduction, less the rebate, plus the highest surcharge band exceeded with marginal relief,
        plus cess. One band must start at age 0; add older bands only where the regime has them.
        Each table must run from ₹0 without gaps or overlaps, ending in one with no limit.
      </p>
    </fieldset>
  );
}
