import { z } from "zod";
import {
  ReceivingFacilityInputContract,
  ReceivingAccessInputContract,
  ReceivingLinkInputContract,
  ReceivingLinkCommandInputContract,
  ReceivingRequestInputContract,
  ReceivingCommandInputContract,
  EMERGENCY_RECEIVING_STATES,
  EMERGENCY_RECEIVING_LINK_STATES,
} from "./emergency-dispatch-contract";
import {
  emergencyOperationalReferenceSchema as reference,
  emergencyResourceIdentifierSchema as id,
} from "./emergency-dispatch";
export { EMERGENCY_RECEIVING_STATES, EMERGENCY_RECEIVING_LINK_STATES };
export const emergencyReceivingFacilitySchema = ReceivingFacilityInputContract.extend({
  name: z.string().trim().min(1).max(150),
  policy_reference: reference,
}).strict();
export const emergencyReceivingAccessSchema = ReceivingAccessInputContract.extend({
  policy_reference: reference,
}).strict();
export const emergencyReceivingLinkSchema = ReceivingLinkInputContract.extend({
  facility_id: id,
  policy_reference: reference,
}).strict();
export const emergencyReceivingLinkCommandSchema = ReceivingLinkCommandInputContract.extend({
  evidence_reference: reference,
}).strict();
export const emergencyReceivingRequestSchema = ReceivingRequestInputContract.extend({
  link_id: id,
  previous_request_id: id.optional(),
  evidence_reference: reference,
}).strict();
export const emergencyReceivingCommandSchema = ReceivingCommandInputContract.extend({
  evidence_reference: reference,
})
  .strict()
  .superRefine((value, ctx) => {
    if ((value.operation === "accept") !== !!value.valid_until)
      ctx.addIssue({
        code: "custom",
        path: ["valid_until"],
        message: "Only acceptance requires an explicit lease",
      });
  });
export type EmergencyReceivingFacilityInput = z.infer<typeof emergencyReceivingFacilitySchema>;
export type EmergencyReceivingAccessInput = z.infer<typeof emergencyReceivingAccessSchema>;
export type EmergencyReceivingLinkInput = z.infer<typeof emergencyReceivingLinkSchema>;
export type EmergencyReceivingLinkCommandInput = z.infer<
  typeof emergencyReceivingLinkCommandSchema
>;
export type EmergencyReceivingRequestInput = z.infer<typeof emergencyReceivingRequestSchema>;
export type EmergencyReceivingCommandInput = z.infer<typeof emergencyReceivingCommandSchema>;
export type EmergencyReceivingFacility = EmergencyReceivingFacilityInput & {
  id: string;
  access_level: "receiver" | "manager" | null;
  created_at: string;
  updated_at: string;
  verified_by: string;
};
export type EmergencyReceivingLink = EmergencyReceivingLinkInput & {
  id: string;
  provider_tenant_id: string;
  base_id: string;
  status: keyof typeof EMERGENCY_RECEIVING_LINK_STATES;
  version: number;
  destination_name: string | null;
  requested_by: string;
  created_at: string;
  updated_at: string;
};
export type EmergencyReceivingRequest = {
  id: string;
  provider_tenant_id: string;
  base_id: string;
  dispatch_id: string;
  dispatch_version: number;
  link_id: string;
  previous_request_id: string | null;
  destination_tenant_id: string;
  facility_id: string;
  sequence: number;
  required_class: "patient_transport" | "bls" | "als";
  response_seconds: number;
  response_deadline: string;
  evidence_reference: string;
  requested_by: string;
  status: keyof typeof EMERGENCY_RECEIVING_STATES;
  version: number;
  acceptance_valid_until: string | null;
  destination_name: string | null;
  superseded: boolean;
  usable: boolean;
  handover_ready: boolean;
  arrival_recorded: boolean;
  created_at: string;
  updated_at: string;
};
export type EmergencyReceivingReceipt = {
  id: string;
  version: number;
  status: string;
  actor_subject: string;
  created_at: string;
};
