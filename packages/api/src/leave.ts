import { leaveConfigInputContract } from "./leave-contract";
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
  `${halfDays / 2} ${halfDays === 2 ? "day" : "days"}`;
