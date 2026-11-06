"use client";

import { useEffect, useState } from "react";
import { Button } from "@tesserix/web";
import { RequestFailure, RefreshButton, Panel } from "@helivanta/ui";
import {
  useStaffPostingReviewScopes,
  useStaffDepartmentTeam,
  useStaffPostingReviewDepartmentNames,
  type StaffPostingInboxScope,
  type StaffPostingReviewHospitalScope,
  type EnrollmentRequestScope,
} from "@helivanta/api";
import { STAFF_ACCESS_CHANGED, useStaffAccessCutoff } from "./staff-access-cutoff";

export function DepartmentTeamRoster({
  scope,
  onContextChanged,
}: {
  scope: EnrollmentRequestScope;
  onContextChanged: () => void;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const hospitals = useStaffPostingReviewScopes(scope, cursor);
  const cutoff = useStaffAccessCutoff([hospitals.error], onContextChanged);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (hospitals.isPending || hospitals.isFetching)
    return <p role="status">Checking team hospitals…</p>;
  if (hospitals.isError)
    return (
      <RequestFailure
        error={hospitals.error}
        message="Team hospital access could not be verified."
        retry={hospitals.refetch}
      />
    );
  const hospital = hospitals.data.data.find((row) => row.hospital_id === selected);
  return (
    <section className="space-y-5" aria-label="Department team roster">
      <p>
        Choose a hospital to inspect approved placements and their current usability. Leadership
        appointments do not grant clinical or operating access.
      </p>
      <div className="flex flex-wrap gap-3">
        {hospitals.data.data.map((row) => (
          <Button
            key={row.hospital_id}
            variant={row.hospital_id === selected ? "default" : "outline"}
            aria-label={`Open team for ${row.name}`}
            onClick={() => setSelected(row.hospital_id)}
          >
            {row.name}
          </Button>
        ))}
      </div>
      {hospitals.data.data.length === 0 && <p>No team hospitals available on this page.</p>}
      <div className="flex gap-3">
        {hospitals.data.page.has_more && (
          <Button
            variant="outline"
            onClick={() => {
              setSelected(null);
              setCursor(hospitals.data.page.next_cursor);
            }}
          >
            Next team hospitals
          </Button>
        )}
        {cursor && (
          <Button
            variant="outline"
            onClick={() => {
              setSelected(null);
              setCursor(null);
            }}
          >
            First team hospitals
          </Button>
        )}
        <RefreshButton
          error={hospitals.error}
          refresh={() => {
            setSelected(null);
            return hospitals.refetch();
          }}
        >
          Refresh team access
        </RefreshButton>
      </div>
      {selected && !hospital && (
        <p role="alert">This team hospital is no longer available. Refresh access.</p>
      )}
      {hospital && (
        <HospitalTeam
          key={hospital.hospital_id}
          scope={{ ...scope, access: "scoped", hospitalId: hospital.hospital_id }}
          hospital={hospital}
          onContextChanged={onContextChanged}
        />
      )}
    </section>
  );
}

function HospitalTeam({
  scope,
  hospital,
  onContextChanged,
}: {
  scope: StaffPostingInboxScope;
  hospital: StaffPostingReviewHospitalScope;
  onContextChanged: () => void;
}) {
  const [chosen, setChosen] = useState(hospital.departments[0]);
  const department = hospital.departments.includes(chosen) ? chosen : hospital.departments[0];
  const names = useStaffPostingReviewDepartmentNames(scope, hospital.departments);
  return (
    <section className="space-y-4" aria-label={`${hospital.name} team`}>
      {hospital.departments.length > 0 && (
        <div role="group" aria-label="Team departments" className="flex flex-wrap gap-3">
          {hospital.departments.map((id) => (
            <Button
              key={id}
              variant={department === id ? "default" : "outline"}
              onClick={() => setChosen(id)}
            >
              {names.get(id) ?? "Department name unavailable"}
            </Button>
          ))}
        </div>
      )}
      <TeamPlacements
        key={department ?? "hospital"}
        scope={department ? { ...scope, department } : scope}
        names={names}
        onContextChanged={onContextChanged}
      />
    </section>
  );
}
const responsibilities = {
  hospital_management: "Hospital management",
  department_supervisor: "Department supervisor",
  department_member: "Department member",
};
function TeamPlacements({
  scope,
  names,
  onContextChanged,
}: {
  scope: StaffPostingInboxScope;
  names: Map<string, string>;
  onContextChanged: () => void;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const team = useStaffDepartmentTeam(scope, cursor);
  const cutoff = useStaffAccessCutoff([team.error], onContextChanged);
  const refetch = team.refetch;
  const earliest =
    team.data?.data.reduce(
      (until, row) =>
        row.source_until && Date.parse(row.source_until) > now
          ? Math.min(until, Date.parse(row.source_until))
          : until,
      Infinity,
    ) ?? Infinity;
  useEffect(() => {
    if (!Number.isFinite(earliest)) return;
    const timer = setTimeout(
      () => {
        setNow(Date.now());
        void refetch();
      },
      Math.max(0, Math.min(earliest - Date.now(), 60_000)),
    );
    return () => clearTimeout(timer);
  }, [earliest, now, refetch]);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (team.isPending || team.isFetching) return <p role="status">Checking current team sources…</p>;
  if (team.isError)
    return (
      <RequestFailure
        error={team.error}
        message="The department team could not be verified."
        retry={team.refetch}
      />
    );
  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        Each page contains at most eight approved placements. Current status is an observation at
        the displayed check time; every operating action rechecks its own authority.
      </p>
      {team.data.data.length === 0 && <p>No approved placements on this page.</p>}
      {team.data.data.map((row) => (
        <Panel key={row.posting_id} className="space-y-2">
          <h3 className="font-semibold">{row.staff.display_name}</h3>
          <p>
            {row.staff.employee_code} · {responsibilities[row.responsibility]} ·{" "}
            {names.get(row.department_id) ?? "Department name unavailable"}
          </p>
          <p>
            {row.state === "current" && row.source_until && Date.parse(row.source_until) > now
              ? "Current at last check"
              : row.state === "current"
                ? "Observation expired; refresh current team"
                : "Not currently usable"}
          </p>
          <p className="text-sm text-muted-foreground">
            Checked {new Date(row.checked_at).toLocaleString(undefined, { timeZoneName: "short" })}
            {row.source_until &&
              ` · Source valid until ${new Date(row.source_until).toLocaleString(undefined, { timeZoneName: "short" })}`}
          </p>
        </Panel>
      ))}
      <div className="flex gap-3">
        <RefreshButton error={team.error} refresh={team.refetch}>
          Refresh current team
        </RefreshButton>
        {team.data.page.has_more && (
          <Button variant="outline" onClick={() => setCursor(team.data.page.next_cursor)}>
            Next team placements
          </Button>
        )}
        {cursor && (
          <Button variant="outline" onClick={() => setCursor(null)}>
            First team placements
          </Button>
        )}
      </div>
    </div>
  );
}
