"use client";

import { useState, type ReactNode } from "react";
import { Button } from "@tesserix/web";
import { Panel } from "@helivanta/ui";
import {
  usePharmacyBindingHospitals,
  usePharmacyBindingOutlets,
  usePharmacyBindingDepartments,
  usePharmacyBindings,
  usePharmacyBinding,
  usePharmacyBindingHistory,
  type StaffActorAccountScope,
  type PharmacyStaffBindingHospitalScope,
  type PharmacyStaffBindingScope,
} from "@helivanta/api";
import { STAFF_ACCESS_CHANGED, useStaffAccessCutoff } from "./staff-access-cutoff";

type Props = { scope: StaffActorAccountScope; onContextChanged: () => void };
type QueryState = {
  error: unknown;
  isPending: boolean;
  isFetching: boolean;
  isError: boolean;
  refetch: () => unknown;
};

function VerifiedRead({
  query,
  label,
  onContextChanged,
  children,
}: {
  query: QueryState;
  label: string;
  onContextChanged: () => void;
  children: ReactNode;
}) {
  const cutoff = useStaffAccessCutoff([query.error], onContextChanged);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (query.isPending || query.isFetching) return <p role="status">Checking {label}…</p>;
  if (query.isError)
    return (
      <div role="alert" className="space-y-3">
        <p>{label} could not be verified.</p>
        <Button onClick={() => void query.refetch()}>Retry {label}</Button>
      </div>
    );
  return children;
}

function PageControls({
  cursor,
  next,
  onPage,
  label,
}: {
  cursor: string | null;
  next: string | null;
  onPage: (cursor: string | null) => void;
  label: string;
}) {
  return (
    <div className="flex flex-wrap gap-3">
      {cursor !== null && (
        <Button variant="outline" onClick={() => onPage(null)}>
          First {label} page
        </Button>
      )}
      {next !== null && (
        <Button variant="outline" onClick={() => onPage(next)}>
          Next {label} page
        </Button>
      )}
    </div>
  );
}

export function PharmacyBindingWorkspace(props: Props) {
  return <HospitalChoices key={JSON.stringify(props.scope)} {...props} />;
}

function HospitalChoices({ scope, onContextChanged }: Props) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [hospital, setHospital] = useState<{ hospital_id: string; name: string } | null>(null);
  const query = usePharmacyBindingHospitals(scope, cursor);
  return (
    <VerifiedRead query={query} label="Staff hospitals" onContextChanged={onContextChanged}>
      {hospital ? (
        <section className="space-y-4">
          <Button variant="outline" onClick={() => setHospital(null)}>
            Change hospital
          </Button>
          <h2 className="text-lg font-semibold">{hospital.name}</h2>
          <OutletChoices
            key={hospital.hospital_id}
            scope={{ ...scope, hospitalId: hospital.hospital_id }}
            onContextChanged={onContextChanged}
          />
        </section>
      ) : (
        <Panel className="space-y-4">
          <h2 className="text-lg font-semibold">Choose your Staff hospital</h2>
          <p>
            Only hospitals with a current reviewed Staff login link are listed. Outlet access is
            checked separately.
          </p>
          {query.data?.data.length === 0 && <p>No approved Staff hospitals on this page.</p>}
          <ul className="space-y-3">
            {query.data?.data.map((row) => (
              <li key={row.hospital_id}>
                <Button
                  variant="outline"
                  aria-label={"Open pharmacy relationships for " + row.name}
                  onClick={() => setHospital(row)}
                >
                  {row.name}
                </Button>
              </li>
            ))}
          </ul>
          <PageControls
            cursor={cursor}
            next={query.data?.page.next_cursor ?? null}
            onPage={setCursor}
            label="hospital"
          />
        </Panel>
      )}
    </VerifiedRead>
  );
}

function OutletChoices({
  scope,
  onContextChanged,
}: {
  scope: PharmacyStaffBindingHospitalScope;
  onContextChanged: () => void;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [outlet, setOutlet] = useState<{
    pharmacy_id: string;
    outlet_id: string;
    pharmacy_name: string;
    outlet_name: string;
  } | null>(null);
  const query = usePharmacyBindingOutlets(scope, cursor);
  return (
    <VerifiedRead
      query={query}
      label="authorized pharmacy outlets"
      onContextChanged={onContextChanged}
    >
      {outlet ? (
        <section className="space-y-4">
          <Button variant="outline" onClick={() => setOutlet(null)}>
            Change outlet
          </Button>
          <h3 className="text-lg font-semibold">
            {outlet.pharmacy_name} · {outlet.outlet_name}
          </h3>
          <Relationships
            key={outlet.outlet_id}
            scope={{ ...scope, pharmacyId: outlet.pharmacy_id, outletId: outlet.outlet_id }}
            onContextChanged={onContextChanged}
          />
        </section>
      ) : (
        <Panel className="space-y-4">
          <h3 className="text-lg font-semibold">Authorized hospital pharmacy outlets</h3>
          {query.data?.data.length === 0 && <p>No authorized outlets on this page.</p>}
          <ul className="space-y-3">
            {query.data?.data.map((row) => (
              <li key={row.outlet_id}>
                <Button
                  variant="outline"
                  aria-label={
                    "Open relationships for " + row.pharmacy_name + " · " + row.outlet_name
                  }
                  onClick={() => setOutlet(row)}
                >
                  {row.pharmacy_name} · {row.outlet_name}
                </Button>
              </li>
            ))}
          </ul>
          <PageControls
            cursor={cursor}
            next={query.data?.page.next_cursor ?? null}
            onPage={setCursor}
            label="outlet"
          />
        </Panel>
      )}
    </VerifiedRead>
  );
}

function Relationships({
  scope,
  onContextChanged,
}: {
  scope: PharmacyStaffBindingScope;
  onContextChanged: () => void;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [departmentCursor, setDepartmentCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const query = usePharmacyBindings(scope, cursor);
  const departments = usePharmacyBindingDepartments(scope, departmentCursor);
  const cutoff = useStaffAccessCutoff([departments.error], onContextChanged);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  return (
    <VerifiedRead query={query} label="recorded relationships" onContextChanged={onContextChanged}>
      {selected ? (
        <section className="space-y-4">
          <Button variant="outline" onClick={() => setSelected(null)}>
            Back to recorded relationships
          </Button>
          <RelationshipDetail
            key={selected}
            scope={scope}
            binding={selected}
            onContextChanged={onContextChanged}
          />
        </section>
      ) : (
        <Panel className="space-y-4">
          <h3 className="text-lg font-semibold">Recorded department relationships</h3>
          <p>
            Recorded approval does not establish current operating authority. Open a relationship to
            check its department source.
          </p>
          {query.data?.data.length === 0 && <p>No recorded relationships on this page.</p>}
          <VerifiedRead
            query={departments}
            label="department names"
            onContextChanged={onContextChanged}
          >
            <p>
              Department names come from the current Organization page. Historical or other-page
              references retain their identifiers.
            </p>
            <PageControls
              cursor={departmentCursor}
              next={departments.data?.page.next_cursor ?? null}
              onPage={setDepartmentCursor}
              label="department"
            />
          </VerifiedRead>
          <ul className="space-y-3">
            {query.data?.data.map((row) => {
              const department =
                !departments.isFetching && !departments.isError
                  ? departments.data?.data.find((item) => item.id === row.department_id)
                  : undefined;
              const name = department?.name ?? "Department " + row.department_id;
              return (
                <li
                  key={row.id}
                  className="flex flex-wrap items-center justify-between gap-3 border-b py-4"
                >
                  <div>
                    <p className="font-medium">{name}</p>
                    <p className="text-sm text-muted-foreground">
                      Recorded {row.status} · Version {row.version}
                    </p>
                  </div>
                  <Button
                    variant="outline"
                    aria-label={"Inspect " + name + " relationship"}
                    onClick={() => setSelected(row.id)}
                  >
                    Inspect relationship
                  </Button>
                </li>
              );
            })}
          </ul>
          <PageControls
            cursor={cursor}
            next={query.data?.page.next_cursor ?? null}
            onPage={setCursor}
            label="relationship"
          />
        </Panel>
      )}
    </VerifiedRead>
  );
}

function RelationshipDetail({
  scope,
  binding,
  onContextChanged,
}: {
  scope: PharmacyStaffBindingScope;
  binding: string;
  onContextChanged: () => void;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const detail = usePharmacyBinding(scope, binding);
  const history = usePharmacyBindingHistory(scope, binding, cursor);
  const [observedVersion, setObservedVersion] = useState(0);
  const cutoff = useStaffAccessCutoff([history.error], onContextChanged);
  const pageVersion = Math.max(0, ...(history.data?.data.map((row) => row.recorded_version) ?? []));
  if (pageVersion > observedVersion) setObservedVersion(pageVersion);
  const changed = Math.max(pageVersion, observedVersion) > (detail.data?.binding.version ?? 0);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  return (
    <VerifiedRead query={detail} label="relationship detail" onContextChanged={onContextChanged}>
      {detail.data && (
        <Panel className="space-y-4">
          <h3 className="text-lg font-semibold">Relationship detail</h3>
          <p role="status">
            {changed
              ? "Relationship changed. Recheck the current record."
              : history.isFetching || history.isError
                ? "Relationship history could not yet be verified."
                : detail.data.source_state === "current"
                  ? "Department source verified"
                  : detail.data.source_state === "changed"
                    ? "Department source changed"
                    : "Department source check does not apply to this recorded status"}
          </p>
          <p>
            This source check reflects the latest detail read. It does not grant operational
            permissions.
          </p>
          <dl className="grid gap-3 sm:grid-cols-2">
            <div>
              <dt className="text-sm text-muted-foreground">Recorded status</dt>
              <dd>
                {detail.data.binding.status} · Version {detail.data.binding.version}
              </dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Department reference</dt>
              <dd className="break-all">{detail.data.binding.department_id}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Retained department revision</dt>
              <dd className="break-all">{detail.data.binding.department_revision_id}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Proposal reason</dt>
              <dd>{detail.data.binding.reason}</dd>
            </div>
            {detail.data.binding.decision_reason && (
              <div>
                <dt className="text-sm text-muted-foreground">Decision reason</dt>
                <dd>{detail.data.binding.decision_reason}</dd>
              </div>
            )}
          </dl>
          <Button
            variant="outline"
            onClick={() => {
              void detail.refetch();
              void history.refetch();
            }}
          >
            Recheck relationship
          </Button>
          <VerifiedRead
            query={history}
            label="relationship history"
            onContextChanged={onContextChanged}
          >
            <h4 className="font-semibold">Protected history</h4>
            {history.data?.data.length === 0 && <p>No history entries on this page.</p>}
            <ul className="space-y-2">
              {history.data?.data.map((row) => (
                <li key={row.id}>
                  {row.action} · Version {row.recorded_version} ·{" "}
                  <time dateTime={row.recorded_at}>{row.recorded_at}</time>
                </li>
              ))}
            </ul>
            <PageControls
              cursor={cursor}
              next={history.data?.page.next_cursor ?? null}
              onPage={setCursor}
              label="history"
            />
          </VerifiedRead>
        </Panel>
      )}
    </VerifiedRead>
  );
}
