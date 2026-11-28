"use client";

import { useEffect, useRef, useState, type ChangeEvent } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  pharmacyRoot,
  pharmacyKey,
  useApiMutation,
  useApiPagedQuery,
  type PharmacyProduct,
  type PharmacyScope,
  pharmacyBarcodeLinkInputSchema,
} from "@helivanta/api";
import { Field, LoadMore, RequestFailure, SelectControl, useZodForm } from "@helivanta/ui";

type Props = {
  scope: PharmacyScope;
  canLink: boolean;
  disabled?: boolean;
  onProduct: (product: PharmacyProduct, signal: AbortSignal) => Promise<string>;
};

export function BarcodeInput(props: Props) {
  return (
    <Scanner
      key={`${props.scope.tenantId}:${props.scope.pharmacyId}:${props.scope.outletId}`}
      {...props}
    />
  );
}

function Scanner({ scope, canLink, disabled = false, onProduct }: Props) {
  const form = useZodForm(
    z.object({
      barcode: z
        .string()
        .trim()
        .min(1, "Scan or enter a barcode")
        .max(128, "Enter a barcode with at most 128 characters"),
    }),
    { barcode: "" },
  );
  const code = form.watch("barcode");
  const [unknown, setUnknown] = useState<string | null>(null);
  const [linking, setLinking] = useState(false);
  const [notice, setNotice] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const request = useRef<AbortController | null>(null);
  const busy = useRef(false);
  useEffect(
    () => () => {
      request.current?.abort();
    },
    [],
  );
  useEffect(() => {
    if (disabled) request.current?.abort();
  }, [disabled]);
  const lookup = useApiMutation(
    async ({ barcode, productId }: { barcode: string; productId?: string }) => {
      const controller = new AbortController();
      request.current = controller;
      const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15000)]);
      try {
        const product = await apiFetch<PharmacyProduct>(
          `${pharmacyRoot(scope)}/catalogue/barcodes/${encodeURIComponent(barcode)}`,
          {
            signal,
            headers: { "X-Helivanta-Expected-Tenant": scope.tenantId },
            ...(productId
              ? {
                  method: "PUT",
                  body: JSON.stringify(
                    pharmacyBarcodeLinkInputSchema.parse({ product_id: productId }),
                  ),
                }
              : {}),
          },
        );
        signal.throwIfAborted();
        const message = await onProduct(product, signal);
        signal.throwIfAborted();
        form.reset({ barcode: "" });
        setUnknown(null);
        setLinking(false);
        setNotice(message);
        input.current?.focus();
      } catch (error) {
        if (controller.signal.aborted) return;
        if (signal.aborted)
          throw new Error("Barcode lookup timed out. Try Find product again.", { cause: error });
        if (error instanceof ApiError && error.code === "barcode_unknown") setUnknown(barcode);
        throw error;
      } finally {
        busy.current = false;
      }
    },
    { suppressErrorToast: true },
  );
  function find(barcode: string, productId?: string) {
    if (disabled || busy.current || barcode.trim() === "") return;
    busy.current = true;
    setNotice("");
    if (!productId) setUnknown(null);
    lookup.mutate({ barcode: barcode.trim(), productId });
  }
  return (
    <section className="my-4 space-y-3" aria-label="Barcode scanner">
      <form
        noValidate
        className="flex flex-wrap items-end gap-3"
        onSubmit={(event) => {
          void form.handleSubmit(({ barcode }) => find(barcode))(event);
        }}
      >
        <Field id="pharmacy-barcode" label="Barcode" error={form.formState.errors.barcode?.message}>
          <Input
            id="pharmacy-barcode"
            ref={input}
            value={code}
            autoComplete="off"
            inputMode="numeric"
            disabled={disabled}
            readOnly={lookup.isPending}
            aria-describedby="pharmacy-barcode-help"
            onChange={(event: ChangeEvent<HTMLInputElement>) => {
              form.setValue("barcode", event.target.value, {
                shouldDirty: true,
                shouldValidate: true,
              });
              setUnknown(null);
              setLinking(false);
              setNotice("");
              lookup.reset();
            }}
          />
        </Field>
        <Button
          type="submit"
          variant="outline"
          disabled={disabled || lookup.isPending || !code.trim()}
        >
          Find product
        </Button>
      </form>
      <p id="pharmacy-barcode-help" className="text-sm text-muted-foreground">
        Focus Barcode and scan with a USB or Bluetooth keyboard scanner, then Enter. Wait for each
        result before scanning again.
      </p>
      {lookup.isPending && <p role="status">Finding product…</p>}
      {notice && <p role="status">{notice}</p>}
      {lookup.isError && <p role="alert">{lookup.error.message}</p>}
      {unknown &&
        (canLink ? (
          <Button
            type="button"
            variant="outline"
            disabled={disabled || lookup.isPending}
            onClick={() => setLinking(true)}
          >
            Link barcode to a product
          </Button>
        ) : (
          <p>Ask a catalogue editor to link this barcode to a product at this outlet.</p>
        ))}
      {unknown && linking && canLink && (
        <BarcodeLink
          scope={scope}
          disabled={disabled || lookup.isPending}
          onSave={(productId) => find(unknown, productId)}
        />
      )}
    </section>
  );
}

function BarcodeLink({
  scope,
  disabled,
  onSave,
}: {
  scope: PharmacyScope;
  disabled: boolean;
  onSave: (id: string) => void;
}) {
  const [productId, setProductId] = useState("");
  const products = useApiPagedQuery<PharmacyProduct>(
    pharmacyKey(scope, "barcode-link", "products"),
    `${pharmacyRoot(scope)}/catalogue/products?status=active`,
    { request: { headers: { "X-Helivanta-Expected-Tenant": scope.tenantId } } },
  );
  if (products.isError)
    return (
      <RequestFailure
        message="Could not load products to link. Retry the product list."
        error={products.error}
        retry={products.retry}
      />
    );
  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">
        Compare the product and pack size with the scanned label before linking.
      </p>
      <Field id="barcode-link-product" label="Link to product">
        <SelectControl
          id="barcode-link-product"
          value={productId}
          onValueChange={setProductId}
          disabled={disabled || products.isPending}
          options={products.items
            .filter((p) => p.status === "active")
            .map((p) => ({
              value: p.id,
              label: `${p.sku} · ${p.brand_name} · ${p.manufacturer} · ${p.pack_size} ${p.unit_of_issue} per pack`,
            }))}
        />
      </Field>
      {!products.isPending && products.items.length === 0 && (
        <p>No active products. Add the product in the catalogue first.</p>
      )}
      <LoadMore
        label="Load more products to link"
        hasMore={products.hasMore}
        isLoading={products.isFetchingMore}
        onClick={products.loadMore}
      />
      <Button type="button" disabled={disabled || !productId} onClick={() => onSave(productId)}>
        Save barcode link
      </Button>
    </div>
  );
}
