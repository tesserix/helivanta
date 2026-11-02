"use client";

import { useEffect, useId, useState } from "react";
import { z } from "zod";
import { Button } from "@tesserix/web";
import {
  isEnrollmentAccessChanged,
  useApiQuery,
  useStaffDirectoryPage,
  useStaffHospitalsPage,
  type EnrollmentRequestScope,
  type Hospital,
} from "@helivanta/api";
import {
  Field,
  GuardedLink,
  PageHeading,
  Panel,
  RequestFailure,
  SelectControl,
  StaffDirectory,
} from "@helivanta/ui";

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
      />
    );
  if (!parsed.data.data.includes("pharmacy.catalogue.read"))
    return (
      <Panel role="alert">You do not have permission to open the pharmacy Staff source.</Panel>
    );
  const scope = { tenantId: parsed.data.tenant_id, subject: parsed.data.subject };
  return (
    <StaffSourceHospitals
      key={`${scope.tenantId}:${scope.subject}`}
      scope={scope}
      onContextChanged={() => setChanged(true)}
    />
  );
}

type SourceProps = { scope: EnrollmentRequestScope; onContextChanged: () => void };
function StaffSourceHospitals({ scope, onContextChanged }: SourceProps) {
  const [cursor, setCursor] = useState<string | null>(null);
  const query = useStaffHospitalsPage(scope, cursor);
  useEffect(() => {
    if (isEnrollmentAccessChanged(query.error)) onContextChanged();
  }, [query.error, onContextChanged]);
  return (
    <>
      <PageHeading description="Read canonical Staff records available to you for pharmacy onboarding." />
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
        <RequestFailure message="Could not load Staff source hospitals." retry={query.refetch} />
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
    </>
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
      onRetry={() => query.refetch()}
      onFirstPage={cursor ? () => setCursor(null) : undefined}
      onNextPage={
        query.data?.page.has_more && query.data.page.next_cursor
          ? () => setCursor(query.data!.page.next_cursor)
          : undefined
      }
    />
  );
}
