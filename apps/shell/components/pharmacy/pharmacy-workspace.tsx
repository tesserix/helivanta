"use client";

import { useState } from "react";
import Link from "next/link";
import { z } from "zod";
import { Button } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  countryProfile,
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
  useZodForm,
} from "@helivanta/ui";
import { CataloguePanel } from "./catalogue-panel";
import { AccountsPanel } from "./accounts-panel";
import { StockPanel } from "./stock-panel";
import { PharmacyRegistrySetup } from "./pharmacy-registry-setup";
import { StockRegister } from "./stock-register";
import { BatchSafety } from "./batch-safety";

type PharmacyIdentity = { subject: string; tenant_id: string };
// The outlet-scoped pages (#1258 D2). Dispensing is tenant-wide and has its own pages.
type PharmacyView = "stock" | "receiving" | "safety" | "catalogue" | "accounts" | "setup";
const DESCRIPTIONS: Record<PharmacyView, string> = {
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
  return <TenantPharmacy key={identity.data.tenant_id} identity={identity.data} view={view} />;
}
const selectionSchema = z.object({
  pharmacyId: z.string().uuid().or(z.literal("")),
  outletId: z.string().uuid().or(z.literal("")),
});
function TenantPharmacy({ identity, view }: { identity: PharmacyIdentity; view: PharmacyView }) {
  const { can } = usePermissions();
  const storageKey = `helivanta:pharmacy:scope:${identity.tenant_id}`;
  const [selection, setSelection] = useState(() => {
    try {
      return selectionSchema.parse(JSON.parse(sessionStorage.getItem(storageKey) ?? "null"));
    } catch {
      return { pharmacyId: "", outletId: "" };
    }
  });
  function select(value: z.infer<typeof selectionSchema>) {
    setSelection(value);
    try {
      sessionStorage.setItem(storageKey, JSON.stringify(value));
    } catch {
      /* Selection persistence is optional; every API request is authorised independently. */
    }
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
  const outlet = selection.outletId
    ? outlets.items.find((row) => row.id === selection.outletId)
    : outlets.items[0];
  return (
    <>
      <PageHeading
        description={DESCRIPTIONS[view]}
        actions={
          <div className="grid w-full gap-4 sm:w-xl sm:grid-cols-2">
            {pharmacies.items.length > 0 && (
              <Field id="pharmacy-organization" label="Pharmacy">
                <SelectControl
                  id="pharmacy-organization"
                  value={pharmacy?.id ?? ""}
                  onValueChange={(pharmacyId) => select({ pharmacyId, outletId: "" })}
                  options={pharmacies.items.map((row) => ({ value: row.id, label: row.name }))}
                />
              </Field>
            )}
            {outlets.items.length > 0 && (
              <Field id="pharmacy-outlet" label="Outlet">
                <SelectControl
                  id="pharmacy-outlet"
                  value={outlet?.id ?? ""}
                  onValueChange={(outletId) => select({ pharmacyId: pharmacy?.id ?? "", outletId })}
                  options={outlets.items.map((row) => ({ value: row.id, label: row.name }))}
                />
              </Field>
            )}
          </div>
        }
      />
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
            Choose an outlet from this pharmacy, load more below, or ask your administrator to
            register one.
          </p>
        </Panel>
      )}
      <LoadMore
        hasMore={pharmacies.hasMore}
        isLoading={pharmacies.isFetchingMore}
        onClick={pharmacies.loadMore}
      />
      <LoadMore
        hasMore={outlets.hasMore}
        isLoading={outlets.isFetchingMore}
        onClick={outlets.loadMore}
      />
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
        "Your saved pharmacy is not in the loaded directory. Choose one above or load more below.",
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
      ) : view === "setup" ? null : view === "accounts" ? (
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
