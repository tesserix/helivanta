import {
  rosterChangeInputContract,
  rosterConfigInputContract,
  rosterInputContract,
  rosterPopulateInputContract,
  rosterSettingsInputContract,
  rosterStatusInputContract,
  staffRosterAssignmentInputContract,
} from "./roster-config-contract";
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
// Mirrors the API and database rule: canonical IANA area names only, never Local or posix/ aliases.
const IANA_ZONE = /^(UTC|[A-Z][A-Za-z_]+(\/[A-Za-z0-9_+-]+){1,2})$/;
export function isSupportedTimezone(name: string) {
  if (!IANA_ZONE.test(name)) return false;
  try {
    new Intl.DateTimeFormat("en", { timeZone: name });
    return true;
  } catch {
    return false;
  }
}
const reviewMessage = "Confirm that you reviewed the policy.";
export const rosterSettingsSchema = rosterSettingsInputContract.superRefine((value, ctx) => {
  if (!value.reviewed) ctx.addIssue({ code: "custom", path: ["reviewed"], message: reviewMessage });
  if (!isSupportedTimezone(value.timezone))
    ctx.addIssue({
      code: "custom",
      path: ["timezone"],
      message: "Use a supported IANA timezone such as Asia/Kolkata.",
    });
});
export const staffRosterAssignmentSchema = staffRosterAssignmentInputContract.superRefine(
  (value, ctx) => {
    if (!value.reviewed)
      ctx.addIssue({ code: "custom", path: ["reviewed"], message: reviewMessage });
  },
);

const DAY_MS = 86_400_000;
export const addDays = (date: string, days: number) =>
  new Date(Date.parse(`${date}T00:00:00Z`) + days * DAY_MS).toISOString().slice(0, 10);
// Monday=0…Sunday=6, matching roster settings.
export const weekdayOf = (date: string) => (new Date(`${date}T00:00:00Z`).getUTCDay() + 6) % 7;
export const rosterDates = (start: string, end: string) => {
  const dates: string[] = [];
  for (let d = start; d <= end && dates.length < 42; d = addDays(d, 1)) dates.push(d);
  return dates;
};
const rosterReview = "Confirm that you reviewed the roster.";
export const rosterSchema = rosterInputContract.superRefine((value, ctx) => {
  if (!value.reviewed) ctx.addIssue({ code: "custom", path: ["reviewed"], message: rosterReview });
  const days = (Date.parse(value.end_date) - Date.parse(value.start_date)) / DAY_MS + 1;
  if (!(days >= 7 && days <= 42 && days % 7 === 0))
    ctx.addIssue({
      code: "custom",
      path: ["end_date"],
      message: "Plan one to six whole weeks.",
    });
});
export const rosterChangeSchema = rosterChangeInputContract.superRefine((value, ctx) => {
  if (!value.reviewed) ctx.addIssue({ code: "custom", path: ["reviewed"], message: rosterReview });
  if (!value.add.length && !value.remove.length)
    ctx.addIssue({ code: "custom", path: ["add"], message: "Add or remove at least one shift." });
  if (value.mode === "roster" && new Set(value.add.map((a) => a.area_revision_id)).size > 1)
    ctx.addIssue({ code: "custom", path: ["add"], message: "Roster-wise changes use one area." });
  if (value.mode === "employee" && new Set(value.add.map((a) => a.staff_id)).size > 1)
    ctx.addIssue({
      code: "custom",
      path: ["add"],
      message: "Employee-wise changes cover one person.",
    });
  if (value.mode === "additional" && value.remove.length)
    ctx.addIssue({
      code: "custom",
      path: ["remove"],
      message: "Additional shifts only add; use roster-wise changes to remove.",
    });
});
export const rosterPopulateSchema = rosterPopulateInputContract.superRefine((value, ctx) => {
  if (!value.reviewed) ctx.addIssue({ code: "custom", path: ["reviewed"], message: rosterReview });
});
export const rosterStatusSchema = (needsReason: boolean) =>
  rosterStatusInputContract.superRefine((value, ctx) => {
    if (!value.reviewed)
      ctx.addIssue({ code: "custom", path: ["reviewed"], message: rosterReview });
    if (needsReason && !value.reason.trim())
      ctx.addIssue({
        code: "custom",
        path: ["reason"],
        message: "Give a reason; it is kept in the roster history.",
      });
  });
