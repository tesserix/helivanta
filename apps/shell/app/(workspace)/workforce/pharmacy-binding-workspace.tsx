"use client";

import { useEffect, useState, type ReactNode } from "react";
import { Button } from "@tesserix/web";
import { Panel, RefreshButton, RequestFailure, firstError } from "@helivanta/ui";
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
import { PharmacyBindingCommand, type PharmacyBindingIntent } from "./pharmacy-binding-command";
import { STAFF_ACCESS_CHANGED, useStaffAccessCutoff } from "./staff-access-cutoff";

type Props = {
  scope: StaffActorAccountScope;
  onContextChanged: () => void;
  onBusyChanged?: (busy: boolean) => void;
};
type Flow = {
  busy: boolean;
  onBusy: (busy: boolean) => void;
  purpose: "read" | "prepare" | "review";
};
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
      <RequestFailure
        message={`${label} could not be verified.`}
        retry={query.refetch}
        error={query.error}
      />
    );
  return children;
}

function PageControls({
  cursor,
  next,
  onPage,
  label,
  busy = false,
}: {
  busy?: boolean;
  cursor: string | null;
  next: string | null;
  onPage: (cursor: string | null) => void;
  label: string;
}) {
  return (
    <div className="flex flex-wrap gap-3">
      {cursor !== null && (
        <Button variant="outline" disabled={busy} onClick={() => onPage(null)}>
          First {label} page
        </Button>
      )}
      {next !== null && (
        <Button variant="outline" disabled={busy} onClick={() => onPage(next)}>
          Next {label} page
        </Button>
      )}
    </div>
  );
}

export function PharmacyBindingWorkspace(props: Props) {
  return <BindingAccount key={JSON.stringify(props.scope)} {...props} />;
}

function BindingAccount({ onBusyChanged, ...props }: Props) {
  const [busy, setBusy] = useState(false);
  const [purpose, setPurpose] = useState<Flow["purpose"]>("read");
  useEffect(() => {
    onBusyChanged?.(busy);
  }, [busy, onBusyChanged]);
  useEffect(() => () => onBusyChanged?.(false), [onBusyChanged]);
  return (
    <section className="space-y-5">
      <div className="flex flex-wrap gap-3" aria-label="Relationship purpose">
        {(["read", "prepare", "review"] as const).map((value) => (
          <Button
            key={value}
            disabled={busy}
            variant={purpose === value ? "default" : "outline"}
            onClick={() => setPurpose(value)}
          >
            {value === "read"
              ? "Browse relationships"
              : value === "prepare"
                ? "Prepare relationships"
                : "Review relationships"}
          </Button>
        ))}
      </div>
      <HospitalChoices key={purpose} {...props} busy={busy} onBusy={setBusy} purpose={purpose} />
    </section>
  );
}

function HospitalChoices({ scope, onContextChanged, busy, onBusy, purpose }: Props & Flow) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [hospital, setHospital] = useState<{ hospital_id: string; name: string } | null>(null);
  const query = usePharmacyBindingHospitals(scope, cursor, busy);
  return (
    <VerifiedRead query={query} label="Staff hospitals" onContextChanged={onContextChanged}>
      {hospital ? (
        <section className="space-y-4">
          <Button variant="outline" disabled={busy} onClick={() => setHospital(null)}>
            Change hospital
          </Button>
          <h2 className="text-lg font-semibold">{hospital.name}</h2>
          <OutletChoices
            key={hospital.hospital_id}
            busy={busy}
            onBusy={onBusy}
            purpose={purpose}
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
            busy={busy}
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
  busy,
  onBusy,
  purpose,
}: Flow & {
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
  const query = usePharmacyBindingOutlets(scope, cursor, purpose, busy);
  return (
    <VerifiedRead
      query={query}
      label="authorized pharmacy outlets"
      onContextChanged={onContextChanged}
    >
      {outlet ? (
        <section className="space-y-4">
          <Button variant="outline" disabled={busy} onClick={() => setOutlet(null)}>
            Change outlet
          </Button>
          <h3 className="text-lg font-semibold">
            {outlet.pharmacy_name} · {outlet.outlet_name}
          </h3>
          <Relationships
            key={outlet.outlet_id}
            busy={busy}
            onBusy={onBusy}
            purpose={purpose}
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
            busy={busy}
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
  busy,
  onBusy,
  purpose,
}: Flow & {
  scope: PharmacyStaffBindingScope;
  onContextChanged: () => void;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [departmentCursor, setDepartmentCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const query = usePharmacyBindings(scope, cursor, busy);
  const departments = usePharmacyBindingDepartments(scope, departmentCursor, { busy });
  const cutoff = useStaffAccessCutoff([departments.error], onContextChanged);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  return (
    <VerifiedRead query={query} label="recorded relationships" onContextChanged={onContextChanged}>
      {creating ? (
        <Panel className="space-y-4">
          <PharmacyBindingCommand
            scope={scope}
            intent={{ kind: "proposal", replacement: null }}
            onBusy={onBusy}
            onContextChanged={onContextChanged}
            onCompleted={() => {
              setCreating(false);
              void query.refetch();
            }}
          />
        </Panel>
      ) : selected ? (
        <section className="space-y-4">
          <Button variant="outline" disabled={busy} onClick={() => setSelected(null)}>
            Back to recorded relationships
          </Button>
          <RelationshipDetail
            key={selected}
            busy={busy}
            onBusy={onBusy}
            purpose={purpose}
            scope={scope}
            binding={selected}
            onContextChanged={onContextChanged}
          />
        </section>
      ) : (
        <Panel className="space-y-4">
          <h3 className="text-lg font-semibold">Recorded department relationships</h3>
          {purpose === "prepare" && (
            <Button disabled={busy} onClick={() => setCreating(true)}>
              Prepare department relationship
            </Button>
          )}
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
              busy={busy}
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
            busy={busy}
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
  busy,
  onBusy,
  purpose,
}: Flow & {
  scope: PharmacyStaffBindingScope;
  binding: string;
  onContextChanged: () => void;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [intent, setIntent] = useState<PharmacyBindingIntent | null>(null);
  const detail = usePharmacyBinding(scope, binding, busy);
  const history = usePharmacyBindingHistory(scope, binding, cursor, busy);
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
          {intent ? (
            <PharmacyBindingCommand
              scope={scope}
              intent={intent}
              onBusy={onBusy}
              onContextChanged={onContextChanged}
              onCompleted={() => {
                setIntent(null);
                void detail.refetch();
                void history.refetch();
              }}
            />
          ) : (
            <div className="flex flex-wrap gap-3">
              {purpose === "prepare" && detail.data.binding.status === "approved" && (
                <Button
                  disabled={busy || changed}
                  onClick={() => setIntent({ kind: "proposal", replacement: detail.data.binding })}
                >
                  Propose replacement relationship
                </Button>
              )}
              {purpose === "prepare" &&
                detail.data.binding.status === "pending" &&
                detail.data.binding.proposed_by_subject === scope.subject && (
                  <Button
                    disabled={busy || changed}
                    onClick={() =>
                      setIntent({
                        kind: "decision",
                        binding: detail.data.binding,
                        operation: "withdraw",
                      })
                    }
                  >
                    Withdraw proposal
                  </Button>
                )}
              {purpose === "review" &&
                detail.data.binding.proposed_by_subject !== scope.subject &&
                (detail.data.binding.status === "pending"
                  ? (["approve", "reject"] as const)
                  : detail.data.binding.status === "approved"
                    ? (["end"] as const)
                    : []
                ).map((operation) => (
                  <Button
                    key={operation}
                    disabled={busy || changed || history.isFetching || history.isError}
                    onClick={() =>
                      setIntent({ kind: "decision", binding: detail.data.binding, operation })
                    }
                  >
                    {operation === "approve"
                      ? "Approve relationship"
                      : operation === "reject"
                        ? "Reject relationship"
                        : "End relationship"}
                  </Button>
                ))}
            </div>
          )}

          {intent === null && (
            <>
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
              <RefreshButton
                busy={busy}
                error={firstError(detail.error, history.error)}
                refresh={() => Promise.all([detail.refetch(), history.refetch()])}
              >
                Recheck relationship
              </RefreshButton>
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
                  busy={busy}
                  cursor={cursor}
                  next={history.data?.page.next_cursor ?? null}
                  onPage={setCursor}
                  label="history"
                />
              </VerifiedRead>
            </>
          )}
        </Panel>
      )}
    </VerifiedRead>
  );
}
