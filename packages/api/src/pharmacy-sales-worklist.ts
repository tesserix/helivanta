import { pharmacyRoot, type PharmacyScope } from "./pharmacy";

/** Who a sale was for, as a kind only (#987); the invoice read names the person. */
export type PharmacySaleCustomer = "walk_in" | "patient" | "admission";

/** What a sale is linked to, as a kind only (#987 slice 2). */
export type PharmacySaleLinkage = "admission" | "opd" | "none";

/** An OPD visit a registered patient's sale may cite (#987 slice 2). */
export type PharmacyOPDEncounter = {
  encounter_id: string;
  status: "arrived" | "in_consultation" | "closed";
  /** When MediCore opened it: what tells a patient's visits apart. */
  opened_at: string;
};

/** One sale in the worklist (#987). */
export type PharmacySaleWorkRow = {
  id: string;
  posted_at: string;
  invoice_number: string;
  sale_date: string;
  customer: PharmacySaleCustomer;
  admission_bed_code: string | null;
  linkage: PharmacySaleLinkage;
  currency: string;
  total_minor: number;
  discount_minor: number;
  units: number;
  /** Credit notes posted so far. */
  credited_minor: number;
  net_minor: number;
  /** Billed to the admission account less reversed; zero unless an admission sale. */
  admission_outstanding_minor: number;
};

/** One sale's reconciled statement (#987). */
export type PharmacySaleSummary = {
  id: string;
  invoice_number: string;
  sale_date: string;
  customer: PharmacySaleCustomer;
  admission_bed_code: string | null;
  linkage: PharmacySaleLinkage;
  currency: string;
  total_minor: number;
  taxable_minor: number;
  cgst_minor: number;
  sgst_minor: number;
  discount_minor: number;
  lines: {
    line_number: number;
    product_id: string;
    sku: string;
    brand_name: string;
    lot_number: string;
    quantity_units: number;
    returned_units: number;
    discount_minor: number;
    amount_minor: number;
    credited_minor: number;
  }[];
  charges: {
    charge_number: number;
    code: string;
    name: string;
    amount_minor: number;
    credited_minor: number;
  }[];
  /** Amount by tender kind. */
  payments: Record<string, number>;
  credit_notes: {
    id: string;
    credit_note_number: string;
    credit_note_date: string;
    total_minor: number;
    refunds: Record<string, number>;
  }[];
  /** More credit notes than are listed; the totals count them all. */
  more_credit_notes: boolean;
  credited_minor: number;
  net_minor: number;
  collected_minor: number;
  refunded_minor: number;
  admission_billed_minor: number;
  admission_reversed_minor: number;
};

export type PharmacySalesWorklistFilter = ({ invoice: string } | { from: string; to: string }) & {
  customer?: PharmacySaleCustomer;
  linkage?: PharmacySaleLinkage;
};

export function pharmacySalesWorklistPath(scope: PharmacyScope, f: PharmacySalesWorklistFilter) {
  const q = new URLSearchParams(
    "invoice" in f ? { invoice: f.invoice } : { from: f.from, to: f.to },
  );
  if (f.customer) q.set("customer", f.customer);
  if (f.linkage) q.set("linkage", f.linkage);
  return `${pharmacyRoot(scope)}/sales/worklist?${q}`;
}

export function pharmacySaleSummaryPath(scope: PharmacyScope, id: string) {
  return `${pharmacyRoot(scope)}/sales/${encodeURIComponent(id)}/summary`;
}

export function pharmacyOPDEncountersPath(
  scope: PharmacyScope,
  patientId: string,
  hospitalId: string,
) {
  const q = new URLSearchParams({ patient_id: patientId, hospital_id: hospitalId });
  return `${pharmacyRoot(scope)}/opd-encounters?${q}`;
}
