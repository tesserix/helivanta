"use client";

import { useRef, useState, type ReactNode } from "react";
import { Button, Input, Textarea } from "@tesserix/web";
import {
  ApiError,
  PharmacyReceiptInputContract,
  isPharmacyReceivingTarget,
  isReceivingAccessChanged,
  newPharmacyReceivingId,
  pharmacyBatchInputSchema,
  pharmacyReceivingKey,
  recordPharmacyReceipt,
  recordPharmacyReceivingBatch,
  recoverPharmacyReceipt,
  recoverPharmacyReceivingBatch,
  useApiMutation,
  useApiQuery,
  usePharmacyReceipts,
  usePharmacyReceivingBatches,
  usePharmacyReceivingLocations,
  usePharmacyReceivingProducts,
  type PharmacyReceiptInput,
  type PharmacyReceivingBatch,
  type PharmacyReceivingBatchInput,
  type PharmacyReceivingProduct,
  type PharmacyReceivingScope,
} from "@helivanta/api";
import {
  Field,
  FormSelectField,
  LoadMore,
  RequestFailure,
  SelectControl,
  firstError,
  formatDateTime,
  sessionEnded,
  useZodForm,
} from "@helivanta/ui";
import { CurrencyAmountField, formatPharmacyMoney } from "./currency-amount-field";

const notAvailable =
  "Receiving at this outlet is not available to you. If your duty changed, ask the outlet's manager to confirm it, then reopen this link.";

/** Reached only from a known outlet link: IAM has no caller-scoped duty directory yet. */
export function ReceiverDuty({ pharmacyId, outletId }: { pharmacyId: string; outletId: string }) {
  const identity = useApiQuery<{ tenant_id: string; subject: string }>(
    ["iam", "me", "permissions"],
    "/iam/me/permissions",
  );
  if (!isPharmacyReceivingTarget(pharmacyId, outletId))
    return (
      <section aria-label="Pharmacy receiving">
        <p role="alert">{notAvailable}</p>
      </section>
    );
  if (identity.isError)
    return (
      <RequestFailure
        message="Could not verify your active tenant."
        retry={identity.refetch}
        error={identity.error}
      />
    );
  if (!identity.data) return <p role="status">Loading receiving duty…</p>;
  const scope = {
    tenantId: identity.data.tenant_id,
    subject: identity.data.subject,
    pharmacyId,
    outletId,
  };
  return <ReceiverPanel key={pharmacyReceivingKey(scope).join(":")} scope={scope} />;
}

function refusalText(error: unknown) {
  if (!isReceivingAccessChanged(error)) return null;
  if ((error as ApiError).status === 401) return sessionEnded;
  if ((error as ApiError).code === "session_changed")
    return "Your signed-in account changed. Reload this page before recording stock.";
  return notAvailable;
}

type Attempt<I> = { id: string; input: I };

// One retained ID per command: a lost answer is retried or recovered under the same ID, never re-sent as new.
function useRetained<I, R>(
  record: (id: string, input: I) => Promise<R>,
  recover: (id: string, input: I) => Promise<R>,
  done: (row: R) => void,
  refuse: (error: unknown) => void,
  hold: (held: boolean) => void,
  invalidate: unknown[][],
) {
  const [attempt, setAttempt] = useState<Attempt<I> | null>(null);
  const [rejected, setRejected] = useState<"conflict" | "invalid" | null>(null);
  const retained = useRef<Attempt<I> | null>(null);
  const working = useRef(false);
  const settle = {
    onSuccess: (row: R) => {
      working.current = false;
      retained.current = null;
      setAttempt(null);
      hold(false);
      done(row);
    },
    onError: (error: Error) => {
      working.current = false;
      if (isReceivingAccessChanged(error)) refuse(error);
      // 400 and 409 are definitive refusals with nothing written, so the ID is released for corrected details.
      else if (error instanceof ApiError && (error.status === 400 || error.status === 409)) {
        retained.current = null;
        setAttempt(null);
        hold(false);
        setRejected(error.status === 409 ? "conflict" : "invalid");
      }
    },
    invalidate,
    suppressErrorToast: true,
  };
  const save = useApiMutation((value: Attempt<I>) => record(value.id, value.input), settle);
  const recovery = useApiMutation((value: Attempt<I>) => recover(value.id, value.input), settle);
  const busy = save.isPending || recovery.isPending;
  const failed = !busy && attempt !== null && (save.isError || recovery.isError);
  return {
    attempt,
    rejected,
    busy,
    failed,
    frozen: attempt !== null || busy,
    submit(input: I) {
      if (working.current) return;
      const value = retained.current ?? { id: newPharmacyReceivingId(), input };
      retained.current = value;
      working.current = true;
      setAttempt(value);
      hold(true);
      setRejected(null);
      save.mutate(value);
    },
    recover() {
      if (working.current || !retained.current) return;
      working.current = true;
      recovery.mutate(retained.current);
    },
  };
}

type Commands = {
  scope: PharmacyReceivingScope;
  product: PharmacyReceivingProduct;
  refuse: (error: unknown) => void;
  hold: (held: boolean) => void;
};

function ReceiverPanel({ scope }: { scope: PharmacyReceivingScope }) {
  const [productId, setProductId] = useState("");
  const receipts = usePharmacyReceipts(scope);
  const products = usePharmacyReceivingProducts(scope);
  const locations = usePharmacyReceivingLocations(scope);
  const lots = usePharmacyReceivingBatches(scope, productId);
  const [refusal, setRefusal] = useState<unknown>(null);
  const [held, setHeld] = useState<Record<"lot" | "receipt", boolean>>({
    lot: false,
    receipt: false,
  });
  const [message, setMessage] = useState("");
  const failure =
    refusal ?? firstError(receipts.error, products.error, locations.error, lots.error);
  const refused = refusalText(failure);
  if (refused)
    return (
      <section aria-label="Pharmacy receiving">
        <p role="alert">{refused}</p>
      </section>
    );
  if (failure)
    return (
      <section aria-label="Pharmacy receiving">
        <RequestFailure
          message="Receiving records are unavailable. Current access could not be confirmed."
          error={failure}
          retry={() => {
            for (const query of [receipts, products, locations, lots])
              if (query.isError) void query.refetch();
          }}
        />
      </section>
    );
  if (!receipts.data || !products.data || !locations.data)
    return (
      <section aria-label="Pharmacy receiving">
        <p role="status">Loading receiving records…</p>
      </section>
    );
  const catalogue = products.data.pages.flatMap((page) => page.data);
  const allLocations = locations.data.pages.flatMap((page) => page.data);
  const quarantine = allLocations.filter((row) => row.stock_status === "quarantine");
  const history = receipts.data.pages.flatMap((page) => page.data);
  const product = catalogue.find((row) => row.id === productId) ?? null;
  const hold = (noun: "lot" | "receipt") => (value: boolean) =>
    setHeld((current) => ({ ...current, [noun]: value }));
  return (
    <section aria-label="Pharmacy receiving" className="space-y-6">
      <div>
        <h2 className="text-lg font-semibold">Receive stock</h2>
        <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
          Everything recorded here stays in quarantine. An outlet editor checks and releases it
          before it can be sold.
        </p>
      </div>
      {message && <p role="status">{message}</p>}
      <section aria-label="Recorded receipts" className="space-y-3">
        <h3 className="font-medium">Recorded receipts</h3>
        {history.length === 0 ? (
          <p className="text-sm text-muted-foreground">No receipts recorded at this outlet yet.</p>
        ) : (
          <table className="w-full text-left text-sm">
            <thead>
              <tr>
                <th>Source document line</th>
                <th>Location</th>
                <th>Units</th>
                <th>Recorded</th>
              </tr>
            </thead>
            <tbody>
              {history.map((row) => (
                <tr key={row.id}>
                  <td className="break-words">{row.source_reference}</td>
                  <td>
                    {allLocations.find((location) => location.id === row.location_id)?.code ??
                      "Location not listed"}
                  </td>
                  <td>{row.quantity_units}</td>
                  <td>{formatDateTime(row.created_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <LoadMore
          hasMore={receipts.hasNextPage}
          isLoading={receipts.isFetchingNextPage}
          onClick={() => void receipts.fetchNextPage({ cancelRefetch: false })}
        />
      </section>
      {catalogue.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          No active catalogue products at this outlet. An outlet editor must add the product before
          it can be received.
        </p>
      ) : quarantine.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          This outlet has no quarantine location. An outlet editor must add one before stock can be
          received.
        </p>
      ) : (
        <div className="space-y-4 border-t pt-4">
          <Field id="receiving-product" label="Product">
            <SelectControl
              id="receiving-product"
              value={productId}
              placeholder="Choose the product on the label"
              disabled={held.lot || held.receipt}
              options={catalogue.map((row) => ({
                value: row.id,
                label: `${row.brand_name} · ${row.generic_name} · ${row.dosage_form} (${row.sku})`,
              }))}
              onValueChange={(value) => {
                setProductId(value);
                setMessage("");
              }}
            />
          </Field>
          <LoadMore
            hasMore={products.hasNextPage}
            isLoading={products.isFetchingNextPage}
            onClick={() => void products.fetchNextPage({ cancelRefetch: false })}
          />
          {product &&
            (!lots.data ? (
              <p role="status">Loading lots…</p>
            ) : (
              <div key={product.id} className="space-y-6">
                <p className="text-sm text-muted-foreground">
                  {product.pack_size} {product.unit_of_issue} per pack ·{" "}
                  {product.ingredients
                    .map((row) => `${row.name} ${row.strength} ${row.unit}`)
                    .join(", ")}
                </p>
                <LotForm
                  scope={scope}
                  product={product}
                  refuse={setRefusal}
                  hold={hold("lot")}
                  onLot={(lot) =>
                    setMessage(
                      `Lot ${lot.lot_number} recorded. Choose it below to record the count.`,
                    )
                  }
                />
                {lots.data.pages[0]?.data.length === 0 ? (
                  <p className="text-sm text-muted-foreground">
                    No lots of this product are recorded at this outlet. Record the lot on the label
                    first.
                  </p>
                ) : (
                  <ReceiptForm
                    scope={scope}
                    product={product}
                    refuse={setRefusal}
                    hold={hold("receipt")}
                    lots={lots.data.pages.flatMap((page) => page.data)}
                    locations={quarantine}
                    onReceipt={() => setMessage("Receipt recorded in quarantine.")}
                    more={
                      <LoadMore
                        hasMore={lots.hasNextPage}
                        isLoading={lots.isFetchingNextPage}
                        onClick={() => void lots.fetchNextPage({ cancelRefetch: false })}
                      />
                    }
                  />
                )}
              </div>
            ))}
        </div>
      )}
    </section>
  );
}

function LotForm({
  scope,
  product,
  refuse,
  hold,
  onLot,
}: Commands & { onLot: (lot: PharmacyReceivingBatch) => void }) {
  const defaults: PharmacyReceivingBatchInput = {
    product_id: product.id,
    lot_number: "",
    expires_on: "",
    mrp_minor: 0,
  };
  const form = useZodForm(pharmacyBatchInputSchema, defaults);
  const [amountKey, setAmountKey] = useState(0);
  const command = useRetained(
    (id: string, input: PharmacyReceivingBatchInput) =>
      recordPharmacyReceivingBatch(scope, id, input),
    (id: string, input: PharmacyReceivingBatchInput) =>
      recoverPharmacyReceivingBatch(scope, id, input),
    (lot: PharmacyReceivingBatch) => {
      form.reset(defaults);
      setAmountKey((value) => value + 1);
      onLot(lot);
    },
    refuse,
    hold,
    [pharmacyReceivingKey(scope, "batches", product.id)],
  );
  return (
    <form
      noValidate
      aria-label="Record lot"
      className="space-y-3"
      onSubmit={(event) => void form.handleSubmit(command.submit)(event)}
    >
      <h3 className="font-medium">Record the lot on the label</h3>
      <Field
        id="lot-lot_number"
        label="Lot number"
        error={form.formState.errors.lot_number?.message}
      >
        <Input id="lot-lot_number" disabled={command.frozen} {...form.register("lot_number")} />
      </Field>
      <Field
        id="lot-expires_on"
        label="Expiry date"
        error={form.formState.errors.expires_on?.message}
      >
        <Input
          id="lot-expires_on"
          type="date"
          disabled={command.frozen}
          {...form.register("expires_on")}
        />
      </Field>
      <CurrencyAmountField
        key={amountKey}
        id="lot-mrp_minor"
        label="MRP printed on the pack"
        currency={product.currency}
        initialMinor={null}
        disabled={command.frozen}
        error={form.formState.errors.mrp_minor?.message}
        onChange={(minor) => form.setValue("mrp_minor", minor, { shouldValidate: true })}
      />
      {command.rejected === "conflict" && (
        <p role="alert">
          This lot conflicts with one already recorded at this outlet. Choose the existing lot
          below, or check the label.
        </p>
      )}
      <Outcome command={command} noun="lot" />
    </form>
  );
}

function ReceiptForm({
  scope,
  product,
  refuse,
  hold,
  lots,
  locations,
  onReceipt,
  more,
}: Commands & {
  lots: PharmacyReceivingBatch[];
  locations: { id: string; code: string; name: string }[];
  onReceipt: () => void;
  more: ReactNode;
}) {
  const defaults: PharmacyReceiptInput = {
    location_id: locations.length === 1 ? locations[0]!.id : "",
    batch_id: "",
    kind: "receipt",
    quantity_units: 0,
    source_reference: "",
    reason: "",
  };
  const form = useZodForm(PharmacyReceiptInputContract, defaults);
  const command = useRetained(
    (id: string, input: PharmacyReceiptInput) => recordPharmacyReceipt(scope, id, input),
    (id: string, input: PharmacyReceiptInput) => recoverPharmacyReceipt(scope, id, input),
    () => {
      form.reset(defaults);
      onReceipt();
    },
    refuse,
    hold,
    [pharmacyReceivingKey(scope, "receipts")],
  );
  return (
    <form
      noValidate
      aria-label="Record receipt"
      className="space-y-3 border-t pt-4"
      onSubmit={(event) => void form.handleSubmit(command.submit)(event)}
    >
      <h3 className="font-medium">Record the count received</h3>
      <FormSelectField
        control={form.control}
        name="batch_id"
        id="receipt-batch_id"
        label="Lot"
        disabled={command.frozen}
        options={lots.map((lot) => ({
          value: lot.id,
          label: `${lot.lot_number} · expires ${lot.expires_on} · MRP ${formatPharmacyMoney(lot.mrp_minor, product.currency)}`,
        }))}
      />
      {more}
      <FormSelectField
        control={form.control}
        name="location_id"
        id="receipt-location_id"
        label="Quarantine location"
        disabled={command.frozen}
        options={locations.map((row) => ({ value: row.id, label: `${row.code} · ${row.name}` }))}
      />
      <Field
        id="receipt-quantity_units"
        label={`Units counted (${product.unit_of_issue})`}
        error={form.formState.errors.quantity_units?.message}
      >
        <Input
          id="receipt-quantity_units"
          type="number"
          min={1}
          step={1}
          disabled={command.frozen}
          {...form.register("quantity_units", { valueAsNumber: true })}
        />
      </Field>
      <Field
        id="receipt-source_reference"
        label="Source document line"
        error={form.formState.errors.source_reference?.message}
      >
        <Input
          id="receipt-source_reference"
          disabled={command.frozen}
          {...form.register("source_reference")}
        />
      </Field>
      <Field id="receipt-reason" label="Reason" error={form.formState.errors.reason?.message}>
        <Textarea id="receipt-reason" disabled={command.frozen} {...form.register("reason")} />
      </Field>
      {command.rejected === "conflict" && (
        <p role="alert">
          This receipt was refused. Receipts go to a quarantine location and a recorded receipt
          cannot change. Check the details and record it again.
        </p>
      )}
      <Outcome command={command} noun="receipt" />
    </form>
  );
}

function Outcome({
  command,
  noun,
}: {
  command: {
    attempt: unknown;
    rejected: "conflict" | "invalid" | null;
    busy: boolean;
    failed: boolean;
    recover: () => void;
  };
  noun: "lot" | "receipt";
}) {
  return (
    <>
      {command.rejected === "invalid" && (
        <p role="alert">
          These {noun} details were refused as invalid and nothing was recorded. Check them against
          the label and record again.
        </p>
      )}
      {command.failed && (
        <p role="alert">
          The {noun} outcome could not be confirmed. Keep these details unchanged, then retry the
          same {noun} or recover it.
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={command.busy}>
          {command.attempt ? `Retry same ${noun}` : `Record ${noun}`}
        </Button>
        {command.failed && (
          <Button type="button" variant="outline" disabled={command.busy} onClick={command.recover}>
            Recover {noun}
          </Button>
        )}
      </div>
    </>
  );
}
