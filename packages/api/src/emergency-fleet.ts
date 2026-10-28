import { z } from "zod";
import choices from "../../../backend/internal/modules/emergency/contract/fleet.json";
import { zonedDateTimeISO } from "./customer-time";

export const emergencyFleetChoices = choices;
const choice = (group: Record<string, string>) =>
  z.string().refine((value) => Object.hasOwn(group, value), "Select a valid option");
const reference = z
  .string()
  .regex(/^[A-Za-z0-9._/-]{1,120}$/, "Reviewed evidence reference is required");
const version = z
  .number()
  .int()
  .min(0)
  .max(Number.MAX_SAFE_INTEGER - 1);
const id = z
  .string()
  .regex(
    /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i,
    "Select a valid resource",
  );
const subject = z
  .string()
  .min(1)
  .max(200)
  .refine(
    (value) =>
      value.trim() === value &&
      !Array.from(value).some((char) => {
        const code = char.codePointAt(0)!;
        return code < 32 || (code >= 127 && code <= 159);
      }),
    "Use a verified identity subject",
  );
const datetime = z
  .string()
  .transform(emergencyLocalTime)
  .pipe(z.string().datetime({ offset: true }));
export const emergencyVehicleSchema = z.object({
  version,
  code: z
    .string()
    .regex(/^[A-Z0-9-]{1,40}$/, "Use an operational code, uppercase letters, digits and hyphens"),
  vehicle_class: choice(choices.classes),
  certified_until: datetime,
  policy_reference: reference,
  oxygen_min_bar: z.number().int().min(1).max(300),
  readiness_minutes: z.number().int().min(1).max(1440),
  retired: z.boolean(),
});
export const emergencyCrewSchema = z.object({
  version,
  subject,
  qualification: choice(choices.qualifications),
  certified_until: datetime,
  evidence_reference: reference,
  suspended: z.boolean(),
});
export const emergencyDutySchema = z
  .object({
    vehicle_id: id,
    driver_subject: subject,
    clinician_subject: subject,
    ends_at: datetime,
  })
  .refine((value) => value.driver_subject !== value.clinician_subject, {
    path: ["clinician_subject"],
    message: "Driver and clinician must be different people",
  });
export const emergencyReadinessSchema = z.object({
  vehicle_id: id,
  vehicle_version: version.min(1),
  duty_id: id,
  oxygen_bar: z.number().int().min(0).max(300),
  defibrillator_passed: z.boolean(),
  equipment_complete: z.boolean(),
  roadworthy: z.boolean(),
});
export const emergencyConstraintClearanceSchema = z.object({ evidence_reference: reference });
export type EmergencyVehicleInput = z.infer<typeof emergencyVehicleSchema>;
export type EmergencyCrewInput = z.infer<typeof emergencyCrewSchema>;
export type EmergencyDutyInput = z.infer<typeof emergencyDutySchema>;
export type EmergencyReadinessInput = z.infer<typeof emergencyReadinessSchema>;
export type EmergencyConstraint = { id: string; code: keyof typeof choices.blockers };
export type EmergencyVehicle = EmergencyVehicleInput & {
  id: string;
  base_id: string;
  dispatchable: boolean;
  effective_capability: string | null;
  computed_at: string;
  duty_id: string | null;
  blockers: (keyof typeof choices.blockers)[];
  open_constraints: EmergencyConstraint[];
  created_at: string;
  updated_at: string;
};
export type EmergencyCrew = EmergencyCrewInput & {
  id: string;
  created_at: string;
  updated_at: string;
};
export type EmergencyDuty = EmergencyDutyInput & {
  id: string;
  ended_at: string | null;
  created_at: string;
  updated_at: string;
};

export function emergencyLocalTime(value: unknown): string {
  if (typeof value !== "string" || !value) return "";
  if (/(Z|[+-]\d{2}:\d{2})$/.test(value)) return value;
  const match = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2})(?::([0-5]\d)(\.\d{1,3})?)?$/.exec(value);
  if (!match) return value;
  const instant = zonedDateTimeISO(match[1]!, Intl.DateTimeFormat().resolvedOptions().timeZone);
  if (!instant) return value;
  return new Date(
    Date.parse(instant) + Number(match[2] ?? 0) * 1000 + Number(match[3] ?? 0) * 1000,
  ).toISOString();
}

export function emergencyTimeInput(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.valueOf())
    ? ""
    : new Date(date.valueOf() - date.getTimezoneOffset() * 60000).toISOString().slice(0, -1);
}
