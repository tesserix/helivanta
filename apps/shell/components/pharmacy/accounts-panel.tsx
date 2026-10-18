"use client";

import { useRef, useState } from "react";
import { Button } from "@tesserix/web";
import { ReceiptText, PackageSearch, Plus } from "lucide-react";
import {
  useApiPagedQuery,
  usePermissions,
  pharmacyKey,
  pharmacyRoot,
  type PharmacyScope,
  type PharmacyPayable,
  type PharmacyInventoryAudit,
} from "@helivanta/api";
import { LoadMore } from "@helivanta/ui";
import { formatPharmacyMoney } from "./currency-amount-field";
import { PayableForm, PayableHistory } from "./payable-forms";
import { RequestFailure } from "../request-failure";

export function AccountsPanel({
  scope,
  writable,
  defaultCurrency,
  timeZone = "UTC",
}: {
  scope: PharmacyScope;
  writable: boolean;
  defaultCurrency: string;
  timeZone?: string;
}) {
  const { can } = usePermissions();
  if (!can("pharmacy.accounts.read"))
    return (
      <section className="rounded-2xl border bg-card p-6">
        <h3 className="font-semibold">Financial access required</h3>
        <p className="mt-2 text-sm text-muted-foreground">
          Ask your administrator for financial permissions and access to this outlet.
        </p>
      </section>
    );
  return (
    <OutletAccounts
      key={`${scope.tenantId}:${scope.pharmacyId}:${scope.outletId}:${timeZone}`}
      scope={scope}
      writable={writable}
      defaultCurrency={defaultCurrency}
      timeZone={timeZone}
    />
  );
}
function OutletAccounts({
  scope,
  writable,
  defaultCurrency,
  timeZone,
}: {
  scope: PharmacyScope;
  writable: boolean;
  defaultCurrency: string;
  timeZone: string;
}) {
  const { can } = usePermissions();
  const [selected, setSelected] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const editor = useRef<HTMLElement>(null);
  function reveal(id: string | null) {
    setCreating(id === null);
    setSelected(id);
    requestAnimationFrame(() => {
      editor.current?.scrollIntoView({ block: "start", behavior: "instant" });
      editor.current?.querySelector<HTMLElement>("input,button")?.focus({ preventScroll: true });
    });
  }
  const bills = useApiPagedQuery<PharmacyPayable>(
    pharmacyKey(scope, "accounts", "payables"),
    `${pharmacyRoot(scope)}/accounts/payables`,
  );
  const audit = useApiPagedQuery<PharmacyInventoryAudit>(
    pharmacyKey(scope, "accounts", "inventory-audit"),
    `${pharmacyRoot(scope)}/accounts/inventory-audit`,
  );
  return (
    <div className="space-y-6">
      <section aria-labelledby="payables-heading" className="rounded-2xl border bg-card p-5 sm:p-6">
        <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
          <div>
            <h3 id="payables-heading" className="flex items-center gap-2 font-semibold">
              <ReceiptText className="size-5 text-(--hms-accent)" />
              Bills & expenses
            </h3>
            <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">
              Track original bills, recorded payments and remaining amounts. Each document keeps its
              own currency and history.
            </p>
          </div>
          {writable && can("pharmacy.accounts.write") && (
            <Button onClick={() => reveal(null)}>
              <Plus className="size-4" />
              Record bill or expense
            </Button>
          )}
        </div>
        {bills.isError ? (
          <RequestFailure message="Could not load bills and expenses." retry={bills.retry} />
        ) : bills.isPending ? (
          <p role="status">Loading bills and expenses…</p>
        ) : !bills.items.length ? (
          <p className="py-8 text-sm text-muted-foreground">
            No bills or expenses recorded for this outlet.
          </p>
        ) : (
          <div className="overflow-x-auto rounded-xl border">
            <table
              aria-label="Bills and expenses"
              className="block w-full text-left text-sm md:table md:min-w-[780px]"
            >
              <thead className="sr-only bg-muted/40 text-xs text-muted-foreground md:not-sr-only">
                <tr>
                  {[
                    "Document / payee",
                    "Due date",
                    "Billed",
                    "Paid",
                    "Credits",
                    "Outstanding",
                    "History",
                  ].map((h) => (
                    <th key={h} scope="col" className="px-4 py-3 font-medium">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="block divide-y md:table-row-group">
                {bills.items.map((b) => (
                  <tr key={b.id} className="grid grid-cols-2 md:table-row">
                    <td className="col-span-2 min-w-0 px-4 pt-4 pb-2 md:py-4">
                      <p className="wrap-anywhere font-medium">{b.document_reference}</p>
                      <p className="mt-1 text-xs text-muted-foreground">{b.payee_name}</p>
                    </td>
                    <td className="col-span-2 px-4 py-2 text-muted-foreground md:py-4 md:whitespace-nowrap">
                      <span aria-hidden="true" className="mr-2 text-xs md:hidden">
                        Due
                      </span>
                      {b.due_on}
                    </td>
                    {[b.total_minor, b.paid_minor, b.credit_minor, b.outstanding_minor].map(
                      (v, i) => (
                        <td
                          key={i}
                          className={`min-w-0 wrap-anywhere px-4 py-2 tabular-nums md:py-4 md:whitespace-nowrap ${i === 3 ? "font-semibold" : ""}`}
                        >
                          <span
                            aria-hidden="true"
                            className="mb-1 block text-xs font-normal text-muted-foreground md:hidden"
                          >
                            {["Billed", "Paid", "Credits", "Outstanding"][i]}
                          </span>
                          {formatPharmacyMoney(v, b.currency)}
                        </td>
                      ),
                    )}
                    <td className="col-span-2 px-4 py-4">
                      <Button
                        variant="ghost"
                        onClick={() => {
                          reveal(b.id);
                        }}
                        aria-label={`Open ${b.document_reference}`}
                      >
                        View
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <LoadMore
          hasMore={bills.hasMore}
          isLoading={bills.isFetchingMore}
          onClick={bills.loadMore}
        />
      </section>
      {creating && (
        <section ref={editor} className="scroll-mt-24 rounded-2xl border bg-card p-5 sm:p-6">
          <PayableForm
            scope={scope}
            defaultCurrency={defaultCurrency}
            onSaved={(id) => {
              setCreating(false);
              setSelected(id);
            }}
            onCancel={() => setCreating(false)}
          />
        </section>
      )}
      {selected && (
        <section ref={editor} className="scroll-mt-24 rounded-2xl border bg-card p-5 sm:p-6">
          <PayableHistory
            key={selected}
            scope={scope}
            id={selected}
            timeZone={timeZone}
            writable={writable}
            onClose={() => setSelected(null)}
          />
        </section>
      )}
      <section
        aria-labelledby="inventory-audit-heading"
        className="rounded-2xl border bg-card p-5 sm:p-6"
      >
        <h3 id="inventory-audit-heading" className="flex items-center gap-2 font-semibold">
          <PackageSearch className="size-5 text-(--hms-accent)" />
          Inventory cost audit
        </h3>
        <p className="mt-2 mb-5 max-w-3xl text-sm leading-6 text-muted-foreground">
          Every manufacturer lot appears once. Acquisition cost excludes recoverable tax. Missing
          receipt costs, credits or stock corrections require review before showing a stock
          valuation.
        </p>
        {audit.isError ? (
          <RequestFailure message="Could not load the inventory cost audit." retry={audit.retry} />
        ) : audit.isPending ? (
          <p role="status">Loading inventory audit…</p>
        ) : !audit.items.length ? (
          <p className="py-8 text-sm text-muted-foreground">
            No inventory batches registered for this outlet.
          </p>
        ) : (
          <div className="overflow-x-auto rounded-xl border">
            <table
              aria-label="Inventory cost audit"
              className="block w-full text-left text-sm md:table md:min-w-[840px]"
            >
              <thead className="sr-only bg-muted/40 text-xs text-muted-foreground md:not-sr-only">
                <tr>
                  {[
                    "Drug / lot",
                    "Expiry",
                    "Received",
                    "On hand",
                    "Acquisition cost",
                    "Stock valuation",
                  ].map((h) => (
                    <th scope="col" key={h} className="px-4 py-3 font-medium">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="block divide-y md:table-row-group">
                {audit.items.map((row) => (
                  <tr key={row.id} className="grid grid-cols-2 md:table-row">
                    <td className="col-span-2 min-w-0 wrap-anywhere px-4 pt-4 pb-2 md:py-4">
                      <p className="font-medium">{row.brand_name}</p>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {row.sku} · {row.lot_number}
                      </p>
                    </td>
                    <td className="col-span-2 px-4 py-2 text-muted-foreground md:py-4 md:whitespace-nowrap">
                      <span aria-hidden="true" className="mr-2 text-xs md:hidden">
                        Expiry
                      </span>
                      {row.expires_on}
                    </td>
                    <td className="min-w-0 wrap-anywhere px-4 py-2 tabular-nums md:py-4">
                      <span
                        aria-hidden="true"
                        className="mb-1 block text-xs text-muted-foreground md:hidden"
                      >
                        Received
                      </span>
                      {row.received_units}
                    </td>
                    <td className="min-w-0 wrap-anywhere px-4 py-2 tabular-nums md:py-4">
                      <span
                        aria-hidden="true"
                        className="mb-1 block text-xs text-muted-foreground md:hidden"
                      >
                        On hand
                      </span>
                      {row.on_hand_units}{" "}
                      <span className="text-xs text-muted-foreground">{row.unit_of_issue}</span>
                    </td>
                    <td className="col-span-2 min-w-0 wrap-anywhere px-4 py-2 tabular-nums md:py-4 md:whitespace-nowrap">
                      <span
                        aria-hidden="true"
                        className="mb-1 block text-xs text-muted-foreground md:hidden"
                      >
                        Acquisition cost
                      </span>
                      {formatPharmacyMoney(row.acquisition_cost_minor, row.currency)}
                      {row.uncosted_receipt_units > 0 && (
                        <p className="mt-1 text-xs text-amber-600 dark:text-amber-400">
                          {row.uncosted_receipt_units} uncosted units
                        </p>
                      )}
                    </td>
                    <td className="col-span-2 px-4 pt-2 pb-4 md:py-4">
                      <span
                        aria-hidden="true"
                        className="mb-1 block text-xs text-muted-foreground md:hidden"
                      >
                        Stock valuation
                      </span>
                      <p
                        className={
                          row.valuation_minor === null
                            ? "text-amber-600 dark:text-amber-400"
                            : "font-semibold tabular-nums"
                        }
                      >
                        {row.valuation_minor === null
                          ? {
                              cost_missing: "Purchase cost missing",
                              valuation_review: "Valuation review required",
                              no_receipts: "No receipts",
                              complete: "Unavailable",
                            }[row.valuation_status]
                          : formatPharmacyMoney(row.valuation_minor, row.currency)}
                      </p>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <LoadMore
          hasMore={audit.hasMore}
          isLoading={audit.isFetchingMore}
          onClick={audit.loadMore}
        />
      </section>
    </div>
  );
}
