"use client";
import { useState } from "react";
import { Button } from "@tesserix/web";
import {
  attendanceSheetPath,
  attendanceTotalsPath,
  formatMinutes,
  useApiPagedQuery,
  useApiQuery,
  zonedClock,
  ATTENDANCE_EXCEPTIONS,
  ATTENDANCE_STATUSES,
  type AttendanceCount,
  type AttendanceDay,
  type AttendanceShift,
  type AttendanceTotals,
} from "@helivanta/api";
import { Field } from "@helivanta/ui";
import { Failure, inputClass, today } from "../roster-policy/policy-ui";
import { PunchForm } from "./punch-panel";

const cell = "px-4 py-3 align-top";
const head = "px-4 py-3 text-left text-xs font-semibold uppercase";
const validDate = (d: string) => /^(2[01]\d\d)-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])$/.test(d);
const LEAVE: Record<AttendanceDay["leave"], string> = {
  none: "",
  full_day: "Full-day leave",
  first_half: "First-half leave",
  second_half: "Second-half leave",
  short: "Short leave",
};

const label = (labels: Record<string, string>, code: string) => labels[code] ?? code;

export function DaySheet({
  hospital,
  base,
  recorder,
  timezone,
}: {
  hospital: string;
  base: string;
  recorder: boolean;
  timezone?: string;
}) {
  const [date, setDate] = useState(today);
  const [recording, setRecording] = useState(false);
  const valid = validDate(date);
  return (
    <section
      aria-label="Attendance by day"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-end justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Attendance by day</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            Derived from published rosters, approved leave and overtime, and punches. Payable
            minutes count only matched duty.
          </p>
        </div>
        <div className="flex flex-wrap items-end gap-3">
          <div className="w-48">
            <Field
              id="attendance-date"
              label="Date"
              error={valid ? undefined : "Choose a date between 2000 and 2199."}
            >
              <input
                id="attendance-date"
                type="date"
                className={inputClass}
                aria-invalid={!valid}
                value={date}
                onChange={(e) => setDate(e.target.value)}
              />
            </Field>
          </div>
          {recorder && timezone && !recording && (
            <Button onClick={() => setRecording(true)}>Record punch</Button>
          )}
        </div>
      </div>
      {recorder && !timezone && (
        <p className="border-b p-5 text-sm text-muted-foreground">
          Punches can be recorded once the hospital&apos;s roster settings set its timezone.
        </p>
      )}
      {recording && timezone && (
        <PunchForm
          hospital={hospital}
          base={base}
          timezone={timezone}
          date={valid ? date : today()}
          onClose={() => setRecording(false)}
        />
      )}
      {valid && <Totals hospital={hospital} base={base} date={date} />}
      {valid && <Sheet hospital={hospital} base={base} date={date} />}
    </section>
  );
}

function Totals({ hospital, base, date }: { hospital: string; base: string; date: string }) {
  const totals = useApiQuery<AttendanceTotals>(
    ["workforce", "attendance", hospital, "totals", date],
    attendanceTotalsPath(base, date),
  );
  if (totals.isPending)
    return (
      <p role="status" className="border-b p-5">
        Loading day totals…
      </p>
    );
  if (totals.isError || !totals.data)
    return (
      <div className="border-b p-5">
        <Failure message="Could not load day totals." retry={() => totals.refetch()} />
      </div>
    );
  const t = totals.data;
  return (
    <div role="region" aria-label="Day totals" className="space-y-3 border-b p-5 text-sm">
      <dl className="grid gap-4 sm:grid-cols-5">
        <Fact label="People" value={String(t.people)} />
        <Fact label="Shifts" value={String(t.shifts)} />
        <Fact label="Payable" value={formatMinutes(t.payable_minutes)} />
        <Fact label="Approved overtime" value={formatMinutes(t.approved_overtime_minutes)} />
        <Fact label="Unpaid half-days" value={String(t.unpaid_half_days)} />
      </dl>
      <Counts title="Statuses" counts={t.statuses} labels={ATTENDANCE_STATUSES} />
      <Counts title="Exceptions" counts={t.exceptions} labels={ATTENDANCE_EXCEPTIONS} />
    </div>
  );
}

function Counts({
  title,
  counts,
  labels,
}: {
  title: string;
  counts: AttendanceCount[];
  labels: Record<string, string>;
}) {
  if (counts.length === 0) return null;
  return (
    <p className="text-muted-foreground">
      <span className="font-medium text-foreground">{title}:</span>{" "}
      {counts.map((c) => `${label(labels, c.code)} ${c.count}`).join(" · ")}
    </p>
  );
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-1 font-medium">{value}</dd>
    </div>
  );
}

function Sheet({ hospital, base, date }: { hospital: string; base: string; date: string }) {
  const days = useApiPagedQuery<AttendanceDay>(
    ["workforce", "attendance", hospital, "sheet", date],
    attendanceSheetPath(base, date),
  );
  if (days.isPending)
    return (
      <p role="status" className="p-5">
        Loading attendance…
      </p>
    );
  if (days.isError)
    return (
      <div className="p-5">
        <Failure message="Could not load attendance." retry={days.retry} />
      </div>
    );
  if (days.items.length === 0)
    return (
      <p className="p-5 text-sm text-muted-foreground">
        Nobody is rostered, on leave or punched on {date}.
      </p>
    );
  return (
    <>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="border-b bg-muted/40">
            <tr>
              <th scope="col" className={head}>
                Staff
              </th>
              <th scope="col" className={head}>
                Shifts
              </th>
              <th scope="col" className={head}>
                Exceptions
              </th>
              <th scope="col" className={head}>
                Payable
              </th>
            </tr>
          </thead>
          <tbody className="divide-y">
            {days.items.map((d) => (
              <tr key={d.staff_id}>
                <td className={cell}>
                  <span className="font-medium">{d.display_name}</span>
                  <span className="block text-xs text-muted-foreground">{d.employee_code}</span>
                  {d.leave !== "none" && (
                    <span className="block text-xs text-muted-foreground">
                      {LEAVE[d.leave]}
                      {d.leave === "short" ? ` · ${formatMinutes(d.short_leave_minutes)}` : ""}
                    </span>
                  )}
                </td>
                <td className={cell}>
                  {d.shifts.length === 0 ? (
                    <span className="text-muted-foreground">No rostered shift</span>
                  ) : (
                    <ul className="space-y-2">
                      {d.shifts.map((s) => (
                        <ShiftLine key={s.allocation_id} shift={s} timezone={d.timezone} />
                      ))}
                    </ul>
                  )}
                  {d.stray_punch_ids.length > 0 && (
                    <span className="mt-2 block text-xs text-muted-foreground">
                      {d.stray_punch_ids.length} punch
                      {d.stray_punch_ids.length === 1 ? "" : "es"} outside any shift
                    </span>
                  )}
                </td>
                <td className={cell}>
                  {d.exceptions.length === 0
                    ? "None"
                    : d.exceptions.map((e) => label(ATTENDANCE_EXCEPTIONS, e)).join(", ")}
                </td>
                <td className={`${cell} font-medium`}>
                  {formatMinutes(d.payable_minutes)}
                  {d.approved_overtime_minutes > 0 && (
                    <span className="block text-xs font-normal text-muted-foreground">
                      + {formatMinutes(d.approved_overtime_minutes)} approved overtime
                    </span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {days.hasMore && (
        <div className="border-t p-5">
          <Button variant="outline" disabled={days.isFetchingMore} onClick={days.loadMore}>
            Load more people
          </Button>
        </div>
      )}
    </>
  );
}

function ShiftLine({ shift: s, timezone }: { shift: AttendanceShift; timezone: string }) {
  const clock = (at: string) => zonedClock(at, timezone);
  return (
    <li>
      <span className="font-medium">
        {s.shift_name} {clock(s.starts_at)}–{clock(s.ends_at)}
      </span>{" "}
      · {ATTENDANCE_STATUSES[s.status]}
      <span className="block text-xs text-muted-foreground">
        {s.first_in ? `In ${clock(s.first_in)}` : "No in punch"} ·{" "}
        {s.last_out ? `Out ${clock(s.last_out)}` : "No out punch"} · worked{" "}
        {formatMinutes(s.worked_minutes)} of {formatMinutes(s.expected_minutes)}
        {s.late_minutes > 0 ? ` · ${formatMinutes(s.late_minutes)} late` : ""}
        {s.early_departure_minutes > 0
          ? ` · left ${formatMinutes(s.early_departure_minutes)} early`
          : ""}
        {s.unpaid_half_days > 0 ? ` · ${s.unpaid_half_days} unpaid half-day` : ""}
      </span>
    </li>
  );
}
