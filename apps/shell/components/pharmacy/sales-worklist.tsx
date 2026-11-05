"use client";

import { useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  pharmacyKey,
  pharmacySaleSummaryPath,
  pharmacySalesWorklistPath,
  useApiPagedQuery,
  useApiQuery,
  type PharmacySaleCustomer,
  type PharmacySaleLinkage,
  type PharmacySaleSummary,
  type PharmacySaleWorkRow,
  type PharmacySalesWorklistFilter,
  type PharmacyScope,
} from "@helivanta/api";
import {
  DetailHeader,
  Field,
  FormSelectField,
  ListDetail,
  LoadMore,
  Panel,
  RequestFailure,
  useZodForm,
} from "@helivanta/ui";
import { useListDetail } from "@/lib/use-list-detail";
import { ReportDownload } from "./sales-reports";
import { formatPharmacyMoney } from "./currency-amount-field";
import { periodSchema } from "./period";

export const linkageLabel: Record<PharmacySaleLinkage, string> = {
  admission: "Admission",
  opd: "OPD visit",
  none: "No visit",
};

export const customerLabel: Record<PharmacySaleCustomer, string> = {
  walk_in: "Walk-in",
  patient: "Registered patient",
  admission: "Admission",
};

const kindLabel: Record<string, string> = {
  cash: "Cash",
  upi: "UPI",
  upi_verified: "UPI (verified)",
  card: "Card",
  admission_account: "Admission account",
};

const filterSchema = periodSchema.and(
  z.object({
    customer: z.enum(["all", "walk_in", "patient", "admission"]),
    linkage: z.enum(["all", "admission", "opd", "none"]),
    // Invoice numbers are stored in capitals, at most 16 characters.
    invoice: z.string().trim().toUpperCase().max(16, "An invoice number is at most 16 characters"),
  }),
);
type FilterValues = z.infer<typeof filterSchema>;

/**
 * The sales worklist (#987 slice 1): an outlet's sales by date or exact
 * invoice, newest first, each opening its reconciled summary beside the
 * list. Neither shows who the buyer or patient was; the invoice does.
 */
export function SalesWorklist({ scope, today }: { scope: PharmacyScope; today: string }) {
  const [filter, setFilter] = useState<PharmacySalesWorklistFilter>({ from: today, to: today });
  const form = useZodForm(filterSchema, {
    from: today,
    to: today,
    customer: "all",
    linkage: "all",
    invoice: "",
  });
  const apply = (v: FilterValues) => {
    const customer = {
      ...(v.customer === "all" ? {} : { customer: v.customer }),
      ...(v.linkage === "all" ? {} : { linkage: v.linkage }),
    };
    // An invoice number is found exactly, whatever the period.
    setFilter(
      v.invoice ? { invoice: v.invoice, ...customer } : { from: v.from, to: v.to, ...customer },
    );
  };
  const path = pharmacySalesWorklistPath(scope, filter);
  const sales = useApiPagedQuery<PharmacySaleWorkRow>(
    pharmacyKey(scope, "sales-worklist", JSON.stringify(filter)),
    path,
  );
  const pane = useListDetail({
    rows: sales.items,
    name: (row) => `invoice ${row.invoice_number}`,
    fallbackName: "this sale",
    newName: "this sale",
    headingId: "sales-worklist-heading",
    // Undefined while the new filter's rows load, so stepping is retaken from them.
    snapshotKey: sales.isPending ? undefined : JSON.stringify(filter),
  });
  const { detail, paneRef, rowProps, openProps } = pane;
  const record = detail.openId ? (
    <SaleSummaryPane key={detail.openId} scope={scope} id={detail.openId} pane={pane} />
  ) : null;

  return (
    <div className="space-y-4" onClickCapture={pane.holdLinks}>
      <Panel>
        <h3 id="sales-worklist-heading" tabIndex={-1} className="font-semibold outline-none">
          Sales
        </h3>
        <p className="mt-1 text-sm text-muted-foreground">
          Every sale at this outlet, newest first, with what has been credited back and what is
          still on an admission account. Open a sale for its statement.
        </p>
        <form
          noValidate
          className="mt-4 flex flex-wrap items-end gap-4"
          onSubmit={form.handleSubmit(apply)}
        >
          <Field id="sales-from" label="From" error={form.formState.errors.from?.message}>
            <Input id="sales-from" type="date" {...form.register("from")} />
          </Field>
          <Field id="sales-to" label="To" error={form.formState.errors.to?.message}>
            <Input id="sales-to" type="date" {...form.register("to")} />
          </Field>
          <FormSelectField
            control={form.control}
            name="customer"
            id="sales-customer"
            label="Sold to"
            options={[
              { value: "all", label: "Everyone" },
              { value: "walk_in", label: customerLabel.walk_in },
              { value: "patient", label: customerLabel.patient },
              { value: "admission", label: customerLabel.admission },
            ]}
          />
          <FormSelectField
            control={form.control}
            name="linkage"
            id="sales-linkage"
            label="Linked to"
            options={[
              { value: "all", label: "Anything" },
              { value: "admission", label: linkageLabel.admission },
              { value: "opd", label: linkageLabel.opd },
              { value: "none", label: linkageLabel.none },
            ]}
          />
          <Field
            id="sales-invoice"
            label="Invoice number"
            error={form.formState.errors.invoice?.message}
          >
            <Input id="sales-invoice" placeholder="Any" {...form.register("invoice")} />
          </Field>
          <Button type="submit" variant="outline">
            Show sales
          </Button>
        </form>
        {/* One invoice is its statement; a file is of a period. */}
        {!("invoice" in filter) && (
          <ReportDownload
            scope={scope}
            path={path}
            label="Sales worklist export"
            recorded="A download holding an admission sale is recorded."
          />
        )}
      </Panel>
      <ListDetail
        label={pane.openName ? `Sale ${pane.openName}` : "Sale"}
        backHref={detail.closeHref}
        backLabel="Back to sales"
        detail={record && <div ref={paneRef}>{record}</div>}
        list={
          <Panel>
            {sales.isError ? (
              <RequestFailure message="Could not load sales." retry={sales.retry} />
            ) : sales.isPending ? (
              <p role="status">Loading sales…</p>
            ) : sales.items.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                {"invoice" in filter
                  ? `No sale with invoice ${filter.invoice}.`
                  : `No sales from ${filter.from} to ${filter.to}.`}
              </p>
            ) : (
              <ul className="divide-y text-sm" aria-label="Sales">
                {sales.items.map((s) => {
                  const money = (minor: number) => formatPharmacyMoney(minor, s.currency);
                  return (
                    <li
                      key={s.id}
                      {...rowProps(s.id)}
                      className={`flex flex-wrap items-start justify-between gap-3 py-3 ${s.id === detail.openId ? "bg-(--hms-accent-dim)" : ""}`}
                    >
                      <div className="min-w-0">
                        <p className="font-medium">
                          {s.invoice_number} · {s.sale_date}
                        </p>
                        <p className="mt-0.5">
                          {customerLabel[s.customer]}
                          {s.admission_bed_code && `, bed ${s.admission_bed_code}`}
                          {s.linkage === "opd" && ", for an OPD visit"} · {s.units} units ·{" "}
                          {money(s.total_minor)}
                        </p>
                        <p className="mt-0.5 text-muted-foreground">
                          {s.credited_minor > 0
                            ? `Credited ${money(s.credited_minor)}, net ${money(s.net_minor)}`
                            : "Nothing credited"}
                          {s.customer === "admission" &&
                            `; on the admission account ${money(s.admission_outstanding_minor)}`}
                        </p>
                      </div>
                      <Button
                        variant="outline"
                        size="sm"
                        {...openProps(s.id)}
                        aria-label={`Open invoice ${s.invoice_number}`}
                        onClick={() => pane.openRow(s.id)}
                      >
                        Open
                      </Button>
                    </li>
                  );
                })}
              </ul>
            )}
            <LoadMore
              label="Load more sales"
              hasMore={sales.hasMore}
              isLoading={sales.isFetchingMore}
              onClick={sales.loadMore}
            />
          </Panel>
        }
      />
    </div>
  );
}

function SaleSummaryPane({
  scope,
  id,
  pane,
}: {
  scope: PharmacyScope;
  id: string;
  pane: ReturnType<typeof useListDetail>;
}) {
  const summary = useApiQuery<PharmacySaleSummary>(
    pharmacyKey(scope, "sale-summary", id),
    pharmacySaleSummaryPath(scope, id),
  );
  const s = summary.data;
  return (
    <Panel className="space-y-5">
      <DetailHeader
        title={s ? `Invoice ${s.invoice_number}` : "Sale"}
        subtitle={
          s
            ? `${s.sale_date} · ${customerLabel[s.customer]}${s.admission_bed_code ? `, bed ${s.admission_bed_code}` : ""}${s.linkage === "opd" ? ", for an OPD visit" : ""}`
            : undefined
        }
        noun="sale"
        previous={{ disabled: !pane.canStep(-1), onClick: () => pane.step(-1) }}
        next={{ disabled: !pane.canStep(1), onClick: () => pane.step(1) }}
        closeLabel="Close sale"
        onClose={pane.close}
      />
      {summary.error instanceof ApiError && summary.error.status === 404 ? (
        <p role="alert" className="text-sm">
          This sale was not found, or you do not have access.
        </p>
      ) : summary.isError ? (
        <RequestFailure message="Could not load this sale." retry={summary.refetch} />
      ) : !s ? (
        <p role="status">Loading sale…</p>
      ) : (
        <SaleStatement summary={s} />
      )}
    </Panel>
  );
}

export function SaleStatement({ summary: s }: { summary: PharmacySaleSummary }) {
  const money = (minor: number) => formatPharmacyMoney(minor, s.currency);
  const kinds = (by: Record<string, number>) =>
    Object.entries(by)
      .map(([kind, amount]) => `${kindLabel[kind] ?? kind} ${money(amount)}`)
      .join(", ");
  return (
    <div className="space-y-5 text-sm">
      <table className="w-full" aria-label="Lines and charges">
        <thead className="text-left text-muted-foreground">
          <tr>
            <th className="py-2">Item</th>
            <th>Units</th>
            <th>Returned</th>
            <th>Charged</th>
            <th>Credited</th>
          </tr>
        </thead>
        <tbody>
          {s.lines.map((l) => (
            <tr key={`l${l.line_number}`} className="border-t align-top">
              <th scope="row" className="py-2 text-left font-normal">
                {l.brand_name}
                <span className="block text-muted-foreground">
                  {l.sku}, lot {l.lot_number}
                </span>
              </th>
              <td>{l.quantity_units}</td>
              <td>{l.returned_units}</td>
              <td>
                {money(l.amount_minor)}
                {l.discount_minor > 0 && (
                  <span className="block text-muted-foreground">
                    after {money(l.discount_minor)} off
                  </span>
                )}
              </td>
              <td>{money(l.credited_minor)}</td>
            </tr>
          ))}
          {s.charges.map((c) => (
            <tr key={`c${c.charge_number}`} className="border-t align-top">
              <th scope="row" className="py-2 text-left font-normal">
                {c.name}
                <span className="block text-muted-foreground">Service charge {c.code}</span>
              </th>
              <td />
              <td />
              <td>{money(c.amount_minor)}</td>
              <td>{money(c.credited_minor)}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1">
        <dt className="text-muted-foreground">Total</dt>
        <dd>
          {money(s.total_minor)} (taxable {money(s.taxable_minor)}, CGST {money(s.cgst_minor)}, SGST{" "}
          {money(s.sgst_minor)})
        </dd>
        <dt className="text-muted-foreground">Paid by</dt>
        <dd>{kinds(s.payments)}</dd>
        <dt className="text-muted-foreground">Credited</dt>
        <dd>{money(s.credited_minor)}</dd>
        <dt className="text-muted-foreground">Net</dt>
        <dd className="font-medium">{money(s.net_minor)}</dd>
        <dt className="text-muted-foreground">Kept at the counter</dt>
        <dd>
          {money(s.collected_minor - s.refunded_minor)} (collected {money(s.collected_minor)},
          refunded {money(s.refunded_minor)})
        </dd>
        {s.customer === "admission" && (
          <>
            <dt className="text-muted-foreground">On the admission account</dt>
            <dd>
              {money(s.admission_billed_minor - s.admission_reversed_minor)} (billed{" "}
              {money(s.admission_billed_minor)}, reversed {money(s.admission_reversed_minor)})
            </dd>
          </>
        )}
      </dl>

      <section aria-labelledby="sale-credit-notes">
        <h4 id="sale-credit-notes" className="font-semibold">
          Credit notes
        </h4>
        {s.credit_notes.length === 0 ? (
          <p className="mt-1 text-muted-foreground">None.</p>
        ) : (
          <ul className="mt-1 space-y-1">
            {s.credit_notes.map((n) => (
              <li key={n.id}>
                {n.credit_note_number} · {n.credit_note_date} · {money(n.total_minor)}
                <span className="text-muted-foreground"> refunded {kinds(n.refunds)}</span>
              </li>
            ))}
          </ul>
        )}
        {s.more_credit_notes && (
          <p className="mt-1 text-muted-foreground">
            More credit notes than are listed here; the totals above count them all.
          </p>
        )}
      </section>
    </div>
  );
}
