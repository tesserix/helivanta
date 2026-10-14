import { rosterConfigInputContract } from "./roster-config-contract";
export * from "./roster-config-contract";

export const WEEKDAYS = [
  "Monday",
  "Tuesday",
  "Wednesday",
  "Thursday",
  "Friday",
  "Saturday",
  "Sunday",
] as const;
export const rosterConfigSchema = rosterConfigInputContract.superRefine((value, ctx) => {
  if (!value.reviewed)
    ctx.addIssue({
      code: "custom",
      path: ["reviewed"],
      message: "Confirm that you reviewed the configuration.",
    });
  if (value.kind !== "weekly_template") {
    if (!value.shift_revision_ids.length)
      ctx.addIssue({
        code: "custom",
        path: ["shift_revision_ids"],
        message: "Choose at least one shift.",
      });
    if (new Set(value.shift_revision_ids).size !== value.shift_revision_ids.length)
      ctx.addIssue({
        code: "custom",
        path: ["shift_revision_ids"],
        message: "Choose each shift once.",
      });
    if (value.days.length)
      ctx.addIssue({
        code: "custom",
        path: ["days"],
        message: "Weekdays belong to a weekly template.",
      });
    return;
  }
  if (value.shift_revision_ids.length)
    ctx.addIssue({
      code: "custom",
      path: ["shift_revision_ids"],
      message: "Choose shifts within each weekday.",
    });
  if (value.days.length !== 7 || new Set(value.days.map((d) => d.weekday)).size !== 7)
    ctx.addIssue({ code: "custom", path: ["days"], message: "Configure all seven weekdays once." });
  let count = 0;
  value.days.forEach((day, index) => {
    if (day.weekly_off && day.allocations.length)
      ctx.addIssue({
        code: "custom",
        path: ["days", index, "allocations"],
        message: "Weekly-off days cannot contain shifts.",
      });
    if (!day.weekly_off && !day.allocations.length)
      ctx.addIssue({
        code: "custom",
        path: ["days", index, "allocations"],
        message: "Choose a shift or mark this day as weekly off.",
      });
    if (new Set(day.allocations.map((a) => a.shift_revision_id)).size !== day.allocations.length)
      ctx.addIssue({
        code: "custom",
        path: ["days", index, "allocations"],
        message: "Choose each shift once per day.",
      });
    count += day.allocations.length;
  });
  if (!count)
    ctx.addIssue({ code: "custom", path: ["days"], message: "Choose at least one working day." });
});
