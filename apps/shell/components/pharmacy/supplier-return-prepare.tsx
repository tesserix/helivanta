"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Input } from "@tesserix/web";
import {
  Field,
  Panel,
  LoadMore,
  RequestFailure,
  RefreshButton,
  SelectControl,
  useLeaveGuard,
} from "@helivanta/ui";
import {
  ApiError,
  newPharmacyId,
  pharmacyRoot,
  pharmacyActorHeaders,
  pharmacySupplierReturnKey,
  readPharmacySupplierReturnPreparation,
  listPharmacySupplierReturnOriginalBills,
  usePagedQuery,
  preparePharmacySupplierReturn,
  recoverPharmacySupplierReturnPreparation,
  pharmacySupplierReturnDraftInputSchema,
  useApiMutation,
  useApiPagedQuery,
  type PharmacyActorScope,
  type PharmacyStockPosition,
  type PharmacySupplierReturnInput,
} from "@helivanta/api";
import { z } from "@/lib/csp-zod";
import { CurrencyAmountField, formatPharmacyMoney } from "./currency-amount-field";

type Props = { scope: PharmacyActorScope; canStart: boolean; onPrepared?: (id: string) => void };
type Line = PharmacySupplierReturnInput["lines"][number] & { bucketLabel: string };
function key(scope: PharmacyActorScope) {
  return (
    "helivanta:supplier-return-prepare:v1:" +
    JSON.stringify([scope.tenantId, scope.subject, scope.pharmacyId, scope.outletId])
  );
}
export function SupplierReturnPrepare(props: Props) {
  return <PrepareForm key={key(props.scope)} {...props} />;
}
function PrepareForm({ scope, canStart, onPrepared }: Props) {
  const client = useQueryClient(),
    storageKey = key(scope);
  const [command, setCommand] = useState<string | null>(() => {
    try {
      const stored = sessionStorage.getItem(storageKey);
      return stored === null
        ? null
        : z.string().guid().safeParse(stored).success
          ? stored
          : "unavailable";
    } catch {
      return "unavailable";
    }
  });
  const [notFound, setNotFound] = useState(false),
    [verified, setVerified] = useState<number | null>(null),
    [failure, setFailure] = useState("");
  const [billId, setBillId] = useState(""),
    [receiptId, setReceiptId] = useState(""),
    [bucketId, setBucketId] = useState(""),
    [units, setUnits] = useState(""),
    [credit, setCredit] = useState<number | null>(null),
    [reason, setReason] = useState("");
  const [entryEpoch, setEntryEpoch] = useState(0),
    [lines, setLines] = useState<Line[]>([]);
  const visible = canStart && (command === null || notFound) && verified === null;
  const bills = usePagedQuery(
    pharmacySupplierReturnKey(scope, "preparation-bills"),
    (cursor, signal) => listPharmacySupplierReturnOriginalBills(scope, cursor, signal),
    { enabled: visible, gcTime: 0, retry: false },
  );
  const invalidBills = bills.items.some((row) => row.outlet_id !== scope.outletId);
  const bill = bills.items.find((row) => row.id === billId);
  const sources = useQuery({
    queryKey: pharmacySupplierReturnKey(scope, "preparation", billId),
    queryFn: ({ signal }) => readPharmacySupplierReturnPreparation(scope, billId, signal),
    enabled: visible && !!bill && !invalidBills && !bills.isError,
    gcTime: 0,
    retry: false,
  });
  const invalidSources =
    !!sources.data &&
    !!bill &&
    (sources.data.document_reference !== bill.document_reference ||
      sources.data.payee_name !== bill.payee_name ||
      sources.data.currency !== bill.currency);
  const source = sources.data?.lines.find((row) => row.receipt_id === receiptId);
  const snapshot = sources.data?.stock_as_of ?? "";
  const positions = useApiPagedQuery<PharmacyStockPosition>(
    pharmacySupplierReturnKey(scope, "preparation-quarantine", receiptId, snapshot),
    `${pharmacyRoot(scope)}/stock/positions?batch_id=${source?.batch_id ?? ""}&as_of=${encodeURIComponent(snapshot)}&limit=50`,
    {
      enabled:
        visible &&
        !!source &&
        source.origin === "external" &&
        !sources.isError &&
        !invalidSources &&
        !bills.isError &&
        !invalidBills,
      request: { headers: pharmacyActorHeaders(scope), cache: "no-store" },
      gcTime: 0,
    },
  );
  const invalidPositions =
    !!source &&
    positions.items.some(
      (row) =>
        row.batch_id !== source.batch_id ||
        row.product_id !== source.product_id ||
        !z.string().guid().safeParse(row.location_id).success ||
        !Number.isSafeInteger(row.on_hand_units) ||
        row.on_hand_units < 0 ||
        !(row.stock_status === "quarantine" || row.stock_status === "sellable"),
    );
  const stockRefreshing = positions.isRefreshing || positions.isFetchingMore;
  const choices = positions.items.filter(
    (row) => row.stock_status === "quarantine" && row.on_hand_units > 0,
  );
  function resetEntry() {
    setBucketId("");
    setUnits("");
    setCredit(null);
    setEntryEpoch((epoch) => epoch + 1);
    setFailure("");
  }
  function clear() {
    resetEntry();
    setReceiptId("");
    setBillId("");
    setReason("");
    setLines([]);
    setVerified(null);
  }
  function confirmed(receipt: { id: string; number: number }) {
    try {
      sessionStorage.removeItem(storageKey);
    } catch {
      /* A retained reference recovers the same verified receipt. */
    }
    setCommand(null);
    setVerified(receipt.number);
    setFailure("");
    void client.invalidateQueries({ queryKey: pharmacySupplierReturnKey(scope) });
    onPrepared?.(receipt.id);
  }
  const prepare = useApiMutation(
    (input: { id: string; body: PharmacySupplierReturnInput }) =>
      preparePharmacySupplierReturn(scope, input.id, input.body),
    {
      onSuccess: confirmed,
      suppressErrorToast: true,
      onError: () => {
        setFailure(
          "The preparation result is unconfirmed. Check the original result before submitting again.",
        );
        void client.invalidateQueries({ queryKey: pharmacySupplierReturnKey(scope) });
      },
    },
  );
  const recover = useApiMutation(() => recoverPharmacySupplierReturnPreparation(scope, command!), {
    onSuccess: confirmed,
    suppressErrorToast: true,
    onError: (error) => {
      setNotFound(error instanceof ApiError && error.status === 404);
      setFailure(
        error instanceof ApiError && error.status === 404
          ? "No committed preparation was found for this reference. Check again if it may still be processing."
          : "The original preparation result is unavailable. Keep this reference and check again.",
      );
    },
  });
  const pending = prepare.isPending || recover.isPending;
  const entryDirty = !!bucketId || !!units || credit !== null;
  useLeaveGuard(
    verified === null && (lines.length > 0 || !!reason || entryDirty || command !== null),
    "supplier return preparation",
    pending,
  );
  if (verified !== null)
    return (
      <div className="space-y-3">
        <p role="status">
          Return {verified} prepared. This command does not reserve stock or record actual supplier
          credit.
        </p>
        {canStart && (
          <Button variant="outline" onClick={clear}>
            Prepare another return
          </Button>
        )}
      </div>
    );
  if (!canStart && command === null) return null;
  function add() {
    setFailure("");
    if (
      pending ||
      !visible ||
      !source ||
      source.origin !== "external" ||
      sources.isError ||
      sources.isFetching ||
      stockRefreshing ||
      invalidSources ||
      bills.isError ||
      invalidBills ||
      positions.isError ||
      positions.isPending ||
      invalidPositions
    )
      return;
    const bucket = choices.find((row) => row.location_id === bucketId),
      quantity = Number(units);
    if (
      !bucket ||
      !/^[1-9]\d*$/.test(units) ||
      !Number.isSafeInteger(quantity) ||
      quantity > 1000000000
    ) {
      setFailure("Choose a quarantine location and whole positive return units.");
      return;
    }
    const sameReceipt = lines
      .filter((row) => row.receipt_id === receiptId)
      .reduce((sum, row) => sum + row.quantity_units, 0);
    const remaining = source.receipt_units - source.dispatched_units;
    if (sameReceipt + quantity > remaining) {
      setFailure(
        `The original receipt has only ${remaining} unreturned units. Recheck the source before continuing.`,
      );
      return;
    }
    const sameBucket = lines
      .filter(
        (row) =>
          sources.data?.lines.find((item) => item.receipt_id === row.receipt_id)?.batch_id ===
            source.batch_id && row.quarantine_location_id === bucketId,
      )
      .reduce((sum, row) => sum + row.quantity_units, 0);
    if (sameBucket + quantity > bucket.on_hand_units) {
      setFailure(
        "Return units exceed recorded stock in this quarantine location. Recheck stock before continuing.",
      );
      return;
    }
    if (credit === null || !Number.isSafeInteger(credit) || credit < 0 || credit > 1000000000000) {
      setFailure("Enter expected credit explicitly, including zero when no credit is expected.");
      return;
    }
    if (
      lines.some((row) => row.receipt_id === receiptId && row.quarantine_location_id === bucketId)
    ) {
      setFailure(
        "This receipt and quarantine location are already included. Remove that line before changing its units or credit.",
      );
      return;
    }
    if (
      lines.length >= 100 ||
      lines.reduce((sum, row) => sum + row.expected_credit_minor, 0) + credit > 1000000000000
    ) {
      setFailure("A return supports at most 100 lines and the supported total credit limit.");
      return;
    }
    setLines([
      ...lines,
      {
        receipt_id: receiptId,
        quarantine_location_id: bucketId,
        quantity_units: quantity,
        expected_credit_minor: credit,
        bucketLabel: `${bucket.location_code} · ${bucket.location_name}`,
      },
    ]);
    resetEntry();
  }
  return (
    <Panel>
      <form
        className="space-y-4"
        noValidate
        onSubmit={(event) => {
          event.preventDefault();
          setFailure("");
          if (pending || !visible || sources.isFetching || stockRefreshing) return;
          if (
            bills.isError ||
            invalidBills ||
            sources.isError ||
            invalidSources ||
            !sources.data ||
            positions.isError ||
            invalidPositions ||
            lines.some(
              (line) =>
                !sources.data.lines.some(
                  (source) => source.receipt_id === line.receipt_id && source.origin === "external",
                ),
            )
          ) {
            setFailure(
              "Recheck the original bill and quarantine sources before preparing this return.",
            );
            return;
          }
          if (entryDirty) {
            setFailure("Add or clear the current line entry before preparing this return.");
            return;
          }
          const requested = new Map<string, number>();
          for (const line of lines)
            requested.set(
              line.receipt_id,
              (requested.get(line.receipt_id) ?? 0) + line.quantity_units,
            );
          for (const [id, quantity] of requested) {
            const source = sources.data.lines.find((row) => row.receipt_id === id)!;
            if (quantity > source.receipt_units - source.dispatched_units) {
              setFailure(
                "Original receipt quantities changed. Remove or revise the affected return lines before continuing.",
              );
              return;
            }
          }
          const parsed = pharmacySupplierReturnDraftInputSchema.safeParse({
            payable_id: billId,
            reason,
            lines: lines.map((line) => ({
              receipt_id: line.receipt_id,
              quarantine_location_id: line.quarantine_location_id,
              quantity_units: line.quantity_units,
              expected_credit_minor: line.expected_credit_minor,
            })),
          });
          if (!parsed.success) {
            const reasonTooLong = parsed.error.issues.some(
              (issue) => issue.code === "too_big" && issue.path[0] === "reason",
            );
            setFailure(
              reasonTooLong
                ? "Shorten the return reason to at most 500 characters."
                : "Add at least one return line and provide a return reason.",
            );
            return;
          }
          const id = command ?? newPharmacyId();
          try {
            sessionStorage.setItem(storageKey, id);
            if (sessionStorage.getItem(storageKey) !== id) throw new Error("storage");
          } catch {
            setFailure(
              "Recovery storage is unavailable. Restore browser storage before preparing a return.",
            );
            return;
          }
          setCommand(id);
          setNotFound(false);
          prepare.mutate({ id, body: parsed.data });
        }}
      >
        <h4 className="font-semibold">Prepare supplier return</h4>
        <p className="text-sm text-muted-foreground">
          Choose an original bill, including a paid bill. Only recorded quarantine stock can be
          included. Preparation reserves no stock and records no actual supplier credit.
        </p>
        {failure && <p role="alert">{failure}</p>}
        {command && command !== "unavailable" && (
          <Button
            type="button"
            variant="outline"
            disabled={pending}
            onClick={() => {
              setFailure("");
              recover.mutate();
            }}
          >
            Check preparation result
          </Button>
        )}
        {command === "unavailable" && (
          <p role="alert">
            Recovery storage is unavailable. Restore browser storage and reload before continuing.
          </p>
        )}
        {visible &&
          (bills.isError || invalidBills ? (
            <RequestFailure
              message="Original supplier bills are unavailable or do not belong to this outlet. Previously loaded choices are hidden."
              error={bills.error}
              retry={bills.retry}
            />
          ) : bills.isPending ? (
            <p role="status">Loading original supplier bills…</p>
          ) : (
            <>
              <Field id="supplier-return-original-bill" label="Original supplier bill">
                <SelectControl
                  id="supplier-return-original-bill"
                  value={billId}
                  onValueChange={(value) => {
                    setBillId(value);
                    setReceiptId("");
                    resetEntry();
                    setLines([]);
                    setReason("");
                  }}
                  disabled={pending || entryDirty || lines.length > 0 || !!reason}
                  options={bills.items.map((row) => ({
                    value: row.id,
                    label: `${row.document_reference} · ${row.payee_name} · ${row.issued_on} · ${row.currency}`,
                  }))}
                />
              </Field>
              <LoadMore
                label="Load more original bills"
                hasMore={bills.hasMore}
                isLoading={bills.isFetchingMore}
                onClick={bills.loadMore}
              />
              {billId &&
                (sources.isError || invalidSources ? (
                  <RequestFailure
                    message="Original receipt evidence is unavailable or inconsistent. Previously loaded details are hidden."
                    error={sources.error}
                    retry={sources.refetch}
                  />
                ) : sources.isPending ? (
                  <p role="status">Checking original receipt evidence…</p>
                ) : (
                  sources.data && (
                    <>
                      {sources.data.lines.some((row) => row.origin === "platform") && (
                        <p role="note">
                          Platform-supplier receipts require reverse acknowledgement before supplier
                          return preparation. They cannot be included here.
                        </p>
                      )}
                      {!sources.data.lines.length && (
                        <p>No inventory receipts were recorded against this bill.</p>
                      )}
                      <Field id="supplier-return-original-receipt" label="Original receipt">
                        <SelectControl
                          id="supplier-return-original-receipt"
                          value={receiptId}
                          disabled={pending || entryDirty}
                          onValueChange={(value) => {
                            setReceiptId(value);
                            resetEntry();
                          }}
                          options={sources.data.lines.map((row) => ({
                            value: row.receipt_id,
                            label: `${row.product_name} · batch ${row.lot_number} · expires ${row.expires_on} · received ${row.receipt_units} ${row.unit_code}`,
                            disabled:
                              row.origin !== "external" ||
                              row.dispatched_units >= row.receipt_units,
                          }))}
                        />
                      </Field>
                      {source?.origin === "external" &&
                        (positions.isError || invalidPositions ? (
                          <RequestFailure
                            message="Quarantine stock could not be verified. Previously loaded locations are hidden."
                            error={positions.error}
                            retry={positions.retry}
                          />
                        ) : positions.isPending ? (
                          <p role="status">Checking quarantine stock…</p>
                        ) : (
                          <>
                            <RefreshButton
                              error={null}
                              busy={pending || sources.isFetching || stockRefreshing}
                              refresh={() => void sources.refetch()}
                            >
                              Recheck receipt and quarantine stock
                            </RefreshButton>
                            <p className="text-sm">
                              Original receipt: {source.receipt_units} {source.unit_code};
                              dispatched: {source.dispatched_units}; unreturned:{" "}
                              {source.receipt_units - source.dispatched_units}. Quantities will be
                              checked again before recording.
                            </p>
                            <Field id="supplier-return-quarantine" label="Quarantine location">
                              <SelectControl
                                id="supplier-return-quarantine"
                                value={bucketId}
                                onValueChange={setBucketId}
                                disabled={pending}
                                options={choices.map((row) => ({
                                  value: row.location_id,
                                  label: `${row.location_code} · ${row.location_name} · ${row.on_hand_units} recorded ${source.unit_code}`,
                                }))}
                              />
                            </Field>
                            {!choices.length && (
                              <p>
                                No positive quarantine stock is recorded for this batch. Complete a
                                physical quarantine hold first.
                              </p>
                            )}
                            <LoadMore
                              label="Load more quarantine locations"
                              hasMore={positions.hasMore}
                              isLoading={positions.isFetchingMore}
                              onClick={positions.loadMore}
                            />
                            <Field id="supplier-return-units" label="Return units">
                              <Input
                                id="supplier-return-units"
                                inputMode="numeric"
                                value={units}
                                onChange={(event) => setUnits(event.target.value)}
                                disabled={pending}
                              />
                            </Field>
                            <CurrencyAmountField
                              key={entryEpoch}
                              id="supplier-return-expected-credit"
                              label="Expected credit"
                              currency={sources.data.currency}
                              initialMinor={
                                credit !== null && Number.isSafeInteger(credit) ? credit : null
                              }
                              onChange={setCredit}
                              disabled={pending}
                            />
                            <div className="flex flex-wrap gap-3">
                              <Button
                                type="button"
                                variant="outline"
                                disabled={pending || sources.isFetching || stockRefreshing}
                                onClick={add}
                              >
                                Add return line
                              </Button>
                              <Button
                                type="button"
                                variant="ghost"
                                disabled={pending}
                                onClick={resetEntry}
                              >
                                Clear line entry
                              </Button>
                            </div>
                          </>
                        ))}
                      {!(positions.isError || invalidPositions) && lines.length > 0 && (
                        <div className="overflow-x-auto">
                          <table
                            aria-label="Prepared return lines"
                            className="w-full text-left text-sm"
                          >
                            <thead>
                              <tr>
                                {[
                                  "Original product / batch",
                                  "Quarantine",
                                  "Units",
                                  "Expected credit",
                                  "Action",
                                ].map((label) => (
                                  <th key={label} className="p-2">
                                    {label}
                                  </th>
                                ))}
                              </tr>
                            </thead>
                            <tbody>
                              {lines.map((line, index) => {
                                const source = sources.data.lines.find(
                                  (item) => item.receipt_id === line.receipt_id,
                                );
                                return (
                                  <tr key={`${line.receipt_id}:${line.quarantine_location_id}`}>
                                    <td className="p-2">
                                      {source?.product_name} · {source?.lot_number}
                                    </td>
                                    <td className="p-2">{line.bucketLabel}</td>
                                    <td className="p-2">
                                      {line.quantity_units} {source?.unit_code}
                                    </td>
                                    <td className="p-2">
                                      {formatPharmacyMoney(
                                        line.expected_credit_minor,
                                        sources.data.currency,
                                      )}
                                    </td>
                                    <td className="p-2">
                                      <Button
                                        type="button"
                                        variant="ghost"
                                        disabled={pending}
                                        aria-label={`Remove return line ${index + 1}`}
                                        onClick={() =>
                                          setLines(lines.filter((_, i) => i !== index))
                                        }
                                      >
                                        Remove
                                      </Button>
                                    </td>
                                  </tr>
                                );
                              })}
                            </tbody>
                          </table>
                        </div>
                      )}
                      <Field id="supplier-return-reason" label="Return reason">
                        <Input
                          id="supplier-return-reason"
                          value={reason}
                          onChange={(event) => setReason(event.target.value)}
                          disabled={pending}
                        />
                      </Field>
                      <Button
                        type="submit"
                        disabled={pending || !lines.length || sources.isFetching || stockRefreshing}
                      >
                        {notFound ? "Retry original preparation" : "Prepare supplier return"}
                      </Button>
                    </>
                  )
                ))}
              <Button type="button" variant="ghost" disabled={pending} onClick={clear}>
                Clear unsubmitted preparation
              </Button>
            </>
          ))}
      </form>
    </Panel>
  );
}
