"use client";
import { useMemo, useState } from "react";
import { Badge, Button } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  cashCountSchema,
  cashReasonSchema,
  counterSchema,
  countFromForm,
  newBedId,
  openShiftSchema,
  pickupCorrectionSchema,
  pickupCountSchema,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type CashCount,
  type CashCounter,
  type CashDenomination,
  type CashPickup,
  type CashShift,
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
import { useBillingAccess } from "./admission-account";
import { RefundPolicySetup } from "./refunds";

const inputClass =
  "w-full rounded-md border bg-background px-3 py-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-(--hms-accent)";

/** The caller's shift not yet closed at the hospital, or none (#198). */
export function useMyShift(hospital: Hospital) {
  const { can } = usePermissions();
  const enabled = can("billing.cash.collect");
  const shift = useApiQuery<CashShift>(
    ["billing", "my-shift", hospital.id],
    `/billing/hospitals/${hospital.id}/cash-shifts/mine`,
    { enabled },
  );
  const none = shift.isError && shift.error instanceof ApiError && shift.error.status === 404;
  return { shift, enabled, none, open: shift.data?.status === "open" ? shift.data : undefined };
}

/**
 * The cash desk (#198): counters for administrators, the cashier's own shift
 * (opened with a float, closed by a blind count) and variances waiting for a
 * supervisor. Payments are taken from an admission's bills.
 */
export function CashDesk({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  const billing = useBillingAccess(hospital);
  if (!billing) return null;
  const counters = can("billing.counter.manage");
  const cashier = can("billing.cash.collect");
  const supervisor = can("billing.shift.authorise");
  if (!counters && !cashier && !supervisor) return null;
  return (
    <Panel>
      <h3 className="font-semibold">Cash desk</h3>
      <div className="mt-3 space-y-4">
        {cashier && <MyShift hospital={hospital} />}
        {supervisor && <Variances hospital={hospital} />}
        {supervisor && <OpenShifts hospital={hospital} />}
        {counters && <Counters hospital={hospital} />}
        {counters && <RefundPolicySetup hospital={hospital} />}
      </div>
    </Panel>
  );
}

function MyShift({ hospital }: { hospital: Hospital }) {
  const { shift, none } = useMyShift(hospital);
  if (none) return <OpenShift hospital={hospital} />;
  if (shift.isError)
    return (
      <RequestFailure message="Could not load your shift." retry={() => void shift.refetch()} />
    );
  if (shift.isPending) return null;
  const s = shift.data;
  const money = (minor: number) => formatPharmacyMoney(minor, s.currency);
  if (s.status === "pending_authorisation")
    return (
      <p className="text-sm" role="status">
        Shift on {s.counter_code} closed {formatDateTime(s.closed_at!)}: counted{" "}
        {money(s.declared_minor!)}, expected {money(s.expected_minor!)}. The difference of{" "}
        {money(s.variance_minor!)} waits for a supervisor.
      </p>
    );
  return (
    <div className="space-y-2">
      <p className="text-sm">
        <Badge variant="outline">Shift open</Badge> on {s.counter_code} since{" "}
        {formatDateTime(s.opened_at)}, float {money(s.opening_float_minor)}.
        {s.picked_up_minor > 0 && <> Taken to the safe: {money(s.picked_up_minor)}.</>}
      </p>
      <CloseShift hospital={hospital} shift={s} />
    </div>
  );
}

function OpenShift({ hospital }: { hospital: Hospital }) {
  const shiftId = useMemo(() => newBedId(), []);
  const counters = useApiPagedQuery<CashCounter>(
    ["billing", "counters", hospital.id],
    `/billing/hospitals/${hospital.id}/counters`,
  );
  const form = useZodForm(openShiftSchema, { counter_id: "", opening_float_minor: Number.NaN });
  const open = useApiMutation(
    ({ counter_id, opening_float_minor }: { counter_id: string; opening_float_minor: number }) =>
      apiFetch<CashShift>(
        `/billing/hospitals/${hospital.id}/counters/${counter_id}/shifts/${shiftId}`,
        { method: "PUT", body: JSON.stringify({ opening_float_minor }) },
      ),
    { successToast: "Shift opened", invalidate: [["billing", "my-shift", hospital.id]] },
  );
  if (counters.isError)
    return <RequestFailure message="Could not load the counters." retry={counters.retry} />;
  if (counters.items.length === 0 && !counters.isPending)
    return <p className="text-sm text-muted-foreground">No counter is set up here yet.</p>;
  return (
    <form
      noValidate
      aria-label="Open a shift"
      className="flex flex-wrap items-end gap-3"
      onSubmit={form.handleSubmit((v) => open.mutate(v))}
    >
      <FormSelectField
        control={form.control}
        name="counter_id"
        id="shift-counter"
        label="Counter"
        options={counters.items.map((c) => ({ value: c.id, label: `${c.code} · ${c.name}` }))}
      />
      <CurrencyAmountField
        id="shift-float"
        label="Opening float"
        currency="INR"
        initialMinor={0}
        error={form.formState.errors.opening_float_minor?.message}
        onChange={(minor) =>
          form.setValue("opening_float_minor", minor, {
            shouldValidate: form.formState.isSubmitted,
          })
        }
      />
      <Button type="submit" size="sm" disabled={open.isPending}>
        Open shift
      </Button>
    </form>
  );
}

function CloseShift({ hospital, shift }: { hospital: Hospital; shift: CashShift }) {
  const close = useApiMutation(
    (counts: CashCount) =>
      apiFetch<CashShift>(`/billing/hospitals/${hospital.id}/cash-shifts/${shift.id}/close`, {
        method: "PUT",
        body: JSON.stringify({ counts }),
      }),
    // True either way: a count that differs is recorded but waits for a
    // supervisor, which the panel then says.
    { successToast: "Count recorded", invalidate: [["billing", "my-shift", hospital.id]] },
  );
  // Blind: the cashier counts the drawer without being told what to expect.
  return (
    <CountForm
      hospital={hospital}
      currency={shift.currency}
      idPrefix="close"
      label="Close the shift"
      legend="Notes and coins in the drawer"
      submit="Close shift"
      pending={close.isPending}
      onCount={(counts) => close.mutate(counts)}
    />
  );
}

/**
 * A count of notes and coins with its running total (#198). The list comes
 * from the API; a pickup must count something, a close may count an empty drawer.
 */
function CountForm({
  hospital,
  currency,
  idPrefix,
  label,
  legend,
  submit,
  pending,
  requireOne = false,
  onCount,
}: {
  hospital: Hospital;
  currency: string;
  idPrefix: string;
  label: string;
  legend: string;
  submit: string;
  pending: boolean;
  requireOne?: boolean;
  onCount: (counts: CashCount) => void;
}) {
  const denominations = useApiQuery<CashDenomination[]>(
    ["billing", "cash-denominations", hospital.id],
    `/billing/hospitals/${hospital.id}/cash-denominations`,
  );
  const form = useZodForm(requireOne ? pickupCountSchema : cashCountSchema, {
    counts: {},
    empty: false,
  });
  const typed = form.watch("counts");
  if (denominations.isError)
    return (
      <RequestFailure
        message="Could not load the notes and coins."
        retry={() => void denominations.refetch()}
      />
    );
  if (denominations.isPending) return null;
  const counts = countFromForm(typed ?? {});
  const total = denominations.data.reduce(
    (sum, d) => sum + d.value_minor * (counts[d.key] ?? 0),
    0,
  );
  const errors = form.formState.errors.counts as
    (Record<string, { message?: string }> & { message?: string }) | undefined;
  return (
    <form
      noValidate
      aria-label={label}
      className="space-y-2"
      onSubmit={form.handleSubmit((v) => onCount(countFromForm(v.counts)))}
    >
      <fieldset>
        <legend className="text-sm font-medium">{legend}</legend>
        <div className="mt-1 grid grid-cols-2 gap-2 sm:grid-cols-4">
          {denominations.data.map((d) => {
            const id = `${idPrefix}-${d.key.replace(":", "-")}`;
            const name = `${formatPharmacyMoney(d.value_minor, currency)} ${d.kind === "note" ? "notes" : "coins"}`;
            return (
              <Field key={d.key} id={id} label={name} error={errors?.[d.key]?.message}>
                <input
                  id={id}
                  inputMode="numeric"
                  className={inputClass}
                  {...form.register(`counts.${d.key}`)}
                />
              </Field>
            );
          })}
        </div>
      </fieldset>
      {!requireOne && (
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" {...form.register("empty")} />
          The drawer is empty
        </label>
      )}
      {errors?.message && (
        <p role="alert" className="text-sm text-destructive">
          {errors.message}
        </p>
      )}
      <div className="flex items-center gap-3">
        <p className="text-sm" role="status">
          Counted: {formatPharmacyMoney(total, currency)}
        </p>
        <Button type="submit" size="sm" variant="outline" disabled={pending}>
          {submit}
        </Button>
      </div>
    </form>
  );
}

function Variances({ hospital }: { hospital: Hospital }) {
  const shifts = useApiPagedQuery<CashShift>(
    ["billing", "shifts", hospital.id, "pending"],
    `/billing/hospitals/${hospital.id}/cash-shifts?status=pending_authorisation`,
  );
  if (shifts.isError)
    return <RequestFailure message="Could not load the shifts." retry={shifts.retry} />;
  const pending = shifts.items;
  return (
    <div>
      <h4 className="text-sm font-medium">Variances to authorise</h4>
      {pending.length === 0 && !shifts.isPending ? (
        <p className="text-sm text-muted-foreground">None waiting.</p>
      ) : (
        <ul className="divide-y divide-border">
          {pending.map((s) => (
            <Variance key={s.id} hospital={hospital} shift={s} />
          ))}
        </ul>
      )}
      <LoadMore
        label="Load more shifts"
        hasMore={shifts.hasMore}
        isLoading={shifts.isFetchingMore}
        onClick={shifts.loadMore}
      />
    </div>
  );
}

function Variance({ hospital, shift }: { hospital: Hospital; shift: CashShift }) {
  const [open, setOpen] = useState(false);
  const form = useZodForm(cashReasonSchema, { reason: "" });
  const authorise = useApiMutation(
    (reason: string) =>
      apiFetch(`/billing/hospitals/${hospital.id}/cash-shifts/${shift.id}/authorisation`, {
        method: "PUT",
        body: JSON.stringify({ reason }),
      }),
    { successToast: "Variance authorised", invalidate: [["billing", "shifts", hospital.id]] },
  );
  const money = (minor: number) => formatPharmacyMoney(minor, shift.currency);
  return (
    <li className="py-2 text-sm">
      <p>
        {shift.counter_code} · {shift.cashier} · closed {formatDateTime(shift.closed_at!)}: counted{" "}
        {money(shift.declared_minor!)}, expected {money(shift.expected_minor!)}, difference{" "}
        {money(shift.variance_minor!)}
      </p>
      {open ? (
        <form
          noValidate
          className="mt-2 flex flex-wrap items-end gap-2"
          onSubmit={form.handleSubmit(({ reason }) => authorise.mutate(reason))}
        >
          <Field
            id={`variance-${shift.id}`}
            label="Why the difference is accepted"
            error={form.formState.errors.reason?.message}
          >
            <input
              id={`variance-${shift.id}`}
              className={`${inputClass} w-72`}
              {...form.register("reason")}
            />
          </Field>
          <Button type="submit" size="sm" disabled={authorise.isPending}>
            Authorise
          </Button>
        </form>
      ) : (
        <Button
          variant="outline"
          size="sm"
          className="mt-1"
          aria-label={`Authorise the variance on ${shift.counter_code}`}
          onClick={() => setOpen(true)}
        >
          Authorise
        </Button>
      )}
    </li>
  );
}

/** Open shifts other than the caller's own: cash is picked up by someone else. */
function OpenShifts({ hospital }: { hospital: Hospital }) {
  const mine = useMyShift(hospital);
  const shifts = useApiPagedQuery<CashShift>(
    ["billing", "shifts", hospital.id, "open"],
    `/billing/hospitals/${hospital.id}/cash-shifts?status=open`,
  );
  if (shifts.isError)
    return <RequestFailure message="Could not load the open shifts." retry={shifts.retry} />;
  const others = shifts.items.filter((s) => s.id !== mine.shift.data?.id);
  return (
    <div>
      <h4 className="text-sm font-medium">Cash pickups</h4>
      {others.length === 0 && !shifts.isPending ? (
        <p className="text-sm text-muted-foreground">No other shift is open.</p>
      ) : (
        <ul className="divide-y divide-border">
          {others.map((s) => (
            <PickupShift key={s.id} hospital={hospital} shift={s} />
          ))}
        </ul>
      )}
      <LoadMore
        label="Load more shifts"
        hasMore={shifts.hasMore}
        isLoading={shifts.isFetchingMore}
        onClick={shifts.loadMore}
      />
    </div>
  );
}

function PickupShift({ hospital, shift }: { hospital: Hospital; shift: CashShift }) {
  const [open, setOpen] = useState(false);
  // A new id for each pickup: a retry repeats the same one.
  const [pickupId, setPickupId] = useState(() => newBedId());
  const key = ["billing", "pickups", hospital.id, shift.id];
  const pickups = useApiPagedQuery<CashPickup>(
    key,
    `/billing/hospitals/${hospital.id}/cash-shifts/${shift.id}/pickups`,
  );
  const record = useApiMutation(
    (counts: CashCount) =>
      apiFetch<CashPickup>(
        `/billing/hospitals/${hospital.id}/cash-shifts/${shift.id}/pickups/${pickupId}`,
        { method: "PUT", body: JSON.stringify({ counts }) },
      ),
    {
      successToast: "Pickup recorded",
      invalidate: [key, ["billing", "shifts", hospital.id]],
      onSuccess: () => {
        setOpen(false);
        setPickupId(newBedId());
      },
    },
  );
  const money = (minor: number) => formatPharmacyMoney(minor, shift.currency);
  const items = pickups.items;
  return (
    <li className="py-2 text-sm">
      <p>
        {shift.counter_code} · {shift.cashier} · open since {formatDateTime(shift.opened_at)}
        {shift.picked_up_minor > 0 && <> · taken to the safe {money(shift.picked_up_minor)}</>}
      </p>
      {pickups.isError ? (
        <RequestFailure message="Could not load the pickups." retry={pickups.retry} />
      ) : (
        <ul className="mt-1">
          {items
            .filter((p) => p.kind === "pickup")
            .map((p) => (
              <Pickup
                key={p.id}
                hospital={hospital}
                shift={shift}
                pickup={p}
                corrections={items.filter((c) => c.corrects_id === p.id)}
              />
            ))}
        </ul>
      )}
      {open ? (
        <div className="mt-2">
          <CountForm
            hospital={hospital}
            currency={shift.currency}
            idPrefix={`pickup-${shift.id}`}
            label={`Record a pickup from ${shift.counter_code}`}
            legend="Notes and coins taken"
            submit="Record pickup"
            requireOne
            pending={record.isPending}
            onCount={(counts) => record.mutate(counts)}
          />
        </div>
      ) : (
        <Button
          variant="outline"
          size="sm"
          className="mt-1"
          aria-label={`Record a pickup from ${shift.counter_code}`}
          onClick={() => setOpen(true)}
        >
          Record pickup
        </Button>
      )}
    </li>
  );
}

function Pickup({
  hospital,
  shift,
  pickup,
  corrections,
}: {
  hospital: Hospital;
  shift: CashShift;
  pickup: CashPickup;
  corrections: CashPickup[];
}) {
  const [open, setOpen] = useState(false);
  const [correctionId, setCorrectionId] = useState(() => newBedId());
  const form = useZodForm(pickupCorrectionSchema, { amount_minor: Number.NaN, reason: "" });
  const correct = useApiMutation(
    (values: { amount_minor: number; reason: string }) =>
      apiFetch<CashPickup>(
        `/billing/hospitals/${hospital.id}/cash-pickups/${pickup.id}/corrections/${correctionId}`,
        { method: "PUT", body: JSON.stringify(values) },
      ),
    {
      successToast: "Correction recorded",
      invalidate: [
        ["billing", "pickups", hospital.id, shift.id],
        ["billing", "shifts", hospital.id],
      ],
      onSuccess: () => {
        setOpen(false);
        setCorrectionId(newBedId());
        form.reset({ amount_minor: Number.NaN, reason: "" });
      },
    },
  );
  const money = (minor: number) => formatPharmacyMoney(minor, shift.currency);
  const id = `correction-${pickup.id}`;
  return (
    <li>
      {money(pickup.amount_minor)} by {pickup.recorded_by}, {formatDateTime(pickup.recorded_at)}
      {corrections.map((c) => (
        <span key={c.id} className="block pl-4 text-muted-foreground">
          Corrected by {money(c.amount_minor)} ({c.reason}), {c.recorded_by}
        </span>
      ))}
      {open ? (
        <form
          noValidate
          aria-label={`Correct the pickup of ${money(pickup.amount_minor)}`}
          className="mt-1 flex flex-wrap items-end gap-2"
          onSubmit={form.handleSubmit((v) => correct.mutate(v))}
        >
          <CurrencyAmountField
            id={`${id}-amount`}
            label="Overstated by"
            currency={shift.currency}
            initialMinor={0}
            error={form.formState.errors.amount_minor?.message}
            onChange={(minor) =>
              form.setValue("amount_minor", minor, { shouldValidate: form.formState.isSubmitted })
            }
          />
          <Field id={`${id}-reason`} label="Why" error={form.formState.errors.reason?.message}>
            <input
              id={`${id}-reason`}
              className={`${inputClass} w-64`}
              {...form.register("reason")}
            />
          </Field>
          <Button type="submit" size="sm" disabled={correct.isPending}>
            Correct
          </Button>
        </form>
      ) : (
        <Button
          variant="ghost"
          size="sm"
          aria-label={`Correct the pickup of ${money(pickup.amount_minor)}`}
          onClick={() => setOpen(true)}
        >
          Correct
        </Button>
      )}
    </li>
  );
}

function Counters({ hospital }: { hospital: Hospital }) {
  // A new id for each counter added: a retry repeats the same one.
  const [counterId, setCounterId] = useState(() => newBedId());
  const counters = useApiPagedQuery<CashCounter>(
    ["billing", "counters", hospital.id],
    `/billing/hospitals/${hospital.id}/counters`,
  );
  const form = useZodForm(counterSchema, { code: "", name: "" });
  const save = useApiMutation(
    (values: { code: string; name: string }) =>
      apiFetch<CashCounter>(`/billing/hospitals/${hospital.id}/counters/${counterId}`, {
        method: "PUT",
        body: JSON.stringify(values),
      }),
    {
      successToast: "Counter added",
      invalidate: [["billing", "counters", hospital.id]],
      onSuccess: () => {
        form.reset({ code: "", name: "" });
        setCounterId(newBedId());
      },
    },
  );
  return (
    <div>
      <h4 className="text-sm font-medium">Counters</h4>
      {counters.isError ? (
        <RequestFailure message="Could not load the counters." retry={counters.retry} />
      ) : (
        <ul className="text-sm">
          {counters.items.map((c) => (
            <li key={c.id}>
              {c.code} · {c.name}
            </li>
          ))}
        </ul>
      )}
      <form
        noValidate
        aria-label="Add a counter"
        className="mt-2 flex flex-wrap items-end gap-2"
        onSubmit={form.handleSubmit((v) => save.mutate(v))}
      >
        <Field id="counter-code" label="Code" error={form.formState.errors.code?.message}>
          <input id="counter-code" className={`${inputClass} w-32`} {...form.register("code")} />
        </Field>
        <Field id="counter-name" label="Name" error={form.formState.errors.name?.message}>
          <input id="counter-name" className={`${inputClass} w-56`} {...form.register("name")} />
        </Field>
        <Button type="submit" size="sm" variant="outline" disabled={save.isPending}>
          Add counter
        </Button>
      </form>
    </div>
  );
}
