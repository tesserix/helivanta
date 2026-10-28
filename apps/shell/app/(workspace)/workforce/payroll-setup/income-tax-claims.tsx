"use client";
import { Button } from "@tesserix/web";
import { blankClaim, blankClaimGroup, INCOME_TAX_CLAIM_KINDS } from "@helivanta/api";
import { useFieldArray } from "@helivanta/ui";
import { Choose, TableError, Text, type Form } from "./statutory-slabs";

function ClaimHead({ form, index, onRemove }: { form: Form; index: number; onRemove: () => void }) {
  const name = `income_tax.claims.${index}` as const;
  const errors = form.formState.errors.income_tax?.claims?.[index];
  const claim = form.watch(name);
  return (
    <fieldset className="space-y-3 rounded-lg border p-3">
      <legend className="px-1 text-sm font-medium">Claim {index + 1}</legend>
      <div className="grid items-end gap-4 sm:grid-cols-[1fr_2fr_2fr_auto]">
        <Text form={form} name={`${name}.code`} label="Code" error={errors?.code?.message} />
        <Text form={form} name={`${name}.label`} label="Name" error={errors?.label?.message} />
        <Choose
          form={form}
          name={`${name}.kind`}
          label="Kind"
          current={claim.kind}
          labels={INCOME_TAX_CLAIM_KINDS}
          error={errors?.kind?.message}
        />
        <Button
          type="button"
          variant="ghost"
          aria-label={`Remove claim ${index + 1}`}
          onClick={onRemove}
        >
          Remove
        </Button>
      </div>
      {claim.kind === "hra" && (
        <div className="grid gap-4 sm:grid-cols-4">
          <Text
            form={form}
            name={`${name}.rent_excess_rate`}
            label="Rent above this share of salary (%)"
            error={errors?.rent_excess_rate?.message}
          />
          <Text
            form={form}
            name={`${name}.metro_rate`}
            label="Metro city cap (% of salary)"
            error={errors?.metro_rate?.message}
          />
          <Text
            form={form}
            name={`${name}.other_rate`}
            label="Other city cap (% of salary)"
            error={errors?.other_rate?.message}
          />
          <Text
            form={form}
            name={`${name}.landlord_pan_above`}
            label="Landlord PAN above annual rent (₹)"
            error={errors?.landlord_pan_above?.message}
          />
        </div>
      )}
      {(claim.kind === "deduction" || claim.kind === "house_property_interest") && (
        <div className="grid gap-4 sm:grid-cols-2">
          <Text
            form={form}
            name={`${name}.cap`}
            label={claim.kind === "deduction" ? "Cap (₹, blank for none)" : "Cap (₹)"}
            error={errors?.cap?.message}
          />
          {claim.kind === "deduction" && (
            <Text
              form={form}
              name={`${name}.cap_group`}
              label="Shared cap group code (optional)"
              error={errors?.cap_group?.message}
            />
          )}
        </div>
      )}
    </fieldset>
  );
}

export function IncomeTaxClaims({ form }: { form: Form }) {
  const groups = useFieldArray({
    control: form.control,
    name: "income_tax.claim_groups",
    keyName: "fieldKey",
  });
  const claims = useFieldArray({
    control: form.control,
    name: "income_tax.claims",
    keyName: "fieldKey",
  });
  const errors = form.formState.errors.income_tax;
  return (
    <div className="space-y-3">
      <p className="text-sm font-medium">Exemptions and deductions</p>
      {groups.fields.map((group, index) => (
        <div key={group.fieldKey} className="grid items-end gap-4 sm:grid-cols-[1fr_2fr_1fr_auto]">
          <Text
            form={form}
            name={`income_tax.claim_groups.${index}.code`}
            label={`Group ${index + 1} code`}
            error={errors?.claim_groups?.[index]?.code?.message}
          />
          <Text
            form={form}
            name={`income_tax.claim_groups.${index}.label`}
            label={`Group ${index + 1} name`}
            error={errors?.claim_groups?.[index]?.label?.message}
          />
          <Text
            form={form}
            name={`income_tax.claim_groups.${index}.cap`}
            label="Shared cap (₹)"
            error={errors?.claim_groups?.[index]?.cap?.message}
          />
          <Button
            type="button"
            variant="ghost"
            aria-label={`Remove claim group ${index + 1}`}
            onClick={() => groups.remove(index)}
          >
            Remove
          </Button>
        </div>
      ))}
      <TableError message={errors?.claim_groups?.root?.message ?? errors?.claim_groups?.message} />
      {claims.fields.map((claim, index) => (
        <ClaimHead
          key={claim.fieldKey}
          form={form}
          index={index}
          onRemove={() => claims.remove(index)}
        />
      ))}
      <TableError message={errors?.claims?.root?.message ?? errors?.claims?.message} />
      <div className="flex gap-3">
        <Button
          type="button"
          variant="outline"
          disabled={claims.fields.length >= 24}
          onClick={() => claims.append(blankClaim())}
        >
          Add claim
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={groups.fields.length >= 8}
          onClick={() => groups.append(blankClaimGroup())}
        >
          Add shared cap group
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        Add only the claims this regime allows, from the cited source; a claim the rule does not
        list never reduces tax. House rent is exempt up to the least of the allowance received, rent
        above the stated share of salary, and the city&apos;s share of salary. Home loan interest is
        set off up to its cap, and each deduction is limited by its own cap, then its group&apos;s.
      </p>
    </div>
  );
}
