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

export function pharmacyItemSalesPath(scope: PharmacyScope, from: string, to: string) {
  return `${pharmacyRoot(scope)}/reports/items?${period(from, to)}`;
}

export function pharmacyReceiptsPath(scope: PharmacyScope, from: string, to: string) {
  return `${pharmacyRoot(scope)}/reports/receipts?${period(from, to)}`;
}
