import { z } from "zod";
import { readAttachmentFile, sha256Hex } from "./attachment-file";
import { apiFetchResponse } from "./client";
import { bedInputSchema, type BedClass } from "./medicore-beds";

// Bed-day charges (#197 slice 2): a hospital's timezone, set once, and its
// daily rates per bed class, before tax.

export type BedCharging = {
  hospital_id: string;
  timezone: string;
  created_by: string;
  created_at: string;
};

export type BedRate = {
  id: string;
  hospital_id: string;
  bed_class: BedClass;
  effective_from: string;
  daily_rate_minor: number;
  currency: string;
  created_by: string;
  created_at: string;
};

export const bedChargingSchema = z.object({
  timezone: z
    .string()
    .trim()
    .min(1, "Enter the hospital's timezone, such as Asia/Kolkata")
    .max(64, "At most 64 characters"),
});

export const bedRateSchema = z.object({
  bed_class: bedInputSchema.shape.bed_class,
  effective_from: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, "Choose the date the rate starts"),
  daily_rate_minor: z
    .number({ message: "Enter the daily rate" })
    .int("Enter the daily rate")
    .min(1, "A rate is more than zero")
    .max(1_000_000_000, "At most 1,00,00,000 a day"),
  currency: z.string().regex(/^[A-Z]{3}$/, "Choose the currency"),
});

// Interim bills (#197 slice 3): the admission account frozen as issued,
// numbered within the admission. A statement, not a tax invoice.

export type InterimBill = {
  id: string;
  admission_id: string;
  hospital_id: string;
  patient_id: string;
  number: number;
  /** One series per admission: interim while the stay runs, final from discharge. */
  kind: "interim" | "final";
  issued_by: string;
  issued_at: string;
  /** Bed days without a rate when issued: no amount, never zero. */
  unpriced_bed_days: number;
  /** INR paid and deposits held when issued (#199); null on older bills. */
  paid_minor: number | null;
  deposit_held_minor: number | null;
  line_count: number;
  new_count: number;
  /** Only on the bill itself; the list leaves them empty. */
  totals: {
    currency: string;
    charged_minor: number;
    reversed_minor: number;
    outstanding_minor: number;
  }[];
  sources: { source_module: string; currency: string; outstanding_minor: number }[];
};

export type InterimBillLine = {
  id: string;
  kind: "charge" | "reversal";
  source_module: string;
  source_document: string;
  amount_minor: number;
  currency: string;
  occurred_at: string;
  recorded_at: string;
  /** Not on the previous bill. */
  new: boolean;
};

// Cash counters (#198 slice 1): a cashier's shift on a counter, closed blind
// against the cash it took, and cash receipts against an admission's account.

export type CashCounter = {
  id: string;
  hospital_id: string;
  code: string;
  name: string;
  created_by: string;
  created_at: string;
};

export type CashShift = {
  id: string;
  hospital_id: string;
  counter_id: string;
  counter_code: string;
  cashier: string;
  currency: string;
  opening_float_minor: number;
  opened_at: string;
  status: "open" | "pending_authorisation" | "closed";
  /** Known only once the shift is closed: the close is blind. */
  expected_minor: number | null;
  declared_minor: number | null;
  variance_minor: number | null;
  closed_at: string | null;
  authorised_by: string | null;
  authorisation_reason: string | null;
  authorised_at: string | null;
};

export type CashReceipt = {
  id: string;
  hospital_id: string;
  shift_id: string;
  counter_id: string;
  counter_code: string;
  number: number;
  cashier: string;
  admission_id: string;
  purpose: "payment" | "deposit" | "refund";
  /** UPI is confirmed by the provider and never in the drawer (#211). */
  mode: "cash" | "upi";
  /** The UPI reference, and whether it landed after its QR closed. */
  rrn: string | null;
  late: boolean;
  approval_id: string | null;
  amount_minor: number;
  currency: string;
  received_at: string;
  void_reason: string | null;
  voided_at: string | null;
};

export const counterSchema = z.object({
  code: z
    .string()
    .trim()
    .toUpperCase()
    .regex(/^[A-Z0-9][A-Z0-9-]{0,19}$/, "Up to 20 letters, digits and dashes"),
  name: z.string().trim().min(1, "Name the counter").max(100, "At most 100 characters"),
});

const cashAmount = (message: string) =>
  z.number({ message }).int(message).min(0, message).max(1_000_000_000, "Too large");

export const openShiftSchema = z.object({
  counter_id: z.string().min(1, "Choose a counter"),
  opening_float_minor: cashAmount("Enter the cash in the drawer"),
});

export const closeShiftSchema = z.object({
  declared_minor: cashAmount("Enter the cash you counted"),
});

export const cashReceiptSchema = z.object({
  amount_minor: cashAmount("Enter the amount received").refine(
    (v) => v >= 1,
    "Enter the amount received",
  ),
});

export const cashReasonSchema = z.object({
  reason: z.string().trim().min(1, "Give a reason").max(500, "At most 500 characters"),
});

// Refunds of unused deposits (#199 slice 2): above the hospital's threshold a
// supervisor approves the amount first; the cashier pays it from the drawer.

export type RefundPolicy = {
  hospital_id: string;
  threshold_minor: number;
  currency: string;
  /** Null while the default (₹10,000) applies. */
  created_by: string | null;
  created_at: string | null;
};

export type RefundApproval = {
  id: string;
  admission_id: string;
  amount_minor: number;
  currency: string;
  reason: string;
  approved_by: string;
  approved_at: string;
  /** The refund receipt that used it, once paid. */
  used_by: string | null;
};

export const refundPolicySchema = z.object({
  threshold_minor: z
    .number({ message: "Enter the threshold" })
    .int("Enter the threshold")
    .min(0, "Enter the threshold")
    .max(1_000_000_000, "Too large"),
});

export const refundApprovalSchema = z.object({
  amount_minor: z
    .number({ message: "Enter the amount" })
    .int("Enter the amount")
    .min(1, "Enter the amount")
    .max(1_000_000_000, "Too large"),
  reason: z.string().trim().min(1, "Give a reason").max(500, "At most 500 characters"),
});

/** An admission account as the billing desk lists it (INR figures). */
export type DeskAccount = {
  admission_id: string;
  hospital_id: string;
  patient_id: string;
  status: "open" | "closed";
  discharge_ordered: boolean;
  opened_at: string | null;
  closed_at: string | null;
  created_at: string;
  /** Left to pay; below zero is credit. */
  balance_minor: number;
  deposit_held_minor: number;
  /** Refundable now: the surplus over the bill, once a final bill exists. */
  refundable_minor: number;
  has_final_bill: boolean;
};

export type DeskView = "" | "open" | "discharge_ordered" | "to_collect" | "to_refund" | "released";

/** One shift on the daily collection report. Money is null while the shift is open: it closes blind. */
export type CollectionShift = {
  shift_id: string;
  counter_code: string;
  counter_name: string;
  cashier: string;
  status: "open" | "pending_authorisation" | "closed";
  opened_at: string;
  closed_at: string | null;
  opening_float_minor: number | null;
  payments_minor: number | null;
  deposits_minor: number | null;
  refunds_minor: number | null;
  voided_count: number | null;
  voided_minor: number | null;
  /** Refunds voided: cash that never left the drawer, apart from voided takings. */
  voided_refunds_minor: number | null;
  expected_minor: number | null;
  declared_minor: number | null;
  variance_minor: number | null;
  authorised_by: string | null;
  authorisation_reason: string | null;
  /** UPI taken in the shift: at the bank, apart from the cash columns. */
  upi_payments_minor: number | null;
  upi_deposits_minor: number | null;
};

/** A hospital-local day's cash (#198 slice 2); totals cover counted shifts only. */
export type DailyCollection = {
  hospital_id: string;
  date: string;
  timezone: string;
  currency: string;
  /** A past day with every shift closed. */
  final: boolean;
  shifts: CollectionShift[];
  totals: {
    payments_minor: number;
    deposits_minor: number;
    refunds_minor: number;
    net_minor: number;
    shortage_minor: number;
    excess_minor: number;
    counted_shifts: number;
    open_shifts: number;
    pending_shifts: number;
    voided_receipts: number;
    voided_minor: number;
    voided_refunds_minor: number;
    upi_payments_minor: number;
    upi_deposits_minor: number;
  };
};

export const collectionDateSchema = z.object({
  date: z.string().min(1, "Choose a date").date("Use a valid date"),
});

export const dailyCollectionPath = (hospitalId: string, date: string) =>
  `/billing/hospitals/${hospitalId}/collections/${date}`;

// The row count is reported only when the bytes match the digest the export was audited with.
export async function fetchDailyCollectionCSV(
  hospitalId: string,
  date: string,
  signal: AbortSignal,
): Promise<{ blob: Blob; filename: string; rows: number }> {
  signal.throwIfAborted();
  collectionDateSchema.parse({ date });
  const response = await apiFetchResponse(`${dailyCollectionPath(hospitalId, date)}/export.csv`, {
    cache: "no-store",
    signal,
  });
  const filename = `billing-collection-${hospitalId}-${date}.csv`;
  const rows = response.headers.get("X-Export-Row-Count") ?? "";
  const digest = response.headers.get("X-Export-Sha256") ?? "";
  if (
    response.headers.get("Content-Disposition") !== `attachment; filename="${filename}"` ||
    !/^text\/csv(?:;|$)/i.test(response.headers.get("Content-Type") ?? "") ||
    !/^(?:0|[1-9]\d{0,2})$/.test(rows) ||
    !/^[0-9a-f]{64}$/.test(digest)
  )
    throw new Error("The server did not return a valid collection file. Try again.");
  const blob = await response.blob();
  const actual = await sha256Hex(await readAttachmentFile(blob, signal));
  signal.throwIfAborted();
  if (actual !== digest) throw new Error("The collection file could not be verified. Try again.");
  return { blob, filename, rows: Number(rows) };
}

/** A hospital's GST registration (#193): set once; rooms are taxed from its date once applied. */
export type GstRegistration = {
  hospital_id: string;
  gstin: string;
  legal_name: string;
  address: string;
  effective_from: string;
  created_by: string;
  created_at: string;
  /** When the census began taxing from it; null until then. */
  applied_at: string | null;
};

// The pattern only: the server also checks the state code and check character.
export const gstRegistrationSchema = z.object({
  gstin: z
    .string()
    .trim()
    .toUpperCase()
    .regex(/^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$/, "Enter the 15-character GSTIN"),
  legal_name: z.string().trim().min(1, "Enter the legal name").max(200, "At most 200 characters"),
  address: z.string().trim().min(1, "Enter the address").max(500, "At most 500 characters"),
  effective_from: z.string().date("Choose the date it takes effect"),
});

/** An invoice-cum-bill of supply for an admission's rooms (INR paise). */
export type TaxInvoice = {
  id: string;
  hospital_id: string;
  admission_id: string;
  number: string;
  issue_date: string;
  issued_at: string;
  issued_by: string;
  gstin: string;
  supplier_name: string;
  supplier_address: string;
  state_code: string;
  recipient_name: string;
  recipient_address: string;
  lines: number;
  value_minor: number;
  taxable_minor: number;
  exempt_minor: number;
  cgst_minor: number;
  sgst_minor: number;
  total_minor: number;
  /** Reversed lines awaiting a credit note. */
  creditable_lines: number;
  currency: string;
};

/**
 * A credit note for an invoice's reversed room days (CGST Act s.34): its lines
 * copy the invoice's, so what is credited is what was invoiced.
 */
export type CreditNote = Omit<TaxInvoice, "creditable_lines"> & {
  invoice_id: string;
  invoice_number: string;
  invoice_date: string;
};

export type TaxInvoiceLine = {
  entry_id: string;
  census_date: string;
  bed_class: BedClass;
  sac: string;
  policy_version: string;
  value_minor: number;
  taxable: boolean;
  cgst_bp: number;
  sgst_bp: number;
  cgst_minor: number;
  sgst_minor: number;
};

export const taxInvoiceSchema = z.object({
  recipient_name: z
    .string()
    .trim()
    .min(1, "Enter the name on the invoice")
    .max(200, "At most 200 characters"),
  recipient_address: z
    .string()
    .trim()
    .min(1, "Enter the address")
    .max(500, "At most 500 characters"),
});

/**
 * A UPI payment or deposit the billing desk asked for (#198, #211). Paid means
 * payments fetched a captured payment for this amount from Razorpay, and the
 * receipt was recorded; nothing the payer shows the cashier makes it paid.
 */
export type BillingUpiRequest = {
  id: string;
  hospital_id: string;
  shift_id: string;
  counter_id: string;
  counter_code: string;
  cashier: string;
  admission_id: string;
  purpose: "payment" | "deposit";
  amount_minor: number;
  currency: string;
  status:
    "requested" | "awaiting_payment" | "cancelling" | "paid" | "failed" | "expired" | "cancelled";
  qr_image_url: string | null;
  expires_at: string | null;
  failure_reason: "provider_not_configured" | "provider_unavailable" | "provider_rejected" | null;
  provider_payment_id: string | null;
  rrn: string | null;
  paid_at: string | null;
  late: boolean;
  created_at: string;
  /** The receipt a paid request made, on its counter. */
  receipt_number: number | null;
};

export const billingUpiRequestPath = (hospitalId: string, id: string) =>
  `/billing/hospitals/${hospitalId}/upi-requests/${id}`;
