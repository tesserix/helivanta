"use client";

import { useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import { Button } from "@tesserix/web";
import { ReceiptText, PackageSearch, Plus } from "lucide-react";
import {
  PAGE_SIZE,
  useApiPageQuery,
  usePermissions,
  pharmacyKey,
  pharmacyRoot,
  type PharmacyScope,
  type PharmacyPayable,
  type PharmacyInventoryAudit,
} from "@helivanta/api";
import { ListDetail, PageTabs, Pager, Panel, RequestFailure } from "@helivanta/ui";
import { useListDetail } from "@/lib/use-list-detail";
import { useStepBackWhenEmpty, useUrlPage } from "@/lib/use-url-page";
import { formatPharmacyMoney } from "./currency-amount-field";
import { PayableForm, PayableHistory } from "./payable-forms";

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
      <Panel>
        <h3 className="font-semibold">Financial access required</h3>
        <p className="mt-2 text-sm text-muted-foreground">
          Ask your administrator for financial permissions and access to this outlet.
        </p>
      </Panel>
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
  // The tab is the URL (#576 slice 3 D4): Overdue and Due within 7 days are
  // the overview's counts, filtered on the outlet's date the same way.
  // Anything else, an old ?due= link included, is All bills.
  const raw = useSearchParams().get("tab") ?? "";
  const tab: BillsTab = Object.hasOwn(TABS, raw) ? (raw as BillsTab) : "all";
  const url = useUrlPage({ tab });
  const [pageRows, setPageRows] = useState<Row[]>([]);
  const pane = useListDetail({
    rows: pageRows,
    name: (row) => row.reference,
    fallbackName: "this bill",
    newName: "the new bill",
    headingId: "payables-heading",
  });
  const { detail, paneRef } = pane;
  const reference = pane.openName;
  const onStateChange = pane.formState("bill");

  const tabs = (
    <PageTabs
      label="Supplier bills"
      current={tab}
      tabs={(Object.keys(TABS) as BillsTab[]).map((key) => ({
        key,
        label: TABS[key],
        href: url.hrefFor({ tab: key }),
      }))}
    />
  );
  if (tab === "audit")
    return (
      <div className="space-y-6">
        <InventoryAudit scope={scope} url={url} tabs={tabs} />
      </div>
    );

  const record = detail.creating ? (
    <Panel>
      <PayableForm
        scope={scope}
        defaultCurrency={defaultCurrency}
        onSaved={pane.saved}
        onCancel={pane.close}
        onStateChange={onStateChange}
      />
    </Panel>
  ) : detail.openId ? (
    <Panel>
      <PayableHistory
        key={detail.openId}
        scope={scope}
        id={detail.openId}
        timeZone={timeZone}
        writable={writable}
        onClose={pane.close}
        onStateChange={onStateChange}
        previous={{ disabled: !pane.canStep(-1), onClick: () => pane.step(-1) }}
        next={{ disabled: !pane.canStep(1), onClick: () => pane.step(1) }}
      />
    </Panel>
  ) : null;

  return (
    <div className="space-y-6" onClickCapture={pane.holdLinks}>
      <Bills
        scope={scope}
        tab={tab}
        url={url}
        tabs={tabs}
        compact={pane.isOpen}
        openId={detail.openId}
        onRows={setPageRows}
        canRecord={writable && can("pharmacy.accounts.write")}
        onRecord={pane.create}
        onOpen={pane.openRow}
        pane={{
          detail: record && <div ref={paneRef}>{record}</div>,
          label: detail.creating
            ? "New supplier bill"
            : reference
              ? `Bill ${reference}`
              : "Supplier bill",
          backHref: detail.closeHref,
        }}
      />
    </div>
  );
}

type Row = { id: string; reference: string };

const TABS = {
  overdue: "Overdue",
  week: "Due within 7 days",
  all: "All bills",
  audit: "Inventory cost audit",
} as const;
type BillsTab = keyof typeof TABS;
type UrlPage = ReturnType<typeof useUrlPage>;

// One tab of bills, 25 a page with the total (#576 slice 3).
function Bills({
  scope,
  tab,
  url,
  tabs,
  canRecord,
  onRecord,
  onOpen,
  compact,
  openId,
  onRows,
  pane,
}: {
  scope: PharmacyScope;
  tab: Exclude<BillsTab, "audit">;
  url: UrlPage;
  tabs: React.ReactNode;
  canRecord: boolean;
  onRecord: () => void;
  onOpen: (id: string) => void;
  /** Fewer columns while a bill is open beside the list (#1303 D1). */
  compact: boolean;
  openId: string | null;
  onRows: (rows: Row[]) => void;
  /** The open bill, shown beside the table or alone. */
  pane: { detail: React.ReactNode | null; label: string; backHref: string };
}) {
  const due = tab === "all" ? null : tab;
  const bills = useApiPageQuery<PharmacyPayable>(
    pharmacyKey(scope, "accounts", "payables", "counted", due),
    `${pharmacyRoot(scope)}/accounts/payables/counted${due ? `?due=${due}` : ""}`,
    url.cursor,
  );
  const emptied = useStepBackWhenEmpty(bills, url);
  const rowKey = bills.items.map((b) => `${b.id}:${b.document_reference}`).join();
  useEffect(() => {
    onRows(bills.items.map((b) => ({ id: b.id, reference: b.document_reference })));
    // rowKey stands for the rows: a new array with the same rows is no change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rowKey, onRows]);
  // Beside an open bill the column is about 440px at the narrowest laptop:
  // short headings on one line, and tighter cells, so nothing wraps the rows
  // down or overflows under the pane (#1303).
  const columns = compact
    ? ["Bill / payee", "Outstanding", "History"]
    : ["Document / payee", "Due date", "Billed", "Paid", "Credits", "Outstanding", "History"];
  const cell = compact ? "px-3" : "px-4";
  const amounts = (b: PharmacyPayable) =>
    compact
      ? ([["Outstanding", b.outstanding_minor]] as const)
      : ([
          ["Billed", b.total_minor],
          ["Paid", b.paid_minor],
          ["Credits", b.credit_minor],
          ["Outstanding", b.outstanding_minor],
        ] as const);
  // The tabs and heading span the page above the split, so opening a bill
  // narrows only the table: nothing above the rows reflows (#1303).
  return (
    <div className="space-y-4">
      <Panel flush aria-labelledby="payables-heading">
        {tabs}
        <div className="p-5">
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div>
              <h3
                id="payables-heading"
                tabIndex={-1}
                className="flex items-center gap-2 font-semibold outline-none"
              >
                <ReceiptText className="size-5 text-(--hms-accent)" />
                Bills & expenses
              </h3>
              <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">
                Track original bills, recorded payments and remaining amounts. Each document keeps
                its own currency and history.
              </p>
            </div>
            {canRecord && (
              <Button onClick={onRecord}>
                <Plus className="size-4" />
                Record bill or expense
              </Button>
            )}
          </div>
        </div>
      </Panel>
      <ListDetail
        label={pane.label}
        backHref={pane.backHref}
        backLabel="Back to bills"
        detail={pane.detail}
        list={
          <Panel flush>
            <div className="p-5">
              {bills.isError ? (
                <RequestFailure message="Could not load bills and expenses." retry={bills.retry} />
              ) : bills.isPending ? (
                <p role="status">Loading bills and expenses…</p>
              ) : !bills.items.length ? (
                <p className="py-8 text-sm text-muted-foreground">
                  {
                    {
                      overdue: "No overdue bills.",
                      week: "No bills due within 7 days.",
                      all: "No bills or expenses recorded for this outlet.",
                    }[tab]
                  }
                </p>
              ) : (
                <div className="overflow-x-auto rounded-md border">
                  <table
                    aria-label="Bills and expenses"
                    className={`block w-full text-left text-sm md:table ${compact ? "" : "md:min-w-[780px]"}`}
                  >
                    <thead className="sr-only bg-muted/40 text-xs text-muted-foreground md:not-sr-only">
                      <tr>
                        {columns.map((h) => (
                          <th
                            key={h}
                            scope="col"
                            className={`${cell} py-3 font-medium whitespace-nowrap`}
                          >
                            {compact && h === "History" ? <span className="sr-only">{h}</span> : h}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody className="block divide-y md:table-row-group">
                      {bills.items.map((b) => (
                        <tr
                          key={b.id}
                          aria-current={b.id === openId ? "true" : undefined}
                          className={`grid grid-cols-2 md:table-row ${b.id === openId ? "bg-(--hms-accent-dim)" : ""}`}
                        >
                          <td className={`col-span-2 min-w-0 ${cell} pt-4 pb-2 md:py-4`}>
                            <p className="wrap-anywhere font-medium">{b.document_reference}</p>
                            <p className="mt-1 text-xs text-muted-foreground">{b.payee_name}</p>
                            {/* Beside an open bill the due date joins the bill, so the
                                reference keeps the width it needs. */}
                            {compact && (
                              <p className="mt-1 text-xs text-muted-foreground">Due {b.due_on}</p>
                            )}
                          </td>
                          {!compact && (
                            <td
                              className={`col-span-2 ${cell} py-2 text-muted-foreground md:py-4 md:whitespace-nowrap`}
                            >
                              <span aria-hidden="true" className="mr-2 text-xs md:hidden">
                                Due
                              </span>
                              {b.due_on}
                            </td>
                          )}
                          {amounts(b).map(([label, v]) => (
                            <td
                              key={label}
                              className={`min-w-0 wrap-anywhere ${cell} py-2 tabular-nums md:py-4 md:whitespace-nowrap ${label === "Outstanding" ? "font-semibold" : ""}`}
                            >
                              <span
                                aria-hidden="true"
                                className="mb-1 block text-xs font-normal text-muted-foreground md:hidden"
                              >
                                {label}
                              </span>
                              {formatPharmacyMoney(v, b.currency)}
                            </td>
                          ))}
                          <td className={`col-span-2 ${cell} py-4`}>
                            <Button
                              variant="ghost"
                              onClick={() => onOpen(b.id)}
                              data-open-row={b.id}
                              aria-label={`Open ${b.document_reference}`}
                            >
                              Open
                            </Button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
            {!bills.isPending && !emptied && (
              <Pager
                page={url.page}
                pageSize={PAGE_SIZE}
                offset={bills.offset}
                shown={bills.items.length}
                total={bills.total}
                previousHref={url.previousHref}
                nextHref={url.nextHref(bills.nextCursor)}
                firstHref={url.first}
              />
            )}
          </Panel>
        }
      />
    </div>
  );
}

// Every manufacturer lot once, 25 a page with the total.
function InventoryAudit({
  scope,
  url,
  tabs,
}: {
  scope: PharmacyScope;
  url: UrlPage;
  tabs: React.ReactNode;
}) {
  const audit = useApiPageQuery<PharmacyInventoryAudit>(
    pharmacyKey(scope, "accounts", "inventory-audit", "counted"),
    `${pharmacyRoot(scope)}/accounts/inventory-audit/counted`,
    url.cursor,
  );
  const emptied = useStepBackWhenEmpty(audit, url);
  return (
    <Panel flush aria-labelledby="inventory-audit-heading">
      {tabs}
      <div className="p-5">
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
          <div className="overflow-x-auto rounded-md border">
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
                        <p className="mt-1 text-xs text-(--hms-amber)">
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
                            ? "text-(--hms-amber)"
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
      </div>
      {!audit.isPending && !emptied && (
        <Pager
          page={url.page}
          pageSize={PAGE_SIZE}
          offset={audit.offset}
          shown={audit.items.length}
          total={audit.total}
          previousHref={url.previousHref}
          nextHref={url.nextHref(audit.nextCursor)}
          firstHref={url.first}
        />
      )}
    </Panel>
  );
}
