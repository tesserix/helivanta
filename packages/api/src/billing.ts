import { z } from "zod";
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
  purpose: "payment" | "deposit";
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
