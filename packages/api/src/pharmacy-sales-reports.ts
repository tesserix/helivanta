import { pharmacyRoot, type PharmacyScope } from "./pharmacy";

/** One product's sales and returns over a period (#1060). */
export type PharmacyItemSales = {
  product_id: string;
  sku: string;
  brand_name: string;
  generic_name: string;
  manufacturer: string;
  pack_size: number;
  unit_of_issue: string;
  currency: string;
  units_sold: number;
  /** Charged, after any discount. */
  sales_minor: number;
  discount_minor: number;
  sales_taxable_minor: number;
  sales_cgst_minor: number;
  sales_sgst_minor: number;
  /** Lowest and highest MRP per pack charged; null when nothing was sold. */
  mrp_low_minor: number | null;
  mrp_high_minor: number | null;
  units_returned: number;
  credits_minor: number;
  credits_taxable_minor: number;
  credits_cgst_minor: number;
  credits_sgst_minor: number;
  net_units: number;
  net_minor: number;
};

export type PharmacyReceiptKind = "cash" | "upi" | "upi_verified" | "card" | "admission_account";

/** One tender on a sale or refund on a credit note (#1060). */
export type PharmacyReceipt = {
  id: string;
  posted_at: string;
  day: string;
  direction: "receipt" | "refund";
  document_id: string;
  document_number: string;
  kind: PharmacyReceiptKind;
  reference: string | null;
  amount_minor: number;
  currency: string;
};

const period = (from: string, to: string) =>
  `from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`;

/**
 * An item category, and optionally one of its subcategories, to cut sales by
 * item and sales and stock by (#988). A sale counts under the category in
 * force on its sale date; sales and stock lists products by today's.
 */
export type PharmacyReportCategory = { categoryId?: string; subcategoryId?: string };

const byCategory = ({ categoryId, subcategoryId }: PharmacyReportCategory = {}) =>
  (categoryId ? `&category_id=${encodeURIComponent(categoryId)}` : "") +
  (categoryId && subcategoryId ? `&subcategory_id=${encodeURIComponent(subcategoryId)}` : "");

/** A prescriber, as written on prescriptions: a registration is unique only within its council. */
export type PharmacyPrescriber = {
  prescriber_name: string;
  prescriber_registration: string;
  prescriber_council: string;
};

/**
 * What else sales by item is cut by (#1060): one lot, matched in any case,
 * and one prescriber, which needs prescription read and is audited.
 */
export type PharmacyItemSalesLines = {
  lotNumber?: string;
  prescriber?: Pick<PharmacyPrescriber, "prescriber_registration" | "prescriber_council">;
};

const byLines = ({ lotNumber, prescriber }: PharmacyItemSalesLines = {}) =>
  (lotNumber ? `&lot_number=${encodeURIComponent(lotNumber)}` : "") +
  (prescriber
    ? `&prescriber_registration=${encodeURIComponent(prescriber.prescriber_registration)}` +
      `&prescriber_council=${encodeURIComponent(prescriber.prescriber_council)}`
    : "");

export function pharmacyItemSalesPath(
  scope: PharmacyScope,
  from: string,
  to: string,
  category?: PharmacyReportCategory,
  lines?: PharmacyItemSalesLines,
) {
  return `${pharmacyRoot(scope)}/reports/items?${period(from, to)}${byCategory(category)}${byLines(lines)}`;
}

/** The prescribers on prescriptions sold against in the period: at most 200, and `more` beyond. */
export type PharmacyPrescribers = { data: PharmacyPrescriber[]; more: boolean };

export function pharmacyPrescribersPath(scope: PharmacyScope, from: string, to: string) {
  return `${pharmacyRoot(scope)}/reports/prescribers?${period(from, to)}`;
}

export function pharmacyReceiptsPath(scope: PharmacyScope, from: string, to: string) {
  return `${pharmacyRoot(scope)}/reports/receipts?${period(from, to)}`;
}

/** One line of a sale to a registered patient (#1060); every read is audited. */
export type PharmacyPatientSaleLine = {
  id: string;
  sale_date: string;
  sale_id: string;
  invoice_number: string;
  line_number: number;
  /** The bed, when the sale was billed to an admission. */
  admission_bed_code: string | null;
  product_id: string;
  sku: string;
  brand_name: string;
  generic_name: string;
  pack_size: number;
  unit_of_issue: string;
  batch_id: string;
  lot_number: string;
  expires_on: string;
  quantity_units: number;
  mrp_minor: number;
  discount_minor: number;
  amount_minor: number;
  currency: string;
  /** Credited back on notes dated up to the period's end. */
  returned_units: number;
};

/** A product's units sold and returned over a period, beside its stock now (#1060). */
export type PharmacyStockSales = {
  product_id: string;
  sku: string;
  brand_name: string;
  generic_name: string;
  pack_size: number;
  unit_of_issue: string;
  units_sold: number;
  units_returned: number;
  on_hand_units: number;
  sellable_units: number;
};

export function pharmacyPatientSalesPath(
  scope: PharmacyScope,
  patientId: string,
  from: string,
  to: string,
) {
  return `${pharmacyRoot(scope)}/reports/patient-sales?patient_id=${encodeURIComponent(patientId)}&${period(from, to)}`;
}

export function pharmacyStockSalesPath(
  scope: PharmacyScope,
  from: string,
  to: string,
  category?: PharmacyReportCategory,
) {
  return `${pharmacyRoot(scope)}/reports/stock-sales?${period(from, to)}${byCategory(category)}`;
}
