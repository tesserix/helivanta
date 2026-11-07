"use client";

import { useEffect, useId, useState } from "react";
import { z } from "zod";
import { Button } from "@tesserix/web";
import {
  isEnrollmentAccessChanged,
  pharmacyStaffKey,
  savePharmacyStaffRecord,
  useApiQuery,
  usePharmacyStaffPage,
  usePharmacyStaffRecord,
  usePharmacyStaffSourcesPage,
  useStaffDirectoryPage,
  useStaffHospitalsPage,
  type EnrollmentRequestScope,
  type Hospital,
  type PharmacyStaff,
  type PharmacyStaffSource as EmployerSource,
} from "@helivanta/api";
import {
  Field,
  GuardedLink,
  PageHeading,
  Panel,
  RequestFailure,
  SelectControl,
  StaffDirectory,
  useConfirmLeave,
} from "@helivanta/ui";
import { StaffRecordForm } from "@/components/workforce/staff-record-form";

const identitySchema = z.object({
  tenant_id: z
    .string()
    .guid()
    .refine((value) => value !== "00000000-0000-0000-0000-000000000000"),
  subject: z.string().min(1).max(200),
  data: z.array(z.string()),
});

export function PharmacyStaffSource() {
  const [attempt, setAttempt] = useState(0);
  return <StaffSourceAccount key={attempt} onRestart={() => setAttempt((value) => value + 1)} />;
}

function StaffSourceAccount({ onRestart }: { onRestart: () => void }) {
  const [nonce] = useState(() => crypto.randomUUID());
  const [changed, setChanged] = useState(false);
  const identity = useApiQuery<unknown>(
    ["pharmacy", "staff-source", "identity", nonce],
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
        <Button onClick={onRestart}>Reopen pharmacy Staff source</Button>
      </Panel>
    );
  if (identity.isPending) return <p role="status">Checking pharmacy Staff access…</p>;
  const parsed = identitySchema.safeParse(identity.data);
  if (identity.isError || !parsed.success)
    return (
      <RequestFailure
        message="Could not verify your pharmacy Staff workspace."
        retry={identity.refetch}
        error={identity.error}
      />
    );
  if (!parsed.data.data.includes("pharmacy.catalogue.read"))
    return (
      <Panel role="alert">You do not have permission to open the pharmacy Staff source.</Panel>
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
      <PageHeading description="Keep your pharmacy's own Staff records and read hospital Staff records available to you." />
      <EmployerSources {...props} />
      <StaffSourceHospitals {...props} />
    </>
  );
}

// A standalone pharmacy registered as an employer keeps its own directory (ADR-0054).
function EmployerSources({ scope, onContextChanged }: SourceProps) {
  const [cursor, setCursor] = useState<string | null>(null);
  const query = usePharmacyStaffSourcesPage(scope, cursor);
  useEffect(() => {
    if (isEnrollmentAccessChanged(query.error)) onContextChanged();
  }, [query.error, onContextChanged]);
  return (
    <section className="mb-10">
      <Panel className="mb-5 space-y-2">
        <h2 className="font-semibold">Pharmacy employer Staff</h2>
        <p className="text-sm text-muted-foreground">
          Staff employed by standalone pharmacies registered as employers. Being listed here does
          not assign someone to an outlet or authorize dispensing.
        </p>
      </Panel>
      {query.isPending ? (
        <p role="status">Loading pharmacy employers…</p>
      ) : query.isError ? (
        <RequestFailure
          message="Could not load pharmacy employers."
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
          <h3 className="font-semibold">No pharmacy employer access on this page</h3>
          <p className="mt-2 text-sm text-muted-foreground">
            Whoever registers a pharmacy as an employer becomes its first Staff manager. Ask them
            for access to its Staff records.
          </p>
        </Panel>
      )}
      {!query.isPending && !query.isError && (
        <nav aria-label="Pharmacy employers" className="mt-4 flex gap-2">
          {cursor && (
            <Button variant="outline" onClick={() => setCursor(null)}>
              First pharmacy page
            </Button>
          )}
          {query.data.page.has_more && query.data.page.next_cursor && (
            <Button variant="outline" onClick={() => setCursor(query.data.page.next_cursor)}>
              Next pharmacy page
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
  const source = selected ? sources.find((item) => item.pharmacy_id === selected) : sources[0];
  return (
    <>
      <div className="mb-5 max-w-xl">
        <Field id={id} label="Pharmacy employer">
          <SelectControl
            id={id}
            value={source?.pharmacy_id ?? ""}
            onValueChange={setSelected}
            options={sources.map((item) => ({ value: item.pharmacy_id, label: item.name }))}
          />
        </Field>
      </div>
      {source ? (
        <Employer
          key={`${scope.tenantId}:${scope.subject}:${source.pharmacy_id}`}
          {...{ scope, onContextChanged, source }}
        />
      ) : (
        <p>Choose a currently accessible pharmacy employer.</p>
      )}
    </>
  );
}

function Employer({ scope, onContextChanged, source }: SourceProps & { source: EmployerSource }) {
  const confirmLeave = useConfirmLeave();
  const [cursor, setCursor] = useState<string | null>(null);
  const [editing, setEditing] = useState<PharmacyStaff | "new" | null>(null);
  const query = usePharmacyStaffPage(scope, source.pharmacy_id, cursor);
  // The access level is display only; the API checks the manager row on every write.
  const manager = source.access_level === "manager";
  useEffect(() => {
    if (isEnrollmentAccessChanged(query.error)) onContextChanged();
  }, [query.error, onContextChanged]);
  return (
    <>
      <StaffDirectory<PharmacyStaff>
        title={source.name}
        description={
          manager
            ? "Pharmacy Staff directory · you manage these records"
            : "Pharmacy Staff directory"
        }
        emptyDescription="No Staff records are in this pharmacy's directory yet."
        scopeDescription="Showing this pharmacy's own Staff records; no outlet assignment is implied"
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
}: SourceProps & { source: EmployerSource; staff?: PharmacyStaff; onClose: () => void }) {
  const [id] = useState(() => staff?.id ?? crypto.randomUUID());
  const [reload, setReload] = useState(0);
  const record = usePharmacyStaffRecord(scope, source.pharmacy_id, id, reload, !!staff);
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
    <StaffRecordForm
      key={reload}
      establishmentName={source.name}
      staff={staff ? record.data : undefined}
      invalidate={[pharmacyStaffKey(scope, source.pharmacy_id)]}
      onClose={onClose}
      onContextChanged={onContextChanged}
      onReload={() => setReload((value) => value + 1)}
      save={(value, expectedVersion) =>
        savePharmacyStaffRecord(scope, source.pharmacy_id, id, value, expectedVersion)
      }
    />
  );
}

function StaffSourceHospitals({ scope, onContextChanged }: SourceProps) {
  const [cursor, setCursor] = useState<string | null>(null);
  const query = useStaffHospitalsPage(scope, cursor);
  useEffect(() => {
    if (isEnrollmentAccessChanged(query.error)) onContextChanged();
  }, [query.error, onContextChanged]);
  return (
    <section>
      <Panel className="mb-5 space-y-2">
        <h2 className="font-semibold">Shared Staff source</h2>
        <p className="text-sm text-muted-foreground">
          These are hospital Staff records you are permitted to read. Being listed here does not
          assign someone to a pharmacy outlet or authorize dispensing.
        </p>
        <GuardedLink
          href="/workforce"
          className="inline-flex text-sm font-medium text-(--hms-accent) hover:underline"
        >
          Manage records in Staff
        </GuardedLink>
      </Panel>
      {query.isPending ? (
        <p role="status">Loading Staff source hospitals…</p>
      ) : query.isError ? (
        <RequestFailure
          message="Could not load Staff source hospitals."
          retry={query.refetch}
          error={query.error}
        />
      ) : query.data && query.data.data.length > 0 ? (
        <HospitalSourcePage
          key={cursor ?? "first"}
          {...{ scope, onContextChanged }}
          hospitals={query.data.data}
        />
      ) : (
        <Panel>
          <h3 className="font-semibold">No shared Staff access on this page</h3>
          <p className="mt-2 text-sm text-muted-foreground">
            Pharmacy access does not include hospital Staff records. Ask an authorized Staff
            administrator about the access required for your work.
          </p>
        </Panel>
      )}
      {!query.isPending && !query.isError && (
        <nav aria-label="Staff source hospitals" className="mt-4 flex gap-2">
          {cursor && (
            <Button variant="outline" onClick={() => setCursor(null)}>
              First hospital page
            </Button>
          )}
          {query.data?.page.has_more && query.data.page.next_cursor && (
            <Button variant="outline" onClick={() => setCursor(query.data!.page.next_cursor)}>
              Next hospital page
            </Button>
          )}
        </nav>
      )}
    </section>
  );
}

function HospitalSourcePage({
  scope,
  onContextChanged,
  hospitals,
}: SourceProps & { hospitals: Hospital[] }) {
  const id = useId();
  const [selected, setSelected] = useState("");
  const hospital = selected ? hospitals.find((item) => item.id === selected) : hospitals[0];
  return (
    <>
      <div className="mb-5 max-w-xl">
        <Field id={id} label="Staff source hospital">
          <SelectControl
            id={id}
            value={hospital?.id ?? ""}
            onValueChange={setSelected}
            options={hospitals.map((item) => ({ value: item.id, label: item.name }))}
          />
        </Field>
      </div>
      {hospital ? (
        <HospitalSource
          key={`${scope.tenantId}:${scope.subject}:${hospital.id}`}
          {...{ scope, onContextChanged, hospital }}
        />
      ) : (
        <p>Choose a currently accessible Staff source hospital.</p>
      )}
    </>
  );
}

function HospitalSource({
  scope,
  onContextChanged,
  hospital,
}: SourceProps & { hospital: Hospital }) {
  const [cursor, setCursor] = useState<string | null>(null);
  const query = useStaffDirectoryPage(scope, hospital.id, cursor);
  useEffect(() => {
    if (isEnrollmentAccessChanged(query.error)) onContextChanged();
  }, [query.error, onContextChanged]);
  return (
    <StaffDirectory
      title={hospital.name}
      description="Read-only canonical Staff source"
      emptyDescription="No Staff records are available in this hospital directory."
      scopeDescription="Showing this hospital's Staff records; no outlet assignment is implied"
      rows={query.data?.data ?? []}
      isPending={query.isPending}
      isError={query.isError}
      error={query.error}
      retry={() => query.refetch()}
      onFirstPage={cursor ? () => setCursor(null) : undefined}
      onNextPage={
        query.data?.page.has_more && query.data.page.next_cursor
          ? () => setCursor(query.data!.page.next_cursor)
          : undefined
      }
    />
  );
}
