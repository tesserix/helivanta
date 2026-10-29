import { pharmacyRoot, type PharmacyScope } from "./pharmacy";

/** One outlet day and currency with sales or credit notes (#1386 C-3, #1060). */
export type PharmacyDailyTotals = {
  day: string;
  currency: string;
  sales: number;
  sales_minor: number;
  sales_taxable_minor: number;
  sales_cgst_minor: number;
  sales_sgst_minor: number;
  discount_minor: number;
  units_sold: number;
  credit_notes: number;
  credits_minor: number;
  credits_taxable_minor: number;
  credits_cgst_minor: number;
  credits_sgst_minor: number;
  units_returned: number;
  net_minor: number;
  cash_minor: number;
  upi_minor: number;
  /** UPI the system saw settle, apart from typed references (#1386 C-4b). */
  upi_verified_minor: number;
  card_minor: number;
  cash_refund_minor: number;
  upi_refund_minor: number;
  card_refund_minor: number;
  /** Billed to an admission account, and returned to it (#1387): not collected or paid out. */
  admission_account_minor: number;
  admission_account_refund_minor: number;
};

export type PharmacyDailyReport = { from: string; to: string; days: PharmacyDailyTotals[] };

export function pharmacyDailyReportPath(scope: PharmacyScope, from: string, to: string) {
  return `${pharmacyRoot(scope)}/reports/daily?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`;
}
