"use client";

import { useState, useSyncExternalStore } from "react";
import {
  listPharmacySupplierCreditAllocations,
  pharmacySupplierCreditKey,
  usePagedQuery,
  type PharmacyActorScope,
  type PharmacySupplierCreditContext,
} from "@helivanta/api";
import { Button } from "@tesserix/web";
import { AccountName, LoadMore, Panel, RequestFailure, useConfirmLeave } from "@helivanta/ui";
import { formatPharmacyMoney } from "./currency-amount-field";
import { SupplierCreditApplicationForm } from "./supplier-credit-application-form";
import { SupplierCreditApplicationReview } from "./supplier-credit-application-review";
import {
  creditApplicationCommandKey,
  readCreditApplicationReference,
} from "./use-credit-application-command";
type Props = {
  scope: PharmacyActorScope;
  credit: PharmacySupplierCreditContext;
  canManage: boolean;
};
const subscribe = () => () => {};
export function SupplierCreditApplications(props: Props) {
  const ready = useSyncExternalStore(
    subscribe,
    () => true,
    () => false,
  );
  if (!ready) return <p role="status">Checking bill application recovery…</p>;
  return (
    <ApplicationWorkspace
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
function ApplicationWorkspace({ scope, credit, canManage }: Props) {
  const [review, setReview] = useState<{ id: string; sequence: number } | null>(() => {
    const saved = readCreditApplicationReference(
      creditApplicationCommandKey(scope, credit.id, "application-reversal"),
    );
    return saved.reference?.target ? { id: saved.reference.target, sequence: 0 } : null;
  });
  const confirmLeave = useConfirmLeave();
  const history = usePagedQuery(
    pharmacySupplierCreditKey(scope, "applications", credit.id),
    (cursor, signal) => listPharmacySupplierCreditAllocations(scope, credit.id, cursor, signal),
    { gcTime: 0, retry: false },
  );
  return (
    <Panel>
      <h4 className="font-semibold">Bill applications</h4>
      <p className="my-3 text-sm text-muted-foreground">
        Applying actual credit reduces both unused credit and the selected bill's liability. Return
        matching, physical stock and cash remain separate.
      </p>
      {history.isError ? (
        <RequestFailure
          message="Bill application history is unavailable. Previously loaded evidence is hidden."
          error={history.error}
          retry={history.retry}
        />
      ) : history.isPending ? (
        <p role="status">Loading bill applications…</p>
      ) : (
        <>
          {!review && (
            <SupplierCreditApplicationForm scope={scope} credit={credit} canManage={canManage} />
          )}
          {!history.items.length ? (
            <p>No bill applications recorded for this credit.</p>
          ) : (
            <div className="overflow-x-auto">
              <table
                aria-label="Supplier credit bill applications"
                className="w-full text-left text-sm"
              >
                <thead>
                  <tr>
                    {[
                      "Bill",
                      "Applied amount",
                      "Status",
                      "Date",
                      "Reason",
                      "Recorded by",
                      "Review",
                    ].map((label) => (
                      <th scope="col" key={label} className="p-3">
                        {label}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {history.items.map((row) => (
                    <tr key={row.id} className="border-t">
                      <td className="p-3">{row.payable_reference}</td>
                      <td className="p-3 tabular-nums">
                        {formatPharmacyMoney(row.amount_minor, credit.currency)}
                      </td>
                      <td className="p-3">{row.reversal_id ? "Reversed" : "Active"}</td>
                      <td className="p-3">{row.occurred_on}</td>
                      <td className="p-3">{row.reason}</td>
                      <td className="p-3">
                        <AccountName subject={row.recorded_by} />
                      </td>
                      <td className="p-3">
                        <Button
                          variant="outline"
                          aria-label={`Review application for ${row.payable_reference}`}
                          onClick={() =>
                            confirmLeave(() =>
                              setReview((current) => ({
                                id: row.id,
                                sequence: (current?.sequence ?? 0) + 1,
                              })),
                            )
                          }
                        >
                          Review application
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <LoadMore
            label="Load more bill applications"
            hasMore={history.hasMore}
            isLoading={history.isFetchingMore}
            onClick={history.loadMore}
          />
          <SupplierCreditApplicationReview
            key={review?.sequence ?? "recovery"}
            scope={scope}
            credit={credit.id}
            requested={review}
            canManage={canManage}
            onClose={() => setReview(null)}
          />
        </>
      )}
    </Panel>
  );
}
