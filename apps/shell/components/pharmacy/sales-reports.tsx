"use client";

import { useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  pharmacyItemSalesPath,
  pharmacyKey,
  pharmacyPatientSalesPath,
  pharmacyPrescribersPath,
  pharmacyReceiptsPath,
  pharmacyStockSalesPath,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type PharmacyItemSales,
  type PharmacyItemSalesLines,
  type PharmacyPrescribers,
  type PharmacyPatientSaleLine,
  type PharmacyReceipt,
  type PharmacyReceiptKind,
  type PharmacyReportCategory,
  type PharmacyScope,
  type PharmacyStockSales,
} from "@helivanta/api";
import { Field, LoadMore, RequestFailure, SelectControl, useZodForm } from "@helivanta/ui";
import { CounterPatientLookup, type CounterPatient } from "./counter-patient";
import { formatPharmacyMoney } from "./currency-amount-field";
import { useItemCategories } from "./item-categories";
import type { Period } from "./period";

/**
 * The item category, then subcategory, a report is cut by (#988). Retired
 * categories are offered: past sales were made under them. Only for catalogue
 * readers, who can list the categories; the API enforces nothing new.
 */
function CategoryFilter(props: {
  scope: PharmacyScope;
  id: string;
  value: PharmacyReportCategory;
  onChange: (value: PharmacyReportCategory) => void;
}) {
  const { can } = usePermissions();
  return can("pharmacy.catalogue.read") ? <CategoryPickers {...props} /> : null;
}

function CategoryPickers({
  scope,
  id,
  value,
  onChange,
}: {
  scope: PharmacyScope;
  id: string;
  value: PharmacyReportCategory;
  onChange: (value: PharmacyReportCategory) => void;
}) {
  const categories = useItemCategories(scope);
  if (categories.isError)
    return <RequestFailure message="Could not load the categories." retry={categories.retry} />;
  const label = (c: { code: string; name: string; retired_at: string | null }) =>
    `${c.name} (${c.code})${c.retired_at ? ", retired" : ""}`;
  const subs = categories.items.filter((c) => value.categoryId && c.parent_id === value.categoryId);
  return (
    <div className="mt-2 grid gap-4 sm:grid-cols-2">
      <Field id={`${id}-category`} label="Category">
        <SelectControl
          id={`${id}-category`}
          value={value.categoryId ?? "all"}
          // A subcategory belongs to one category: changing it drops the subcategory.
          onValueChange={(v) => onChange(v === "all" ? {} : { categoryId: v })}
          options={[
            { value: "all", label: "All categories" },
            ...categories.items
              .filter((c) => c.parent_id === null)
              .map((c) => ({ value: c.id, label: label(c) })),
          ]}
        />
      </Field>
      {value.categoryId && subs.length > 0 && (
        <Field id={`${id}-subcategory`} label="Subcategory">
          <SelectControl
            id={`${id}-subcategory`}
            value={value.subcategoryId ?? "all"}
            onValueChange={(v) =>
              onChange({
                categoryId: value.categoryId,
                ...(v === "all" ? {} : { subcategoryId: v }),
              })
            }
            options={[
              { value: "all", label: "All subcategories" },
              ...subs.map((c) => ({ value: c.id, label: label(c) })),
            ]}
          />
        </Field>
      )}
    </div>
  );
}

/** Sales by item over the period (#1060): sold, returned and net per product. */
const lotSchema = z.object({
  lot: z.string().trim().max(100, "A lot number is at most 100 characters"),
});

/** The lot sales by item is cut by (#1060), applied when submitted. */
function LotFilter({ onChange }: { onChange: (lot: string | undefined) => void }) {
  const form = useZodForm(lotSchema, { lot: "" });
  return (
    <form
      noValidate
      className="mt-2 flex flex-wrap items-end gap-4"
      onSubmit={form.handleSubmit((v) => onChange(v.lot || undefined))}
    >
      <Field id="item-sales-lot" label="Lot number" error={form.formState.errors.lot?.message}>
        <Input id="item-sales-lot" {...form.register("lot")} />
      </Field>
      <Button type="submit" variant="outline">
        Apply lot
      </Button>
    </form>
  );
}

type Prescriber = NonNullable<PharmacyItemSalesLines["prescriber"]>;
const prescriberValue = (p: Prescriber) =>
  JSON.stringify([p.prescriber_registration, p.prescriber_council]);

/**
 * The prescriber sales by item is cut by (#1060), chosen from the period's
 * prescribers. Only for prescription readers: the API refuses anyone else,
 * and every list and every filtered page is recorded.
 */
function PrescriberFilter(props: {
  scope: PharmacyScope;
  period: Period;
  value: Prescriber | undefined;
  onChange: (value: Prescriber | undefined) => void;
}) {
  const { can } = usePermissions();
  return can("pharmacy.prescription.read") ? <PrescriberPicker {...props} /> : null;
}

function PrescriberPicker({
  scope,
  period,
  value,
  onChange,
}: {
  scope: PharmacyScope;
  period: Period;
  value: Prescriber | undefined;
  onChange: (value: Prescriber | undefined) => void;
}) {
  const list = useApiQuery<PharmacyPrescribers>(
    pharmacyKey(scope, "prescribers", period.from, period.to),
    pharmacyPrescribersPath(scope, period.from, period.to),
  );
  if (list.isError)
    return (
      <RequestFailure message="Could not load the prescribers." retry={() => void list.refetch()} />
    );
  const prescribers = list.data?.data ?? [];
  const options = prescribers.map((p) => ({
    value: prescriberValue(p),
    label: `${p.prescriber_name} (${p.prescriber_registration}, ${p.prescriber_council})`,
  }));
  // A prescriber chosen for another period stays chosen, and named.
  if (value && !options.some((o) => o.value === prescriberValue(value)))
    options.unshift({
      value: prescriberValue(value),
      label: `${value.prescriber_registration}, ${value.prescriber_council}`,
    });
  return (
    <div className="mt-2">
      <Field id="item-sales-prescriber" label="Prescriber">
        <SelectControl
          id="item-sales-prescriber"
          value={value ? prescriberValue(value) : "all"}
          disabled={list.isPending}
          onValueChange={(v) => {
            if (v === "all") return onChange(undefined);
            const [prescriber_registration, prescriber_council] = JSON.parse(v) as [string, string];
            onChange({ prescriber_registration, prescriber_council });
          }}
          options={[
            { value: "all", label: "All sales, with or without a prescription" },
            ...options,
          ]}
        />
      </Field>
      {list.data?.more && (
        <p className="mt-1 text-sm text-muted-foreground">
          More than 200 prescribers in this period; choose a shorter period to see them all.
        </p>
      )}
    </div>
  );
}

export function ItemSales({ scope, period }: { scope: PharmacyScope; period: Period }) {
  const [category, setCategory] = useState<PharmacyReportCategory>({});
  const [lines, setLines] = useState<PharmacyItemSalesLines>({});
  const filtered = Boolean(category.categoryId || lines.lotNumber || lines.prescriber);
  const items = useApiPagedQuery<PharmacyItemSales>(
    pharmacyKey(
      scope,
      "item-sales",
      period.from,
      period.to,
      category.categoryId ?? "",
      category.subcategoryId ?? "",
      lines.lotNumber ?? "",
      lines.prescriber ? prescriberValue(lines.prescriber) : "",
    ),
    pharmacyItemSalesPath(scope, period.from, period.to, category, lines),
  );
  return (
    <section className="mt-6" aria-labelledby="item-sales-heading">
      <h4 id="item-sales-heading" className="font-semibold">
        Sales by item
      </h4>
      <p className="mt-1 text-sm text-muted-foreground">
        Medicines only; service charges are in the daily totals above. A sale counts under the
        category its item had on the sale date, and a return under its sale&apos;s.
      </p>
      <CategoryFilter scope={scope} id="item-sales" value={category} onChange={setCategory} />
      <LotFilter onChange={(lotNumber) => setLines((l) => ({ ...l, lotNumber }))} />
      <PrescriberFilter
        scope={scope}
        period={period}
        value={lines.prescriber}
        onChange={(prescriber) => setLines((l) => ({ ...l, prescriber }))}
      />
      {items.isError ? (
        <RequestFailure message="Could not load sales by item." retry={items.retry} />
      ) : items.isPending ? (
        <p role="status">Loading sales by item…</p>
      ) : items.items.length === 0 ? (
        <p className="mt-2 text-sm text-muted-foreground">
          No items sold or returned{filtered && " for these filters"} from {period.from} to{" "}
          {period.to}.
        </p>
      ) : (
        <table className="mt-2 w-full text-sm" aria-label="Sales by item">
          <thead className="text-left text-muted-foreground">
            <tr>
              <th className="py-2">Item</th>
              <th>Sold</th>
              <th>Returned</th>
              <th>Net</th>
            </tr>
          </thead>
          <tbody>
            {items.items.map((i) => {
              const money = (minor: number) => formatPharmacyMoney(minor, i.currency);
              const mrp =
                i.mrp_low_minor === null
                  ? null
                  : i.mrp_low_minor === i.mrp_high_minor
                    ? money(i.mrp_low_minor)
                    : `${money(i.mrp_low_minor)}–${money(i.mrp_high_minor!)}`;
              return (
                <tr key={i.product_id} className="border-t align-top">
                  <th scope="row" className="py-2 text-left font-normal">
                    {i.brand_name} ({i.generic_name})
                    <span className="block text-muted-foreground">
                      {i.sku}, pack of {i.pack_size} {i.unit_of_issue}, {i.manufacturer}
                    </span>
                  </th>
                  <td>
                    {i.units_sold} units for {money(i.sales_minor)}
                    <span className="block text-muted-foreground">
                      discount {money(i.discount_minor)}; taxable {money(i.sales_taxable_minor)}
                      {mrp && `; MRP ${mrp} a pack`}
                    </span>
                  </td>
                  <td>
                    {i.units_returned} units for {money(i.credits_minor)}
                  </td>
                  <td>
                    {i.net_units} units, {money(i.net_minor)}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      <LoadMore
        label="Load more items"
        hasMore={items.hasMore}
        isLoading={items.isFetchingMore}
        onClick={items.loadMore}
      />
    </section>
  );
}

const kindLabel: Record<PharmacyReceiptKind, string> = {
  cash: "Cash",
  upi: "UPI",
  upi_verified: "UPI (verified)",
  card: "Card",
  admission_account: "Admission account",
};

/** Every payment taken and refund paid over the period, oldest first (#1060). */
export function ReceiptsRegister({ scope, period }: { scope: PharmacyScope; period: Period }) {
  const entries = useApiPagedQuery<PharmacyReceipt>(
    pharmacyKey(scope, "receipts", period.from, period.to),
    pharmacyReceiptsPath(scope, period.from, period.to),
  );
  return (
    <section className="mt-6" aria-labelledby="receipts-heading">
      <h4 id="receipts-heading" className="font-semibold">
        Receipts and refunds
      </h4>
      <p className="mt-1 text-sm text-muted-foreground">
        Each payment and refund, to tick off against the till or a statement. Cash is what was
        applied to the sale, not counting change.
      </p>
      {entries.isError ? (
        <RequestFailure message="Could not load receipts and refunds." retry={entries.retry} />
      ) : entries.isPending ? (
        <p role="status">Loading receipts and refunds…</p>
      ) : entries.items.length === 0 ? (
        <p className="mt-2 text-sm text-muted-foreground">
          No receipts or refunds from {period.from} to {period.to}.
        </p>
      ) : (
        <table className="mt-2 w-full text-sm" aria-label="Receipts and refunds">
          <thead className="text-left text-muted-foreground">
            <tr>
              <th className="py-2">Date</th>
              <th>Document</th>
              <th>Type</th>
              <th>Mode</th>
              <th>Reference</th>
              <th>Receipt</th>
              <th>Refund</th>
            </tr>
          </thead>
          <tbody>
            {entries.items.map((e) => {
              const amount = formatPharmacyMoney(e.amount_minor, e.currency);
              return (
                <tr key={e.id} className="border-t align-top">
                  <td className="py-2">{e.day}</td>
                  <td>{e.document_number}</td>
                  <td>{e.direction === "receipt" ? "Receipt" : "Refund"}</td>
                  <td>
                    {kindLabel[e.kind]}
                    {e.kind === "admission_account" && (
                      <span className="block text-muted-foreground">
                        {e.direction === "receipt" ? "not collected" : "not paid out"}
                      </span>
                    )}
                  </td>
                  <td>{e.reference ?? "—"}</td>
                  <td>{e.direction === "receipt" ? amount : ""}</td>
                  <td>{e.direction === "refund" ? amount : ""}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      <LoadMore
        label="Load more receipts and refunds"
        hasMore={entries.hasMore}
        isLoading={entries.isFetchingMore}
        onClick={entries.loadMore}
      />
    </section>
  );
}

/**
 * One registered patient's purchases over the period (#1060, legacy 442).
 * Nothing is read until a patient is found through the audited MRN lookup,
 * and the API audits every page it returns.
 */
export function PatientSales({ scope, period }: { scope: PharmacyScope; period: Period }) {
  const [patient, setPatient] = useState<CounterPatient | null>(null);
  // The lookup is patientmaster's and pharmacists' alone; offering it to
  // anyone else would be a search that is always refused.
  const { can } = usePermissions();
  return (
    <section className="mt-6" aria-labelledby="patient-sales-heading">
      <h4 id="patient-sales-heading" className="font-semibold">
        Patient-wise sales
      </h4>
      <p className="mt-1 text-sm text-muted-foreground">
        What one registered patient bought, by batch, and what came back. Each view is recorded.
      </p>
      {can("patient.pharmacy-lookup") ? (
        <div className="mt-2">
          <CounterPatientLookup scope={scope} value={patient} onChange={setPatient} />
        </div>
      ) : (
        <p className="mt-2 text-sm text-muted-foreground">
          Finding a patient needs patient lookup, which pharmacists hold.
        </p>
      )}
      {patient && <PatientSaleLines scope={scope} period={period} patient={patient} />}
    </section>
  );
}

export function PatientSaleLines({
  scope,
  period,
  patient,
}: {
  scope: PharmacyScope;
  period: Period;
  patient: CounterPatient;
}) {
  const lines = useApiPagedQuery<PharmacyPatientSaleLine>(
    pharmacyKey(scope, "patient-sales", patient.patient_id, period.from, period.to),
    pharmacyPatientSalesPath(scope, patient.patient_id, period.from, period.to),
  );
  return (
    <>
      {lines.isError ? (
        <RequestFailure message="Could not load the patient's purchases." retry={lines.retry} />
      ) : lines.isPending ? (
        <p role="status">Loading the patient's purchases…</p>
      ) : lines.items.length === 0 ? (
        <p className="mt-2 text-sm text-muted-foreground">
          {patient.name} bought nothing from {period.from} to {period.to}.
        </p>
      ) : (
        <table className="mt-2 w-full text-sm" aria-label={`Purchases by ${patient.name}`}>
          <thead className="text-left text-muted-foreground">
            <tr>
              <th className="py-2">Date</th>
              <th>Invoice</th>
              <th>Item</th>
              <th>Batch</th>
              <th>Units</th>
              <th>Amount</th>
              <th>Returned</th>
            </tr>
          </thead>
          <tbody>
            {lines.items.map((l) => {
              const money = (minor: number) => formatPharmacyMoney(minor, l.currency);
              return (
                <tr key={l.id} className="border-t align-top">
                  <td className="py-2">{l.sale_date}</td>
                  <td>
                    {l.invoice_number}, line {l.line_number}
                    {l.admission_bed_code && (
                      <span className="block text-muted-foreground">
                        admission, bed {l.admission_bed_code}
                      </span>
                    )}
                  </td>
                  <th scope="row" className="text-left font-normal">
                    {l.brand_name} ({l.generic_name})
                    <span className="block text-muted-foreground">
                      {l.sku}, pack of {l.pack_size} {l.unit_of_issue}
                    </span>
                  </th>
                  <td>
                    {l.lot_number}
                    <span className="block text-muted-foreground">expires {l.expires_on}</span>
                  </td>
                  <td>{l.quantity_units}</td>
                  <td>
                    {money(l.amount_minor)}
                    <span className="block text-muted-foreground">
                      MRP {money(l.mrp_minor)} a pack; discount {money(l.discount_minor)}
                    </span>
                  </td>
                  <td>{l.returned_units}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}

      <LoadMore
        label="Load more purchases"
        hasMore={lines.hasMore}
        isLoading={lines.isFetchingMore}
        onClick={lines.loadMore}
      />
    </>
  );
}

/** Each product's sales over the period beside its stock now (#1060, legacy 755). */
export function StockSales({ scope, period }: { scope: PharmacyScope; period: Period }) {
  const [category, setCategory] = useState<PharmacyReportCategory>({});
  const rows = useApiPagedQuery<PharmacyStockSales>(
    pharmacyKey(
      scope,
      "stock-sales",
      period.from,
      period.to,
      category.categoryId ?? "",
      category.subcategoryId ?? "",
    ),
    pharmacyStockSalesPath(scope, period.from, period.to, category),
  );
  return (
    <section className="mt-6" aria-labelledby="stock-sales-heading">
      <h4 id="stock-sales-heading" className="font-semibold">
        Sales and stock
      </h4>
      <p className="mt-1 text-sm text-muted-foreground">
        Units sold and returned in the period, beside the stock held now. By category, a product is
        listed under its category today, with only the sales made while it was in it.
      </p>
      <CategoryFilter scope={scope} id="stock-sales" value={category} onChange={setCategory} />

      {rows.isError ? (
        <RequestFailure message="Could not load sales and stock." retry={rows.retry} />
      ) : rows.isPending ? (
        <p role="status">Loading sales and stock…</p>
      ) : rows.items.length === 0 ? (
        <p className="mt-2 text-sm text-muted-foreground">
          {category.categoryId
            ? "No product in this category today has held stock."
            : "No product has held stock yet."}
        </p>
      ) : (
        <table className="mt-2 w-full text-sm" aria-label="Sales and stock">
          <thead className="text-left text-muted-foreground">
            <tr>
              <th className="py-2">Item</th>
              <th>Sold</th>
              <th>Returned</th>
              <th>On hand</th>
              <th>On sellable shelves</th>
            </tr>
          </thead>
          <tbody>
            {rows.items.map((r) => (
              <tr key={r.product_id} className="border-t align-top">
                <th scope="row" className="py-2 text-left font-normal">
                  {r.brand_name} ({r.generic_name})
                  <span className="block text-muted-foreground">
                    {r.sku}, pack of {r.pack_size} {r.unit_of_issue}
                  </span>
                </th>
                <td>{r.units_sold}</td>
                <td>{r.units_returned}</td>
                <td>{r.on_hand_units}</td>
                <td>{r.sellable_units}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <LoadMore
        label="Load more products"
        hasMore={rows.hasMore}
        isLoading={rows.isFetchingMore}
        onClick={rows.loadMore}
      />
    </section>
  );
}
