"use client";
import { useId, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  newOrganizationId,
  organizationDepartmentEditorContract,
  recordOrganizationDepartment,
  registerOrganizationHospital,
  useApiQuery,
  useOrganizationDepartments,
  useOrganizationDepartmentRevisions,
  useOrganizationHospitals,
  useTenantTransition,
  type OrganizationDepartment,
  type OrganizationDepartmentCommand,
  type OrganizationScope,
} from "@helivanta/api";
import {
  Field,
  PageHeading,
  Panel,
  RequestFailure,
  SelectControl,
  useConfirmLeave,
  useLeaveGuard,
  useZodForm,
  RefreshButton,
} from "@helivanta/ui";
import { useOrganizationCommand } from "./command";

const identityContract = z.object({
  tenant_id: z.string().guid(),
  subject: z.string().min(1).max(200),
  data: z.array(z.string()),
});
export function OrganizationStructure() {
  const [nonce] = useState(() => crypto.randomUUID());
  const switching = useTenantTransition();
  const identity = useApiQuery<unknown>(
    ["organization", "identity", nonce],
    "/iam/me/permissions",
    { gcTime: 0, staleTime: 0, poll: true, request: { cache: "no-store" } },
  );
  if (switching || identity.isPending) return <p role="status">Checking organization access…</p>;
  const parsed = identityContract.safeParse(identity.data);
  if (identity.isError || !parsed.success)
    return (
      <RequestFailure
        message="Could not verify organization access."
        retry={identity.refetch}
        error={identity.error}
      />
    );
  if (!parsed.data.data.includes("organization.structure.read"))
    return <Panel role="alert">Organization structure access required</Panel>;
  const scope = { tenantId: parsed.data.tenant_id, subject: parsed.data.subject };
  return (
    <StructureHospitals
      key={`${scope.tenantId}:${scope.subject}`}
      scope={scope}
      canManage={parsed.data.data.includes("organization.structure.manage")}
    />
  );
}
type Props = { scope: OrganizationScope; canManage: boolean };
function PageButtons({
  cursor,
  next,
  loading,
  onPage,
}: {
  cursor: string | null;
  next: string | null;
  loading: boolean;
  onPage: (cursor: string | null) => void;
}) {
  const confirm = useConfirmLeave();
  return (
    <nav aria-label="Organization pages" className="flex flex-wrap gap-2">
      {cursor && (
        <Button variant="outline" disabled={loading} onClick={() => confirm(() => onPage(null))}>
          First page
        </Button>
      )}
      {next && (
        <Button variant="outline" disabled={loading} onClick={() => confirm(() => onPage(next))}>
          Next page
        </Button>
      )}
    </nav>
  );
}
function StructureHospitals({ scope, canManage }: Props) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [hospital, setHospital] = useState<string | null>(null);
  const [registering, setRegistering] = useState(false);
  const query = useOrganizationHospitals(scope, cursor);
  const confirm = useConfirmLeave();
  return (
    <div className="space-y-6">
      <PageHeading description="The shared department structure used by Staff and product teams. Department membership and product duties are managed separately." />
      {hospital ? (
        <>
          <Button variant="outline" onClick={() => confirm(() => setHospital(null))}>
            Back to hospital references
          </Button>
          <DepartmentDirectory key={hospital} {...{ scope, canManage, hospital }} />
        </>
      ) : (
        <>
          <Panel className="space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h2 className="text-xl font-semibold">Hospital department structure</h2>
              <div className="flex gap-2">
                <RefreshButton refresh={query.refetch} error={query.error}>
                  Refresh hospital references
                </RefreshButton>
                {canManage && (
                  <Button onClick={() => setRegistering(true)}>Register hospital reference</Button>
                )}
              </div>
            </div>
            <p className="text-sm text-muted-foreground">
              A verified reference means the hospital exists in the source directory. Professional
              approval and operating permissions are separate.
            </p>
            {query.isPending ? (
              <p role="status">Loading hospital references…</p>
            ) : query.isError ? (
              <RequestFailure
                message="Could not load hospital references."
                retry={query.refetch}
                error={query.error}
              />
            ) : (
              <>
                {query.data?.data.length === 0 ? (
                  <p>No hospital references have been registered.</p>
                ) : (
                  <ul className="divide-y">
                    {query.data?.data.map((root) => (
                      <li
                        key={root.hospital_id}
                        className="flex flex-wrap items-center justify-between gap-3 py-4"
                      >
                        <div>
                          <p className="font-medium">Hospital {root.hospital_id}</p>
                          <p className="text-sm text-muted-foreground">Reference {root.status}</p>
                        </div>
                        {root.status === "verified" && (
                          <Button
                            variant="outline"
                            aria-label={`Open departments for ${root.hospital_id}`}
                            onClick={() =>
                              confirm(() => {
                                setRegistering(false);
                                setHospital(root.hospital_id);
                              })
                            }
                          >
                            Open departments
                          </Button>
                        )}
                      </li>
                    ))}
                  </ul>
                )}
                <PageButtons
                  cursor={cursor}
                  next={query.data?.page.next_cursor ?? null}
                  loading={query.isFetching}
                  onPage={setCursor}
                />
              </>
            )}
          </Panel>
          {registering && canManage && (
            <ReferenceForm scope={scope} onClose={() => setRegistering(false)} />
          )}
        </>
      )}
    </div>
  );
}
const referenceContract = z.object({
  hospital: z.string().guid("Enter the hospital ID from the source directory."),
  reason: z.string().trim().min(1, "Enter a reason for registering the reference.").max(500),
});
function ReferenceForm({ scope, onClose }: { scope: OrganizationScope; onClose: () => void }) {
  const prefix = useId();
  const form = useZodForm(referenceContract, { hospital: "", reason: "" });
  const save = useOrganizationCommand(
    scope,
    (input: z.infer<typeof referenceContract>, id, signal) =>
      registerOrganizationHospital(scope, input.hospital, id, input.reason, signal),
    onClose,
  );
  const guard = useLeaveGuard(form.formState.isDirty, "hospital reference", save.isPending);
  return (
    <Panel className="space-y-4">
      <h3 className="text-lg font-semibold">Register a hospital reference</h3>
      <form
        noValidate
        onSubmit={form.handleSubmit((input) => save.mutate(input))}
        className="space-y-4"
      >
        <Field
          id={`${prefix}-hospital`}
          label="Hospital ID"
          error={form.formState.errors.hospital?.message}
        >
          <Input
            id={`${prefix}-hospital`}
            {...form.register("hospital")}
            aria-invalid={Boolean(form.formState.errors.hospital)}
            aria-describedby={
              form.formState.errors.hospital ? `${prefix}-hospital-error` : undefined
            }
          />
        </Field>
        <Field
          id={`${prefix}-reason`}
          label="Reason for registration"
          error={form.formState.errors.reason?.message}
        >
          <Input
            id={`${prefix}-reason`}
            {...form.register("reason")}
            aria-invalid={Boolean(form.formState.errors.reason)}
            aria-describedby={form.formState.errors.reason ? `${prefix}-reason-error` : undefined}
          />
        </Field>
        {save.error && (
          <p role="alert" className="text-sm text-destructive">
            {save.error.message}
          </p>
        )}
        <p className="text-sm text-muted-foreground">
          The source owner checks the reference. Refresh the directory to see the result.
        </p>
        <div className="flex gap-2">
          <Button type="submit" disabled={save.isPending}>
            {save.isPending ? "Registering…" : "Request source check"}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={save.isPending}
            onClick={() => guard.confirm(onClose)}
          >
            Cancel
          </Button>
        </div>
      </form>
    </Panel>
  );
}
function DepartmentDirectory({ scope, canManage, hospital }: Props & { hospital: string }) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [editor, setEditor] = useState<OrganizationDepartment | "new" | null>(null);
  const [history, setHistory] = useState<OrganizationDepartment | null>(null);
  const query = useOrganizationDepartments(scope, hospital, cursor);
  const confirm = useConfirmLeave();
  const rows = query.isError ? [] : (query.data?.data ?? []);
  return (
    <div className="space-y-6">
      <Panel className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="text-xl font-semibold">Departments and sub-units</h2>
            <p className="mt-1 break-all text-sm text-muted-foreground">Hospital {hospital}</p>
          </div>
          <div className="flex flex-wrap gap-2">
            <RefreshButton refresh={query.refetch} error={query.error}>
              Refresh departments
            </RefreshButton>
            {canManage && (
              <Button
                disabled={query.isPending || query.isError}
                onClick={() => confirm(() => setEditor("new"))}
              >
                Add department or sub-unit
              </Button>
            )}
          </div>
        </div>
        <p className="text-sm text-muted-foreground">
          These units describe the organization. They do not assign Staff or grant outlet, clinical
          or login access.
        </p>
        {query.isPending ? (
          <p role="status">Loading departments…</p>
        ) : query.isError ? (
          <RequestFailure
            message="Could not load departments. Current access must be verified before editing."
            retry={query.refetch}
            error={query.error}
          />
        ) : (
          <>
            {rows.length === 0 ? (
              <p>No departments on this page.</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-left text-sm">
                  <caption className="sr-only">Departments at hospital {hospital}</caption>
                  <thead className="border-b text-muted-foreground">
                    <tr>
                      {["Name", "Code", "Type", "Parent", "Status", "Actions"].map((label) => (
                        <th key={label} className="px-3 py-3 font-medium">
                          {label}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {rows.map((row) => (
                      <tr key={row.id}>
                        <td className="px-3 py-4 font-medium">{row.name}</td>
                        <td className="px-3 py-4">{row.code}</td>
                        <td className="px-3 py-4">
                          {row.kind === "department" ? "Department" : "Sub-unit"}
                        </td>
                        <td className="max-w-56 break-all px-3 py-4">
                          {row.parent_id
                            ? (rows.find((parent) => parent.id === row.parent_id)?.name ??
                              `Parent ${row.parent_id}`)
                            : "Hospital"}
                        </td>
                        <td className="px-3 py-4 capitalize">{row.status}</td>
                        <td className="px-3 py-4">
                          <div className="flex gap-2">
                            <Button
                              variant="outline"
                              aria-label={`History of ${row.name}`}
                              onClick={() => confirm(() => setHistory(row))}
                            >
                              History
                            </Button>
                            {canManage && row.status === "active" && (
                              <Button
                                variant="outline"
                                aria-label={`Edit ${row.name}`}
                                onClick={() => confirm(() => setEditor(row))}
                              >
                                Edit
                              </Button>
                            )}
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <PageButtons
              cursor={cursor}
              next={query.data?.page.next_cursor ?? null}
              loading={query.isFetching}
              onPage={(value) => {
                setCursor(value);
                setEditor(null);
                setHistory(null);
              }}
            />
          </>
        )}
      </Panel>
      {editor && canManage && !query.isError && (
        <DepartmentEditor
          key={editor === "new" ? "new" : `${editor.id}:${editor.version}`}
          {...{ scope, hospital, rows }}
          existing={editor === "new" ? undefined : editor}
          onClose={() => setEditor(null)}
        />
      )}
      {history && !query.isError && (
        <DepartmentHistory
          key={history.id}
          {...{ scope, hospital }}
          node={history}
          onClose={() => setHistory(null)}
        />
      )}
    </div>
  );
}
function DepartmentEditor({
  scope,
  hospital,
  rows,
  existing,
  onClose,
}: {
  scope: OrganizationScope;
  hospital: string;
  rows: OrganizationDepartment[];
  existing?: OrganizationDepartment;
  onClose: () => void;
}) {
  const prefix = useId();
  const [nodeId] = useState(() => existing?.id ?? newOrganizationId());
  const form = useZodForm(organizationDepartmentEditorContract, {
    expected_version: existing?.version ?? 0,
    code: existing?.code ?? "",
    name: existing?.name ?? "",
    kind: existing?.kind ?? "department",
    parent_id: existing?.parent_id ?? null,
    status: existing?.status ?? "active",
    reason: "",
  });
  const save = useOrganizationCommand(
    scope,
    (input: OrganizationDepartmentCommand, id, signal) =>
      recordOrganizationDepartment(scope, hospital, nodeId, id, input, signal),
    onClose,
  );
  const guard = useLeaveGuard(form.formState.isDirty, "department revision", save.isPending);
  const kind = form.watch("kind");
  const parentChoices = rows
    .filter((row) => row.status === "active" && row.id !== nodeId)
    .map((row) => ({ value: row.id, label: `${row.name} (${row.code})` }));
  if (existing?.parent_id && !parentChoices.some((row) => row.value === existing.parent_id))
    parentChoices.push({
      value: existing.parent_id,
      label: `Current parent ${existing.parent_id}`,
    });
  const input = (name: "code" | "name" | "reason", label: string) => (
    <Field
      id={`${prefix}-${name}`}
      label={label}
      error={
        form.formState.errors[name]
          ? name === "reason"
            ? "Enter a reason for this change (1–500 characters)."
            : form.formState.errors[name]?.message
          : undefined
      }
    >
      <Input
        id={`${prefix}-${name}`}
        {...form.register(name)}
        readOnly={name === "code" && Boolean(existing)}
        aria-invalid={Boolean(form.formState.errors[name])}
        aria-describedby={form.formState.errors[name] ? `${prefix}-${name}-error` : undefined}
      />
    </Field>
  );
  return (
    <Panel className="space-y-4">
      <h3 className="text-lg font-semibold">
        {existing ? `Edit ${existing.name}` : "Add department or sub-unit"}
      </h3>
      <p className="text-sm text-muted-foreground">
        {existing
          ? `Based on revision ${existing.version}. Code and type remain fixed.`
          : "Choose a stable code and the correct unit type."}
      </p>
      <form
        noValidate
        onSubmit={form.handleSubmit((values) => save.mutate(values))}
        className="space-y-4"
      >
        <div className="grid gap-4 md:grid-cols-2">
          {input("code", "Department code")}
          {input("name", "Name")}
          <Field
            id={`${prefix}-kind`}
            label="Unit type"
            error={form.formState.errors.kind?.message}
          >
            {existing ? (
              <Input
                id={`${prefix}-kind`}
                value={existing.kind === "department" ? "Department" : "Sub-unit"}
                readOnly
              />
            ) : (
              <SelectControl
                id={`${prefix}-kind`}
                value={kind}
                onValueChange={(value) => {
                  form.setValue("kind", value as OrganizationDepartmentCommand["kind"], {
                    shouldDirty: true,
                  });
                  form.setValue("parent_id", null, { shouldDirty: true });
                }}
                options={[
                  { value: "department", label: "Department" },
                  { value: "sub_unit", label: "Sub-unit" },
                ]}
              />
            )}
          </Field>
          {kind === "sub_unit" && (
            <Field
              id={`${prefix}-parent`}
              label="Parent department or sub-unit"
              error={form.formState.errors.parent_id?.message}
            >
              <SelectControl
                id={`${prefix}-parent`}
                value={form.watch("parent_id") ?? ""}
                onValueChange={(value) =>
                  form.setValue("parent_id", value, { shouldDirty: true, shouldValidate: true })
                }
                options={parentChoices}
              />
              <p className="text-xs font-normal text-muted-foreground">
                Choose a parent from this page. Refresh or change pages before starting another edit
                if needed.
              </p>
            </Field>
          )}
          {existing && (
            <Field
              id={`${prefix}-status`}
              label="Unit status"
              error={form.formState.errors.status?.message}
            >
              <SelectControl
                id={`${prefix}-status`}
                value={form.watch("status")}
                onValueChange={(value) =>
                  form.setValue("status", value as "active" | "retired", { shouldDirty: true })
                }
                options={[
                  { value: "active", label: "Active" },
                  { value: "retired", label: "Retired" },
                ]}
              />
              <p className="text-xs font-normal text-muted-foreground">
                Retirement is permanent. Active children must first be moved or retired.
              </p>
            </Field>
          )}
        </div>
        {input("reason", "Reason for change")}
        {save.error && (
          <p role="alert" className="text-sm text-destructive">
            {save.error.message} If the save result is uncertain, retry unchanged or refresh before
            editing again.
          </p>
        )}
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={save.isPending}>
            {save.isPending ? "Saving revision…" : "Save department revision"}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={save.isPending}
            onClick={() => guard.confirm(onClose)}
          >
            Cancel
          </Button>
        </div>
      </form>
    </Panel>
  );
}
function DepartmentHistory({
  scope,
  hospital,
  node,
  onClose,
}: {
  scope: OrganizationScope;
  hospital: string;
  node: OrganizationDepartment;
  onClose: () => void;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const query = useOrganizationDepartmentRevisions(scope, hospital, node.id, cursor);
  return (
    <Panel className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 className="text-lg font-semibold">History of {node.name}</h3>
        <Button variant="outline" onClick={onClose}>
          Close history
        </Button>
      </div>
      {query.isPending ? (
        <p role="status">Loading department history…</p>
      ) : query.isError ? (
        <RequestFailure
          message="Could not verify department history."
          retry={query.refetch}
          error={query.error}
        />
      ) : (
        <>
          <ol className="divide-y">
            {query.data?.data.map((row) => (
              <li key={row.revision_id} className="space-y-1 py-3">
                <p className="font-medium">
                  Revision {row.version}: {row.name}
                </p>
                <p className="text-sm text-muted-foreground">
                  {row.code} · {row.status} · Effective{" "}
                  {new Date(row.effective_at).toLocaleString(undefined, {
                    timeZone: "UTC",
                    timeZoneName: "short",
                  })}
                </p>
              </li>
            ))}
          </ol>
          <PageButtons
            cursor={cursor}
            next={query.data?.page.next_cursor ?? null}
            loading={query.isFetching}
            onPage={setCursor}
          />
        </>
      )}
    </Panel>
  );
}
