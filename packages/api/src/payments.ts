/**
 * The tenant's Razorpay account (#1386 C-4a). Secrets never come back:
 * only whether each one is set.
 */
export type PaymentAccount = {
  configured: boolean;
  mode?: "test" | "live";
  key_id?: string;
  key_secret_set: boolean;
  webhook_secret_set: boolean;
  updated_by?: string;
  updated_at?: string;
  webhook_path?: string;
  /** Absent where this deployment exposes no public webhook origin. */
  webhook_url?: string;
};

export type PaymentAccountInput = {
  mode: "test" | "live";
  key_id: string;
  key_secret?: string;
  webhook_secret?: string;
};

export const paymentAccountPath = "/payments/razorpay-account";
