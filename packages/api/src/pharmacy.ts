import { z } from "zod";
import {
  pharmacyProductInputSchema,
  pharmacyProductFieldsSchema,
  pharmacyMovementInputSchema,
  pharmacyBatchInputSchema,
} from "./pharmacy-contract";
export { v7 as newPharmacyId } from "uuid";
export {
  pharmacyBatchInputSchema,
  pharmacyMovementInputSchema,
  pharmacyProductFieldsSchema,
  pharmacyLocationInputSchema,
} from "./pharmacy-contract";

export const pharmacyProductFormSchema = pharmacyProductInputSchema.superRefine((value, ctx) => {
  const names = new Set<string>();
  value.ingredients.forEach((ingredient, index) => {
    if (Number(ingredient.strength) <= 0)
      ctx.addIssue({
        code: "custom",
        path: ["ingredients", index, "strength"],
        message: "Enter a positive strength.",
      });
    const key = ingredient.name.toLowerCase();
    if (names.has(key))
      ctx.addIssue({
        code: "custom",
        path: ["ingredients", index, "name"],
        message: "Each ingredient must be unique.",
      });
    names.add(key);
  });
});
export type PharmacyProductInput = z.infer<typeof pharmacyProductInputSchema>;
export type PharmacyProduct = z.infer<typeof pharmacyProductFieldsSchema> & {
  id: string;
  outlet_id: string;
  version: number;
  regulatory_status: "pending_review";
  created_at: string;
  updated_at: string;
};
export type PharmacyLocation = {
  id: string;
  outlet_id: string;
  code: string;
  name: string;
  stock_status: "quarantine";
  created_at: string;
};
export type PharmacyBatch = z.infer<typeof pharmacyBatchInputSchema> & {
  id: string;
  outlet_id: string;
  product_version: number;
  created_at: string;
};
export type PharmacyMovement = z.infer<typeof pharmacyMovementInputSchema> & {
  id: string;
  outlet_id: string;
  actor_subject: string;
  created_at: string;
};
export type PharmacyRevision = {
  id: string;
  product_id: string;
  version: number;
  snapshot: PharmacyProduct;
  actor_subject: string;
  created_at: string;
};
export type PharmacyStockPosition = {
  id: string;
  created_at: string;
  batch_id: string;
  location_id: string;
  product_id: string;
  location_code: string;
  location_name: string;
  sku: string;
  brand_name: string;
  unit_of_issue: string;
  lot_number: string;
  expires_on: string;
  stock_status: "quarantine";
  expiry_status: "expired" | "unexpired";
  product_status: PharmacyProduct["status"];
  regulatory_status: "pending_review";
  on_hand_units: number;
  receipt_units: string;
  correction_units: string;
  dispensable_units: 0;
};
export type PharmacyStockReconciliation = {
  location_id: string;
  batch_id: string;
  from: string;
  as_of: string;
  opening_units: number;
  receipt_units: string;
  correction_units: string;
  closing_units: number;
  movement_count: number;
};
export type PharmacyScope = { tenantId: string; pharmacyId: string; outletId: string };
export function pharmacyRoot(scope: PharmacyScope) {
  return `/pharmacy/pharmacies/${z.string().uuid().parse(scope.pharmacyId)}/outlets/${z.string().uuid().parse(scope.outletId)}`;
}
export function pharmacyKey(scope: PharmacyScope, ...parts: unknown[]) {
  return ["pharmacy", scope.tenantId, scope.pharmacyId, scope.outletId, ...parts];
}

export {
  pharmacyOrganizationInputSchema,
  pharmacyOutletInputSchema,
  pharmacyAffiliationInputSchema,
} from "./pharmacy-contract";
export type PharmacyOrganization = {
  id: string;
  code: string;
  name: string;
  ownership_type: "independent" | "chain" | "hospital_owned";
  onboarding_status: "draft";
  created_at: string;
};
export type PharmacyOutlet = {
  id: string;
  pharmacy_id: string;
  code: string;
  name: string;
  country_code: string;
  timezone: string;
  regulatory_status: "pending_review";
  created_at: string;
};
export type PharmacyHospitalAffiliation = {
  id: string;
  pharmacy_id: string;
  hospital_id: string;
  relationship: "owner" | "serving" | "partner";
  verification_status: "pending_validation";
  created_at: string;
};

export { pharmacyPayableLineInputSchema, pharmacySettlementInputSchema } from "./pharmacy-contract";
import { pharmacyPayableInputSchema, pharmacySettlementInputSchema } from "./pharmacy-contract";
export const pharmacyPayableFormSchema = pharmacyPayableInputSchema.superRefine((value, ctx) => {
  if (value.due_on < value.issued_on)
    ctx.addIssue({
      code: "custom",
      path: ["due_on"],
      message: "Due date must be on or after the document date.",
    });
  let total = 0;
  value.lines.forEach((line, index) => {
    if (line.recoverable_tax_minor > line.tax_minor)
      ctx.addIssue({
        code: "custom",
        path: ["lines", index, "recoverable_tax_minor"],
        message: "Recoverable tax cannot exceed tax.",
      });
    if ((line.category === "inventory") !== !!line.receipt_id)
      ctx.addIssue({
        code: "custom",
        path: ["lines", index, "receipt_id"],
        message: "Inventory costs require a receipt; expenses must not link stock.",
      });
    const amount = line.net_minor + line.tax_minor + line.landed_minor;
    total += amount;
    if (amount <= 0)
      ctx.addIssue({
        code: "custom",
        path: ["lines", index, "net_minor"],
        message: "Enter a positive line amount.",
      });
  });
  if (total > 1000000000000)
    ctx.addIssue({
      code: "custom",
      path: ["lines"],
      message: "Document total exceeds the supported limit.",
    });
});
export type PharmacyPayableInput = z.infer<typeof pharmacyPayableInputSchema>;
export type PharmacyPayable = Omit<PharmacyPayableInput, "lines"> & {
  id: string;
  outlet_id: string;
  line_count: number;
  total_minor: number;
  paid_minor: number;
  credit_minor: number;
  outstanding_minor: number;
  actor_subject: string;
  created_at: string;
};
export type PharmacyPayableDetail = PharmacyPayable & { lines: PharmacyPayableInput["lines"] };
export type PharmacySettlementInput = z.infer<typeof pharmacySettlementInputSchema>;
export type PharmacySettlement = PharmacySettlementInput & {
  id: string;
  payable_id: string;
  actor_subject: string;
  created_at: string;
};
export type PharmacyInventoryAudit = {
  id: string;
  product_id: string;
  sku: string;
  brand_name: string;
  unit_of_issue: string;
  currency: string;
  lot_number: string;
  expires_on: string;
  created_at: string;
  on_hand_units: number;
  received_units: number;
  uncosted_receipt_units: number;
  acquisition_cost_minor: string;
  valuation_minor: string | null;
  valuation_status: "complete" | "no_receipts" | "cost_missing" | "valuation_review";
};
