"use client";

import { useState } from "react";
import {
  pharmacyItemSalesPath,
  pharmacyKey,
  pharmacyPatientSalesPath,
  pharmacyReceiptsPath,
  pharmacyStockSalesPath,
  useApiPagedQuery,
  usePermissions,
  type PharmacyItemSales,
  type PharmacyPatientSaleLine,
  type PharmacyReceipt,
  type PharmacyReceiptKind,
  type PharmacyScope,
  type PharmacyStockSales,
} from "@helivanta/api";
import { LoadMore, RequestFailure } from "@helivanta/ui";
import { CounterPatientLookup, type CounterPatient } from "./counter-patient";
import { formatPharmacyMoney } from "./currency-amount-field";
import type { Period } from "./period";

/** Sales by item over the period (#1060): sold, returned and net per product. */
export function ItemSales({ scope, period }: { scope: PharmacyScope; period: Period }) {
  const items = useApiPagedQuery<PharmacyItemSales>(
    pharmacyKey(scope, "item-sales", period.from, period.to),
    pharmacyItemSalesPath(scope, period.from, period.to),
  );
  return (
    <section className="mt-6" aria-labelledby="item-sales-heading">
      <h4 id="item-sales-heading" className="font-semibold">
        Sales by item
      </h4>
      <p className="mt-1 text-sm text-muted-foreground">
        Medicines only; service charges are in the daily totals above.
      </p>
      {items.isError ? (
        <RequestFailure message="Could not load sales by item." retry={items.retry} />
      ) : items.isPending ? (
        <p role="status">Loading sales by item…</p>
      ) : items.items.length === 0 ? (
        <p className="mt-2 text-sm text-muted-foreground">
          No items sold or returned from {period.from} to {period.to}.
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
  const rows = useApiPagedQuery<PharmacyStockSales>(
    pharmacyKey(scope, "stock-sales", period.from, period.to),
    pharmacyStockSalesPath(scope, period.from, period.to),
  );
  return (
    <section className="mt-6" aria-labelledby="stock-sales-heading">
      <h4 id="stock-sales-heading" className="font-semibold">
        Sales and stock
      </h4>
      <p className="mt-1 text-sm text-muted-foreground">
        Units sold and returned in the period, beside the stock held now.
      </p>

      {rows.isError ? (
        <RequestFailure message="Could not load sales and stock." retry={rows.retry} />
      ) : rows.isPending ? (
        <p role="status">Loading sales and stock…</p>
      ) : rows.items.length === 0 ? (
        <p className="mt-2 text-sm text-muted-foreground">No product has held stock yet.</p>
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
