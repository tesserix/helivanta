"use client";
import { useEffect, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  billingUpiRequestPath,
  cashReceiptSchema,
  newBedId,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  useApiRefresh,
  type BillingUpiRequest,
  type CashShift,
  type Hospital,
} from "@helivanta/api";
import { RequestFailure, useZodForm } from "@helivanta/ui";
import { useNow } from "@/lib/use-now";
import { CurrencyAmountField, formatPharmacyMoney } from "../pharmacy/currency-amount-field";
import { PaymentState } from "../pharmacy/verified-upi";

const money = (minor: number) => formatPharmacyMoney(minor, "INR");

// The server's window: a request unanswered 15 minutes after it was asked
// holds nothing and blocks nothing, so the desk does not pick it up again.
const staleMs = 15 * 60_000;

// Still waiting on payments' answer, as far as the list knows: the desk follows it.
const waiting = (u: BillingUpiRequest, now: number) =>
  (u.status === "requested" || u.status === "awaiting_payment" || u.status === "cancelling") &&
  now - Date.parse(u.created_at) < staleMs;

// Paid, or a failure before any QR existed, can never change. Expired and
// cancelled can still turn paid (late), so they are followed while shown.
const settled = (u: BillingUpiRequest | undefined) =>
  u !== undefined && (u.status === "paid" || (u.status === "failed" && u.qr_image_url === null));

/**
 * UPI at the billing desk (#198, #211): the cashier asks for a QR in their
 * open shift; once payments confirms the money with Razorpay, the receipt is
 * recorded by the server, numbered on the counter. A screenshot is never a
 * payment, and UPI never enters the drawer.
 */
export function BillingUpi({
  hospital,
  admissionId,
  purpose,
  shift,
  max,
  receiptsKey,
}: {
  hospital: Hospital;
  admissionId: string;
  purpose: "payment" | "deposit";
  shift: CashShift;
  /** A payment is capped at the balance due less UPI already waiting. */
  max?: number;
  receiptsKey: unknown[];
}) {
  const requestsKey = ["billing", "upi-requests", hospital.id, admissionId];
  const requests = useApiPagedQuery<BillingUpiRequest>(
    requestsKey,
    `/billing/hospitals/${hospital.id}/admissions/${admissionId}/upi-requests`,
  );
  const now = useNow(1000);
  // Requests the cashier moved on from: the list may still say waiting.
  const [dismissed, setDismissed] = useState<string[]>([]);
  // A QR this shift is still waiting on, after a reload, is picked up again.
  const resumable = requests.items.find(
    (u) =>
      u.shift_id === shift.id &&
      u.purpose === purpose &&
      !dismissed.includes(u.id) &&
      waiting(u, now),
  );
  const [id, setId] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  // Adjusted during render, not in an effect.
  if (id === null && resumable) setId(resumable.id);
  const request = useApiQuery<BillingUpiRequest>(
    ["billing", "upi-request", hospital.id, id ?? ""],
    id ? billingUpiRequestPath(hospital.id, id) : "",
    { enabled: id !== null && !done, poll: true },
  );
  const u = request.data?.id === id ? request.data : undefined;
  if (!done && settled(u)) setDone(true);
  const refresh = useApiRefresh();
  const paid = u?.status === "paid";
  // The receipt was written by the server, not by this screen: refresh what shows it.
  useEffect(() => {
    if (paid)
      refresh([
        receiptsKey,
        requestsKey,
        ["billing", "account", hospital.id, admissionId],
        ["billing", "desk", hospital.id],
      ]);
    // Once per paid request.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [paid, id]);

  const form = useZodForm(cashReceiptSchema, { amount_minor: Number.NaN });
  const [over, setOver] = useState(false);
  const ask = useApiMutation(
    ({ next, amount_minor }: { next: string; amount_minor: number }) =>
      apiFetch<BillingUpiRequest>(
        `/billing/hospitals/${hospital.id}/admissions/${admissionId}/upi-requests/${next}`,
        { method: "PUT", body: JSON.stringify({ purpose, amount_minor }) },
      ),
    {
      invalidate: [requestsKey, ["billing", "account", hospital.id, admissionId]],
      invalidateOnError: true,
      onSuccess: (r) => {
        setDone(false);
        setId(r.id);
      },
    },
  );
  const cancel = useApiMutation(
    (target: string) =>
      apiFetch<BillingUpiRequest>(`${billingUpiRequestPath(hospital.id, target)}/cancel`, {
        method: "POST",
      }),
    {
      invalidate: [["billing", "upi-request", hospital.id], requestsKey],
      invalidateOnError: true,
    },
  );
  // One id per form until it succeeds: a retried click repeats the same request.
  const [next, setNext] = useState(() => newBedId());
  const restart = () => {
    if (id) setDismissed((d) => [...d, id]);
    setId(null);
    setDone(false);
    setNext(newBedId());
  };
  const label = purpose === "deposit" ? "UPI deposit" : "UPI payment";

  if (id && !u && request.isError)
    return (
      <RequestFailure
        message={`Could not check the ${label}.`}
        retry={() => void request.refetch()}
      />
    );
  if (id && !u) return <p role="status">Starting UPI…</p>;
  if (u) {
    return (
      <div className="space-y-2" aria-label={label}>
        {paid ? (
          <div className="text-sm">
            <p role="status" className="font-medium">
              {label} received: {money(u.amount_minor)} · receipt {u.counter_code}-
              {u.receipt_number}
              {u.rrn ? ` · RRN ${u.rrn}` : ""}
              {u.late ? " · arrived after the QR closed" : ""}
            </p>
            <Button type="button" size="sm" variant="outline" className="mt-2" onClick={restart}>
              Take another UPI {purpose}
            </Button>
          </div>
        ) : (
          <PaymentState
            payment={u}
            now={now}
            money={money}
            onCancel={() => cancel.mutate(u.id)}
            cancelling={cancel.isPending}
            onRestart={restart}
            waitingNote="The receipt is recorded once Razorpay confirms the payment. A screenshot is not a payment."
          />
        )}
      </div>
    );
  }
  // Nothing left to pay: no form, but a confirmation above stays shown.
  if (max !== undefined && max <= 0) return null;
  return (
    <form
      noValidate
      aria-label={`Take a ${label}`}
      className="flex flex-wrap items-end gap-2"
      onSubmit={form.handleSubmit(({ amount_minor }) => {
        // Checked here to say why; the server refuses it too.
        const tooMuch = max !== undefined && amount_minor > max;
        setOver(tooMuch);
        if (!tooMuch) ask.mutate({ next, amount_minor }, { onSuccess: () => setNext(newBedId()) });
      })}
    >
      <CurrencyAmountField
        id={`upi-${purpose}-${admissionId}`}
        label={purpose === "deposit" ? "UPI deposit" : "UPI amount"}
        currency="INR"
        initialMinor={0}
        error={form.formState.errors.amount_minor?.message}
        onChange={(minor) =>
          form.setValue("amount_minor", minor, { shouldValidate: form.formState.isSubmitted })
        }
      />
      <Button type="submit" size="sm" variant="outline" disabled={ask.isPending}>
        Show UPI QR
      </Button>
      {over && (
        <p role="alert" className="w-full text-destructive">
          At most {money(max ?? 0)}: the balance due less UPI already waiting.
        </p>
      )}
    </form>
  );
}
