"use client";
import { Button } from "@tesserix/web";
import {
  formatHalfDays,
  leaveReportPath,
  useApiPagedQuery,
  LEAVE_REQUEST_STATUSES,
  LEAVE_SESSIONS,
  LEAVE_UNITS,
  type LeaveMonthlyReportRow,
  type LeaveReportFilter,
  type LeaveStatusReportRow,
} from "@helivanta/api";
import { Failure } from "../roster-policy/policy-ui";

const cell = "px-4 py-3 align-top";
const head = "px-4 py-3 text-left text-xs font-semibold uppercase";

export const describeTaken = (halfDays: number, minutes: number) =>
  [halfDays ? formatHalfDays(halfDays) : "", minutes ? `${minutes} min` : ""]
    .filter(Boolean)
    .join(" · ") || "—";

export function LeaveReportTable({
  hospital,
  base,
  filter,
}: {
  hospital: string;
  base: string;
  filter: LeaveReportFilter;
}) {
  const path = leaveReportPath(base, filter);
  const rows = useApiPagedQuery<LeaveMonthlyReportRow | LeaveStatusReportRow>(
    ["workforce", "leave-reports", hospital],
    path,
  );
  // The API includes profile columns only for enrollment managers, so their presence decides the columns.
  const profiles = rows.items.some((r) => r.department !== undefined);
  return (
    <section
      aria-label={filter.report === "monthly" ? "Leave by month" : "Leave status"}
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="border-b p-5">
        <h3 className="font-semibold">
          {filter.report === "monthly" ? "Leave by month" : "Leave status"}
        </h3>
        <p className="mt-1 text-xs text-muted-foreground">
          {filter.from} to {filter.to}
          {filter.report === "monthly"
            ? " · approved leave only, holidays excluded"
            : " · requests overlapping the period, newest filing first"}
        </p>
      </div>
      {rows.isPending ? (
        <p role="status" className="p-5">
          Loading report…
        </p>
      ) : rows.isError ? (
        <div className="p-5">
          <Failure message="Could not load the report." retry={rows.retry} />
        </div>
      ) : rows.items.length === 0 ? (
        <p className="p-5 text-sm text-muted-foreground">No leave matches these filters.</p>
      ) : filter.report === "monthly" ? (
        <MonthlyTable rows={rows.items as LeaveMonthlyReportRow[]} profiles={profiles} />
      ) : (
        <StatusTable rows={rows.items as LeaveStatusReportRow[]} profiles={profiles} />
      )}
      {rows.hasMore && (
        <div className="border-t p-5">
          <Button variant="outline" disabled={rows.isFetchingMore} onClick={rows.loadMore}>
            Load more rows
          </Button>
        </div>
      )}
    </section>
  );
}

function Person({
  r,
  profiles,
}: {
  r: LeaveMonthlyReportRow | LeaveStatusReportRow;
  profiles: boolean;
}) {
  return (
    <>
      <td className={cell}>
        <span className="font-medium">{r.display_name}</span>
        <span className="block text-xs text-muted-foreground">{r.employee_code}</span>
      </td>
      {profiles && (
        <>
          <td className={cell}>{r.department || "—"}</td>
          <td className={cell}>{r.joined_on ?? "—"}</td>
        </>
      )}
    </>
  );
}

function PersonHead({ profiles }: { profiles: boolean }) {
  return (
    <>
      <th scope="col" className={head}>
        Staff
      </th>
      {profiles && (
        <>
          <th scope="col" className={head}>
            Department
          </th>
          <th scope="col" className={head}>
            Joined
          </th>
        </>
      )}
    </>
  );
}

function MonthlyTable({ rows, profiles }: { rows: LeaveMonthlyReportRow[]; profiles: boolean }) {
  const types = new Map<string, { code: string; name: string }>();
  for (const r of rows)
    for (const m of r.months) for (const t of m.types) types.set(t.leave_type_id, t);
  const columns = [...types.entries()].sort(([, a], [, b]) => a.code.localeCompare(b.code));
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="border-b bg-muted/40">
          <tr>
            <PersonHead profiles={profiles} />
            <th scope="col" className={head}>
              Month
            </th>
            <th scope="col" className={head}>
              Leave dates
            </th>
            {columns.map(([id, t]) => (
              <th key={id} scope="col" className={head} title={t.name}>
                {t.code}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y">
          {rows.flatMap((r) =>
            r.months.map((m) => (
              <tr key={`${r.staff_id}-${m.month}`}>
                <Person r={r} profiles={profiles} />
                <td className={cell}>{m.month}</td>
                <td className={cell}>{m.leave_dates.join(", ")}</td>
                {columns.map(([id]) => {
                  const t = m.types.find((x) => x.leave_type_id === id);
                  return (
                    <td key={id} className={cell}>
                      {t ? describeTaken(t.half_days, t.minutes) : "—"}
                    </td>
                  );
                })}
              </tr>
            )),
          )}
        </tbody>
      </table>
    </div>
  );
}

const describeDates = (r: LeaveStatusReportRow) =>
  r.unit === "half_day"
    ? LEAVE_SESSIONS[r.session ?? "first_half"]
    : r.unit === "short_leave"
      ? `${LEAVE_UNITS.short_leave}, ${r.minutes} min`
      : LEAVE_UNITS.full_day;

function StatusTable({ rows, profiles }: { rows: LeaveStatusReportRow[]; profiles: boolean }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead className="border-b bg-muted/40">
          <tr>
            <PersonHead profiles={profiles} />
            {["Leave", "Start", "End", "Total", "Requested by", "Decided by", "Status"].map(
              (label) => (
                <th key={label} scope="col" className={head}>
                  {label}
                </th>
              ),
            )}
          </tr>
        </thead>
        <tbody className="divide-y">
          {rows.map((r) => (
            <tr key={r.id}>
              <Person r={r} profiles={profiles} />
              <td className={cell}>
                {r.name} ({r.code})
                <span className="block text-xs text-muted-foreground">{describeDates(r)}</span>
              </td>
              <td className={cell}>{r.start_on}</td>
              <td className={cell}>{r.end_on}</td>
              <td className={cell}>{describeTaken(r.half_days, 0)}</td>
              <td className={cell}>{r.requested_by_subject}</td>
              <td className={cell}>{r.decided_by_subject ?? "—"}</td>
              <td className={cell}>{LEAVE_REQUEST_STATUSES[r.status]}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
