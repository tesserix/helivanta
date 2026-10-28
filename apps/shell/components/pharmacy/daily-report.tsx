"use client";

import { useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  pharmacyDailyReportPath,
  pharmacyKey,
  useApiQuery,
  type PharmacyDailyReport,
  type PharmacyDailyTotals,
  type PharmacyScope,
} from "@helivanta/api";
import { Field, Panel, RequestFailure, useZodForm } from "@helivanta/ui";
import { formatPharmacyMoney } from "./currency-amount-field";
import { periodSchema, type Period } from "./period";

/**
 * The daily sales report (#1386 C-3, #1060): posted sales and credit notes
 * summed per outlet day, read afresh from the records each time.
 */
export function DailyReport({ scope, today }: { scope: PharmacyScope; today: string }) {
  const [period, setPeriod] = useState<Period>({ from: today.slice(0, 8) + "01", to: today });
  const form = useZodForm(periodSchema, period);
  return (
    <Panel>
      <h3 className="font-semibold">Daily sales</h3>
      <p className="mt-1 text-sm text-muted-foreground">
        Each day&apos;s invoices, credit notes, payments taken and refunds paid, by the
        outlet&apos;s date. A return counts on the day it was taken. Cash is what was applied to the
        sale, not counting change.
      </p>
      <form
        noValidate
        className="mt-4 flex flex-wrap items-end gap-4"
        onSubmit={form.handleSubmit(setPeriod)}
      >
        <Field id="daily-from" label="From" error={form.formState.errors.from?.message}>
          <Input id="daily-from" type="date" {...form.register("from")} />
        </Field>
        <Field id="daily-to" label="To" error={form.formState.errors.to?.message}>
          <Input id="daily-to" type="date" {...form.register("to")} />
        </Field>
        <Button type="submit" variant="outline">
          Show report
        </Button>
      </form>
      <Days scope={scope} period={period} />
    </Panel>
  );
}

function Days({ scope, period }: { scope: PharmacyScope; period: Period }) {
  const report = useApiQuery<PharmacyDailyReport>(
    pharmacyKey(scope, "daily-report", period.from, period.to),
    pharmacyDailyReportPath(scope, period.from, period.to),
  );
  if (report.isError)
    return <RequestFailure message="Could not load the daily report." retry={report.refetch} />;
  if (!report.data) return <p role="status">Loading the report…</p>;
  if (report.data.days.length === 0)
    return (
      <p className="mt-4 text-sm text-muted-foreground">
        No sales or returns from {period.from} to {period.to}.
      </p>
    );
  return (
    <table className="mt-4 w-full text-sm" aria-label="Daily sales">
      <thead className="text-left text-muted-foreground">
        <tr>
          <th className="py-2">Date</th>
          <th>Invoices</th>
          <th>Credit notes</th>
          <th>Net</th>
          <th>Taken</th>
          <th>Refunded</th>
        </tr>
      </thead>
      <tbody>
        {report.data.days.map((d) => (
          <DayRow key={`${d.day}:${d.currency}`} day={d} />
        ))}
      </tbody>
    </table>
  );
}

function DayRow({ day: d }: { day: PharmacyDailyTotals }) {
  const money = (minor: number) => formatPharmacyMoney(minor, d.currency);
  const byKind = (cash: number, upiVerified: number, upi: number, card: number) =>
    (
      [
        ["Cash", cash],
        ["UPI (verified)", upiVerified],
        ["UPI", upi],
        ["Card", card],
      ] as const
    )
      .filter(([, minor]) => minor > 0)
      .map(([kind, minor]) => `${kind} ${money(minor)}`)
      .join(", ") || "—";
  return (
    <tr className="border-t align-top">
      <th scope="row" className="py-2 text-left font-normal">
        {d.day}
      </th>
      <td>
        {d.sales} for {money(d.sales_minor)}
        <span className="block text-muted-foreground">
          taxable {money(d.sales_taxable_minor)}, CGST {money(d.sales_cgst_minor)}, SGST{" "}
          {money(d.sales_sgst_minor)}; discount {money(d.discount_minor)}; {d.units_sold} units
        </span>
      </td>
      <td>
        {d.credit_notes} for {money(d.credits_minor)}
        <span className="block text-muted-foreground">
          taxable {money(d.credits_taxable_minor)}, CGST {money(d.credits_cgst_minor)}, SGST{" "}
          {money(d.credits_sgst_minor)}; {d.units_returned} units
        </span>
      </td>
      <td>{money(d.net_minor)}</td>
      <td>{byKind(d.cash_minor, d.upi_verified_minor, d.upi_minor, d.card_minor)}</td>
      <td>{byKind(d.cash_refund_minor, 0, d.upi_refund_minor, d.card_refund_minor)}</td>
    </tr>
  );
}
