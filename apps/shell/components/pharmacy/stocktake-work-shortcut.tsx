"use client";

import { useQuery } from "@tanstack/react-query";
import {
  capabilityForPage,
  listStocktakeWorkerLocations,
  listHospitalStocktakeScopes,
  hospitalStocktakeScopesKey,
  stocktakeWorkerLocationsKey,
  useApiQuery,
  useReleaseCapabilities,
  useTenantTransition,
  type PermissionsResponse,
  type StocktakeWorkerScope,
} from "@helivanta/api";
import { PanelLink } from "@helivanta/ui";

const destination = "/pharmacy/working-stocktakes";

export function StocktakeWorkShortcut() {
  const transition = useTenantTransition();
  const release = useReleaseCapabilities();
  const identity = useApiQuery<PermissionsResponse>(
    ["iam", "me", "permissions"],
    "/iam/me/permissions",
    { enabled: !transition },
  );
  const current = identity.data;
  if (
    transition ||
    identity.error ||
    identity.isFetching ||
    !current?.tenant_id ||
    !current.subject ||
    release.error ||
    !release.available(capabilityForPage(destination))
  )
    return null;
  const scope = { tenantId: current.tenant_id, subject: current.subject };
  return <CurrentWork key={JSON.stringify(stocktakeWorkerLocationsKey(scope))} scope={scope} />;
}

function CurrentWork({ scope }: { scope: StocktakeWorkerScope }) {
  const work = useQuery({
    queryKey: [...stocktakeWorkerLocationsKey(scope), "shortcut"],
    queryFn: ({ signal }) => listStocktakeWorkerLocations(scope, null, signal),
    retry: false,
    staleTime: 0,
    gcTime: 0,
  });
  const hospitals = useQuery({
    queryKey: [...hospitalStocktakeScopesKey(scope), "shortcut"],
    queryFn: ({ signal }) => listHospitalStocktakeScopes(scope, null, signal),
    retry: false,
    staleTime: 0,
    gcTime: 0,
  });
  const hasWork = [work, hospitals].some(
    (source) =>
      !source.error &&
      !source.isFetching &&
      source.data &&
      (source.data.data.length > 0 || source.data.page.has_more),
  );
  if (!hasWork) return null;
  return (
    <nav aria-label="My work" className="mb-6">
      <PanelLink href={destination}>
        <div className="font-medium">My stocktakes</div>
        <p className="mt-1 text-sm text-muted-foreground">
          Check current reviewed working locations and retained stocktake results.
        </p>
      </PanelLink>
    </nav>
  );
}
