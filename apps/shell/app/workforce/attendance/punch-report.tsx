"use client";
import { useState } from "react";
import { Button } from "@tesserix/web";
import {
  attendancePunchReportPath,
  useApiPagedQuery,
  zonedClock,
  JOB_LABELS,
  type AttendancePunchReportRow,
} from "@helivanta/api";
import { Field, SelectControl } from "@helivanta/ui";
import { Failure, inputClass, today } from "../roster-policy/policy-ui";
import { useActiveStaff } from "./punch-panel";

const MAX_REPORT_DAYS = 31;
const shiftDate = (date: string, days: number) =>
  new Date(Date.parse(`${date}T00:00:00Z`) + days * 86400000).toISOString().slice(0, 10);
const validDate = (d: string) => /^(2[01]\d\d)-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])$/.test(d);

const jobLabel = (code: string) => JOB_LABELS[code as keyof typeof JOB_LABELS] ?? code;

// Hospital-wide punch listing (MIS 929): who recorded each punch, its source and void state, never its reason.
export function PunchReport({ hospital, base }: { hospital: string; base: string }) {
  const [person, setPerson] = useState("all");
  const [to, setTo] = useState(today);
  const [from, setFrom] = useState(() => shiftDate(today(), -6));
  const { staff, options } = useActiveStaff(hospital, base);
  const span =
    validDate(from) && validDate(to)
      ? (Date.parse(`${to}T00:00:00Z`) - Date.parse(`${from}T00:00:00Z`)) / 86400000
      : -1;
  const rangeError =
    !validDate(from) || !validDate(to)
      ? "Choose dates between 2000 and 2199."
      : span < 0
        ? "The end date must not be before the start date."
        : span >= MAX_REPORT_DAYS
          ? `Choose at most ${MAX_REPORT_DAYS} days.`
          : undefined;
  const staffId = person === "all" ? undefined : person;
  const punches = useApiPagedQuery<AttendancePunchReportRow>(
    ["workforce", "attendance", hospital, "punch-report", person, from, to],
    attendancePunchReportPath(base, { from, to, staff_id: staffId }),
    { enabled: rangeError === undefined },
  );
  return (
    <section
      aria-label="Punch report"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-end justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Punch report</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            Every punch in the hospital, voided ones included, most recently recorded first. Reasons
            stay on the punch ledger.
          </p>
        </div>
        <div className="flex flex-wrap items-start gap-3">
          <div className="w-64">
            <Field id="report-staff" label="Staff member">
              <SelectControl
                id="report-staff"
                value={person}
                onValueChange={setPerson}
                options={[
                  { value: "all", label: staff.isPending ? "Loading staff…" : "Everyone" },
                  ...options,
                ]}
              />
            </Field>
          </div>
          <div className="w-40">
            <Field id="report-from" label="From" error={rangeError}>
              <input
                id="report-from"
                type="date"
                className={inputClass}
                aria-invalid={rangeError !== undefined}
                value={from}
                onChange={(e) => setFrom(e.target.value)}
              />
            </Field>
          </div>
          <div className="w-40">
            <Field id="report-to" label="To">
              <input
                id="report-to"
                type="date"
                className={inputClass}
                aria-invalid={rangeError !== undefined}
                value={to}
                onChange={(e) => setTo(e.target.value)}
              />
            </Field>
          </div>
        </div>
      </div>
      {rangeError !== undefined ? null : punches.isPending ? (
        <p role="status" className="p-5">
          Loading punch report…
        </p>
      ) : punches.isError ? (
        <div className="p-5">
          <Failure message="Could not load the punch report." retry={punches.retry} />
        </div>
      ) : punches.items.length === 0 ? (
        <p className="p-5 text-sm text-muted-foreground">
          No punches between {from} and {to}.
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-xs text-muted-foreground">
              <tr>
                <th className="p-3">Date</th>
                <th className="p-3">Time</th>
                <th className="p-3">Staff</th>
                <th className="p-3">Job</th>
                <th className="p-3">Direction</th>
                <th className="p-3">Source</th>
                <th className="p-3">Recorded</th>
                <th className="p-3">State</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {punches.items.map((p) => (
                <tr key={p.id} className={p.voided ? "text-muted-foreground" : undefined}>
                  <td className="p-3">{p.local_date}</td>
                  <td className="p-3">{zonedClock(p.punched_at, p.timezone)}</td>
                  <td className="p-3">
                    {p.display_name} · {p.employee_code}
                  </td>
                  <td className="p-3">{jobLabel(p.job_category)}</td>
                  <td className="p-3">{p.direction === "in" ? "In" : "Out"}</td>
                  <td className="p-3">
                    {p.correction_id ? "Correction" : p.source === "manual" ? "Manual" : p.source}
                  </td>
                  <td className="p-3">
                    {p.recorded_by_subject} · {new Date(p.recorded_at).toLocaleString()}
                  </td>
                  <td className="p-3">
                    {p.voided
                      ? `Voided by ${p.voided_by_subject ?? "unknown"}${p.voided_at ? ` · ${new Date(p.voided_at).toLocaleString()}` : ""}${p.void_correction_id ? " · by correction" : ""}`
                      : "Active"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {punches.hasMore && (
        <div className="border-t p-5">
          <Button variant="outline" disabled={punches.isFetchingMore} onClick={punches.loadMore}>
            Load more punches
          </Button>
        </div>
      )}
    </section>
  );
}
