"use client";

import { useRef, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Building2, ChevronRight, Plus, Search, ShieldCheck } from "lucide-react";
import {
  countryProfile,
  supplyKey,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type SupplyScope,
  type SupplyParty,
  type SupplyPartySummary,
  type SupplyPartyRevision,
} from "@helivanta/api";
import { Field, LoadMore, useZodForm, formatDateTime } from "@helivanta/ui";
import { RequestFailure } from "../request-failure";
import { PartyEditor } from "./party-editor";
import { PartyRegistration } from "./party-registration";
import { PlatformIdentity } from "./platform-identity";
import { WorkspaceConnections } from "../workspace-connections";

const searchSchema = z.object({ query: z.string().trim().max(200) });
const readRoles = ["administrator", "purchasing", "finance", "compliance", "auditor"];

export function PartiesWorkspace() {
  const identity = useApiQuery<{ tenant_id: string; subject: string }>(
    ["iam", "me", "permissions"],
    "/iam/me/permissions",
  );
  if (identity.isError)
    return (
      <RequestFailure message="Could not verify your active tenant." retry={identity.refetch} />
    );
  if (!identity.data) return <p role="status">Loading supply workspace…</p>;
  return (
    <TenantParties
      key={`${identity.data.tenant_id}:${identity.data.subject}`}
      scope={{ tenantId: identity.data.tenant_id, subject: identity.data.subject }}
    />
  );
}
function TenantParties({ scope }: { scope: SupplyScope }) {
  const { can } = usePermissions();
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<SupplyPartySummary | null>();
  const editor = useRef<HTMLElement>(null);
  const search = useZodForm(searchSchema, { query: "" });
  const parties = useApiPagedQuery<SupplyPartySummary>(
    supplyKey(scope, "parties", query),
    `/supply/parties?q=${encodeURIComponent(query)}`,
    { enabled: can("supply.directory.read") },
  );
  function reveal(party: SupplyPartySummary | null) {
    setSelected(party);
    requestAnimationFrame(() => {
      editor.current?.scrollIntoView({ block: "start", behavior: "instant" });
      editor.current
        ?.querySelector<HTMLElement>(party ? "h3" : "input")
        ?.focus({ preventScroll: true });
    });
  }
  if (!can("supply.directory.read"))
    return (
      <section className="rounded-2xl border bg-card p-7">
        <h2 className="text-xl font-semibold">Supply access required</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          Ask your administrator for the appropriate SupplyConnect role.
        </p>
      </section>
    );
  return (
    <div className="space-y-6">
      <header className="space-y-3">
        <p className="flex items-center gap-2 text-xs font-semibold uppercase tracking-widest text-(--hms-accent)">
          <Building2 className="size-4" />
          Supply Connect
        </p>
        <h2 className="text-3xl font-semibold tracking-tight">
          The right connections. Clearly accounted for.
        </h2>
        <p className="max-w-2xl text-sm leading-6 text-muted-foreground">
          Manage your organisations and supplier records across India and Pakistan, with private
          profiles and explicit access.
        </p>
      </header>
      <WorkspaceConnections current="supply" />
      <div className="flex items-start gap-3 rounded-xl border bg-card p-5">
        <ShieldCheck className="mt-0.5 size-5 shrink-0 text-(--hms-accent)" />
        <p className="text-sm leading-6 text-muted-foreground">
          Shared ownership or a hospital affiliation does not share private records. Draft profiles
          await verification before trading approval.
        </p>
      </div>
      <section className="overflow-hidden rounded-2xl border bg-card">
        <div className="flex flex-wrap items-start justify-between gap-4 border-b p-5">
          <div>
            <h3 className="font-semibold">Organisation directory</h3>
            <p className="mt-1 text-sm text-muted-foreground">
              Your organisations, pharmacies, distributors and other counterparties.
            </p>
          </div>
          {can("supply.party.write") && (
            <Button onClick={() => reveal(null)}>
              <Plus className="mr-2 size-4" />
              Add organisation
            </Button>
          )}
        </div>
        <form
          noValidate
          onSubmit={search.handleSubmit((values) => setQuery(values.query))}
          className="flex flex-wrap items-end gap-3 border-b p-5"
        >
          <div className="min-w-0 flex-1 basis-60">
            <Field
              id="party-search"
              label="Search by name or code prefix"
              error={search.formState.errors.query?.message}
            >
              <Input id="party-search" {...search.register("query")} />
            </Field>
          </div>
          <Button variant="outline" type="submit">
            <Search className="mr-2 size-4" />
            Search organisations
          </Button>
        </form>
        {parties.isError ? (
          <RequestFailure message="Could not load organisations." retry={parties.retry} />
        ) : parties.isPending ? (
          <p role="status" className="p-6">
            Loading organisations…
          </p>
        ) : parties.items.length === 0 ? (
          <div className="p-10 text-center">
            <Building2 className="mx-auto mb-3 size-8 text-muted-foreground" />
            <h4 className="font-semibold">
              {query ? "No matching organisations" : "No organisations yet"}
            </h4>
            <p className="mt-2 text-sm text-muted-foreground">
              {query
                ? "Try a name or organisation code prefix."
                : "Add a private organisation profile to begin."}
            </p>
          </div>
        ) : (
          <ul className="divide-y">
            {parties.items.map((party) => (
              <li key={party.id}>
                <button
                  type="button"
                  onClick={() => reveal(party)}
                  className="flex w-full items-center gap-4 px-5 py-5 text-left transition-colors hover:bg-muted/40 focus-visible:outline-2 focus-visible:outline-(--hms-accent)"
                >
                  <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-(--hms-accent-dim) text-(--hms-accent)">
                    <Building2 className="size-5" />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block break-words font-medium">{party.legal_name}</span>
                    <span className="mt-1 block text-sm text-muted-foreground">
                      {party.code} ·{" "}
                      {countryProfile(party.country_code)?.name ?? party.country_code} ·{" "}
                      {party.business_kind}
                    </span>
                  </span>
                  <span className="hidden rounded-full border px-3 py-1 text-xs text-muted-foreground sm:block">
                    Draft
                  </span>
                  <ChevronRight className="size-4 shrink-0 text-muted-foreground" />
                </button>
              </li>
            ))}
          </ul>
        )}
        <LoadMore
          hasMore={parties.hasMore}
          isLoading={parties.isFetchingMore}
          onClick={parties.loadMore}
        />
      </section>
      {selected !== undefined && (
        <section ref={editor} className="scroll-mt-6" aria-label="Selected organisation">
          {selected === null ? (
            <PartyEditor
              key="new"
              scope={scope}
              party={null}
              canManageOrganisation={can("supply.organization.manage")}
              onCancel={() => setSelected(undefined)}
              onSaved={(party) => setSelected({ ...party, access_roles: ["administrator"] })}
            />
          ) : (
            <PartyDetail
              key={selected.id}
              scope={scope}
              summary={selected}
              onClose={() => setSelected(undefined)}
            />
          )}
        </section>
      )}
    </div>
  );
}
function PartyDetail({
  scope,
  summary,
  onClose,
}: {
  scope: SupplyScope;
  summary: SupplyPartySummary;
  onClose: () => void;
}) {
  const access = useApiQuery<{ roles: string[] }>(
    supplyKey(scope, "party", summary.id, "access"),
    `/supply/parties/${summary.id}/access`,
    { gcTime: 0, refetchOnWindowFocus: true },
  );
  const { can } = usePermissions();
  if (access.isPending) return <p role="status">Checking organisation access…</p>;
  if (access.isError)
    return (
      <RequestFailure
        message="Organisation access could not be verified. It may have been revoked."
        retry={access.refetch}
      />
    );
  if (!can("supply.party.read") || !access.data?.roles.some((role) => readRoles.includes(role)))
    return (
      <div className="rounded-2xl border bg-card p-6">
        <h3 tabIndex={-1} className="font-semibold">
          Private profile access required
        </h3>
        <p className="mt-2 text-sm text-muted-foreground">
          Ask an administrator for explicit access to this organisation’s private profile.
        </p>
        <Button variant="outline" className="mt-4" onClick={onClose}>
          Close profile
        </Button>
      </div>
    );
  return <PrivateParty scope={scope} id={summary.id} roles={access.data.roles} onClose={onClose} />;
}
function PrivateParty({
  scope,
  id,
  roles,
  onClose,
}: {
  scope: SupplyScope;
  id: string;
  roles: string[];
  onClose: () => void;
}) {
  const { can } = usePermissions();
  const [editing, setEditing] = useState(false);
  const profile = useApiQuery<SupplyParty>(
    supplyKey(scope, "party", id, "detail"),
    `/supply/parties/${id}`,
    { gcTime: 0, refetchOnWindowFocus: true },
  );
  if (profile.isError)
    return (
      <RequestFailure
        message="The private profile is unavailable. Refresh access or contact your administrator."
        retry={profile.refetch}
      />
    );
  if (!profile.data) return <p role="status">Loading private profile…</p>;
  const party = profile.data;
  const writable =
    can("supply.party.write") &&
    roles.some((role) => role === "administrator" || role === "purchasing") &&
    (party.record_kind !== "owned_organisation" || can("supply.organization.manage"));
  if (editing && writable)
    return (
      <PartyEditor
        key={`${id}:${party.version}`}
        scope={scope}
        party={party}
        canManageOrganisation={can("supply.organization.manage")}
        onSaved={() => setEditing(false)}
        onCancel={() => setEditing(false)}
      />
    );
  return (
    <div className="space-y-5 rounded-2xl border bg-card p-5 sm:p-7">
      <div className="flex flex-wrap justify-between gap-4">
        <div>
          <p className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">
            Private profile · Version {party.version}
          </p>
          <h3 tabIndex={-1} className="mt-2 break-words text-xl font-semibold">
            {party.legal_name}
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">
            {party.trading_name || party.code} · {countryProfile(party.country_code)?.name}
          </p>
        </div>
        <div className="flex gap-2">
          {writable && (
            <Button
              variant="outline"
              onClick={() => {
                setEditing(true);
                requestAnimationFrame(() =>
                  document.getElementById("party-code")?.focus({ preventScroll: true }),
                );
              }}
            >
              Edit profile
            </Button>
          )}
          <Button variant="ghost" onClick={onClose}>
            Close profile
          </Button>
        </div>
      </div>
      <dl className="grid gap-5 border-y py-5 sm:grid-cols-2 lg:grid-cols-3">
        {[
          ["Business registration", party.registration_number],
          ["Jurisdiction", party.jurisdiction],
          ["Corporate group", party.corporate_group || "—"],
          ["Website", party.website || "—"],
          ["Preferred credit days", party.default_credit_days.toString()],
          ["Status", "Draft · verification pending"],
        ].map(([name, value]) => (
          <div key={name}>
            <dt className="text-xs text-muted-foreground">{name}</dt>
            <dd className="mt-1 break-words text-sm font-medium">{value}</dd>
          </div>
        ))}
      </dl>
      <section>
        <h4 className="font-semibold">Contacts</h4>
        {party.contacts.length === 0 ? (
          <p className="mt-2 text-sm text-muted-foreground">No contacts recorded.</p>
        ) : (
          <ul className="mt-3 grid gap-3 sm:grid-cols-2">
            {party.contacts.map((row) => (
              <li key={row.id} className="min-w-0 rounded-xl border p-4 text-sm">
                <p className="break-words font-medium">
                  {row.name} · {row.role}
                </p>
                <p className="mt-1 break-words text-muted-foreground">
                  {[row.email, row.phone, row.alternate_phone].filter(Boolean).join(" · ") ||
                    "No contact channels recorded"}
                </p>
              </li>
            ))}
          </ul>
        )}
      </section>
      <section>
        <h4 className="font-semibold">Addresses</h4>
        {party.addresses.length === 0 ? (
          <p className="mt-2 text-sm text-muted-foreground">No addresses recorded.</p>
        ) : (
          <ul className="mt-3 grid gap-3 sm:grid-cols-2">
            {party.addresses.map((row) => (
              <li key={row.id} className="min-w-0 rounded-xl border p-4 text-sm">
                <p className="font-medium capitalize">{row.kind}</p>
                <p className="mt-1 break-words text-muted-foreground">
                  {[
                    row.line1,
                    row.line2,
                    row.city,
                    row.region,
                    row.postal_code,
                    countryProfile(row.country_code)?.name,
                  ]
                    .filter(Boolean)
                    .join(", ")}
                </p>
              </li>
            ))}
          </ul>
        )}
      </section>
      <section>
        <h4 className="font-semibold">Tax declarations</h4>
        {party.tax_registrations.length === 0 ? (
          <p className="mt-2 text-sm text-muted-foreground">No tax registrations recorded.</p>
        ) : (
          <ul className="mt-3 space-y-2">
            {party.tax_registrations.map((row) => (
              <li key={row.id} className="break-words rounded-xl border p-4 text-sm">
                <strong>{row.kind.toUpperCase()}</strong> · {row.number} · {row.jurisdiction}
                <span className="ml-2 text-xs text-muted-foreground">Unverified declaration</span>
              </li>
            ))}
          </ul>
        )}
      </section>
      {party.record_kind === "owned_organisation" && (
        <PlatformIdentity
          key={`${scope.tenantId}:${scope.subject}:${id}:${party.version}`}
          scope={{ ...scope, partyId: id }}
          partyVersion={party.version}
        />
      )}
      <PartyRegistration
        scope={{ ...scope, partyId: id }}
        country={party.country_code}
        owned={party.record_kind === "owned_organisation"}
        roles={roles}
      />
      <details className="border-t pt-5">
        <summary className="cursor-pointer text-sm font-medium">Profile revision history</summary>
        <PartyHistory scope={scope} id={id} />
      </details>
    </div>
  );
}
function PartyHistory({ scope, id }: { scope: SupplyScope; id: string }) {
  const history = useApiPagedQuery<SupplyPartyRevision>(
    supplyKey(scope, "party", id, "revisions"),
    `/supply/parties/${id}/revisions`,
  );
  if (history.isError)
    return <RequestFailure message="Could not load profile history." retry={history.retry} />;
  if (history.isPending)
    return (
      <p className="mt-4 text-sm" role="status">
        Loading revisions…
      </p>
    );
  return (
    <div className="mt-4">
      <ul className="divide-y">
        {history.items.map((revision) => (
          <li key={revision.id} className="py-3 text-sm">
            <p className="break-words font-medium">
              Version {revision.version} · {revision.snapshot.legal_name}
            </p>
            <p className="mt-1 break-words text-xs text-muted-foreground">
              {formatDateTime(revision.created_at)} · {revision.actor_subject}
            </p>
          </li>
        ))}
      </ul>
      <LoadMore
        hasMore={history.hasMore}
        isLoading={history.isFetchingMore}
        onClick={history.loadMore}
      />
    </div>
  );
}
