import { z } from "zod";
import choices from "../../../backend/internal/modules/emergency/contract/intake.json";
import { apiFetchResponse } from "./client";

export const emergencyChoices = choices;
const choice = (group: Record<string, string>) =>
  z.string().refine((value) => Object.hasOwn(group, value), "Select a valid option");
const narrative = (max: number) =>
  z
    .string()
    .trim()
    .max(max)
    .refine(
      (value) =>
        !Array.from(value).some((char) => {
          const code = char.codePointAt(0)!;
          return (code < 32 && code !== 9 && code !== 10) || (code >= 127 && code <= 159);
        }),
      "Remove control characters",
    );
const singleLine = (max: number) =>
  z
    .string()
    .trim()
    .max(max)
    .refine(
      (value) =>
        !Array.from(value).some((char) => {
          const code = char.codePointAt(0)!;
          return code < 32 || (code >= 127 && code <= 159);
        }),
      "Use a single line without control characters",
    );
export const emergencyBaseSchema = z.object({
  name: singleLine(150).refine(Boolean, "Base name is required"),
  timezone: z
    .string()
    .min(1)
    .max(100)
    .refine((value) => {
      try {
        new Intl.DateTimeFormat("en", { timeZone: value });
        return true;
      } catch {
        return false;
      }
    }, "Use an IANA timezone"),
  policy_reference: z
    .string()
    .regex(/^[A-Za-z0-9._/-]{1,120}$/, "Reviewed policy reference is required"),
  contact_retention_days: z.number().int().min(1).max(365),
});
export const emergencyContactSchema = z.object({
  caller_name: singleLine(150),
  callback_phone: z
    .string()
    .trim()
    .refine(
      (value) => value === "" || /^\+?[0-9]{7,15}$/.test(value),
      "Use 7–15 digits, optionally starting with +",
    ),
  relationship: choice(choices.relationships),
});
export const emergencyIntakeSchema = z
  .object({
    channel: choice(choices.channels),
    requested_service: choice(choices.services),
    processing_basis: choice(choices.bases),
    contact: emergencyContactSchema,
    incident: z.object({ location_text: narrative(500), patient_description: narrative(1500) }),
  })
  .superRefine((value, ctx) => {
    if (
      value.processing_basis === "medical_emergency" &&
      ["patient_transport", "mortuary"].includes(value.requested_service)
    ) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["processing_basis"],
        message: "Use consent under the reviewed transport policy",
      });
    }
  });
export const emergencyClosureSchema = z.object({
  version: z.number().int().min(1),
  reason: choice(choices.closure_reasons),
  explanation: narrative(1000).refine(Boolean, "Review explanation is required"),
});
export const emergencyGrantSchema = z.object({
  subject: singleLine(200).refine(Boolean, "Verified identity subject is required"),
  level: choice(choices.access_levels),
});
export type EmergencyBaseInput = z.infer<typeof emergencyBaseSchema>;
export type EmergencyIntakeInput = z.infer<typeof emergencyIntakeSchema>;
export type EmergencyClosureInput = z.infer<typeof emergencyClosureSchema>;
export type EmergencyBase = EmergencyBaseInput & {
  id: string;
  access_level: "reader" | "dispatcher" | "supervisor" | null;
  created_at: string;
  updated_at: string;
};
export type EmergencyCase = {
  id: string;
  base_id: string;
  case_number: string;
  channel: string;
  requested_service: string;
  processing_basis: string;
  policy_reference: string;
  status: "received" | "closed";
  priority: "untriaged";
  version: number;
  contact_retain_until: string;
  contact_purged_at: string | null;
  closure_reason: string | null;
  closed_at: string | null;
  created_at: string;
  updated_at: string;
};
export type EmergencyCaseDetail = EmergencyCase & {
  contact: z.infer<typeof emergencyContactSchema> | null;
  incident: { location_text: string; patient_description: string };
  contact_expired: boolean;
  closure_explanation: string | null;
};
export type EmergencyTimeline = {
  id: string;
  case_id: string;
  version: number;
  state: string;
  reason: string | null;
  actor_subject: string;
  occurred_at: string;
  created_at: string;
};
export async function revokeEmergencyGrant(base: string, subject: string): Promise<void> {
  await apiFetchResponse(
    `/emergency/bases/${encodeURIComponent(base)}/access/${encodeURIComponent(subject)}`,
    { method: "DELETE" },
  );
}

export { v7 as newEmergencyId } from "uuid";
