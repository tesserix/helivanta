"use client";

import { z } from "@/lib/csp-zod";
import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Input } from "@tesserix/web";
import { Field, Pager, Panel, RequestFailure, useLeaveGuard, useConfirmLeave } from "@helivanta/ui";
import {
  listPharmacySupplierReturns,
  readPharmacySupplierReturn,
  pharmacySupplierReturnKey,
  cancelPharmacySupplierReturn,
  recoverPharmacySupplierReturnCancellation,
  ApiError,
  newPharmacyId,
  pharmacySupplierReturnCancelInputSchema,
  type PharmacyActorScope,
  type PharmacySupplierReturn,
} from "@helivanta/api";
import { useApiMutation } from "@helivanta/api";
import { useUrlPage } from "@/lib/use-url-page";
import {
  SupplierReturnDispatch,
  supplierReturnDispatchStorageKey,
} from "./supplier-return-dispatch";
import { formatPharmacyMoney } from "./currency-amount-field";
import { SupplierReturnPrepare } from "./supplier-return-prepare";
import { SupplierReturnNoteDownload } from "./supplier-return-note";

function returnState(state: "draft" | "cancelled" | "dispatched") {
  return state === "draft" ? "Draft" : state === "cancelled" ? "Cancelled" : "Dispatched";
}

export function SupplierReturnHistory({
  scope,
  canCancel,
  canDispatch = false,
  canPrepare = false,
  timeZone,
}: {
  scope: PharmacyActorScope;
  canCancel: boolean;
  canDispatch?: boolean;
  canPrepare?: boolean;
  timeZone: string;
}) {
  const confirmLeave = useConfirmLeave();
  const url = useUrlPage({ tab: "supplier-returns" });
  const [selected, setSelected] = useState<string | null>(null);
  const history = useQuery({
    queryKey: pharmacySupplierReturnKey(scope, "history", url.cursor),
    queryFn: ({ signal }) => listPharmacySupplierReturns(scope, url.cursor, signal),
    gcTime: 0,
    retry: false,
  });
  return (
    <div className="space-y-5">
      <SupplierReturnPrepare
        scope={scope}
        canStart={canPrepare && !history.isError && !history.isPending}
        onPrepared={setSelected}
      />
      <Panel>
        <h3 className="font-semibold">Supplier returns</h3>
        <p className="mt-2 text-sm text-muted-foreground">
          Expected credit is not a supplier credit or bill settlement.
        </p>
        {history.isError ? (
          <RequestFailure
            message="Supplier return history is unavailable. Previously loaded records are hidden."
            error={history.error}
            retry={history.refetch}
          />
        ) : history.isPending ? (
          <p role="status">Loading supplier return history…</p>
        ) : (
          <>
            {!history.data.data.length ? (
              <p className="py-5 text-sm text-muted-foreground">
                No supplier returns recorded for this outlet.
              </p>
            ) : (
              <div className="overflow-x-auto">
                <table aria-label="Supplier return history" className="w-full text-left text-sm">
                  <thead>
                    <tr>
                      {[
                        "Return",
                        "Supplier",
                        "Original bill",
                        "Status",
                        "Expected credit",
                        "Prepared",
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
                            aria-label={`Open return ${row.number}`}
                            onClick={() => confirmLeave(() => setSelected(row.id))}
                          >
                            Return {row.number}
                          </Button>
                        </td>
                        <td className="p-3">{row.payee_name}</td>
                        <td className="p-3">{row.document_reference}</td>
                        <td className="p-3">{returnState(row.state)}</td>
                        <td className="p-3 tabular-nums">
                          {formatPharmacyMoney(row.expected_credit_minor, row.currency)}
                        </td>
                        <td className="p-3">
                          {new Intl.DateTimeFormat("en-IN", {
                            timeZone,
                            dateStyle: "medium",
                          }).format(new Date(row.prepared_at))}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <Pager
              page={url.page}
              pageSize={25}
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
        <SupplierReturnDetail
          key={selected}
          scope={scope}
          id={selected}
          canCancel={canCancel}
          canDispatch={canDispatch}
          timeZone={timeZone}
          close={() => confirmLeave(() => setSelected(null))}
        />
      )}
    </div>
  );
}

function SupplierReturnDetail({
  scope,
  id,
  canCancel,
  canDispatch,
  timeZone,
  close,
}: {
  scope: PharmacyActorScope;
  id: string;
  canCancel: boolean;
  canDispatch: boolean;
  timeZone: string;
  close: () => void;
}) {
  const detail = useQuery({
    queryKey: pharmacySupplierReturnKey(scope, "detail", id),
    queryFn: ({ signal }) => readPharmacySupplierReturn(scope, id, signal),
    gcTime: 0,
    retry: false,
  });
  return (
    <Panel>
      <Button variant="ghost" onClick={close}>
        Close return details
      </Button>
      {detail.isError ? (
        <RequestFailure
          message="Return details are unavailable. Previously loaded evidence is hidden."
          error={detail.error}
          retry={detail.refetch}
        />
      ) : detail.isPending ? (
        <p role="status">Loading return details…</p>
      ) : (
        <>
          <h3 className="mt-3 font-semibold">
            Return {detail.data.number} · {detail.data.payee_name}
          </h3>
          <p className="mt-2 text-sm">
            Original bill {detail.data.document_reference} · {returnState(detail.data.state)}
          </p>
          <p className="mt-2 text-sm">{detail.data.reason}</p>
          <div className="my-4 overflow-x-auto">
            <table aria-label="Supplier return lines" className="w-full text-left text-sm">
              <thead>
                <tr>
                  {["Product", "Batch", "Expiry", "Units", "Expected credit"].map((label) => (
                    <th key={label} scope="col" className="p-3">
                      {label}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {detail.data.lines.map((line) => (
                  <tr key={line.id} className="border-t">
                    <td className="p-3">
                      {line.product_name}
                      <span className="block text-muted-foreground">{line.generic_name}</span>
                    </td>
                    <td className="p-3">{line.lot_number}</td>
                    <td className="p-3">{line.expires_on}</td>
                    <td className="p-3">
                      {line.quantity_units} {line.unit_code}
                    </td>
                    <td className="p-3 tabular-nums">
                      {formatPharmacyMoney(line.expected_credit_minor, detail.data.currency)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <SupplierReturnDispatch
            scope={scope}
            value={detail.data}
            canStart={canDispatch}
            timeZone={timeZone}
          />
          <SupplierReturnNoteDownload scope={scope} value={detail.data} disabled={detail.isFetching} />
          {
            <CancelReturn
              key={id}
              scope={scope}
              value={detail.data}
              canCancel={canCancel && detail.data.state === "draft"}
            />
          }
        </>
      )}
    </Panel>
  );
}

function CancelReturn({
  scope,
  value,
  canCancel,
}: {
  scope: PharmacyActorScope;
  value: PharmacySupplierReturn;
  canCancel: boolean;
}) {
  const client = useQueryClient();
  const storageKey =
    "helivanta:supplier-return-cancellation:v1:" +
    JSON.stringify([scope.tenantId, scope.subject, scope.pharmacyId, scope.outletId, value.id]);
  const [reason, setReason] = useState("");
  const [verified, setVerified] = useState(false);
  const [failure, setFailure] = useState("");
  const [notFound, setNotFound] = useState(false);
  const [command, setCommand] = useState<string | null>(() => {
    try {
      const value = sessionStorage.getItem(storageKey);
      return value === null
        ? null
        : z.string().guid().safeParse(value).success
          ? value
          : "unavailable";
    } catch {
      return "unavailable";
    }
  });
  function confirmed() {
    try {
      sessionStorage.removeItem(storageKey);
    } catch {
      /* Confirmed server result remains authoritative. */
    }
    setVerified(true);
    setCommand(null);
    void client.invalidateQueries({ queryKey: pharmacySupplierReturnKey(scope) });
  }
  const cancel = useApiMutation(
    async (input: { command: string; reason: string }) =>
      cancelPharmacySupplierReturn(scope, value.id, input.command, {
        expected_version: value.version,
        reason: input.reason,
      }),
    { onSuccess: confirmed, suppressErrorToast: true },
  );
  const recover = useApiMutation(
    () => recoverPharmacySupplierReturnCancellation(scope, value.id, command!),
    {
      onSuccess: confirmed,
      onError: (error) => {
        if (error instanceof ApiError && error.status === 404) setNotFound(true);
      },
      suppressErrorToast: true,
    },
  );
  const pending = cancel.isPending || recover.isPending;
  useLeaveGuard(
    !verified && (!!reason || command !== null),
    `cancellation for return ${value.number}`,
    pending,
  );
  if (!canCancel && command === null && !verified) return null;
  if (verified) return <p role="status">Draft cancellation verified.</p>;
  return (
    <form
      className="space-y-3 border-t pt-4"
      onSubmit={(event) => {
        event.preventDefault();
        if (pending || !canCancel || (command !== null && (!notFound || command === "unavailable")))
          return;
        const parsed = pharmacySupplierReturnCancelInputSchema.safeParse({
          expected_version: value.version,
          reason,
        });
        if (!parsed.success) {
          setFailure("Provide a cancellation reason of 1 to 500 characters.");
          return;
        }
        const id = command ?? newPharmacyId();
        try {
          if (sessionStorage.getItem(supplierReturnDispatchStorageKey(scope, value.id)) !== null) {
            setFailure("Verify the pending physical dispatch before cancelling this return.");
            return;
          }
          sessionStorage.setItem(storageKey, id);
          if (sessionStorage.getItem(storageKey) !== id) throw new Error("storage");
        } catch {
          setFailure("Recovery storage is unavailable. No cancellation was sent.");
          return;
        }
        setFailure("");
        setNotFound(false);
        setCommand(id);
        cancel.mutate({ command: id, reason: parsed.data.reason });
      }}
    >
      <h4 className="font-medium">Cancel this draft</h4>
      <p className="text-sm text-muted-foreground">
        Cancellation preserves this return’s history and changes no stock or bill settlement.
      </p>
      {command ? (
        <>
          <p role="status">
            Keep this return open until the original cancellation result is verified.
          </p>
          {notFound && canCancel && (
            <>
              <p className="text-sm">
                No cancellation is recorded for this reference. Retry using the original reason and
                reference, or check again if the request may still be processing.
              </p>
              <Field id="supplier-return-cancel-reason" label="Cancellation reason">
                <Input
                  id="supplier-return-cancel-reason"
                  value={reason}
                  maxLength={500}
                  onChange={(event) => setReason(event.target.value)}
                  disabled={pending}
                />
              </Field>
              <Button type="submit" variant="outline" disabled={pending}>
                Retry original cancellation
              </Button>
            </>
          )}
          {command === "unavailable" ? (
            <p role="alert">
              Recovery storage is unavailable. Reload after restoring browser storage.
            </p>
          ) : (
            <Button
              type="button"
              variant="outline"
              disabled={pending}
              onClick={() => recover.mutate()}
            >
              Check cancellation result
            </Button>
          )}
        </>
      ) : (
        <>
          <Field id="supplier-return-cancel-reason" label="Cancellation reason">
            <Input
              id="supplier-return-cancel-reason"
              value={reason}
              maxLength={500}
              onChange={(event) => setReason(event.target.value)}
              disabled={pending}
            />
          </Field>
          <Button type="submit" variant="outline" disabled={pending}>
            Cancel draft
          </Button>
        </>
      )}
      {(failure || cancel.isError || recover.isError) && (
        <p role="alert" className="text-sm text-destructive">
          {failure || recover.error?.message || cancel.error?.message}
        </p>
      )}
    </form>
  );
}
