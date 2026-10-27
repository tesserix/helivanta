"use client";

import { Suspense, useEffect } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { z } from "zod";
import {
  ApiError,
  countryProfile,
  useApiQuery,
  useApiPagedQuery,
  usePermissions,
  pharmacyKey,
  pharmacyRoot,
  type PharmacyOrganization,
  type PharmacyOutlet,
  type PharmacyScope,
} from "@helivanta/api";
import {
  Field,
  GuardedLink as Link,
  LoadMore,
  PageHeading,
  Panel,
  RequestFailure,
  SelectControl,
  useConfirmLeave,
  useNavigationPending,
} from "@helivanta/ui";
import { CataloguePanel } from "./catalogue-panel";
import { OverviewTiles, WaitingTile } from "./overview-tiles";
import { AccountsPanel } from "./accounts-panel";
import { StockPanel } from "./stock-panel";
import { PharmacyRegistrySetup } from "./pharmacy-registry-setup";
import { StockRegister } from "./stock-register";
import { BatchSafety } from "./batch-safety";
import { OutletsSetup } from "./outlets-setup";
import { CounterSale } from "./counter-sale";

type PharmacyIdentity = { subject: string; tenant_id: string };
// The outlet-scoped pages (#1258 D2). Dispensing is tenant-wide and has its own pages.
type PharmacyView =
  "overview" | "counter" | "stock" | "receiving" | "safety" | "catalogue" | "accounts" | "setup";
const DESCRIPTIONS: Record<PharmacyView, string> = {
  overview: "What needs attention at this outlet, most urgent first.",
  counter: "Sell OTC medicines from sellable stock and print the GST invoice.",
  stock: "What each outlet holds, by product and batch, with reconciliation and export.",
  receiving: "Register manufacturer lots, receive them into quarantine and record movements.",
  safety: "Batches nearing expiry or under a safety concern, and their release decisions.",
  catalogue: "Product identities and their history for this pharmacy.",
  accounts: "Supplier bills, payments, adjustments and outstanding balances.",
  setup: "Pharmacies, outlets, hospital links and who can work at each outlet.",
};
export function PharmacyWorkspace({ view }: { view: PharmacyView }) {
  const identity = useApiQuery<PharmacyIdentity>(
    ["iam", "me", "permissions"],
    "/iam/me/permissions",
  );
  if (identity.isError)
    return (
      <RequestFailure message="Could not verify your active tenant." retry={identity.refetch} />
    );
  if (!identity.data) return <p role="status">Loading pharmacy workspace…</p>;
  // Suspense: an outlet page reads its starting filter from the URL (#576 D4).
  return (
    <Suspense fallback={<p role="status">Loading pharmacy workspace…</p>}>
      <TenantPharmacy key={identity.data.tenant_id} identity={identity.data} view={view} />
    </Suspense>
  );
}
const selectionSchema = z.object({
  pharmacyId: z.string().guid().or(z.literal("")),
  outletId: z.string().guid().or(z.literal("")),
});
function rememberPharmacySelection(storageKey: string, value: z.infer<typeof selectionSchema>) {
  try {
    sessionStorage.setItem(storageKey, JSON.stringify(value));
  } catch {
    /* Selection persistence is optional; every API request is authorised independently. */
  }
}

function TenantPharmacy({ identity, view }: { identity: PharmacyIdentity; view: PharmacyView }) {
  const { can } = usePermissions();
  const storageKey = `helivanta:pharmacy:scope:${identity.tenant_id}`;
  // Read as it is now, not once at mount: a rail link back to this same
  // page drops the address's scope and must land on the latest choice.
  const readSaved = () => {
    try {
      return selectionSchema.parse(JSON.parse(sessionStorage.getItem(storageKey) ?? "null"));
    } catch {
      return { pharmacyId: "", outletId: "" };
    }
  };
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const query = params.toString();
  const leave = useConfirmLeave();
  const { pending: navigationPending, isPending: isNavigationPending } = useNavigationPending();
  // The address first, so a shared link opens the outlet it was copied from
  // (#1303 E8); then this tab's saved choice; then the first loaded.
  const inAddress = (key: string) => {
    const value = params.get(key) ?? "";
    return z.string().guid().safeParse(value).success ? value : "";
  };
  const addressed = !!(inAddress("pharmacy") || inAddress("outlet"));
  const selection = addressed
    ? { pharmacyId: inAddress("pharmacy"), outletId: inAddress("outlet") }
    : readSaved();
  function select(value: z.infer<typeof selectionSchema>) {
    // A page, its cursor and an open record belong to one outlet's list:
    // another outlet's list opens at its first page with nothing open,
    // keeping the tab and filters (#576, #1303 E8).
    leave(() => {
      const kept = new URLSearchParams(params);
      // A location, product or lot id belongs to one outlet too: another
      // outlet would be asked for an id it does not have (#1303 E8).
      for (const key of ["page", "cursor", "open", "new", "at", "location", "product"])
        kept.delete(key);
      kept.set("pharmacy", value.pharmacyId);
      if (value.outletId) kept.set("outlet", value.outletId);
      else kept.delete("outlet");
      router.replace(`${pathname}?${kept}`);
      rememberPharmacySelection(storageKey, value);
    });
  }
  const pharmacies = useApiPagedQuery<PharmacyOrganization>(
    ["pharmacy", identity.tenant_id, "organizations"],
    "/pharmacy/pharmacies",
  );
  const pharmacy = selection.pharmacyId
    ? pharmacies.items.find((row) => row.id === selection.pharmacyId)
    : pharmacies.items[0];
  const outlets = useApiPagedQuery<PharmacyOutlet>(
    ["pharmacy", identity.tenant_id, pharmacy?.id, "outlets"],
    `/pharmacy/pharmacies/${pharmacy?.id ?? ""}/outlets`,
    { enabled: !!pharmacy },
  );
  // An outlet asked for and not loaded stays unresolved: "Choose an outlet",
  // never another outlet in its place. Fail closed, so an open record from
  // one outlet is never looked up under another (#1303 E8).
  const outlet = selection.outletId
    ? outlets.items.find((row) => row.id === selection.outletId)
    : outlets.items[0];
  // What resolved is written into the address, so a copied address always
  // carries it, and saved as this tab's working outlet.
  const pharmacyId = pharmacy?.id ?? "";
  const outletId = outlet?.id ?? "";
  useEffect(() => {
    if (navigationPending || isNavigationPending() || !pharmacyId || !outletId) return;
    const current = new URL(window.location.href);
    if (current.pathname !== pathname || current.searchParams.toString() !== query) return;
    rememberPharmacySelection(storageKey, { pharmacyId, outletId });
    if (
      current.searchParams.get("pharmacy") === pharmacyId &&
      current.searchParams.get("outlet") === outletId
    )
      return;
    current.searchParams.set("pharmacy", pharmacyId);
    current.searchParams.set("outlet", outletId);
    window.history.replaceState(null, "", `${current.pathname}${current.search}${current.hash}`);
  }, [pharmacyId, outletId, pathname, query, storageKey, navigationPending, isNavigationPending]);
  return (
    <>
      <PageHeading
        description={DESCRIPTIONS[view]}
        actions={
          <div className="grid w-full gap-4 sm:w-xl sm:grid-cols-2">
            {pharmacies.items.length > 0 && (
              <div data-picker>
                <Field id="pharmacy-organization" label="Pharmacy">
                  <SelectControl
                    id="pharmacy-organization"
                    value={pharmacy?.id ?? ""}
                    onValueChange={(pharmacyId) => select({ pharmacyId, outletId: "" })}
                    options={pharmacies.items.map((row) => ({ value: row.id, label: row.name }))}
                  />
                </Field>
                <LoadMore
                  label="Load more pharmacies"
                  hasMore={pharmacies.hasMore}
                  isLoading={pharmacies.isFetchingMore}
                  onClick={pharmacies.loadMore}
                />
              </div>
            )}
            {/* On Outlets & access the list below is the outlet chooser (#1303 E6). */}
            {outlets.items.length > 0 && view !== "setup" && (
              <div data-picker>
                <Field id="pharmacy-outlet" label="Outlet">
                  <SelectControl
                    id="pharmacy-outlet"
                    value={outlet?.id ?? ""}
                    onValueChange={(outletId) =>
                      select({ pharmacyId: pharmacy?.id ?? "", outletId })
                    }
                    options={outlets.items.map((row) => ({ value: row.id, label: row.name }))}
                  />
                </Field>
                <LoadMore
                  label="Load more outlets"
                  hasMore={outlets.hasMore}
                  isLoading={outlets.isFetchingMore}
                  onClick={outlets.loadMore}
                />
              </div>
            )}
          </div>
        }
      />
      {/* Hospital-wide, so it shows whatever the outlet state is (#576 D2). */}
      {view === "overview" && can("pharmacy.dispense.read") && (
        <div className="mb-4">
          <WaitingTile />
        </div>
      )}
      {pharmacies.isError ? (
        <RequestFailure message="Could not load your pharmacies." retry={pharmacies.retry} />
      ) : pharmacies.isPending ? (
        <p role="status">Loading pharmacies…</p>
      ) : !pharmacy ? (
        <DirectoryEmpty selected={!!selection.pharmacyId} setup={view === "setup"} />
      ) : view === "setup" ? (
        <OutletsSetup
          tenantId={identity.tenant_id}
          pharmacy={pharmacy}
          subject={identity.subject}
          outlets={outlets}
        />
      ) : outlets.isError ? (
        <RequestFailure message="Could not load this pharmacy's outlets." retry={outlets.retry} />
      ) : outlets.isPending ? (
        <p role="status">Loading outlets…</p>
      ) : outlet ? (
        <OutletPharmacy
          key={`${identity.tenant_id}:${pharmacy.id}:${outlet.id}:${outlet.timezone}`}
          outlet={outlet}
          scope={{ tenantId: identity.tenant_id, pharmacyId: pharmacy.id, outletId: outlet.id }}
          view={view}
        />
      ) : (
        <Panel>
          <h3 className="font-semibold">
            {selection.outletId ? "Choose an outlet" : "No outlets available"}
          </h3>
          <p className="mt-2 text-sm text-muted-foreground">
            Choose an outlet from this pharmacy, load more above, or ask your administrator to
            register one.
          </p>
        </Panel>
      )}
      {/* No pharmacy yet: register the first. With one, the setup page has its own. */}
      {/* Only once the directory is known to be empty: mounted while it
          loads, the form would be replaced, and anything typed lost. */}
      {view === "setup" &&
        !pharmacies.isPending &&
        !pharmacies.isError &&
        !pharmacy &&
        can("pharmacy.access.manage") && <PharmacyRegistrySetup tenantId={identity.tenant_id} />}
    </>
  );
}
function DirectoryEmpty({ selected, setup }: { selected: boolean; setup: boolean }) {
  const [title, hint] = selected
    ? [
        "Choose a pharmacy",
        "Your saved pharmacy is not in the loaded directory. Choose one or load more above.",
      ]
    : setup
      ? [
          "No pharmacy registered yet",
          "Register the first pharmacy below. A hospital is not required.",
        ]
      : [
          "No pharmacies available",
          "Ask your administrator to register a pharmacy and grant outlet access. A hospital is not required.",
        ];
  return (
    <Panel>
      <h3 className="font-semibold">{title}</h3>
      <p className="mt-2 text-sm text-muted-foreground">{hint}</p>
    </Panel>
  );
}
// Setup never renders an outlet's pages (#1303 E6), so this is always another
// page; a grant manager is linked straight to the outlet's own pane.
function NoOutletAccess({ canGrant, scope }: { canGrant: boolean; scope: PharmacyScope }) {
  return (
    <Panel>
      <h3 className="font-semibold">You don't have access to this outlet yet</h3>
      <p className="mt-2 text-sm text-muted-foreground">
        {canGrant
          ? "Outlet access is granted per person, administrators included."
          : "Ask your administrator to grant you access to this outlet."}
      </p>
      {canGrant && (
        <Link
          href={`/pharmacy/setup?${new URLSearchParams({ pharmacy: scope.pharmacyId, outlet: scope.outletId, open: scope.outletId })}`}
          className="mt-3 inline-flex text-sm font-medium text-(--hms-accent) hover:underline"
        >
          Grant access in Setup
        </Link>
      )}
    </Panel>
  );
}
function OutletPharmacy({
  outlet,
  scope,
  view,
}: {
  outlet: PharmacyOutlet;
  scope: PharmacyScope;
  view: PharmacyView;
}) {
  const { can } = usePermissions();
  const access = useApiQuery<{ level: "reader" | "editor" }>(
    pharmacyKey(scope, "access"),
    `${pharmacyRoot(scope)}/access`,
    { gcTime: 0, refetchOnWindowFocus: true },
  );
  return (
    <>
      <p className="mb-4 text-xs text-muted-foreground">Times are in {outlet.timezone}.</p>
      {access.isPending ? (
        <p role="status">Checking pharmacy access…</p>
      ) : access.error instanceof ApiError && access.error.status === 404 ? (
        // 404 is the server's settled "no grant here"; retrying cannot change it (#1285).
        <NoOutletAccess canGrant={can("pharmacy.access.manage")} scope={scope} />
      ) : access.isError ? (
        <RequestFailure
          message="Could not check your access to this outlet. Try again in a moment."
          retry={access.refetch}
        />
      ) : view === "setup" ? null : view === "overview" ? (
        <OverviewTiles scope={scope} outlet={outlet} />
      ) : view === "counter" ? (
        <CounterSale
          scope={scope}
          canSell={access.data?.level === "editor" && can("pharmacy.sale.create")}
          canManageProfile={access.data?.level === "editor" && can("pharmacy.tax.manage")}
        />
      ) : view === "accounts" ? (
        <AccountsPanel
          scope={scope}
          timeZone={outlet.timezone}
          defaultCurrency={countryProfile(outlet.country_code)?.currency ?? ""}
          writable={access.data?.level === "editor"}
        />
      ) : view === "catalogue" ? (
        <CataloguePanel
          scope={scope}
          timeZone={outlet.timezone}
          defaultCurrency={countryProfile(outlet.country_code)?.currency ?? ""}
          writable={access.data?.level === "editor" && can("pharmacy.catalogue.write")}
          countryCode={outlet.country_code}
          canManageTax={access.data?.level === "editor" && can("pharmacy.tax.manage")}
        />
      ) : view === "stock" ? (
        <StockRegister scope={scope} timeZone={outlet.timezone} />
      ) : view === "safety" ? (
        <BatchSafety scope={scope} timeZone={outlet.timezone} />
      ) : (
        <StockPanel
          scope={scope}
          timeZone={outlet.timezone}
          writable={access.data?.level === "editor" && can("pharmacy.stock.receive")}
          correctable={access.data?.level === "editor" && can("pharmacy.stock.correct")}
          releasable={access.data?.level === "editor" && can("pharmacy.stock.release")}
        />
      )}
    </>
  );
}
