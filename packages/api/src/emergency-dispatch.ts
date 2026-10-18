import { z } from "zod";
import {
  DispatchCommandInputContract,
  DispatchReservationInputContract,
  EMERGENCY_DISPATCH_CAPABILITIES,
  EMERGENCY_DISPATCH_OPERATIONS,
  EMERGENCY_DISPATCH_STATES,
} from "./emergency-dispatch-contract";

export { EMERGENCY_DISPATCH_OPERATIONS, EMERGENCY_DISPATCH_STATES };
const id = z
  .string()
  .uuid()
  .refine((value) => value[14] === "7", "A current resource identifier is required");
const reference = z
  .string()
  .min(1)
  .max(120)
  .regex(/^[A-Za-z0-9._/-]+$/, "Use an operational reference without personal details");
export const emergencyDispatchReservationSchema = DispatchReservationInputContract.extend({
  case_id: id,
  assessment_id: id,
  vehicle_id: id,
  duty_id: id,
}).strict();
export const emergencyDispatchCommandSchema = DispatchCommandInputContract.extend({
  evidence_reference: reference,
}).strict();
export type EmergencyDispatchReservationInput = z.infer<typeof emergencyDispatchReservationSchema>;
export type EmergencyDispatchCommandInput = z.infer<typeof emergencyDispatchCommandSchema>;
export type EmergencyDispatchState = keyof typeof EMERGENCY_DISPATCH_STATES;
export type EmergencyDispatch = EmergencyDispatchReservationInput & {
  id: string;
  base_id: string;
  requested_by: string;
  state: EmergencyDispatchState;
  version: number;
  driver_acknowledged: boolean;
  clinician_acknowledged: boolean;
  acknowledgement_deadline: string | null;
  created_at: string;
  updated_at: string;
};
export type EmergencyDispatchEligibility = {
  case_id: string;
  case_version: number;
  assessment_id: string | null;
  required_class: string | null;
  valid_until: string | null;
  eligible: boolean;
  dispatch_enabled: boolean;
  blockers: string[];
  computed_at: string;
};
export type EmergencyCrewDispatch = EmergencyDispatch & { own_acknowledged: boolean };
export type EmergencyDispatchCommandReceipt = {
  id: string;
  dispatch_id: string;
  expected_version: number;
  operation: EmergencyDispatchCommandInput["operation"];
  evidence_reference: string;
  actor_subject: string;
  state: EmergencyDispatchState;
  version: number;
  created_at: string;
};
export function emergencyClassSupports(actual: string | null, required: string | null): boolean {
  if (!actual || !required) return false;
  const allowed = EMERGENCY_DISPATCH_CAPABILITIES[
    actual as keyof typeof EMERGENCY_DISPATCH_CAPABILITIES
  ] as readonly string[] | undefined;
  return allowed?.includes(required) ?? false;
}
