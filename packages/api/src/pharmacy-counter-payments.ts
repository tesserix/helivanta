import { pharmacyRoot, type PharmacyScope } from "./pharmacy";

/**
 * A UPI payment the counter asked for (#1386 C-4a/C-4b). Paid means
 * payments fetched a captured payment for this amount from Razorpay;
 * nothing the payer shows the cashier makes it paid.
 */
export type PharmacyCounterPayment = {
  id: string;
  purpose: "counter_sale";
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
};

export const pharmacyCounterPaymentPath = (scope: PharmacyScope, id: string) =>
  `${pharmacyRoot(scope)}/counter-payments/${id}`;

/** Paid counter payments not yet on a sale. */
export const pharmacyUnusedCounterPaymentsPath = (scope: PharmacyScope) =>
  `${pharmacyRoot(scope)}/counter-payments`;
