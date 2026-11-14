"use client";

import { useId, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  departmentLinkSchema,
  useDepartmentLinkHistory,
  useDepartmentLinkChoices,
  recordDepartmentLink,
  useApiQuery,
  useTenantTransition,
  type DepartmentLinkInput,
  type DepartmentLinkOutput,
  type HRMaster,
  type Hospital,
  type OrganizationScope,
} from "@helivanta/api";
import {
  Field,
  Panel,
  SelectControl,
  RequestFailure,
  useConfirmLeave,
  useLeaveGuard,
  useZodForm,
  RefreshButton,
} from "@helivanta/ui";
import { useOrganizationCommand } from "./command";

type Props = { hospital: Hospital; master: HRMaster; onClose: () => void };
const identitySchema = z.object({
  tenant_id: z.string().guid(),
  subject: z.string().min(1).max(200),
  data: z.array(z.string()),
});
const editorSchema = departmentLinkSchema.pick({ department_id: true, reason: true });

export function DepartmentHandoff(props: Props) {
  const [nonce] = useState(() => crypto.randomUUID());
  const switching = useTenantTransition();
  const identity = useApiQuery<unknown>(
    ["department-handoff", "identity", nonce],
    "/iam/me/permissions",
    { gcTime: 0, staleTime: 0, poll: true, request: { cache: "no-store" } },
  );
  if (switching || identity.isPending)
    return <p role="status">Checking department mapping access…</p>;
  const parsed = identitySchema.safeParse(identity.data);
  if (identity.isError || !parsed.success)
    return (
      <RequestFailure
        message="Could not verify department mapping access."
        retry={identity.refetch}
        error={identity.error}
      />
    );
  if (!parsed.data.data.includes("workforce.master.manage"))
    return <p role="alert">Department mapping access required</p>;
  const scope = { tenantId: parsed.data.tenant_id, subject: parsed.data.subject };
  return (
    <HandoffEditor
      key={`${scope.tenantId}:${scope.subject}:${props.hospital.id}:${props.master.id}:${props.master.revision_id}`}
      {...props}
      scope={scope}
      canReadOrganization={parsed.data.data.includes("organization.structure.read")}
    />
  );
}

function HandoffEditor({
  hospital,
  master,
  onClose,
  scope,
  canReadOrganization,
}: Props & { scope: OrganizationScope; canReadOrganization: boolean }) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [departmentCursor, setDepartmentCursor] = useState<string | null>(null);
  const [receipt, setReceipt] = useState<DepartmentLinkOutput | null>(null);
  const [retained, setRetained] = useState<DepartmentLinkInput | null>(null);
  const fieldId = useId();
  const confirm = useConfirmLeave();
  const history = useDepartmentLinkHistory(scope, hospital.id, master.id, cursor);
  const departments = useDepartmentLinkChoices(
    scope,
    hospital.id,
    departmentCursor,
    canReadOrganization,
  );
  const form = useZodForm(editorSchema, { department_id: "", reason: "" });
  const mutation = useOrganizationCommand<DepartmentLinkInput, DepartmentLinkOutput>(
    scope,
    (input, id, signal) => recordDepartmentLink(scope, hospital.id, master.id, id, input, signal),
    (result) => {
      setReceipt(result);
      form.reset(form.getValues());
      void history.refetch();
    },
  );
  useLeaveGuard(form.formState.isDirty, "department mapping", mutation.isPending);
  const latest = history.data?.data[0];
  const pending = latest?.status === "pending";
  const ownPending =
    pending &&
    retained !== null &&
    (receipt?.request_id === latest.request_id || (receipt === null && mutation.isError));
  const rows = departments.data?.data.filter((row) => row.status === "active") ?? [];
  const accessFailure =
    mutation.error instanceof ApiError &&
    ([401, 403, 404, 503].includes(mutation.error.status) ||
      mutation.error.code === "session_changed");
  if (history.isPending) return <p role="status">Loading department mapping history…</p>;
  if (history.isError || accessFailure)
    return (
      <RequestFailure
        message="Could not verify current department mapping access. Reopen the mapping to continue."
        retry={async () => {
          const result = await history.refetch();
          if (result.isSuccess) mutation.reset();
        }}
        error={history.error}
      />
    );
  const submit = form.handleSubmit(async (fields) => {
    const selected = rows.find((row) => row.id === fields.department_id);
    if (!ownPending && !selected) {
      form.setError("department_id", { message: "Choose an active department from this page." });
      return;
    }
    const retryUnconfirmed =
      retained !== null &&
      receipt === null &&
      mutation.isError &&
      retained.department_id === fields.department_id &&
      retained.reason === fields.reason &&
      retained.master_revision_id === master.revision_id;
    const input =
      ownPending || retryUnconfirmed
        ? retained!
        : {
            expected_version: latest?.version ?? 0,
            master_revision_id: master.revision_id,
            department_id: selected!.id,
            department_revision_id: selected!.revision_id,
            reason: fields.reason,
          };
    setRetained(input);
    try {
      await mutation.mutateAsync(input);
    } catch {
      /* The attributed command and fields remain available for retry. */
    }
  });
  return (
    <Panel className="mt-6" aria-label={`Department mapping for ${master.name}`}>
      <div className="mb-4 flex items-start justify-between gap-4">
        <div>
          <h3 className="font-semibold">Department mapping for {master.name}</h3>
          <p className="mt-2 max-w-3xl text-sm text-muted-foreground">
            Connect this retained HR vocabulary revision to the shared organization tree. A mapping
            records a snapshot; it does not assign Staff or grant access.
          </p>
          <p className="mt-2 break-all text-xs text-muted-foreground">
            HR revision {master.version} · {master.revision_id}
          </p>
        </div>
        <Button variant="outline" onClick={() => confirm(onClose)}>
          Close mapping
        </Button>
      </div>
      <RefreshButton refresh={history.refetch} error={history.error} busy={history.isFetching}>
        Refresh mapping history
      </RefreshButton>
      <ol className="my-4 space-y-3" aria-label="Department mapping history">
        {history.data?.data.map((row) => (
          <Panel as="li" key={row.request_id}>
            <p className="font-medium">
              Mapping {row.version}:{" "}
              {row.status === "pending"
                ? "Owner observation pending"
                : row.status === "matched"
                  ? "Matched snapshot"
                  : "Source revision did not match"}
            </p>
            <p className="mt-1 break-all text-xs">
              Department {row.department_id} · revision {row.department_revision_id}
            </p>
            <p className="mt-1 break-all text-xs text-muted-foreground">
              HR revision {row.master_revision_id}
              {row.master_revision_id !== master.revision_id ? " · Historical HR revision" : ""}
            </p>
            {row.observed_at && (
              <p className="mt-1 text-xs text-muted-foreground">
                Observed {new Date(row.observed_at).toLocaleString("en-GB", { timeZone: "UTC" })}{" "}
                UTC. Current eligibility must be checked separately.
              </p>
            )}
          </Panel>
        ))}
      </ol>
      {history.data?.data.length === 0 && (
        <p className="my-4 text-sm text-muted-foreground">No department mapping recorded.</p>
      )}
      <nav className="flex gap-2" aria-label="Department mapping pages">
        {cursor && (
          <Button variant="outline" onClick={() => confirm(() => setCursor(null))}>
            First mapping page
          </Button>
        )}
        {history.data?.page.next_cursor && (
          <Button
            variant="outline"
            onClick={() => confirm(() => setCursor(history.data!.page.next_cursor))}
          >
            Next mapping page
          </Button>
        )}
      </nav>
      {hospital.access_level !== "manager" ? (
        <p className="mt-4 text-sm text-muted-foreground">Read-only department mapping history.</p>
      ) : !master.active ? (
        <p className="mt-4">Retired HR vocabulary cannot receive a new mapping.</p>
      ) : cursor ? (
        <p className="mt-4">Return to the first history page to prepare a mapping.</p>
      ) : !canReadOrganization ? (
        <p className="mt-4">
          Organization structure read access is required to select a canonical department.
        </p>
      ) : departments.isPending ? (
        <p role="status">Loading canonical departments…</p>
      ) : departments.isError ? (
        <RequestFailure
          message="Could not verify canonical departments."
          retry={departments.refetch}
          error={departments.error}
        />
      ) : (
        <form noValidate className="mt-5 space-y-4 border-t pt-5" onSubmit={submit}>
          <Field
            id={`${fieldId}-department`}
            label="Canonical department or sub-unit"
            error={form.formState.errors.department_id?.message}
          >
            <SelectControl
              id={`${fieldId}-department`}
              value={form.watch("department_id")}
              onValueChange={(value) =>
                form.setValue("department_id", value, { shouldDirty: true, shouldValidate: true })
              }
              disabled={pending || mutation.isPending}
              placeholder="Choose a canonical department"
              options={rows.map((row) => ({
                value: row.id,
                label: `${row.name} · ${row.code} · v${row.version}`,
              }))}
            />
          </Field>
          <nav className="flex gap-2" aria-label="Canonical department pages">
            {departmentCursor && (
              <Button
                type="button"
                variant="outline"
                disabled={pending || mutation.isPending}
                onClick={() =>
                  confirm(() => {
                    setDepartmentCursor(null);
                    form.setValue("department_id", "");
                  })
                }
              >
                First department page
              </Button>
            )}
            {departments.data?.page.next_cursor && (
              <Button
                type="button"
                variant="outline"
                disabled={pending || mutation.isPending}
                onClick={() =>
                  confirm(() => {
                    setDepartmentCursor(departments.data!.page.next_cursor);
                    form.setValue("department_id", "");
                  })
                }
              >
                Next department page
              </Button>
            )}
          </nav>
          <Field
            id={`${fieldId}-reason`}
            label="Mapping reason"
            error={
              form.formState.errors.reason
                ? "Enter a mapping reason (1–500 characters)."
                : undefined
            }
          >
            <Input
              id={`${fieldId}-reason`}
              disabled={pending || mutation.isPending}
              {...form.register("reason")}
            />
          </Field>
          {mutation.isError && (
            <p role="alert">
              The department mapping could not be recorded. Check the source revisions or retry the
              unchanged command if its outcome is unknown.
            </p>
          )}
          {pending && (
            <p className="text-sm text-muted-foreground">
              The owner observation is pending. The original requested revisions remain pinned; this
              is not current posting or operating authority.
            </p>
          )}
          <Button type="submit" disabled={mutation.isPending || (pending && !ownPending)}>
            {mutation.isPending
              ? "Recording mapping…"
              : ownPending
                ? "Retry pending observation"
                : "Record department mapping"}
          </Button>
        </form>
      )}
    </Panel>
  );
}
