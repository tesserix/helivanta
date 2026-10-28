"use client";

import { useCallback, useMemo, useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import { ShieldCheck } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { useListDetail, useReportState, type FormState } from "@/lib/use-list-detail";
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
  DetailHeader,
  Field,
  ListDetail,
  LoadMore,
  PageTabs,
  Pager,
  Panel,
  QueryState,
  RequestFailure,
  SelectControl,
  isModifiedClick,
  useConfirmLeave,
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
  const router = useRouter();
  const leave = useConfirmLeave();
  // The tabs sit above either tab's list, outside its pane's own link hold:
  // a half-typed concern is a regulated record and must not go silently (D5).
  // A modified click opens another tab and leaves this one as it is.
  const holdTabs = (event: React.MouseEvent) => {
    const link = (event.target as Element).closest("a");
    const href = link?.getAttribute("href");
    if (!href || isModifiedClick(event)) return;
    event.preventDefault();
    leave(() => router.push(href));
  };
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
    <div onClickCapture={holdTabs}>
      <PageTabs
        label="Safety concerns"
        current={tab}
        tabs={(Object.keys(TABS) as SafetyTab[]).map((key) => ({
          key,
          label: TABS[key],
          href: url.hrefFor({ tab: key }),
        }))}
      />
    </div>
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
  // A concern opens its batch (#1303 E5). Two concerns about one lot are one
  // stop for Previous / Next: the batch is the record, not the concern.
  const batches = [...new Set(recent.items.map((incident) => incident.batch_id))];
  const pane = useListDetail({
    rows: batches.map((id) => ({ id })),
    name: () => "this batch",
    fallbackName: "this batch",
    newName: "this batch",
    headingId: "recent-concerns-heading",
  });
  const { detail, paneRef, rowProps, openProps } = pane;
  const record = detail.openId ? (
    <BatchPane
      key={detail.openId}
      scope={scope}
      timeZone={timeZone}
      id={detail.openId}
      pane={pane}
    />
  ) : null;
  return (
    <div className="space-y-4" onClickCapture={pane.holdLinks}>
      <Panel flush aria-label="Concerns recorded in the last 30 days">
        {tabs}
        <div className="p-5">
          <h3 id="recent-concerns-heading" tabIndex={-1} className="font-semibold outline-none">
            Concerns recorded in the last 30 days
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">
            Every batch at this outlet, newest first. Open a concern to see its batch and record
            another.
          </p>
        </div>
      </Panel>
      <ListDetail
        label="Batch safety"
        backHref={detail.closeHref}
        backLabel="Back to concerns"
        detail={record && <div ref={paneRef}>{record}</div>}
        list={
          <Panel flush>
            <div className="p-5">
              <QueryState
                query={recent}
                label="recent concerns"
                empty={{ icon: ShieldCheck, title: "No concerns in the last 30 days" }}
              >
                {(rows) => (
                  <ul className="divide-y text-sm">
                    {rows.map((incident) => {
                      const kind =
                        concernTypes.find((item) => item.value === incident.kind)?.label ??
                        incident.kind;
                      const recorded = formatZonedDateTime(incident.recorded_at, timeZone);
                      return (
                        <li
                          key={incident.id}
                          {...rowProps(incident.batch_id)}
                          className={`flex flex-wrap items-start justify-between gap-3 py-3 ${incident.batch_id === detail.openId ? "bg-(--hms-accent-dim)" : ""}`}
                        >
                          <div className="min-w-0">
                            <p className="font-medium">{kind}</p>
                            <p className="mt-0.5 whitespace-pre-wrap break-words">
                              {incident.reason}
                            </p>
                            <p className="mt-0.5 text-muted-foreground">
                              Recorded <time dateTime={incident.recorded_at}>{recorded}</time> by{" "}
                              {incident.actor_subject}
                            </p>
                          </div>
                          <Button
                            variant="outline"
                            size="sm"
                            {...openProps(incident.batch_id)}
                            aria-label={`Open batch for ${kind} recorded ${recorded}`}
                            onClick={() => pane.openRow(incident.batch_id)}
                          >
                            Open batch
                          </Button>
                        </li>
                      );
                    })}
                  </ul>
                )}
              </QueryState>
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
        }
      />
    </div>
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
          <h3 id="batch-safety-heading" tabIndex={-1} className="font-semibold outline-none">
            Batch safety concerns
          </h3>
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

// The outlet's batches as a list (#1303 E5), loaded 100 at a time; a batch
// opens beside it with its history and the concern form.
function SafetyBatchPicker({
  scope,
  timeZone,
  batches,
  products,
  locations,
}: ZonedProps & {
  canRecord: boolean;
  batches: Collection<PharmacyBatch>;
  products: Collection<PharmacyProduct>;
  locations: Collection<PharmacyLocation>;
}) {
  const pane = useListDetail({
    rows: batches.items.map((batch) => ({ id: batch.id, name: batch.lot_number })),
    name: (row) => row.name,
    fallbackName: "this batch",
    newName: "this batch",
    headingId: "batch-safety-heading",
  });
  const { detail, paneRef, rowProps, openProps } = pane;
  const record = detail.openId ? (
    <BatchPane
      key={detail.openId}
      scope={scope}
      timeZone={timeZone}
      id={detail.openId}
      pane={pane}
    />
  ) : null;
  return (
    <div onClickCapture={pane.holdLinks}>
      <ListDetail
        label={pane.openName ? `Batch ${pane.openName}` : "Batch safety"}
        backHref={detail.closeHref}
        backLabel="Back to batches"
        detail={record && <div ref={paneRef}>{record}</div>}
        list={
          <div className="space-y-3">
            <ul aria-label="Batches at this outlet" className="divide-y rounded-md border text-sm">
              {batches.items.map((batch) => {
                const product = products.items.find((item) => item.id === batch.product_id);
                const label = product
                  ? `${product.sku} · ${product.brand_name}`
                  : `Product ${batch.product_id}`;
                return (
                  <li
                    key={batch.id}
                    {...rowProps(batch.id)}
                    className={`flex flex-wrap items-center justify-between gap-3 px-4 py-3 ${batch.id === detail.openId ? "bg-(--hms-accent-dim)" : ""}`}
                  >
                    <div className="min-w-0">
                      <p className="wrap-anywhere font-medium">{batch.lot_number}</p>
                      <p className="mt-0.5 wrap-anywhere text-muted-foreground">{label}</p>
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        Expires <time dateTime={batch.expires_on}>{batch.expires_on}</time>
                      </p>
                    </div>
                    <Button
                      variant="outline"
                      size="sm"
                      {...openProps(batch.id)}
                      aria-label={`Open lot ${batch.lot_number}`}
                      onClick={() => pane.openRow(batch.id)}
                    >
                      Open
                    </Button>
                  </li>
                );
              })}
            </ul>
            {(batches.hasMore || products.hasMore || locations.hasMore) && (
              <div className="grid gap-3 @[560px]/main:grid-cols-3">
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
          </div>
        }
      />
    </div>
  );
}

// The open batch, read by its id so a reload or a link reopens it, with its
// safety history and the form. Its own reads share the list's cache keys, so
// the Last 30 days tab can open it too. Another outlet's or tenant's batch is
// the same 404 as a missing one (#1303 D3).
function BatchPane({
  scope,
  timeZone,
  id,
  pane,
}: ZonedProps & { id: string; pane: ReturnType<typeof useListDetail> }) {
  const permissions = useApiQuery<{ tenant_id: string; data: string[] }>(
    pharmacyKey(scope, "batch-safety", "permissions"),
    "/iam/me/permissions",
    { request: requestFor(scope), gcTime: 0, refetchOnWindowFocus: false },
  );
  const granted = permissions.data?.tenant_id === scope.tenantId ? permissions.data.data : [];
  const canReadProducts = granted.includes("pharmacy.catalogue.read");
  const batch = useApiQuery<PharmacyBatch>(
    pharmacyKey(scope, "batch-safety", "batch", id),
    `${pharmacyRoot(scope)}/stock/batches/${id}`,
    { request: requestFor(scope), enabled: granted.includes("pharmacy.stock.read") },
  );
  const products = useApiPagedQuery<PharmacyProduct>(
    pharmacyKey(scope, "batch-safety", "products"),
    `${pharmacyRoot(scope)}/catalogue/products?limit=100`,
    { request: requestFor(scope), enabled: canReadProducts },
  );
  const locations = useApiPagedQuery<PharmacyLocation>(
    pharmacyKey(scope, "batch-safety", "locations"),
    `${pharmacyRoot(scope)}/stock/locations?limit=100`,
    { request: requestFor(scope), enabled: !!batch.data },
  );
  const row = batch.data;
  const product = row && products.items.find((item) => item.id === row.product_id);
  // Close is in the header whatever loaded: beside the list there is no back link.
  const header = (
    <DetailHeader
      title={row ? `Lot ${row.lot_number}` : "Batch"}
      subtitle={
        row
          ? `${product ? `${product.sku} · ${product.brand_name}` : `Product ${row.product_id}`} · Expires ${row.expires_on}`
          : undefined
      }
      noun="batch"
      previous={{ disabled: !pane.canStep(-1), onClick: () => pane.step(-1) }}
      next={{ disabled: !pane.canStep(1), onClick: () => pane.step(1) }}
      closeLabel="Close batch"
      onClose={pane.close}
    />
  );
  const body = permissions.isError ? (
    <RequestFailure
      message="Could not verify safety access. Previously loaded records are hidden."
      retry={permissions.refetch}
    />
  ) : permissions.isPending ? (
    <p role="status">Checking safety access…</p>
  ) : !granted.includes("pharmacy.stock.read") ? (
    <p role="alert">You do not have permission to view batch safety concerns.</p>
  ) : batch.error instanceof ApiError && batch.error.status === 404 ? (
    <p role="alert" className="text-sm">
      This record was not found, or you do not have access.
    </p>
  ) : batch.isError ? (
    <RequestFailure message="Could not load this batch." retry={batch.refetch} />
  ) : !row || locations.isPending ? (
    <p role="status">Loading batch…</p>
  ) : locations.isError ? (
    <RequestFailure message="Could not load safety locations." retry={locations.retry} />
  ) : (
    <SelectedBatchSafety
      scope={scope}
      timeZone={timeZone}
      batch={row}
      canRecord={granted.includes("pharmacy.stock.quarantine")}
      locations={locations.items}
      onStateChange={pane.formState("concern")}
    />
  );
  return (
    <Panel className="space-y-5">
      {header}
      {body}
    </Panel>
  );
}

function SelectedBatchSafety({
  scope,
  timeZone,
  batch,
  canRecord,
  locations,
  onStateChange,
}: ZonedProps & {
  batch: PharmacyBatch;
  canRecord: boolean;
  locations: PharmacyLocation[];
  onStateChange?: FormState;
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
        <ConcernForm
          scope={scope}
          timeZone={timeZone}
          batchId={batch.id}
          locations={locations}
          onStateChange={onStateChange}
        />
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
  onStateChange,
}: ZonedProps & {
  batchId: string;
  locations: PharmacyLocation[];
  /** A concern is a permanent regulated record: an unsaved one holds the batch open (D5). */
  onStateChange?: FormState;
}) {
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
  useReportState(onStateChange, form.formState.isDirty, mutation.isPending);
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
