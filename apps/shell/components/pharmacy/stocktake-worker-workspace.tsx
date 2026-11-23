"use client";

import { useState } from "react";
import { Button } from "@tesserix/web";
import {
  listStocktakeWorkerLocations,
  stocktakeWorkerLocationsKey,
  useApiQuery,
  usePagedQuery,
  useTenantTransition,
  type StocktakeWorkerScope,
} from "@helivanta/api";
import { LoadMore, PageHeading, Panel, RequestFailure, useConfirmLeave } from "@helivanta/ui";
import { StocktakeContinuations } from "./stocktake-continuations";

export function StocktakeWorkerWorkspace() {
  const transition = useTenantTransition();
  const identity = useApiQuery<{ tenant_id: string; subject: string }>(
    ["iam", "me", "permissions"],
    "/iam/me/permissions",
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
    <WorkingLocations key={JSON.stringify(stocktakeWorkerLocationsKey(scope))} scope={scope} />
  );
}

function WorkingLocations({ scope }: { scope: StocktakeWorkerScope }) {
  const leave = useConfirmLeave();
  const [selected, setSelected] = useState<string | null>(null);
  const locations = usePagedQuery(
    [...stocktakeWorkerLocationsKey(scope)],
    (cursor, signal) => listStocktakeWorkerLocations(scope, cursor, signal),
    { retry: false, gcTime: 0 },
  );
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
    <div className="flex flex-col gap-5">
      <PageHeading
        title="My retained stocktakes"
        description="Choose a current reviewed duty to inspect retained work at its outlet."
      />
      <Panel aria-label="Stocktake working locations">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="font-semibold">Working locations</h2>
          <Button variant="outline" onClick={() => leave(locations.retry)}>
            Refresh working locations
          </Button>
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
                variant="outline"
                aria-pressed={selected === item.duty_id}
                onClick={() => leave(() => setSelected(item.duty_id))}
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
            choice.duty_id,
            choice.snapshot_version,
            choice.authority_epoch,
          ])}
          aria-label="Current outlet stocktakes"
        >
          <h2 className="mb-3 font-semibold">
            {choice.employer_name} · {choice.location_name}
          </h2>
          <p className="mb-3 text-sm">
            New stocktake work is not available. Retained status and recovery remain accessible.
          </p>
          <StocktakeContinuations
            scope={{
              ...scope,
              authority: "operating",
              pharmacyId: choice.employer_id,
              outletId: choice.location_id,
            }}
            timeZone={choice.time_zone}
            canCancel={choice.assigned_permissions.includes("pharmacy.stocktake.count")}
          />
        </section>
      )}
    </div>
  );
}
