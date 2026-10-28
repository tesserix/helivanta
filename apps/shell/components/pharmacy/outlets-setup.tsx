"use client";

import { useEffect, useRef } from "react";
import { Plus, Store } from "lucide-react";
import { z } from "zod";
import { Button } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  countryProfile,
  pharmacyKey,
  pharmacyRoot,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  zonedDateTimeInput,
  type PharmacyOrganization,
  type PharmacyOutlet,
  type PharmacyScope,
} from "@helivanta/api";
import {
  DetailHeader,
  ListDetail,
  LoadMore,
  Panel,
  RequestFailure,
  useZodForm,
} from "@helivanta/ui";
import { useListDetail, useReportState, type FormState } from "@/lib/use-list-detail";
import { OutletForm, PharmacyRegistrySetup } from "./pharmacy-registry-setup";
import { TaxRegistrationPanel } from "./tax-registration-panel";

type Outlets = ReturnType<typeof useApiPagedQuery<PharmacyOutlet>>;

/**
 * Outlets & access (#1303 E6): the pharmacy's outlets as a list, one open
 * beside it with its details, its access and its tax registration. Opening
 * an outlet here is setting it up, not choosing where to work: the working
 * outlet (`outlet=` in the address) is never changed by it.
 */
export function OutletsSetup({
  tenantId,
  pharmacy,
  subject,
  outlets,
}: {
  tenantId: string;
  pharmacy: PharmacyOrganization;
  subject: string;
  outlets: Outlets;
}) {
  const { can } = usePermissions();
  const pane = useListDetail({
    rows: outlets.items,
    name: (row) => row.name,
    fallbackName: "this outlet",
    newName: "the new outlet",
    headingId: "outlets-heading",
  });
  const { detail, paneRef, rowProps, openProps } = pane;
  const manage = can("pharmacy.access.manage");
  const record = detail.creating ? (
    <Panel className="space-y-6">
      <DetailHeader
        title={`Register outlet at ${pharmacy.name}`}
        noun="outlet"
        stepping={false}
        previous={{ disabled: true, onClick: () => {} }}
        next={{ disabled: true, onClick: () => {} }}
        closeLabel="Close outlet"
        onClose={pane.close}
      />
      <OutletForm
        tenantId={tenantId}
        pharmacy={pharmacy}
        onSaved={pane.saved}
        onStateChange={pane.formState("outlet")}
      />
    </Panel>
  ) : detail.openId ? (
    <OpenOutlet
      key={detail.openId}
      tenantId={tenantId}
      pharmacy={pharmacy}
      id={detail.openId}
      subject={subject}
      pane={pane}
    />
  ) : null;
  return (
    <div className="space-y-6" onClickCapture={pane.holdLinks}>
      <ListDetail
        label={
          detail.creating ? "New outlet" : pane.openName ? `Outlet ${pane.openName}` : "Outlet"
        }
        backHref={detail.closeHref}
        backLabel="Back to outlets"
        detail={record && <div ref={paneRef}>{record}</div>}
        list={
          <Panel flush aria-labelledby="outlets-heading">
            <div className="flex flex-wrap items-center justify-between gap-4 border-b p-5">
              <div>
                <h3
                  id="outlets-heading"
                  tabIndex={-1}
                  className="flex items-center gap-2 font-semibold outline-none"
                >
                  <Store className="size-5 text-(--hms-accent)" />
                  Outlets at {pharmacy.name}
                </h3>
                <p className="mt-1 text-sm text-muted-foreground">
                  Each outlet has its own access grants and tax registration.
                </p>
              </div>
              {manage && (
                <Button onClick={pane.create}>
                  <Plus className="mr-2 size-4" />
                  Register outlet
                </Button>
              )}
            </div>
            {outlets.isError ? (
              <RequestFailure
                message="Could not load this pharmacy's outlets."
                retry={outlets.retry}
              />
            ) : outlets.isPending ? (
              <p role="status" className="p-5">
                Loading outlets…
              </p>
            ) : outlets.items.length === 0 ? (
              <p className="p-5 text-sm text-muted-foreground">
                No outlets registered yet. Register the first one.
              </p>
            ) : (
              <ul className="divide-y">
                {outlets.items.map((row) => (
                  <li
                    key={row.id}
                    {...rowProps(row.id)}
                    className={`flex flex-wrap items-center justify-between gap-3 px-5 py-3 ${row.id === detail.openId ? "bg-(--hms-accent-dim)" : ""}`}
                  >
                    <div className="min-w-0">
                      <p className="wrap-anywhere font-medium">{row.name}</p>
                      <p className="mt-0.5 text-sm text-muted-foreground">
                        {row.code} · {countryProfile(row.country_code)?.name ?? row.country_code}
                      </p>
                    </div>
                    <Button
                      variant="outline"
                      size="sm"
                      {...openProps(row.id)}
                      aria-label={`Open ${row.code}`}
                      onClick={() => pane.openRow(row.id)}
                    >
                      Open
                    </Button>
                  </li>
                ))}
              </ul>
            )}
            <LoadMore
              label="Load more outlets"
              hasMore={outlets.hasMore}
              isLoading={outlets.isFetchingMore}
              onClick={outlets.loadMore}
            />
          </Panel>
        }
      />
      {/* The pharmacy's own setup, not an outlet's: below the list. */}
      {manage && <PharmacyRegistrySetup tenantId={tenantId} pharmacy={pharmacy} />}
    </div>
  );
}

// The open outlet, read by its id so a reload or a link reopens it. One the
// outlets list would hide is the same 404 as a missing one (#1303 D3, E6).
function OpenOutlet({
  tenantId,
  pharmacy,
  id,
  subject,
  pane,
}: {
  tenantId: string;
  pharmacy: PharmacyOrganization;
  id: string;
  subject: string;
  pane: ReturnType<typeof useListDetail<PharmacyOutlet>>;
}) {
  const { can } = usePermissions();
  const outlet = useApiQuery<PharmacyOutlet>(
    ["pharmacy", tenantId, pharmacy.id, "outlets", "one", id],
    `/pharmacy/pharmacies/${pharmacy.id}/outlets/${id}`,
  );
  const heading = useRef<HTMLHeadingElement>(null);
  // Viewing focuses the heading, not an edit field (#1303 D6).
  const loaded = outlet.data?.id;
  useEffect(() => {
    if (loaded) heading.current?.focus({ preventScroll: true });
  }, [loaded]);
  const row = outlet.data;
  // Close is in the header whatever loaded: beside the list there is no back link.
  const header = (
    <DetailHeader
      title={row?.name ?? "Outlet"}
      subtitle={row?.code}
      headingRef={heading}
      noun="outlet"
      previous={{ disabled: !pane.canStep(-1), onClick: () => pane.step(-1) }}
      next={{ disabled: !pane.canStep(1), onClick: () => pane.step(1) }}
      closeLabel="Close outlet"
      onClose={pane.close}
    />
  );
  if (!row)
    return (
      <Panel className="space-y-4">
        {header}
        {outlet.error instanceof ApiError && outlet.error.status === 404 ? (
          <p role="alert" className="text-sm">
            This record was not found, or you do not have access.
          </p>
        ) : outlet.isError ? (
          <RequestFailure message="Could not load this outlet." retry={outlet.refetch} />
        ) : (
          <p role="status">Loading outlet…</p>
        )}
      </Panel>
    );
  const scope: PharmacyScope = { tenantId, pharmacyId: pharmacy.id, outletId: row.id };
  return (
    <div className="space-y-6">
      <Panel className="space-y-5">
        {header}
        {/* Read only: an existing outlet cannot be changed yet (spec E6). */}
        <dl className="grid grid-cols-2 gap-4 text-sm">
          {(
            [
              ["Code", row.code],
              ["Country", countryProfile(row.country_code)?.name ?? row.country_code],
              ["Time zone", row.timezone],
              ["Regulatory review", "Pending"],
            ] as const
          ).map(([label, value]) => (
            <div key={label}>
              <dt className="text-muted-foreground">{label}</dt>
              <dd className="mt-1 font-medium wrap-anywhere">{value}</dd>
            </div>
          ))}
        </dl>
      </Panel>
      <OutletAccess
        scope={scope}
        subject={subject}
        outletName={row.name}
        canGrant={can("pharmacy.access.manage")}
        onStateChange={pane.formState("access")}
      />
      {can("pharmacy.tax.manage") && (
        <TaxRegistrationPanel
          scope={scope}
          outlet={row}
          today={zonedDateTimeInput(new Date().toISOString(), row.timezone).slice(0, 10)}
          onStateChange={pane.formState("tax")}
        />
      )}
    </div>
  );
}

const accessSchema = z.object({
  confirmed: z.boolean().refine(Boolean, "Confirm the outlet before granting access."),
});

// This outlet's access for the signed-in subject, and the grant-my-access
// form. 404 is the server's settled "no grant here" (#1285).
function OutletAccess({
  scope,
  subject,
  outletName,
  canGrant,
  onStateChange,
}: {
  scope: PharmacyScope;
  subject: string;
  outletName: string;
  canGrant: boolean;
  onStateChange?: FormState;
}) {
  const access = useApiQuery<{ level: "reader" | "editor" }>(
    pharmacyKey(scope, "access"),
    `${pharmacyRoot(scope)}/access`,
    { gcTime: 0, refetchOnWindowFocus: true },
  );
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
      onSuccess: () => form.reset({ confirmed: false }),
      suppressErrorToast: true,
    },
  );
  useReportState(onStateChange, form.formState.isDirty, grant.isPending);
  const missing = access.error instanceof ApiError && access.error.status === 404;
  return (
    <Panel aria-labelledby="outlet-access-heading" className="space-y-4">
      <h3 id="outlet-access-heading" className="font-semibold">
        Outlet access
      </h3>
      {access.isPending ? (
        <p role="status">Checking your access to this outlet…</p>
      ) : missing ? (
        <p className="text-sm text-muted-foreground">
          You don&apos;t have access to this outlet yet.
          {canGrant ? "" : " Ask your administrator to grant it."}
        </p>
      ) : access.isError ? (
        <RequestFailure
          message="Could not check your access to this outlet. Try again in a moment."
          retry={access.refetch}
        />
      ) : (
        <p className="text-sm">
          Your access here: <span className="font-medium capitalize">{access.data?.level}</span>
        </p>
      )}
      {canGrant && (
        <form noValidate className="space-y-4" onSubmit={form.handleSubmit(() => grant.mutate())}>
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
      )}
    </Panel>
  );
}
