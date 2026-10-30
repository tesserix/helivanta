"use client";
import { useMemo, useState } from "react";
import { Badge, Button } from "@tesserix/web";
import {
  apiFetch,
  cashReasonSchema,
  cashReceiptSchema,
  newBedId,
  useApiMutation,
  useApiPagedQuery,
  usePermissions,
  type AdmissionAccount,
  type CashReceipt,
  type Hospital,
} from "@helivanta/api";
import { Field, LoadMore, RequestFailure, formatDateTime, useZodForm } from "@helivanta/ui";
import { CurrencyAmountField, formatPharmacyMoney } from "../pharmacy/currency-amount-field";
import { useMyShift } from "./cash-desk";

const inputClass =
  "w-full rounded-md border bg-background px-3 py-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-(--hms-accent)";

/**
 * Cash taken against an admission (#198): what has been paid, and, for a
 * cashier with an open shift, taking the next payment. A receipt is voided,
 * never edited, while its shift is open.
 */
export function CashPayments({
  hospital,
  admissionId,
  account,
}: {
  hospital: Hospital;
  admissionId: string;
  account: AdmissionAccount | undefined;
}) {
  const { open } = useMyShift(hospital);
  const path = `/billing/hospitals/${hospital.id}/admissions/${admissionId}/receipts`;
  const key = ["billing", "receipts", hospital.id, admissionId];
  const receipts = useApiPagedQuery<CashReceipt>(key, path);
  const inr = account?.totals.find((t) => t.currency === "INR");
  return (
    <div className="space-y-2 border-t border-border pt-2 text-sm">
      {inr && (
        <p>
          Paid {formatPharmacyMoney(inr.paid_minor, "INR")} ·{" "}
          {inr.balance_minor < 0
            ? `in credit ${formatPharmacyMoney(-inr.balance_minor, "INR")}`
            : `balance due ${formatPharmacyMoney(inr.balance_minor, "INR")}`}
        </p>
      )}
      {open && inr && inr.balance_minor > 0 && (
        <TakeCash
          hospital={hospital}
          admissionId={admissionId}
          due={inr.balance_minor}
          receiptsKey={key}
        />
      )}
      {receipts.isError ? (
        <RequestFailure message="Could not load the receipts." retry={receipts.retry} />
      ) : (
        <ul className="divide-y divide-border">
          {receipts.items.map((r) => (
            <Receipt
              key={r.id}
              hospital={hospital}
              receipt={r}
              mine={open !== undefined && r.shift_id === open.id}
              receiptsKey={key}
            />
          ))}
        </ul>
      )}
      <LoadMore
        label="Load more receipts"
        hasMore={receipts.hasMore}
        isLoading={receipts.isFetchingMore}
        onClick={receipts.loadMore}
      />
    </div>
  );
}

function TakeCash({
  hospital,
  admissionId,
  due,
  receiptsKey,
}: {
  hospital: Hospital;
  admissionId: string;
  due: number;
  receiptsKey: unknown[];
}) {
  // One receipt id per form: a retried submit repeats the same receipt.
  const [receiptId, setReceiptId] = useState(() => newBedId());
  const form = useZodForm(cashReceiptSchema, { amount_minor: Number.NaN });
  const [over, setOver] = useState(false);
  const take = useApiMutation(
    ({ amount_minor }: { amount_minor: number }) =>
      apiFetch<CashReceipt>(
        `/billing/hospitals/${hospital.id}/admissions/${admissionId}/receipts/${receiptId}`,
        { method: "PUT", body: JSON.stringify({ amount_minor, currency: "INR" }) },
      ),
    {
      successToast: "Cash received",
      invalidate: [receiptsKey, ["billing", "account", hospital.id, admissionId]],
      invalidateOnError: true,
      onSuccess: () => setReceiptId(newBedId()),
    },
  );
  return (
    <form
      noValidate
      aria-label="Take cash"
      className="flex flex-wrap items-end gap-2"
      onSubmit={form.handleSubmit(({ amount_minor }) => {
        // Checked here to say why; the server refuses it too (an advance is a deposit).
        setOver(amount_minor > due);
        if (amount_minor <= due) take.mutate({ amount_minor });
      })}
    >
      <CurrencyAmountField
        id={`cash-${admissionId}`}
        label="Cash received"
        currency="INR"
        initialMinor={0}
        error={form.formState.errors.amount_minor?.message}
        onChange={(minor) =>
          form.setValue("amount_minor", minor, { shouldValidate: form.formState.isSubmitted })
        }
      />
      <Button type="submit" size="sm" disabled={take.isPending}>
        Take cash
      </Button>
      {over && (
        <p role="alert" className="w-full text-destructive">
          At most the balance due, {formatPharmacyMoney(due, "INR")}; an advance is a deposit.
        </p>
      )}
    </form>
  );
}

function Receipt({
  hospital,
  receipt,
  mine,
  receiptsKey,
}: {
  hospital: Hospital;
  receipt: CashReceipt;
  mine: boolean;
  receiptsKey: unknown[];
}) {
  const { can } = usePermissions();
  const [voiding, setVoiding] = useState(false);
  const voidId = useMemo(() => newBedId(), []);
  const form = useZodForm(cashReasonSchema, { reason: "" });
  const voidIt = useApiMutation(
    (reason: string) =>
      apiFetch(`/billing/hospitals/${hospital.id}/receipts/${receipt.id}/voids/${voidId}`, {
        method: "PUT",
        body: JSON.stringify({ reason }),
      }),
    {
      successToast: "Receipt voided",
      invalidate: [receiptsKey, ["billing", "account", hospital.id, receipt.admission_id]],
    },
  );
  const label = `Receipt ${receipt.counter_code}-${receipt.number}`;
  return (
    <li className="py-1">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span>
          {label} · {formatDateTime(receipt.received_at)} · {receipt.cashier}
          {receipt.voided_at && (
            <Badge variant="outline" className="ml-2">
              Void: {receipt.void_reason}
            </Badge>
          )}
        </span>
        <span className={receipt.voided_at ? "line-through" : undefined}>
          {formatPharmacyMoney(receipt.amount_minor, receipt.currency)}
        </span>
      </div>
      {mine && can("billing.cash.collect") && !receipt.voided_at && !voiding && (
        <Button
          variant="ghost"
          size="sm"
          aria-label={`Void ${label}`}
          onClick={() => setVoiding(true)}
        >
          Void
        </Button>
      )}
      {voiding && !receipt.voided_at && (
        <form
          noValidate
          className="mt-1 flex flex-wrap items-end gap-2"
          onSubmit={form.handleSubmit(({ reason }) => voidIt.mutate(reason))}
        >
          <Field
            id={`void-${receipt.id}`}
            label="Why it is voided"
            error={form.formState.errors.reason?.message}
          >
            <input
              id={`void-${receipt.id}`}
              className={`${inputClass} w-64`}
              {...form.register("reason")}
            />
          </Field>
          <Button type="submit" size="sm" variant="outline" disabled={voidIt.isPending}>
            Void receipt
          </Button>
        </form>
      )}
    </li>
  );
}
