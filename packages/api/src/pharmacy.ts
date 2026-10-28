import { z } from "zod";
import {
  pharmacyStorageAddressSchema,
  pharmacyLocationInputSchema,
  pharmacyProductInputSchema,
  pharmacyProductFieldsSchema,
  pharmacyMovementInputSchema,
  pharmacyBatchInputSchema,
  pharmacyStockExpiryFieldsSchema,
  pharmacyTaxRegistrationInputSchema,
  pharmacyTaxClassificationInputSchema,
} from "./pharmacy-contract";
export { v7 as newPharmacyId } from "uuid";
export {
  pharmacyBatchInputSchema,
  pharmacyMovementInputSchema,
  pharmacyProductFieldsSchema,
  pharmacyLocationInputSchema,
  pharmacyTaxRegistrationInputSchema,
  pharmacyTaxClassificationInputSchema,
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
export type PharmacySupplyNature = "taxable" | "nil_rated" | "exempt";
/** The classification in force on the outlet's date; null means "Not classified". */
export type PharmacyProductTax = {
  classification_code: string;
  supply_nature: PharmacySupplyNature;
  rate_ppm: number;
  effective_from: string;
};
export type PharmacyProduct = z.infer<typeof pharmacyProductFieldsSchema> & {
  id: string;
  outlet_id: string;
  version: number;
  regulatory_status: "pending_review";
  created_at: string;
  updated_at: string;
  tax?: PharmacyProductTax | null;
};
/** Effective-dated, append-only tax rows report whether they apply today. */
type TaxHistoryRow = {
  id: string;
  outlet_id: string;
  effective_from: string;
  reason: string | null;
  recorded_by: string;
  created_at: string;
  in_force: boolean;
  replaced: boolean;
};
export type PharmacyTaxRegistration = TaxHistoryRow & {
  jurisdiction_code: string;
  identifier_kind: string;
  identifier: string;
};
export type PharmacyTaxClassification = TaxHistoryRow & {
  product_id: string;
  classification_code: string;
  supply_nature: PharmacySupplyNature;
  rate_ppm: number;
  exemption_basis: string | null;
};
export type PharmacyTaxRegistrationInput = z.infer<typeof pharmacyTaxRegistrationInputSchema>;
export type PharmacyTaxClassificationInput = z.infer<typeof pharmacyTaxClassificationInputSchema>;
/** "5" for 50000 ppm, "0.25" for 2500; rates are stored in ppm, shown as percent. */
export function formatRatePercent(ratePPM: number): string {
  const hundredths = Math.round(ratePPM / 100);
  const whole = Math.trunc(hundredths / 100);
  const frac = String(hundredths % 100)
    .padStart(2, "0")
    .replace(/0+$/, "");
  return frac ? `${whole}.${frac}` : String(whole);
}
export type PharmacyLocation = z.infer<typeof pharmacyLocationInputSchema> & {
  id: string;
  outlet_id: string;
  stock_status: "quarantine" | "sellable";
  created_at: string;
};
export type PharmacyBatch = Omit<z.infer<typeof pharmacyBatchInputSchema>, "mrp_minor"> & {
  // Batches received before batch pricing (#1386 A3) have no MRP and can
  // never be released.
  mrp_minor: number | null;
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
export type PharmacyStockPosition = z.infer<typeof pharmacyStockExpiryFieldsSchema> & {
  id: string;
  created_at: string;
  batch_id: string;
  location_id: string;
  product_id: string;
  location_code: string;
  location_name: string;
  storage_address?: PharmacyLocation["storage_address"];
  sku: string;
  brand_name: string;
  unit_of_issue: string;
  lot_number: string;
  expires_on: string;
  stock_status: "quarantine" | "sellable";
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
// Counter sale (#1386 B-1).
export type PharmacyInvoiceProfile = {
  id: string;
  legal_name: string;
  address: string;
  invoice_prefix: string;
  recorded_by: string;
  created_at: string;
};
export type PharmacySaleLine = {
  line_number: number;
  product_id: string;
  batch_id: string;
  location_id: string;
  lot_number: string;
  expires_on: string;
  regulatory_class: string;
  classification_code: string;
  supply_nature: "taxable" | "nil_rated" | "exempt";
  rate_ppm: number;
  mrp_minor: number;
  pack_size: number;
  quantity_units: number;
  amount_minor: number;
  taxable_minor: number;
  cgst_minor: number;
  sgst_minor: number;
  /** The prescription line a G, H or H1 line dispenses (#1386 B-2b); null for OTC. */
  prescription_id: string | null;
  prescription_line: number | null;
};
export type PharmacySaleQuote = {
  currency: string;
  total_minor: number;
  taxable_minor: number;
  cgst_minor: number;
  sgst_minor: number;
  lines: PharmacySaleLine[];
  nppa_ceiling_checked: false;
};
export type PharmacySaleTender = {
  tender_number: number;
  kind: "cash" | "upi" | "card";
  amount_minor: number;
  tendered_minor: number | null;
  change_minor: number | null;
  reference: string | null;
};
export type PharmacySale = Omit<PharmacySaleQuote, "lines"> & {
  id: string;
  invoice_number: string;
  financial_year: string;
  sale_date: string;
  seller_legal_name: string;
  seller_address: string;
  seller_gstin: string;
  place_of_supply: string;
  actor_subject: string;
  buyer_name: string | null;
  buyer_phone: string | null;
  created_at: string;
  class_origin: "declared_by_outlet";
  lines: PharmacySaleLine[];
  tenders: PharmacySaleTender[];
};
export type PharmacySaleAvailability = {
  product_id: string;
  sellable_units: number;
  nearest_expiry: string | null;
  /** The approved schedule class in force; null when none is approved. */
  regulatory_class: "none" | "G" | "H" | "H1" | "X" | null;
};
export type PharmacyScope = { tenantId: string; pharmacyId: string; outletId: string };
export function pharmacyRoot(scope: PharmacyScope) {
  return `/pharmacy/pharmacies/${z.string().guid().parse(scope.pharmacyId)}/outlets/${z.string().guid().parse(scope.outletId)}`;
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

import { pharmacySafetyIncidentFieldsSchema } from "./pharmacy-contract";
export const pharmacySafetyIncidentInputSchema = pharmacySafetyIncidentFieldsSchema.superRefine(
  (value, ctx) => {
    if (
      (value.kind === "temperature_excursion") !==
      (value.measured_temperature_millicelsius != null)
    )
      ctx.addIssue({
        code: "custom",
        path: ["measured_temperature_millicelsius"],
        message: "Record a temperature only for a temperature excursion.",
      });
    const date = new Date(value.occurred_at);
    const fraction = value.occurred_at.match(/\.(\d+)/)?.[1] ?? "";
    if (
      date.getUTCFullYear() < 1 ||
      fraction.length > 6 ||
      !/(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$/.test(value.occurred_at)
    )
      ctx.addIssue({
        code: "custom",
        path: ["occurred_at"],
        message: "Use a date from year 1 with at most microsecond precision.",
      });
  },
);
export type PharmacySafetyIncidentInput = z.infer<typeof pharmacySafetyIncidentInputSchema>;
export type PharmacySafetyIncident = Omit<
  PharmacySafetyIncidentInput,
  "location_id" | "measured_temperature_millicelsius"
> & {
  location_id: string | null;
  measured_temperature_millicelsius: number | null;
  id: string;
  outlet_id: string;
  actor_subject: string;
  recorded_at: string;
};

export function formatPharmacyStorageAddress(
  address?: z.infer<typeof pharmacyStorageAddressSchema> | null,
) {
  const parts = (
    [
      ["Zone", address?.zone_code],
      ["Aisle", address?.aisle_code],
      ["Rack", address?.rack_code],
      ["Shelf", address?.shelf_code],
    ] as const
  )
    .filter(([, code]) => code)
    .map(([label, code]) => `${label} ${code}`);
  return parts.length ? parts.join(" · ") : "Address not recorded";
}
