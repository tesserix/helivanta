"use client";
import { useEffect } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  formatPaise,
  salaryAssignmentAmountsFor,
  salaryAssignmentFormSchema,
  toSalaryAssignmentInput,
  useApiMutation,
  type UseApiPagedQueryResult,
  type SalaryAssignment,
  type SalaryAssignmentInput,
  type SalaryStructure,
} from "@helivanta/api";
import { Field, SelectControl, useFieldArray, useZodForm } from "@helivanta/ui";
import { Failure, inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { staffPayKey } from "./staff-pay-panel";

export function StaffPayEditor({
  hospital,
  path,
  staff,
  record,
  structures,
  onClose,
}: {
  hospital: string;
  path: string;
  staff: string;
  record?: SalaryAssignment;
  structures: UseApiPagedQueryResult<SalaryStructure>;
  onClose: () => void;
}) {
  const basis = record?.current;
  const linesOf = (id: string) => structures.items.find((s) => s.id === id)?.lines ?? [];
  const form = useZodForm(salaryAssignmentFormSchema, {
    structure_id: basis?.structure_id ?? "",
    effective_on: today(),
    reason: "",
    amounts: basis ? salaryAssignmentAmountsFor(linesOf(basis.structure_id), basis) : [],
  });
  const amounts = useFieldArray({ control: form.control, name: "amounts", keyName: "fieldKey" });
  const save = useApiMutation(
    (input: SalaryAssignmentInput) =>
      apiFetch<SalaryAssignment>(path, { method: "PUT", body: JSON.stringify(input) }),
    {
      invalidate: [staffPayKey(hospital, staff), ["workforce", "salary-assignments", hospital]],
      successToast: "Pay proposed",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  useEffect(() => form.setFocus("reason"), [form]);
  const errors = form.formState.errors;
  const value = form.watch();
  const lines = new Map(linesOf(value.structure_id).map((l) => [l.head_id, l]));
  // Inactive structures stay listed only where the pay in force already uses them, so it can be read.
  const options = structures.items
    .filter((s) => s.active || s.id === basis?.structure_id)
    .map((s) => ({ value: s.id, label: `${s.name} · ${s.code}${s.active ? "" : " (inactive)"}` }));
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label={record ? "Propose pay change" : "Propose pay"}
      onSubmit={form.handleSubmit((input) =>
        save.mutate(toSalaryAssignmentInput(input, record?.version ?? 0)),
      )}
      className="space-y-6 border-t bg-muted/10 p-5"
    >
      {structures.isError ? (
        <Failure message="Could not load salary structures." retry={structures.retry} />
      ) : (
        <fieldset disabled={save.isPending} className="space-y-6">
          <div className="grid gap-5 sm:grid-cols-2">
            <Field
              id="staff-pay-structure"
              label="Salary structure"
              error={errors.structure_id?.message}
            >
              <SelectControl
                id="staff-pay-structure"
                value={value.structure_id}
                aria-invalid={!!errors.structure_id}
                placeholder={structures.isPending ? "Loading…" : "Choose a structure"}
                onValueChange={(next) => {
                  form.setValue("structure_id", next, {
                    shouldDirty: true,
                    shouldValidate: form.formState.isSubmitted,
                  });
                  amounts.replace(
                    salaryAssignmentAmountsFor(
                      linesOf(next),
                      next === basis?.structure_id ? basis : undefined,
                    ),
                  );
                }}
                options={options}
              />
            </Field>
            <Field
              id="staff-pay-effective"
              label="Effective date"
              error={errors.effective_on?.message}
            >
              <input
                id="staff-pay-effective"
                type="date"
                className={inputClass}
                aria-invalid={!!errors.effective_on}
                {...form.register("effective_on")}
              />
            </Field>
          </div>
          <Field id="staff-pay-reason" label="Reason" error={errors.reason?.message}>
            <textarea
              id="staff-pay-reason"
              rows={2}
              className={inputClass}
              aria-invalid={!!errors.reason}
              {...form.register("reason")}
            />
          </Field>
          {amounts.fields.length > 0 && (
            <fieldset className="space-y-3 rounded-xl border p-4">
              <legend className="px-1 text-sm font-semibold">Personal amounts</legend>
              <p className="text-xs text-muted-foreground">
                Leave a head blank to pay the structure&apos;s amount. Percentage heads always
                compute from their bases.
              </p>
              <div className="grid gap-4 sm:grid-cols-2">
                {amounts.fields.map((field, index) => {
                  const line = lines.get(field.head_id);
                  return (
                    <Field
                      key={field.fieldKey}
                      id={`staff-pay-amount-${index}`}
                      label={`${line?.name ?? "Head"} (₹ monthly)`}
                      error={errors.amounts?.[index]?.amount?.message}
                    >
                      <input
                        id={`staff-pay-amount-${index}`}
                        inputMode="decimal"
                        className={inputClass}
                        placeholder={
                          line?.amount_paise !== undefined
                            ? `Structure ${formatPaise(line.amount_paise)}`
                            : undefined
                        }
                        aria-invalid={!!errors.amounts?.[index]?.amount}
                        {...form.register(`amounts.${index}.amount`)}
                      />
                    </Field>
                  );
                })}
              </div>
            </fieldset>
          )}
          <p className="text-xs text-muted-foreground">
            A second person with approval rights must approve this before it takes effect. The
            structure must be active and keep these heads as fixed amounts on the effective date.
          </p>
        </fieldset>
      )}
      {save.isError && (
        <SaveError message={`${save.error.message} Cancel and reload to see the current pay.`} />
      )}
      <div className="flex justify-end gap-2 border-t pt-5">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" disabled={save.isPending || structures.isError}>
          {save.isPending ? "Proposing…" : "Propose pay"}
        </Button>
      </div>
    </form>
  );
}
