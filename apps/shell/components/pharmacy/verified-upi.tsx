"use client";

import { useEffect, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  newPharmacyId,
  pharmacyCounterPaymentPath,
  pharmacyKey,
  pharmacyUnusedCounterPaymentsPath,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  type PharmacyCounterPayment,
  type PharmacyScope,
} from "@helivanta/api";
import { useNow } from "@/lib/use-now";
import { formatPharmacyMoney } from "./currency-amount-field";

// A request with no QR after this long is not going to get one in time for
// the queue; the cashier is told to take another tender (spec, Not covered).
const startTimeoutMs = 60_000;

const settled = new Set(["paid", "failed", "expired", "cancelled"]);

function failureText(reason: PharmacyCounterPayment["failure_reason"]) {
  if (reason === "provider_not_configured")
    return "No Razorpay account is set up, so UPI can't be verified. Take another tender.";
  return "Couldn't start UPI. Take another tender.";
}

/**
 * The verified-UPI part of a counter sale (#1386 C-4b). It shows the QR,
 * follows the payment, and reports the payment id only once payments has
 * fetched it from Razorpay as captured for this amount. It never shows
 * paid on anything else.
 */
export function VerifiedUpi({
  scope,
  amountMinor,
  currency,
  onPaid,
}: {
  scope: PharmacyScope;
  amountMinor: number;
  currency: string;
  onPaid: (paymentRequestId: string | null) => void;
}) {
  const money = (minor: number) => formatPharmacyMoney(minor, currency);
  const [id, setId] = useState<string | null>(null);
  const [chosen, setChosen] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const now = useNow(1000);
  // Polled until it settles; a settled payment never changes again here.
  const payment = useApiQuery<PharmacyCounterPayment>(
    pharmacyKey(scope, "counter-payment", id ?? ""),
    id ? pharmacyCounterPaymentPath(scope, id) : "",
    { enabled: id !== null && !done, poll: true },
  );
  const unused = useApiPagedQuery<PharmacyCounterPayment>(
    pharmacyKey(scope, "counter-payments", "unused"),
    pharmacyUnusedCounterPaymentsPath(scope),
  );
  const start = useApiMutation(
    (next: string) =>
      apiFetch<PharmacyCounterPayment>(pharmacyCounterPaymentPath(scope, next), {
        method: "PUT",
        body: JSON.stringify({ amount_minor: amountMinor }),
      }),
    { suppressErrorToast: true },
  );
  const cancel = useApiMutation(
    () =>
      apiFetch<PharmacyCounterPayment>(`${pharmacyCounterPaymentPath(scope, id ?? "")}/cancel`, {
        method: "POST",
      }),
    { suppressErrorToast: true, invalidate: [pharmacyKey(scope, "counter-payment", id ?? "")] },
  );
  const p = payment.data;
  const paidHere = p?.status === "paid" && p.amount_minor === amountMinor ? p.id : null;
  const usable = paidHere ?? chosen;
  useEffect(() => onPaid(usable), [usable, onPaid]);
  // Adjusted during render, not in an effect: once settled, stop polling.
  if (!done && settled.has(p?.status ?? "")) setDone(true);

  const begin = () => {
    const next = newPharmacyId();
    setChosen(null);
    setDone(false);
    setId(next);
    start.mutate(next);
  };
  // Paid money not on any sale yet, for exactly this bill: use it rather
  // than take the payer's money twice.
  const waiting = unused.items.filter((u) => u.amount_minor === amountMinor && u.id !== id);

  return (
    <div className="space-y-3 sm:col-span-2" aria-live="polite">
      {waiting.length > 0 && !usable && (
        <div className="rounded-md border p-3 text-sm">
          <p>A paid UPI payment of {money(amountMinor)} isn't on a sale yet.</p>
          {waiting.map((u) => (
            <Button
              key={u.id}
              type="button"
              variant="outline"
              className="mt-2"
              onClick={() => setChosen(u.id)}
            >
              Use the payment paid {u.paid_at ? new Date(u.paid_at).toLocaleTimeString() : ""}
              {u.rrn ? ` · RRN ${u.rrn}` : ""}
            </Button>
          ))}
        </div>
      )}
      {chosen && !paidHere ? (
        <p role="status" className="text-sm font-medium">
          Paid {money(amountMinor)} · earlier UPI payment
        </p>
      ) : id === null ? (
        <Button type="button" onClick={begin}>
          Show UPI QR for {money(amountMinor)}
        </Button>
      ) : start.isError ? (
        <div role="alert" className="text-sm text-destructive">
          <p>{start.error.message}</p>
          <Button type="button" variant="outline" className="mt-2" onClick={() => start.mutate(id)}>
            Try again
          </Button>
        </div>
      ) : !p ? (
        <p role="status">Starting UPI…</p>
      ) : (
        <PaymentState
          payment={p}
          now={now}
          money={money}
          onCancel={() => cancel.mutate()}
          cancelling={cancel.isPending}
          onRestart={begin}
        />
      )}
    </div>
  );
}

function PaymentState({
  payment: p,
  now,
  money,
  onCancel,
  cancelling,
  onRestart,
}: {
  payment: PharmacyCounterPayment;
  now: number;
  money: (minor: number) => string;
  onCancel: () => void;
  cancelling: boolean;
  onRestart: () => void;
}) {
  const again = (
    <Button type="button" variant="outline" className="mt-2" onClick={onRestart}>
      Show a new QR
    </Button>
  );
  switch (p.status) {
    case "requested":
      return now - Date.parse(p.created_at) > startTimeoutMs ? (
        <div role="alert" className="text-sm">
          <p>Couldn't start UPI. Take another tender.</p>
          {again}
        </div>
      ) : (
        <p role="status">Starting UPI…</p>
      );
    case "awaiting_payment": {
      const left = Math.max(0, Math.round((Date.parse(p.expires_at ?? "") - now) / 1000));
      return (
        <div className="space-y-2">
          {p.qr_image_url && (
            // Razorpay's own short-lived QR image, shown as served: routing
            // it through next/image would proxy a payment QR via our server.
            // eslint-disable-next-line @next/next/no-img-element
            <img
              src={p.qr_image_url}
              alt={`UPI QR for ${money(p.amount_minor)}`}
              className="h-56 w-56 rounded-md border"
            />
          )}
          <p role="status" className="text-sm">
            Waiting for payment of {money(p.amount_minor)} · {Math.floor(left / 60)}:
            {String(left % 60).padStart(2, "0")} left
          </p>
          <p className="text-xs text-muted-foreground">
            The sale can be posted once Razorpay confirms the payment. A screenshot is not a
            payment.
          </p>
          <Button type="button" variant="outline" onClick={onCancel} disabled={cancelling}>
            Cancel UPI
          </Button>
        </div>
      );
    }
    case "cancelling":
      return <p role="status">Cancelling… checking nothing was paid.</p>;
    case "paid":
      return (
        <p role="status" className="text-sm font-medium">
          Paid {money(p.amount_minor)}
          {p.rrn ? ` · RRN ${p.rrn}` : ""}
          {p.late ? " · arrived after the QR closed" : ""}
        </p>
      );
    case "failed":
      return (
        <div role="alert" className="text-sm">
          <p>{failureText(p.failure_reason)}</p>
          {p.failure_reason !== "provider_not_configured" && again}
        </div>
      );
    case "expired":
      return (
        <div role="alert" className="text-sm">
          <p>Expired. Take another tender, or show a new QR.</p>
          {again}
        </div>
      );
    case "cancelled":
      return (
        <div className="text-sm">
          <p role="status">UPI cancelled. Nothing was paid.</p>
          {again}
        </div>
      );
  }
}
