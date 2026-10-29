"use client";

import { useState } from "react";
import { Button } from "@tesserix/web";
import { usePharmacyStockOverview, type PharmacyStockOverviewScope } from "@helivanta/api";
import { Field, Panel, RequestFailure, SelectControl } from "@helivanta/ui";
import { formatPharmacyMoney } from "./currency-amount-field";
import { StockExport } from "./stock-export";

/** A pharmacy-level operational read; it never uses the selected outlet as authority. */
export function StockOverview({ scope }: { scope: PharmacyStockOverviewScope }) {
  const [expiry, setExpiry] = useState("all");
  const [balance, setBalance] = useState("on_hand");
  const [asOf, setAsOf] = useState(() => new Date().toISOString());
  const [cursors, setCursors] = useState<(string | null)[]>([null]);
  const filters = new URLSearchParams({ as_of: asOf, expiry, balance }).toString();
  const page = usePharmacyStockOverview(scope, filters, cursors[cursors.length - 1]);
  function changeFilter(set: (value: string) => void, value: string) {
    set(value);
    setCursors([null]);
  }
  return (
    <Panel>
      <h3 className="font-semibold">Stock across outlets</h3>
      <p className="mt-2 text-sm text-muted-foreground">
        Only outlets you have access to are included. Totals are for each outlet&apos;s product and
        unit across all matching positions, including other pages. Matching names or SKUs at
        different outlets remain separate products.
      </p>
      <div className="my-4 flex flex-wrap items-end gap-4">
        <Field id="overview-expiry" label="Expiry">
          <SelectControl
            id="overview-expiry"
            value={expiry}
            onValueChange={(value) => changeFilter(setExpiry, value)}
            options={[
              { value: "all", label: "All expiry dates" },
              { value: "expired", label: "Expired" },
              { value: "unexpired", label: "Unexpired" },
              { value: "today", label: "Expires today" },
              { value: "due_30", label: "Due within 30 days" },
              { value: "due_90", label: "Due within 90 days" },
              { value: "due_180", label: "Due within 180 days" },
            ]}
          />
        </Field>
        <Field id="overview-balance" label="Balance">
          <SelectControl
            id="overview-balance"
            value={balance}
            onValueChange={(value) => changeFilter(setBalance, value)}
            options={[
              { value: "on_hand", label: "Stock on hand" },
              { value: "all", label: "Include zero balances" },
            ]}
          />
        </Field>
        <Button
          variant="outline"
          onClick={() => {
            setAsOf(new Date().toISOString());
            setCursors([null]);
          }}
        >
          Refresh snapshot
        </Button>
        <StockExport
          overview
          scope={scope}
          filters={filters}
          disabled={page.isPending || page.isError || !page.data}
        />
      </div>
      <p className="text-sm text-muted-foreground">
        Snapshot: {asOf}. Expiry uses each outlet&apos;s local date.
      </p>
      {page.isError ? (
        <RequestFailure message="Could not load stock across outlets." retry={page.refetch} />
      ) : !page.data ? (
        <p role="status">Loading stock across outlets…</p>
      ) : (
        <>
          {page.data.coverage === "restricted_outlets_excluded" && (
            <p role="status" className="my-3 font-medium">
              Restricted outlets are excluded. These totals do not cover the whole pharmacy.
            </p>
          )}
          {page.data.values.map((value) => (
            <p key={value.currency} className="my-3 font-medium">
              Combined recorded purchase value:{" "}
              {formatPharmacyMoney(value.known_purchase_value_minor, value.currency)} known.{" "}
              {value.uncosted_products} products have uncosted stock.
            </p>
          ))}
          <p className="my-3 text-sm text-muted-foreground">
            Purchase value uses recorded pack costs, prorated to units and rounded down once per
            product. Uncosted units are shown separately. This is recorded purchase value; landed
            costs are not included. Physical stock includes quarantine and expired stock and is not
            a promise of sale availability.
          </p>
          {page.data.data.length === 0 ? (
            <p role="status">No stock positions match these filters in your accessible outlets.</p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm" aria-label="Stock across outlets">
                <thead className="text-left text-muted-foreground">
                  <tr>
                    <th className="py-2">Outlet</th>
                    <th>Product / unit</th>
                    <th>Batch / location</th>
                    <th>Expiry</th>
                    <th>Physical units</th>
                    <th>Product total</th>
                    <th>Product purchase value</th>
                  </tr>
                </thead>
                <tbody>
                  {page.data.data.map((row) => (
                    <tr key={row.id} className="border-t">
                      <td className="py-3">
                        {row.outlet_name} ({row.outlet_code})
                      </td>
                      <td>
                        {row.brand_name} ({row.sku})<br />
                        {row.unit_of_issue}
                      </td>
                      <td>
                        {row.lot_number} / {row.location_name}
                        <br />
                        {row.stock_status}
                      </td>
                      <td>
                        {row.expires_on}
                        <br />
                        {row.days_to_expiry} days ({row.expiry_time_zone})
                      </td>
                      <td>
                        {row.on_hand_units} {row.unit_of_issue}
                      </td>
                      <td>
                        {row.product_on_hand_units} {row.unit_of_issue}
                      </td>
                      <td>
                        {formatPharmacyMoney(row.known_purchase_value_minor, row.currency)} known
                        <br />
                        {row.uncosted_units} {row.unit_of_issue} uncosted
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <div className="mt-4 flex gap-3" aria-label="Stock pages">
            <Button
              variant="outline"
              disabled={cursors.length === 1}
              onClick={() => setCursors((old) => old.slice(0, -1))}
            >
              Previous page
            </Button>
            <Button
              variant="outline"
              disabled={!page.data.page.has_more || !page.data.page.next_cursor}
              onClick={() => setCursors((old) => [...old, page.data!.page.next_cursor])}
            >
              Next page
            </Button>
          </div>
        </>
      )}
    </Panel>
  );
}
