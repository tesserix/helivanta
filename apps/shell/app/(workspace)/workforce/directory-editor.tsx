"use client";
import { useEffect, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  enrollmentRequestOptions,
  isEnrollmentAccessChanged,
  useApiQuery,
  staffDirectoryViewSchema,
  STAFF_DIRECTORY_CONTEXT,
  type Hospital,
  type Staff,
  type StaffInput,
  type StaffDirectoryView,
  type EnrollmentRequestScope,
} from "@helivanta/api";
import { Panel, RequestFailure } from "@helivanta/ui";
import { StaffAccessPreparation } from "@/components/workforce/staff-access-preparation";
import { StaffRecordForm } from "@/components/workforce/staff-record-form";

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
      <div className="mt-6 space-y-3">
        <RequestFailure
          message={
            query.isError
              ? "The current Staff record is not available. Retry before editing."
              : "Could not verify the current Staff record in this account and workspace. Refresh before editing."
          }
          retry={query.refetch}
          // A record that fails verification has no error, so it keeps the retry.
          error={query.error}
        />
        <Button variant="ghost" onClick={onClose}>
          Cancel
        </Button>
      </div>
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
  return (
    <>
      {staff && (
        <StaffAccessPreparation kind="hospital" active={staff.employment_status === "active"} />
      )}
      <StaffRecordForm
        establishmentName={hospital.name}
        staff={staff}
        invalidate={[["workforce", "staff", hospital.id]]}
        onClose={onClose}
        onReload={onReload}
        onContextChanged={onContextChanged}
        save={async (value, expectedVersion) => {
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
        }}
      />
    </>
  );
}
