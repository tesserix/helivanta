import { z } from "zod";

export const SHIFT_TYPES = {
  day: "Day",
  evening: "Evening",
  night: "Night",
  rotating: "Rotating",
  on_call: "On call",
} as const;
const clock = z.string().regex(/^([01]\d|2[0-3]):[0-5]\d$/, "Use a valid HH:MM time.");
export function shiftDuration(start: string, end: string) {
  const minutes = (value: string) => Number(value.slice(0, 2)) * 60 + Number(value.slice(3, 5));
  return (minutes(end) - minutes(start) + 1440) % 1440;
}
export const shiftSchema = z
  .object({
    expected_version: z.number().int().min(0),
    shift_type: z.enum(["day", "evening", "night", "rotating", "on_call"]),
    code: z
      .string()
      .trim()
      .toUpperCase()
      .regex(/^[A-Z0-9_-]{1,40}$/, "Use up to 40 letters, numbers, underscores or hyphens."),
    name: z
      .string()
      .trim()
      .min(1, "Enter a name.")
      .max(80)
      .refine((v) => !v.includes("\0"), "Remove invalid characters."),
    description: z
      .string()
      .trim()
      .max(500)
      .refine((v) => !v.includes("\0"), "Remove invalid characters."),
    active: z.boolean(),
    start_time: clock,
    end_time: clock,
    break_minutes: z.number().int().min(0).max(1439),
    grace_minutes: z.number().int().min(0).max(240),
    full_day_ot_minutes: z.number().int().min(0).max(1440),
    half_day_ot_minutes: z.number().int().min(0).max(1440),
    locum: z.boolean(),
  })
  .strict()
  .superRefine((value, ctx) => {
    const duration = shiftDuration(value.start_time, value.end_time);
    if (!duration)
      ctx.addIssue({
        code: "custom",
        path: ["end_time"],
        message: "A shift must be shorter than 24 hours and have different start and end times.",
      });
    if (value.break_minutes >= duration)
      ctx.addIssue({
        code: "custom",
        path: ["break_minutes"],
        message: "Break must be shorter than the shift.",
      });
    if (value.half_day_ot_minutes > value.full_day_ot_minutes)
      ctx.addIssue({
        code: "custom",
        path: ["half_day_ot_minutes"],
        message: "Half-day OT minutes cannot exceed full-day OT minutes.",
      });
  });
export type ShiftInput = z.infer<typeof shiftSchema>;
export type ShiftDefinition = Omit<ShiftInput, "expected_version"> & {
  id: string;
  revision_id: string;
  version: number;
  duration_minutes: number;
  created_at: string;
  updated_at: string;
};
export type ShiftRevision = Omit<ShiftDefinition, "revision_id" | "updated_at"> & {
  shift_id: string;
};
