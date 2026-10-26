"use client";

import { useCallback, useMemo, useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import { ShieldCheck } from "lucide-react";
import { useSearchParams } from "next/navigation";
import { useStepBackWhenEmpty, useUrlPage } from "@/lib/use-url-page";
import {
  ApiError,
  apiFetch,
  formatZonedDateTime,
  isCustomerTimeZone,
  newPharmacyId,
  pharmacyKey,
  pharmacyRoot,
  pharmacySafetyIncidentInputSchema,
  PAGE_SIZE,
  useApiMutation,
  useApiPageQuery,
  useApiPagedQuery,
  useApiQuery,
  zonedDateTimeInput,
  zonedDateTimeISO,
  type PharmacyBatch,
  formatPharmacyStorageAddress,
  type PharmacyLocation,
  type PharmacyProduct,
  type PharmacySafetyIncident,
  type PharmacySafetyIncidentInput,
  type PharmacyScope,
} from "@helivanta/api";
import {
  Field,
  LoadMore,
  PageTabs,
  Pager,
  Panel,
  QueryState,
  RequestFailure,
  SelectControl,
  useZodForm,
} from "@helivanta/ui";

type Props = {
  scope: PharmacyScope;
  timeZone?: string;
};
type ZonedProps = { scope: PharmacyScope; timeZone: string };
type Collection<T> = ReturnType<typeof useApiPagedQuery<T>>;
const concernTypes = [
  { value: "quality_concern", label: "Quality concern" },
  { value: "temperature_excursion", label: "Temperature excursion" },
  { value: "packaging_damage", label: "Packaging damage" },
  { value: "suspected_recall", label: "Suspected recall report" },
] as const;

const requestFor = (scope: PharmacyScope) => ({
  headers: new Headers({ "X-Helivanta-Expected-Tenant": scope.tenantId }),
  cache: "no-store" as const,
});
const invalidLocalTime =
  "Choose another local date and time. This time may not exist or may occur twice because of a timezone clock change.";
const millicelsius = (value: unknown) => {
  if (value === "" || value === undefined) return undefined;
  const match = /^(-?)(\d+)(?:\.(\d{1,3}))?$/.exec(String(value));
  if (!match) return Number.NaN;
  const amount = Number(match[2]) * 1000 + Number((match[3] ?? "").padEnd(3, "0"));
  return match[1] === "-" ? -amount : amount;
};

const TABS = { recent: "Last 30 days", batch: "By batch" } as const;
type SafetyTab = keyof typeof TABS;

export function BatchSafety({ scope, timeZone = "UTC" }: Props) {
  // The tab is the URL (#576 slice 3 D3); anything else, the overview's
  // older ?recorded=30d link included, is the last 30 days.
  const tab: SafetyTab = useSearchParams().get("tab") === "batch" ? "batch" : "recent";
  const url = useUrlPage({ tab });
  if (!isCustomerTimeZone(timeZone))
    return (
      <Panel aria-label="Batch safety concerns">
        <p role="alert">
          Batch safety is unavailable because the configured outlet timezone is invalid. Ask an
          administrator to update the outlet timezone before continuing.
        </p>
      </Panel>
    );
  const tabs = (
    <PageTabs
      label="Safety concerns"
      current={tab}
      tabs={(Object.keys(TABS) as SafetyTab[]).map((key) => ({
        key,
        label: TABS[key],
        href: url.hrefFor({ tab: key }),
      }))}
    />
  );
  const key = `${scope.tenantId}:${scope.pharmacyId}:${scope.outletId}:${timeZone}`;
  return tab === "recent" ? (
    <RecentConcerns key={key} scope={scope} timeZone={timeZone} url={url} tabs={tabs} />
  ) : (
    <ScopedSafety key={key} scope={scope} timeZone={timeZone} tabs={tabs} />
  );
}

// The outlet's concerns recorded in the last 30 days, newest first and 25 a
// page: what the overview's safety count counts (#576). A journal, not a set
// of open holds.
function RecentConcerns({
  scope,
  timeZone,
  url,
  tabs,
}: ZonedProps & { url: ReturnType<typeof useUrlPage>; tabs: React.ReactNode }) {
  const recent = useApiPageQuery<PharmacySafetyIncident>(
    pharmacyKey(scope, "safety-incidents", "recent", "counted"),
    `${pharmacyRoot(scope)}/stock/safety-incidents/recent`,
    url.cursor,
    { request: requestFor(scope) },
  );
  const emptied = useStepBackWhenEmpty(recent, url);
  return (
    <Panel flush aria-label="Concerns recorded in the last 30 days">
      {tabs}
      <div className="p-5">
        <h3 className="font-semibold">Concerns recorded in the last 30 days</h3>
        <p className="mt-1 text-sm text-muted-foreground">
          Every batch at this outlet, newest first. Open By batch to record a concern.
        </p>
        <div className="mt-4">
          <QueryState
            query={recent}
            label="recent concerns"
            empty={{ icon: ShieldCheck, title: "No concerns in the last 30 days" }}
          >
            {(rows) => (
              <ul className="divide-y text-sm">
                {rows.map((incident) => (
                  <li key={incident.id} className="py-3">
                    <p className="font-medium">
                      {concernTypes.find((item) => item.value === incident.kind)?.label ??
                        incident.kind}
                    </p>
                    <p className="mt-0.5 whitespace-pre-wrap break-words">{incident.reason}</p>
                    <p className="mt-0.5 text-muted-foreground">
                      Recorded{" "}
                      <time dateTime={incident.recorded_at}>
                        {formatZonedDateTime(incident.recorded_at, timeZone)}
                      </time>{" "}
                      by {incident.actor_subject}
                    </p>
                  </li>
                ))}
              </ul>
            )}
          </QueryState>
        </div>
      </div>
      {!recent.isPending && !emptied && (
        <Pager
          page={url.page}
          pageSize={PAGE_SIZE}
          offset={recent.offset}
          shown={recent.items.length}
          total={recent.total}
          previousHref={url.previousHref}
          nextHref={url.nextHref(recent.nextCursor)}
          firstHref={url.first}
        />
      )}
    </Panel>
  );
}

function ScopedSafety({ scope, timeZone, tabs }: ZonedProps & { tabs: React.ReactNode }) {
  const permissions = useApiQuery<{ tenant_id: string; data: string[] }>(
    pharmacyKey(scope, "batch-safety", "permissions"),
    "/iam/me/permissions",
    { request: requestFor(scope), gcTime: 0, refetchOnWindowFocus: false },
  );

  return (
    <Panel flush aria-label="Batch safety concerns" className="min-w-0">
      {tabs}
      <div className="space-y-5 p-5">
        <header>
          <h3 className="font-semibold">Batch safety concerns</h3>
          <p className="mt-1 text-sm text-muted-foreground">
            Record observed quality, packaging or temperature concerns for review. A suspected
            recall report does not verify a recall or release stock. All stock remains quarantined.
          </p>
        </header>
        {permissions.isError ? (
          <RequestFailure
            message="Could not verify safety access. Previously loaded records are hidden."
            retry={permissions.refetch}
          />
        ) : permissions.isPending ? (
          <p role="status">Checking safety access…</p>
        ) : permissions.data?.tenant_id !== scope.tenantId ? (
          <p role="alert">
            Safety access does not match the active tenant. Reload the current workspace.
          </p>
        ) : !permissions.data.data.includes("pharmacy.stock.read") ? (
          <p role="alert">You do not have permission to view batch safety concerns.</p>
        ) : (
          <SafetyCollections
            scope={scope}
            timeZone={timeZone}
            canRecord={permissions.data.data.includes("pharmacy.stock.quarantine")}
            canReadProducts={permissions.data.data.includes("pharmacy.catalogue.read")}
          />
        )}
      </div>
    </Panel>
  );
}

function SafetyCollections({
  scope,
  timeZone,
  canRecord,
  canReadProducts,
}: ZonedProps & { canRecord: boolean; canReadProducts: boolean }) {
  const batches = useApiPagedQuery<PharmacyBatch>(
    pharmacyKey(scope, "batch-safety", "batches"),
    `${pharmacyRoot(scope)}/stock/batches?limit=100`,
    { request: requestFor(scope) },
  );
  const products = useApiPagedQuery<PharmacyProduct>(
    pharmacyKey(scope, "batch-safety", "products"),
    `${pharmacyRoot(scope)}/catalogue/products?limit=100`,
    { request: requestFor(scope), enabled: canReadProducts },
  );
  const locations = useApiPagedQuery<PharmacyLocation>(
    pharmacyKey(scope, "batch-safety", "locations"),
    `${pharmacyRoot(scope)}/stock/locations?limit=100`,
    { request: requestFor(scope) },
  );
  if (batches.isError || locations.isError)
    return (
      <RequestFailure
        message="Could not load safety batch options. Previously selected batches and forms are hidden."
        retry={() => {
          batches.retry();
          if (canReadProducts) products.retry();
          locations.retry();
        }}
      />
    );
  if (batches.isPending || locations.isPending)
    return <p role="status">Loading safety batch options…</p>;
  if (batches.items.length === 0) return <p>No batches are registered for this outlet.</p>;

  return (
    <>
      {canReadProducts && products.isError && (
        <RequestFailure
          message="Product names are unavailable. Exact batch, expiry and product identifiers remain available."
          retry={products.retry}
        />
      )}
      {canReadProducts && products.isPending && (
        <p role="status">Loading optional product names…</p>
      )}
      <SafetyBatchPicker
        scope={scope}
        timeZone={timeZone}
        canRecord={canRecord}
        batches={batches}
        products={
          !canReadProducts || products.isError
            ? { ...products, items: [], hasMore: false }
            : products
        }
        locations={locations}
      />
    </>
  );
}

function SafetyBatchPicker({
  scope,
  timeZone,
  canRecord,
  batches,
  products,
  locations,
}: ZonedProps & {
  canRecord: boolean;
  batches: Collection<PharmacyBatch>;
  products: Collection<PharmacyProduct>;
  locations: Collection<PharmacyLocation>;
}) {
  const [batchId, setBatch] = useState("none");
  const selected = batches.items.find((batch) => batch.id === batchId);
  return (
    <>
      <Field id="safety-batch" label="Safety concern batch">
        <SelectControl
          id="safety-batch"
          value={batchId}
          onValueChange={setBatch}
          options={[
            { value: "none", label: "Choose a batch" },
            ...batches.items.map((batch) => {
              const product = products.items.find((item) => item.id === batch.product_id);
              return {
                value: batch.id,
                label: `${product ? `${product.sku} · ${product.brand_name}` : `Product ${batch.product_id}`} · ${batch.lot_number} · Expires ${batch.expires_on}`,
              };
            }),
          ]}
        />
      </Field>
      {(batches.hasMore || products.hasMore || locations.hasMore) && (
        <div className="grid gap-3 sm:grid-cols-3">
          {(
            [
              [batches, "Load more batches"],
              [products, "Load more products"],
              [locations, "Load more locations"],
            ] as const
          ).map(([list, label]) => (
            <LoadMore
              key={label}
              label={label}
              hasMore={list.hasMore}
              isLoading={list.isFetchingMore}
              onClick={list.loadMore}
            />
          ))}
        </div>
      )}
      {selected ? (
        <SelectedBatchSafety
          key={selected.id}
          scope={scope}
          timeZone={timeZone}
          batch={selected}
          canRecord={canRecord}
          locations={locations.items}
        />
      ) : (
        <p className="text-sm text-muted-foreground">
          Choose a batch to view its safety history or record a concern.
        </p>
      )}
    </>
  );
}

function SelectedBatchSafety({
  scope,
  timeZone,
  batch,
  canRecord,
  locations,
}: ZonedProps & {
  batch: PharmacyBatch;
  canRecord: boolean;
  locations: PharmacyLocation[];
}) {
  const focusHistory = useCallback((node: HTMLHeadingElement | null) => node?.focus(), []);
  const incidents = useApiPagedQuery<PharmacySafetyIncident>(
    pharmacyKey(scope, "safety-incidents", batch.id),
    `${pharmacyRoot(scope)}/stock/safety-incidents?batch_id=${encodeURIComponent(batch.id)}&limit=25`,
    { request: requestFor(scope) },
  );
  if (incidents.isError)
    return (
      <RequestFailure
        message="Could not load this batch's safety history. Previously loaded records and the recording form are hidden."
        retry={incidents.retry}
      />
    );
  if (incidents.isPending) return <p role="status">Loading batch safety history…</p>;
  return (
    <div className="space-y-6">
      <section aria-label={`Safety history for ${batch.lot_number}`} className="space-y-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <h4 tabIndex={-1} ref={focusHistory} className="font-medium">
            Safety history · {batch.lot_number}
          </h4>
          <Button type="button" variant="outline" size="sm" onClick={incidents.retry}>
            Refresh safety history
          </Button>
        </div>
        <p className="text-sm text-muted-foreground">
          These are permanent reports of observed concerns, not clearance decisions.
        </p>
        <p className="text-xs text-muted-foreground">Times shown in {timeZone}.</p>
        {incidents.items.length === 0 ? (
          <p>No safety concerns have been recorded for this batch.</p>
        ) : (
          <ul aria-label="Recorded safety concerns" className="space-y-3">
            {incidents.items.map((incident) => {
              const location = locations.find((item) => item.id === incident.location_id);
              return (
                <Panel key={incident.id} as="li" className="space-y-3">
                  <h5 className="font-medium">
                    {concernTypes.find((item) => item.value === incident.kind)?.label ??
                      incident.kind}
                  </h5>
                  <p className="whitespace-pre-wrap break-words text-sm">{incident.reason}</p>
                  <dl className="grid gap-3 text-sm sm:grid-cols-2">
                    <div>
                      <dt className="text-muted-foreground">Scope</dt>
                      <dd className="break-words">
                        {incident.location_id
                          ? location
                            ? `${location.code} · ${location.name} · ${formatPharmacyStorageAddress(location.storage_address)}`
                            : `Location ${incident.location_id}`
                          : "Batch across this outlet"}
                      </dd>
                    </div>
                    {incident.measured_temperature_millicelsius !== undefined &&
                      incident.measured_temperature_millicelsius !== null && (
                        <div>
                          <dt className="text-muted-foreground">Measured temperature</dt>
                          <dd>
                            {(incident.measured_temperature_millicelsius / 1000).toLocaleString(
                              undefined,
                              { maximumFractionDigits: 3 },
                            )}{" "}
                            °C
                          </dd>
                        </div>
                      )}
                    <div>
                      <dt className="text-muted-foreground">Occurred at</dt>
                      <dd className="break-words">
                        <time title={incident.occurred_at} dateTime={incident.occurred_at}>
                          {formatZonedDateTime(incident.occurred_at, timeZone)}
                        </time>
                      </dd>
                    </div>
                    <div>
                      <dt className="text-muted-foreground">Recorded at</dt>
                      <dd className="break-words">
                        <time title={incident.recorded_at} dateTime={incident.recorded_at}>
                          {formatZonedDateTime(incident.recorded_at, timeZone)}
                        </time>
                      </dd>
                    </div>
                    <div>
                      <dt className="text-muted-foreground">Recorded by</dt>
                      <dd className="break-words">{incident.actor_subject}</dd>
                    </div>
                  </dl>
                </Panel>
              );
            })}
          </ul>
        )}
        <LoadMore
          label="Load more concerns"
          hasMore={incidents.hasMore}
          isLoading={incidents.isFetchingMore}
          onClick={incidents.loadMore}
        />
      </section>
      {canRecord ? (
        <ConcernForm scope={scope} timeZone={timeZone} batchId={batch.id} locations={locations} />
      ) : (
        <p className="text-sm text-muted-foreground">
          You have read-only access to batch safety concerns.
        </p>
      )}
    </div>
  );
}

function ConcernForm({
  scope,
  timeZone,
  batchId,
  locations,
}: ZonedProps & { batchId: string; locations: PharmacyLocation[] }) {
  const [identity, setIdentity] = useState(() => newPharmacyId());
  const [occurred, setOccurred] = useState(() =>
    zonedDateTimeInput(new Date().toISOString(), timeZone),
  );
  const occurredInstant = useMemo(() => zonedDateTimeISO(occurred, timeZone), [occurred, timeZone]);
  const occurredInput = useRef<HTMLInputElement>(null);
  const [submitted, setSubmitted] = useState<PharmacySafetyIncidentInput | null>(null);
  const [confirmed, setConfirmed] = useState(false);
  const focusConfirmation = useCallback((node: HTMLParagraphElement | null) => node?.focus(), []);
  const form = useZodForm(pharmacySafetyIncidentInputSchema, {
    batch_id: batchId,
    kind: "quality_concern",
    reason: "",
    occurred_at: occurredInstant ?? "",
  });
  const mutation = useApiMutation(
    (input: PharmacySafetyIncidentInput) =>
      apiFetch<PharmacySafetyIncident>(
        `${pharmacyRoot(scope)}/stock/safety-incidents/${identity}`,
        { ...requestFor(scope), method: "PUT", body: JSON.stringify(input) },
      ),
    {
      suppressErrorToast: true,
      invalidate: [pharmacyKey(scope, "safety-incidents")],
      onSuccess: () => {
        const nextOccurred = zonedDateTimeInput(new Date().toISOString(), timeZone);
        setSubmitted(null);
        setIdentity(newPharmacyId());
        setOccurred(nextOccurred);
        form.reset({
          batch_id: batchId,
          kind: "quality_concern",
          reason: "",
          occurred_at: zonedDateTimeISO(nextOccurred, timeZone) ?? "",
        });
        setConfirmed(true);
      },
    },
  );
  const sendConcern = (input: PharmacySafetyIncidentInput) =>
    mutation.mutate(input, {
      onError: (error) => {
        if (error instanceof ApiError && (error.status === 400 || error.status === 422))
          setSubmitted(null);
      },
    });
  const kind = form.watch("kind");
  return (
    <Panel>
      <form
        noValidate
        className="space-y-4"
        aria-label="Record batch safety concern"
        onSubmit={(event) => {
          if (!occurredInstant) {
            event.preventDefault();
            form.setError("occurred_at", { type: "validate", message: invalidLocalTime });
            occurredInput.current?.focus();
            return;
          }
          void form.handleSubmit((input) => {
            setConfirmed(false);
            setSubmitted(input);
            sendConcern(input);
          })(event);
        }}
      >
        <h4 className="font-medium">Record a safety concern</h4>
        <p className="text-sm text-muted-foreground">
          Use observed evidence. Do not enter patient information. Reports cannot be edited or
          cleared here.
        </p>
        {confirmed && (
          <p role="status" tabIndex={-1} ref={focusConfirmation}>
            Safety concern recorded. Stock remains quarantined.
          </p>
        )}
        <fieldset
          disabled={mutation.isPending || submitted !== null}
          className="grid min-w-0 gap-4 sm:grid-cols-2"
        >
          <legend className="sr-only">Safety concern details</legend>
          <Field id="safety-kind" label="Concern type" error={form.formState.errors.kind?.message}>
            <SelectControl
              id="safety-kind"
              value={kind}
              onValueChange={(value) => {
                form.setValue("kind", value as PharmacySafetyIncidentInput["kind"], {
                  shouldValidate: true,
                });
                if (value !== "temperature_excursion")
                  form.setValue("measured_temperature_millicelsius", undefined, {
                    shouldValidate: true,
                  });
              }}
              options={[...concernTypes]}
              disabled={submitted !== null}
            />
          </Field>
          <Field
            id="safety-location"
            label="Concern location"
            error={form.formState.errors.location_id?.message}
          >
            <SelectControl
              id="safety-location"
              value={form.watch("location_id") ?? "all"}
              onValueChange={(value) =>
                form.setValue("location_id", value === "all" ? undefined : value, {
                  shouldValidate: true,
                })
              }
              options={[
                { value: "all", label: "Batch across this outlet" },
                ...locations.map((location) => ({
                  value: location.id,
                  label: `${location.code} · ${location.name} · ${formatPharmacyStorageAddress(location.storage_address)}`,
                })),
              ]}
              disabled={submitted !== null}
            />
          </Field>
          <Field
            id="safety-occurred"
            label={`Occurred at (${timeZone})`}
            error={form.formState.errors.occurred_at?.message}
          >
            <Input
              id="safety-occurred"
              ref={occurredInput}
              type="datetime-local"
              value={occurred}
              onChange={(event) => {
                setOccurred(event.target.value);
                const instant = zonedDateTimeISO(event.target.value, timeZone);
                form.setValue("occurred_at", instant ?? "", { shouldValidate: instant !== null });
                if (!instant)
                  form.setError("occurred_at", { type: "validate", message: invalidLocalTime });
              }}
              aria-invalid={!!form.formState.errors.occurred_at}
              aria-describedby={
                form.formState.errors.occurred_at ? "safety-occurred-error" : undefined
              }
            />
          </Field>
          {kind === "temperature_excursion" && (
            <Field
              id="safety-temperature"
              label="Measured temperature (°C)"
              error={form.formState.errors.measured_temperature_millicelsius?.message}
            >
              <Input
                id="safety-temperature"
                type="number"
                step="0.001"
                {...form.register("measured_temperature_millicelsius", {
                  setValueAs: millicelsius,
                })}
                aria-invalid={!!form.formState.errors.measured_temperature_millicelsius}
                aria-describedby={
                  form.formState.errors.measured_temperature_millicelsius
                    ? "safety-temperature-error"
                    : undefined
                }
              />
            </Field>
          )}
          <div className="min-w-0 sm:col-span-2">
            <Field
              id="safety-reason"
              label="Reason and observed evidence"
              error={form.formState.errors.reason?.message}
            >
              <textarea
                id="safety-reason"
                rows={3}
                maxLength={500}
                {...form.register("reason")}
                aria-invalid={!!form.formState.errors.reason}
                aria-describedby={
                  form.formState.errors.reason ? "safety-reason-error" : "safety-reason-help"
                }
                className="w-full rounded-md border bg-background p-3 text-sm focus-visible:outline-2 focus-visible:outline-(--hms-accent)"
              />
              <p id="safety-reason-help" className="text-xs font-normal text-muted-foreground">
                10–500 characters. The record includes your authenticated identity and recording
                time.
              </p>
            </Field>
          </div>
        </fieldset>
        {mutation.isError && (
          <div role="alert" className="space-y-2 text-sm text-destructive">
            <p>{mutation.error.message}</p>
            <p>
              {submitted
                ? "The submitted details are locked. Retry sends the same concern and cannot create a second record."
                : "Correct the rejected details and submit again."}
            </p>
          </div>
        )}
        {submitted ? (
          <Button
            type="button"
            disabled={mutation.isPending}
            onClick={() => sendConcern(submitted)}
          >
            {mutation.isPending ? "Recording concern…" : "Retry same concern"}
          </Button>
        ) : (
          <Button type="submit">Record safety concern</Button>
        )}
      </form>
    </Panel>
  );
}
