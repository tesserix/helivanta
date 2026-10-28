"use client";
import { useEffect, useState } from "react";
import { Button } from "@tesserix/web";
import {
  dependentFormSchema,
  dependentsKey,
  saveDependent,
  useApiMutation,
  type BenefitScope,
  type Dependent,
  type DependentForm,
  type HRMaster,
  type UseApiPagedQueryResult,
} from "@helivanta/api";
import { ConfirmDialog, Field, SelectControl, useZodForm } from "@helivanta/ui";
import { inputClass } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { DEPENDENT_STATUSES, saveFailure } from "./benefits-ui";

const statusOptions = Object.entries(DEPENDENT_STATUSES).map(([value, label]) => ({
  value,
  label,
}));

const toForm = (dependent?: Dependent): DependentForm =>
  dependent
    ? {
        expected_version: dependent.version,
        full_name: dependent.full_name,
        relationship_revision_id: dependent.relationship_revision_id,
        birth_date: dependent.birth_date,
        phone: dependent.phone,
        status: dependent.status,
        reason: "",
      }
    : {
        expected_version: 0,
        full_name: "",
        relationship_revision_id: "",
        birth_date: "",
        phone: "",
        status: "active",
        reason: "",
      };

export function DependentEditor({
  scope,
  record,
  relationships,
  onClose,
}: {
  scope: BenefitScope;
  record?: Dependent;
  relationships: UseApiPagedQueryResult<HRMaster>;
  onClose: () => void;
}) {
  // One id while the form is open, so a retried add replays instead of recording the person twice.
  const [id] = useState(() => record?.id ?? crypto.randomUUID());
  const form = useZodForm(dependentFormSchema, toForm(record));
  const [removing, setRemoving] = useState<DependentForm | null>(null);
  const settle = (successToast: string) => ({
    invalidate: [dependentsKey(scope)],
    successToast,
    suppressErrorToast: true,
    onSuccess: onClose,
  });
  const save = useApiMutation(
    (input: DependentForm) => saveDependent(scope, id, input),
    settle("Dependent saved"),
  );
  const remove = useApiMutation(
    (input: DependentForm) => saveDependent(scope, id, input),
    settle("Dependent removed"),
  );
  useEffect(() => form.setFocus("full_name"), [form]);
  const errors = form.formState.errors;
  const value = form.watch();
  const busy = save.isPending || remove.isPending;
  const failure = save.error ?? remove.error;
  // The recorded revision always saves; any other choice must be an active master's current revision.
  const options = [
    ...(record
      ? [{ value: record.relationship_revision_id, label: record.relationship_name }]
      : []),
    ...relationships.items
      .filter((r) => r.active && r.id !== record?.relationship_master_id)
      .map((r) => ({ value: r.revision_id, label: r.name })),
  ];
  const text = (name: "full_name" | "phone" | "birth_date", label: string, type = "text") => (
    <Field id={`dependent-${name}`} label={label} error={errors[name]?.message}>
      <input
        id={`dependent-${name}`}
        type={type}
        className={inputClass}
        aria-invalid={!!errors[name]}
        {...form.register(name)}
      />
    </Field>
  );
  return (
    <>
      <form
        noValidate
        autoComplete="off"
        aria-label={record ? `Edit ${record.full_name}` : "Add dependent"}
        onSubmit={form.handleSubmit((input) =>
          record?.status === "active" && input.status === "removed"
            ? setRemoving(input)
            : save.mutate(input),
        )}
        className="space-y-6 border-b bg-muted/10 p-5"
      >
        <fieldset disabled={busy} className="space-y-6">
          <div className="grid gap-5 sm:grid-cols-3">
            {text("full_name", "Full name")}
            <Field
              id="dependent-relationship"
              label="Relationship"
              error={errors.relationship_revision_id?.message}
            >
              <SelectControl
                id="dependent-relationship"
                value={value.relationship_revision_id}
                placeholder={relationships.isPending ? "Loading…" : "Choose a relationship"}
                onValueChange={(next) =>
                  form.setValue("relationship_revision_id", next, {
                    shouldDirty: true,
                    shouldValidate: form.formState.isSubmitted,
                  })
                }
                options={options}
              />
            </Field>
            {text("birth_date", "Birth date", "date")}
            {text("phone", "Phone", "tel")}
            {record && (
              <Field id="dependent-status" label="Status" error={errors.status?.message}>
                <SelectControl
                  id="dependent-status"
                  value={value.status}
                  onValueChange={(next) =>
                    form.setValue("status", next as DependentForm["status"], { shouldDirty: true })
                  }
                  options={statusOptions}
                />
              </Field>
            )}
          </div>
          {relationships.hasMore && (
            <Button
              type="button"
              variant="outline"
              disabled={relationships.isFetchingMore}
              onClick={relationships.loadMore}
            >
              Load more relationships
            </Button>
          )}
          <Field id="dependent-reason" label="Where this comes from" error={errors.reason?.message}>
            <textarea
              id="dependent-reason"
              rows={2}
              maxLength={500}
              className={inputClass}
              aria-invalid={!!errors.reason}
              {...form.register("reason")}
            />
          </Field>
        </fieldset>
        {failure && <SaveError message={saveFailure(failure, "dependent")} />}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={busy}>
            {save.isPending ? "Saving…" : "Save dependent"}
          </Button>
        </div>
      </form>
      {record && (
        <ConfirmDialog
          open={removing !== null}
          onOpenChange={(open) => !open && setRemoving(null)}
          title={`Remove ${record.full_name}?`}
          description="They stay in the history and can be restored later. Any coverage for them must end first."
          confirmLabel="Remove dependent"
          busy={remove.isPending}
          onConfirm={() => {
            if (removing) remove.mutate(removing);
            setRemoving(null);
          }}
        />
      )}
    </>
  );
}
