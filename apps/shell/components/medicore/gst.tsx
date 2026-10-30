"use client";
import { useState } from "react";
import type { z } from "zod";
import { Button, Input, Textarea } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  bedClassLabel,
  gstRegistrationSchema,
  newBedId,
  taxInvoiceSchema,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type BedCharging,
  type GstRegistration,
  type Hospital,
  type TaxInvoice,
  type TaxInvoiceLine,
} from "@helivanta/api";
import { Field, LoadMore, Panel, RequestFailure, formatDateTime, useZodForm } from "@helivanta/ui";
import { formatPharmacyMoney } from "../pharmacy/currency-amount-field";

const money = (minor: number) => formatPharmacyMoney(minor, "INR");

/**
 * The hospital's GST registration (#193), for administrators: set once. From
 * its date, a room other than ICU or NICU charged above ₹5,000 a day is taxed
 * at 5% (2.5% CGST + 2.5% SGST), and the desk can issue tax invoices.
 */
export function GstSetup({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  if (!can("billing.tax.manage")) return null;
  return <GstPanel hospital={hospital} />;
}

function GstPanel({ hospital }: { hospital: Hospital }) {
  const registration = useApiQuery<GstRegistration>(
    ["billing", "gst-registration", hospital.id],
    `/billing/hospitals/${hospital.id}/gst-registration`,
  );
  const none = registration.error instanceof ApiError && registration.error.status === 404;
  // A registration is dated by the hospital's own day, as the server checks it.
  const charging = useApiQuery<BedCharging>(
    ["billing", "bed-charging", hospital.id],
    `/billing/hospitals/${hospital.id}/bed-charging`,
    { enabled: none },
  );
  const noZone = charging.error instanceof ApiError && charging.error.status === 404;
  return (
    <Panel aria-labelledby="gst-heading" className="space-y-3">
      <div>
        <h3 id="gst-heading" className="font-semibold">
          GST at {hospital.name}
        </h3>
        <p className="mt-1 text-sm text-muted-foreground">
          Health care is exempt from GST, except a room other than ICU or NICU charged above ₹5,000
          a day, taxed at 5% (Notifications 03/2022 and 04/2022-Central Tax (Rate)). Medicine is
          taxed on the pharmacy&apos;s own invoice.
        </p>
      </div>
      {registration.isPending ? null : none ? (
        noZone ? (
          <p className="text-sm text-muted-foreground">
            Set the hospital&apos;s timezone under bed charges first: GST is dated by its day.
          </p>
        ) : charging.isError ? (
          <RequestFailure
            message="Could not load the hospital's timezone."
            retry={() => void charging.refetch()}
          />
        ) : charging.data ? (
          <RegistrationForm hospital={hospital} timezone={charging.data.timezone} />
        ) : null
      ) : registration.isError ? (
        <RequestFailure
          message="Could not load the GST registration."
          retry={() => void registration.refetch()}
        />
      ) : (
        <dl className="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-2" aria-label="GST registration">
          <dt className="text-muted-foreground">GSTIN</dt>
          <dd className="font-medium">{registration.data.gstin}</dd>
          <dt className="text-muted-foreground">Legal name</dt>
          <dd>{registration.data.legal_name}</dd>
          <dt className="text-muted-foreground">Address</dt>
          <dd>{registration.data.address}</dd>
          <dt className="text-muted-foreground">Taxed from</dt>
          <dd>
            {registration.data.effective_from}
            {registration.data.applied_at === null && " (being applied)"}
          </dd>
        </dl>
      )}
    </Panel>
  );
}

function RegistrationForm({ hospital, timezone }: { hospital: Hospital; timezone: string }) {
  // The hospital's own date (en-CA formats as YYYY-MM-DD).
  const [today] = useState(() =>
    new Intl.DateTimeFormat("en-CA", { timeZone: timezone }).format(new Date()),
  );
  const form = useZodForm(gstRegistrationSchema, {
    gstin: "",
    legal_name: "",
    address: "",
    effective_from: today,
  });
  const save = useApiMutation(
    (values: z.infer<typeof gstRegistrationSchema>) =>
      apiFetch<GstRegistration>(`/billing/hospitals/${hospital.id}/gst-registration`, {
        method: "PUT",
        body: JSON.stringify(values),
      }),
    {
      successToast: "GST registration recorded",
      invalidate: [["billing", "gst-registration", hospital.id]],
    },
  );
  const errors = form.formState.errors;
  return (
    <form
      noValidate
      aria-label="Record the GST registration"
      className="grid gap-4 sm:grid-cols-2"
      onSubmit={form.handleSubmit((values) => save.mutate(values))}
    >
      <Field id="gst-gstin" label="GSTIN" error={errors.gstin?.message}>
        <Input id="gst-gstin" autoComplete="off" {...form.register("gstin")} />
      </Field>
      <Field id="gst-from" label="Taxed from" error={errors.effective_from?.message}>
        <Input id="gst-from" type="date" min={today} {...form.register("effective_from")} />
      </Field>
      <Field id="gst-name" label="Legal name" error={errors.legal_name?.message}>
        <Input id="gst-name" {...form.register("legal_name")} />
      </Field>
      <Field id="gst-address" label="Registered address" error={errors.address?.message}>
        <Textarea id="gst-address" rows={2} {...form.register("address")} />
      </Field>
      <div className="sm:col-span-2 space-y-1">
        <Button type="submit" variant="outline" disabled={save.isPending}>
          Record GST registration
        </Button>
        <p className="text-sm text-muted-foreground">
          Set once, from today or later: it cannot be changed or backdated here.
        </p>
      </div>
    </form>
  );
}

/**
 * An admission's tax invoices (#193): each an invoice-cum-bill of supply for
 * the room days not yet invoiced, issued once a final bill exists.
 */
export function TaxInvoices({
  hospital,
  admissionId,
  hasFinalBill,
}: {
  hospital: Hospital;
  admissionId: string;
  hasFinalBill: boolean;
}) {
  const { can } = usePermissions();
  const [shown, setShown] = useState<string | null>(null);
  const path = `/billing/hospitals/${hospital.id}/admissions/${admissionId}/tax-invoices`;
  const key = ["billing", "tax-invoices", hospital.id, admissionId];
  const invoices = useApiPagedQuery<TaxInvoice>(key, path);
  return (
    <section aria-label="Tax invoices" className="space-y-2 border-t border-border pt-2">
      <p className="text-sm font-medium">Tax invoices</p>
      {invoices.isError ? (
        <RequestFailure message="Could not load the tax invoices." retry={invoices.retry} />
      ) : invoices.items.length === 0 && !invoices.isPending ? (
        <p className="text-sm text-muted-foreground">No tax invoice yet.</p>
      ) : (
        <ul className="space-y-1 text-sm">
          {invoices.items.map((i) => (
            <li key={i.id}>
              <Button
                variant="link"
                size="sm"
                className="h-auto p-0"
                aria-expanded={shown === i.id}
                onClick={() => setShown(shown === i.id ? null : i.id)}
              >
                {i.number}
              </Button>
              <span className="ml-2 text-muted-foreground">
                {i.issue_date} · {money(i.total_minor)}
              </span>
              {shown === i.id && <InvoiceDetail invoice={i} path={`${path}/${i.id}`} />}
            </li>
          ))}
        </ul>
      )}
      <LoadMore
        label="Load more tax invoices"
        hasMore={invoices.hasMore}
        isLoading={invoices.isFetchingMore}
        onClick={invoices.loadMore}
      />
      {can("billing.bill.issue") && hasFinalBill && (
        <IssueForm path={path} invalidate={key} onIssued={setShown} />
      )}
    </section>
  );
}

function IssueForm({
  path,
  invalidate,
  onIssued,
}: {
  path: string;
  invalidate: string[];
  onIssued: (id: string) => void;
}) {
  const [id, setId] = useState(() => newBedId());
  const form = useZodForm(taxInvoiceSchema, { recipient_name: "", recipient_address: "" });
  const issue = useApiMutation(
    (values: z.infer<typeof taxInvoiceSchema>) =>
      apiFetch<TaxInvoice>(`${path}/${id}`, { method: "PUT", body: JSON.stringify(values) }),
    {
      successToast: "Tax invoice issued",
      invalidate: [invalidate],
      onSuccess: (invoice) => {
        setId(newBedId());
        form.reset();
        onIssued(invoice.id);
      },
    },
  );
  const errors = form.formState.errors;
  return (
    <form
      noValidate
      aria-label="Issue a tax invoice"
      className="grid gap-3 sm:grid-cols-2"
      onSubmit={form.handleSubmit((values) => issue.mutate(values))}
    >
      <Field id={`tax-name-${id}`} label="Bill to" error={errors.recipient_name?.message}>
        <Input id={`tax-name-${id}`} {...form.register("recipient_name")} />
      </Field>
      <Field id={`tax-address-${id}`} label="Address" error={errors.recipient_address?.message}>
        <Textarea id={`tax-address-${id}`} rows={2} {...form.register("recipient_address")} />
      </Field>
      <div className="sm:col-span-2">
        <Button type="submit" size="sm" variant="outline" disabled={issue.isPending}>
          Issue tax invoice
        </Button>
      </div>
    </form>
  );
}

function InvoiceDetail({ invoice, path }: { invoice: TaxInvoice; path: string }) {
  const lines = useApiPagedQuery<TaxInvoiceLine>(
    ["billing", "tax-invoice-lines", path],
    `${path}/lines`,
  );
  return (
    <section
      aria-label={`Tax invoice ${invoice.number}`}
      className="mt-2 space-y-2 border-t border-border pt-2"
    >
      <p className="font-medium">Invoice-cum-bill of supply {invoice.number}</p>
      <p className="text-xs text-muted-foreground">
        {invoice.supplier_name}, {invoice.supplier_address} · GSTIN {invoice.gstin} · Place of
        supply: state {invoice.state_code} · Issued {formatDateTime(invoice.issued_at)} by{" "}
        {invoice.issued_by}
      </p>
      <p className="text-xs text-muted-foreground">
        To {invoice.recipient_name}, {invoice.recipient_address}
      </p>
      {lines.isError ? (
        <RequestFailure message="Could not load the invoice lines." retry={lines.retry} />
      ) : (
        <table className="w-full text-sm" aria-label="Invoice lines">
          <thead className="text-left text-muted-foreground">
            <tr>
              <th className="py-1">Room day</th>
              <th>SAC</th>
              <th className="text-right">Value</th>
              <th className="text-right">CGST</th>
              <th className="text-right">SGST</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {lines.items.map((l) => (
              <tr key={l.entry_id}>
                <td className="py-1">
                  {l.census_date} · {bedClassLabel[l.bed_class] ?? l.bed_class}
                  {!l.taxable && <span className="text-muted-foreground"> · exempt</span>}
                </td>
                <td>{l.sac}</td>
                <td className="text-right">{money(l.value_minor)}</td>
                <td className="text-right">
                  {l.taxable ? `${l.cgst_bp / 100}% ${money(l.cgst_minor)}` : "—"}
                </td>
                <td className="text-right">
                  {l.taxable ? `${l.sgst_bp / 100}% ${money(l.sgst_minor)}` : "—"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <LoadMore
        label="Load more invoice lines"
        hasMore={lines.hasMore}
        isLoading={lines.isFetchingMore}
        onClick={lines.loadMore}
      />
      <dl className="grid grid-cols-2 gap-x-6 text-sm sm:grid-cols-3" aria-label="Invoice totals">
        <Total label="Taxable value" value={money(invoice.taxable_minor)} />
        <Total label="Exempt value" value={money(invoice.exempt_minor)} />
        <Total label="CGST" value={money(invoice.cgst_minor)} />
        <Total label="SGST" value={money(invoice.sgst_minor)} />
        <Total label="Invoice total" value={money(invoice.total_minor)} />
      </dl>
    </section>
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
