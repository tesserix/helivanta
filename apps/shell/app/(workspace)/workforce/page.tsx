"use client";
import { z } from "zod";
import { useState, useEffect, useRef } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  enrollmentRequestOptions,
  isEnrollmentAccessChanged,
  type EnrollmentRequestScope,
  type PermissionsResponse,
  useApiQuery,
  useApiMutation,
  staffSchema,
  JOB_LABELS,
  type Hospital,
  type Staff,
  type StaffInput,
  type Page,
} from "@helivanta/api";
import { Field, FormSelectField, PageHeading, SelectControl, useZodForm } from "@helivanta/ui";
import { EnrollmentEditor } from "./enrollment-editor";
import { StaffDocumentPanel } from "./document-panel";
import { StaffCredentialPanel } from "./credential-panel";
import { StaffIdentitySourcePanel } from "./identity-source-panel";
import { StaffLoginLinkPanel } from "./login-link-panel";

const inputClass =
  "w-full rounded-lg border bg-background px-3 py-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-(--hms-accent)";

export default function Workforce() {
  const [attempt, setAttempt] = useState(0);
  return <WorkforceAccount key={attempt} onRestart={() => setAttempt((v) => v + 1)} />;
}

function WorkforceAccount({ onRestart }: { onRestart: () => void }) {
  const [nonce] = useState(() => crypto.randomUUID());
  const [changed, setChanged] = useState(false);
  const identity = useApiQuery<PermissionsResponse>(
    ["workforce", "profile-workspace", nonce],
    "/iam/me/permissions",
    { gcTime: 0, refetchOnWindowFocus: false, request: { cache: "no-store" } },
  );
  if (changed)
    return (
      <section role="alert" className="space-y-4 rounded-xl border bg-card p-6">
        <p>
          Your account or workspace changed, or access to this Staff record is no longer available.
          Reopen the Staff workspace to verify your access.
        </p>
        <Button onClick={onRestart}>Reload Staff workspace</Button>
      </section>
    );
  if (identity.isPending) return <p role="status">Checking Staff workspace…</p>;
  if (
    identity.isError ||
    typeof identity.data?.tenant_id !== "string" ||
    !identity.data.tenant_id ||
    typeof identity.data.subject !== "string" ||
    !identity.data.subject ||
    !Array.isArray(identity.data.data)
  )
    return (
      <section role="alert" className="space-y-4 rounded-xl border bg-card p-6">
        <p>Could not verify your Staff workspace. No protected record has been opened.</p>
        <Button onClick={() => void identity.refetch()}>Retry Staff workspace</Button>
      </section>
    );
  const scope = { tenantId: identity.data.tenant_id, subject: identity.data.subject };
  return (
    <WorkforceWorkspace
      key={`${scope.tenantId}:${scope.subject}`}
      scope={scope}
      permissions={identity.data.data}
      onContextChanged={() => setChanged(true)}
    />
  );
}

type WorkspaceProps = {
  scope: EnrollmentRequestScope;
  permissions: string[];
  onContextChanged: () => void;
};

function WorkforceWorkspace({ scope, permissions, onContextChanged }: WorkspaceProps) {
  const can = (permission: string) => permissions.includes(permission);
  const [hospitalId, setHospitalId] = useState("");
  const [cursor, setCursor] = useState<string | null>(null);
  const hospitals = useApiQuery<Page<Hospital>>(
    ["workforce", "hospitals", scope.tenantId, scope.subject, cursor],
    `/workforce/hospitals${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`,
    { gcTime: 0, request: enrollmentRequestOptions(scope) },
  );
  useEffect(() => {
    if (isEnrollmentAccessChanged(hospitals.error)) onContextChanged();
  }, [hospitals.error, onContextChanged]);
  const hospital = hospitals.data?.data.find((h) => h.id === hospitalId) ?? hospitals.data?.data[0];
  return (
    <>
      <PageHeading
        description="Enrol, edit and find the staff of each hospital."
        actions={
          hospital && (
            <div className="w-full max-w-sm sm:w-72">
              <Field id="workforce-hospital" label="Hospital">
                <SelectControl
                  id="workforce-hospital"
                  value={hospital.id}
                  onValueChange={setHospitalId}
                  options={(hospitals.data?.data ?? []).map((item) => ({
                    value: item.id,
                    label: item.name,
                  }))}
                />
              </Field>
            </div>
          )
        }
      />
      {can("workforce.hospital.manage") && <HospitalSetup />}
      {hospitals.isPending ? (
        <p role="status">Loading hospitals…</p>
      ) : hospitals.isError ? (
        <div role="alert" className="rounded-xl border p-6">
          Could not load hospitals.{" "}
          <Button variant="outline" onClick={() => void hospitals.refetch()}>
            Try again
          </Button>
        </div>
      ) : hospital ? (
        <Directory
          key={hospital.id}
          hospital={hospital}
          scope={scope}
          permissions={permissions}
          onContextChanged={onContextChanged}
        />
      ) : (
        <section className="rounded-2xl border bg-card p-12 text-center">
          <h3 className="text-lg font-semibold">No hospital access yet</h3>
          <p className="mt-2 text-sm text-muted-foreground">
            Ask your tenant administrator to grant access to a hospital before managing its staff.
          </p>
        </section>
      )}
      {hospitals.data?.page.has_more && (
        <Button
          className="mt-4"
          variant="outline"
          onClick={() => {
            setCursor(hospitals.data?.page.next_cursor ?? null);
            setHospitalId("");
          }}
        >
          More hospitals
        </Button>
      )}
      {cursor && (
        <Button
          className="ml-3 mt-4"
          variant="ghost"
          onClick={() => {
            setCursor(null);
            setHospitalId("");
          }}
        >
          First hospitals
        </Button>
      )}
    </>
  );
}
function Directory({
  hospital,
  scope,
  permissions,
  onContextChanged,
}: { hospital: Hospital } & WorkspaceProps) {
  const can = (permission: string) => permissions.includes(permission);
  const opener = useRef<HTMLButtonElement>(null);
  const [cursor, setCursor] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
  const [editing, setEditing] = useState<Staff | "new" | null>(null);
  const [documentStaff, setDocumentStaff] = useState<Staff | null>(null);
  const [credentialStaff, setCredentialStaff] = useState<Staff | null>(null);
  const [identityStaff, setIdentityStaff] = useState<Staff | null>(null);
  const [linkStaff, setLinkStaff] = useState<Staff | null>(null);
  useEffect(() => {
    if (!editing) opener.current?.focus();
  }, [editing]);
  const path = `/workforce/hospitals/${hospital.id}/staff`;
  const query = useApiQuery<Page<Staff>>(
    ["workforce", "staff", hospital.id, scope.tenantId, scope.subject, cursor],
    `${path}${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`,
    { gcTime: 0, request: enrollmentRequestOptions(scope) },
  );
  useEffect(() => {
    if (isEnrollmentAccessChanged(query.error)) onContextChanged();
  }, [query.error, onContextChanged]);
  const rows = (query.data?.data ?? []).filter((row) =>
    `${row.display_name} ${row.employee_code} ${JOB_LABELS[row.job_category]}`
      .toLowerCase()
      .includes(filter.toLowerCase()),
  );
  const manager = hospital.access_level === "manager";
  const writable = manager;
  return (
    <>
      <section className="overflow-hidden rounded-2xl border bg-card shadow-sm">
        <div className="flex flex-wrap items-center justify-between gap-4 border-b p-5">
          <div>
            <h3 className="font-semibold">{hospital.name}</h3>
            <p className="mt-1 text-xs text-muted-foreground">
              {writable ? "Manage staff records" : "Read-only access"} · Hospital-specific directory
            </p>
          </div>
          {writable && (
            <Button
              onClick={(event) => {
                opener.current = event.currentTarget;
                setDocumentStaff(null);
                setCredentialStaff(null);
                setLinkStaff(null);
                setIdentityStaff(null);
                setEditing("new");
              }}
            >
              Add staff
            </Button>
          )}
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-4">
          <label className="w-full max-w-sm">
            <span className="sr-only">Filter this page</span>
            <input
              className={inputClass}
              placeholder="Filter this page by name, code or category"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            />
          </label>
          <span className="text-xs text-muted-foreground">
            {rows.length} {rows.length === 1 ? "record" : "records"} on this page
          </span>
        </div>
        {query.isPending ? (
          <p className="p-8" role="status">
            Loading staff…
          </p>
        ) : query.isError ? (
          <div role="alert" className="p-8">
            Could not load staff.{" "}
            <Button variant="outline" onClick={() => void query.refetch()}>
              Try again
            </Button>
          </div>
        ) : rows.length === 0 ? (
          <div className="px-6 py-16 text-center">
            <h4 className="font-semibold">
              {filter ? "No matching records on this page" : "Your staff directory starts here"}
            </h4>
            <p className="mt-2 text-sm text-muted-foreground">
              {filter
                ? "Try a different name or employee code."
                : writable
                  ? "Add your first staff member to get started."
                  : "No staff records have been added to this hospital."}
            </p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <caption className="sr-only">Staff at {hospital.name}</caption>
              <thead className="bg-muted/40 text-xs text-muted-foreground">
                <tr>
                  {["Staff member", "Employee code", "Job category", "Status", "Actions"].map(
                    (label) => (
                      <th key={label} scope="col" className="px-5 py-3 font-medium">
                        {label}
                      </th>
                    ),
                  )}
                </tr>
              </thead>
              <tbody className="divide-y">
                {rows.map((row) => (
                  <tr key={row.id} className="hover:bg-muted/25">
                    <td className="px-5 py-4 font-medium">{row.display_name}</td>
                    <td className="px-5 py-4 font-mono text-xs text-muted-foreground">
                      {row.employee_code}
                    </td>
                    <td className="px-5 py-4 text-muted-foreground">
                      {JOB_LABELS[row.job_category]}
                    </td>
                    <td className="px-5 py-4">
                      <span
                        className={
                          row.employment_status === "active"
                            ? "rounded-full bg-(--hms-accent-dim) px-2.5 py-1 text-xs font-medium text-(--hms-accent)"
                            : "rounded-full bg-muted px-2.5 py-1 text-xs text-muted-foreground"
                        }
                      >
                        {row.employment_status === "active" ? "Active" : "Inactive"}
                      </span>
                    </td>
                    <td className="px-5 py-4">
                      <Button
                        variant="ghost"
                        size="sm"
                        aria-label={`Identity details for ${row.display_name}`}
                        onClick={(event) => {
                          opener.current = event.currentTarget;
                          setEditing(null);
                          setDocumentStaff(null);
                          setCredentialStaff(null);
                          setLinkStaff(null);
                          setIdentityStaff(row);
                        }}
                      >
                        Identity details
                      </Button>
                      {manager && (
                        <>
                          {writable && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={(event) => {
                                opener.current = event.currentTarget;
                                setDocumentStaff(null);
                                setCredentialStaff(null);
                                setLinkStaff(null);
                                setIdentityStaff(null);
                                setEditing(row);
                              }}
                              aria-label={`Edit ${row.display_name}`}
                            >
                              Edit
                            </Button>
                          )}
                          {can("workforce.documents.manage") && (
                            <Button
                              variant="ghost"
                              size="sm"
                              aria-label={`Documents for ${row.display_name}`}
                              onClick={() => {
                                setEditing(null);
                                setCredentialStaff(null);
                                setLinkStaff(null);
                                setIdentityStaff(null);
                                setDocumentStaff(row);
                              }}
                            >
                              Documents
                            </Button>
                          )}
                          {(can("workforce.login_links.manage") ||
                            can("workforce.login_links.review")) && (
                            <Button
                              variant="ghost"
                              size="sm"
                              aria-label={`Account link for ${row.display_name}`}
                              onClick={() => {
                                setEditing(null);
                                setDocumentStaff(null);
                                setCredentialStaff(null);
                                setIdentityStaff(null);
                                setLinkStaff(row);
                              }}
                            >
                              Account link
                            </Button>
                          )}
                          {can("workforce.enrollment.manage") && (
                            <Button
                              variant="ghost"
                              size="sm"
                              aria-label={`Credentials for ${row.display_name}`}
                              onClick={() => {
                                setEditing(null);
                                setDocumentStaff(null);
                                setIdentityStaff(null);
                                setCredentialStaff(row);
                                setLinkStaff(null);
                              }}
                            >
                              Credentials
                            </Button>
                          )}
                        </>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <div className="flex justify-between border-t px-5 py-3">
          <span className="text-xs text-muted-foreground">
            Showing data from this hospital only
          </span>
          <div className="flex gap-2">
            {cursor && (
              <Button size="sm" variant="outline" onClick={() => setCursor(null)}>
                First page
              </Button>
            )}
            {query.data?.page.has_more && (
              <Button
                size="sm"
                variant="outline"
                onClick={() => setCursor(query.data?.page.next_cursor ?? null)}
              >
                Next page
              </Button>
            )}
          </div>
        </div>
      </section>
      {identityStaff && (
        <StaffIdentitySourcePanel
          hospitalId={hospital.id}
          staffId={identityStaff.id}
          onClose={() => {
            setIdentityStaff(null);
            opener.current?.focus();
          }}
        />
      )}
      {linkStaff &&
        manager &&
        (can("workforce.login_links.manage") || can("workforce.login_links.review")) && (
          <StaffLoginLinkPanel
            hospital={hospital}
            staff={linkStaff}
            onClose={() => setLinkStaff(null)}
          />
        )}
      {documentStaff && manager && can("workforce.documents.manage") && (
        <StaffDocumentPanel
          key={documentStaff.id}
          hospital={hospital}
          staff={documentStaff}
          onClose={() => setDocumentStaff(null)}
        />
      )}
      {credentialStaff && manager && can("workforce.enrollment.manage") && (
        <StaffCredentialPanel
          key={credentialStaff.id}
          hospital={hospital}
          staff={credentialStaff}
          onClose={() => setCredentialStaff(null)}
        />
      )}
      {editing && writable && can("workforce.enrollment.manage") ? (
        <EnrollmentEditor
          key={editing === "new" ? "new" : editing.id}
          hospital={hospital}
          scope={scope}
          onContextChanged={onContextChanged}
          staff={editing === "new" ? undefined : editing}
          onClose={() => setEditing(null)}
        />
      ) : (
        editing &&
        writable && (
          <StaffEditor
            key={editing === "new" ? "new" : editing.id}
            hospital={hospital}
            staff={editing === "new" ? undefined : editing}
            onClose={() => setEditing(null)}
          />
        )
      )}
    </>
  );
}
function StaffEditor({
  hospital,
  staff,
  onClose,
}: {
  hospital: Hospital;
  staff?: Staff;
  onClose: () => void;
}) {
  const [id] = useState(() => staff?.id ?? crypto.randomUUID());
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
    (value: StaffInput) =>
      apiFetch<Staff>(`/workforce/hospitals/${hospital.id}/staff/${id}`, {
        method: "PUT",
        body: JSON.stringify(value),
      }),
    {
      invalidate: [["workforce", "staff", hospital.id]],
      onSuccess: onClose,
      suppressErrorToast: true,
    },
  );
  return (
    <section
      aria-labelledby="staff-editor-heading"
      className="mt-6 rounded-2xl border bg-card p-6 shadow-sm"
    >
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
          <input
            id="staff-name"
            className={inputClass}
            autoComplete="off"
            {...form.register("display_name")}
          />
        </Field>
        <Field
          id="staff-code"
          label="Employee code"
          error={form.formState.errors.employee_code?.message}
        >
          <input
            id="staff-code"
            className={inputClass}
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
    </section>
  );
}

const hospitalSchema = z.object({
  name: z.string().trim().min(1, "Enter a hospital name.").max(150),
  grant: z.literal(true, { error: "Confirm your hospital access grant." }),
});
function HospitalSetup() {
  const [id, setId] = useState(() => crypto.randomUUID());
  const identity = useApiQuery<{ subject: string }>(
    ["iam", "me", "permissions"],
    "/iam/me/permissions",
  );
  const form = useZodForm(hospitalSchema, { name: "" });
  const save = useApiMutation(
    async (value: z.infer<typeof hospitalSchema>) => {
      if (!identity.data?.subject)
        throw new Error("Your account could not be verified. Reload and try again.");
      await apiFetch(`/workforce/hospitals/${id}`, {
        method: "PUT",
        body: JSON.stringify({ name: value.name }),
      });
      return apiFetch(
        `/workforce/hospitals/${id}/access/${encodeURIComponent(identity.data.subject)}`,
        { method: "PUT", body: JSON.stringify({ level: "manager" }) },
      );
    },
    {
      invalidate: [["workforce", "hospitals"]],
      suppressErrorToast: true,
      successToast: "Hospital created with manager access",
      onSuccess: () => {
        form.reset();
        setId(crypto.randomUUID());
      },
    },
  );
  return (
    <details className="mb-6 rounded-xl border bg-card p-5">
      <summary className="cursor-pointer text-sm font-medium">Add a hospital</summary>
      <form
        noValidate
        onSubmit={form.handleSubmit((value) => save.mutate(value))}
        className="mt-5 max-w-lg space-y-4"
      >
        <Field id="hospital-name" label="Hospital name" error={form.formState.errors.name?.message}>
          <input id="hospital-name" className={inputClass} {...form.register("name")} />
        </Field>
        <label className="flex items-start gap-3 text-sm">
          <input type="checkbox" className="mt-1" {...form.register("grant")} />
          Grant me staff manager access to this hospital
        </label>
        {form.formState.errors.grant && (
          <p role="alert" className="text-sm text-destructive">
            {form.formState.errors.grant.message}
          </p>
        )}
        {save.isError && (
          <p role="alert" className="text-sm text-destructive">
            {save.error.message} Your hospital setup can be retried safely.
          </p>
        )}
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Creating…" : "Create hospital"}
        </Button>
      </form>
    </details>
  );
}
