"use client";

import { useEffect, useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  apiFetch,
  newSupplyId,
  supplyHeaders,
  supplyKey,
  supplySupportRequestInputSchema as supplySupportInputSchema,
  supplySupportDecisionInputSchema,
  supplyAccessScopePolicy,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type SupplyRegistryScope,
  type SupplyPremise,
  type SupplySupportRequest,
  type SupplySupportRequestInput as SupplySupportInput,
  type SupplySupportDecisionInput,
  type SupplySupportDiagnostics,
  type SupplyRegistryRevision,
} from "@helivanta/api";
import { Field, FormSelectField, LoadMore, useZodForm } from "@helivanta/ui";
import { RequestFailure } from "../request-failure";
import { dateTimeInputISO, dateTimeInputValue, formatAbsoluteTime } from "./time-input";

type Member = { id: string; subject: string; role_key: string; created_at: string };
const decisionActions = (actions: string[], manage: boolean, revoke: boolean) =>
  actions.filter((action) =>
    action === "revoke" ? revoke : manage && ["approve", "reject"].includes(action),
  );
const rootFor = (scope: SupplyRegistryScope) => `/supply/parties/${scope.partyId}/support`;
const keyFor = (scope: SupplyRegistryScope, ...extra: unknown[]) =>
  supplyKey(scope, "party", scope.partyId, "support", ...extra);

export function OrganisationSupport({
  scope,
  roles,
}: {
  scope: SupplyRegistryScope;
  roles: string[];
}) {
  const { can } = usePermissions();
  if (!can("supply.support.read") && !can("supply.support.manage")) return null;
  return (
    <SupportPanel
      scope={scope}
      admin={can("supply.support.manage") && roles.includes("administrator")}
    />
  );
}
function SupportPanel({ scope, admin }: { scope: SupplyRegistryScope; admin: boolean }) {
  const { can } = usePermissions();
  const [creating, setCreating] = useState(false);
  const [diagnostics, setDiagnostics] = useState<string | null>(null);
  const [history, setHistory] = useState<string | null>(null);
  const [review, setReview] = useState<string | null>(null);
  const target = useRef<HTMLDivElement>(null);
  const rows = useApiPagedQuery<SupplySupportRequest>(keyFor(scope), rootFor(scope), {
    request: { headers: supplyHeaders(scope) },
  });
  return (
    <section aria-label="Support access requests" className="space-y-5 rounded-xl border p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h4 className="font-semibold">Support access</h4>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            Time-limited diagnostic access requires a ticket, an explicit location and approval by
            an independent administrator. Approval does not create an organisation role.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="ghost" onClick={rows.retry}>
            Refresh support requests
          </Button>
          {admin && !rows.isError && (
            <Button
              variant="outline"
              onClick={() => {
                setCreating(true);
                requestAnimationFrame(() => {
                  target.current?.scrollIntoView({ block: "start", behavior: "instant" });
                  target.current?.querySelector<HTMLElement>("[role=combobox],input")?.focus();
                });
              }}
            >
              Request support access
            </Button>
          )}
        </div>
      </div>
      {rows.isError ? (
        <RequestFailure
          message="Support requests are unavailable with your current access. Refresh before continuing."
          retry={rows.retry}
        />
      ) : rows.isPending ? (
        <p role="status">Loading support requests…</p>
      ) : (
        <>
          {rows.items.length === 0 && (
            <p className="text-sm text-muted-foreground">
              No support access requests are available.
            </p>
          )}
          <ul className="space-y-3">
            {rows.items.map((row) => (
              <li key={row.id} className="space-y-3 rounded-lg border p-4">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0">
                    <h5 className="break-words font-medium">{row.ticket_reference}</h5>
                    <p className="text-sm capitalize">
                      {row.effective_status.replaceAll("_", " ")} · Version {row.version}
                    </p>
                    <p className="mt-1 text-sm text-muted-foreground">
                      {formatAbsoluteTime(row.valid_from)} to {formatAbsoluteTime(row.expires_at)}
                    </p>
                    <p className="mt-1 break-words text-sm">
                      Requested by {row.requester_subject} · Beneficiary {row.beneficiary_subject}
                    </p>
                    <p className="mt-1 break-words text-sm text-muted-foreground">{row.reason}</p>
                    {row.approver_subject && (
                      <p className="mt-1 break-words text-sm text-muted-foreground">
                        Independent approver: {row.approver_subject}
                      </p>
                    )}
                  </div>
                  <div className="flex flex-wrap items-start gap-2">
                    {decisionActions(
                      row.allowed_actions,
                      can("supply.support.manage"),
                      can("supply.support.revoke"),
                    ).length > 0 && (
                      <Button
                        variant="outline"
                        aria-label={`Review support ${row.ticket_reference}`}
                        onClick={() => setReview(review === row.id ? null : row.id)}
                      >
                        Review request
                      </Button>
                    )}
                    {row.allowed_actions.includes("read_diagnostics") && (
                      <Button
                        variant="outline"
                        aria-label={`Open diagnostics ${row.ticket_reference}`}
                        onClick={() => setDiagnostics(diagnostics === row.id ? null : row.id)}
                      >
                        Open read-only diagnostics
                      </Button>
                    )}
                    {admin && (
                      <Button
                        variant="ghost"
                        aria-label={`History support ${row.ticket_reference}`}
                        onClick={() => setHistory(history === row.id ? null : row.id)}
                      >
                        History
                      </Button>
                    )}
                  </div>
                </div>
                {review === row.id &&
                  decisionActions(
                    row.allowed_actions,
                    can("supply.support.manage"),
                    can("supply.support.revoke"),
                  ).length > 0 && (
                    <SupportDecision
                      key={`${row.id}:${row.version}`}
                      scope={scope}
                      row={row}
                      onDone={() => setReview(null)}
                    />
                  )}
                {diagnostics === row.id && row.allowed_actions.includes("read_diagnostics") && (
                  <SupportDiagnostics scope={scope} row={row} />
                )}
                {admin && history === row.id && <SupportHistory scope={scope} id={row.id} />}
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
        {admin && creating && !rows.isError && (
          <SupportEditor scope={scope} existing={rows.items} onDone={() => setCreating(false)} />
        )}
      </div>
      <p className="text-xs text-muted-foreground">
        Validity and available actions come from the server. Times show your browser time zone (
        {Intl.DateTimeFormat().resolvedOptions().timeZone}). Diagnostic sessions cannot edit
        records.
      </p>
    </section>
  );
}
function SupportEditor({
  scope,
  existing,
  onDone,
}: {
  scope: SupplyRegistryScope;
  existing: SupplySupportRequest[];
  onDone: () => void;
}) {
  const { can } = usePermissions();
  const id = useRef(newSupplyId());
  const form = useZodForm(supplySupportInputSchema, {
    expected_version: 0,
    beneficiary_subject: "",
    premise_id: "",
    ticket_reference: "",
    reason: "",
    valid_from: new Date().toISOString(),
    expires_at: "",
  });
  const members = useApiPagedQuery<Member>(
    ["iam", "members", "support", scope.tenantId, scope.subject],
    "/iam/members",
    { enabled: can("iam.member.manage"), request: { headers: supplyHeaders(scope) } },
  );
  const premises = useApiPagedQuery<SupplyPremise>(
    keyFor(scope, "premise-options"),
    `/supply/parties/${scope.partyId}/premises`,
    { enabled: can("supply.premise.read"), request: { headers: supplyHeaders(scope) } },
  );
  const subjects = [
    ...new Set([
      scope.subject,
      ...existing.map((row) => row.beneficiary_subject),
      ...(!members.isError ? members.items.map((row) => row.subject) : []),
    ]),
  ];
  const save = useApiMutation(
    (value: SupplySupportInput) =>
      apiFetch<SupplySupportRequest>(`${rootFor(scope)}/${id.current}`, {
        method: "PUT",
        headers: supplyHeaders(scope),
        body: JSON.stringify(value),
      }),
    {
      invalidate: [keyFor(scope)],
      successToast: "Support request recorded",
      onSuccess: onDone,
      suppressErrorToast: true,
    },
  );
  const error = (name: keyof SupplySupportInput) => form.formState.errors[name]?.message;
  return (
    <form
      noValidate
      aria-label="Support request form"
      className="space-y-4 rounded-xl bg-muted/30 p-4"
      onSubmit={form.handleSubmit((value) => {
        if (!save.isPending && !premises.isError) save.mutate(value);
      })}
    >
      <h5 className="font-medium">New support access request</h5>
      <div className="grid gap-4 sm:grid-cols-2">
        <FormSelectField
          control={form.control}
          name="beneficiary_subject"
          id="support-beneficiary"
          label="Support beneficiary"
          options={subjects.map((subject) => ({ value: subject, label: subject }))}
          disabled={save.isPending}
        />
        <FormSelectField
          control={form.control}
          name="premise_id"
          id="support-premise"
          label="Location to investigate"
          options={premises.items
            .filter(
              (row) =>
                row.status === "active" &&
                (supplyAccessScopePolicy.premise_kinds as readonly string[]).includes(row.kind),
            )
            .map((row) => ({ value: row.id, label: `${row.code} · ${row.name}` }))}
          disabled={
            save.isPending || premises.isPending || premises.isError || !can("supply.premise.read")
          }
        />
      </div>
      {!can("iam.member.manage") && (
        <p className="text-sm text-muted-foreground">
          Tenant administrator permission is required to choose a new tenant member. Existing
          support beneficiaries remain selectable.
        </p>
      )}
      {members.isError && (
        <RequestFailure message="Could not load trusted tenant members." retry={members.retry} />
      )}
      {premises.isError && (
        <RequestFailure message="Could not load support locations." retry={premises.retry} />
      )}
      {!can("supply.premise.read") && (
        <p className="text-sm text-muted-foreground">
          Location read permission is required to request support access.
        </p>
      )}
      <LoadMore
        hasMore={members.hasMore}
        isLoading={members.isFetchingMore}
        onClick={members.loadMore}
      />
      <LoadMore
        hasMore={premises.hasMore}
        isLoading={premises.isFetchingMore}
        onClick={premises.loadMore}
      />
      <Field id="support-ticket" label="Support ticket reference" error={error("ticket_reference")}>
        <Input
          id="support-ticket"
          {...form.register("ticket_reference")}
          disabled={save.isPending}
          aria-invalid={!!error("ticket_reference")}
          aria-describedby={error("ticket_reference") ? "support-ticket-error" : undefined}
        />
      </Field>
      <Field id="support-reason" label="Reason for investigation" error={error("reason")}>
        <Input
          id="support-reason"
          {...form.register("reason")}
          disabled={save.isPending}
          aria-invalid={!!error("reason")}
          aria-describedby={error("reason") ? "support-reason-error" : undefined}
        />
      </Field>
      <div className="grid gap-4 sm:grid-cols-2">
        {(["valid_from", "expires_at"] as const).map((name) => (
          <Field
            key={name}
            id={`support-${name}`}
            label={name === "valid_from" ? "Support starts at" : "Support expires at"}
            error={error(name)}
          >
            <Input
              id={`support-${name}`}
              type="datetime-local"
              value={dateTimeInputValue(form.watch(name))}
              onChange={(event) =>
                form.setValue(name, dateTimeInputISO(event.target.value), {
                  shouldDirty: true,
                  shouldValidate: true,
                })
              }
              disabled={save.isPending}
              aria-invalid={!!error(name)}
              aria-describedby={error(name) ? `support-${name}-error` : undefined}
            />
          </Field>
        ))}
      </div>
      <p className="text-xs text-muted-foreground">
        Choose a window of no more than 24 hours. Another administrator must approve; the requester
        and beneficiary cannot approve this request.
      </p>
      {save.error && (
        <p role="alert" className="text-sm text-destructive">
          {save.error.message}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <Button
          type="submit"
          disabled={save.isPending || premises.isError || !can("supply.premise.read")}
        >
          {save.isPending ? "Recording…" : "Submit support request"}
        </Button>
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onDone}>
          Cancel support request
        </Button>
      </div>
    </form>
  );
}
function SupportDecision({
  scope,
  row,
  onDone,
}: {
  scope: SupplyRegistryScope;
  row: SupplySupportRequest;
  onDone: () => void;
}) {
  const { can } = usePermissions();
  const actions = decisionActions(
    row.allowed_actions,
    can("supply.support.manage"),
    can("supply.support.revoke"),
  );
  const form = useZodForm(supplySupportDecisionInputSchema, {
    expected_version: row.version,
    action: actions[0] as SupplySupportDecisionInput["action"],
    reason: "",
  });
  const save = useApiMutation(
    (value: SupplySupportDecisionInput) =>
      apiFetch<SupplySupportRequest>(
        `${rootFor(scope)}/${row.id}/${value.action === "revoke" ? "revoke" : "decision"}`,
        {
          method: "POST",
          headers: supplyHeaders(scope),
          body: JSON.stringify(
            value.action === "revoke"
              ? { expected_version: value.expected_version, reason: value.reason }
              : value,
          ),
        },
      ),
    {
      invalidate: [keyFor(scope)],
      successToast: "Support decision recorded",
      onSuccess: onDone,
      suppressErrorToast: true,
    },
  );
  return (
    <form
      noValidate
      aria-label={`Decision for ${row.ticket_reference}`}
      className="space-y-3 border-t pt-3"
      onSubmit={form.handleSubmit((value) => {
        if (actions.includes(value.action) && !save.isPending) save.mutate(value);
      })}
    >
      <FormSelectField
        control={form.control}
        name="action"
        id={`support-decision-${row.id}`}
        label="Decision"
        options={actions.map((action) => ({
          value: action,
          label: action[0].toUpperCase() + action.slice(1),
        }))}
        disabled={save.isPending}
      />
      <Field
        id={`support-decision-reason-${row.id}`}
        label="Decision reason"
        error={form.formState.errors.reason?.message}
      >
        <Input
          id={`support-decision-reason-${row.id}`}
          {...form.register("reason")}
          disabled={save.isPending}
        />
      </Field>
      {save.error && (
        <p role="alert" className="text-sm text-destructive">
          {save.error.message} Reload the request if its version or approval changed.
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={save.isPending || actions.length === 0}>
          {save.isPending ? "Recording…" : "Record support decision"}
        </Button>
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onDone}>
          Close support review
        </Button>
      </div>
    </form>
  );
}
function SupportDiagnostics({
  scope,
  row,
}: {
  scope: SupplyRegistryScope;
  row: SupplySupportRequest;
}) {
  const [expiredAt, setExpiredAt] = useState<string | null>(() =>
    Date.now() >= Date.parse(row.expires_at) ? row.expires_at : null,
  );
  const expiry = Date.parse(row.expires_at);
  const expired =
    expiredAt === row.expires_at || !Number.isFinite(expiry) || row.effective_status === "expired";
  const result = useApiQuery<SupplySupportDiagnostics>(
    keyFor(scope, row.id, "diagnostics"),
    `${rootFor(scope)}/${row.id}/diagnostics`,
    {
      enabled: !expired,
      gcTime: 0,
      refetchOnWindowFocus: true,
      request: { headers: supplyHeaders(scope) },
    },
  );
  const refetch = result.refetch;
  useEffect(() => {
    if (!Number.isFinite(expiry)) return;
    let timer: ReturnType<typeof setTimeout>;
    const schedule = () => {
      const remaining = expiry - Date.now();
      if (remaining <= 0) {
        setExpiredAt(row.expires_at);
        void refetch();
      } else timer = setTimeout(schedule, Math.min(remaining, 2_147_483_647));
    };
    schedule();
    return () => clearTimeout(timer);
  }, [expiry, row.expires_at, refetch]);
  if (expired)
    return (
      <p role="status">Support session has expired. Request a new approved session to continue.</p>
    );
  if (result.isError)
    return (
      <RequestFailure
        message="This diagnostic session is no longer available. It may have expired, been revoked or lost its approval."
        retry={result.refetch}
      />
    );
  if (!result.data) return <p role="status">Loading read-only diagnostics…</p>;
  return (
    <section
      aria-label="Read-only support diagnostics"
      className="space-y-3 rounded-lg border bg-background p-4"
    >
      <h6 className="font-medium">Read-only location diagnostics</h6>
      <p className="text-sm text-muted-foreground">
        Approved ticket {row.ticket_reference} · Expires {formatAbsoluteTime(row.expires_at)}
      </p>
      <dl className="grid gap-3 text-sm sm:grid-cols-2">
        {Object.entries(result.data)
          .filter(([key]) =>
            [
              "premise_kind",
              "premise_status",
              "bin_count",
              "active_bin_count",
              "quarantine_bin_count",
            ].includes(key),
          )
          .map(([name, value]) => (
            <div key={name}>
              <dt className="capitalize text-muted-foreground">{name.replaceAll("_", " ")}</dt>
              <dd className="font-medium">{String(value)}</dd>
            </div>
          ))}
      </dl>
    </section>
  );
}
function SupportHistory({ scope, id }: { scope: SupplyRegistryScope; id: string }) {
  const rows = useApiPagedQuery<SupplyRegistryRevision<SupplySupportRequest>>(
    keyFor(scope, id, "revisions"),
    `${rootFor(scope)}/${id}/revisions`,
    { poll: true, request: { headers: supplyHeaders(scope) } },
  );
  if (rows.isError)
    return <RequestFailure message="Could not load support access history." retry={rows.retry} />;
  if (rows.isPending) return <p role="status">Loading support history…</p>;
  return (
    <section aria-label="Support request history" className="space-y-2 border-t pt-3">
      {rows.items.map((row) => (
        <div className="text-sm" key={row.id}>
          <p>
            Version {row.version} · {formatAbsoluteTime(row.created_at)} · Actor {row.actor_subject}
          </p>
          <p className="break-words text-muted-foreground">
            {row.snapshot.status} · {row.snapshot.decision_reason || row.snapshot.reason}
          </p>
        </div>
      ))}
      <LoadMore hasMore={rows.hasMore} isLoading={rows.isFetchingMore} onClick={rows.loadMore} />
    </section>
  );
}
