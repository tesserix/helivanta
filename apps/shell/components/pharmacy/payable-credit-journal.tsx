"use client";
import "@/lib/csp-zod";
import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@tesserix/web";
import {
  listPharmacyPayableCreditAllocations,
  readPharmacySupplierCreditAllocationReversal,
  pharmacySupplierCreditKey,
  pharmacyKey,
  usePagedQuery,
  formatZonedDateTime,
  type PharmacyActorScope,
} from "@helivanta/api";
import { AccountName, LoadMore, Panel, RefreshButton, RequestFailure } from "@helivanta/ui";
import { formatPharmacyMoney } from "./currency-amount-field";
type Props = { scope: PharmacyActorScope; payable: string; currency: string; timeZone?: string };
export function PayableCreditJournal(props: Props) {
  return (
    <Journal
      key={JSON.stringify([
        props.scope.tenantId,
        props.scope.subject,
        props.scope.pharmacyId,
        props.scope.outletId,
        props.payable,
      ])}
      {...props}
    />
  );
}
function Journal({ scope, payable, currency, timeZone = "UTC" }: Props) {
  const client = useQueryClient();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const history = usePagedQuery(
    pharmacySupplierCreditKey(scope, "payable-journal", payable),
    (cursor, signal) => listPharmacyPayableCreditAllocations(scope, payable, cursor, signal),
    { gcTime: 0, retry: false },
  );
  const selected = history.isError
    ? undefined
    : history.items.find((row) => row.id === selectedId && row.reversal_id !== null);
  const reversal = useQuery({
    queryKey: pharmacySupplierCreditKey(
      scope,
      "payable-journal-reversal",
      payable,
      selected?.id,
      selected?.reversal_id,
    ),
    queryFn: ({ signal }) => {
      if (!selected) throw new Error("Choose an original application reversal.");
      return readPharmacySupplierCreditAllocationReversal(
        scope,
        selected.credit_id,
        selected.id,
        signal,
        selected,
      );
    },
    enabled: !!selected,
    gcTime: 0,
    retry: false,
  });
  return (
    <Panel>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h4 className="font-semibold">Supplier credit applications</h4>
        <RefreshButton
          refresh={() => client.invalidateQueries({ queryKey: pharmacyKey(scope) })}
          error={history.error}
          busy={history.isRefreshing || history.isFetchingMore}
        >
          Refresh bill and credit history
        </RefreshButton>
      </div>
      <p className="my-3 text-sm text-muted-foreground">
        Bill credits include direct adjustments and managed supplier-credit applications. These rows
        retain original applications; bill totals come from the bill record, not a sum of loaded
        pages. Matching a return alone does not settle a bill.
      </p>
      {history.isError ? (
        <RequestFailure
          message="Supplier credit history is unavailable. Previously loaded evidence is hidden."
          error={history.error}
          retry={history.retry}
          recheck="Check bill credit access"
        />
      ) : history.isPending ? (
        <p role="status">Loading supplier credit applications…</p>
      ) : (
        <>
          {!history.items.length ? (
            <p>No managed supplier credits applied to this bill.</p>
          ) : (
            <div className="overflow-x-auto">
              <table
                aria-label="Bill supplier credit applications"
                className="w-full text-left text-sm"
              >
                <thead>
                  <tr>
                    {[
                      "Credit",
                      "Applied amount",
                      "Status",
                      "Date",
                      "Reason",
                      "Recorded by",
                      "Review",
                    ].map((label) => (
                      <th key={label} className="p-3">
                        {label}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {history.items.map((row) => (
                    <tr key={row.id} className="border-t">
                      <td className="p-3">{row.credit_document_reference}</td>
                      <td className="p-3 tabular-nums">
                        {formatPharmacyMoney(row.amount_minor, currency)}
                      </td>
                      <td className="p-3">{row.reversal_id ? "Reversed" : "Active"}</td>
                      <td className="p-3">
                        <time dateTime={row.occurred_on}>{row.occurred_on}</time>
                      </td>
                      <td className="p-3">{row.reason}</td>
                      <td className="p-3">
                        <AccountName subject={row.recorded_by} />
                        <br />
                        <time dateTime={row.recorded_at}>
                          {formatZonedDateTime(row.recorded_at, timeZone)}
                        </time>
                      </td>
                      <td className="p-3">
                        {row.reversal_id ? (
                          <Button
                            variant="outline"
                            size="sm"
                            aria-label={`View reversal for ${row.credit_document_reference}`}
                            onClick={() => setSelectedId(row.id)}
                          >
                            View reversal
                          </Button>
                        ) : (
                          "—"
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <LoadMore
            label="Load more supplier credit applications"
            hasMore={history.hasMore}
            isLoading={history.isFetchingMore}
            onClick={history.loadMore}
          />
          {selected && (
            <section
              aria-label="Supplier credit application reversal"
              className="mt-4 space-y-3 rounded-lg bg-muted/40 p-4"
            >
              <div className="flex flex-wrap items-center justify-between gap-3">
                <h5 className="font-semibold">Reversal of {selected.credit_document_reference}</h5>
                <Button variant="ghost" onClick={() => setSelectedId(null)}>
                  Close reversal details
                </Button>
              </div>
              {reversal.isError ? (
                <RequestFailure
                  message="Original reversal evidence is unavailable. Previously loaded details are hidden."
                  error={reversal.error}
                  retry={reversal.refetch}
                  recheck="Check application reversal access"
                />
              ) : reversal.isPending ? (
                <p role="status">Loading retained reversal…</p>
              ) : (
                reversal.data && (
                  <>
                    <p>
                      Original applied amount:{" "}
                      {formatPharmacyMoney(selected.amount_minor, currency)}
                    </p>
                    <p>{reversal.data.reason}</p>
                    <p>
                      Reversed on {reversal.data.occurred_on} by{" "}
                      <AccountName subject={reversal.data.recorded_by} />
                    </p>
                    <p className="text-sm text-muted-foreground">
                      Recorded payments are retained. This reverses the credit application; it does
                      not refund cash or change stock.
                    </p>
                  </>
                )
              )}
            </section>
          )}
        </>
      )}
    </Panel>
  );
}
