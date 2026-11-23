"use client";

import { useState } from "react";
import { Button } from "@tesserix/web";
import {
  listStocktakeWorkerLocations,
  stocktakeWorkerLocationsKey,
  listHospitalStocktakeScopes,
  hospitalStocktakeScopesKey,
  useReleaseCapabilities,
  useApiQuery,
  usePagedQuery,
  useTenantTransition,
  type StocktakeWorkerScope,
  type StocktakeWorkerLocation,
} from "@helivanta/api";
import {
  LoadMore,
  PageHeading,
  Panel,
  RefreshButton,
  RequestFailure,
  useConfirmLeave,
} from "@helivanta/ui";
import { HospitalStocktakeLocations } from "./hospital-stocktake-working-locations";
import { StocktakeOutletWork } from "./stocktake-outlet-work";

export function StocktakeWorkerWorkspace() {
  const transition = useTenantTransition();
  const release = useReleaseCapabilities();
  const identity = useApiQuery<{ tenant_id: string; subject: string }>(
    ["iam", "me", "permissions"],
    "/iam/me/permissions",
    { enabled: !transition },
  );
  if (transition) return <p role="status">Changing workspace…</p>;
  if (identity.error)
    return (
      <RequestFailure
        message="Could not verify your active account."
        error={identity.error}
        retry={() => void identity.refetch()}
      />
    );
  if (!identity.data || identity.isFetching)
    return <p role="status">Checking stocktake account…</p>;
  const scope = { tenantId: identity.data.tenant_id, subject: identity.data.subject };
  return (
    <WorkingLocations
      key={JSON.stringify(stocktakeWorkerLocationsKey(scope))}
      scope={scope}
      released={!release.error && release.available("pharmacy_stocktake")}
    />
  );
}

function WorkingLocations({ scope, released }: { scope: StocktakeWorkerScope; released: boolean }) {
  const leave = useConfirmLeave();
  const [selected, setSelected] = useState<{
    authority: "operating" | "hospital";
    id: string;
  } | null>(null);
  const locations = usePagedQuery(
    [...stocktakeWorkerLocationsKey(scope)],
    (cursor, signal) => listStocktakeWorkerLocations(scope, cursor, signal),
    { retry: false, gcTime: 0 },
  );
  const hospitals = usePagedQuery(
    [...hospitalStocktakeScopesKey(scope)],
    (cursor, signal) => listHospitalStocktakeScopes(scope, cursor, signal),
    { retry: false, gcTime: 0 },
  );
  const choose = (authority: "operating" | "hospital", id: string) => {
    if (selected?.authority === authority && selected.id === id) return;
    leave(() => setSelected({ authority, id }));
  };
  const showLocations =
    Boolean(locations.error) ||
    locations.isPending ||
    locations.isRefreshing ||
    locations.hasMore ||
    locations.items.length > 0;
  const showHospitals =
    Boolean(hospitals.error) ||
    hospitals.isPending ||
    hospitals.isRefreshing ||
    hospitals.hasMore ||
    hospitals.items.length > 0;
  return (
    <div className="flex flex-col gap-5">
      <PageHeading
        title="My stocktakes"
        description="Choose a current hospital department or reviewed pharmacy duty."
      />
      {!showLocations && !showHospitals && (
        <Panel>
          <p>No current reviewed stocktake assignment is available to this account.</p>
        </Panel>
      )}
      {showHospitals && (
        <HospitalStocktakeLocations
          scope={scope}
          released={released}
          locations={hospitals}
          selected={selected?.authority === "hospital" ? selected.id : null}
          onSelect={(id) => choose("hospital", id)}
        />
      )}
      {showLocations && (
        <IndependentLocations
          scope={scope}
          released={released}
          locations={locations}
          selected={selected?.authority === "operating" ? selected.id : null}
          onSelect={(id) => choose("operating", id)}
        />
      )}
    </div>
  );
}

function IndependentLocations({
  scope,
  released,
  locations,
  selected,
  onSelect,
}: {
  scope: StocktakeWorkerScope;
  released: boolean;
  locations: ReturnType<typeof usePagedQuery<StocktakeWorkerLocation>>;
  selected: string | null;
  onSelect: (id: string) => void;
}) {
  const leave = useConfirmLeave();
  if (locations.error)
    return (
      <RequestFailure
        message="Your current stocktake locations could not be verified."
        error={locations.error}
        retry={locations.retry}
      />
    );
  if (locations.isPending || locations.isRefreshing)
    return <p role="status">Checking current working locations…</p>;
  const choice = locations.items.find((item) => item.duty_id === selected);
  return (
    <>
      <Panel aria-label="Stocktake working locations">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="font-semibold">Independent pharmacy duties</h2>
          <RefreshButton
            refresh={() => (selected ? leave(locations.retry) : locations.retry())}
            error={locations.error}
            busy={locations.isRefreshing}
          >
            Refresh working locations
          </RefreshButton>
        </div>
        {locations.items.length === 0 && (
          <p className="mt-3 text-sm">
            {locations.hasMore
              ? "No current working location on this page. Continue to check further duties."
              : "No current reviewed stocktake duty is available to this account."}
          </p>
        )}
        <ul className="mt-3 space-y-2">
          {locations.items.map((item) => (
            <li key={item.duty_id}>
              <Button
                className="h-auto min-h-10 w-full justify-start whitespace-normal text-left"
                variant="outline"
                aria-pressed={selected === item.duty_id}
                onClick={() => onSelect(item.duty_id)}
              >
                {item.employer_name} · {item.location_name} · {item.location_code} ·{" "}
                {item.template_label}
              </Button>
            </li>
          ))}
        </ul>
        <LoadMore
          hasMore={locations.hasMore}
          isLoading={locations.isFetchingMore}
          onClick={locations.loadMore}
          label="Load more working locations"
        />
      </Panel>
      {choice && (
        <section
          key={JSON.stringify([
            scope.tenantId,
            scope.subject,
            choice.employer_id,
            choice.location_id,
            choice.duty_id,
            choice.snapshot_version,
            choice.authority_epoch,
          ])}
          aria-label="Current outlet stocktakes"
        >
          <h2 className="mb-3 font-semibold">
            {choice.employer_name} · {choice.location_name}
          </h2>
          <StocktakeOutletWork
            scope={{
              ...scope,
              authority: "operating",
              pharmacyId: choice.employer_id,
              outletId: choice.location_id,
            }}
            released={released}
            timeZone={choice.time_zone}
            canCount={choice.assigned_permissions.includes("pharmacy.stocktake.count")}
            canReview={choice.assigned_permissions.includes("pharmacy.stocktake.review")}
          />
        </section>
      )}
    </>
  );
}
