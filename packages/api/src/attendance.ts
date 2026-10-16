import { z } from "zod";
import {
  attendanceSettingsInputContract,
  type AttendancePunchInput,
  type AttendanceVoidInput,
} from "./attendance-contract";

export * from "./attendance-contract";

const allowance = (label: string, min: number, max: number) =>
  z
    .number({ invalid_type_error: `Enter the ${label}.` })
    .int(`Enter the ${label} as a whole number.`)
    .min(min, `Use at least ${min} for the ${label}.`)
    .max(max, `Keep the ${label} within ${max}.`);

export const attendanceSettingsSchema = attendanceSettingsInputContract
  .extend({
    early_arrival_minutes: allowance("early arrival allowance", 0, 240),
    late_departure_minutes: allowance("late departure allowance", 0, 720),
    early_departure_grace_minutes: allowance("early departure grace", 0, 240),
    short_duty_minutes: allowance("short duty tolerance", 0, 720),
    duplicate_window_minutes: allowance("duplicate window", 1, 60),
    backdate_days: allowance("back-dating window", 0, 92),
  })
  .superRefine((value, ctx) => {
    if (!value.reviewed)
      ctx.addIssue({
        code: "custom",
        path: ["reviewed"],
        message: "Confirm that you reviewed the policy.",
      });
  });

const clock = /^([01][0-9]|2[0-3]):[0-5][0-9]$/;
const reason = (missing: string) =>
  z
    .string()
    .trim()
    .min(1, missing)
    .max(500, "Keep the reason within 500 characters.")
    .refine((value) => !value.includes("\0"), "Remove invalid characters.");

export const attendancePunchFormSchema = z
  .object({
    staff_id: z.string().min(1, "Choose a staff member."),
    direction: z.enum(["in", "out"], { errorMap: () => ({ message: "Choose in or out." }) }),
    date: z.string().date("Choose the punch date."),
    time: z.string().regex(clock, "Use a 24-hour time such as 08:05."),
    reason: reason("Say why this punch is entered by hand."),
  })
  .strict();
export type AttendancePunchForm = z.infer<typeof attendancePunchFormSchema>;

export const attendanceVoidFormSchema = z
  .object({ reason: reason("Say why this punch is being voided.") })
  .strict();
export type AttendanceVoidForm = z.infer<typeof attendanceVoidFormSchema>;

const offsetAt = (instant: number, timeZone: string) => {
  const parts = Object.fromEntries(
    new Intl.DateTimeFormat("en-US", {
      timeZone,
      hourCycle: "h23",
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
    })
      .formatToParts(new Date(instant))
      .map((p) => [p.type, p.value]),
  );
  const local = Date.UTC(+parts.year, +parts.month - 1, +parts.day, +parts.hour, +parts.minute);
  return local - Math.floor(instant / 60000) * 60000;
};

// zonedInstant reads a wall-clock date and time in the hospital timezone; a time skipped by a clock change has no instant.
export const zonedInstant = (date: string, time: string, timeZone: string): string | null => {
  const wall = Date.UTC(
    +date.slice(0, 4),
    +date.slice(5, 7) - 1,
    +date.slice(8, 10),
    +time.slice(0, 2),
    +time.slice(3, 5),
  );
  for (const guess of [
    wall - offsetAt(wall, timeZone),
    wall - offsetAt(wall - 86400000, timeZone),
  ]) {
    if (guess + offsetAt(guess, timeZone) === wall)
      return new Date(guess).toISOString().replace(".000Z", "Z");
  }
  return null;
};

// zonedClock shows an instant as the hospital's wall-clock time.
export const zonedClock = (instant: string, timeZone: string) =>
  new Intl.DateTimeFormat("en-GB", {
    timeZone,
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  }).format(new Date(instant));

export const toAttendancePunch = (
  form: AttendancePunchForm,
  timeZone: string,
): AttendancePunchInput | null => {
  const at = zonedInstant(form.date, form.time, timeZone);
  return at === null
    ? null
    : { staff_id: form.staff_id, direction: form.direction, punched_at: at, reason: form.reason };
};

export const toAttendanceVoid = (form: AttendanceVoidForm): AttendanceVoidInput => ({
  reason: form.reason,
});

export const attendanceSheetPath = (base: string, date: string) =>
  `${base}/hr/attendance?date=${encodeURIComponent(date)}`;

export const attendanceTotalsPath = (base: string, date: string) =>
  `${base}/hr/attendance-totals?date=${encodeURIComponent(date)}`;

export const attendancePunchesPath = (base: string, staff: string, from: string, to: string) =>
  `${base}/hr/attendance/punches?${new URLSearchParams({ staff_id: staff, from, to })}`;

export const staffAttendancePath = (base: string, staff: string, from: string, to: string) =>
  `${base}/staff/${encodeURIComponent(staff)}/attendance?${new URLSearchParams({ from, to })}`;
