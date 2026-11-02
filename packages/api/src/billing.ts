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
  /** Which of card, netbanking and wallet it takes now (#198 slice 6). */
  modes: SwitchedMode[];
};

/** The modes a counter is set up to take; cash and UPI are not switched. */
export type SwitchedMode = "card" | "netbanking" | "wallet";

export const switchedModes: { mode: SwitchedMode; label: string }[] = [
  { mode: "card", label: "Card" },
  { mode: "netbanking", label: "Netbanking" },
  { mode: "wallet", label: "Wallet" },
];

export type CashShift = {
  id: string;
  hospital_id: string;
  counter_id: string;
  counter_code: string;
  cashier: string;
  currency: string;
  opening_float_minor: number;
  opened_at: string;
  /** handing_over: offered to another cashier, taking nothing meanwhile. */
  status: "open" | "handing_over" | "pending_authorisation" | "closed";
  /** Known only once the shift is closed: the close is blind. */
  expected_minor: number | null;
  declared_minor: number | null;
  variance_minor: number | null;
  closed_at: string | null;
  authorised_by: string | null;
  authorisation_reason: string | null;
  authorised_at: string | null;
  /** The close's count by note and coin; null before counting by note. */
  declared_counts: CashCount | null;
  /** Cash taken to the safe less corrections: the cashier handed it over. */
  picked_up_minor: number;
  /** The handover the shift opened from, and whether a handover closed it. */
  handover_id: string | null;
  handed_over: boolean;
  /** The handover in progress, if any. */
  live_handover_id: string | null;
  live_handover_to: string | null;
  live_handover_status: "offered" | "disputed" | null;
  /** Which of card, netbanking and wallet its counter takes now. */
  counter_modes: SwitchedMode[];
};

/**
 * A drawer passed between cashiers (#198 slice 5). Neither cashier's count is
 * ever shown: each counts blind, and only the agreed count appears, once
 * there is one.
 */
export type CashHandover = {
  id: string;
  hospital_id: string;
  from_shift_id: string;
  counter_id: string;
  counter_code: string;
  from_cashier: string;
  to_cashier: string;
  currency: string;
  status: "offered" | "withdrawn" | "disputed" | "accepted" | "settled";
  to_shift_id: string | null;
  agreed_counts: CashCount | null;
  agreed_minor: number | null;
  settled_by: string | null;
  settlement_reason: string | null;
  offered_at: string;
  decided_at: string | null;
  settled_at: string | null;
};

/** A count by note and coin, keyed `note:<paise>` or `coin:<paise>` (#198). */
export type CashCount = Record<string, number>;

/** A note or coin a count may hold; the API owns the list. */
export type CashDenomination = { key: string; kind: "note" | "coin"; value_minor: number };

/** Cash taken from an open drawer to the safe, or a correction of one. */
export type CashPickup = {
  id: string;
  hospital_id: string;
  shift_id: string;
  counter_id: string;
  kind: "pickup" | "correction";
  amount_minor: number;
  currency: string;
  counts: CashCount | null;
  corrects_id: string | null;
  reason: string | null;
  recorded_by: string;
  recorded_at: string;
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
  /**
   * UPI is confirmed by the provider (#211); card, netbanking and wallet are
   * recorded with their reference (#198 slice 6). None is in the drawer.
   */
  mode: "cash" | "upi" | SwitchedMode;
  reference: string | null;
  /** Card only: the slip's approval code. No card digits are kept. */
  approval_code: string | null;
  /** Wallet only: the wallet's name, lower case. */
  wallet: string | null;
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

const countEntries = z.record(
  z.string(),
  z
    .string()
    .trim()
    .regex(/^\d*$/, "Whole numbers only")
    .refine((v) => v === "" || Number(v) <= 1_000_000, "At most 1,000,000"),
);

/**
 * A close as typed: blanks are none. An empty drawer is said, never inferred
 * from an untouched form: a close is final and a zero count is a shortage.
 */
export const cashCountSchema = z
  .object({ counts: countEntries, empty: z.boolean() })
  .refine((v) => v.empty || Object.values(v.counts).some((n) => Number(n) > 0), {
    message: "Count the notes and coins, or confirm the drawer is empty",
    path: ["counts"],
  });

/** A pickup takes at least one note or coin. */
export const pickupCountSchema = z.object({
  counts: countEntries.refine(
    (c) => Object.values(c).some((v) => Number(v) > 0),
    "Count at least one note or coin",
  ),
  // The same form as a close; a pickup is never of an empty drawer.
  empty: z.literal(false),
});

/** The typed count as the API takes it: whole numbers, zeros dropped. */
export function countFromForm(counts: Record<string, string>): CashCount {
  return Object.fromEntries(
    Object.entries(counts)
      .map(([key, v]) => [key, Number(v || 0)] as const)
      .filter(([, n]) => n > 0),
  );
}

/** Whom a drawer is handed to: their user ID, as shifts name their cashier. */
export const handoverToSchema = z.object({
  to: z
    .string()
    .trim()
    .min(1, "Enter the incoming cashier's user ID")
    .max(200, "At most 200 characters"),
});

export const pickupCorrectionSchema = z.object({
  amount_minor: cashAmount("Enter the amount put back").refine(
    (v) => v >= 1,
    "Enter the amount put back",
  ),
  reason: z.string().trim().min(1, "Give a reason").max(500, "At most 500 characters"),
});

export const cashReceiptSchema = z.object({
  amount_minor: cashAmount("Enter the amount received").refine(
    (v) => v >= 1,
    "Enter the amount received",
  ),
});

/** A payment or deposit the terminal or bank already confirmed. */
export const tenderSchema = z
  .object({
    amount_minor: cashAmount("Enter the amount received").refine(
      (v) => v >= 1,
      "Enter the amount received",
    ),
    mode: z.enum(["card", "netbanking", "wallet"], { message: "Choose how it was paid" }),
    reference: z
      .string()
      .trim()
      .toUpperCase()
      .regex(/^[A-Z0-9][A-Z0-9/-]{3,39}$/, "4 to 40 letters, digits, dashes or slashes"),
    approval_code: z.string().trim().toUpperCase(),
    wallet: z.string().trim().toLowerCase(),
  })
  .superRefine((v, ctx) => {
    if (v.mode === "card" && !/^[A-Z0-9]{6}$/.test(v.approval_code))
      ctx.addIssue({
        code: "custom",
        path: ["approval_code"],
        message: "The 6-character code on the slip",
      });
    if (v.mode === "wallet" && !/^[a-z0-9]([a-z0-9 .-]{0,28}[a-z0-9])?$/.test(v.wallet))
      ctx.addIssue({
        code: "custom",
        path: ["wallet"],
        message: "Name the wallet, up to 30 characters",
      });
  });

/** The tender as the API takes it: only the fields its mode carries. */
export function tenderBody(v: z.output<typeof tenderSchema>) {
  return {
    amount_minor: v.amount_minor,
    currency: "INR",
    mode: v.mode,
    reference: v.reference,
    ...(v.mode === "card" ? { approval_code: v.approval_code } : {}),
    ...(v.mode === "wallet" ? { wallet: v.wallet } : {}),
  };
}

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
  /** Cash taken to the safe less corrections: a movement, not revenue. */
  picked_up_minor: number | null;
  /** Card, netbanking and wallet: at the bank, apart from the cash columns. */
  card_payments_minor: number | null;
  card_deposits_minor: number | null;
  netbanking_payments_minor: number | null;
  netbanking_deposits_minor: number | null;
  wallet_payments_minor: number | null;
  wallet_deposits_minor: number | null;
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
    picked_up_minor: number;
    card_payments_minor: number;
    card_deposits_minor: number;
    netbanking_payments_minor: number;
    netbanking_deposits_minor: number;
    wallet_payments_minor: number;
    wallet_deposits_minor: number;
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
  /** Discounts taken off the room charges before their value (#200). */
  discount_minor: number;
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
export type CreditNote = Omit<TaxInvoice, "creditable_lines" | "discount_minor"> & {
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
  /** After the line's discount (#200; CGST Rule 46(1)(i)). */
  value_minor: number;
  discount_minor: number;
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

// Discounts on room charges (#200 slice 1): a hospital's reasons carry what
// the desk may give alone and the most anyone may approve, per admission.

export type DiscountReason = {
  id: string;
  hospital_id: string;
  code: string;
  name: string;
  desk_percent_bp: number;
  desk_max_minor: number;
  approval_percent_bp: number;
  approval_max_minor: number;
  currency: string;
  created_by: string;
  created_at: string;
  retired_by: string | null;
  retired_at: string | null;
};

export type Discount = {
  id: string;
  admission_id: string;
  patient_id: string;
  reason_id: string;
  reason_code: string;
  reason_name: string;
  amount_minor: number;
  currency: string;
  justification: string;
  status: "pending" | "applied" | "rejected";
  requested_by: string;
  requested_at: string;
  decided_by: string | null;
  decided_at: string | null;
  decision_note: string | null;
  applied_at: string | null;
  /** What it takes off now, less withdrawals from days later voided. */
  net_minor: number;
};

// Typed as text, so an empty field is missing rather than zero.
const percent = (what: string) =>
  z
    .string()
    .trim()
    .min(1, `Enter the ${what} percentage`)
    .regex(/^\d{1,3}(\.\d{1,2})?$/, "0 to 100, at most two decimals")
    .transform(Number)
    .refine((v) => v <= 100, "0 to 100");
const rupees = (what: string) =>
  z
    .number({ message: `Enter the ${what} amount` })
    .int(`Enter the ${what} amount`)
    .min(0, `Enter the ${what} amount`)
    .max(1_000_000_000, "Too large");

export const discountReasonSchema = z
  .object({
    code: z
      .string()
      .trim()
      .regex(/^[a-z0-9_-]{1,32}$/, "Lower-case letters, digits, _ or -, up to 32"),
    name: z.string().trim().min(1, "Enter a name").max(100, "At most 100 characters"),
    desk_percent: percent("desk"),
    desk_max_minor: rupees("desk"),
    approval_percent: percent("approval"),
    approval_max_minor: rupees("approval"),
  })
  .refine((v) => v.desk_percent <= v.approval_percent, {
    path: ["desk_percent"],
    message: "Not above the approval percentage",
  })
  .refine((v) => v.desk_max_minor <= v.approval_max_minor, {
    path: ["desk_max_minor"],
    message: "Not above the approval amount",
  });

export const discountSchema = z.object({
  reason_id: z.string().min(1, "Choose a reason"),
  amount_minor: z
    .number({ message: "Enter the amount" })
    .int("Enter the amount")
    .min(1, "Enter the amount")
    .max(1_000_000_000, "Too large"),
  justification: z
    .string()
    .trim()
    .min(1, "Say why the discount is given")
    .max(500, "At most 500 characters"),
});

export const discountDecisionSchema = z.object({
  note: z.string().trim().min(1, "Add a note").max(500, "At most 500 characters"),
});
