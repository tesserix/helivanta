"use client";
import { useEffect, useRef, useState } from "react";
import { Badge, Button, Input } from "@tesserix/web";
import {
  ApiError,
  collectionDateSchema,
  dailyCollectionPath,
  fetchDailyCollectionCSV,
  tenantTransitionPending,
  useApiQuery,
  usePermissions,
  type BedCharging,
  type CollectionShift,
  type DailyCollection,
  type Hospital,
} from "@helivanta/api";
import { Field, Panel, RequestFailure, useZodForm } from "@helivanta/ui";
import { formatPharmacyMoney } from "../pharmacy/currency-amount-field";

const money = (minor: number) => formatPharmacyMoney(minor, "INR");

/**
 * The daily collection report (#198 slice 2): a hospital day's shifts and
 * what each took, for finance to sign off. Administrators only.
 */
export function DailyCollectionReport({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  if (!can("billing.collection.read")) return null;
  return <CollectionPanel hospital={hospital} />;
}

function CollectionPanel({ hospital }: { hospital: Hospital }) {
  const setup = useApiQuery<BedCharging>(
    ["billing", "bed-charging", hospital.id],
    `/billing/hospitals/${hospital.id}/bed-charging`,
  );
  const notSetUp = setup.error instanceof ApiError && setup.error.status === 404;
  return (
    <Panel aria-labelledby="collection-heading" className="space-y-4">
      <div>
        <h3 id="collection-heading" className="font-semibold">
          Collections
        </h3>
        <p className="mt-1 text-sm text-muted-foreground">
          Each shift that opened on the day, by the hospital&apos;s date, with the cash it took and
          whether its drawer matched. Open shifts are not counted until their cashier closes them.
        </p>
      </div>
      {setup.isPending ? (
        <p role="status">Loading…</p>
      ) : notSetUp ? (
        <p className="text-sm text-muted-foreground">
          Set the hospital&apos;s timezone under bed charges first: a day runs from its midnights.
        </p>
      ) : setup.isError ? (
        <RequestFailure
          message="Could not load the hospital's timezone."
          retry={() => void setup.refetch()}
        />
      ) : (
        <CollectionDay hospital={hospital} timezone={setup.data.timezone} />
      )}
    </Panel>
  );
}

function CollectionDay({ hospital, timezone }: { hospital: Hospital; timezone: string }) {
  // The hospital's own date (en-CA formats as YYYY-MM-DD).
  const [today] = useState(() =>
    new Intl.DateTimeFormat("en-CA", { timeZone: timezone }).format(new Date()),
  );
  const [date, setDate] = useState(today);
  const form = useZodForm(collectionDateSchema, { date });
  return (
    <>
      <form
        noValidate
        className="flex flex-wrap items-end gap-4"
        onSubmit={form.handleSubmit((v) => setDate(v.date))}
      >
        <Field id="collection-date" label="Day" error={form.formState.errors.date?.message}>
          <Input id="collection-date" type="date" max={today} {...form.register("date")} />
        </Field>
        <Button type="submit" variant="outline">
          Show day
        </Button>
      </form>
      <Day key={date} hospital={hospital} date={date} />
    </>
  );
}

function Day({ hospital, date }: { hospital: Hospital; date: string }) {
  const day = useApiQuery<DailyCollection>(
    ["billing", "collections", hospital.id, date],
    dailyCollectionPath(hospital.id, date),
  );
  if (day.isPending) return <p role="status">Loading the day…</p>;
  if (day.isError)
    return (
      <RequestFailure
        message={day.error instanceof ApiError ? day.error.message : "Could not load the day."}
        retry={() => void day.refetch()}
      />
    );
  const { shifts, totals } = day.data;
  return (
    <div className="space-y-4">
      <p className="text-sm" role="status" aria-label="Day status">
        {day.data.final ? (
          <Badge variant="outline">Final</Badge>
        ) : (
          <Badge variant="secondary">Not final</Badge>
        )}{" "}
        {day.data.final
          ? "Every shift that opened this day is closed."
          : `${totals.open_shifts} open, ${totals.pending_shifts} waiting for authorisation.${totals.open_shifts + totals.pending_shifts === 0 ? " The day is not over." : ""}`}
      </p>
      {shifts.length === 0 ? (
        <p className="text-sm text-muted-foreground">No shifts opened on {date}.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm" aria-label="Shifts">
            <thead className="text-left text-muted-foreground">
              <tr>
                <th className="py-2">Counter</th>
                <th>Cashier</th>
                <th>Status</th>
                <th className="text-right">Payments</th>
                <th className="text-right">Deposits</th>
                <th className="text-right">Refunds</th>
                <th className="text-right">Voided</th>
                <th className="text-right">Expected</th>
                <th className="text-right">Declared</th>
                <th className="text-right">Variance</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {shifts.map((s) => (
                <ShiftRow key={s.shift_id} shift={s} />
              ))}
            </tbody>
          </table>
        </div>
      )}
      <dl
        className="grid grid-cols-2 gap-x-6 gap-y-1 text-sm sm:grid-cols-4"
        aria-label="Day totals"
      >
        <Total label="Payments" value={money(totals.payments_minor)} />
        <Total label="Deposits" value={money(totals.deposits_minor)} />
        <Total label="Refunds" value={money(totals.refunds_minor)} />
        <Total label="Net cash" value={money(totals.net_minor)} />
        <Total label="Shortages" value={money(totals.shortage_minor)} />
        <Total label="Excesses" value={money(totals.excess_minor)} />
        <Total label="Counted shifts" value={String(totals.counted_shifts)} />
        <Total label="Voided receipts" value={String(totals.voided_receipts)} />
      </dl>
      <CollectionExport hospital={hospital} date={date} />
    </div>
  );
}

function Total({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-medium">{value}</dd>
    </div>
  );
}

const statusLabel = {
  open: "Open, not counted",
  pending_authorisation: "Waiting for authorisation",
  closed: "Closed",
} as const;

function ShiftRow({ shift }: { shift: CollectionShift }) {
  const cell = (minor: number | null) => (minor === null ? "—" : money(minor));
  const v = shift.variance_minor;
  return (
    <tr>
      <td className="py-2">
        {shift.counter_code} <span className="text-muted-foreground">{shift.counter_name}</span>
      </td>
      <td>{shift.cashier}</td>
      <td>
        {statusLabel[shift.status]}
        {shift.authorisation_reason && (
          <span className="block text-muted-foreground">
            Authorised by {shift.authorised_by}: {shift.authorisation_reason}
          </span>
        )}
      </td>
      <td className="text-right">{cell(shift.payments_minor)}</td>
      <td className="text-right">{cell(shift.deposits_minor)}</td>
      <td className="text-right">{cell(shift.refunds_minor)}</td>
      <td className="text-right">
        {shift.voided_count === null
          ? "—"
          : shift.voided_count === 0
            ? "None"
            : `${shift.voided_count} · ${cell(shift.voided_minor)}`}
      </td>
      <td className="text-right">{cell(shift.expected_minor)}</td>
      <td className="text-right">{cell(shift.declared_minor)}</td>
      <td className="text-right">
        {v === null ? "—" : v === 0 ? "Matched" : v < 0 ? `Short ${money(-v)}` : `Over ${money(v)}`}
      </td>
    </tr>
  );
}

function CollectionExport({ hospital, date }: { hospital: Hospital; date: string }) {
  const controller = useRef<AbortController | null>(null);
  const [pending, setPending] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  useEffect(() => () => controller.current?.abort(), []);

  async function download() {
    if (controller.current || tenantTransitionPending()) return;
    const current = new AbortController();
    controller.current = current;
    setPending(true);
    setMessage("");
    setError("");
    try {
      const signal = AbortSignal.any([current.signal, AbortSignal.timeout(20_000)]);
      const file = await fetchDailyCollectionCSV(hospital.id, date, signal);
      if (tenantTransitionPending()) return;
      const objectURL = URL.createObjectURL(file.blob);
      try {
        const link = document.createElement("a");
        link.href = objectURL;
        link.download = file.filename;
        link.click();
      } finally {
        URL.revokeObjectURL(objectURL);
      }
      setMessage(`CSV download started: ${file.rows} shifts.`);
    } catch (cause) {
      if (!current.signal.aborted)
        setError(
          cause instanceof ApiError
            ? cause.message
            : cause instanceof Error && cause.name === "TimeoutError"
              ? "CSV preparation timed out. Try again."
              : "Could not download the collection CSV. Try again.",
        );
    } finally {
      if (!current.signal.aborted) {
        controller.current = null;
        setPending(false);
      }
    }
  }

  return (
    <div className="space-y-2">
      <Button variant="outline" disabled={pending} onClick={download}>
        {pending ? "Preparing CSV…" : "Download CSV"}
      </Button>
      <p role="status" aria-label="Collection CSV export" className="text-sm text-muted-foreground">
        {message}
      </p>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}
