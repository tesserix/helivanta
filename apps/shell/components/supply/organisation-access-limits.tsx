"use client";

import { useCallback, useEffect, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  supplyAccessLimitsInputSchema,
  supplyHeaders,
  supplyKey,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  useSupplyAccessRefresh,
  type SupplyAccessLimits,
  type SupplyAccessLimitsInput,
  type SupplyRegistryRevision,
  type SupplyRegistryScope,
} from "@helivanta/api";
import { Controller, Field, LoadMore, useZodForm } from "@helivanta/ui";
import { RequestFailure } from "../request-failure";
import { formatAbsoluteTime } from "./time-input";

const rootFor = (scope: SupplyRegistryScope) => `/supply/parties/${scope.partyId}/access-limits`;
const keyFor = (scope: SupplyRegistryScope, ...parts: unknown[]) =>
  supplyKey(scope, "party", scope.partyId, "access-limits", ...parts);

export function OrganisationAccessLimits({ scope }: { scope: SupplyRegistryScope }) {
  const { denied } = useSupplyAccessRefresh(scope);
  const [editing, setEditing] = useState(false);
  const [history, setHistory] = useState(false);
  const policy = useApiQuery<SupplyAccessLimits>(keyFor(scope), rootFor(scope), {
    gcTime: 0,
    refetchOnWindowFocus: true,
    request: { headers: supplyHeaders(scope) },
  });
  useEffect(() => {
    if (policy.error instanceof ApiError && [401, 403, 404].includes(policy.error.status)) denied();
  }, [policy.error, denied]);
  if (policy.isError)
    return (
      <RequestFailure
        message="Access limits are unavailable. Refresh your organisation access before continuing."
        retry={policy.refetch}
      />
    );
  if (!policy.data) return <p role="status">Loading organisation access limits…</p>;
  const row = policy.data;
  return (
    <section
      aria-label="Organisation access limits"
      className="space-y-4 rounded-xl sm:border sm:bg-muted/20 sm:p-4"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h5 className="font-semibold">Access duration limits</h5>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            Set shorter windows for new or extended non-administrator grants and new support
            requests or approvals. Existing grants keep their recorded expiry; revoke them
            explicitly to end access sooner.
          </p>
        </div>
        <Button variant="outline" onClick={() => setEditing(true)}>
          Edit access limits
        </Button>
      </div>
      <dl className="grid gap-3 text-sm sm:grid-cols-2">
        <div>
          <dt className="text-muted-foreground">Grant duration ceiling</dt>
          <dd className="font-medium">{row.max_grant_hours} hours</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Support duration ceiling</dt>
          <dd className="font-medium">{row.max_support_hours} hours</dd>
        </div>
      </dl>
      <p className="text-xs text-muted-foreground">
        Independent support approval remains mandatory. Platform ceilings of 8,784 hours (366 days)
        for grants and 24 hours for support cannot be increased.
        {row.version === 0 ? " Platform defaults apply." : ` Policy version ${row.version}.`}
      </p>
      {row.reason && (
        <p className="break-words text-sm text-muted-foreground">Last change: {row.reason}</p>
      )}
      <div className="scroll-mt-32">
        {editing && (
          <LimitsEditor
            key={row.version}
            scope={scope}
            row={row}
            onDone={() => setEditing(false)}
            onReload={() => {
              setEditing(false);
              void policy.refetch();
            }}
          />
        )}
      </div>
      <Button variant="ghost" onClick={() => setHistory(!history)}>
        {history ? "Hide access limit history" : "View access limit history"}
      </Button>
      {history && <LimitsHistory scope={scope} />}
    </section>
  );
}

function LimitsEditor({
  scope,
  row,
  onDone,
  onReload,
}: {
  scope: SupplyRegistryScope;
  row: SupplyAccessLimits;
  onDone: () => void;
  onReload: () => void;
}) {
  const { denied } = useSupplyAccessRefresh(scope);
  const reveal = useCallback((element: HTMLFormElement | null) => {
    if (!element) return;
    element.scrollIntoView({ block: "start", behavior: "instant" });
    element.querySelector<HTMLInputElement>("input")?.focus({ preventScroll: true });
  }, []);
  const form = useZodForm(supplyAccessLimitsInputSchema, {
    expected_version: row.version,
    max_grant_hours: row.max_grant_hours,
    max_support_hours: row.max_support_hours,
    reason: "",
  });
  const save = useApiMutation(
    (values: SupplyAccessLimitsInput) =>
      apiFetch<SupplyAccessLimits>(rootFor(scope), {
        method: "PUT",
        headers: supplyHeaders(scope),
        body: JSON.stringify(values),
      }),
    {
      invalidate: [keyFor(scope)],
      successToast: "Access limits saved",
      suppressErrorToast: true,
      onSuccess: onDone,
    },
  );
  useEffect(() => {
    if (save.error instanceof ApiError && [401, 403, 404].includes(save.error.status)) denied();
  }, [save.error, denied]);
  if (save.error instanceof ApiError && [401, 403, 404].includes(save.error.status))
    return (
      <RequestFailure
        message="You can no longer change access limits. Refresh your organisation access."
        retry={onReload}
      />
    );
  return (
    <form
      ref={reveal}
      noValidate
      onSubmit={form.handleSubmit((values) => {
        if (!save.isPending) save.mutate(values);
      })}
      className="scroll-mt-32 space-y-4 rounded-xl border bg-background p-3 sm:p-4"
    >
      <h6 className="font-medium">Edit organisation access limits</h6>
      <div className="grid gap-4 sm:grid-cols-2">
        {(
          [
            ["max_grant_hours", "Maximum grant duration (hours)"],
            ["max_support_hours", "Maximum support duration (hours)"],
          ] as const
        ).map(([name, label]) => (
          <Controller
            key={name}
            control={form.control}
            name={name}
            render={({ field, fieldState }) => (
              <Field id={`limits-${name}`} label={label} error={fieldState.error?.message}>
                <Input
                  {...field}
                  id={`limits-${name}`}
                  type="number"
                  min={supplyAccessLimitsInputSchema.shape[name].minValue ?? undefined}
                  max={supplyAccessLimitsInputSchema.shape[name].maxValue ?? undefined}
                  step={1}
                  value={Number.isFinite(field.value) ? field.value : ""}
                  onChange={(event) => field.onChange(event.target.valueAsNumber)}
                  disabled={save.isPending}
                  aria-invalid={fieldState.invalid}
                  aria-describedby={fieldState.error ? `limits-${name}-error` : undefined}
                />
              </Field>
            )}
          />
        ))}
      </div>
      <Field
        id="limits-reason"
        label="Reason for policy change"
        error={form.formState.errors.reason?.message}
      >
        <Input
          id="limits-reason"
          {...form.register("reason")}
          disabled={save.isPending}
          aria-invalid={!!form.formState.errors.reason}
          aria-describedby={form.formState.errors.reason ? "limits-reason-error" : undefined}
        />
      </Field>
      {save.error && (
        <div className="space-y-2">
          <p role="alert" className="text-sm text-destructive">
            {save.error.message}
          </p>
          <Button type="button" variant="outline" onClick={onReload}>
            Reload access limits
          </Button>
        </div>
      )}
      <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap">
        <Button type="submit" className="w-full sm:w-auto" disabled={save.isPending}>
          {save.isPending ? "Saving…" : "Save access limits"}
        </Button>
        <Button
          type="button"
          variant="ghost"
          className="w-full sm:w-auto"
          disabled={save.isPending}
          onClick={onDone}
        >
          Cancel limit changes
        </Button>
      </div>
    </form>
  );
}

function LimitsHistory({ scope }: { scope: SupplyRegistryScope }) {
  const { denied } = useSupplyAccessRefresh(scope);
  const rows = useApiPagedQuery<SupplyRegistryRevision<SupplyAccessLimits>>(
    keyFor(scope, "revisions"),
    `${rootFor(scope)}/revisions`,
    { request: { headers: supplyHeaders(scope) } },
  );
  useEffect(() => {
    if (rows.error instanceof ApiError && [401, 403, 404].includes(rows.error.status)) denied();
  }, [rows.error, denied]);
  if (rows.isError)
    return <RequestFailure message="Could not load access limit history." retry={rows.retry} />;
  if (rows.isPending) return <p role="status">Loading access limit history…</p>;
  return (
    <section aria-label="Access limit history" className="space-y-3 border-t pt-4">
      {rows.items.length === 0 && (
        <p className="text-sm text-muted-foreground">No organisation-specific changes recorded.</p>
      )}
      {rows.items.map((revision) => (
        <article key={revision.id} className="space-y-1 text-sm">
          <p>
            Version {revision.version} · {formatAbsoluteTime(revision.created_at)} · Actor{" "}
            {revision.actor_subject}
          </p>
          <p>
            {revision.snapshot.max_grant_hours} grant hours · {revision.snapshot.max_support_hours}{" "}
            support hours
          </p>
          <p className="break-words text-muted-foreground">{revision.snapshot.reason}</p>
        </article>
      ))}
      <LoadMore hasMore={rows.hasMore} isLoading={rows.isFetchingMore} onClick={rows.loadMore} />
    </section>
  );
}
