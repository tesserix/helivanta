"use client";
import { useState } from "react";
import { Badge, Button } from "@tesserix/web";
import {
  apiFetch,
  discountDecisionSchema,
  discountReasonSchema,
  discountSchema,
  newBedId,
  useApiMutation,
  useApiPagedQuery,
  usePermissions,
  type Discount,
  type DiscountReason,
  type Hospital,
} from "@helivanta/api";
import {
  Field,
  FormSelectField,
  LoadMore,
  Panel,
  RequestFailure,
  formatDateTime,
  useZodForm,
} from "@helivanta/ui";
import { CurrencyAmountField, formatPharmacyMoney } from "../pharmacy/currency-amount-field";
import { PatientName } from "./opd-encounters";

const inputClass =
  "w-full rounded-md border bg-background px-3 py-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-(--hms-accent)";
const money = (minor: number) => formatPharmacyMoney(minor, "INR");
const percent = (bp: number) => `${bp / 100}%`;

const statusLabel: Record<Discount["status"], string> = {
  applied: "Applied",
  pending: "Waiting for approval",
  rejected: "Rejected",
};

// Every reason in one page: the desk chooses from the active ones.
function useDiscountReasons(hospital: Hospital) {
  return useApiPagedQuery<DiscountReason>(
    ["billing", "discount-reasons", hospital.id],
    `/billing/hospitals/${hospital.id}/discount-reasons?limit=200`,
  );
}

/** A hospital's discount reasons and their limits, for administrators (#200). */
export function DiscountReasonsSetup({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  if (!can("billing.discount.manage")) return null;
  return <ReasonsPanel hospital={hospital} />;
}

function ReasonsPanel({ hospital }: { hospital: Hospital }) {
  const reasons = useDiscountReasons(hospital);
  return (
    <Panel>
      <h3 className="font-semibold">Discount reasons</h3>
      <p className="mt-1 text-sm text-muted-foreground">
        The desk gives a discount within a reason's desk limit; beyond it, an administrator approves
        up to the reason's approval limit. Limits count every discount of the reason on an
        admission. A reason is fixed once set: to change its limits, retire it and add another.
      </p>
      {reasons.isError ? (
        <RequestFailure message="Could not load the reasons." retry={reasons.retry} />
      ) : (
        <ul className="mt-3 divide-y divide-border text-sm">
          {reasons.items.map((r) => (
            <Reason key={r.id} hospital={hospital} reason={r} />
          ))}
        </ul>
      )}
      <LoadMore
        label="Load more reasons"
        hasMore={reasons.hasMore}
        isLoading={reasons.isFetchingMore}
        onClick={reasons.loadMore}
      />
      <ReasonForm hospital={hospital} />
    </Panel>
  );
}

function Reason({ hospital, reason }: { hospital: Hospital; reason: DiscountReason }) {
  const retire = useApiMutation(
    () =>
      apiFetch<DiscountReason>(
        `/billing/hospitals/${hospital.id}/discount-reasons/${reason.id}/retire`,
        { method: "POST" },
      ),
    {
      successToast: "Reason retired",
      invalidate: [["billing", "discount-reasons", hospital.id]],
    },
  );
  return (
    <li className="flex flex-wrap items-center justify-between gap-2 py-2">
      <span className={reason.retired_at ? "text-muted-foreground" : undefined}>
        {reason.name} ({reason.code}) · desk {percent(reason.desk_percent_bp)} up to{" "}
        {money(reason.desk_max_minor)} · approval {percent(reason.approval_percent_bp)} up to{" "}
        {money(reason.approval_max_minor)}
        {reason.retired_at && (
          <Badge variant="outline" className="ml-2">
            Retired
          </Badge>
        )}
      </span>
      {!reason.retired_at && (
        <Button
          variant="ghost"
          size="sm"
          aria-label={`Retire ${reason.name}`}
          disabled={retire.isPending}
          onClick={() => retire.mutate()}
        >
          Retire
        </Button>
      )}
    </li>
  );
}

function ReasonForm({ hospital }: { hospital: Hospital }) {
  // One id per form: a retried submit repeats the same reason.
  const [id, setId] = useState(() => newBedId());
  const form = useZodForm(discountReasonSchema, {
    code: "",
    name: "",
    desk_percent: "",
    desk_max_minor: 0,
    approval_percent: "",
    approval_max_minor: 0,
  });
  const save = useApiMutation(
    (v: {
      code: string;
      name: string;
      desk_percent: number;
      desk_max_minor: number;
      approval_percent: number;
      approval_max_minor: number;
    }) =>
      apiFetch<DiscountReason>(`/billing/hospitals/${hospital.id}/discount-reasons/${id}`, {
        method: "PUT",
        body: JSON.stringify({
          code: v.code,
          name: v.name,
          desk_percent_bp: Math.round(v.desk_percent * 100),
          desk_max_minor: v.desk_max_minor,
          approval_percent_bp: Math.round(v.approval_percent * 100),
          approval_max_minor: v.approval_max_minor,
        }),
      }),
    {
      successToast: "Reason added",
      invalidate: [["billing", "discount-reasons", hospital.id]],
      onSuccess: () => {
        setId(newBedId());
        form.reset();
      },
    },
  );
  const e = form.formState.errors;
  const amount = (name: "desk_max_minor" | "approval_max_minor", label: string) => (
    <CurrencyAmountField
      id={`reason-${name}`}
      label={label}
      currency="INR"
      initialMinor={0}
      error={e[name]?.message}
      onChange={(minor) =>
        form.setValue(name, minor, { shouldValidate: form.formState.isSubmitted })
      }
    />
  );
  return (
    <form
      noValidate
      aria-label="Add a discount reason"
      className="mt-3 flex flex-wrap items-end gap-2"
      onSubmit={form.handleSubmit((v) => save.mutate(v))}
    >
      <Field id="reason-code" label="Code" error={e.code?.message}>
        <input id="reason-code" className={`${inputClass} w-32`} {...form.register("code")} />
      </Field>
      <Field id="reason-name" label="Name" error={e.name?.message}>
        <input id="reason-name" className={`${inputClass} w-48`} {...form.register("name")} />
      </Field>
      <Field id="reason-desk-percent" label="Desk %" error={e.desk_percent?.message}>
        <input
          id="reason-desk-percent"
          inputMode="decimal"
          className={`${inputClass} w-24`}
          {...form.register("desk_percent")}
        />
      </Field>
      {amount("desk_max_minor", "Desk up to")}
      <Field id="reason-approval-percent" label="Approval %" error={e.approval_percent?.message}>
        <input
          id="reason-approval-percent"
          inputMode="decimal"
          className={`${inputClass} w-24`}
          {...form.register("approval_percent")}
        />
      </Field>
      {amount("approval_max_minor", "Approval up to")}
      <Button type="submit" size="sm" variant="outline" disabled={save.isPending}>
        Add reason
      </Button>
    </form>
  );
}

/**
 * An admission's discounts (#200): what was given, by whom and why; the desk
 * asks for one, and an approver decides those beyond the desk's limit.
 */
export function AdmissionDiscounts({
  hospital,
  admissionId,
  discountable,
}: {
  hospital: Hospital;
  admissionId: string;
  /** The most a discount may take now; 0 when nothing is discountable. */
  discountable: number;
}) {
  const { can } = usePermissions();
  const key = ["billing", "discounts", hospital.id, admissionId];
  const discounts = useApiPagedQuery<Discount>(
    key,
    `/billing/hospitals/${hospital.id}/admissions/${admissionId}/discounts`,
  );
  return (
    <div className="space-y-2">
      {can("billing.bill.issue") && discountable > 0 && (
        <GiveDiscount
          hospital={hospital}
          admissionId={admissionId}
          most={discountable}
          discountsKey={key}
        />
      )}
      {discounts.isError ? (
        <RequestFailure message="Could not load the discounts." retry={discounts.retry} />
      ) : (
        <ul className="divide-y divide-border">
          {discounts.items.map((d) => (
            <DiscountRow key={d.id} hospital={hospital} discount={d} />
          ))}
        </ul>
      )}
      <LoadMore
        label="Load more discounts"
        hasMore={discounts.hasMore}
        isLoading={discounts.isFetchingMore}
        onClick={discounts.loadMore}
      />
    </div>
  );
}

function GiveDiscount({
  hospital,
  admissionId,
  most,
  discountsKey,
}: {
  hospital: Hospital;
  admissionId: string;
  most: number;
  discountsKey: unknown[];
}) {
  const reasons = useDiscountReasons(hospital);
  const active = reasons.items.filter((r) => !r.retired_at);
  // One id per form: a retried submit repeats the same request.
  const [id, setId] = useState(() => newBedId());
  const [outcome, setOutcome] = useState("");
  const [over, setOver] = useState(false);
  const form = useZodForm(discountSchema, {
    reason_id: "",
    amount_minor: Number.NaN,
    justification: "",
  });
  const give = useApiMutation(
    (v: { reason_id: string; amount_minor: number; justification: string }) =>
      apiFetch<Discount>(
        `/billing/hospitals/${hospital.id}/admissions/${admissionId}/discounts/${id}`,
        { method: "PUT", body: JSON.stringify(v) },
      ),
    {
      invalidate: [
        discountsKey,
        ["billing", "account", hospital.id, admissionId],
        ["billing", "desk", hospital.id],
        ["billing", "pending-discounts", hospital.id],
      ],
      invalidateOnError: true,
      // The server decides: applied within the desk's limit, otherwise waiting.
      onSuccess: (d) => {
        setOutcome(
          d.status === "applied"
            ? `Discount of ${money(d.amount_minor)} applied`
            : `Discount of ${money(d.amount_minor)} sent for approval`,
        );
        setId(newBedId());
        form.reset();
      },
    },
  );
  if (reasons.isError)
    return <RequestFailure message="Could not load the reasons." retry={reasons.retry} />;
  if (!reasons.isPending && active.length === 0)
    return (
      <p className="text-muted-foreground">
        No discount reasons are set up here; an administrator adds them.
      </p>
    );
  return (
    <form
      noValidate
      aria-label="Give a discount"
      className="flex flex-wrap items-end gap-2"
      onSubmit={form.handleSubmit((v) => {
        // Checked here to say why; the server refuses it too.
        setOver(v.amount_minor > most);
        if (v.amount_minor <= most) give.mutate(v);
      })}
    >
      <FormSelectField
        control={form.control}
        name="reason_id"
        id={`discount-reason-${admissionId}`}
        label="Reason"
        options={active.map((r) => ({ value: r.id, label: r.name }))}
      />
      <CurrencyAmountField
        id={`discount-${admissionId}`}
        label="Discount"
        currency="INR"
        initialMinor={0}
        error={form.formState.errors.amount_minor?.message}
        onChange={(minor) =>
          form.setValue("amount_minor", minor, { shouldValidate: form.formState.isSubmitted })
        }
      />
      <Field
        id={`discount-why-${admissionId}`}
        label="Why"
        error={form.formState.errors.justification?.message}
      >
        <input
          id={`discount-why-${admissionId}`}
          className={`${inputClass} w-56`}
          {...form.register("justification")}
        />
      </Field>
      <Button type="submit" size="sm" variant="outline" disabled={give.isPending}>
        Give discount
      </Button>
      <p className="w-full text-muted-foreground">
        Up to {money(most)}, split over the room days not yet on a tax invoice. Taxable rooms are
        not discounted.
      </p>
      {over && (
        <p role="alert" className="w-full text-destructive">
          At most {money(most)}
        </p>
      )}
      <p role="status" className="w-full">
        {outcome}
      </p>
    </form>
  );
}

function DiscountRow({
  hospital,
  discount: d,
  withPatient = false,
}: {
  hospital: Hospital;
  discount: Discount;
  /** The approvers' queue spans admissions: say whose it is. */
  withPatient?: boolean;
}) {
  const { can } = usePermissions();
  return (
    <li className="py-1">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span>
          {withPatient && (
            <>
              <PatientName hospital={hospital} id={d.patient_id} /> ·{" "}
            </>
          )}
          Discount · {d.reason_name} · {formatDateTime(d.requested_at)} · {d.requested_by}
          <Badge variant="outline" className="ml-2">
            {statusLabel[d.status]}
          </Badge>
          {d.decided_by &&
            ` · ${d.status === "rejected" ? "rejected" : "approved"} by ${d.decided_by}`}
        </span>
        <span>−{money(d.amount_minor)}</span>
      </div>
      <p className="text-muted-foreground">
        {d.justification}
        {d.decision_note && ` · ${d.decision_note}`}
        {d.status === "applied" &&
          d.net_minor !== d.amount_minor &&
          ` · ${money(d.amount_minor - d.net_minor)} withdrawn with voided days`}
      </p>
      {d.status === "pending" && can("billing.discount.approve") && (
        <Decide hospital={hospital} discount={d} />
      )}
    </li>
  );
}

function Decide({ hospital, discount }: { hospital: Hospital; discount: Discount }) {
  const form = useZodForm(discountDecisionSchema, { note: "" });
  const decision = (approved: boolean) => ({
    successToast: approved ? "Discount approved" : "Discount rejected",
    invalidate: [
      ["billing", "discounts", hospital.id, discount.admission_id],
      ["billing", "pending-discounts", hospital.id],
      ["billing", "account", hospital.id, discount.admission_id],
      ["billing", "desk", hospital.id],
    ],
    invalidateOnError: true,
  });
  const put = (note: string, approved: boolean) =>
    apiFetch<Discount>(`/billing/hospitals/${hospital.id}/discounts/${discount.id}/decision`, {
      method: "PUT",
      body: JSON.stringify({ approve: approved, note }),
    });
  const approveIt = useApiMutation((note: string) => put(note, true), decision(true));
  const rejectIt = useApiMutation((note: string) => put(note, false), decision(false));
  const pending = approveIt.isPending || rejectIt.isPending;
  // Each button decides for itself; Enter in the note decides nothing.
  return (
    <form
      noValidate
      aria-label={`Decide the ${discount.reason_name} discount of ${money(discount.amount_minor)}`}
      className="mt-1 flex flex-wrap items-end gap-2"
      onSubmit={(e) => e.preventDefault()}
    >
      <Field
        id={`decision-${discount.id}`}
        label="Note"
        error={form.formState.errors.note?.message}
      >
        <input
          id={`decision-${discount.id}`}
          className={`${inputClass} w-56`}
          {...form.register("note")}
        />
      </Field>
      <Button
        type="button"
        size="sm"
        disabled={pending}
        onClick={form.handleSubmit(({ note }) => approveIt.mutate(note))}
      >
        Approve
      </Button>
      <Button
        type="button"
        size="sm"
        variant="outline"
        disabled={pending}
        onClick={form.handleSubmit(({ note }) => rejectIt.mutate(note))}
      >
        Reject
      </Button>
    </form>
  );
}

/** A hospital's discounts waiting for approval, for approvers (#200). */
export function PendingDiscounts({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  if (!can("billing.discount.approve")) return null;
  return <PendingPanel hospital={hospital} />;
}

function PendingPanel({ hospital }: { hospital: Hospital }) {
  const pending = useApiPagedQuery<Discount>(
    ["billing", "pending-discounts", hospital.id],
    `/billing/hospitals/${hospital.id}/pending-discounts`,
  );
  return (
    <Panel>
      <h3 className="font-semibold">Discounts waiting for approval</h3>
      {pending.isError ? (
        <RequestFailure message="Could not load the discounts." retry={pending.retry} />
      ) : pending.items.length === 0 && !pending.isPending ? (
        <p className="mt-1 text-sm text-muted-foreground">None waiting.</p>
      ) : (
        <ul className="mt-2 divide-y divide-border text-sm">
          {pending.items.map((d) => (
            <DiscountRow key={d.id} hospital={hospital} discount={d} withPatient />
          ))}
        </ul>
      )}
      <LoadMore
        label="Load more discounts"
        hasMore={pending.hasMore}
        isLoading={pending.isFetchingMore}
        onClick={pending.loadMore}
      />
    </Panel>
  );
}
