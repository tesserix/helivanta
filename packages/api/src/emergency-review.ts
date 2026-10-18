import { z } from "zod";
import { emergencyCrewSchema, emergencyVehicleSchema } from "./emergency-fleet";

export const emergencyClinicalSubjectSchema = z.object({
  subject: emergencyCrewSchema.shape.subject,
});
export const emergencyClinicalGrantSchema = z.object({
  version: emergencyVehicleSchema.shape.version,
  enabled: z.boolean(),
  evidence_reference: emergencyVehicleSchema.shape.policy_reference,
  expires_at: emergencyCrewSchema.shape.certified_until,
});
export const emergencyTransportReviewSchema = z.object({
  case_version: emergencyVehicleSchema.shape.version.min(1),
  required_class: emergencyVehicleSchema.shape.vehicle_class,
  policy_reference: emergencyVehicleSchema.shape.policy_reference,
  valid_until: emergencyCrewSchema.shape.certified_until,
});
export type EmergencyClinicalGrantInput = z.infer<typeof emergencyClinicalGrantSchema>;
export type EmergencyTransportReviewInput = z.infer<typeof emergencyTransportReviewSchema>;
export type EmergencyClinicalGrant = EmergencyClinicalGrantInput & {
  id: string;
  subject: string;
  verified_by: string;
  currently_active: boolean;
  created_at: string;
  updated_at: string;
};
export type EmergencyTransportReview = EmergencyTransportReviewInput & {
  id: string;
  case_id: string;
  sequence: number;
  reviewer_grant_id: string;
  reviewer_subject: string;
  created_at: string;
  updated_at: string;
};
