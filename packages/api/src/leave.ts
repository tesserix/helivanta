import { z } from "zod";
import {
  leaveConfigInputContract,
  leaveCorrectionInputContract,
  leaveRequestInputContract,
  leaveSettingsInputContract,
  staffLeaveAssignmentInputContract,
  type LeaveRequestInput,
  type LeaveTypeRules,
} from "./leave-contract";
export * from "./leave-contract";

// Pass the revision IDs of untracked leave types so their zero-allowance rule is caught before submit.
export const leaveConfigSchema = (untracked: ReadonlySet<string> = new Set()) =>
  leaveConfigInputContract.superRefine((value, ctx) => {
    if (!value.reviewed)
      ctx.addIssue({
        code: "custom",
        path: ["reviewed"],
        message: "Confirm that you reviewed the configuration.",
      });
    const forbid = (present: boolean, path: string, message: string) => {
      if (present) ctx.addIssue({ code: "custom", path: [path], message });
    };
    if (value.kind === "leave_type") {
      if (!value.rules)
        ctx.addIssue({ code: "custom", path: ["rules"], message: "Set the leave type rules." });
      else if (value.rules.short_leave_minutes % 15)
        ctx.addIssue({
          code: "custom",
          path: ["rules", "short_leave_minutes"],
          message: "Use steps of 15 minutes.",
        });
      forbid(
        value.calendar_year !== undefined,
        "calendar_year",
        "Only holiday calendars have a year.",
      );
      forbid(value.holidays.length > 0, "holidays", "Only holiday calendars list holidays.");
      forbid(value.entries.length > 0, "entries", "Only leave structures list leave types.");
      return;
    }
    forbid(value.rules !== undefined, "rules", "Only leave types have rules.");
    if (value.kind === "holiday_calendar") {
      forbid(value.entries.length > 0, "entries", "Only leave structures list leave types.");
      const year = value.calendar_year;
      if (year === undefined) {
        ctx.addIssue({
          code: "custom",
          path: ["calendar_year"],
          message: "Choose the calendar year.",
        });
        return;
      }
      if (value.effective_on > `${year}-01-01`)
        ctx.addIssue({
          code: "custom",
          path: ["effective_on"],
          message: `Make the calendar effective on or before 1 January ${year}.`,
        });
      const seen = new Set<string>();
      value.holidays.forEach((day, index) => {
        if (!day.holiday_date.startsWith(`${year}-`))
          ctx.addIssue({
            code: "custom",
            path: ["holidays", index, "holiday_date"],
            message: `Choose a date in ${year}.`,
          });
        if (seen.has(day.holiday_date))
          ctx.addIssue({
            code: "custom",
            path: ["holidays", index, "holiday_date"],
            message: "Each date can be listed once.",
          });
        seen.add(day.holiday_date);
      });
      return;
    }
    forbid(
      value.calendar_year !== undefined,
      "calendar_year",
      "Only holiday calendars have a year.",
    );
    forbid(value.holidays.length > 0, "holidays", "Only holiday calendars list holidays.");
    if (!value.entries.length)
      ctx.addIssue({ code: "custom", path: ["entries"], message: "Add at least one leave type." });
    const seen = new Set<string>();
    value.entries.forEach((entry, index) => {
      if (seen.has(entry.leave_type_revision_id))
        ctx.addIssue({
          code: "custom",
          path: ["entries", index, "leave_type_revision_id"],
          message: "Add each leave type once.",
        });
      seen.add(entry.leave_type_revision_id);
      if (
        untracked.has(entry.leave_type_revision_id) &&
        (entry.annual_half_days || entry.carry_forward_half_days)
      )
        ctx.addIssue({
          code: "custom",
          path: ["entries", index, "annual_half_days"],
          message: "Untracked leave types carry no allowance.",
        });
    });
  });

export const formatHalfDays = (halfDays: number) =>
  `${halfDays / 2} ${Math.abs(halfDays) === 2 ? "day" : "days"}`;

const leaveReviewMessage = "Confirm that you reviewed the change.";
export const leaveSettingsSchema = leaveSettingsInputContract.superRefine((value, ctx) => {
  if (!value.reviewed)
    ctx.addIssue({ code: "custom", path: ["reviewed"], message: leaveReviewMessage });
});
export const staffLeaveAssignmentSchema = staffLeaveAssignmentInputContract.superRefine(
  (value, ctx) => {
    if (!value.reviewed)
      ctx.addIssue({ code: "custom", path: ["reviewed"], message: leaveReviewMessage });
  },
);
export const leaveCorrectionSchema = leaveCorrectionInputContract.superRefine((value, ctx) => {
  if (!value.half_days)
    ctx.addIssue({
      code: "custom",
      path: ["half_days"],
      message: "Enter a non-zero adjustment.",
    });
});
export const leaveYearOpenSchema = z
  .object({
    leave_year: z
      .number({ error: "Enter a year." })
      .int("Enter a whole year.")
      .min(2000, "Choose a year from 2000 to 2199.")
      .max(2199, "Choose a year from 2000 to 2199."),
  })
  .strict();
export type LeaveYearOpenInput = z.infer<typeof leaveYearOpenSchema>;
export const leaveMonthName = (month: number) =>
  new Date(Date.UTC(2000, month - 1, 1)).toLocaleString("en", { month: "long", timeZone: "UTC" });

// Pass each leave type's rules so unit choices the type forbids are caught before submit.
export const leaveRequestFormSchema = (rules: ReadonlyMap<string, LeaveTypeRules> = new Map()) =>
  z
    .object({
      leave_type_id: z.string().min(1, "Choose a leave type."),
      unit: leaveRequestInputContract.shape.unit,
      start_on: z.string().date("Choose a start date."),
      end_on: z.string(),
      session: z.enum(["first_half", "second_half"]),
      minutes: z.number({ error: "Enter the minutes." }),
      reason: leaveRequestInputContract.shape.reason,
    })
    .strict()
    .superRefine((value, ctx) => {
      const issue = (path: string, message: string) =>
        ctx.addIssue({ code: "custom", path: [path], message });
      const type = rules.get(value.leave_type_id);
      if (value.unit === "full_day") {
        if (!/^\d{4}-\d{2}-\d{2}$/.test(value.end_on) || Number.isNaN(Date.parse(value.end_on)))
          issue("end_on", "Choose an end date.");
        else if (value.end_on < value.start_on)
          issue("end_on", "End the leave on or after its start date.");
        else if (Date.parse(value.end_on) - Date.parse(value.start_on) > 365 * 86_400_000)
          issue("end_on", "Keep one request within a year.");
      }
      if (value.unit === "half_day" && type && !type.half_day_allowed)
        issue("unit", "This leave type cannot be taken as a half day.");
      if (value.unit === "short_leave") {
        if (type && type.short_leave_minutes === 0)
          issue("unit", "This leave type cannot be taken as short leave.");
        else if (!Number.isInteger(value.minutes) || value.minutes < 15 || value.minutes % 15)
          issue("minutes", "Enter minutes in steps of 15.");
        else if (type && value.minutes > type.short_leave_minutes)
          issue(
            "minutes",
            `Short leave for this type is at most ${type.short_leave_minutes} minutes.`,
          );
      }
    });
export type LeaveRequestForm = z.infer<ReturnType<typeof leaveRequestFormSchema>>;

// Sends only the fields the chosen unit carries; single-day units end on their start date.
export const toLeaveRequestInput = (form: LeaveRequestForm): LeaveRequestInput => ({
  leave_type_id: form.leave_type_id,
  unit: form.unit,
  start_on: form.start_on,
  end_on: form.unit === "full_day" ? form.end_on : form.start_on,
  ...(form.unit === "half_day" && { session: form.session }),
  ...(form.unit === "short_leave" && { minutes: form.minutes }),
  reason: form.reason,
});

// Mirrors the report API: an inclusive period of at most 366 days between 2000 and 2199, optional search and filters.
export const leaveReportFilterSchema = z
  .object({
    report: z.enum(["monthly", "status"]),
    from: z.string().date("Choose a start date."),
    to: z.string(),
    q: z
      .string()
      .trim()
      .max(100, "Search with at most 100 characters.")
      .refine((value) => !value.includes("\0"), "Remove invalid characters."),
    department_id: z.string(),
    leave_type_id: z.string(),
    status: z.enum(["all", "pending", "approved", "rejected", "cancelled"]),
  })
  .strict()
  .superRefine((value, ctx) => {
    const issue = (path: string, message: string) =>
      ctx.addIssue({ code: "custom", path: [path], message });
    if (value.from && (value.from < "2000-01-01" || value.from > "2199-12-31"))
      issue("from", "Choose a date from 2000 to 2199.");
    if (!/^\d{4}-\d{2}-\d{2}$/.test(value.to) || Number.isNaN(Date.parse(value.to)))
      issue("to", "Choose an end date.");
    else if (value.to > "2199-12-31") issue("to", "Choose a date from 2000 to 2199.");
    else if (value.to < value.from) issue("to", "End the period on or after its start.");
    else if (Date.parse(value.to) - Date.parse(value.from) > 365 * 86_400_000)
      issue("to", "Keep the period within 366 days.");
  });
export type LeaveReportFilter = z.infer<typeof leaveReportFilterSchema>;

// Builds the report path; status-only filters are sent only to the status report.
export const leaveReportPath = (base: string, f: LeaveReportFilter) => {
  const query = new URLSearchParams({ from: f.from, to: f.to });
  if (f.q) query.set("q", f.q);
  if (f.department_id) query.set("department_id", f.department_id);
  if (f.report === "status") {
    if (f.leave_type_id) query.set("leave_type_id", f.leave_type_id);
    if (f.status !== "all") query.set("status", f.status);
  }
  return `${base}/hr/leave-reports/${f.report}?${query.toString()}`;
};
