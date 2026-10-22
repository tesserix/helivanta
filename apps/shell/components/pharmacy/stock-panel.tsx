"use client";

import { useState } from "react";
import { z } from "zod";
import Link from "next/link";
import { Button, Input } from "@tesserix/web";
import { PackageCheck, Warehouse, Layers3 } from "lucide-react";
import {
  apiFetch,
  formatZonedDateTime,
  useApiPagedQuery,
  useApiQuery,
  useApiMutation,
  newPharmacyId,
  pharmacyRoot,
  pharmacyKey,
  pharmacyLocationInputSchema,
  formatPharmacyStorageAddress,
  pharmacyBatchInputSchema,
  pharmacyMovementInputSchema,
  type PharmacyScope,
  type PharmacyProduct,
  type PharmacyLocation,
  type PharmacyBatch,
  type PharmacyMovement,
} from "@helivanta/api";
import {
  ConfirmDialog,
  Field,
  FormSelectField,
  LoadMore,
  Panel,
  RequestFailure,
  SelectControl,
  useZodForm,
} from "@helivanta/ui";

const inputClass =
  "w-full rounded-md border bg-background px-3 py-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-(--hms-accent)";

export function StockPanel({
  scope,
  writable,
  correctable,
  timeZone = "UTC",
}: {
  scope: PharmacyScope;
  writable: boolean;
  correctable: boolean;
  timeZone?: string;
}) {
  const [locationId, setLocation] = useState("");
  const [productId, setProduct] = useState("");
  const [batchId, setBatch] = useState("");
  const locations = useApiPagedQuery<PharmacyLocation>(
    pharmacyKey(scope, "stock", "locations"),
    `${pharmacyRoot(scope)}/stock/locations`,
  );
  const products = useApiPagedQuery<PharmacyProduct>(
    pharmacyKey(scope, "catalogue", "active"),
    `${pharmacyRoot(scope)}/catalogue/products?status=active`,
  );
  const batches = useApiPagedQuery<PharmacyBatch>(
    pharmacyKey(scope, "stock", "batches", productId),
    `${pharmacyRoot(scope)}/stock/batches?product_id=${productId}`,
    { enabled: !!productId },
  );
  return (
    <div className="space-y-6">
      <Panel>
        <div className="mb-5 flex flex-wrap items-center justify-between gap-4">
          <div>
            <h3 className="flex items-center gap-2 font-semibold">
              <Warehouse className="size-5 text-(--hms-accent)" />
              Quarantine receiving
            </h3>
            <p className="mt-2 text-sm text-muted-foreground">
              Select a location, product and exact manufacturer lot before recording stock.
            </p>
          </div>
          <Link
            href="/pharmacy/catalogue"
            className="text-sm font-medium text-(--hms-accent) underline-offset-4 hover:underline"
          >
            Open product catalogue
          </Link>
        </div>
        {locations.isError || products.isError ? (
          <RequestFailure
            message="Could not load stock locations or products."
            retry={() => {
              locations.retry();
              products.retry();
            }}
          />
        ) : (
          <>
            <div className="grid gap-5 lg:grid-cols-3">
              <Field id="stock-location" label="Quarantine location">
                <SelectControl
                  id="stock-location"
                  value={locationId}
                  onValueChange={setLocation}
                  placeholder="Select a location"
                  options={locations.items.map((row) => ({
                    value: row.id,
                    label: `${row.code} · ${row.name} · ${formatPharmacyStorageAddress(row.storage_address)}`,
                  }))}
                  disabled={locations.isPending}
                />
              </Field>
              <Field id="stock-product" label="Product">
                <SelectControl
                  id="stock-product"
                  value={productId}
                  onValueChange={(value) => {
                    setProduct(value);
                    setBatch("");
                  }}
                  placeholder="Select a product"
                  options={products.items.map((row) => ({
                    value: row.id,
                    label: `${row.sku} · ${row.brand_name}`,
                  }))}
                  disabled={products.isPending}
                />
              </Field>
              <Field id="stock-batch" label="Manufacturer lot">
                <SelectControl
                  id="stock-batch"
                  value={batchId}
                  onValueChange={setBatch}
                  placeholder={productId ? "Select an exact lot" : "Choose a product first"}
                  options={batches.items.map((row) => ({
                    value: row.id,
                    label: `${row.lot_number} · Expires ${row.expires_on}`,
                  }))}
                  disabled={!productId || batches.isPending || batches.isError}
                />
              </Field>
            </div>
            <div className="mt-4 grid gap-3 lg:grid-cols-3">
              <LoadMore
                hasMore={locations.hasMore}
                isLoading={locations.isFetchingMore}
                onClick={locations.loadMore}
              />
              <LoadMore
                hasMore={products.hasMore}
                isLoading={products.isFetchingMore}
                onClick={products.loadMore}
              />
              <LoadMore
                hasMore={batches.hasMore}
                isLoading={batches.isFetchingMore}
                onClick={batches.loadMore}
              />
            </div>
            {batches.isError && productId && (
              <RequestFailure message="Could not load manufacturer lots." retry={batches.retry} />
            )}
          </>
        )}
      </Panel>
      {locationId && batchId && (
        <StockBucket
          key={`${scope.tenantId}:${scope.pharmacyId}:${scope.outletId}:${locationId}:${batchId}:${timeZone}`}
          scope={scope}
          locationId={locationId}
          batchId={batchId}
          timeZone={timeZone}
          writable={writable}
          correctable={correctable}
        />
      )}
      {writable && (
        <div className="grid items-start gap-6 xl:grid-cols-2">
          <LocationForm scope={scope} />
          {productId ? (
            <BatchForm key={productId} scope={scope} productId={productId} />
          ) : (
            <Panel>
              <Layers3 className="mb-3 size-5 text-muted-foreground" />
              <h3 className="font-semibold">Register a manufacturer lot</h3>
              <p className="mt-2 text-sm text-muted-foreground">
                Choose a product above to record its lot and expiry. Lot identity cannot be edited
                after registration.
              </p>
            </Panel>
          )}
        </div>
      )}
    </div>
  );
}

function LocationForm({ scope }: { scope: PharmacyScope }) {
  const [id, setId] = useState(() => newPharmacyId());
  const form = useZodForm(pharmacyLocationInputSchema, {
    code: "",
    name: "",
    storage_address: { zone_code: "", aisle_code: "", rack_code: "", shelf_code: "" },
  });
  const save = useApiMutation(
    (values: z.infer<typeof pharmacyLocationInputSchema>) =>
      apiFetch(`${pharmacyRoot(scope)}/stock/locations/${id}`, {
        method: "PUT",
        body: JSON.stringify(values),
      }),
    {
      successToast: "Quarantine location added",
      invalidate: [pharmacyKey(scope, "stock", "locations")],
      onSuccess: () => {
        setId(newPharmacyId());
        form.reset();
      },
      suppressErrorToast: true,
    },
  );
  return (
    <Panel>
      <h3 className="mb-2 font-semibold">Add quarantine location</h3>
      <p className="mb-5 text-sm text-muted-foreground">
        Use a unique code. Stock in this location is unavailable for dispensing.
      </p>
      <form
        noValidate
        className="space-y-4"
        onSubmit={form.handleSubmit((values) => save.mutate(values))}
      >
        <Field id="location-code" label="Location code" error={form.formState.errors.code?.message}>
          <Input id="location-code" {...form.register("code")} />
        </Field>
        <Field id="location-name" label="Location name" error={form.formState.errors.name?.message}>
          <Input id="location-name" {...form.register("name")} />
        </Field>
        <fieldset className="space-y-4">
          <legend className="text-sm font-medium">Physical storage address (optional)</legend>
          <p className="text-sm text-muted-foreground">
            Leave unknown labels blank. The location code identifies the bin; several bins may share
            a shelf. Saved locations cannot be relabelled.
          </p>
          <div className="grid gap-4 sm:grid-cols-2">
            {(
              [
                ["zone_code", "Zone"],
                ["aisle_code", "Aisle"],
                ["rack_code", "Rack"],
                ["shelf_code", "Shelf"],
              ] as const
            ).map(([key, label]) => (
              <Field
                key={key}
                id={`location-${key}`}
                label={`${label} code (optional)`}
                error={form.formState.errors.storage_address?.[key]?.message}
              >
                <Input
                  id={`location-${key}`}
                  maxLength={32}
                  {...form.register(`storage_address.${key}`)}
                />
              </Field>
            ))}
          </div>
        </fieldset>
        {save.isError && (
          <p role="alert" className="text-sm text-destructive">
            {save.error.message}
          </p>
        )}
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Adding…" : "Add quarantine location"}
        </Button>
      </form>
    </Panel>
  );
}
const batchSchema = pharmacyBatchInputSchema.superRefine((value, ctx) => {
  if (value.manufactured_on && value.manufactured_on > value.expires_on)
    ctx.addIssue({
      code: "custom",
      path: ["manufactured_on"],
      message: "Manufacturing date cannot follow expiry.",
    });
});
function BatchForm({ scope, productId }: { scope: PharmacyScope; productId: string }) {
  const [id, setId] = useState(() => newPharmacyId());
  const form = useZodForm(batchSchema, {
    product_id: productId,
    lot_number: "",
    expires_on: "",
    manufactured_on: null,
  });
  const save = useApiMutation(
    (values: z.infer<typeof batchSchema>) =>
      apiFetch(`${pharmacyRoot(scope)}/stock/batches/${id}`, {
        method: "PUT",
        body: JSON.stringify(values),
      }),
    {
      successToast: "Manufacturer lot registered",
      invalidate: [pharmacyKey(scope, "stock", "batches", productId)],
      onSuccess: () => {
        setId(newPharmacyId());
        form.reset();
      },
      suppressErrorToast: true,
    },
  );
  return (
    <Panel>
      <h3 className="mb-2 font-semibold">Register manufacturer lot</h3>
      <p className="mb-5 text-sm text-muted-foreground">
        Use the exact label and a confirmed full expiry date. Month-only labels need review before
        entry.
      </p>
      <form
        noValidate
        className="space-y-4"
        onSubmit={form.handleSubmit((values) => save.mutate(values))}
      >
        <Field
          id="lot-number"
          label="Manufacturer lot number"
          error={form.formState.errors.lot_number?.message}
        >
          <Input id="lot-number" {...form.register("lot_number")} />
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            id="lot-expiry"
            label="Expiry date"
            error={form.formState.errors.expires_on?.message}
          >
            <input
              id="lot-expiry"
              className={inputClass}
              type="date"
              {...form.register("expires_on")}
            />
          </Field>
          <Field
            id="lot-manufactured"
            label="Manufactured on (optional)"
            error={form.formState.errors.manufactured_on?.message}
          >
            <input
              id="lot-manufactured"
              className={inputClass}
              type="date"
              {...form.register("manufactured_on", { setValueAs: (value) => value || null })}
            />
          </Field>
        </div>
        {save.isError && (
          <p role="alert" className="text-sm text-destructive">
            {save.error.message}
          </p>
        )}
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Registering…" : "Register manufacturer lot"}
        </Button>
      </form>
    </Panel>
  );
}

function StockBucket({
  scope,
  locationId,
  batchId,
  writable,
  correctable,
  timeZone,
}: {
  scope: PharmacyScope;
  locationId: string;
  batchId: string;
  writable: boolean;
  correctable: boolean;
  timeZone: string;
}) {
  const balance = useApiQuery<{
    on_hand_units: number;
    dispensable_units: number;
    stock_status: string;
  }>(
    pharmacyKey(scope, "stock", "balance", locationId, batchId),
    `${pharmacyRoot(scope)}/stock/balance?location_id=${locationId}&batch_id=${batchId}`,
    { gcTime: 0 },
  );
  const movements = useApiPagedQuery<PharmacyMovement>(
    pharmacyKey(scope, "stock", "movements", locationId, batchId),
    `${pharmacyRoot(scope)}/stock/movements?location_id=${locationId}&batch_id=${batchId}`,
  );
  return (
    <Panel flush>
      <div className="border-b p-6">
        <h3 className="mb-5 flex items-center gap-2 font-semibold">
          <PackageCheck className="size-5 text-(--hms-accent)" />
          Selected lot balance
        </h3>
        {balance.isError ? (
          <RequestFailure message="Could not load this stock balance." retry={balance.refetch} />
        ) : balance.isPending ? (
          <p role="status">Loading balance…</p>
        ) : (
          <dl className="grid gap-5 sm:grid-cols-3">
            <div>
              <dt className="text-sm text-muted-foreground">On hand (base units)</dt>
              <dd className="mt-1 text-3xl font-semibold tabular-nums">
                {balance.data?.on_hand_units}
              </dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Available to dispense</dt>
              <dd className="mt-1 text-3xl font-semibold tabular-nums">
                {balance.data?.dispensable_units}
              </dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Stock status</dt>
              <dd className="mt-2 inline-flex rounded-full bg-(--hms-amber-tint) px-3 py-1 text-sm font-medium text-(--hms-amber)">
                Quarantine
              </dd>
            </div>
          </dl>
        )}
      </div>
      {(writable || correctable) && (
        <MovementForm
          scope={scope}
          locationId={locationId}
          batchId={batchId}
          writable={writable}
          correctable={correctable}
        />
      )}
      <div className="border-t p-6">
        <h4 className="mb-4 font-medium">Movement history</h4>
        {movements.isError ? (
          <RequestFailure message="Could not load stock movements." retry={movements.retry} />
        ) : movements.isPending ? (
          <p role="status">Loading movements…</p>
        ) : movements.items.length === 0 ? (
          <p className="text-sm text-muted-foreground">No movements for this location and lot.</p>
        ) : (
          <ol className="divide-y">
            {movements.items.map((row) => (
              <li key={row.id} className="flex flex-wrap justify-between gap-3 py-4 text-sm">
                <div>
                  <p className="font-medium">{row.source_reference}</p>
                  <p className="mt-1 text-muted-foreground">{row.reason}</p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    <time dateTime={row.created_at} title={row.created_at}>
                      {formatZonedDateTime(row.created_at, timeZone)}
                    </time>{" "}
                    · {row.actor_subject}
                  </p>
                </div>
                <p className="font-semibold tabular-nums">
                  {row.quantity_units > 0 ? "+" : ""}
                  {row.quantity_units} · {row.kind}
                </p>
              </li>
            ))}
          </ol>
        )}
        <LoadMore
          hasMore={movements.hasMore}
          isLoading={movements.isFetchingMore}
          onClick={movements.loadMore}
        />
      </div>
    </Panel>
  );
}
const movementSchema = pharmacyMovementInputSchema.superRefine((value, ctx) => {
  if (value.kind === "receipt" && value.quantity_units <= 0)
    ctx.addIssue({
      code: "custom",
      path: ["quantity_units"],
      message: "A receipt needs a positive quantity.",
    });
});
function MovementForm({
  scope,
  locationId,
  batchId,
  writable,
  correctable,
}: {
  scope: PharmacyScope;
  locationId: string;
  batchId: string;
  writable: boolean;
  correctable: boolean;
}) {
  const [id, setId] = useState(() => newPharmacyId());
  const [confirm, setConfirm] = useState(false);
  const defaults = {
    location_id: locationId,
    batch_id: batchId,
    kind: writable ? ("receipt" as const) : ("correction" as const),
    quantity_units: 0,
    reason: "",
    source_reference: "",
  };
  const form = useZodForm(movementSchema, defaults);
  const save = useApiMutation(
    (values: z.infer<typeof movementSchema>) =>
      apiFetch(
        `${pharmacyRoot(scope)}/stock/${values.kind === "receipt" ? "movements" : "corrections"}/${id}`,
        { method: "PUT", body: JSON.stringify(values) },
      ),
    {
      successToast: "Movement recorded",
      invalidate: [pharmacyKey(scope, "stock")],
      onSuccess: () => {
        setId(newPharmacyId());
        setConfirm(false);
        form.reset(defaults);
      },
      suppressErrorToast: true,
    },
  );
  const options = [
    ...(writable ? [{ value: "receipt", label: "Receipt" }] : []),
    ...(correctable ? [{ value: "correction", label: "Correction" }] : []),
  ];
  return (
    <>
      <form
        noValidate
        className="space-y-4 p-6"
        onSubmit={form.handleSubmit((values) =>
          values.quantity_units < 0 ? setConfirm(true) : save.mutate(values),
        )}
      >
        <h4 className="font-medium">Record stock movement</h4>
        <div className="grid gap-4 sm:grid-cols-2">
          <FormSelectField
            control={form.control}
            name="kind"
            id="movement-kind"
            label="Movement type"
            options={options}
          />
          <Field
            id="movement-quantity"
            label="Quantity in base units"
            error={form.formState.errors.quantity_units?.message}
          >
            <Input
              id="movement-quantity"
              type="number"
              step="1"
              {...form.register("quantity_units", { valueAsNumber: true })}
            />
          </Field>
          <Field
            id="movement-source"
            label="Receipt / correction line reference"
            error={form.formState.errors.source_reference?.message}
          >
            <Input id="movement-source" {...form.register("source_reference")} />
          </Field>
          <Field id="movement-reason" label="Reason" error={form.formState.errors.reason?.message}>
            <Input id="movement-reason" {...form.register("reason")} />
          </Field>
        </div>
        <p className="text-xs text-muted-foreground">
          Use a unique source line reference to prevent duplicate stock. Corrections may add or
          remove units; existing ledger entries are preserved.
        </p>
        {save.isError && (
          <p role="alert" className="text-sm text-destructive">
            {save.error.message}
          </p>
        )}
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Recording…" : "Record movement"}
        </Button>
      </form>
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title="Remove stock by correction?"
        description="This creates a permanent, attributable correction entry. It cannot reduce on-hand stock below zero."
        confirmLabel="Record correction"
        busy={save.isPending}
        onConfirm={() => save.mutate(form.getValues())}
      />
    </>
  );
}
