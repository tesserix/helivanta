"use client";
import { useEffect, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  apiFetch,
  enrollmentRequestOptions,
  isEnrollmentAccessChanged,
  useApiQuery,
  useApiMutation,
  staffSchema,
  staffDirectoryViewSchema,
  STAFF_DIRECTORY_CONTEXT,
  JOB_LABELS,
  type Hospital,
  type Staff,
  type StaffInput,
  type StaffDirectoryView,
  type EnrollmentRequestScope,
} from "@helivanta/api";
import { Field, FormSelectField, useZodForm, ConfirmDialog, Panel } from "@helivanta/ui";

type DirectoryEditorProps = {
  hospital: Hospital;
  staff?: Staff;
  scope: EnrollmentRequestScope;
  onClose: () => void;
  onContextChanged: () => void;
  onReloadDirectory: () => void;
};
export function StaffDirectoryEditor({
  hospital,
  staff,
  scope,
  onClose,
  onContextChanged,
  onReloadDirectory,
}: DirectoryEditorProps) {
  const [id] = useState(() => staff?.id ?? crypto.randomUUID());
  const [reload, setReload] = useState(0);
  const path = `/workforce/hospitals/${hospital.id}/staff/${id}?${STAFF_DIRECTORY_CONTEXT.query}=${STAFF_DIRECTORY_CONTEXT.version}`;
  const query = useApiQuery<unknown>(
    ["workforce", "directory-editor", scope.tenantId, scope.subject, hospital.id, id, reload],
    path,
    {
      enabled: !!staff,
      gcTime: 0,
      staleTime: Infinity,
      refetchOnWindowFocus: false,
      request: enrollmentRequestOptions(scope),
    },
  );
  useEffect(() => {
    if (isEnrollmentAccessChanged(query.error)) onContextChanged();
  }, [query.error, onContextChanged]);
  if (staff && query.isPending)
    return (
      <Panel role="status" className="mt-6">
        Loading current Staff record…
      </Panel>
    );
  const parsed = staff ? staffDirectoryViewSchema.safeParse(query.data) : undefined;
  if (
    staff &&
    (query.isError ||
      !parsed?.success ||
      parsed.data.id !== id ||
      parsed.data.hospital_id !== hospital.id)
  )
    return (
      <Panel role="alert" className="mt-6">
        <p>
          {query.isError
            ? "The current Staff record is not available. Retry before editing."
            : "Could not verify the current Staff record in this account and workspace. Refresh before editing."}
        </p>
        <div className="mt-4 flex gap-3">
          <Button onClick={() => void query.refetch()}>Try again</Button>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
        </div>
      </Panel>
    );
  return (
    <StaffDirectoryForm
      key={`${id}:${reload}`}
      hospital={hospital}
      staff={parsed?.success ? parsed.data : undefined}
      id={id}
      path={path}
      scope={scope}
      onClose={onClose}
      onContextChanged={onContextChanged}
      onReload={() => {
        onReloadDirectory();
        setReload((v) => v + 1);
      }}
    />
  );
}

function StaffDirectoryForm({
  hospital,
  staff,
  id,
  path,
  scope,
  onClose,
  onReload,
  onContextChanged,
}: Omit<DirectoryEditorProps, "onReloadDirectory"> & {
  id: string;
  path: string;
  staff?: StaffDirectoryView;
  onReload: () => void;
}) {
  const [expectedVersion] = useState(() => staff?.version ?? 0);
  const [confirmReload, setConfirmReload] = useState(false);
  const form = useZodForm(
    staffSchema,
    staff ?? {
      employee_code: "",
      display_name: "",
      job_category: "other",
      employment_status: "active",
    },
  );
  const save = useApiMutation(
    async (value: StaffInput) => {
      const response = await apiFetch<unknown>(path, {
        ...enrollmentRequestOptions(scope),
        method: "PUT",
        body: JSON.stringify({
          ...value,
          expected_version: expectedVersion,
          directory_context_version: STAFF_DIRECTORY_CONTEXT.version,
        }),
      });
      const parsed = staffDirectoryViewSchema.safeParse(response);
      if (
        !parsed.success ||
        parsed.data.id !== id ||
        parsed.data.hospital_id !== hospital.id ||
        parsed.data.version < expectedVersion ||
        (Object.keys(value) as Array<keyof StaffInput>).some(
          (key) => parsed.data[key] !== value[key],
        )
      ) {
        throw new Error(
          "Could not verify the saved Staff record. Your draft has been kept. Reload to check the current record.",
        );
      }
      return parsed.data;
    },
    {
      invalidate: [["workforce", "staff", hospital.id]],
      onSuccess: onClose,
      suppressErrorToast: true,
    },
  );
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
          <p className="mt-1 text-sm text-muted-foreground">{hospital.name} · Staff profile</p>
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
