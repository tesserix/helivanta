"use client";

import { useEffect, useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  apiFetch,
  ApiError,
  useSupplyAccessRefresh,
  newSupplyId,
  supplyHeaders,
  supplyKey,
  supplyAccessGrantInputSchema,
  supplyAccessGrantFieldsSchema,
  supplyAccessScopePolicy,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type SupplyPremise,
  type SupplyAccessGrantInput as GrantInput,
  type SupplyAccessGrant as Grant,
  type SupplyLegacyGrant as LegacyGrant,
  type SupplyRegistryScope,
} from "@helivanta/api";
import { Field, FormSelectField, LoadMore, useZodForm } from "@helivanta/ui";
import { RequestFailure } from "../request-failure";
import { OrganisationAccessLimits } from "./organisation-access-limits";
import {
  AccessWindowPolicyStatus,
  accessWindowLabel,
  useAccessWindowPolicy,
  validateAccessWindow,
} from "./access-window-policy";
import {
  dateTimeInputValue as dateInput,
  dateTimeInputISO as isoInput,
  formatAbsoluteTime as timestamp,
} from "./time-input";

type Member = { id: string; subject: string; role_key: string; created_at: string };
const label = (value: string) => value.replaceAll("_", " ");

export function OrganisationAccess({
  scope,
  roles,
}: {
  scope: SupplyRegistryScope;
  roles: string[];
}) {
  const { can } = usePermissions();
  if (!can("supply.party.access.manage") || !roles.includes("administrator")) return null;
  return <AccessPanel scope={scope} />;
}
function AccessPanel({ scope }: { scope: SupplyRegistryScope }) {
  const { denied } = useSupplyAccessRefresh(scope);
  const [editing, setEditing] = useState<Grant | null>();
  const [history, setHistory] = useState<string | null>(null);
  const target = useRef<HTMLDivElement>(null);
  const root = `/supply/parties/${scope.partyId}`;
  const key = supplyKey(scope, "party", scope.partyId, "grants");
  const grants = useApiPagedQuery<Grant>(key, `${root}/grants`, {
    request: { headers: supplyHeaders(scope) },
  });
  const legacy = useApiPagedQuery<LegacyGrant>(
    supplyKey(scope, "party", scope.partyId, "legacy-grants"),
    `${root}/access/grants`,
    { request: { headers: supplyHeaders(scope) } },
  );
  useEffect(() => {
    if (
      [grants.error, legacy.error].some(
        (error) => error instanceof ApiError && [401, 403, 404].includes(error.status),
      )
    )
      denied();
  }, [grants.error, legacy.error, denied]);
  if (grants.isError || legacy.isError)
    return (
      <section aria-label="Organisation access" className="rounded-xl border p-3 sm:p-5">
        <RequestFailure
          message="Organisation access management is unavailable. Your access may have changed."
          retry={() => {
            grants.retry();
            legacy.retry();
          }}
        />
      </section>
    );
  function edit(row: Grant | null) {
    setEditing(row);
    requestAnimationFrame(() => {
      target.current?.scrollIntoView({ block: "start", behavior: "instant" });
      target.current?.querySelector<HTMLElement>("[role=combobox],input")?.focus();
    });
  }
  const available = !grants.isError && !legacy.isError;
  return (
    <section aria-label="Organisation access" className="space-y-5 rounded-xl border p-3 sm:p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h4 className="font-semibold">Organisation access</h4>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            Explicit roles remain within this organisation and its selected location. Corporate
            links and sales territory assignments never grant access.
          </p>
        </div>
        {available && (
          <Button variant="outline" onClick={() => edit(null)}>
            Add time-limited grant
          </Button>
        )}
      </div>
      {grants.isError ? (
        <RequestFailure
          message="Could not load time-limited access grants. Refresh access before continuing."
          retry={grants.retry}
        />
      ) : grants.isPending ? (
        <p role="status">Loading time-limited grants…</p>
      ) : (
        <div className="space-y-3">
          {grants.items.length === 0 && (
            <p className="text-sm text-muted-foreground">No time-limited grants recorded.</p>
          )}
          {grants.items.map((row) => (
            <article
              key={row.id}
              className="flex flex-wrap justify-between gap-3 rounded-lg border p-4"
            >
              <div className="min-w-0">
                <p className="break-words font-medium">{row.subject}</p>
                <p className="text-sm capitalize">
                  {label(row.role)} · {row.effective_status} · Version {row.version}
                </p>
                <p className="mt-1 text-sm text-muted-foreground">
                  {row.premise_id ? "Location-scoped" : "Organisation-wide"} ·{" "}
                  {timestamp(row.valid_from)} to {timestamp(row.expires_at)}
                </p>
                <p className="mt-1 break-words text-sm text-muted-foreground">{row.reason}</p>
              </div>
              <div className="flex flex-wrap items-start gap-2">
                {row.status !== "revoked" && (
                  <Button
                    variant="outline"
                    onClick={() => edit(row)}
                    aria-label={`Manage ${row.subject} ${row.role} grant`}
                  >
                    Manage grant
                  </Button>
                )}
                <Button
                  variant="ghost"
                  onClick={() => setHistory(history === row.id ? null : row.id)}
                  aria-label={`History ${row.subject} ${row.role} grant`}
                >
                  History
                </Button>
              </div>
              {history === row.id && <GrantHistory scope={scope} id={row.id} />}
            </article>
          ))}
          <LoadMore
            hasMore={grants.hasMore}
            isLoading={grants.isFetchingMore}
            onClick={grants.loadMore}
          />
        </div>
      )}
      <OrganisationAccessLimits scope={scope} />
      <div ref={target} className="scroll-mt-32">
        {available && editing !== undefined && (
          <GrantEditor
            key={editing?.id ?? "new"}
            scope={scope}
            grant={editing}
            current={[...grants.items, ...legacy.items]}
            onDone={() => setEditing(undefined)}
            onReload={() => {
              grants.retry();
              legacy.retry();
              setEditing(undefined);
            }}
          />
        )}
      </div>
      <section aria-label="Existing organisation-wide grants" className="space-y-3 border-t pt-5">
        <h5 className="font-medium">Existing organisation-wide grants</h5>
        <p className="text-sm text-muted-foreground">
          Earlier grants have no expiry. Review and revoke access that is no longer required. Create
          a time-limited grant when replacing legacy access. Keep at least one permanent
          organisation administrator; a temporary administrator cannot replace that permanent
          access.
        </p>
        {legacy.isError ? (
          <RequestFailure message="Could not load earlier access grants." retry={legacy.retry} />
        ) : legacy.isPending ? (
          <p role="status">Loading earlier grants…</p>
        ) : (
          <>
            {legacy.items.length === 0 && (
              <p className="text-sm text-muted-foreground">No earlier grants recorded.</p>
            )}
            {legacy.items.map((row) => (
              <LegacyRow key={row.id} scope={scope} row={row} />
            ))}
            <LoadMore
              hasMore={legacy.hasMore}
              isLoading={legacy.isFetchingMore}
              onClick={legacy.loadMore}
            />
          </>
        )}
      </section>
      <p className="text-xs text-muted-foreground">
        Times use your browser time zone ({Intl.DateTimeFormat().resolvedOptions().timeZone}).
        Effective access status is evaluated by the server. Every time-limited grant requires a
        recorded reason.
      </p>
    </section>
  );
}
function LegacyRow({ scope, row }: { scope: SupplyRegistryScope; row: LegacyGrant }) {
  const { changed } = useSupplyAccessRefresh(scope);
  const [confirm, setConfirm] = useState(false);
  const revoke = useApiMutation(
    () =>
      apiFetch(
        `/supply/parties/${scope.partyId}/access/${encodeURIComponent(row.subject)}/${encodeURIComponent(row.role)}`,
        { method: "DELETE", headers: supplyHeaders(scope) },
      ),
    {
      onSuccess: () => changed(row.subject, true),
      successToast: "Earlier access revoked",
      suppressErrorToast: true,
    },
  );
  return (
    <div className="space-y-2 rounded-lg border p-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="break-words text-sm">
          {row.subject} · {label(row.role)} · No expiry
        </p>
        <Button variant="outline" disabled={revoke.isPending} onClick={() => setConfirm(true)}>
          Review revocation {row.subject} {label(row.role)}
        </Button>
      </div>
      {confirm && (
        <div className="space-y-2">
          <p className="text-sm">
            Revoke this organisation-wide {label(row.role)} grant for {row.subject}?
          </p>
          <div className="flex gap-2">
            <Button disabled={revoke.isPending} onClick={() => revoke.mutate(undefined)}>
              Confirm revoke earlier grant
            </Button>
            <Button variant="ghost" disabled={revoke.isPending} onClick={() => setConfirm(false)}>
              Cancel revocation
            </Button>
          </div>
        </div>
      )}
      {revoke.error && (
        <p role="alert" className="text-sm text-destructive">
          {revoke.error.message}
        </p>
      )}
    </div>
  );
}
function GrantEditor({
  scope,
  grant,
  current,
  onDone,
  onReload,
}: {
  scope: SupplyRegistryScope;
  grant: Grant | null;
  current: Array<Pick<Grant, "subject">>;
  onDone: () => void;
  onReload: () => void;
}) {
  const { changed } = useSupplyAccessRefresh(scope);
  const { can } = usePermissions();
  const id = useRef(grant?.id ?? newSupplyId());
  const policy = useAccessWindowPolicy(scope);
  const requiresPolicy = (value: GrantInput) =>
    value.status !== "revoked" &&
    value.role !== "administrator" &&
    (!grant || Date.parse(value.expires_at) > Date.parse(grant.expires_at));
  const form = useZodForm(
    supplyAccessGrantInputSchema.superRefine((value, ctx) => {
      if (requiresPolicy(value)) validateAccessWindow(value, policy.data?.max_grant_hours, ctx);
    }),
    grant
      ? {
          expected_version: grant.version,
          subject: grant.subject,
          role: grant.role,
          premise_id: grant.premise_id ?? null,
          valid_from: grant.valid_from,
          expires_at: grant.expires_at,
          reason: grant.reason,
          status: grant.status,
        }
      : {
          expected_version: 0,
          subject: "",
          role: "auditor",
          premise_id: null,
          valid_from: new Date().toISOString(),
          expires_at: "",
          reason: "",
          status: "active",
        },
  );
  const members = useApiPagedQuery<Member>(
    ["iam", "members", "supply-grant", scope.tenantId, scope.subject],
    "/iam/members",
    { enabled: can("iam.member.manage") && !grant },
  );
  const premises = useApiPagedQuery<SupplyPremise>(
    supplyKey(scope, "party", scope.partyId, "premises", "grant-options"),
    `/supply/parties/${scope.partyId}/premises`,
    { enabled: can("supply.premise.read"), request: { headers: supplyHeaders(scope) } },
  );
  const subjects = [
    ...new Set([
      ...current.map((row) => row.subject),
      ...(!members.isError ? members.items.map((row) => row.subject) : []),
    ]),
  ];
  const mutation = useApiMutation(
    (values: GrantInput) =>
      apiFetch<Grant>(`/supply/parties/${scope.partyId}/grants/${id.current}`, {
        method: "PUT",
        headers: supplyHeaders(scope),
        body: JSON.stringify(values),
      }),
    {
      successToast: "Access grant saved",
      onSuccess: (saved) => {
        changed(saved.subject, saved.status === "revoked" || saved.effective_status !== "active");
        onDone();
      },
      suppressErrorToast: true,
    },
  );
  const error = (name: keyof GrantInput) => form.formState.errors[name]?.message;
  const selectedScope = form.watch("premise_id");
  const selectedRole = form.watch("role");
  const globalOnly = (supplyAccessScopePolicy.global_only as readonly string[]).includes(
    selectedRole,
  );
  const locationRequired = (
    supplyAccessScopePolicy.location_required as readonly string[]
  ).includes(selectedRole);
  const selected = useApiQuery<SupplyPremise>(
    supplyKey(scope, "party", scope.partyId, "premises", selectedScope, "grant-option"),
    `/supply/parties/${scope.partyId}/premises/${selectedScope}`,
    {
      enabled: !!selectedScope && can("supply.premise.read"),
      gcTime: 0,
      refetchOnWindowFocus: true,
      request: { headers: supplyHeaders(scope) },
    },
  );
  const premiseOptions = premises.items.filter(
    (row) =>
      row.status === "active" &&
      (supplyAccessScopePolicy.premise_kinds as readonly string[]).includes(row.kind),
  );
  if (
    selected.data &&
    !selected.isError &&
    (grant ||
      (selected.data.status === "active" &&
        (supplyAccessScopePolicy.premise_kinds as readonly string[]).includes(
          selected.data.kind,
        ))) &&
    !premiseOptions.some((row) => row.id === selected.data.id)
  )
    premiseOptions.push(selected.data);
  const unavailable =
    form.watch("status") !== "revoked" &&
    !!selectedScope &&
    (!grant || can("supply.premise.read")) &&
    (selected.isError || selected.isPending);
  return (
    <form
      noValidate
      onSubmit={form.handleSubmit((values) => {
        if (!unavailable && !mutation.isPending && (!requiresPolicy(values) || policy.ready))
          mutation.mutate(values);
      })}
      className="space-y-4 rounded-xl bg-muted/30 p-4"
    >
      <h5 className="font-medium">{grant ? "Manage access grant" : "New time-limited grant"}</h5>
      <AccessWindowPolicyStatus policy={policy} />
      {policy.data && (
        <p className="text-xs text-muted-foreground">
          New or extended non-administrator access is limited to{" "}
          {accessWindowLabel(policy.data.max_grant_hours)}. Existing access may be shortened or
          revoked. The server checks the current policy when saving.
        </p>
      )}
      {!can("iam.member.manage") && !grant && (
        <p className="text-sm text-muted-foreground">
          Tenant administrator permission is required to select a new member. You may reuse an
          existing granted member.
        </p>
      )}
      {members.isError && (
        <RequestFailure
          message="Could not load trusted tenant members. Existing granted members remain selectable."
          retry={members.retry}
        />
      )}
      <div className="grid gap-4 sm:grid-cols-2">
        <FormSelectField
          control={form.control}
          name="subject"
          id="grant-subject"
          label="Tenant member"
          disabled={!!grant || mutation.isPending}
          options={subjects.map((subject) => ({ value: subject, label: subject }))}
        />
        <FormSelectField
          control={form.control}
          name="role"
          id="grant-role"
          label="Organisation role"
          disabled={!!grant || mutation.isPending}
          onValueChange={(role) => {
            if ((supplyAccessScopePolicy.global_only as readonly string[]).includes(role))
              form.setValue("premise_id", null, { shouldDirty: true, shouldValidate: true });
          }}
          options={supplyAccessGrantFieldsSchema.shape.role.options.map((role) => ({
            value: role,
            label: label(role),
          }))}
        />
      </div>
      {can("iam.member.manage") && !grant && (
        <LoadMore
          hasMore={members.hasMore}
          isLoading={members.isFetchingMore}
          onClick={members.loadMore}
        />
      )}
      <FormSelectField
        control={form.control}
        name="premise_id"
        id="grant-premise"
        label="Location scope"
        emptyValue={null}
        disabled={
          !!grant ||
          globalOnly ||
          mutation.isPending ||
          premises.isPending ||
          premises.isError ||
          !can("supply.premise.read")
        }
        options={[
          ...(!locationRequired || grant ? [{ value: "", label: "Entire organisation" }] : []),
          ...(grant?.premise_id && !premiseOptions.some((row) => row.id === grant.premise_id)
            ? [{ value: grant.premise_id, label: "Current assigned location" }]
            : []),
          ...premiseOptions.map((row) => ({ value: row.id, label: `${row.code} · ${row.name}` })),
        ]}
      />
      <LoadMore
        hasMore={premises.hasMore}
        isLoading={premises.isFetchingMore}
        onClick={premises.loadMore}
      />
      {premises.isError && (
        <RequestFailure
          message="Could not load location scopes. Refresh before choosing a location."
          retry={premises.retry}
        />
      )}
      {unavailable && (
        <p role="alert" className="text-sm text-destructive">
          The selected location is unavailable. Reload access before saving.
        </p>
      )}
      <div className="grid gap-4 sm:grid-cols-2">
        {(["valid_from", "expires_at"] as const).map((name) => (
          <Field
            key={name}
            id={`grant-${name}`}
            label={name === "valid_from" ? "Valid from" : "Expires at"}
            error={error(name)}
          >
            <Input
              id={`grant-${name}`}
              type="datetime-local"
              disabled={mutation.isPending || (!!grant && name === "valid_from")}
              value={dateInput(form.watch(name))}
              onChange={(event) =>
                form.setValue(name, isoInput(event.target.value), {
                  shouldDirty: true,
                  shouldValidate: true,
                })
              }
              aria-invalid={!!error(name)}
              aria-describedby={error(name) ? `grant-${name}-error` : undefined}
            />
          </Field>
        ))}
      </div>
      <Field id="grant-reason" label="Reason for access" error={error("reason")}>
        <Input
          id="grant-reason"
          {...form.register("reason")}
          disabled={mutation.isPending}
          aria-invalid={!!error("reason")}
          aria-describedby={error("reason") ? "grant-reason-error" : undefined}
        />
      </Field>
      {grant && (
        <FormSelectField
          control={form.control}
          name="status"
          id="grant-status"
          label="Grant status"
          disabled={mutation.isPending}
          options={[
            { value: "active", label: "Active within validity period" },
            { value: "revoked", label: "Revoke access" },
          ]}
        />
      )}
      {mutation.error && (
        <div className="space-y-2">
          <p role="alert" className="text-sm text-destructive">
            {mutation.error.message}
          </p>
          <Button type="button" variant="outline" onClick={onReload}>
            Reload organisation grants
          </Button>
        </div>
      )}
      <div className="flex flex-wrap gap-2">
        <Button
          type="submit"
          disabled={
            mutation.isPending ||
            (requiresPolicy(form.watch()) && !policy.ready) ||
            unavailable ||
            (!grant && !subjects.includes(form.watch("subject")))
          }
        >
          {mutation.isPending ? "Saving…" : "Save access grant"}
        </Button>
        <Button type="button" variant="ghost" disabled={mutation.isPending} onClick={onDone}>
          Cancel grant changes
        </Button>
      </div>
    </form>
  );
}
function GrantHistory({ scope, id }: { scope: SupplyRegistryScope; id: string }) {
  const rows = useApiPagedQuery<{
    id: string;
    version: number;
    snapshot: Grant;
    created_at: string;
  }>(
    supplyKey(scope, "party", scope.partyId, "grants", id, "revisions"),
    `/supply/parties/${scope.partyId}/grants/${id}/revisions`,
    { request: { headers: supplyHeaders(scope) } },
  );
  if (rows.isError)
    return <RequestFailure message="Could not load access grant history." retry={rows.retry} />;
  if (rows.isPending) return <p role="status">Loading access history…</p>;
  return (
    <div className="w-full space-y-2 border-t pt-3">
      {rows.items.map((row) => (
        <p className="break-words text-sm" key={row.id}>
          Version {row.version} · {timestamp(row.created_at)} · {row.snapshot.status} ·{" "}
          {row.snapshot.reason}
        </p>
      ))}
      <LoadMore hasMore={rows.hasMore} isLoading={rows.isFetchingMore} onClick={rows.loadMore} />
    </div>
  );
}
