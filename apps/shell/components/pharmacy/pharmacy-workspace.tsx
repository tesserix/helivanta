"use client";

import { useState } from "react";
import { z } from "zod";
import { Button } from "@tesserix/web";
import { ShieldCheck } from "lucide-react";
import {
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
import { Field, LoadMore, PageHeading, SelectControl, useZodForm } from "@helivanta/ui";
import { RequestFailure } from "../request-failure";
import { CataloguePanel } from "./catalogue-panel";
import { AccountsPanel } from "./accounts-panel";
import { StockPanel } from "./stock-panel";
import { PharmacyRegistrySetup } from "./pharmacy-registry-setup";
import { WorkspaceConnections } from "../workspace-connections";

type PharmacyIdentity = { subject: string; tenant_id: string };
export function PharmacyWorkspace({ view }: { view: "catalogue" | "stock" | "accounts" }) {
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
function TenantPharmacy({
  identity,
  view,
}: {
  identity: PharmacyIdentity;
  view: "catalogue" | "stock" | "accounts";
}) {
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
        description={
          view === "catalogue"
            ? "Product identities and their history for independent, chain or hospital-owned pharmacies."
            : view === "accounts"
              ? "Purchase costs, expenses and outstanding payments across your pharmacy inventory."
              : "Receive and reconcile quarantined stock in the selected pharmacy and outlet."
        }
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
      <div className="mb-6">
        <WorkspaceConnections current="pharmacy" />
      </div>
      {pharmacies.isError ? (
        <RequestFailure message="Could not load your pharmacies." retry={pharmacies.retry} />
      ) : pharmacies.isPending ? (
        <p role="status">Loading pharmacies…</p>
      ) : !pharmacy ? (
        <DirectoryEmpty selected={!!selection.pharmacyId} />
      ) : outlets.isError ? (
        <RequestFailure message="Could not load this pharmacy's outlets." retry={outlets.retry} />
      ) : outlets.isPending ? (
        <p role="status">Loading outlets…</p>
      ) : outlet ? (
        <OutletPharmacy
          key={`${identity.tenant_id}:${pharmacy.id}:${outlet.id}:${outlet.timezone}`}
          pharmacy={pharmacy}
          outlet={outlet}
          scope={{ tenantId: identity.tenant_id, pharmacyId: pharmacy.id, outletId: outlet.id }}
          subject={identity.subject}
          view={view}
        />
      ) : (
        <section className="rounded-2xl border bg-card p-8">
          <h3 className="font-semibold">
            {selection.outletId ? "Choose an outlet" : "No outlets available"}
          </h3>
          <p className="mt-2 text-sm text-muted-foreground">
            Choose an outlet from this pharmacy, load more below, or ask your administrator to
            register one.
          </p>
        </section>
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
      {can("pharmacy.access.manage") && (
        <PharmacyRegistrySetup tenantId={identity.tenant_id} pharmacy={pharmacy} />
      )}
    </>
  );
}
function DirectoryEmpty({ selected }: { selected: boolean }) {
  return (
    <section className="rounded-2xl border bg-card p-8">
      <h3 className="font-semibold">
        {selected ? "Choose a pharmacy" : "No pharmacies available"}
      </h3>
      <p className="mt-2 text-sm text-muted-foreground">
        {selected
          ? "Your saved pharmacy is not in the loaded directory. Choose one above or load more below."
          : "Ask your administrator to register a pharmacy and grant outlet access. A hospital is not required."}
      </p>
    </section>
  );
}
function OutletPharmacy({
  pharmacy,
  outlet,
  scope,
  subject,
  view,
}: {
  pharmacy: PharmacyOrganization;
  outlet: PharmacyOutlet;
  scope: PharmacyScope;
  subject: string;
  view: "catalogue" | "stock" | "accounts";
}) {
  const { can } = usePermissions();
  const access = useApiQuery<{ level: "reader" | "editor" }>(
    pharmacyKey(scope, "access"),
    `${pharmacyRoot(scope)}/access`,
    { gcTime: 0, refetchOnWindowFocus: true },
  );
  return (
    <>
      <div className="mb-6 flex items-start gap-3 rounded-xl border bg-card px-5 py-4">
        <ShieldCheck className="mt-0.5 size-5 shrink-0 text-(--hms-accent)" />
        <p className="text-sm leading-6">
          <strong>
            {pharmacy.name} · {outlet.name}
          </strong>
          <span className="block text-muted-foreground">Time zone: {outlet.timezone}</span>
          <span className="text-muted-foreground">
            Outlet access is explicit. Common ownership or a hospital affiliation does not share
            patient records or stock. Products and quarantine stock awaiting review are unavailable
            for dispensing.
          </span>
        </p>
      </div>
      {access.isPending ? (
        <p role="status">Checking pharmacy access…</p>
      ) : access.isError ? (
        <RequestFailure
          message="Pharmacy access is unavailable. Ask your administrator for access to this outlet, or retry if it was just updated."
          retry={access.refetch}
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
        />
      ) : (
        <StockPanel
          scope={scope}
          timeZone={outlet.timezone}
          writable={access.data?.level === "editor" && can("pharmacy.stock.receive")}
          correctable={access.data?.level === "editor" && can("pharmacy.stock.correct")}
        />
      )}
      {can("pharmacy.access.manage") && (
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
    <details className="mt-7 rounded-xl border bg-card p-5">
      <summary className="cursor-pointer text-sm font-medium">Outlet access administration</summary>
      <form
        noValidate
        className="mt-4 space-y-4"
        onSubmit={form.handleSubmit(() => grant.mutate())}
      >
        <p className="text-sm text-muted-foreground">
          Grant your signed-in account catalogue and receiving access at {outletName}. This does not
          approve medicines or confer hospital clinical access.
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
  );
}
