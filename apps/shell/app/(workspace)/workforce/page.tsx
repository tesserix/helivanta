"use client";
import { z } from "zod";
import { useState, useEffect, useRef } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  ApiError,
  withStaffIntakeScope,
  staffIntakeScopeKey,
  useStaffIntakeScopes,
  type HrIntakeScope,
  enrollmentRequestOptions,
  isEnrollmentAccessChanged,
  type EnrollmentRequestScope,
  type PermissionsResponse,
  useApiQuery,
  useApiMutation,
  JOB_LABELS,
  type Hospital,
  type Staff,
  type Page,
} from "@helivanta/api";
import { Field, PageHeading, SelectControl, useZodForm, useConfirmLeave } from "@helivanta/ui";
import { EnrollmentEditor } from "./enrollment-editor";
import { StaffDirectoryEditor } from "./directory-editor";
import { StaffDocumentPanel } from "./document-panel";
import { StaffCredentialPanel } from "./credential-panel";
import { StaffIdentitySourcePanel } from "./identity-source-panel";
import { StaffEmploymentPeriods } from "./employment-periods-panel";
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

type HospitalChoice =
  | { key: string; kind: "legacy"; hospital: Hospital }
  | {
      key: string;
      kind: "intake";
      hospital: Pick<Hospital, "id" | "name">;
      capabilities: HrIntakeScope;
    };

function WorkforceWorkspace({ scope, permissions, onContextChanged }: WorkspaceProps) {
  const confirmLeave = useConfirmLeave();
  const [cursor, setCursor] = useState<string | null>(null);
  const [intakeCursor, setIntakeCursor] = useState<string | null>(null);
  const hospitals = useApiQuery<Page<Hospital>>(
    ["workforce", "hospitals", scope.tenantId, scope.subject, cursor],
    `/workforce/hospitals${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`,
    { gcTime: 0, request: enrollmentRequestOptions(scope) },
  );
  const intake = useStaffIntakeScopes(scope, intakeCursor);
  useEffect(() => {
    const error = intake.error;
    const intakeAccountChanged =
      error instanceof ApiError &&
      ([401, 403].includes(error.status) ||
        (error.status === 409 && ["session_changed", "workspace_changed"].includes(error.code)));
    if (isEnrollmentAccessChanged(hospitals.error) || intakeAccountChanged) onContextChanged();
  }, [hospitals.error, intake.error, onContextChanged]);
  const choices: HospitalChoice[] = [
    ...(hospitals.data?.data ?? []).map((hospital): HospitalChoice => ({
      key: hospital.id,
      kind: "legacy",
      hospital,
    })),
    ...(intake.data?.data ?? []).map((capabilities): HospitalChoice => ({
      key: `hr-intake:${capabilities.hospital_id}`,
      kind: "intake",
      hospital: { id: capabilities.hospital_id, name: capabilities.name },
      capabilities,
    })),
  ];
  const pending = hospitals.isPending || intake.isPending;
  return (
    <>
      <PageHeading description="Enrol, edit and find the staff of each hospital." />
      {permissions.includes("workforce.hospital.manage") && <HospitalSetup />}
      {pending ? (
        <p role="status">Loading hospitals…</p>
      ) : choices.length > 0 ? (
        <>
          {(hospitals.isError || intake.isError) && (
            <p className="mb-4 text-sm text-muted-foreground">
              Additional hospital access could not be verified.{" "}
              <Button
                variant="ghost"
                onClick={() => {
                  void hospitals.refetch();
                  void intake.refetch();
                }}
              >
                Retry hospital access
              </Button>
            </p>
          )}
          <HospitalStaffWorkspace
            key={`${cursor ?? "first"}:${intakeCursor ?? "first"}`}
            choices={choices}
            scope={scope}
            permissions={permissions}
            onContextChanged={onContextChanged}
          />
        </>
      ) : hospitals.isError || intake.isError ? (
        <div role="alert" className="rounded-xl border p-6">
          {hospitals.isError
            ? "Could not load hospitals."
            : "Could not verify Staff onboarding hospitals."}{" "}
          <Button
            variant="outline"
            onClick={() => {
              void hospitals.refetch();
              void intake.refetch();
            }}
          >
            Try again
          </Button>
        </div>
      ) : (
        <section className="rounded-2xl border bg-card p-12 text-center">
          <h3 className="text-lg font-semibold">
            {hospitals.data?.page.has_more || intake.data?.page.has_more
              ? "No hospital access on this page"
              : "No hospital access yet"}
          </h3>
          <p className="mt-2 text-sm text-muted-foreground">
            {hospitals.data?.page.has_more || intake.data?.page.has_more
              ? "Continue to check the next hospitals."
              : "Ask your tenant administrator to grant access to a hospital before managing its staff."}
          </p>
        </section>
      )}
      {hospitals.data?.page.has_more && (
        <Button
          className="mt-4"
          variant="outline"
          onClick={() => confirmLeave(() => setCursor(hospitals.data?.page.next_cursor ?? null))}
        >
          More hospitals
        </Button>
      )}
      {cursor && (
        <Button
          className="ml-3 mt-4"
          variant="ghost"
          onClick={() => confirmLeave(() => setCursor(null))}
        >
          First hospitals
        </Button>
      )}
      {intake.data?.page.has_more && (
        <Button
          className="ml-3 mt-4"
          variant="outline"
          onClick={() => confirmLeave(() => setIntakeCursor(intake.data?.page.next_cursor ?? null))}
        >
          More onboarding hospitals
        </Button>
      )}
      {intakeCursor && (
        <Button
          className="ml-3 mt-4"
          variant="ghost"
          onClick={() => confirmLeave(() => setIntakeCursor(null))}
        >
          First onboarding hospitals
        </Button>
      )}
    </>
  );
}

function HospitalStaffWorkspace({
  choices,
  scope,
  permissions,
  onContextChanged,
}: WorkspaceProps & { choices: HospitalChoice[] }) {
  const confirmLeave = useConfirmLeave();
  const [selected, setSelected] = useState(() => choices[0]!.key);
  const choice = choices.find((item) => item.key === selected);
  useEffect(() => {
    if (!choice) onContextChanged();
  }, [choice, onContextChanged]);
  if (!choice)
    return (
      <p role="alert">
        The selected hospital access is no longer available. Reload Staff to verify your access.
      </p>
    );
  const requestScope = choice.kind === "intake" ? { ...scope, hrIntake: true } : scope;
  return (
    <>
      <div className="mb-6 flex justify-end">
        <div className="w-full max-w-sm sm:w-80">
          <Field id="workforce-hospital" label="Hospital">
            <SelectControl
              id="workforce-hospital"
              value={choice.key}
              onValueChange={(next) => confirmLeave(() => setSelected(next))}
              options={choices.map((item) => ({
                value: item.key,
                label:
                  item.kind === "intake"
                    ? `${item.hospital.name} · Staff onboarding`
                    : item.hospital.name,
              }))}
            />
          </Field>
        </div>
      </div>
      <Directory
        key={choice.key}
        choice={choice}
        scope={requestScope}
        permissions={permissions}
        onContextChanged={onContextChanged}
      />
    </>
  );
}
function Directory({
  choice,
  scope,
  permissions,
  onContextChanged,
}: { choice: HospitalChoice } & WorkspaceProps) {
  const hospital = choice.hospital;
  const confirmLeave = useConfirmLeave();
  const can = (permission: string) => permissions.includes(permission);
  const opener = useRef<HTMLButtonElement>(null);
  const [cursor, setCursor] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
  const [editing, setEditing] = useState<Staff | "new" | null>(null);
  const [documentStaff, setDocumentStaff] = useState<Staff | null>(null);
  const [credentialStaff, setCredentialStaff] = useState<Staff | null>(null);
  const [identityStaff, setIdentityStaff] = useState<Staff | null>(null);
  const [employmentStaff, setEmploymentStaff] = useState<Staff | null>(null);
  const [linkStaff, setLinkStaff] = useState<Staff | null>(null);
  useEffect(() => {
    if (!editing) opener.current?.focus();
  }, [editing]);
  const path = `/workforce/hospitals/${hospital.id}/staff`;
  const query = useApiQuery<Page<Staff>>(
    [
      "workforce",
      "staff",
      hospital.id,
      scope.tenantId,
      scope.subject,
      staffIntakeScopeKey(scope),
      cursor,
    ],
    withStaffIntakeScope(`${path}${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`, scope),
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
  const manager = choice.kind === "legacy" && choice.hospital.access_level === "manager";
  const canPrepare =
    choice.kind === "intake"
      ? choice.capabilities.can_prepare_staff
      : manager && can("workforce.enrollment.manage");
  const canDocuments =
    choice.kind === "intake"
      ? choice.capabilities.can_manage_staff_documents
      : manager && can("workforce.documents.manage");
  const writable = manager || canPrepare;
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
                const button = event.currentTarget;
                confirmLeave(() => {
                  opener.current = button;
                  setDocumentStaff(null);
                  setCredentialStaff(null);
                  setLinkStaff(null);
                  setIdentityStaff(null);
                  setEmploymentStaff(null);
                  setEditing("new");
                });
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
                      {choice.kind === "legacy" && (
                        <Button
                          variant="ghost"
                          size="sm"
                          aria-label={`Identity details for ${row.display_name}`}
                          onClick={(event) => {
                            const button = event.currentTarget;
                            confirmLeave(() => {
                              opener.current = button;
                              setEditing(null);
                              setDocumentStaff(null);
                              setCredentialStaff(null);
                              setLinkStaff(null);
                              setIdentityStaff(row);
                              setEmploymentStaff(null);
                            });
                          }}
                        >
                          Identity details
                        </Button>
                      )}
                      {canPrepare && (
                        <Button
                          variant="ghost"
                          size="sm"
                          aria-label={`Employment terms for ${row.display_name}`}
                          onClick={(event) => {
                            const button = event.currentTarget;
                            confirmLeave(() => {
                              opener.current = button;
                              setEditing(null);
                              setDocumentStaff(null);
                              setCredentialStaff(null);
                              setLinkStaff(null);
                              setIdentityStaff(null);
                              setEmploymentStaff(row);
                            });
                          }}
                        >
                          Employment terms
                        </Button>
                      )}
                      {writable && (
                        <>
                          {writable && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={(event) => {
                                const button = event.currentTarget;
                                confirmLeave(() => {
                                  opener.current = button;
                                  setDocumentStaff(null);
                                  setCredentialStaff(null);
                                  setLinkStaff(null);
                                  setIdentityStaff(null);
                                  setEmploymentStaff(null);
                                  setEditing(row);
                                });
                              }}
                              aria-label={`Edit ${row.display_name}`}
                            >
                              Edit
                            </Button>
                          )}
                          {canDocuments && (
                            <Button
                              variant="ghost"
                              size="sm"
                              aria-label={`Documents for ${row.display_name}`}
                              onClick={() =>
                                confirmLeave(() => {
                                  setEditing(null);
                                  setCredentialStaff(null);
                                  setLinkStaff(null);
                                  setIdentityStaff(null);
                                  setEmploymentStaff(null);
                                  setDocumentStaff(row);
                                })
                              }
                            >
                              Documents
                            </Button>
                          )}
                          {manager &&
                            (can("workforce.login_links.manage") ||
                              can("workforce.login_links.review")) && (
                              <Button
                                variant="ghost"
                                size="sm"
                                aria-label={`Account link for ${row.display_name}`}
                                onClick={() =>
                                  confirmLeave(() => {
                                    setEditing(null);
                                    setDocumentStaff(null);
                                    setCredentialStaff(null);
                                    setIdentityStaff(null);
                                    setEmploymentStaff(null);
                                    setLinkStaff(row);
                                  })
                                }
                              >
                                Account link
                              </Button>
                            )}
                          {manager && can("workforce.enrollment.manage") && (
                            <Button
                              variant="ghost"
                              size="sm"
                              aria-label={`Credentials for ${row.display_name}`}
                              onClick={() =>
                                confirmLeave(() => {
                                  setEditing(null);
                                  setDocumentStaff(null);
                                  setIdentityStaff(null);
                                  setEmploymentStaff(null);
                                  setCredentialStaff(row);
                                  setLinkStaff(null);
                                })
                              }
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
      {employmentStaff && canPrepare && (
        <StaffEmploymentPeriods
          canManageDocuments={canDocuments}
          staffLabel={`${employmentStaff.display_name} · ${employmentStaff.employee_code}`}
          key={employmentStaff.id}
          scope={{ ...scope, hospitalId: hospital.id, staffId: employmentStaff.id }}
          onContextChanged={onContextChanged}
          onClose={() => setEmploymentStaff(null)}
        />
      )}
      {identityStaff && (
        <StaffIdentitySourcePanel
          hospitalId={hospital.id}
          staffId={identityStaff.id}
          onClose={() => {
            setIdentityStaff(null);
            setEmploymentStaff(null);
            opener.current?.focus();
          }}
        />
      )}
      {linkStaff &&
        choice.kind === "legacy" &&
        manager &&
        (can("workforce.login_links.manage") || can("workforce.login_links.review")) && (
          <StaffLoginLinkPanel
            hospital={choice.hospital}
            staff={linkStaff}
            onClose={() => setLinkStaff(null)}
          />
        )}
      {documentStaff && canDocuments && (
        <StaffDocumentPanel
          scope={scope}
          onContextChanged={onContextChanged}
          key={documentStaff.id}
          hospital={hospital}
          staff={documentStaff}
          onClose={() => setDocumentStaff(null)}
        />
      )}
      {credentialStaff &&
        choice.kind === "legacy" &&
        manager &&
        can("workforce.enrollment.manage") && (
          <StaffCredentialPanel
            account={scope}
            key={credentialStaff.id}
            hospital={choice.hospital}
            staff={credentialStaff}
            onClose={() => setCredentialStaff(null)}
          />
        )}
      {editing && canPrepare ? (
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
        writable &&
        choice.kind === "legacy" && (
          <StaffDirectoryEditor
            key={editing === "new" ? "new" : editing.id}
            hospital={choice.hospital}
            onReloadDirectory={() => void query.refetch()}
            scope={scope}
            onContextChanged={onContextChanged}
            staff={editing === "new" ? undefined : editing}
            onClose={() => setEditing(null)}
          />
        )
      )}
    </>
  );
}
const hospitalSchema = z.object({
  name: z.string().trim().min(1, "Enter a hospital name.").max(150),
  grant: z.literal(true, { error: "Confirm your hospital access grant." }),
});
function HospitalSetup() {
  const confirmLeave = useConfirmLeave();
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
        onSubmit={form.handleSubmit((value) => confirmLeave(() => save.mutate(value)))}
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
