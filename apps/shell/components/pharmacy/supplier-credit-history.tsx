"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@tesserix/web";
import {
  listPharmacySupplierCredits,
  readPharmacySupplierCreditContext,
  pharmacySupplierCreditKey,
  type PharmacyActorScope,
  type PharmacySupplierCredit,
} from "@helivanta/api";
import { AccountName, Pager, Panel, RequestFailure, useConfirmLeave } from "@helivanta/ui";
import { useUrlPage } from "@/lib/use-url-page";
import { formatPharmacyMoney } from "./currency-amount-field";
import { SupplierCreditRecord } from "./supplier-credit-record";

const states: Record<PharmacySupplierCredit["state"], string> = {
  active: "Active",
  voided: "Voided",
  corrected: "Corrected",
};
export function SupplierCreditHistory({
  scope,
  canRecord,
  timeZone,
}: {
  scope: PharmacyActorScope;
  canRecord: boolean;
  timeZone: string;
}) {
  const url = useUrlPage({ tab: "supplier-credits" });
  const confirmLeave = useConfirmLeave();
  const [selected, setSelected] = useState<string | null>(null);
  const history = useQuery({
    queryKey: pharmacySupplierCreditKey(scope, "history", url.cursor),
    queryFn: ({ signal }) => listPharmacySupplierCredits(scope, url.cursor, signal),
    gcTime: 0,
    retry: false,
  });
  return (
    <div className="space-y-5">
      {!history.isError && (
        <SupplierCreditRecord
          scope={scope}
          canRecord={canRecord && !history.isError && !history.isPending}
          onRecorded={setSelected}
        />
      )}
      <Panel>
        <h3 className="font-semibold">Supplier credits</h3>
        <p className="mt-2 text-sm text-muted-foreground">
          Actual supplier credit, return matching and bill applications keep separate records.
        </p>
        {history.isError ? (
          <RequestFailure
            message="Supplier credit history is unavailable. Previously loaded records are hidden."
            error={history.error}
            retry={history.refetch}
          />
        ) : history.isPending ? (
          <p role="status">Loading supplier credit history…</p>
        ) : (
          <>
            {!history.data.data.length ? (
              <p className="py-5 text-sm text-muted-foreground">
                No supplier credits recorded for this outlet.
              </p>
            ) : (
              <div className="overflow-x-auto">
                <table aria-label="Supplier credit history" className="w-full text-left text-sm">
                  <thead>
                    <tr>
                      {[
                        "Credit",
                        "Supplier",
                        "Issue date",
                        "Actual credit",
                        "Unused credit",
                        "Status",
                      ].map((label) => (
                        <th key={label} scope="col" className="p-3">
                          {label}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {history.data.data.map((row) => (
                      <tr key={row.id} className="border-t">
                        <td className="p-3">
                          <Button
                            variant="ghost"
                            aria-label={`Open credit ${row.document_reference}`}
                            onClick={() => confirmLeave(() => setSelected(row.id))}
                          >
                            {row.document_reference}
                          </Button>
                        </td>
                        <td className="p-3">{row.payee_name}</td>
                        <td className="p-3">{row.issued_on}</td>
                        <td className="p-3 tabular-nums">
                          {formatPharmacyMoney(row.amount_minor, row.currency)}
                        </td>
                        <td className="p-3 tabular-nums">
                          {formatPharmacyMoney(row.available_minor, row.currency)}
                        </td>
                        <td className="p-3">{states[row.state]}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <Pager
              page={url.page}
              pageSize={50}
              shown={history.data.data.length}
              total={null}
              previousHref={url.previousHref}
              nextHref={
                history.data.page.has_more ? url.nextHref(history.data.page.next_cursor) : null
              }
              firstHref={url.first}
            />
          </>
        )}
      </Panel>
      {selected && !history.isError && (
        <CreditDetail
          key={selected}
          scope={scope}
          id={selected}
          timeZone={timeZone}
          close={() => confirmLeave(() => setSelected(null))}
        />
      )}
    </div>
  );
}
function CreditDetail({
  scope,
  id,
  timeZone,
  close,
}: {
  scope: PharmacyActorScope;
  id: string;
  timeZone: string;
  close: () => void;
}) {
  const detail = useQuery({
    queryKey: pharmacySupplierCreditKey(scope, "detail", id),
    queryFn: ({ signal }) => readPharmacySupplierCreditContext(scope, id, signal),
    gcTime: 0,
    retry: false,
  });
  return (
    <section aria-label="Supplier credit details">
      <Panel>
        <Button variant="ghost" onClick={close}>
          Close credit details
        </Button>
        {detail.isError ? (
          <RequestFailure
            message="Credit details are unavailable. Previously loaded financial data is hidden."
            error={detail.error}
            retry={detail.refetch}
          />
        ) : detail.isPending ? (
          <p role="status">Loading supplier credit details…</p>
        ) : (
          <>
            <h3 className="mt-3 font-semibold">
              Credit {detail.data.document_reference} · {detail.data.payee_name}
            </h3>
            <p className="mt-2 text-sm">
              Original bill {detail.data.original_bill_reference} · Return{" "}
              {detail.data.return_number} · {states[detail.data.state]}
            </p>
            <dl className="my-5 grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
              {(
                [
                  ["Actual credit", detail.data.amount_minor],
                  ["Matched to returns", detail.data.matched_minor],
                  ["Applied to bills", detail.data.allocated_minor],
                  ["Unused credit", detail.data.available_minor],
                ] as const
              ).map(([label, amount]) => (
                <div key={label}>
                  <dt className="text-xs text-muted-foreground">{label}</dt>
                  <dd className="mt-1 font-semibold tabular-nums">
                    {formatPharmacyMoney(amount, detail.data.currency)}
                  </dd>
                </div>
              ))}
            </dl>
            <p className="text-sm">{detail.data.reason}</p>
            <p className="mt-3 text-sm text-muted-foreground">
              Recorded by <AccountName subject={detail.data.recorded_by} /> ·{" "}
              {new Intl.DateTimeFormat("en", {
                timeZone,
                dateStyle: "medium",
                timeStyle: "short",
              }).format(new Date(detail.data.recorded_at))}
            </p>
          </>
        )}
      </Panel>
    </section>
  );
}
