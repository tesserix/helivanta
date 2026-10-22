"use client";

import { useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  apiFetch,
  newSupplyId,
  supplyTerritoryInputSchema,
  supplyCorporateInputSchema,
  type SupplyCorporateInput,
  type SupplyPremise,
  type SupplyCorporateStructure as CorporateStructure,
  type SupplyTerritoryInput as TerritoryInput,
  type SupplyTerritory as Territory,
  supplyHeaders,
  supplyKey,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type SupplyPartySummary,
  type SupplyRegistryScope,
} from "@helivanta/api";
import { Field, FormSelectField, LoadMore, useZodForm, RequestFailure } from "@helivanta/ui";

export function OrganisationStructure({
  scope,
  country,
  roles,
}: {
  scope: SupplyRegistryScope;
  country: string;
  roles: string[];
}) {
  const { can } = usePermissions();
  const allowed =
    can("supply.party.read") &&
    roles.some((role) =>
      ["administrator", "purchasing", "finance", "compliance", "auditor"].includes(role),
    );
  const territories =
    can("supply.territory.read") &&
    roles.some((role) =>
      ["administrator", "purchasing", "finance", "compliance", "auditor", "sales"].includes(role),
    );
  if (!allowed && !territories) return null;
  return (
    <div className="space-y-5">
      {allowed && <CorporatePanel scope={scope} roles={roles} />}
      {territories && <TerritoryPanel scope={scope} country={country} roles={roles} />}
    </div>
  );
}

function CorporatePanel({ scope, roles }: { scope: SupplyRegistryScope; roles: string[] }) {
  const { can } = usePermissions();
  const [editing, setEditing] = useState(false);
  const [history, setHistory] = useState(false);
  const target = useRef<HTMLDivElement>(null);
  const root = `/supply/parties/${scope.partyId}/corporate-structure`;
  const key = supplyKey(scope, "party", scope.partyId, "corporate-structure");
  const structure = useApiQuery<CorporateStructure>(key, root, {
    gcTime: 0,
    refetchOnWindowFocus: true,
    request: { headers: supplyHeaders(scope) },
  });
  const writable = can("supply.organization.manage") && roles.includes("administrator");
  return (
    <section aria-label="Organisation structure" className="space-y-5 rounded-xl border p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h4 className="font-semibold">Organisation structure</h4>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            Record the corporate relationship between owned organisations. Parent links do not share
            records or grant access to staff.
          </p>
        </div>
        {writable && structure.data && !structure.isError && !editing && (
          <Button
            variant="outline"
            onClick={() => {
              setEditing(true);
              requestAnimationFrame(() => {
                target.current?.scrollIntoView({ block: "start", behavior: "instant" });
                target.current?.querySelector<HTMLButtonElement>("[role=combobox]")?.focus();
              });
            }}
          >
            Edit corporate relationship
          </Button>
        )}
      </div>
      {structure.isError ? (
        <RequestFailure
          message="The corporate relationship is unavailable. Refresh access or contact your administrator."
          retry={structure.refetch}
        />
      ) : !structure.data ? (
        <p role="status">Loading corporate relationship…</p>
      ) : editing && writable ? (
        <div ref={target} className="scroll-mt-32">
          <CorporateEditor
            key={structure.data.version}
            scope={scope}
            structure={structure.data}
            onDone={() => setEditing(false)}
            onReload={() => structure.refetch()}
          />
        </div>
      ) : (
        <div className="space-y-2 text-sm text-muted-foreground">
          <p>
            {structure.data.version === 0
              ? "Corporate relationship has not been configured."
              : structure.data.parent_party_id
                ? `Subsidiary organisation · Version ${structure.data.version}`
                : `Root organisation · Version ${structure.data.version}`}
          </p>
          {structure.data.parent_party_id && (
            <CorporateParent scope={scope} id={structure.data.parent_party_id} />
          )}
        </div>
      )}
      {!structure.isError && structure.data && structure.data.version > 0 && (
        <details
          onToggle={(event) => setHistory(event.currentTarget.open)}
          className="border-t pt-4"
        >
          <summary className="cursor-pointer text-sm font-medium">
            Corporate relationship history
          </summary>
          {history && <CorporateHistory scope={scope} />}
        </details>
      )}
    </section>
  );
}

function CorporateHistory({ scope }: { scope: SupplyRegistryScope }) {
  const rows = useApiPagedQuery<{
    id: string;
    version: number;
    snapshot: CorporateStructure;
    created_at: string;
  }>(
    supplyKey(scope, "party", scope.partyId, "corporate-structure", "revisions"),
    `/supply/parties/${scope.partyId}/corporate-structure/revisions`,
    { request: { headers: supplyHeaders(scope) } },
  );
  if (rows.isError)
    return (
      <RequestFailure message="Could not load corporate relationship history." retry={rows.retry} />
    );
  if (rows.isPending) return <p role="status">Loading corporate history…</p>;
  return (
    <div className="mt-3 space-y-3">
      {rows.items.length === 0 && (
        <p className="text-sm text-muted-foreground">No corporate revisions recorded.</p>
      )}
      {rows.items.map((row) => (
        <div className="rounded-lg border p-3 text-sm" key={row.id}>
          <p>
            Version {row.version} · {new Date(row.created_at).toLocaleString()}
          </p>
          {row.snapshot.parent_party_id ? (
            <CorporateParent scope={scope} id={row.snapshot.parent_party_id} />
          ) : (
            <p>Root organisation</p>
          )}
        </div>
      ))}
      <LoadMore hasMore={rows.hasMore} isLoading={rows.isFetchingMore} onClick={rows.loadMore} />
    </div>
  );
}

function CorporateParent({ scope, id }: { scope: SupplyRegistryScope; id: string }) {
  const parent = useApiQuery<SupplyPartySummary>(
    supplyKey(scope, "party", id, "corporate-parent-detail"),
    `/supply/parties/${id}`,
    { gcTime: 0, refetchOnWindowFocus: true, request: { headers: supplyHeaders(scope) } },
  );
  if (parent.isError) return <p>Parent profile is unavailable with your current access.</p>;
  if (!parent.data) return <p role="status">Loading parent organisation…</p>;
  return (
    <p>
      Parent: {parent.data.code} · {parent.data.legal_name}
    </p>
  );
}

function CorporateEditor({
  scope,
  structure,
  onDone,
  onReload,
}: {
  scope: SupplyRegistryScope;
  structure: CorporateStructure;
  onDone: () => void;
  onReload: () => unknown;
}) {
  const form = useZodForm(supplyCorporateInputSchema, {
    expected_version: structure.version,
    parent_party_id: structure.parent_party_id,
  });
  const parent = form.watch("parent_party_id") ?? "";
  const root = `/supply/parties/${scope.partyId}/corporate-structure`;
  const candidates = useApiPagedQuery<SupplyPartySummary>(
    supplyKey(scope, "directory", "corporate-parents"),
    "/supply/parties",
    { request: { headers: supplyHeaders(scope) } },
  );
  const selected = useApiQuery<SupplyPartySummary>(
    supplyKey(scope, "party", parent, "corporate-parent-detail"),
    `/supply/parties/${parent}`,
    {
      enabled: !!parent,
      gcTime: 0,
      refetchOnWindowFocus: true,
      request: { headers: supplyHeaders(scope) },
    },
  );
  const parentStructure = useApiQuery<CorporateStructure>(
    supplyKey(scope, "party", parent, "corporate-parent-structure"),
    `/supply/parties/${parent}/corporate-structure`,
    {
      enabled: !!parent,
      gcTime: 0,
      refetchOnWindowFocus: true,
      request: { headers: supplyHeaders(scope) },
    },
  );
  const options = candidates.items.filter(
    (row) =>
      row.id !== scope.partyId &&
      row.record_kind === "owned_organisation" &&
      row.access_roles.includes("administrator"),
  );
  if (selected.data && !selected.isError && !options.some((row) => row.id === parent))
    options.push(selected.data);
  const validParent =
    !parent ||
    (!selected.isError &&
      selected.data?.record_kind === "owned_organisation" &&
      !parentStructure.isError &&
      !!parentStructure.data?.version &&
      parentStructure.data.parent_party_id === null);
  const mutation = useApiMutation(
    (values: SupplyCorporateInput) =>
      apiFetch<CorporateStructure>(root, {
        method: "PUT",
        headers: supplyHeaders(scope),
        body: JSON.stringify(values),
      }),
    {
      invalidate: [supplyKey(scope, "party", scope.partyId, "corporate-structure")],
      successToast: "Corporate relationship saved",
      onSuccess: onDone,
      suppressErrorToast: true,
    },
  );
  return (
    <form
      noValidate
      className="space-y-4"
      onSubmit={form.handleSubmit((values) => {
        if (validParent && !candidates.isError && !mutation.isPending) mutation.mutate(values);
      })}
    >
      <div className="space-y-1.5">
        <FormSelectField
          control={form.control}
          name="parent_party_id"
          id="corporate-parent"
          label="Parent organisation"
          emptyValue={null}
          disabled={mutation.isPending || candidates.isPending || candidates.isError}
          options={[
            { value: "", label: "No parent — root organisation" },
            ...options.map((row) => ({ value: row.id, label: `${row.code} · ${row.legal_name}` })),
          ]}
        />
        <p id="corporate-parent-help" className="text-xs text-muted-foreground">
          Choose an owned organisation configured as a root. Each country keeps its local rules. You
          must administer both organisations. Each organisation keeps its own explicit access
          grants.
        </p>
      </div>
      {candidates.isError && (
        <RequestFailure message="Could not load eligible organisations." retry={candidates.retry} />
      )}
      <LoadMore
        hasMore={candidates.hasMore}
        isLoading={candidates.isFetchingMore}
        onClick={candidates.loadMore}
      />
      {parent && (selected.isPending || parentStructure.isPending) && (
        <p role="status">Checking parent organisation…</p>
      )}
      {parent && !validParent && !selected.isPending && !parentStructure.isPending && (
        <p role="alert" className="text-sm text-destructive">
          This parent is unavailable or is not a configured root. Choose another organisation or
          refresh access.
        </p>
      )}
      {mutation.error && (
        <div className="space-y-2">
          <p role="alert" className="text-sm text-destructive">
            {mutation.error.message} Reload this relationship if another administrator changed it.
          </p>
          <Button type="button" variant="outline" onClick={onReload}>
            Reload corporate relationship
          </Button>
        </div>
      )}
      <div className="flex flex-wrap gap-2">
        <Button disabled={!validParent || candidates.isError || mutation.isPending} type="submit">
          {mutation.isPending ? "Saving…" : "Save corporate relationship"}
        </Button>
        <Button type="button" variant="ghost" disabled={mutation.isPending} onClick={onDone}>
          Cancel corporate changes
        </Button>
      </div>
    </form>
  );
}

type SalesGrant = {
  subject: string;
  role: string;
  premise_id?: string | null;
  effective_status?: string;
};
function TerritoryPanel({
  scope,
  country,
  roles,
}: {
  scope: SupplyRegistryScope;
  country: string;
  roles: string[];
}) {
  const { can } = usePermissions();
  const [editing, setEditing] = useState<Territory | null>();
  const [history, setHistory] = useState<string | null>(null);
  const target = useRef<HTMLDivElement>(null);
  const rows = useApiPagedQuery<Territory>(
    supplyKey(scope, "party", scope.partyId, "territories"),
    `/supply/parties/${scope.partyId}/territories`,
    { request: { headers: supplyHeaders(scope) } },
  );
  const writable = can("supply.territory.write") && roles.includes("administrator");
  const privateReader = roles.some((role) =>
    ["administrator", "purchasing", "finance", "compliance", "auditor"].includes(role),
  );
  function edit(row: Territory | null) {
    setEditing(row);
    requestAnimationFrame(() => {
      target.current?.scrollIntoView({ block: "start", behavior: "instant" });
      target.current?.querySelector<HTMLInputElement>("input")?.focus({ preventScroll: true });
    });
  }
  return (
    <section aria-label="Sales territories" className="space-y-5 rounded-xl border p-5">
      <div className="flex flex-wrap justify-between gap-3">
        <div>
          <h4 className="font-semibold">Sales territories</h4>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            Maintain regional coverage, operating branches and explicit representative assignments.
            Assignments describe sales coverage and never create access rights.
          </p>
        </div>
        {writable && !rows.isError && (
          <Button variant="outline" onClick={() => edit(null)}>
            Add sales territory
          </Button>
        )}
      </div>
      {rows.isError ? (
        <RequestFailure
          message="Could not load sales territories. Refresh access before continuing."
          retry={rows.retry}
        />
      ) : rows.isPending ? (
        <p role="status">Loading sales territories…</p>
      ) : (
        <>
          {rows.items.length === 0 && (
            <p className="text-sm text-muted-foreground">No sales territories recorded.</p>
          )}
          <ul className="divide-y">
            {rows.items.map((row) => (
              <li className="space-y-3 py-4" key={row.id}>
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0">
                    <p className="break-words font-medium">
                      {row.code} · {row.name}
                    </p>
                    <p className="text-sm text-muted-foreground">
                      {[row.city, row.region, row.country_code].filter(Boolean).join(", ")} ·{" "}
                      {row.status} · Version {row.version}
                    </p>
                    <p className="mt-1 break-words text-sm text-muted-foreground">
                      {row.description}
                    </p>
                    {privateReader ? (
                      <p className="mt-1 break-words text-sm text-muted-foreground">
                        Representatives:{" "}
                        {row.assigned_sales_subjects.length
                          ? row.assigned_sales_subjects.join(", ")
                          : "None assigned"}
                      </p>
                    ) : (
                      <p className="mt-1 text-sm text-muted-foreground">Assigned to you</p>
                    )}
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {writable && (
                      <Button
                        variant="outline"
                        aria-label={`Edit territory ${row.code}`}
                        onClick={() => edit(row)}
                      >
                        Edit territory
                      </Button>
                    )}
                    {privateReader && (
                      <Button
                        variant="ghost"
                        aria-label={`History territory ${row.code}`}
                        onClick={() => setHistory(history === row.id ? null : row.id)}
                      >
                        History
                      </Button>
                    )}
                  </div>
                </div>
                {privateReader && history === row.id && (
                  <TerritoryHistory scope={scope} id={row.id} />
                )}
              </li>
            ))}
          </ul>
          <LoadMore
            hasMore={rows.hasMore}
            isLoading={rows.isFetchingMore}
            onClick={rows.loadMore}
          />
        </>
      )}
      <div ref={target} className="scroll-mt-32">
        {writable && !rows.isError && editing !== undefined && (
          <TerritoryEditor
            key={editing?.id ?? "new-territory"}
            scope={scope}
            country={country}
            territory={editing}
            admin={roles.includes("administrator") && can("supply.party.access.manage")}
            onDone={() => setEditing(undefined)}
            onReload={() => {
              rows.retry();
              setEditing(undefined);
            }}
          />
        )}
      </div>
    </section>
  );
}
function TerritoryEditor({
  scope,
  country,
  territory,
  admin,
  onDone,
  onReload,
}: {
  scope: SupplyRegistryScope;
  country: string;
  territory: Territory | null;
  admin: boolean;
  onDone: () => void;
  onReload: () => void;
}) {
  const { can } = usePermissions();
  const id = useRef(territory?.id ?? newSupplyId());
  const form = useZodForm(
    supplyTerritoryInputSchema,
    territory
      ? {
          expected_version: territory.version,
          code: territory.code,
          name: territory.name,
          country_code: territory.country_code,
          region: territory.region,
          city: territory.city,
          description: territory.description,
          status: territory.status,
          branch_premise_id: territory.branch_premise_id ?? null,
          assigned_sales_subjects: territory.assigned_sales_subjects,
        }
      : {
          expected_version: 0,
          code: "",
          name: "",
          country_code: country === "PK" ? "PK" : "IN",
          region: "",
          city: "",
          description: "",
          status: "active",
          branch_premise_id: null,
          assigned_sales_subjects: [],
        },
  );
  const premises = useApiPagedQuery<SupplyPremise>(
    supplyKey(scope, "party", scope.partyId, "premises", "territory-options"),
    `/supply/parties/${scope.partyId}/premises`,
    { enabled: can("supply.premise.read"), request: { headers: supplyHeaders(scope) } },
  );
  const branch = form.watch("branch_premise_id");
  const selected = useApiQuery<SupplyPremise>(
    supplyKey(scope, "party", scope.partyId, "premise", branch, "territory-option"),
    `/supply/parties/${scope.partyId}/premises/${branch}`,
    {
      enabled: !!branch && can("supply.premise.read"),
      gcTime: 0,
      refetchOnWindowFocus: true,
      request: { headers: supplyHeaders(scope) },
    },
  );
  const candidates = useApiPagedQuery<SalesGrant>(
    supplyKey(scope, "party", scope.partyId, "grants", "territory-options"),
    `/supply/parties/${scope.partyId}/grants`,
    { enabled: admin, request: { headers: supplyHeaders(scope) } },
  );
  const legacy = useApiPagedQuery<SalesGrant>(
    supplyKey(scope, "party", scope.partyId, "legacy-grants", "territory-options"),
    `/supply/parties/${scope.partyId}/access/grants`,
    { enabled: admin, request: { headers: supplyHeaders(scope) } },
  );
  const subjects = [
    ...new Set([
      ...candidates.items
        .filter(
          (row) => row.role === "sales" && !row.premise_id && row.effective_status === "active",
        )
        .map((row) => row.subject),
      ...legacy.items.filter((row) => row.role === "sales").map((row) => row.subject),
    ]),
  ];
  const assigned = form.watch("assigned_sales_subjects");
  const options = premises.items.filter(
    (row) => row.kind === "branch" && row.country_code === country && row.status === "active",
  );
  if (selected.data && !selected.isError && !options.some((row) => row.id === selected.data.id))
    options.push(selected.data);
  const validBranch =
    !branch ||
    (!can("supply.premise.read") && territory?.branch_premise_id === branch) ||
    (!selected.isError &&
      !!selected.data &&
      selected.data.kind === "branch" &&
      selected.data.country_code === country &&
      (selected.data.status === "active" || territory?.branch_premise_id === branch));
  const mutation = useApiMutation(
    (values: TerritoryInput) =>
      apiFetch<Territory>(`/supply/parties/${scope.partyId}/territories/${id.current}`, {
        method: "PUT",
        headers: supplyHeaders(scope),
        body: JSON.stringify(values),
      }),
    {
      invalidate: [supplyKey(scope, "party", scope.partyId, "territories")],
      successToast: "Sales territory saved",
      onSuccess: onDone,
      suppressErrorToast: true,
    },
  );
  const error = (name: keyof TerritoryInput) => form.formState.errors[name]?.message;
  return (
    <form
      noValidate
      onSubmit={form.handleSubmit((values) => {
        if (validBranch && !premises.isError && !mutation.isPending) mutation.mutate(values);
      })}
      className="space-y-4 rounded-xl bg-muted/30 p-4"
    >
      <h5 className="font-medium">{territory ? "Edit sales territory" : "New sales territory"}</h5>
      <div className="grid gap-4 sm:grid-cols-2">
        {(
          [
            ["code", "Territory code"],
            ["name", "Territory name"],
            ["region", "State or province"],
            ["city", "City"],
            ["description", "Coverage description"],
          ] as const
        ).map(([name, text]) => (
          <Field key={name} id={`territory-${name}`} label={text} error={error(name)}>
            <Input
              id={`territory-${name}`}
              {...form.register(name)}
              disabled={mutation.isPending}
              aria-invalid={!!error(name)}
              aria-describedby={error(name) ? `territory-${name}-error` : undefined}
            />
          </Field>
        ))}
        <FormSelectField
          control={form.control}
          name="status"
          id="territory-status"
          label="Territory status"
          disabled={mutation.isPending}
          options={[
            { value: "active", label: "Active" },
            { value: "inactive", label: "Inactive" },
          ]}
        />
      </div>
      <p className="text-sm text-muted-foreground">
        Country: {country === "PK" ? "Pakistan" : "India"}. Territory country follows the owning
        organisation.
      </p>
      <FormSelectField
        control={form.control}
        name="branch_premise_id"
        id="territory-branch"
        label="Operating branch"
        emptyValue={null}
        disabled={
          mutation.isPending ||
          premises.isPending ||
          premises.isError ||
          !can("supply.premise.read")
        }
        options={[
          { value: "", label: "No specific operating branch" },
          ...options.map((row) => ({ value: row.id, label: `${row.code} · ${row.name}` })),
        ]}
      />
      <LoadMore
        hasMore={premises.hasMore}
        isLoading={premises.isFetchingMore}
        onClick={premises.loadMore}
      />
      {premises.isError && (
        <RequestFailure message="Could not load operating branches." retry={premises.retry} />
      )}
      {branch && selected.isPending && <p role="status">Checking operating branch…</p>}
      {branch && !validBranch && !selected.isPending && (
        <p role="alert" className="text-sm text-destructive">
          The selected branch is unavailable. Refresh access or choose an active branch of this
          organisation.
        </p>
      )}
      <fieldset className="space-y-2 rounded-lg border p-3">
        <legend className="px-1 text-sm font-medium">Assigned sales representatives</legend>
        <p className="text-sm text-muted-foreground">
          Choose members with an existing active organisation-wide sales grant. Assignment does not
          create permissions.
        </p>
        {admin ? (
          candidates.isError || legacy.isError ? (
            <RequestFailure
              message="Could not load eligible sales representatives. Existing assignments are preserved."
              retry={() => {
                candidates.retry();
                legacy.retry();
              }}
            />
          ) : (
            <>
              {[...new Set([...subjects, ...assigned])].map((subject) => (
                <label className="flex items-start gap-2 break-words text-sm" key={subject}>
                  <input
                    type="checkbox"
                    checked={assigned.includes(subject)}
                    disabled={
                      mutation.isPending ||
                      (!subjects.includes(subject) && !assigned.includes(subject))
                    }
                    onChange={(event) =>
                      form.setValue(
                        "assigned_sales_subjects",
                        event.target.checked
                          ? [...assigned, subject]
                          : assigned.filter((value) => value !== subject),
                        { shouldValidate: true, shouldDirty: true },
                      )
                    }
                  />
                  {subject}
                  {!subjects.includes(subject) && " · Current grant unavailable"}
                </label>
              ))}
              {subjects.length === 0 && !candidates.isPending && !legacy.isPending && (
                <p className="text-sm text-muted-foreground">
                  No eligible sales grants. An organisation administrator must grant sales access
                  first.
                </p>
              )}
              <LoadMore
                hasMore={candidates.hasMore}
                isLoading={candidates.isFetchingMore}
                onClick={candidates.loadMore}
              />
              <LoadMore
                hasMore={legacy.hasMore}
                isLoading={legacy.isFetchingMore}
                onClick={legacy.loadMore}
              />
            </>
          )
        ) : (
          <p className="text-sm text-muted-foreground">
            An organisation administrator manages representative assignments. Current assignments:{" "}
            {assigned.length ? assigned.join(", ") : "None"}.
          </p>
        )}
        {error("assigned_sales_subjects") && (
          <p role="alert" className="text-sm text-destructive">
            {error("assigned_sales_subjects")}
          </p>
        )}
      </fieldset>
      {mutation.error && (
        <div className="space-y-2">
          <p role="alert" className="text-sm text-destructive">
            {mutation.error.message}
          </p>
          <Button type="button" variant="outline" onClick={onReload}>
            Reload sales territories
          </Button>
        </div>
      )}
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={mutation.isPending || !validBranch || premises.isError}>
          {mutation.isPending ? "Saving…" : "Save sales territory"}
        </Button>
        <Button type="button" variant="ghost" disabled={mutation.isPending} onClick={onDone}>
          Cancel territory changes
        </Button>
      </div>
    </form>
  );
}
function TerritoryHistory({ scope, id }: { scope: SupplyRegistryScope; id: string }) {
  const rows = useApiPagedQuery<{
    id: string;
    version: number;
    snapshot: Territory;
    created_at: string;
  }>(
    supplyKey(scope, "party", scope.partyId, "territories", id, "revisions"),
    `/supply/parties/${scope.partyId}/territories/${id}/revisions`,
    { request: { headers: supplyHeaders(scope) } },
  );
  if (rows.isError)
    return <RequestFailure message="Could not load territory history." retry={rows.retry} />;
  if (rows.isPending) return <p role="status">Loading territory history…</p>;
  return (
    <div className="space-y-2 rounded-lg border p-3">
      {rows.items.map((row) => (
        <div key={row.id} className="text-sm">
          <p>
            Version {row.version} · {new Date(row.created_at).toLocaleString()}
          </p>
          <p>
            {row.snapshot.name} · {row.snapshot.status} · {row.snapshot.region}
          </p>
          <p className="break-words text-muted-foreground">
            Representatives: {row.snapshot.assigned_sales_subjects.join(", ") || "None assigned"}
          </p>
        </div>
      ))}
      <LoadMore hasMore={rows.hasMore} isLoading={rows.isFetchingMore} onClick={rows.loadMore} />
    </div>
  );
}
