"use client";

import { Suspense, useEffect } from "react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { z } from "zod";
import { Button } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  countryProfile,
  zonedDateTimeInput,
  useApiQuery,
  useApiPagedQuery,
  useApiMutation,
  usePermissions,
  pharmacyKey,
  pharmacyRoot,
  type PharmacyOrganization,
  type PharmacyOutlet,
  type PharmacyScope,
} from "@helivanta/api";
import {
  Field,
  LoadMore,
  PageHeading,
  Panel,
  RequestFailure,
  SelectControl,
  useConfirmLeave,
  useZodForm,
} from "@helivanta/ui";
import { CataloguePanel } from "./catalogue-panel";
import { OverviewTiles, WaitingTile } from "./overview-tiles";
import { AccountsPanel } from "./accounts-panel";
import { StockPanel } from "./stock-panel";
import { PharmacyRegistrySetup } from "./pharmacy-registry-setup";
import { TaxRegistrationPanel } from "./tax-registration-panel";
import { StockRegister } from "./stock-register";
import { BatchSafety } from "./batch-safety";

type PharmacyIdentity = { subject: string; tenant_id: string };
// The outlet-scoped pages (#1258 D2). Dispensing is tenant-wide and has its own pages.
type PharmacyView =
  "overview" | "stock" | "receiving" | "safety" | "catalogue" | "accounts" | "setup";
const DESCRIPTIONS: Record<PharmacyView, string> = {
  overview: "What needs attention at this outlet, most urgent first.",
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
  const leave = useConfirmLeave();
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
  function remember(value: z.infer<typeof selectionSchema>) {
    try {
      sessionStorage.setItem(storageKey, JSON.stringify(value));
    } catch {
      /* Selection persistence is optional; every API request is authorised independently. */
    }
  }
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
      remember(value);
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
  const resolved = pharmacy && outlet ? `${pharmacy.id}:${outlet.id}` : "";
  useEffect(() => {
    if (!pharmacy || !outlet) return;
    remember({ pharmacyId: pharmacy.id, outletId: outlet.id });
    if (params.get("pharmacy") === pharmacy.id && params.get("outlet") === outlet.id) return;
    const kept = new URLSearchParams(params);
    kept.set("pharmacy", pharmacy.id);
    kept.set("outlet", outlet.id);
    router.replace(`${pathname}?${kept}`);
    // The address's own scope too: a rail link to the same outlet drops it,
    // and it must be put back. The comparison above ends the loop.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [resolved, params.get("pharmacy"), params.get("outlet")]);
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
            {outlets.items.length > 0 && (
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
      ) : outlets.isError ? (
        <RequestFailure message="Could not load this pharmacy's outlets." retry={outlets.retry} />
      ) : outlets.isPending ? (
        <p role="status">Loading outlets…</p>
      ) : outlet ? (
        <OutletPharmacy
          key={`${identity.tenant_id}:${pharmacy.id}:${outlet.id}:${outlet.timezone}`}
          outlet={outlet}
          scope={{ tenantId: identity.tenant_id, pharmacyId: pharmacy.id, outletId: outlet.id }}
          subject={identity.subject}
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
      {view === "setup" && can("pharmacy.access.manage") && (
        <PharmacyRegistrySetup tenantId={identity.tenant_id} pharmacy={pharmacy} />
      )}
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
function NoOutletAccess({ setup, canGrant }: { setup: boolean; canGrant: boolean }) {
  return (
    <Panel>
      <h3 className="font-semibold">You don't have access to this outlet yet</h3>
      <p className="mt-2 text-sm text-muted-foreground">
        {!canGrant
          ? "Ask your administrator to grant you access to this outlet."
          : setup
            ? "Grant yourself access with the form below."
            : "Outlet access is granted per person, administrators included."}
      </p>
      {canGrant && !setup && (
        <Link
          href="/pharmacy/setup"
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
  subject,
  view,
}: {
  outlet: PharmacyOutlet;
  scope: PharmacyScope;
  subject: string;
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
        <NoOutletAccess setup={view === "setup"} canGrant={can("pharmacy.access.manage")} />
      ) : access.isError ? (
        <RequestFailure
          message="Could not check your access to this outlet. Try again in a moment."
          retry={access.refetch}
        />
      ) : view === "setup" ? null : view === "overview" ? (
        <OverviewTiles scope={scope} outlet={outlet} />
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
        />
      )}
      {view === "setup" && can("pharmacy.access.manage") && (
        <AccessSetup scope={scope} subject={subject} outletName={outlet.name} />
      )}
      {view === "setup" && can("pharmacy.tax.manage") && (
        <TaxRegistrationPanel
          scope={scope}
          outlet={outlet}
          today={zonedDateTimeInput(new Date().toISOString(), outlet.timezone).slice(0, 10)}
        />
      )}
    </>
  );
}
const accessSchema = z.object({
  confirmed: z.boolean().refine(Boolean, "Confirm the outlet before granting access."),
});
function AccessSetup({
  scope,
  subject,
  outletName,
}: {
  scope: PharmacyScope;
  subject: string;
  outletName: string;
}) {
  const form = useZodForm(accessSchema, { confirmed: false });
  const grant = useApiMutation(
    () =>
      apiFetch(`${pharmacyRoot(scope)}/access/${encodeURIComponent(subject)}`, {
        method: "PUT",
        body: JSON.stringify({ level: "editor" }),
      }),
    {
      successToast: "Pharmacy access granted",
      invalidate: [pharmacyKey(scope, "access")],
      suppressErrorToast: true,
    },
  );
  return (
    <Panel className="mt-7">
      <details>
        <summary className="cursor-pointer text-sm font-medium">
          Outlet access administration
        </summary>
        <form
          noValidate
          className="mt-4 space-y-4"
          onSubmit={form.handleSubmit(() => grant.mutate())}
        >
          <p className="text-sm text-muted-foreground">
            Grant your signed-in account catalogue and receiving access at {outletName}. This does
            not approve medicines or confer hospital clinical access.
          </p>
          <label className="flex items-start gap-3 text-sm">
            <input type="checkbox" className="mt-1" {...form.register("confirmed")} />
            Grant my account pharmacy access to this outlet
          </label>
          {form.formState.errors.confirmed && (
            <p role="alert" className="text-sm text-destructive">
              {form.formState.errors.confirmed.message}
            </p>
          )}
          {grant.isError && (
            <p role="alert" className="text-sm text-destructive">
              {grant.error.message}
            </p>
          )}
          <Button type="submit" disabled={grant.isPending || !subject}>
            Grant pharmacy access
          </Button>
        </form>
      </details>
    </Panel>
  );
}
