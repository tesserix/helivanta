"use client";

import { useRef, useState } from "react";
import {
  getStockProductReference,
  newWholesaleReceiptId,
  recordWholesaleReceipt,
  recoverWholesaleReceipt,
  useApiMutation,
  usePermissions,
  useWholesaleReceipts,
  wholesaleStockKey,
  WholesaleReceiptInputContract,
  type WholesaleReceiptInput,
  type WholesaleStockScope,
} from "@helivanta/api";
import { Field, RequestFailure, useZodForm } from "@helivanta/ui";
import { Button, Input, Textarea } from "@tesserix/web";

type Props = {
  scope: WholesaleStockScope;
  location: { id: string; code: string; name: string; status: string };
  roles: string[];
  canWrite: boolean;
  onClose: () => void;
};
const readers = new Set(["administrator", "warehouse", "inventory", "auditor", "regulator"]);
const defaults: WholesaleReceiptInput = {
  product_version_id: "",
  lot_number: "",
  expires_on: "",
  unit_code: "pack",
  quantity_units: 0,
  source_reference: "",
  reason: "",
};
type Attempt = { id: string; input: WholesaleReceiptInput };

export function WarehouseStock(props: Props) {
  return <WarehouseStockPanel key={wholesaleStockKey(props.scope).join(":")} {...props} />;
}
function WarehouseStockPanel({ scope, location, roles, canWrite, onClose }: Props) {
  const { can } = usePermissions();
  const allowed = can("supply.operations.read") && roles.some((role) => readers.has(role));
  const canReceive =
    canWrite &&
    can("supply.operations.write") &&
    roles.some((role) => role === "administrator" || role === "inventory") &&
    location.status === "active";
  const query = useWholesaleReceipts(scope, allowed);
  const form = useZodForm(WholesaleReceiptInputContract, defaults);
  const [barcode, setBarcode] = useState("");
  const [product, setProduct] = useState<Awaited<
    ReturnType<typeof getStockProductReference>
  > | null>(null);
  const [attempt, setAttempt] = useState<Attempt | null>(null);
  const pending = useRef<Attempt | null>(null);
  const working = useRef(false);
  const [message, setMessage] = useState("");
  const lookup = useApiMutation((value: string) => getStockProductReference(scope, value), {
    suppressErrorToast: true,
    onSuccess: (value) => {
      setProduct(value);
      form.setValue("product_version_id", value.version_id);
    },
  });
  const complete = () => {
    pending.current = null;
    setAttempt(null);
    working.current = false;
    form.reset(defaults);
    setProduct(null);
    setBarcode("");
    setMessage("Receipt recorded in quarantine.");
  };
  const save = useApiMutation(
    (value: Attempt) => recordWholesaleReceipt(scope, value.id, value.input),
    {
      onSuccess: complete,
      onError: () => {
        working.current = false;
      },
      invalidate: [wholesaleStockKey(scope)],
      suppressErrorToast: true,
    },
  );
  const recovery = useApiMutation(
    (value: Attempt) => recoverWholesaleReceipt(scope, value.id, value.input),
    {
      onSuccess: complete,
      onError: () => {
        working.current = false;
      },
      invalidate: [wholesaleStockKey(scope)],
      suppressErrorToast: true,
    },
  );
  if (!allowed) return null;
  if (query.isError)
    return (
      <section aria-label="Warehouse stock">
        <RequestFailure
          message="Stock receipts are unavailable. Current access could not be confirmed."
          error={query.error}
          retry={() => void query.refetch()}
        />
        <Button variant="ghost" onClick={onClose}>
          Close stock
        </Button>
      </section>
    );
  if (query.isPending)
    return (
      <section aria-label="Warehouse stock">
        <p role="status">Loading stock receipts…</p>
      </section>
    );
  const receipts = query.data?.pages.flatMap((page) => page.data) ?? [];
  const frozen = Boolean(attempt) || save.isPending || recovery.isPending;
  return (
    <section className="space-y-4 rounded-md border p-4" aria-label="Warehouse stock">
      <div className="flex items-center justify-between">
        <h3 className="font-medium">Physical receipts · {location.code}</h3>
        <Button variant="ghost" onClick={onClose}>
          Close stock
        </Button>
      </div>
      {message && <p role="status">{message}</p>}
      {receipts.length === 0 ? (
        <p>No physical receipts recorded at this warehouse.</p>
      ) : (
        <table className="w-full text-left text-sm">
          <thead>
            <tr>
              <th>Source document line</th>
              <th>Lot</th>
              <th>Count</th>
              <th>Expiry</th>
              <th>Intake state</th>
            </tr>
          </thead>
          <tbody>
            {receipts.map((receipt) => (
              <tr key={receipt.id}>
                <td>{receipt.input.source_reference}</td>
                <td>{receipt.input.lot_number}</td>
                <td>
                  {receipt.input.quantity_units} {receipt.input.unit_code}
                </td>
                <td>{receipt.input.expires_on}</td>
                <td>Received in quarantine</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {query.hasNextPage && (
        <Button
          variant="outline"
          disabled={query.isFetching}
          onClick={() => void query.fetchNextPage({ cancelRefetch: false })}
        >
          Load more receipts
        </Button>
      )}
      {canReceive && (
        <form
          noValidate
          aria-label="Record physical receipt"
          className="space-y-3 border-t pt-4"
          onSubmit={(event) => {
            void form.handleSubmit((input) => {
              if (working.current) return;
              const value = pending.current ?? { id: newWholesaleReceiptId(), input };
              pending.current = value;
              setAttempt(value);
              working.current = true;
              setMessage("");
              save.mutate(value);
            })(event);
          }}
        >
          <p>
            Count complete packs on arrival. Items remain quarantined until identity reconciliation
            and quality release.
          </p>
          <input type="hidden" {...form.register("product_version_id")} />
          <input type="hidden" {...form.register("unit_code")} />
          <Field
            id="stock-barcode"
            label="Product barcode"
            error={form.formState.errors.product_version_id?.message}
          >
            <Input
              id="stock-barcode"
              disabled={frozen || lookup.isPending}
              value={barcode}
              onChange={(event) => {
                setBarcode(event.target.value);
                setProduct(null);
                form.setValue("product_version_id", "");
              }}
            />
          </Field>
          <Button
            type="button"
            variant="outline"
            disabled={frozen || lookup.isPending || !barcode}
            onClick={() => lookup.mutate(barcode)}
          >
            Find product
          </Button>
          {lookup.error && (
            <RequestFailure
              message="The product source could not be verified."
              refused="This product could not be verified for stock. Check the barcode and your access."
              error={lookup.error}
              retry={() => lookup.mutate(barcode)}
            />
          )}
          {product && (
            <div>
              <p>{product.brand_name}</p>
              <p>
                {product.formulation_label} · {product.manufacturer}
              </p>
              <p>
                {product.pack_size} {product.unit_of_issue} per pack
              </p>
            </div>
          )}
          <Field
            id="stock-lot_number"
            label="Lot number"
            error={form.formState.errors.lot_number?.message}
          >
            <Input id="stock-lot_number" disabled={frozen} {...form.register("lot_number")} />
          </Field>
          <Field
            id="stock-expires_on"
            label="Expiry date"
            error={form.formState.errors.expires_on?.message}
          >
            <Input
              id="stock-expires_on"
              type="date"
              disabled={frozen}
              {...form.register("expires_on")}
            />
          </Field>
          <Field
            id="stock-quantity_units"
            label="Packs counted"
            error={form.formState.errors.quantity_units?.message}
          >
            <Input
              id="stock-quantity_units"
              type="number"
              min={1}
              step={1}
              disabled={frozen}
              {...form.register("quantity_units", { valueAsNumber: true })}
            />
          </Field>
          <Field
            id="stock-source_reference"
            label="Source document line"
            error={form.formState.errors.source_reference?.message}
          >
            <Input
              id="stock-source_reference"
              disabled={frozen}
              {...form.register("source_reference")}
            />
          </Field>
          <Field id="stock-reason" label="Reason" error={form.formState.errors.reason?.message}>
            <Textarea id="stock-reason" disabled={frozen} {...form.register("reason")} />
          </Field>
          {(save.error || recovery.error) && (
            <p role="alert">
              The receipt outcome could not be confirmed. Recover the retained count before
              recording it again.
            </p>
          )}
          {attempt && save.isError && (
            <p>
              The receipt outcome needs recovery. Keep this count unchanged until it is resolved.
            </p>
          )}
          <Button
            type="submit"
            disabled={save.isPending || recovery.isPending || (!product && !attempt)}
          >
            {attempt ? "Retry same receipt" : "Record receipt"}
          </Button>
          {attempt && save.isError && (
            <Button
              type="button"
              variant="outline"
              disabled={recovery.isPending}
              onClick={() => {
                if (working.current) return;
                working.current = true;
                recovery.mutate(attempt);
              }}
            >
              Recover receipt
            </Button>
          )}
        </form>
      )}
    </section>
  );
}
