"use client";

import { useCallback, useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  pharmacyKey,
  pharmacyRoot,
  formatZonedDateTime,
  isCustomerTimeZone,
  zonedDateTimeISO,
  useApiPagedQuery,
  useApiQuery,
  type PharmacyScope,
  formatPharmacyStorageAddress,
  type PharmacyLocation,
  type PharmacyProduct,
  type PharmacyStockPosition,
  type PharmacyStockReconciliation,
  type PharmacyMovement,
} from "@helivanta/api";
import { Field, LoadMore, Panel, RequestFailure, SelectControl } from "@helivanta/ui";
import { StockExport } from "./stock-export";

type Props = { scope: PharmacyScope; timeZone?: string };
const quantity = (value: number | string) =>
  typeof value === "string" ? BigInt(value).toLocaleString() : value.toLocaleString();
const expiryDescription = (days: number) => {
  if (days === 0) return "Expires today";
  const duration = `${Math.abs(days)} ${Math.abs(days) === 1 ? "day" : "days"}`;
  return days < 0 ? `Expired ${duration} ago` : `Expires in ${duration}`;
};
const requestFor = (scope: PharmacyScope) => ({
  headers: new Headers({ "X-Helivanta-Expected-Tenant": scope.tenantId }),
  cache: "no-store" as const,
});

export function StockRegister(props: Props) {
  const timeZone = props.timeZone ?? "UTC";
  if (!isCustomerTimeZone(timeZone))
    return (
      <p role="alert">
        The configured outlet time zone is invalid. Ask your administrator to correct it.
      </p>
    );
  return (
    <OutletStockRegister
      key={`${props.scope.tenantId}:${props.scope.pharmacyId}:${props.scope.outletId}:${timeZone}`}
      {...props}
    />
  );
}

function OutletStockRegister({ scope, timeZone = "UTC" }: Props) {
  const [asOf, setAsOf] = useState(() => new Date(Date.now()).toISOString());
  const [locationId, setLocation] = useState("all");
  const [productId, setProduct] = useState("all");
  const [expiry, setExpiry] = useState("all");
  const [balance, setBalance] = useState("all");
  const [selected, setSelected] = useState<PharmacyStockPosition | null>(null);
  const selectedTrigger = useRef<HTMLButtonElement | null>(null);
  const locations = useApiPagedQuery<PharmacyLocation>(
    pharmacyKey(scope, "stock", "locations"),
    `${pharmacyRoot(scope)}/stock/locations`,
    { request: requestFor(scope) },
  );
  const products = useApiPagedQuery<PharmacyProduct>(
    pharmacyKey(scope, "catalogue", "stock-register"),
    `${pharmacyRoot(scope)}/catalogue/products`,
    { request: requestFor(scope) },
  );
  const params = new URLSearchParams({ as_of: asOf, expiry, balance });
  if (locationId !== "all") params.set("location_id", locationId);
  if (productId !== "all") params.set("product_id", productId);
  const positions = useApiPagedQuery<PharmacyStockPosition>(
    pharmacyKey(scope, "stock", "positions", asOf, locationId, productId, expiry, balance),
    `${pharmacyRoot(scope)}/stock/positions?${params}`,
    { request: requestFor(scope) },
  );
  return (
    <Panel aria-label="Stock register" className="min-w-0 space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="font-semibold">Stock register</h3>
          <p className="mt-1 text-sm text-muted-foreground">
            Recorded stock by location and exact manufacturer lot, including zero balances.
          </p>
        </div>
        <Button
          variant="outline"
          onClick={() => {
            setSelected(null);
            setAsOf(new Date(Date.now()).toISOString());
          }}
        >
          Refresh snapshot
        </Button>
      </div>
      <p className="break-words text-xs text-muted-foreground">
        Snapshot ends at{" "}
        <time dateTime={asOf} title={asOf}>
          {formatZonedDateTime(asOf, timeZone)}
        </time>{" "}
        ({timeZone}). Quantities use this cutoff; product and location details show current records.
        All stock remains quarantined and unavailable for dispensing.
      </p>
      <p className="text-sm text-muted-foreground">
        Expiry uses the outlet’s saved time zone at this snapshot. Due windows include today and
        exclude already expired stock.
      </p>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <p className="max-w-2xl text-xs text-muted-foreground">
          CSV includes all matching rows, including unloaded pages. Snapshot and filters apply;
          quantities use each product’s base unit. Narrow the filters if the export is too large.
        </p>
        <StockExport
          key={`${scope.tenantId}:${scope.pharmacyId}:${scope.outletId}:${params}`}
          scope={scope}
          filters={params.toString()}
          disabled={
            positions.isPending || positions.isError || locations.isError || products.isError
          }
        />
      </div>
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Field id="register-location" label="Filter stock location">
          <SelectControl
            id="register-location"
            value={locationId}
            onValueChange={(value) => {
              setLocation(value);
              setSelected(null);
            }}
            disabled={locations.isPending || locations.isError}
            options={[
              { value: "all", label: "All locations" },
              ...locations.items.map((row) => ({
                value: row.id,
                label: `${row.code} · ${row.name} · ${formatPharmacyStorageAddress(row.storage_address)}`,
              })),
            ]}
          />
        </Field>
        <Field id="register-product" label="Filter stock product">
          <SelectControl
            id="register-product"
            value={productId}
            onValueChange={(value) => {
              setProduct(value);
              setSelected(null);
            }}
            disabled={products.isPending || products.isError}
            options={[
              { value: "all", label: "All products" },
              ...products.items.map((row) => ({
                value: row.id,
                label: `${row.sku} · ${row.brand_name}`,
              })),
            ]}
          />
        </Field>
        <Field id="register-expiry" label="Filter expiry at snapshot">
          <SelectControl
            id="register-expiry"
            value={expiry}
            onValueChange={(value) => {
              setExpiry(value);
              setSelected(null);
            }}
            options={[
              { value: "all", label: "All expiry dates" },
              { value: "expired", label: "Expired" },
              { value: "today", label: "Expires today" },
              { value: "due_30", label: "Due within 30 days (including today)" },
              { value: "due_90", label: "Due within 90 days (including today)" },
              { value: "due_180", label: "Due within 180 days (including today)" },
              { value: "unexpired", label: "Unexpired" },
            ]}
          />
        </Field>
        <Field id="register-balance" label="Filter stock balance">
          <SelectControl
            id="register-balance"
            value={balance}
            onValueChange={(value) => {
              setBalance(value);
              setSelected(null);
            }}
            options={[
              { value: "all", label: "All balances (including zero)" },
              { value: "on_hand", label: "Positive stock on hand" },
            ]}
          />
        </Field>
      </div>
      {locations.isError || products.isError ? (
        <RequestFailure
          message="Stock filter options are unavailable."
          retry={() => {
            locations.retry();
            products.retry();
          }}
        />
      ) : (
        <div className="grid gap-3 sm:grid-cols-2">
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
        </div>
      )}
      {positions.isError ? (
        <RequestFailure
          message="Could not load this stock snapshot. Previously loaded rows are hidden."
          retry={positions.retry}
        />
      ) : positions.isPending ? (
        <p role="status">Loading stock snapshot…</p>
      ) : positions.items.length === 0 ? (
        <p>No stock positions match these filters at this snapshot.</p>
      ) : (
        <>
          <div
            role="region"
            aria-label="Scrollable stock register"
            // eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex -- Keyboard users need focus here for arrow-key horizontal scrolling.
            tabIndex={0}
            className="max-w-full overflow-x-auto rounded-md border focus-within:outline-2 focus-within:outline-(--hms-accent)"
          >
            <table className="w-full min-w-200 text-left text-sm">
              <caption className="sr-only">Stock by location and manufacturer lot</caption>
              <thead className="bg-muted/40">
                <tr>
                  {[
                    "Product / lot",
                    "Location",
                    "Expiry",
                    "State",
                    "On hand",
                    "Receipts",
                    "Corrections",
                    "Dispensable",
                    "Inspect",
                  ].map((label) => (
                    <th scope="col" key={label} className="p-3 font-medium">
                      {label}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {positions.items.map((row) => (
                  <tr key={row.id} className="border-t align-top">
                    <th scope="row" className="p-3 font-normal">
                      <strong>{row.sku}</strong>
                      <div>{row.brand_name}</div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        Base unit: {row.unit_of_issue}
                      </div>
                      <div className="mt-1 text-xs text-muted-foreground">{row.lot_number}</div>
                    </th>
                    <td className="p-3">
                      {row.location_code}
                      <div className="text-xs text-muted-foreground">{row.location_name}</div>
                      <div className="text-xs text-muted-foreground">
                        {formatPharmacyStorageAddress(row.storage_address)}
                      </div>
                    </td>
                    <td className="p-3">
                      <time dateTime={row.expires_on}>{row.expires_on}</time>
                      <div
                        className={`mt-1 font-medium ${row.expiry_band === "expired" ? "text-destructive" : ""}`}
                      >
                        {expiryDescription(row.days_to_expiry)}
                      </div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        As of {row.expiry_local_date} · {row.expiry_time_zone}
                      </div>
                    </td>
                    <td className="p-3">
                      <span className="capitalize">{row.stock_status}</span>
                      <div className="text-xs text-muted-foreground">
                        {row.product_status} · {row.regulatory_status.replaceAll("_", " ")}
                      </div>
                    </td>
                    {(
                      [
                        ["on_hand", row.on_hand_units],
                        ["receipts", row.receipt_units],
                        ["corrections", row.correction_units],
                        ["dispensable", row.dispensable_units],
                      ] as const
                    ).map(([field, value]) => (
                      <td className="p-3 tabular-nums" key={field}>
                        {quantity(value)}
                        <span className="sr-only"> {row.unit_of_issue}</span>
                      </td>
                    ))}
                    <td className="p-3">
                      <Button
                        variant="outline"
                        size="sm"
                        aria-label={`Inspect ${row.sku} lot ${row.lot_number} at ${row.location_code}`}
                        onClick={(event) => {
                          selectedTrigger.current = event.currentTarget;
                          setSelected(row);
                        }}
                      >
                        Inspect lot
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="text-xs text-muted-foreground">
            {positions.items.length} loaded stock positions. Quantities are per row, in each
            product’s base unit.
          </p>
          <LoadMore
            hasMore={positions.hasMore}
            isLoading={positions.isFetchingMore}
            onClick={positions.loadMore}
          />
        </>
      )}
      {!positions.isError && selected && (
        <PositionDetail
          key={`${selected.id}:${asOf}`}
          scope={scope}
          row={selected}
          asOf={asOf}
          timeZone={timeZone}
          onClose={() => {
            setSelected(null);
            selectedTrigger.current?.focus();
          }}
        />
      )}
    </Panel>
  );
}

function PositionDetail({
  scope,
  timeZone = "UTC",
  row,
  asOf,
  onClose,
}: Props & { row: PharmacyStockPosition; asOf: string; onClose: () => void }) {
  const [fromInput, setFromInput] = useState("");
  const [from, setFrom] = useState<string | null>(null);
  const [error, setError] = useState("");
  const heading = useCallback((element: HTMLHeadingElement | null) => {
    element?.scrollIntoView({ block: "start", behavior: "instant" });
    element?.focus({ preventScroll: true });
  }, []);
  const params = new URLSearchParams({
    location_id: row.location_id,
    batch_id: row.batch_id,
    as_of: asOf,
  });
  if (from) params.set("from", from);
  const reconciliation = useApiQuery<PharmacyStockReconciliation>(
    pharmacyKey(scope, "stock", "reconciliation", row.location_id, row.batch_id, from, asOf),
    `${pharmacyRoot(scope)}/stock/reconciliation?${params}`,
    { gcTime: 0, request: requestFor(scope) },
  );
  const movements = useApiPagedQuery<PharmacyMovement>(
    pharmacyKey(scope, "stock", "period-movements", row.location_id, row.batch_id, from, asOf),
    `${pharmacyRoot(scope)}/stock/movements?${params}`,
    { enabled: !!reconciliation.data && !reconciliation.isError, request: requestFor(scope) },
  );
  return (
    <section aria-label="Selected stock reconciliation" className="space-y-4 border-t pt-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <h4 ref={heading} tabIndex={-1} className="scroll-mt-32 break-words font-semibold">
          {row.sku} · {row.lot_number} · {row.location_code}
        </h4>
        <Button variant="ghost" onClick={onClose}>
          Close reconciliation
        </Button>
      </div>
      <form
        noValidate
        className="flex flex-wrap items-end gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          const instant = fromInput ? zonedDateTimeISO(fromInput, timeZone) : null;
          if (fromInput && (!instant || Date.parse(instant) > Date.parse(asOf))) {
            setError(
              `Period start must be an unambiguous time in ${timeZone} at or before the snapshot.`,
            );
            return;
          }
          setError("");
          setFrom(instant);
        }}
      >
        <Field
          id="register-period-from"
          label={`Period starts at (${timeZone}, optional)`}
          error={error || undefined}
        >
          <Input
            id="register-period-from"
            type="datetime-local"
            value={fromInput}
            onChange={(event) => setFromInput(event.target.value)}
            aria-invalid={!!error}
            aria-describedby={error ? "register-period-from-error" : undefined}
          />
        </Field>
        <Button type="submit" variant="outline">
          Apply period
        </Button>
      </form>
      <p className="break-words text-xs text-muted-foreground">
        {from ? `Period from ${formatZonedDateTime(from, timeZone)}` : "All recorded history"}{" "}
        through {formatZonedDateTime(asOf, timeZone)} ({timeZone}). Opening + receipts + corrections
        = closing stock.
      </p>
      {reconciliation.isError ? (
        <RequestFailure
          message="Could not reconcile this lot for the selected period."
          retry={reconciliation.refetch}
        />
      ) : reconciliation.isPending ? (
        <p role="status">Loading period reconciliation…</p>
      ) : (
        <>
          <dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {[
              ["Opening stock", reconciliation.data?.opening_units],
              ["Receipts in period", reconciliation.data?.receipt_units],
              ["Corrections in period", reconciliation.data?.correction_units],
              ["Closing stock", reconciliation.data?.closing_units],
            ].map(([label, value]) => (
              <div key={label}>
                <dt className="text-sm text-muted-foreground">{label}</dt>
                <dd className="mt-1 text-xl font-semibold tabular-nums">
                  {quantity(value ?? 0)}{" "}
                  <span className="text-sm font-normal">{row.unit_of_issue}</span>
                </dd>
              </div>
            ))}
          </dl>
          <h5 className="font-medium">Period movement ledger</h5>
          <p className="text-xs text-muted-foreground">
            {reconciliation.data?.movement_count} movements in this period. Existing entries cannot
            be edited.
          </p>
          {movements.isError ? (
            <RequestFailure message="Could not load the period ledger." retry={movements.retry} />
          ) : movements.isPending ? (
            <p role="status">Loading period ledger…</p>
          ) : movements.items.length === 0 ? (
            <p>No movements in this period.</p>
          ) : (
            <ol className="divide-y">
              {movements.items.map((movement) => (
                <li className="space-y-1 py-3 text-sm" key={movement.id}>
                  <p className="break-words font-medium">
                    {movement.source_reference} · {movement.kind} ·{" "}
                    {quantity(movement.quantity_units)} {row.unit_of_issue}
                  </p>
                  <p className="break-words text-muted-foreground">{movement.reason}</p>
                  <p className="break-words text-xs text-muted-foreground">
                    <time dateTime={movement.created_at} title={movement.created_at}>
                      {formatZonedDateTime(movement.created_at, timeZone)}
                    </time>{" "}
                    · {movement.actor_subject}
                  </p>
                </li>
              ))}
            </ol>
          )}
          {!movements.isError && (
            <LoadMore
              hasMore={movements.hasMore}
              isLoading={movements.isFetchingMore}
              onClick={movements.loadMore}
            />
          )}
        </>
      )}
    </section>
  );
}
