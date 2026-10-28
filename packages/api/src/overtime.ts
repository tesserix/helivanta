import { z } from "zod";
import {
  overtimeSettingsInputContract,
  type OvertimeDecisionInput,
  type OvertimeRequestInput,
} from "./overtime-contract";

export * from "./overtime-contract";

const minutes = (label: string, max: number) =>
  z
    .number({ error: `Enter the ${label} in minutes.` })
    .int(`Enter the ${label} in whole minutes.`)
    .min(1, `Allow at least 1 minute ${label}.`)
    .max(max, `Keep the ${label} within ${max} minutes.`);

// Limits must nest: a day cannot allow more than its week, nor a week more than its month.
export const overtimeSettingsSchema = overtimeSettingsInputContract
  .extend({
    daily_limit_minutes: minutes("daily limit", 1440),
    weekly_limit_minutes: minutes("weekly limit", 10080),
    monthly_limit_minutes: minutes("monthly limit", 44640),
    filing_window_days: z
      .number({ error: "Enter the filing window in days." })
      .int("Enter the filing window in whole days.")
      .min(0, "Use a filing window of 0 days or more.")
      .max(92, "Keep the filing window within 92 days."),
  })
  .superRefine((value, ctx) => {
    const issue = (path: string, message: string) =>
      ctx.addIssue({ code: "custom", path: [path], message });
    if (!value.reviewed) issue("reviewed", "Confirm that you reviewed the policy.");
    if (value.weekly_limit_minutes < value.daily_limit_minutes)
      issue("weekly_limit_minutes", "Allow at least the daily limit each week.");
    if (value.monthly_limit_minutes < value.weekly_limit_minutes)
      issue("monthly_limit_minutes", "Allow at least the weekly limit each month.");
  });

const clock = /^([01][0-9]|2[0-3]):[0-5][0-9]$/;
const toMinutes = (time: string) => Number(time.slice(0, 2)) * 60 + Number(time.slice(3, 5));

// An end at or before the start is on the next day, matching the server.
export const overtimeMinutes = (start: string, end: string) =>
  clock.test(start) && clock.test(end)
    ? (toMinutes(end) - toMinutes(start) + 1440) % 1440 || 1440
    : 0;

export const overtimeRequestFormSchema = z
  .object({
    staff_id: z.string().min(1, "Choose a staff member."),
    work_date: z.string().date("Choose the work date."),
    start_time: z.string().regex(clock, "Use a 24-hour time such as 18:30."),
    end_time: z.string().regex(clock, "Use a 24-hour time such as 18:30."),
    reason: z
      .string()
      .trim()
      .min(1, "Add a reason for the overtime.")
      .max(500, "Keep the reason within 500 characters.")
      .refine((value) => !value.includes("\0"), "Remove invalid characters."),
  })
  .strict()
  .superRefine((value, ctx) => {
    const span = overtimeMinutes(value.start_time, value.end_time);
    if (span > 0 && span < 15)
      ctx.addIssue({
        code: "custom",
        path: ["end_time"],
        message: "Record at least 15 minutes of overtime.",
      });
  });
export type OvertimeRequestForm = z.infer<typeof overtimeRequestFormSchema>;

export const toOvertimeInput = (form: OvertimeRequestForm): OvertimeRequestInput => ({
  staff_id: form.staff_id,
  work_date: form.work_date,
  start_time: form.start_time,
  end_time: form.end_time,
  reason: form.reason,
});

// Approving fewer minutes than requested, rejecting and revoking each need a note.
export const overtimeDecisionFormSchema = (requested: number) =>
  z
    .object({
      approved_minutes: z
        .number({ error: "Enter the approved minutes." })
        .int("Enter whole minutes.")
        .min(1, "Approve at least 1 minute, or reject the request.")
        .max(requested, `Approve no more than the ${requested} minutes requested.`),
      note: z
        .string()
        .trim()
        .max(500, "Keep the note within 500 characters.")
        .refine((value) => !value.includes("\0"), "Remove invalid characters."),
    })
    .strict();
export type OvertimeDecisionForm = z.infer<ReturnType<typeof overtimeDecisionFormSchema>>;

export const noteRequiredFor = (
  action: "approve" | "reject" | "revoke" | "withdraw",
  form: OvertimeDecisionForm,
  requested: number,
) =>
  action === "reject" ||
  action === "revoke" ||
  (action === "approve" && form.approved_minutes < requested);

export const toOvertimeDecision = (
  version: number,
  decision: "approve" | "reject",
  form: OvertimeDecisionForm,
): OvertimeDecisionInput => ({
  expected_version: version,
  decision,
  ...(decision === "approve" && { approved_minutes: form.approved_minutes }),
  note: form.note,
});

// Builds the list path; an empty filter value is not sent.
export const overtimePath = (
  base: string,
  f: { status?: string; staff_id?: string; from?: string; to?: string },
) => {
  const query = new URLSearchParams();
  if (f.status && f.status !== "all") query.set("status", f.status);
  if (f.staff_id) query.set("staff_id", f.staff_id);
  if (f.from) query.set("from", f.from);
  if (f.to) query.set("to", f.to);
  const q = query.toString();
  return `${base}/hr/overtime${q ? `?${q}` : ""}`;
};

export const overtimeSummaryPath = (base: string, month: string) =>
  `${base}/hr/overtime-summary?month=${encodeURIComponent(month)}`;

export const formatMinutes = (total: number) => {
  const h = Math.floor(total / 60);
  const m = total % 60;
  return h === 0 ? `${m} min` : m === 0 ? `${h} h` : `${h} h ${m} min`;
};
