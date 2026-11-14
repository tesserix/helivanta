"use client";
import { useState } from "react";
import { Button } from "@tesserix/web";
import { Panel, RequestFailure, RefreshButton, useConfirmLeave } from "@helivanta/ui";
import {
  useOperatingAuthorityScopes,
  useOperatingStaffChoices,
  type OperatingAuthorityScope,
  type OperatingGovernanceEmployerScope,
  type OperatingGovernanceStaffChoice,
} from "@helivanta/api";
import { IndependentOperatingDuties } from "./independent-operating-duties";
type Props = {
  scope: OperatingAuthorityScope;
  accessSuspended: boolean;
  onBusyChange?: (busy: boolean) => void;
};
export function GovernanceOperatingAccess(props: Props) {
  const [employer, setEmployer] = useState<OperatingGovernanceEmployerScope | null>(null);
  if (employer)
    return (
      <EmployerAccess
        key={`${employer.employer_kind}:${employer.employer_id}`}
        {...props}
        employer={employer}
        onClose={() => setEmployer(null)}
      />
    );
  return <EmployerChoices {...props} select={setEmployer} />;
}
function EmployerChoices({
  scope,
  accessSuspended,
  select,
}: Props & { select: (value: OperatingGovernanceEmployerScope) => void }) {
  const [cursor, setCursor] = useState<string | null>(null);
  const query = useOperatingAuthorityScopes(scope, cursor, !accessSuspended);
  if (accessSuspended)
    return (
      <p role="alert">
        Current independent Staff access could not be verified. Restore your account access before
        continuing.
      </p>
    );
  return (
    <Panel className="space-y-4">
      <h2 className="text-lg font-semibold">Choose an employer</h2>
      <p className="text-sm text-muted-foreground">
        Open an independent pharmacy or supplier you currently administer. Hospital department
        assignments are managed separately.
      </p>
      {query.isPending && <p role="status">Loading authorised employers…</p>}
      {query.isError ? (
        <RequestFailure
          message="Employer access could not be verified."
          error={query.error}
          retry={query.refetch}
        />
      ) : (
        <>
          {query.data?.data.map((row) => (
            <Panel
              key={`${row.employer_kind}:${row.employer_id}`}
              className="flex items-center justify-between gap-3"
            >
              <div>
                <p className="font-medium">{row.name}</p>
                <p className="text-sm text-muted-foreground">
                  {row.employer_kind === "pharmacy"
                    ? "Independent pharmacy"
                    : "Supplier organisation"}
                </p>
              </div>
              <Button disabled={!row.can_manage} onClick={() => select(row)}>
                Manage {row.name}
              </Button>
            </Panel>
          ))}
          {query.data?.data.length === 0 && <p>No authorised employers on this page.</p>}
          <nav aria-label="Employer pages" className="flex gap-3">
            {cursor && (
              <Button variant="outline" onClick={() => setCursor(null)}>
                First employers page
              </Button>
            )}
            {query.data?.page.has_more && (
              <Button variant="outline" onClick={() => setCursor(query.data.page.next_cursor)}>
                More employers
              </Button>
            )}
          </nav>
        </>
      )}
      <RefreshButton busy={query.isFetching} refresh={query.refetch} error={query.error}>
        Refresh employers
      </RefreshButton>
    </Panel>
  );
}
function EmployerAccess({
  scope,
  employer,
  accessSuspended,
  onBusyChange,
  onClose,
}: Props & { employer: OperatingGovernanceEmployerScope; onClose: () => void }) {
  const [cursor, setCursor] = useState<string | null>(null),
    [selected, setSelected] = useState<OperatingGovernanceStaffChoice | null>(null),
    [busy, updateBusy] = useState(false);
  const confirmLeave = useConfirmLeave();
  const selectedScope = {
    ...scope,
    employerKind: employer.employer_kind,
    employerId: employer.employer_id,
  };
  const query = useOperatingStaffChoices(selectedScope, cursor, !busy && !accessSuspended);
  const setBusy = (value: boolean) => {
    updateBusy(value);
    onBusyChange?.(value);
  };
  const current =
    selected &&
    query.data?.data.find(
      (row) =>
        row.staff_id === selected.staff_id &&
        row.staff_version === selected.staff_version &&
        row.subject === selected.subject &&
        row.link_id === selected.link_id &&
        row.link_version === selected.link_version,
    );
  const suspended = accessSuspended || query.isError || !query.data || (!!selected && !current);
  function change(action: () => void) {
    if (!busy) void confirmLeave(action);
  }
  return (
    <Panel className="space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-semibold">
          {suspended ? "Independent Staff access" : employer.name}
        </h2>
        <Button variant="outline" disabled={busy} onClick={() => change(onClose)}>
          Change employer
        </Button>
      </header>
      {!suspended && (
        <p className="text-sm text-muted-foreground">
          Select a current Staff member with an independently reviewed account. A separate reviewer
          must approve each operating duty.
        </p>
      )}
      {accessSuspended ? (
        <p role="alert">
          Your account or access changed. Recover any unconfirmed command in its original account
          before continuing.
        </p>
      ) : query.isError ? (
        <RequestFailure
          message="Current reviewed Staff could not be verified."
          error={query.error}
          retry={() => {
            if (!busy) void query.refetch();
          }}
        />
      ) : query.isPending ? (
        <p role="status">Loading reviewed Staff…</p>
      ) : null}
      {selected ? (
        <>
          {!suspended && <h3 className="font-semibold">{selected.display_name}</h3>}
          {suspended && !busy && (
            <p role="alert">
              This reviewed Staff choice is no longer current. Refresh and select the account again.
            </p>
          )}
          <Button variant="outline" disabled={busy} onClick={() => change(() => setSelected(null))}>
            Change Staff member
          </Button>
          <IndependentOperatingDuties
            scope={{ ...selectedScope, staffId: selected.staff_id, administration: "governance" }}
            staffVersion={selected.staff_version}
            staffActive
            reviewedAccount={selected}
            accessSuspended={suspended}
            onBusyChange={setBusy}
          />
        </>
      ) : (
        !suspended && (
          <>
            {query.data?.data.map((row) => (
              <Panel key={row.staff_id} className="flex items-center justify-between gap-3">
                <p className="font-medium">{row.display_name}</p>
                <Button onClick={() => setSelected(row)}>Open {row.display_name}</Button>
              </Panel>
            ))}
            {query.data?.data.length === 0 && (
              <p>
                No currently eligible reviewed Staff on this page. Complete Staff and account review
                before preparing a duty.
              </p>
            )}
            <nav aria-label="Reviewed Staff pages" className="flex gap-3">
              {cursor && (
                <Button variant="outline" onClick={() => setCursor(null)}>
                  First Staff page
                </Button>
              )}
              {query.data?.page.has_more && (
                <Button variant="outline" onClick={() => setCursor(query.data.page.next_cursor)}>
                  More reviewed Staff
                </Button>
              )}
            </nav>
          </>
        )
      )}
      {!accessSuspended && (
        <RefreshButton busy={busy || query.isFetching} refresh={query.refetch} error={query.error}>
          Refresh reviewed Staff
        </RefreshButton>
      )}
    </Panel>
  );
}
