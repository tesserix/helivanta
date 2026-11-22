"use client";

import { useState } from "react";
import { Button } from "@tesserix/web";

import {
  listPharmacySupplierCreditMatches,
  pharmacySupplierCreditKey,
  usePagedQuery,
  type PharmacyActorScope,
  type PharmacySupplierCreditContext,
} from "@helivanta/api";
import { AccountName, LoadMore, Panel, RequestFailure, useConfirmLeave } from "@helivanta/ui";
import { formatPharmacyMoney } from "./currency-amount-field";
import { SupplierCreditMatchForm } from "./supplier-credit-match-form";
import { SupplierCreditMatchReversal } from "./supplier-credit-match-reversal";

type Props = {
  scope: PharmacyActorScope;
  credit: PharmacySupplierCreditContext;
  canManage: boolean;
};
export function SupplierCreditMatching(props: Props) {
  return (
    <MatchingHistory
      key={JSON.stringify([
        props.scope.tenantId,
        props.scope.subject,
        props.scope.pharmacyId,
        props.scope.outletId,
        props.credit.id,
      ])}
      {...props}
    />
  );
}
function MatchingHistory({ scope, credit, canManage }: Props) {
  const [review, setReview] = useState<{ id: string; sequence: number } | null>(null);
  const confirmLeave = useConfirmLeave();
  const history = usePagedQuery(
    pharmacySupplierCreditKey(scope, "matches", credit.id),
    (cursor, signal) => listPharmacySupplierCreditMatches(scope, credit.id, cursor, signal),
    { gcTime: 0, retry: false },
  );
  return (
    <Panel>
      <h4 className="font-semibold">Return matching</h4>
      <p className="my-3 text-sm text-muted-foreground">
        Matching attributes actual credit to dispatched returns. It does not apply credit to bills
        or move stock.
      </p>
      {history.isError ? (
        <RequestFailure
          message="Return matching history is unavailable. Previously loaded evidence is hidden."
          error={history.error}
          retry={history.retry}
        />
      ) : history.isPending ? (
        <p role="status">Loading return matching…</p>
      ) : (
        <>
          <SupplierCreditMatchForm scope={scope} credit={credit} canManage={canManage} />
          {!history.items.length ? (
            <p>No returns matched to this credit.</p>
          ) : (
            <div className="overflow-x-auto">
              <table
                aria-label="Supplier credit return matching"
                className="w-full text-left text-sm"
              >
                <thead>
                  <tr>
                    {["Return", "Matched amount", "Status", "Reason", "Recorded by", "Review"].map(
                      (label) => (
                        <th scope="col" className="p-3" key={label}>
                          {label}
                        </th>
                      ),
                    )}
                  </tr>
                </thead>
                <tbody>
                  {history.items.map((row) => (
                    <tr key={row.id} className="border-t">
                      <td className="p-3">Return {row.return_number}</td>
                      <td className="p-3 tabular-nums">
                        {formatPharmacyMoney(row.amount_minor, credit.currency)}
                      </td>
                      <td className="p-3">{row.reversal_id ? "Reversed" : "Active"}</td>
                      <td className="p-3">{row.reason}</td>
                      <td className="p-3">
                        <AccountName subject={row.recorded_by} />
                      </td>
                      <td className="p-3">
                        <Button
                          variant="outline"
                          aria-label={`Review match for return ${row.return_number}`}
                          onClick={() =>
                            confirmLeave(() =>
                              setReview((current) => ({
                                id: row.id,
                                sequence: (current?.sequence ?? 0) + 1,
                              })),
                            )
                          }
                        >
                          Review match
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <LoadMore
            label="Load more return matches"
            hasMore={history.hasMore}
            isLoading={history.isFetchingMore}
            onClick={history.loadMore}
          />
          <SupplierCreditMatchReversal
            scope={scope}
            credit={credit.id}
            requested={review}
            canManage={canManage}
          />
        </>
      )}
    </Panel>
  );
}
