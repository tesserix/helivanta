import {
  LEAVE_ACCRUALS,
  LEAVE_HOLIDAY_CATEGORIES,
  LEAVE_PRORATIONS,
  formatHalfDays,
  type LeaveConfig,
} from "@helivanta/api";
import { effectiveDate } from "../roster-setup/roster-summary";

export const configNoun = {
  leave_type: "leave type",
  holiday_calendar: "holiday calendar",
  structure: "leave structure",
} as const;

const chip = "rounded-lg border bg-muted/30 px-3 py-2 text-xs";

export function LeaveSummary({ record }: { record: LeaveConfig }) {
  if (record.kind === "leave_type" && record.rules) {
    const r = record.rules;
    return (
      <ul className="flex flex-wrap gap-2">
        <li className={chip}>{r.paid ? "Paid" : "Unpaid"}</li>
        <li className={chip}>{r.balance_tracked ? "Balance tracked" : "No balance kept"}</li>
        <li className={chip}>{r.half_day_allowed ? "Half days allowed" : "Full days only"}</li>
        <li className={chip}>
          {r.short_leave_minutes
            ? `Short leave up to ${r.short_leave_minutes} minutes`
            : "No short leave"}
        </li>
        <li className={chip}>
          {r.clubbable ? "Can combine with other leave" : "Cannot combine with other leave"}
        </li>
      </ul>
    );
  }
  if (record.kind === "holiday_calendar")
    return record.holidays.length ? (
      <ul className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
        {record.holidays.map((h) => (
          <li key={h.holiday_date} className={chip}>
            <span className="font-medium">{effectiveDate(h.holiday_date)}</span>
            <span className="ml-2">{h.name}</span>
            <span className="ml-2 text-muted-foreground">
              {LEAVE_HOLIDAY_CATEGORIES[h.category]}
            </span>
          </li>
        ))}
      </ul>
    ) : (
      <p className="text-xs text-muted-foreground">No holidays in {record.calendar_year}.</p>
    );
  return (
    <ul className="grid gap-2 lg:grid-cols-2">
      {record.entries.map((e) => (
        <li key={e.leave_type_revision_id} className={chip}>
          <p className="font-medium">
            {e.name} · {e.code}
          </p>
          <p className="mt-1 text-muted-foreground">
            {e.balance_tracked
              ? `${formatHalfDays(e.annual_half_days)} a year · ${LEAVE_ACCRUALS[e.accrual]} · carry forward up to ${formatHalfDays(e.carry_forward_half_days)} · ${LEAVE_PRORATIONS[e.proration]}`
              : "No balance kept"}
            {e.probation_eligible ? " · Available during probation" : " · Not during probation"}
          </p>
        </li>
      ))}
    </ul>
  );
}
