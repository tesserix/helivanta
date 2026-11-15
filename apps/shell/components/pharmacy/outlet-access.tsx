"use client";

import { useState } from "react";
import { z } from "zod";
import { Button } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  pharmacyKey,
  pharmacyRoot,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  type PharmacyScope,
} from "@helivanta/api";
import {
  ConfirmDialog,
  FormSelectField,
  LoadMore,
  Panel,
  RequestFailure,
  SelectControl,
  useZodForm,
} from "@helivanta/ui";
import { useReportState, type FormState } from "@/lib/use-list-detail";

type Level = "reader" | "editor";
type Grant = { id: string; subject: string; level: Level; created_at: string; updated_at: string };
type Member = { id: string; subject: string; role_key: string; created_at: string };

const levels = [
  { value: "editor", label: "Editor" },
  { value: "reader", label: "Reader" },
];

const grantSchema = z.object({
  subject: z.string().min(1, "Choose a person."),
  level: z.enum(["reader", "editor"]),
  confirmed: z.boolean().refine(Boolean, "Confirm the outlet before granting access."),
});

// This outlet's access: the caller's own level and, for whoever manages
// access, every grant here (#1718). 404 is the server's settled "no grant
// here" (#1285).
export function OutletAccess({
  scope,
  subject,
  outletName,
  canManage,
  onStateChange,
}: {
  scope: PharmacyScope;
  subject: string;
  outletName: string;
  canManage: boolean;
  onStateChange?: FormState;
}) {
  const access = useApiQuery<{ level: Level }>(
    pharmacyKey(scope, "access"),
    `${pharmacyRoot(scope)}/access`,
    { gcTime: 0, refetchOnWindowFocus: true },
  );
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
          {canManage ? "" : " Ask your administrator to grant it."}
        </p>
      ) : access.isError ? (
        <RequestFailure
          message="Could not check your access to this outlet. Try again in a moment."
          retry={access.refetch}
          error={access.error}
        />
      ) : (
        <p className="text-sm">
          Your access here: <span className="font-medium capitalize">{access.data?.level}</span>
        </p>
      )}
      {canManage && (
        <ManageAccess
          scope={scope}
          subject={subject}
          outletName={outletName}
          onStateChange={onStateChange}
        />
      )}
    </Panel>
  );
}

function ManageAccess({
  scope,
  subject,
  outletName,
  onStateChange,
}: {
  scope: PharmacyScope;
  subject: string;
  outletName: string;
  onStateChange?: FormState;
}) {
  const grants = useApiPagedQuery<Grant>(
    pharmacyKey(scope, "access-grants"),
    `${pharmacyRoot(scope)}/access-grants`,
    { gcTime: 0 },
  );
  const members = useApiPagedQuery<Member>(
    ["iam", "members", "pharmacy-grant", scope.tenantId, subject],
    "/iam/members",
  );
  // Only a tenant administrator may list members; anyone else can still
  // manage the grants already here and grant themselves.
  const membersRefused = members.error instanceof ApiError && members.error.status === 403;
  const granted = new Set(grants.items.map((row) => row.subject));
  const choices = [
    ...new Set([subject, ...(members.isError ? [] : members.items.map((row) => row.subject))]),
  ].filter((choice) => choice && !granted.has(choice));
  return (
    <div className="space-y-4">
      <section aria-labelledby="outlet-access-list-heading" className="space-y-2">
        <h4 id="outlet-access-list-heading" className="text-sm font-medium">
          Who has access
        </h4>
        {grants.isPending ? (
          <p role="status">Loading who has access…</p>
        ) : grants.isError ? (
          <RequestFailure
            message="Could not load who has access to this outlet. Try again in a moment."
            retry={grants.retry}
            error={grants.error}
          />
        ) : grants.items.length === 0 ? (
          <p className="text-sm text-muted-foreground">Nobody has access to this outlet yet.</p>
        ) : (
          <ul aria-labelledby="outlet-access-list-heading" className="divide-y">
            {grants.items.map((row) => (
              <GrantRow
                key={row.subject}
                scope={scope}
                row={row}
                mine={row.subject === subject}
                outletName={outletName}
              />
            ))}
          </ul>
        )}
        <LoadMore
          hasMore={grants.hasMore}
          isLoading={grants.isFetchingMore}
          onClick={grants.loadMore}
        />
      </section>
      {membersRefused ? (
        <p className="text-sm text-muted-foreground">
          Only a tenant administrator can add a new person here. You can still change or remove
          existing access.
        </p>
      ) : (
        members.isError && (
          <RequestFailure
            message="Could not load tenant members. You can still grant your own account."
            retry={members.retry}
            error={members.error}
          />
        )
      )}
      <GrantForm
        scope={scope}
        outletName={outletName}
        choices={choices}
        onStateChange={onStateChange}
      />
      {!members.isError && (
        <LoadMore
          hasMore={members.hasMore}
          isLoading={members.isFetchingMore}
          onClick={members.loadMore}
        />
      )}
    </div>
  );
}

// Every change refetches both the list and the caller's own access, since
// a change to your own row changes what you can do here.
function useAccessChange(scope: PharmacyScope, successToast: string) {
  return useApiMutation(
    ({ subject, level }: { subject: string; level?: Level }) =>
      apiFetch(`${pharmacyRoot(scope)}/access/${encodeURIComponent(subject)}`, {
        method: level ? "PUT" : "DELETE",
        ...(level ? { body: JSON.stringify({ level }) } : {}),
      }),
    {
      successToast,
      invalidate: [pharmacyKey(scope, "access"), pharmacyKey(scope, "access-grants")],
      suppressErrorToast: true,
    },
  );
}

function GrantRow({
  scope,
  row,
  mine,
  outletName,
}: {
  scope: PharmacyScope;
  row: Grant;
  mine: boolean;
  outletName: string;
}) {
  const [confirm, setConfirm] = useState(false);
  const change = useAccessChange(scope, "Access changed");
  const revoke = useAccessChange(scope, "Access removed");
  const busy = change.isPending || revoke.isPending;
  const error = change.error ?? revoke.error;
  return (
    <li className="space-y-2 p-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="break-words text-sm font-medium">
          {row.subject}
          {mine && <span className="ml-2 text-xs text-muted-foreground">You</span>}
        </p>
        <div className="flex items-center gap-2">
          <SelectControl
            aria-label={`Access level for ${row.subject}`}
            value={row.level}
            options={levels}
            disabled={busy}
            onValueChange={(level) => {
              if (level !== row.level)
                change.mutate({ subject: row.subject, level: level as Level });
            }}
          />
          <Button variant="outline" disabled={busy} onClick={() => setConfirm(true)}>
            Remove <span className="sr-only">{row.subject}</span>
          </Button>
        </div>
      </div>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error.message}
        </p>
      )}
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title="Remove access"
        description={`Remove ${row.subject}'s access to ${outletName}?${mine ? " You will lose access to this outlet." : ""}`}
        confirmLabel="Remove access"
        busy={revoke.isPending}
        onConfirm={() =>
          revoke.mutate({ subject: row.subject }, { onSettled: () => setConfirm(false) })
        }
      />
    </li>
  );
}

function GrantForm({
  scope,
  outletName,
  choices,
  onStateChange,
}: {
  scope: PharmacyScope;
  outletName: string;
  choices: string[];
  onStateChange?: FormState;
}) {
  const form = useZodForm(grantSchema, { subject: "", level: "editor", confirmed: false });
  const grant = useAccessChange(scope, "Pharmacy access granted");
  useReportState(onStateChange, form.formState.isDirty, grant.isPending);
  return (
    <form
      noValidate
      className="space-y-4"
      onSubmit={form.handleSubmit(({ subject, level }) =>
        grant.mutate(
          { subject, level },
          { onSuccess: () => form.reset({ subject: "", level: "editor", confirmed: false }) },
        ),
      )}
    >
      <h4 className="text-sm font-medium">Grant access</h4>
      <p className="text-sm text-muted-foreground">
        Grant catalogue and receiving access at {outletName}. This does not approve medicines or
        confer hospital clinical access.
      </p>
      <div className="grid gap-4 sm:grid-cols-2">
        <FormSelectField
          control={form.control}
          name="subject"
          id="outlet-grant-subject"
          label="Person"
          disabled={grant.isPending}
          options={choices.map((choice) => ({ value: choice, label: choice }))}
        />
        <FormSelectField
          control={form.control}
          name="level"
          id="outlet-grant-level"
          label="Level"
          disabled={grant.isPending}
          options={levels}
        />
      </div>
      <label className="flex items-start gap-3 text-sm">
        <input type="checkbox" className="mt-1" {...form.register("confirmed")} />
        Confirm this is the outlet to grant access at
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
      <Button type="submit" disabled={grant.isPending}>
        Grant pharmacy access
      </Button>
    </form>
  );
}
