"use client";
import { useEffect, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  isEnrollmentAccessChanged,
  useApiMutation,
  staffSchema,
  JOB_LABELS,
  type StaffInput,
} from "@helivanta/api";
import { Field, FormSelectField, useZodForm, ConfirmDialog, Panel } from "@helivanta/ui";

export type StaffRecordFormProps = {
  establishmentName: string;
  /** The verified current record; absent when adding a new one. */
  staff?: StaffInput & { version: number };
  /** Writes against expectedVersion and throws unless the stored record is verified. */
  save: (value: StaffInput, expectedVersion: number) => Promise<unknown>;
  invalidate: unknown[][];
  onClose: () => void;
  onReload: () => void;
  onContextChanged: () => void;
};

export function StaffRecordForm({
  establishmentName,
  staff,
  save: write,
  invalidate,
  onClose,
  onReload,
  onContextChanged,
}: StaffRecordFormProps) {
  const [expectedVersion] = useState(() => staff?.version ?? 0);
  const [confirmReload, setConfirmReload] = useState(false);
  const form = useZodForm(
    staffSchema,
    staff
      ? {
          employee_code: staff.employee_code,
          display_name: staff.display_name,
          job_category: staff.job_category,
          employment_status: staff.employment_status,
        }
      : {
          employee_code: "",
          display_name: "",
          job_category: "other",
          employment_status: "active",
        },
  );
  const save = useApiMutation((value: StaffInput) => write(value, expectedVersion), {
    invalidate,
    onSuccess: onClose,
    suppressErrorToast: true,
  });
  useEffect(() => {
    if (isEnrollmentAccessChanged(save.error)) onContextChanged();
  }, [save.error, onContextChanged]);
  return (
    <Panel aria-labelledby="staff-editor-heading" className="mt-6">
      <div className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h3 id="staff-editor-heading" className="text-lg font-semibold">
            {staff ? "Edit staff member" : "Add staff member"}
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">{establishmentName} · Staff profile</p>
        </div>
        <Button variant="ghost" onClick={onClose} disabled={save.isPending}>
          Cancel
        </Button>
      </div>
      <form
        noValidate
        onSubmit={form.handleSubmit((value) => save.mutate(value))}
        className="grid gap-5 sm:grid-cols-2"
      >
        <Field
          id="staff-name"
          label="Full name"
          error={form.formState.errors.display_name?.message}
        >
          <Input
            id="staff-name"
            className="h-11 w-full"
            autoComplete="off"
            {...form.register("display_name")}
          />
        </Field>
        <Field
          id="staff-code"
          label="Employee code"
          error={form.formState.errors.employee_code?.message}
        >
          <Input
            id="staff-code"
            className="h-11 w-full"
            autoComplete="off"
            {...form.register("employee_code")}
          />
        </Field>
        <FormSelectField
          control={form.control}
          name="job_category"
          id="staff-category"
          label="Job category"
          options={Object.entries(JOB_LABELS).map(([value, label]) => ({ value, label }))}
        />
        <FormSelectField
          control={form.control}
          name="employment_status"
          id="staff-status"
          label="Employment status"
          options={[
            { value: "active", label: "Active" },
            { value: "inactive", label: "Inactive" },
          ]}
        />
        {save.isError && (
          <p role="alert" className="text-sm text-destructive sm:col-span-2">
            {save.error.message}
          </p>
        )}
        <div className="flex justify-end border-t pt-5 sm:col-span-2">
          <Button type="submit" disabled={save.isPending}>
            {save.isPending ? "Saving…" : "Save staff"}
          </Button>
        </div>
      </form>
      {save.isError && (
        <Button
          variant="outline"
          className="mt-4"
          disabled={save.isPending}
          onClick={() => setConfirmReload(true)}
        >
          Reload current record
        </Button>
      )}
      <ConfirmDialog
        open={confirmReload}
        onOpenChange={setConfirmReload}
        title="Reload the current Staff record?"
        description="Your unsaved changes will be discarded. The saved Staff record will not be changed."
        confirmLabel="Discard edits and reload"
        onConfirm={onReload}
      />
    </Panel>
  );
}
