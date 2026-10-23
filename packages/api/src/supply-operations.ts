import type { z } from "zod";
import type {
  supplyAccessGrantFieldsSchema,
  supplyAccessLimitsInputSchema,
  supplyAccessGrantInputSchema,
  supplyCorporateInputSchema,
  supplyTerritoryFieldsSchema,
  supplyTerritoryInputSchema,
  supplySupportRequestInputSchema,
  supplySupportDecisionInputSchema,
  supplySupportRevokeInputSchema,
  supplyDeliveryTaskFieldsSchema,
  supplyDeliveryTaskInputSchema,
  supplyDeliveryActionInputSchema,
} from "./supply-contract";

export type SupplyCorporateStructure = {
  party_id: string;
  parent_party_id: string | null;
  version: number;
  created_at: string | null;
  updated_at: string | null;
};
export type SupplyCorporateInput = z.infer<typeof supplyCorporateInputSchema>;
export type SupplyTerritoryInput = z.infer<typeof supplyTerritoryInputSchema>;
export type SupplyTerritory = z.infer<typeof supplyTerritoryFieldsSchema> & {
  party_id: string;
  id: string;
  version: number;
  created_at: string;
  updated_at: string;
};
export type SupplyAccessLimitsInput = z.infer<typeof supplyAccessLimitsInputSchema>;
export type SupplyAccessLimits = Omit<SupplyAccessLimitsInput, "expected_version"> & {
  party_id: string;
  version: number;
  created_at: string | null;
  updated_at: string | null;
};
export type SupplyAccessGrantInput = z.infer<typeof supplyAccessGrantInputSchema>;
export type SupplyAccessGrant = z.infer<typeof supplyAccessGrantFieldsSchema> & {
  party_id: string;
  id: string;
  version: number;
  effective_status: "active" | "scheduled" | "expired" | "revoked";
  created_at: string;
  updated_at: string;
};
export type SupplyGrant = SupplyAccessGrant;
export type SupplyLegacyGrant = {
  id: string;
  party_id: string;
  subject: string;
  role: string;
  created_at: string;
};

export type SupplySupportRequestInput = z.infer<typeof supplySupportRequestInputSchema>;
export type SupplySupportDecisionInput = z.infer<typeof supplySupportDecisionInputSchema>;
export type SupplySupportRevokeInput = z.infer<typeof supplySupportRevokeInputSchema>;
export type SupplySupportRequest = Omit<SupplySupportRequestInput, "expected_version"> & {
  id: string;
  party_id: string;
  requester_subject: string;
  status: "pending" | "approved" | "rejected" | "revoked";
  approver_subject: string | null;
  decision_actor: string;
  decision_reason: string;
  version: number;
  created_at: string;
  updated_at: string;
  effective_status:
    "pending" | "scheduled" | "active" | "expired" | "rejected" | "revoked" | "approval_withdrawn";
  allowed_actions: Array<"approve" | "reject" | "revoke" | "read_diagnostics">;
};
export type SupplySupportDiagnostics = {
  support_id: string;
  party_id: string;
  premise_id: string;
  premise_kind: string;
  premise_status: string;
  bin_count: number;
  active_bin_count: number;
  quarantine_bin_count: number;
};
export type SupplyOperatingLocation = {
  id: string;
  party_id: string;
  code: string;
  name: string;
  kind: string;
  country_code: "IN";
  time_zone: string;
  status: "active" | "inactive";
  created_at: string;
  allowed_actions: string[];
};

export type SupplyDeliveryTaskInput = z.infer<typeof supplyDeliveryTaskInputSchema>;
export type SupplyDeliveryActionInput = z.infer<typeof supplyDeliveryActionInputSchema>;
export type SupplyDeliveryTask = Omit<z.infer<typeof supplyDeliveryTaskFieldsSchema>, "status"> & {
  id: string;
  party_id: string;
  premise_id: string;
  source_kind: "manual_logistics";
  assignment_generation: number;
  version: number;
  status: "draft" | "assigned" | "in_transit" | "delivered" | "exception" | "cancelled";
  created_at: string;
  updated_at: string;
  allowed_actions: string[];
};
export type SupplyAssignedDelivery = Omit<
  SupplyDeliveryTask,
  "driver_subject" | "source_kind" | "source_reference"
>;
