"use client";

import { useState } from "react";
import { Button } from "@tesserix/web";
import {
  listHospitalStocktakeOutlets,
  hospitalStocktakeOutletsKey,
  usePagedQuery,
  type StocktakeWorkerScope,
  type HospitalStocktakeScopeChoice,
} from "@helivanta/api";
import { LoadMore, Panel, RefreshButton, RequestFailure, useConfirmLeave } from "@helivanta/ui";
import { StocktakeOutletWork } from "./stocktake-outlet-work";

type Props = {
  scope: StocktakeWorkerScope;
  released: boolean;
  locations: ReturnType<typeof usePagedQuery<HospitalStocktakeScopeChoice>>;
  selected: string | null;
  onSelect: (id: string) => void;
};
export function HospitalStocktakeLocations({
  scope,
  released,
  locations,
  selected,
  onSelect,
}: Props) {
  const leave = useConfirmLeave();
  if (locations.error)
    return (
      <RequestFailure
        message="Current hospital departments could not be verified."
        error={locations.error}
        retry={locations.retry}
      />
    );
  if (locations.isPending || locations.isRefreshing)
    return <p role="status">Checking current hospital departments…</p>;
  const assignment = locations.items.find((item) => item.id === selected);
  return (
    <>
      <Panel aria-label="Hospital stocktake departments">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="font-semibold">Hospital departments</h2>
          <RefreshButton
            refresh={() => (selected ? leave(locations.retry) : locations.retry())}
            error={locations.error}
            busy={locations.isRefreshing}
          >
            Refresh hospital departments
          </RefreshButton>
        </div>
        {locations.items.length === 0 && (
          <p className="mt-3 text-sm">
            {locations.hasMore
              ? "No current hospital department on this page. Continue to check further assignments."
              : "No current hospital stocktake assignment is available."}
          </p>
        )}
        <ul className="mt-3 space-y-2">
          {locations.items.map((item) => (
            <li key={item.id}>
              <Button
                className="h-auto min-h-10 w-full justify-start whitespace-normal text-left"
                variant="outline"
                aria-pressed={selected === item.id}
                onClick={() => onSelect(item.id)}
              >
                {item.hospital_name} · {item.department_name} · {item.template_label}
              </Button>
            </li>
          ))}
        </ul>
        <LoadMore
          hasMore={locations.hasMore}
          isLoading={locations.isFetchingMore}
          onClick={locations.loadMore}
          label="Load more hospital departments"
        />
      </Panel>
      {assignment && (
        <HospitalOutlets
          key={JSON.stringify([
            assignment.id,
            assignment.snapshot_version,
            assignment.authority_epoch,
          ])}
          scope={scope}
          assignment={assignment}
          released={released}
        />
      )}
    </>
  );
}
function HospitalOutlets({
  scope,
  assignment,
  released,
}: {
  scope: StocktakeWorkerScope;
  assignment: HospitalStocktakeScopeChoice;
  released: boolean;
}) {
  const leave = useConfirmLeave();
  const [selected, setSelected] = useState<string | null>(null);
  const account = {
    ...scope,
    hospitalId: assignment.hospital_id,
    departmentId: assignment.department_id,
  };
  const outlets = usePagedQuery(
    [
      ...hospitalStocktakeOutletsKey(account),
      assignment.id,
      assignment.snapshot_version,
      assignment.authority_epoch,
    ],
    (cursor, signal) => listHospitalStocktakeOutlets(account, cursor, signal),
    { retry: false, gcTime: 0 },
  );
  if (outlets.error)
    return (
      <RequestFailure
        message="Current hospital outlets could not be verified."
        error={outlets.error}
        retry={outlets.retry}
      />
    );
  if (outlets.isPending || outlets.isRefreshing)
    return <p role="status">Checking approved hospital outlets…</p>;
  const choice = outlets.items.find((item) => item.binding_id === selected);
  return (
    <>
      <Panel aria-label="Approved hospital pharmacy outlets">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 className="font-semibold">Approved pharmacy outlets</h2>
          <RefreshButton
            refresh={() => (selected ? leave(outlets.retry) : outlets.retry())}
            error={outlets.error}
            busy={outlets.isRefreshing}
          >
            Refresh approved outlets
          </RefreshButton>
        </div>
        {outlets.items.length === 0 && (
          <p className="mt-3 text-sm">
            {outlets.hasMore
              ? "No current approved outlet on this page. Continue to check further outlets."
              : "No current approved pharmacy outlet is available for this department."}
          </p>
        )}
        <ul className="mt-3 space-y-2">
          {outlets.items.map((item) => (
            <li key={item.binding_id}>
              <Button
                className="h-auto min-h-10 w-full justify-start whitespace-normal text-left"
                variant="outline"
                aria-pressed={selected === item.binding_id}
                onClick={() => {
                  if (selected !== item.binding_id) leave(() => setSelected(item.binding_id));
                }}
              >
                {item.pharmacy_name} · {item.outlet_name} · {item.outlet_code}
              </Button>
            </li>
          ))}
        </ul>
        <LoadMore
          hasMore={outlets.hasMore}
          isLoading={outlets.isFetchingMore}
          onClick={outlets.loadMore}
          label="Load more approved hospital outlets"
        />
      </Panel>
      {choice && (
        <section
          aria-label="Current hospital outlet stocktakes"
          key={JSON.stringify([
            choice.binding_id,
            choice.binding_version,
            choice.department_revision_id,
          ])}
        >
          <h2 className="mb-3 font-semibold">
            {choice.pharmacy_name} · {choice.outlet_name}
          </h2>
          <StocktakeOutletWork
            scope={{
              ...account,
              authority: "hospital",
              pharmacyId: choice.pharmacy_id,
              outletId: choice.outlet_id,
            }}
            released={released}
            timeZone={choice.time_zone}
            canCount={assignment.assigned_permissions.includes("pharmacy.stocktake.count")}
            canReview={assignment.assigned_permissions.includes("pharmacy.stocktake.review")}
          />
        </section>
      )}
    </>
  );
}
