"use client";

import { useEffect, useRef, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  fetchPharmacyH1RegisterCSV,
  pharmacyH1RegisterPath,
  pharmacyKey,
  tenantTransitionPending,
  useApiPagedQuery,
  type PharmacyH1Entry,
  type PharmacyScope,
} from "@helivanta/api";
import { Field, LoadMore, Panel, RequestFailure, useZodForm } from "@helivanta/ui";

/**
 * The Schedule H1 register (#1386 C-2; Drugs Rules 1945, Rule 65(3)(h)): each
 * H1 supply as it was recorded at the sale, with what has come back. It is
 * read, never edited, and every read is recorded.
 */
const periodSchema = z
  .object({
    from: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, "Enter the first day"),
    to: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, "Enter the last day"),
  })
  .superRefine((v, ctx) => {
    const days = (Date.parse(v.to) - Date.parse(v.from)) / 86_400_000;
    if (days < 0)
      ctx.addIssue({
        code: "custom",
        path: ["to"],
        message: "The last day is on or after the first",
      });
    else if (days > 365)
      ctx.addIssue({ code: "custom", path: ["to"], message: "A period is at most 366 days" });
  });
type Period = z.infer<typeof periodSchema>;

export function H1Register({ scope, today }: { scope: PharmacyScope; today: string }) {
  const [period, setPeriod] = useState<Period>({ from: today.slice(0, 8) + "01", to: today });
  const form = useZodForm(periodSchema, period);
  return (
    <Panel>
      <h3 className="font-semibold">Schedule H1 register</h3>
      <p className="mt-1 text-sm text-muted-foreground">
        Each Schedule H1 supply, with the prescriber, the patient, the drug and the quantity, as
        recorded at the sale. Returns are shown against their supply. The records are kept and
        cannot be edited. Every view and download is recorded.
      </p>
      <form
        noValidate
        className="mt-4 flex flex-wrap items-end gap-4"
        onSubmit={form.handleSubmit(setPeriod)}
      >
        <Field id="h1-from" label="From" error={form.formState.errors.from?.message}>
          <Input id="h1-from" type="date" {...form.register("from")} />
        </Field>
        <Field id="h1-to" label="To" error={form.formState.errors.to?.message}>
          <Input id="h1-to" type="date" {...form.register("to")} />
        </Field>
        <Button type="submit" variant="outline">
          Show register
        </Button>
      </form>
      <Entries key={`${period.from}:${period.to}`} scope={scope} period={period} />
    </Panel>
  );
}

function Entries({ scope, period }: { scope: PharmacyScope; period: Period }) {
  const entries = useApiPagedQuery<PharmacyH1Entry>(
    pharmacyKey(scope, "h1-register", period.from, period.to),
    pharmacyH1RegisterPath(scope, period.from, period.to),
  );
  return (
    <div className="mt-4 space-y-3">
      <RegisterExport scope={scope} period={period} />
      {entries.isError ? (
        <RequestFailure message="Could not load the H1 register." retry={entries.retry} />
      ) : entries.isPending ? (
        <p role="status">Loading the register…</p>
      ) : entries.items.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          No Schedule H1 supplies from {period.from} to {period.to}.
        </p>
      ) : (
        <table className="w-full text-sm" aria-label="H1 register">
          <thead className="text-left text-muted-foreground">
            <tr>
              <th className="py-2">Date</th>
              <th>Invoice</th>
              <th>Prescriber</th>
              <th>Patient</th>
              <th>Drug</th>
              <th>Batch</th>
              <th>Supplied</th>
              <th>Returned</th>
            </tr>
          </thead>
          <tbody>
            {entries.items.map((e) => (
              <tr key={e.id} className="border-t align-top">
                <td className="py-2">{e.supply_date}</td>
                <td>
                  {e.invoice_number} / {e.line_number}
                </td>
                <td>
                  {e.prescriber_name} ({e.prescriber_registration}), {e.prescriber_address}
                </td>
                <td>
                  {e.patient_name}, {e.patient_address}
                </td>
                <td>
                  {e.brand_name} ({e.generic_name})
                </td>
                <td>
                  {e.lot_number}, exp. {e.expires_on}
                </td>
                <td>{e.quantity_units}</td>
                <td>{e.returned_units}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <LoadMore
        label="Load more entries"
        hasMore={entries.hasMore}
        isLoading={entries.isFetchingMore}
        onClick={entries.loadMore}
      />
    </div>
  );
}

function RegisterExport({ scope, period }: { scope: PharmacyScope; period: Period }) {
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
      const file = await fetchPharmacyH1RegisterCSV(scope, period.from, period.to, signal);
      if (tenantTransitionPending()) return;
      const url = URL.createObjectURL(file.blob);
      try {
        const link = document.createElement("a");
        link.href = url;
        link.download = file.filename;
        link.click();
      } finally {
        URL.revokeObjectURL(url);
      }
      setMessage("CSV download started.");
    } catch (cause) {
      if (!current.signal.aborted)
        setError(
          cause instanceof ApiError
            ? cause.message
            : "Could not download the register CSV. Try again.",
        );
    } finally {
      if (!current.signal.aborted) {
        controller.current = null;
        setPending(false);
      }
    }
  }
  return (
    <div className="space-y-1">
      <Button variant="outline" disabled={pending} onClick={download}>
        {pending ? "Preparing CSV…" : "Download register CSV"}
      </Button>
      <p role="status" aria-label="H1 register export" className="text-sm text-muted-foreground">
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
