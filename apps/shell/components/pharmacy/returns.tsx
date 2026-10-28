"use client";

import { useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  newPharmacyId,
  pharmacyKey,
  pharmacyRoot,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  type PharmacyCreditNote,
  type PharmacyLocation,
  type PharmacySale,
  type PharmacyScope,
} from "@helivanta/api";
import { Field, FormSelectField, LoadMore, Panel, RequestFailure, useZodForm } from "@helivanta/ui";
import { formatPharmacyMoney } from "./currency-amount-field";

/**
 * Returns at the counter (#1386 C-1): a return is a GST credit note against
 * one posted invoice (CGST Act s.34, Rule 53(1A)). Returned medicine goes to
 * quarantine; a pharmacist releases it before it can be sold again.
 */
export function Returns({ scope, canReturn }: { scope: PharmacyScope; canReturn: boolean }) {
  const sales = useApiPagedQuery<PharmacySale>(
    pharmacyKey(scope, "returns", "sales"),
    `${pharmacyRoot(scope)}/sales?limit=50`,
  );
  const [chosen, setChosen] = useState<string | null>(null);
  const [issued, setIssued] = useState<PharmacyCreditNote | null>(null);
  if (issued)
    return (
      <CreditNoteDocument
        note={issued}
        onNext={() => {
          setIssued(null);
          setChosen(null);
        }}
      />
    );
  if (chosen)
    return (
      <ReturnForm
        key={chosen}
        scope={scope}
        saleId={chosen}
        canReturn={canReturn}
        onIssued={setIssued}
        onBack={() => setChosen(null)}
      />
    );
  return (
    <Panel>
      <h3 className="font-semibold">Returns</h3>
      <p className="mt-1 text-sm text-muted-foreground">
        Choose the invoice the medicine was sold on. A return issues a credit note against it, and
        the medicine goes to quarantine until a pharmacist releases it.
      </p>
      {sales.isError ? (
        <RequestFailure message="Could not load this outlet's invoices." retry={sales.retry} />
      ) : sales.isPending ? (
        <p role="status">Loading invoices…</p>
      ) : sales.items.length === 0 ? (
        <p className="mt-3 text-sm text-muted-foreground">No invoices at this outlet yet.</p>
      ) : (
        <ul className="mt-3 divide-y" aria-label="Invoices">
          {sales.items.map((s) => (
            <li key={s.id} className="flex flex-wrap items-center justify-between gap-3 py-3">
              <span className="text-sm">
                {s.invoice_number} · {s.sale_date} ·{" "}
                {formatPharmacyMoney(s.total_minor, s.currency)}
              </span>
              <Button variant="outline" onClick={() => setChosen(s.id)}>
                Return from {s.invoice_number}
              </Button>
            </li>
          ))}
        </ul>
      )}
      <LoadMore
        label="Load more invoices"
        hasMore={sales.hasMore}
        isLoading={sales.isFetchingMore}
        onClick={sales.loadMore}
      />
    </Panel>
  );
}

const returnSchema = z
  .object({
    units: z.array(
      z
        .string()
        .trim()
        .regex(/^[0-9]{0,7}$/, "Enter whole units"),
    ),
    left: z.array(z.number()),
    location: z.string().min(1, "Choose the quarantine location"),
    name: z.string().trim().min(1, "Enter the name of the person refunded").max(200),
    address: z.string().trim().min(1, "Enter their address").max(500),
    reason: z.string().trim().min(5, "Give the reason, at least 5 characters").max(200),
    kind: z.enum(["cash", "upi", "card"]),
    reference: z.string().trim().max(64, "At most 64 characters"),
  })
  .superRefine((v, ctx) => {
    let any = false;
    v.units.forEach((u, i) => {
      const n = u === "" ? 0 : Number(u);
      if (n > v.left[i])
        ctx.addIssue({
          code: "custom",
          path: ["units", i],
          message: `At most ${v.left[i]} left to return`,
        });
      if (n > 0) any = true;
    });
    if (!any)
      ctx.addIssue({ code: "custom", path: ["units"], message: "Enter the units coming back" });
    if (v.kind !== "cash" && v.reference === "")
      ctx.addIssue({ code: "custom", path: ["reference"], message: "Enter the refund reference" });
  });
type ReturnValues = z.infer<typeof returnSchema>;

function ReturnForm({
  scope,
  saleId,
  canReturn,
  onIssued,
  onBack,
}: {
  scope: PharmacyScope;
  saleId: string;
  canReturn: boolean;
  onIssued: (note: PharmacyCreditNote) => void;
  onBack: () => void;
}) {
  const sale = useApiQuery<PharmacySale>(
    pharmacyKey(scope, "returns", "sale", saleId),
    `${pharmacyRoot(scope)}/sales/${saleId}`,
  );
  const locations = useApiPagedQuery<PharmacyLocation>(
    pharmacyKey(scope, "returns", "locations"),
    `${pharmacyRoot(scope)}/stock/locations?limit=100`,
  );
  if (sale.isError)
    return <RequestFailure message="Could not load this invoice." retry={sale.refetch} />;
  if (!sale.data || locations.isPending) return <p role="status">Loading invoice…</p>;
  return (
    <ReturnFormFor
      scope={scope}
      sale={sale.data}
      quarantine={locations.items.filter((l) => l.stock_status === "quarantine")}
      canReturn={canReturn}
      onIssued={onIssued}
      onBack={onBack}
    />
  );
}

function ReturnFormFor({
  scope,
  sale,
  quarantine,
  canReturn,
  onIssued,
  onBack,
}: {
  scope: PharmacyScope;
  sale: PharmacySale;
  quarantine: PharmacyLocation[];
  canReturn: boolean;
  onIssued: (note: PharmacyCreditNote) => void;
  onBack: () => void;
}) {
  // One id per return being entered, so a retry after a lost response replays it.
  const [id, setId] = useState(() => newPharmacyId());
  const left = sale.lines.map((l) => l.quantity_units - (l.returned_units ?? 0));
  const form = useZodForm(returnSchema, {
    units: sale.lines.map(() => ""),
    left,
    location: "",
    name: sale.buyer_name ?? "",
    address: "",
    reason: "",
    kind: "cash",
    reference: "",
  });
  const kind = form.watch("kind");
  const issue = useApiMutation(
    (v: ReturnValues) =>
      apiFetch<PharmacyCreditNote>(`${pharmacyRoot(scope)}/credit-notes/${id}`, {
        method: "PUT",
        body: JSON.stringify({
          sale_id: sale.id,
          lines: sale.lines
            .map((l, i) => ({ sale_line: l.line_number, quantity_units: Number(v.units[i] || 0) }))
            .filter((l) => l.quantity_units > 0)
            .map((l) => ({ ...l, quarantine_location_id: v.location })),
          // The server prices the credit; the refund is collected after it says the total.
          refunds: [refund(v, preview(sale, v.units))],
          reason: v.reason,
          recipient: { name: v.name, address: v.address },
        }),
      }),
    {
      successToast: "Credit note issued",
      invalidate: [pharmacyKey(scope, "returns"), pharmacyKey(scope, "stock")],
      onSuccess: (note) => {
        setId(newPharmacyId());
        onIssued(note);
      },
      suppressErrorToast: true,
    },
  );
  const refused =
    issue.error instanceof ApiError &&
    issue.error.status === 409 &&
    issue.error.code === "not_returnable";
  const errors = form.formState.errors;
  const units = form.watch("units");
  const total = preview(sale, units);
  return (
    <Panel>
      <div className="flex items-start justify-between gap-4">
        <h3 className="font-semibold">
          Return from {sale.invoice_number} · {sale.sale_date}
        </h3>
        <Button variant="ghost" onClick={onBack}>
          Choose another invoice
        </Button>
      </div>
      {!canReturn ? (
        <p className="mt-2 text-sm text-muted-foreground">
          Taking a return needs the pharmacist role and editor access to this outlet.
        </p>
      ) : (
        <form
          noValidate
          className="mt-4 space-y-4"
          onSubmit={form.handleSubmit((v) => issue.mutate(v))}
        >
          <table className="w-full text-sm">
            <thead className="text-left text-muted-foreground">
              <tr>
                <th className="py-2">Line</th>
                <th>Batch</th>
                <th>Sold</th>
                <th>Left to return</th>
                <th>Returning</th>
              </tr>
            </thead>
            <tbody>
              {sale.lines.map((l, i) => (
                <tr key={l.line_number} className="border-t">
                  <td className="py-2">{l.line_number}</td>
                  <td>{l.lot_number}</td>
                  <td>{l.quantity_units}</td>
                  <td>{left[i]}</td>
                  <td>
                    <Field
                      id={`return-${i}`}
                      label={`Units returned on line ${l.line_number}`}
                      error={errors.units?.[i]?.message}
                    >
                      <Input
                        id={`return-${i}`}
                        inputMode="numeric"
                        disabled={left[i] === 0}
                        {...form.register(`units.${i}`)}
                      />
                    </Field>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {errors.units?.message && (
            <p role="alert" className="text-sm text-destructive">
              {errors.units.message}
            </p>
          )}
          <div className="grid gap-4 sm:grid-cols-3">
            <FormSelectField
              control={form.control}
              name="location"
              id="return-location"
              label="Quarantine location"
              options={quarantine.map((l) => ({ value: l.id, label: `${l.code} · ${l.name}` }))}
            />
            <Field id="return-name" label="Refunded to" error={errors.name?.message}>
              <Input id="return-name" {...form.register("name")} />
            </Field>
            <Field id="return-address" label="Their address" error={errors.address?.message}>
              <Input id="return-address" {...form.register("address")} />
            </Field>
            <Field id="return-reason" label="Reason" error={errors.reason?.message}>
              <Input id="return-reason" {...form.register("reason")} />
            </Field>
            <FormSelectField
              control={form.control}
              name="kind"
              id="return-kind"
              label="Refund by"
              options={[
                { value: "cash", label: "Cash" },
                { value: "upi", label: "UPI" },
                { value: "card", label: "Card" },
              ]}
            />
            {kind !== "cash" && (
              <Field
                id="return-reference"
                label="Refund reference"
                error={errors.reference?.message}
              >
                <Input id="return-reference" maxLength={64} {...form.register("reference")} />
              </Field>
            )}
          </div>
          <p className="text-sm">Refund {formatPharmacyMoney(total, sale.currency)}</p>
          {issue.isError && (
            <div role="alert" className="text-sm text-destructive">
              <p>{issue.error.message}</p>
              {!refused && (
                <p className="mt-1">
                  The credit note may have been issued. Try again: the same return is never issued
                  twice.
                </p>
              )}
            </div>
          )}
          <Button type="submit" disabled={issue.isPending}>
            {issue.isPending
              ? "Issuing…"
              : issue.isError && !refused
                ? "Try again"
                : "Issue credit note"}
          </Button>
        </form>
      )}
    </Panel>
  );
}

// preview is the refund the server will credit: the cumulative pro-rata step
// of each line's taxable value and of each tax half, rounded down, summed
// (#1386 C-1 decision 2).
function preview(sale: PharmacySale, units: string[]): number {
  return sale.lines.reduce((sum, l, i) => {
    const r = Number(units[i] || 0);
    const k = l.returned_units ?? 0;
    if (r <= 0) return sum;
    const prorate = (v: number) =>
      Math.floor((v * (k + r)) / l.quantity_units) - Math.floor((v * k) / l.quantity_units);
    return sum + prorate(l.taxable_minor) + 2 * prorate(l.cgst_minor);
  }, 0);
}

function refund(v: ReturnValues, amount: number) {
  return v.kind === "cash"
    ? { kind: "cash", amount_minor: amount }
    : { kind: v.kind, amount_minor: amount, reference: v.reference };
}

/** The credit note as Rule 53(1A) requires it printed. */
function CreditNoteDocument({ note, onNext }: { note: PharmacyCreditNote; onNext: () => void }) {
  const money = (minor: number) => formatPharmacyMoney(minor, note.currency);
  return (
    <Panel>
      <article aria-label={`Credit note ${note.credit_note_number}`}>
        <h3 className="font-semibold">Credit note {note.credit_note_number}</h3>
        <p className="text-sm">
          {note.seller_legal_name}, {note.seller_address} · GSTIN {note.seller_gstin}
        </p>
        <p className="text-sm">
          Date {note.credit_note_date} · Against invoice {note.invoice_number} of{" "}
          {note.invoice_date}
        </p>
        <p className="text-sm">
          Refunded to {note.recipient_name}, {note.recipient_address} · State {note.place_of_supply}
        </p>
        <table className="mt-4 w-full text-sm">
          <thead className="text-left text-muted-foreground">
            <tr>
              <th className="py-2">HSN</th>
              <th>Batch</th>
              <th>Units</th>
              <th>Rate</th>
              <th>Taxable</th>
              <th>CGST + SGST</th>
              <th>Credited</th>
            </tr>
          </thead>
          <tbody>
            {note.lines.map((l) => (
              <tr key={l.line_number} className="border-t">
                <td className="py-2">{l.classification_code}</td>
                <td>{l.lot_number}</td>
                <td>{l.quantity_units}</td>
                <td>{l.rate_ppm / 10000}%</td>
                <td>{money(l.taxable_minor)}</td>
                <td>
                  {money(l.cgst_minor)} + {money(l.sgst_minor)}
                </td>
                <td>{money(l.amount_minor)}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <p className="mt-3 text-sm font-medium">Total credited {money(note.total_minor)}</p>
        <p className="text-sm">Reason: {note.reason}</p>
        <p className="mt-2 text-xs text-muted-foreground">
          Issued by {note.actor_subject}. Returned medicine is in quarantine until released.
        </p>
      </article>
      <div className="mt-4 flex gap-2 print:hidden">
        <Button type="button" variant="outline" onClick={() => window.print()}>
          Print credit note
        </Button>
        <Button type="button" onClick={onNext}>
          New return
        </Button>
      </div>
    </Panel>
  );
}
