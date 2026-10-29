"use client";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  bedChargingSchema,
  bedClassLabel,
  bedRateSchema,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type BedCharging,
  type BedRate,
  type Hospital,
} from "@helivanta/api";
import { Field, FormSelectField, LoadMore, Panel, RequestFailure, useZodForm } from "@helivanta/ui";
import {
  CurrencyAmountField,
  formatPharmacyMoney,
  supportedCurrencies,
} from "../pharmacy/currency-amount-field";
import { useBillingAccess } from "./admission-account";

/**
 * A hospital's bed charges (#197 slice 2): its timezone, set once, and a daily
 * rate per bed class from a date. Administrators holding the billing grant only.
 */
export function BedChargingSetup({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  const granted = useBillingAccess(hospital);
  if (!can("billing.rate.manage") || !granted) return null;
  return <BedChargingPanel hospital={hospital} />;
}

function BedChargingPanel({ hospital }: { hospital: Hospital }) {
  const setup = useApiQuery<BedCharging>(
    ["billing", "bed-charging", hospital.id],
    `/billing/hospitals/${hospital.id}/bed-charging`,
  );
  const notSetUp = setup.error instanceof ApiError && setup.error.status === 404;
  return (
    <Panel aria-labelledby="bed-charging-heading" className="space-y-4">
      <div>
        <h3 id="bed-charging-heading" className="font-semibold">
          Bed charges at {hospital.name}
        </h3>
        <p className="mt-1 text-sm text-muted-foreground">
          A bed day is charged for each midnight a patient spends in a bed, at that bed&apos;s
          class; a stay that crosses no midnight is one day. Rates are before tax.
        </p>
      </div>
      {setup.isPending ? null : notSetUp ? (
        <TimezoneForm hospital={hospital} />
      ) : setup.isError ? (
        <RequestFailure message="Could not load bed charging." retry={() => void setup.refetch()} />
      ) : (
        <>
          <p className="text-sm">Midnights are counted in {setup.data.timezone}.</p>
          <Rates hospital={hospital} />
          <RateForm hospital={hospital} />
        </>
      )}
    </Panel>
  );
}

function TimezoneForm({ hospital }: { hospital: Hospital }) {
  const form = useZodForm(bedChargingSchema, { timezone: "Asia/Kolkata" });
  const save = useApiMutation(
    (values: z.infer<typeof bedChargingSchema>) =>
      apiFetch<BedCharging>(`/billing/hospitals/${hospital.id}/bed-charging`, {
        method: "PUT",
        body: JSON.stringify(values),
      }),
    {
      successToast: "Timezone set",
      invalidate: [
        ["billing", "bed-charging", hospital.id],
        ["billing", "account", hospital.id],
      ],
    },
  );
  return (
    <form
      noValidate
      aria-label="Set the hospital's timezone"
      className="grid items-end gap-4 sm:grid-cols-3"
      onSubmit={form.handleSubmit((values) => save.mutate(values))}
    >
      <Field
        id="bed-charging-timezone"
        label="Timezone"
        error={form.formState.errors.timezone?.message}
      >
        <Input id="bed-charging-timezone" {...form.register("timezone")} />
      </Field>
      <Button type="submit" variant="outline" disabled={save.isPending}>
        Set timezone
      </Button>
      <p className="text-sm text-muted-foreground sm:col-span-3">
        Set once: it cannot be changed, because days already charged were counted by it.
      </p>
    </form>
  );
}

function Rates({ hospital }: { hospital: Hospital }) {
  const rates = useApiPagedQuery<BedRate>(
    ["billing", "bed-rates", hospital.id],
    `/billing/hospitals/${hospital.id}/bed-rates`,
  );
  if (rates.isError)
    return <RequestFailure message="Could not load the bed rates." retry={rates.retry} />;
  return (
    <div>
      {rates.items.length === 0 && !rates.isPending ? (
        <p className="text-sm text-muted-foreground">
          No rates yet: bed days are recorded as not priced until one is set.
        </p>
      ) : (
        <ul aria-label="Bed rates" className="divide-y text-sm">
          {rates.items.map((r) => (
            <li key={r.id} className="py-2">
              {bedClassLabel[r.bed_class]} · {formatPharmacyMoney(r.daily_rate_minor, r.currency)} a
              day from {r.effective_from}
            </li>
          ))}
        </ul>
      )}
      <LoadMore
        label="Load more rates"
        hasMore={rates.hasMore}
        isLoading={rates.isFetchingMore}
        onClick={rates.loadMore}
      />
    </div>
  );
}

function RateForm({ hospital }: { hospital: Hospital }) {
  const form = useZodForm(bedRateSchema, {
    bed_class: "general",
    effective_from: "",
    daily_rate_minor: Number.NaN,
    currency: supportedCurrencies.includes("INR") ? "INR" : (supportedCurrencies[0] ?? ""),
  });
  const currency = form.watch("currency");
  const save = useApiMutation(
    ({ bed_class, effective_from, ...body }: z.infer<typeof bedRateSchema>) =>
      apiFetch<BedRate>(
        `/billing/hospitals/${hospital.id}/bed-rates/${bed_class}/${effective_from}`,
        { method: "PUT", body: JSON.stringify(body) },
      ),
    {
      successToast: "Rate set",
      invalidate: [
        ["billing", "bed-rates", hospital.id],
        ["billing", "account", hospital.id],
      ],
    },
  );
  return (
    <form
      noValidate
      aria-label="Set a bed rate"
      className="grid items-end gap-4 sm:grid-cols-4"
      onSubmit={form.handleSubmit((values) => save.mutate(values))}
    >
      <FormSelectField
        control={form.control}
        name="bed_class"
        id="bed-rate-class"
        label="Bed class"
        options={Object.entries(bedClassLabel).map(([value, label]) => ({ value, label }))}
      />
      <CurrencyAmountField
        key={currency}
        id="bed-rate-amount"
        label="Daily rate"
        currency={currency}
        initialMinor={0}
        error={form.formState.errors.daily_rate_minor?.message}
        onChange={(minor) =>
          form.setValue("daily_rate_minor", minor, {
            shouldValidate: form.formState.isSubmitted,
          })
        }
      />
      <Field id="bed-rate-from" label="From" error={form.formState.errors.effective_from?.message}>
        <Input id="bed-rate-from" type="date" {...form.register("effective_from")} />
      </Field>
      <Button type="submit" variant="outline" disabled={save.isPending}>
        Set rate
      </Button>
    </form>
  );
}
