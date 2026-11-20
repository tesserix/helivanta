"use client";

import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  newPharmacyId,
  stocktakeKey,
  pharmacyRoot,
  pharmacyStocktakeStartInputSchema,
  startStocktake,
  recoverStocktakeStart,
  useApiPagedQuery,
  type PharmacyStocktakeScope,
  type PharmacyLocation,
  type PharmacyProduct,
  type PharmacyStocktakeStartInput,
  type PharmacyStocktake,
} from "@helivanta/api";
import {
  Field,
  FormSelectField,
  LoadMore,
  Panel,
  RequestFailure,
  useLeaveGuard,
  useZodForm,
} from "@helivanta/ui";

import {
  retainStocktakeCommand,
  forgetStocktakeCommand,
  StocktakeRecoveryError,
} from "./stocktake-recovery";

const schema = pharmacyStocktakeStartInputSchema.superRefine((value, ctx) => {
  if (value.selection === "selected" && value.product_ids.length === 0)
    ctx.addIssue({
      code: "custom",
      path: ["product_ids"],
      message: "Select at least one product, up to 100.",
    });
  if (value.selection === "all" && value.product_ids.length !== 0)
    ctx.addIssue({
      code: "custom",
      path: ["product_ids"],
      message: "All-product counts cannot also contain a selection.",
    });
});
type Attempt = { command: string; input: PharmacyStocktakeStartInput };

export function StocktakeStart({
  scope,
  onStarted,
}: {
  scope: PharmacyStocktakeScope;
  onStarted: (take: PharmacyStocktake) => void;
}) {
  const form = useZodForm(schema, {
    selection: "all",
    product_ids: [],
    quarantine_location_id: "",
    reason: "",
  });
  const selected = form.watch("selection") === "selected";
  const request = {
    headers: { "X-Helivanta-Expected-Tenant": scope.tenantId },
    cache: "no-store" as const,
  };
  const locations = useApiPagedQuery<PharmacyLocation>(
    stocktakeKey(scope, "stocktake-locations"),
    `${pharmacyRoot(scope)}/stock/locations`,
    { request },
  );
  const products = useApiPagedQuery<PharmacyProduct>(
    stocktakeKey(scope, "stocktake-products"),
    `${pharmacyRoot(scope)}/catalogue/products`,
    { request, enabled: selected },
  );
  const [attempt, setAttempt] = useState<Attempt | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [saved, setSaved] = useState(false);
  const complete = (take: PharmacyStocktake) => {
    forgetStocktakeCommand(scope, { kind: "start", stocktakeId: take.id, commandId: take.id });
    setAttempt(null);
    setSaved(true);
    form.reset();
    onStarted(take);
  };
  const save = useMutation({
    mutationFn: async (value: Attempt) => {
      retainStocktakeCommand(scope, {
        kind: "start",
        stocktakeId: value.command,
        commandId: value.command,
      });
      complete(await startStocktake(scope, value.command, value.input));
    },
    retry: false,
  });
  const recover = useMutation({
    mutationFn: async () => {
      if (!attempt) return;
      setNotFound(false);
      try {
        complete(await recoverStocktakeStart(scope, attempt.command, attempt.input));
      } catch (error) {
        if (error instanceof ApiError && error.status === 404) {
          setNotFound(true);
          return;
        }
        throw error;
      }
    },
    retry: false,
  });
  const busy = save.isPending || recover.isPending;
  const retentionFailed = save.error instanceof StocktakeRecoveryError;
  const unavailable =
    locations.isPending ||
    locations.isRefreshing ||
    Boolean(locations.error) ||
    (selected && (products.isPending || products.isRefreshing || Boolean(products.error)));
  const locked = busy || attempt !== null || saved;
  useLeaveGuard(!saved && (form.formState.isDirty || attempt !== null), "new stocktake", busy);
  const chosen = form.watch("product_ids");
  return (
    <Panel aria-label="Start stocktake">
      <form
        noValidate
        className="flex max-w-xl flex-col gap-4"
        onSubmit={form.handleSubmit((input) => {
          if (locked || unavailable) return;
          const next = { command: newPharmacyId(), input };
          setAttempt(next);
          setNotFound(false);
          save.mutate(next);
        })}
      >
        <h2 className="font-semibold">Start stocktake</h2>
        <p className="text-sm text-muted-foreground">
          Freeze current batch/location book quantities before counting. Stock movements before you
          record a bucket invalidate that observation. One open stocktake is allowed per outlet.
        </p>
        <FormSelectField
          control={form.control}
          name="selection"
          id="stocktake-selection"
          label="Product scope"
          options={[
            { value: "all", label: "All products" },
            { value: "selected", label: "Selected products" },
          ]}
          disabled={locked}
          onValueChange={(value) => {
            if (value === "all") form.setValue("product_ids", [], { shouldDirty: true });
          }}
        />
        {selected && (
          <fieldset disabled={locked}>
            <legend className="text-sm font-medium">Select products ({chosen.length}/100)</legend>
            {products.error ? (
              <RequestFailure
                message="Products are unavailable."
                error={products.error}
                retry={products.retry}
              />
            ) : products.isPending ? (
              <p role="status">Loading products…</p>
            ) : (
              <div className="mt-2 max-h-72 space-y-2 overflow-y-auto">
                {products.items.map((product) => (
                  <label key={product.id} className="flex items-center gap-2 text-sm">
                    <input
                      type="checkbox"
                      checked={chosen.includes(product.id)}
                      disabled={!chosen.includes(product.id) && chosen.length >= 100}
                      onChange={(event) =>
                        form.setValue(
                          "product_ids",
                          event.target.checked
                            ? [...chosen, product.id]
                            : chosen.filter((id) => id !== product.id),
                          { shouldDirty: true, shouldValidate: true },
                        )
                      }
                    />
                    {product.brand_name} · {product.sku}
                  </label>
                ))}
              </div>
            )}
            {form.formState.errors.product_ids?.message && (
              <p role="alert" className="text-sm text-destructive">
                {form.formState.errors.product_ids.message}
              </p>
            )}
            <LoadMore
              hasMore={products.hasMore}
              isLoading={products.isFetchingMore}
              onClick={products.loadMore}
              label="Load more stocktake products"
            />
          </fieldset>
        )}
        {locations.error && (
          <RequestFailure
            message="Quarantine locations are unavailable."
            error={locations.error}
            retry={locations.retry}
          />
        )}
        <FormSelectField
          control={form.control}
          name="quarantine_location_id"
          id="stocktake-quarantine"
          label="Quarantine destination"
          options={locations.items
            .filter((location) => location.stock_status === "quarantine")
            .map((location) => ({
              value: location.id,
              label: `${location.name} · ${location.code}`,
            }))}
          disabled={locked || locations.isPending || Boolean(locations.error)}
        />
        <LoadMore
          hasMore={locations.hasMore}
          isLoading={locations.isFetchingMore}
          onClick={locations.loadMore}
          label="Load more stocktake locations"
        />
        <Field
          id="stocktake-start-reason"
          label="Start reason"
          error={form.formState.errors.reason?.message}
        >
          <Input
            id="stocktake-start-reason"
            maxLength={500}
            {...form.register("reason")}
            disabled={locked}
          />
        </Field>
        {attempt && save.error && (
          <div role="alert" className="space-y-2 text-sm">
            <p>
              {retentionFailed
                ? "No start request was sent because its recovery reference could not be retained. Check browser storage or resolve pending results before retrying."
                : "The start could not be confirmed. Your original scope and command are retained. Check for the count before retrying."}
            </p>
            {!retentionFailed && (
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={() => recover.mutate()}
              >
                Check started count
              </Button>
            )}
            {(notFound || retentionFailed) && (
              <>
                <p>
                  {retentionFailed
                    ? "Retry the same start after recovery storage is available."
                    : "The count was not found. Retry the same start without changing its scope."}
                </p>
                <Button
                  type="button"
                  disabled={busy}
                  onClick={() => {
                    setNotFound(false);
                    save.mutate(attempt);
                  }}
                >
                  Retry original start
                </Button>
              </>
            )}
          </div>
        )}
        {recover.error && (
          <RequestFailure
            message="The original count could not be checked. Keep this form open."
            error={recover.error}
            retry={() => recover.mutate()}
          />
        )}
        {saved && <p role="status">Stocktake started.</p>}
        <Button type="submit" disabled={locked || unavailable}>
          {busy ? "Checking start…" : "Freeze book and start count"}
        </Button>
      </form>
    </Panel>
  );
}
