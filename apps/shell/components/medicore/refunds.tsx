"use client";
import { useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  newBedId,
  refundApprovalSchema,
  refundPolicySchema,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type CashReceipt,
  type Hospital,
  type RefundApproval,
  type RefundPolicy,
} from "@helivanta/api";
import { Field, RequestFailure, useZodForm } from "@helivanta/ui";
import { CurrencyAmountField, formatPharmacyMoney } from "../pharmacy/currency-amount-field";

const inputClass =
  "w-full rounded-md border bg-background px-3 py-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-(--hms-accent)";

export function useRefundPolicy(hospital: Hospital) {
  return useApiQuery<RefundPolicy>(
    ["billing", "refund-policy", hospital.id],
    `/billing/hospitals/${hospital.id}/refund-policy`,
  );
}

/** The hospital's approval threshold for refunds, for administrators (#199). */
export function RefundPolicySetup({ hospital }: { hospital: Hospital }) {
  const policy = useRefundPolicy(hospital);
  const [id, setId] = useState(() => newBedId());
  const form = useZodForm(refundPolicySchema, { threshold_minor: Number.NaN });
  const save = useApiMutation(
    ({ threshold_minor }: { threshold_minor: number }) =>
      apiFetch<RefundPolicy>(`/billing/hospitals/${hospital.id}/refund-policies/${id}`, {
        method: "PUT",
        body: JSON.stringify({ threshold_minor }),
      }),
    {
      successToast: "Refund threshold set",
      invalidate: [["billing", "refund-policy", hospital.id]],
      onSuccess: () => setId(newBedId()),
    },
  );
  if (policy.isError)
    return (
      <RequestFailure
        message="Could not load the refund threshold."
        retry={() => void policy.refetch()}
      />
    );
  return (
    <div>
      <h4 className="text-sm font-medium">Refunds</h4>
      {policy.data && (
        <p className="text-sm text-muted-foreground">
          Refunds above {formatPharmacyMoney(policy.data.threshold_minor, policy.data.currency)}{" "}
          need a supervisor's approval{policy.data.created_by ? "" : " (the default)"}.
        </p>
      )}
      <form
        noValidate
        aria-label="Set the refund threshold"
        className="mt-2 flex flex-wrap items-end gap-2"
        onSubmit={form.handleSubmit((v) => save.mutate(v))}
      >
        <CurrencyAmountField
          id="refund-threshold"
          label="Approval needed above"
          currency="INR"
          initialMinor={0}
          error={form.formState.errors.threshold_minor?.message}
          onChange={(minor) =>
            form.setValue("threshold_minor", minor, { shouldValidate: form.formState.isSubmitted })
          }
        />
        <Button type="submit" size="sm" variant="outline" disabled={save.isPending}>
          Set threshold
        </Button>
      </form>
    </div>
  );
}

/**
 * Refunding what is held for the patient once the final bill is out (#199):
 * a supervisor approves an amount above the threshold; the cashier pays it.
 */
export function DepositRefunds({
  hospital,
  admissionId,
  held,
  shiftOpen,
  receiptsKey,
}: {
  hospital: Hospital;
  admissionId: string;
  held: number;
  shiftOpen: boolean;
  receiptsKey: unknown[];
}) {
  const { can } = usePermissions();
  const policy = useRefundPolicy(hospital);
  const approvals = useApiPagedQuery<RefundApproval>(
    ["billing", "refund-approvals", hospital.id, admissionId],
    `/billing/hospitals/${hospital.id}/admissions/${admissionId}/refund-approvals`,
  );
  const unused = approvals.items.filter((a) => a.used_by === null && a.amount_minor <= held);
  if (!policy.data) return null;
  const threshold = policy.data.threshold_minor;
  return (
    <div className="space-y-2">
      {can("billing.shift.authorise") && (
        <ApproveRefund hospital={hospital} admissionId={admissionId} held={held} />
      )}
      {shiftOpen && can("billing.cash.collect") && (
        <PayRefund
          hospital={hospital}
          admissionId={admissionId}
          held={held}
          threshold={threshold}
          approvals={unused}
          receiptsKey={receiptsKey}
        />
      )}
    </div>
  );
}

function ApproveRefund({
  hospital,
  admissionId,
  held,
}: {
  hospital: Hospital;
  admissionId: string;
  held: number;
}) {
  const [id, setId] = useState(() => newBedId());
  const form = useZodForm(refundApprovalSchema, { amount_minor: Number.NaN, reason: "" });
  const approve = useApiMutation(
    (v: { amount_minor: number; reason: string }) =>
      apiFetch<RefundApproval>(
        `/billing/hospitals/${hospital.id}/admissions/${admissionId}/refund-approvals/${id}`,
        { method: "PUT", body: JSON.stringify(v) },
      ),
    {
      successToast: "Refund approved",
      invalidate: [["billing", "refund-approvals", hospital.id, admissionId]],
      onSuccess: () => setId(newBedId()),
    },
  );
  return (
    <form
      noValidate
      aria-label="Approve a refund"
      className="flex flex-wrap items-end gap-2"
      onSubmit={form.handleSubmit((v) => {
        if (v.amount_minor > held) {
          form.setError("amount_minor", { message: "More than is held for the patient" });
          return;
        }
        approve.mutate(v);
      })}
    >
      <CurrencyAmountField
        id={`approve-${admissionId}`}
        label="Approve a refund of"
        currency="INR"
        initialMinor={0}
        error={form.formState.errors.amount_minor?.message}
        onChange={(minor) =>
          form.setValue("amount_minor", minor, { shouldValidate: form.formState.isSubmitted })
        }
      />
      <Field
        id={`approve-reason-${admissionId}`}
        label="Reason"
        error={form.formState.errors.reason?.message}
      >
        <input
          id={`approve-reason-${admissionId}`}
          className={`${inputClass} w-56`}
          {...form.register("reason")}
        />
      </Field>
      <Button type="submit" size="sm" variant="outline" disabled={approve.isPending}>
        Approve refund
      </Button>
    </form>
  );
}

function PayRefund({
  hospital,
  admissionId,
  held,
  threshold,
  approvals,
  receiptsKey,
}: {
  hospital: Hospital;
  admissionId: string;
  held: number;
  threshold: number;
  approvals: RefundApproval[];
  receiptsKey: unknown[];
}) {
  const [id, setId] = useState(() => newBedId());
  const [amount, setAmount] = useState(Number.NaN);
  const [problem, setProblem] = useState<string | null>(null);
  const pay = useApiMutation(
    (body: { amount_minor: number; approval_id?: string }) =>
      apiFetch<CashReceipt>(
        `/billing/hospitals/${hospital.id}/admissions/${admissionId}/refunds/${id}`,
        {
          method: "PUT",
          body: JSON.stringify({ ...body, currency: "INR" }),
        },
      ),
    {
      successToast: "Refund paid",
      invalidate: [
        receiptsKey,
        ["billing", "account", hospital.id, admissionId],
        ["billing", "refund-approvals", hospital.id, admissionId],
      ],
      invalidateOnError: true,
      onSuccess: () => setId(newBedId()),
    },
  );
  const money = (minor: number) => formatPharmacyMoney(minor, "INR");
  // Above the threshold only an approval for exactly this amount pays it.
  const approval = approvals.find((a) => a.amount_minor === amount);
  function submit() {
    if (!Number.isSafeInteger(amount) || amount < 1)
      return setProblem("Enter the amount to refund.");
    if (amount > held) return setProblem(`At most what is held, ${money(held)}.`);
    if (amount > threshold && !approval)
      return setProblem(`Above ${money(threshold)} a supervisor approves this amount first.`);
    setProblem(null);
    pay.mutate(
      amount > threshold && approval
        ? { amount_minor: amount, approval_id: approval.id }
        : { amount_minor: amount },
    );
  }
  return (
    <div className="flex flex-wrap items-end gap-2">
      <CurrencyAmountField
        id={`refund-${admissionId}`}
        label="Refund in cash"
        currency="INR"
        initialMinor={0}
        onChange={setAmount}
      />
      <Button size="sm" variant="outline" disabled={pay.isPending} onClick={submit}>
        Pay refund
      </Button>
      {approvals.length > 0 && (
        <p className="w-full text-xs text-muted-foreground">
          Approved: {approvals.map((a) => money(a.amount_minor)).join(", ")}
        </p>
      )}
      {problem && (
        <p role="alert" className="w-full text-destructive">
          {problem}
        </p>
      )}
    </div>
  );
}
