"use client";

import { useEffect, useId, useState } from "react";
import { z } from "zod";
import { Button } from "@tesserix/web";
import {
  isEnrollmentAccessChanged,
  supplierStaffKey,
  saveSupplierStaffRecord,
  useApiQuery,
  usePermissions,
  newIndependentStaffLinkId,
  useSupplierStaffPage,
  useSupplierStaffRecord,
  useSupplierStaffSourcesPage,
  type EnrollmentRequestScope,
  type SupplierStaff,
  type SupplierStaffSource as EmployerSource,
} from "@helivanta/api";
import {
  Field,
  PageHeading,
  Panel,
  RequestFailure,
  SelectControl,
  StaffDirectory,
  useConfirmLeave,
} from "@helivanta/ui";
import { IndependentOperatingDuties } from "@/components/workforce/independent-operating-duties";
import { IndependentStaffLinks } from "@/components/workforce/independent-staff-links";
import { StaffAccessPreparation } from "@/components/workforce/staff-access-preparation";
import { StaffRecordForm } from "@/components/workforce/staff-record-form";

const identitySchema = z.object({
  tenant_id: z
    .string()
    .guid()
    .refine((value) => value !== "00000000-0000-0000-0000-000000000000"),
  subject: z.string().min(1).max(200),
  data: z.array(z.string()),
});

export function SupplierStaffSource() {
  const [attempt, setAttempt] = useState(0);
  return <StaffSourceAccount key={attempt} onRestart={() => setAttempt((value) => value + 1)} />;
}

function StaffSourceAccount({ onRestart }: { onRestart: () => void }) {
  const [nonce] = useState(() => crypto.randomUUID());
  const [changed, setChanged] = useState(false);
  const identity = useApiQuery<unknown>(
    ["supplier", "staff-source", "identity", nonce],
    "/iam/me/permissions",
    {
      gcTime: 0,
      staleTime: 0,
      poll: true,
      refetchOnWindowFocus: false,
      request: { cache: "no-store" },
    },
  );
  if (changed)
    return (
      <Panel role="alert" className="space-y-3">
        <p>
          Your account or Staff access changed. Reopen the source to verify your current access.
        </p>
        <Button onClick={onRestart}>Reopen supplier Staff source</Button>
      </Panel>
    );
  if (identity.isPending) return <p role="status">Checking supplier Staff access…</p>;
  const parsed = identitySchema.safeParse(identity.data);
  if (identity.isError || !parsed.success)
    return (
      <RequestFailure
        message="Could not verify your supplier Staff workspace."
        retry={identity.refetch}
        error={identity.error}
      />
    );
  const scope = { tenantId: parsed.data.tenant_id, subject: parsed.data.subject };
  return (
    <StaffSources
      key={`${scope.tenantId}:${scope.subject}`}
      scope={scope}
      onContextChanged={() => setChanged(true)}
    />
  );
}

type SourceProps = { scope: EnrollmentRequestScope; onContextChanged: () => void };
function StaffSources(props: SourceProps) {
  return (
    <>
      <PageHeading description="Manage your independent supplier's Staff directory and reviewed accounts." />
      <EmployerSources {...props} />
    </>
  );
}

// A standalone supplier registered as an employer keeps its own directory (ADR-0054).
function EmployerSources({ scope, onContextChanged }: SourceProps) {
  const [cursor, setCursor] = useState<string | null>(null);
  const query = useSupplierStaffSourcesPage(scope, cursor);
  useEffect(() => {
    if (isEnrollmentAccessChanged(query.error)) onContextChanged();
  }, [query.error, onContextChanged]);
  return (
    <section className="mb-10">
      <Panel className="mb-5 space-y-2">
        <h2 className="font-semibold">Supplier employer Staff</h2>
        <p className="text-sm text-muted-foreground">
          Staff employed by standalone suppliers registered as employers. Being listed here does not
          assign someone to an outlet or authorize dispensing.
        </p>
      </Panel>
      {query.isPending ? (
        <p role="status">Loading supplier employers…</p>
      ) : query.isError ? (
        <RequestFailure
          message="Could not load supplier employers."
          retry={query.refetch}
          error={query.error}
        />
      ) : query.data.data.length > 0 ? (
        <EmployerSourcePage
          key={cursor ?? "first"}
          {...{ scope, onContextChanged }}
          sources={query.data.data}
        />
      ) : (
        <Panel>
          <h3 className="font-semibold">No supplier employer access on this page</h3>
          <p className="mt-2 text-sm text-muted-foreground">
            Whoever registers a supplier as an employer becomes its first Staff manager. Ask them
            for access to its Staff records.
          </p>
        </Panel>
      )}
      {!query.isPending && !query.isError && (
        <nav aria-label="Supplier employers" className="mt-4 flex gap-2">
          {cursor && (
            <Button variant="outline" onClick={() => setCursor(null)}>
              First supplier page
            </Button>
          )}
          {query.data.page.has_more && query.data.page.next_cursor && (
            <Button variant="outline" onClick={() => setCursor(query.data.page.next_cursor)}>
              Next supplier page
            </Button>
          )}
        </nav>
      )}
    </section>
  );
}

function EmployerSourcePage({
  scope,
  onContextChanged,
  sources,
}: SourceProps & { sources: EmployerSource[] }) {
  const id = useId();
  const [selected, setSelected] = useState("");
  const source = selected ? sources.find((item) => item.supplier_id === selected) : sources[0];
  return (
    <>
      <div className="mb-5 max-w-xl">
        <Field id={id} label="Supplier employer">
          <SelectControl
            id={id}
            value={source?.supplier_id ?? ""}
            onValueChange={setSelected}
            options={sources.map((item) => ({ value: item.supplier_id, label: item.name }))}
          />
        </Field>
      </div>
      {source ? (
        <Employer
          key={`${scope.tenantId}:${scope.subject}:${source.supplier_id}`}
          {...{ scope, onContextChanged, source }}
        />
      ) : (
        <p>Choose a currently accessible supplier employer.</p>
      )}
    </>
  );
}

function Employer({ scope, onContextChanged, source }: SourceProps & { source: EmployerSource }) {
  const confirmLeave = useConfirmLeave();
  const [cursor, setCursor] = useState<string | null>(null);
  const [editing, setEditing] = useState<SupplierStaff | "new" | null>(null);
  const query = useSupplierStaffPage(scope, source.supplier_id, cursor);
  // The access level is display only; the API checks the manager row on every write.
  const manager = source.access_level === "manager";
  useEffect(() => {
    if (isEnrollmentAccessChanged(query.error)) onContextChanged();
  }, [query.error, onContextChanged]);
  return (
    <>
      <StaffDirectory<SupplierStaff>
        title={source.name}
        description={
          manager
            ? "Supplier Staff directory · you manage these records"
            : "Supplier Staff directory"
        }
        emptyDescription="No Staff records are in this supplier's directory yet."
        scopeDescription="Showing this supplier's own Staff records; no outlet assignment is implied"
        rows={query.data?.data ?? []}
        isPending={query.isPending}
        isError={query.isError}
        error={query.error}
        retry={() => query.refetch()}
        headerActions={
          manager && (
            <Button onClick={() => confirmLeave(() => setEditing("new"))}>Add staff</Button>
          )
        }
        rowActions={
          manager
            ? (row) => (
                <Button
                  variant="ghost"
                  size="sm"
                  aria-label={`Edit ${row.display_name}`}
                  onClick={() => confirmLeave(() => setEditing(row))}
                >
                  Edit
                </Button>
              )
            : undefined
        }
        onFirstPage={cursor ? () => setCursor(null) : undefined}
        onNextPage={
          query.data?.page.has_more && query.data.page.next_cursor
            ? () => setCursor(query.data!.page.next_cursor)
            : undefined
        }
      />
      {manager && editing && (
        <EmployerStaffEditor
          key={editing === "new" ? "new" : editing.id}
          {...{ scope, onContextChanged, source }}
          staff={editing === "new" ? undefined : editing}
          onClose={() => setEditing(null)}
        />
      )}
    </>
  );
}

function EmployerStaffEditor({
  scope,
  onContextChanged,
  source,
  staff,
  onClose,
}: SourceProps & { source: EmployerSource; staff?: SupplierStaff; onClose: () => void }) {
  const { can } = usePermissions();
  const [id] = useState(() => staff?.id ?? newIndependentStaffLinkId());
  const [reload, setReload] = useState(0);
  const record = useSupplierStaffRecord(scope, source.supplier_id, id, reload, !!staff);
  useEffect(() => {
    if (isEnrollmentAccessChanged(record.error)) onContextChanged();
  }, [record.error, onContextChanged]);
  if (staff && record.isPending)
    return (
      <Panel role="status" className="mt-6">
        Loading current Staff record…
      </Panel>
    );
  if (staff && record.isError)
    return (
      <div className="mt-6 space-y-3">
        <RequestFailure
          message="The current Staff record is not available. Retry before editing."
          retry={record.refetch}
          error={record.error}
        />
        <Button variant="ghost" onClick={onClose}>
          Cancel
        </Button>
      </div>
    );
  return (
    <>
      <StaffRecordForm
        key={reload}
        establishmentName={source.name}
        staff={staff ? record.data : undefined}
        invalidate={[supplierStaffKey(scope, source.supplier_id)]}
        onClose={onClose}
        onContextChanged={onContextChanged}
        onReload={() => setReload((value) => value + 1)}
        save={(value, expectedVersion) =>
          saveSupplierStaffRecord(scope, source.supplier_id, id, value, expectedVersion)
        }
      />
      {staff && record.data && (
        <StaffAccessPreparation
          kind="supplier"
          active={record.data.employment_status === "active"}
        />
      )}
      {staff &&
        record.data &&
        (can("workforce.independent_login.manage") ||
          can("workforce.independent_login.review")) && (
          <IndependentStaffLinks
            scope={{
              ...scope,
              employerKind: "supplier",
              employerId: source.supplier_id,
              staffId: staff.id,
            }}
            staffVersion={record.data.version}
            canManage={
              can("workforce.independent_login.manage") &&
              record.data.employment_status === "active"
            }
            canReview={
              can("workforce.independent_login.review") &&
              record.data.employment_status === "active"
            }
          />
        )}
      {staff && record.data && can("iam.operating.read") && (
        <IndependentOperatingDuties
          scope={{
            ...scope,
            employerKind: "supplier",
            employerId: source.supplier_id,
            staffId: staff.id,
          }}
          staffVersion={record.data.version}
          staffActive={record.data.employment_status === "active"}
        />
      )}
    </>
  );
}
