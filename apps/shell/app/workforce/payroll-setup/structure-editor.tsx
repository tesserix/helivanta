"use client";
import { useEffect, useMemo, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  newSalaryStructureForm,
  salaryStructureSchemaFor,
  salaryStructureToForm,
  toSalaryStructureInput,
  useApiMutation,
  SALARY_HEAD_KINDS,
  type HRMaster,
  type SalaryHead,
  type SalaryStructure,
  type SalaryStructureInput,
} from "@helivanta/api";
import { Field, SelectControl, useFieldArray, useZodForm } from "@helivanta/ui";
import { inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { Check, ReviewCheck, structuresKey } from "./payroll-workspace";

const scopes = [
  ["department_id", "department", "Department"],
  ["designation_id", "designation", "Designation"],
  ["grade_id", "grade", "Grade"],
] as const;

export function StructureEditor({
  hospital,
  base,
  record,
  heads,
  masters,
  onClose,
}: {
  hospital: string;
  base: string;
  record?: SalaryStructure;
  heads: SalaryHead[];
  masters: HRMaster[];
  onClose: () => void;
}) {
  const [id] = useState(() => record?.id ?? crypto.randomUUID());
  const schema = useMemo(() => salaryStructureSchemaFor(heads), [heads]);
  const form = useZodForm(
    schema,
    record ? salaryStructureToForm(record) : newSalaryStructureForm(today()),
  );
  const lines = useFieldArray({ control: form.control, name: "lines", keyName: "fieldKey" });
  const save = useApiMutation(
    (input: SalaryStructureInput) =>
      apiFetch<SalaryStructure>(`${base}/hr/payroll/salary-structures/${id}`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    {
      invalidate: [structuresKey(hospital)],
      successToast: "Salary structure saved",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  useEffect(() => form.setFocus(record ? "name" : "code"), [form, record]);
  const errors = form.formState.errors;
  const value = form.watch();
  const byId = new Map(heads.map((h) => [h.id, h]));
  // Inactive heads stay selectable only where the stored revision already uses them.
  const headOptions = (current: string) =>
    heads
      .filter((h) => h.active || h.id === current)
      .map((h) => ({ value: h.id, label: `${h.name} · ${SALARY_HEAD_KINDS[h.kind]}` }));
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label={record ? `Edit ${record.name}` : "Add salary structure"}
      onSubmit={form.handleSubmit((input) => save.mutate(toSalaryStructureInput(input, heads)))}
      className="space-y-6 border-b bg-muted/10 p-5"
    >
      <fieldset disabled={save.isPending} className="space-y-6">
        <div className="grid gap-5 sm:grid-cols-3">
          <Field id="salary-structure-code" label="Code" error={errors.code?.message}>
            <input
              id="salary-structure-code"
              className={inputClass}
              disabled={!!record}
              aria-invalid={!!errors.code}
              {...form.register("code")}
            />
          </Field>
          <Field id="salary-structure-name" label="Name" error={errors.name?.message}>
            <input
              id="salary-structure-name"
              className={inputClass}
              aria-invalid={!!errors.name}
              {...form.register("name")}
            />
          </Field>
          <Field
            id="salary-structure-effective"
            label="Effective date"
            error={errors.effective_on?.message}
          >
            <input
              id="salary-structure-effective"
              type="date"
              className={inputClass}
              aria-invalid={!!errors.effective_on}
              {...form.register("effective_on")}
            />
          </Field>
        </div>
        <Field
          id="salary-structure-description"
          label="Description (optional)"
          error={errors.description?.message}
        >
          <textarea
            id="salary-structure-description"
            rows={2}
            className={inputClass}
            aria-invalid={!!errors.description}
            {...form.register("description")}
          />
        </Field>
        <div className="grid gap-5 sm:grid-cols-3">
          {scopes.map(([name, kind, label]) => (
            <Field key={name} id={`salary-structure-${kind}`} label={label}>
              <SelectControl
                id={`salary-structure-${kind}`}
                value={value[name]}
                onValueChange={(next) => form.setValue(name, next, { shouldDirty: true })}
                options={[
                  { value: "", label: "Any" },
                  ...masters
                    .filter((m) => m.kind === kind && (m.active || m.id === value[name]))
                    .map((m) => ({ value: m.id, label: m.name })),
                ]}
              />
            </Field>
          ))}
        </div>
        <Check
          label="Active"
          checked={value.active}
          onChange={(on) => form.setValue("active", on, { shouldDirty: true })}
        />

        <fieldset className="space-y-3 rounded-xl border p-4">
          <legend className="px-1 text-sm font-semibold">Heads</legend>
          {lines.fields.map((line, index) => {
            const head = byId.get(value.lines[index]?.head_id ?? "");
            const lineErrors = errors.lines?.[index];
            return (
              <div key={line.fieldKey} className="grid items-end gap-3 sm:grid-cols-[2fr_1fr_auto]">
                <Field
                  id={`salary-line-${index}-head`}
                  label={`Head ${index + 1}`}
                  error={lineErrors?.head_id?.message}
                >
                  <SelectControl
                    id={`salary-line-${index}-head`}
                    value={value.lines[index]?.head_id ?? ""}
                    placeholder="Choose a head"
                    onValueChange={(next) =>
                      form.setValue(`lines.${index}.head_id`, next, {
                        shouldDirty: true,
                        shouldValidate: form.formState.isSubmitted,
                      })
                    }
                    options={headOptions(value.lines[index]?.head_id ?? "")}
                  />
                </Field>
                {head?.basis === "fixed" ? (
                  <Field
                    id={`salary-line-${index}-amount`}
                    label="Monthly amount (₹)"
                    error={lineErrors?.amount?.message}
                  >
                    <input
                      id={`salary-line-${index}-amount`}
                      inputMode="decimal"
                      className={inputClass}
                      aria-invalid={!!lineErrors?.amount}
                      {...form.register(`lines.${index}.amount`)}
                    />
                  </Field>
                ) : (
                  <p className="pb-3 text-sm text-muted-foreground">
                    {head ? "Computed from its bases" : ""}
                  </p>
                )}
                <Button
                  type="button"
                  variant="ghost"
                  aria-label={`Remove head ${index + 1}`}
                  disabled={lines.fields.length === 1}
                  onClick={() => lines.remove(index)}
                >
                  Remove
                </Button>
              </div>
            );
          })}
          {errors.lines?.root?.message || errors.lines?.message ? (
            <p role="alert" className="text-sm text-destructive">
              {errors.lines.root?.message ?? errors.lines.message}
            </p>
          ) : null}
          <Button
            type="button"
            variant="outline"
            disabled={lines.fields.length >= 60}
            onClick={() => lines.append({ head_id: "", amount: "" })}
          >
            Add head
          </Button>
          <p className="text-xs text-muted-foreground">
            Every percentage head needs its bases in the same structure. The server refuses a
            structure it cannot compute.
          </p>
        </fieldset>

        <ReviewCheck
          id="salary-structure-review"
          error={errors.reviewed?.message}
          register={form.register("reviewed")}
        >
          I reviewed the heads and amounts in this structure
        </ReviewCheck>
      </fieldset>
      {save.isError && (
        <SaveError
          message={`${save.error.message} Cancel and reload to see the current structure.`}
        />
      )}
      <div className="flex justify-end gap-2 border-t pt-5">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel changes
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Saving…" : "Save salary structure"}
        </Button>
      </div>
    </form>
  );
}
