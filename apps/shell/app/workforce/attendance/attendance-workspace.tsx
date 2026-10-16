"use client";
import { useState } from "react";
import { Button } from "@tesserix/web";
import {
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type Hospital,
  type RosterSettings,
} from "@helivanta/api";
import { HmsShell, Field, SelectControl } from "@helivanta/ui";
import { TenantPicker } from "@/components/tenant-picker";
import { Failure } from "../roster-policy/policy-ui";
import { AttendancePolicyPanel } from "./policy-panel";
import { DaySheet } from "./day-sheet";
import { PunchLedger } from "./punch-panel";

export function AttendanceWorkspace() {
  const hospitals = useApiPagedQuery<Hospital>(
    ["workforce", "planning-hospitals"],
    "/workforce/hospitals",
  );
  const [selected, setSelected] = useState("");
  const hospital = hospitals.items.find((h) => h.id === selected) ?? hospitals.items[0];
  return (
    <HmsShell active="/workforce/attendance" tenantPicker={<TenantPicker />}>
      <div className="mb-8 flex flex-wrap items-end justify-between gap-6">
        <div>
          <p className="mb-2 text-xs font-semibold uppercase tracking-[0.18em] text-(--hms-accent)">
            People & operations
          </p>
          <h2 className="text-3xl font-semibold tracking-tight">Attendance</h2>
          <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
            See who worked their rostered shifts, record punches by hand with a reason, and void
            wrong ones. Attendance is derived from the roster, leave, overtime and punches; nothing
            is typed in directly.
          </p>
        </div>
        {hospital && (
          <div className="w-full sm:w-72">
            <Field id="attendance-hospital" label="Hospital">
              <SelectControl
                id="attendance-hospital"
                value={hospital.id}
                onValueChange={setSelected}
                options={hospitals.items.map((h) => ({ value: h.id, label: h.name }))}
              />
            </Field>
          </div>
        )}
      </div>
      {hospitals.isPending ? (
        <p role="status">Loading hospitals…</p>
      ) : hospitals.isError ? (
        <Failure message="Could not load hospitals." retry={hospitals.retry} />
      ) : hospital ? (
        <HospitalAttendance key={hospital.id} hospital={hospital} />
      ) : (
        <p className="rounded-2xl border bg-card p-8">
          Ask your tenant administrator for hospital access before managing attendance.
        </p>
      )}
      {hospitals.hasMore && (
        <Button
          className="mt-4"
          variant="outline"
          disabled={hospitals.isFetchingMore}
          onClick={hospitals.loadMore}
        >
          Load more hospitals
        </Button>
      )}
    </HmsShell>
  );
}

function HospitalAttendance({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  const manager = hospital.access_level === "manager";
  const recorder = manager && can("workforce.attendance.record");
  const base = `/workforce/hospitals/${hospital.id}`;
  // The server re-checks the timezone in force on the punch date, so the current one only guides entry.
  const roster = useApiQuery<RosterSettings>(
    ["workforce", "roster-settings", hospital.id],
    `${base}/hr/roster-settings`,
    { enabled: recorder },
  );
  return (
    <div className="space-y-6">
      <AttendancePolicyPanel
        hospital={hospital}
        writable={manager && can("workforce.attendance.configure")}
      />
      <DaySheet
        hospital={hospital.id}
        base={base}
        recorder={recorder}
        timezone={roster.data?.timezone}
      />
      <PunchLedger hospital={hospital.id} base={base} recorder={recorder} />
    </div>
  );
}
