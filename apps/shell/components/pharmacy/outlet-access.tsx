"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { Button } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  accountProfileSchema,
  readMemberPresentations,
  type MemberPresentation,
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
  RefreshButton,
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
  const subjects = [...new Set([subject, ...members.items.map((row) => row.subject)])].sort();
  const identities = useQuery({
    queryKey: [
      "iam",
      "member-presentations",
      scope.tenantId,
      subject,
      subjects,
      members.items.map((row) => row.id),
    ],
    queryFn: ({ signal }) =>
      readMemberPresentations({ tenantId: scope.tenantId, subject }, subjects, signal),
    enabled: !members.isPending && !members.isError && !grants.isPending && !grants.isError,
    gcTime: 0,
    staleTime: 0,
    retry: false,
  });
  const additionalSubjects = grants.items
    .map((row) => row.subject)
    .filter((value) => !subjects.includes(value))
    .sort();
  const additionalIdentities = useQuery({
    queryKey: [
      "iam",
      "member-presentations",
      scope.tenantId,
      subject,
      "existing-grants",
      additionalSubjects,
    ],
    queryFn: ({ signal }) =>
      readMemberPresentations({ tenantId: scope.tenantId, subject }, additionalSubjects, signal),
    enabled:
      additionalSubjects.length > 0 &&
      !members.isPending &&
      !members.isError &&
      !grants.isPending &&
      !grants.isError,
    gcTime: 0,
    staleTime: 0,
    retry: false,
  });
  const identityAuthorityLost = [identities.error, additionalIdentities.error].some(
    (error) => error instanceof ApiError && [401, 403, 409].includes(error.status),
  );
  const own = useApiQuery(["iam", "me", "profile", scope.tenantId, subject], "/iam/me/profile", {
    gcTime: 0,
    enabled: members.isError || identityAuthorityLost,
  });
  const profile = accountProfileSchema.safeParse(own.data);
  const self: MemberPresentation | undefined =
    !own.isError &&
    !own.isPending &&
    profile.success &&
    profile.data.subject === subject &&
    profile.data.tenant_id === scope.tenantId &&
    profile.data.display_name &&
    profile.data.roles.length
      ? {
          subject,
          available: true,
          name: profile.data.display_name,
          email: profile.data.email,
          roles: profile.data.roles,
        }
      : undefined;
  const people =
    !identityAuthorityLost && !members.isError && !identities.isError
      ? (identities.data ?? [])
      : self
        ? [self]
        : [];
  const choices = people
    .filter((person) => person.available && !granted.has(person.subject))
    .map((person) => ({ value: person.subject, label: personLabel(person) }));
  const additionalPeople =
    identityAuthorityLost || members.isError || additionalIdentities.isError
      ? []
      : (additionalIdentities.data ?? []);
  const details = new Map(
    [...people, ...additionalPeople].map((person) => [person.subject, person]),
  );
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
                person={details.get(row.subject)}
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
        disabled={
          grants.isError ||
          grants.isPending ||
          (!members.isError &&
            !identityAuthorityLost &&
            (identities.isPending || identities.isError))
        }
        onStateChange={onStateChange}
      />
      {!members.isError && identities.isError && (
        <RequestFailure
          message="Could not load account names and roles."
          error={identities.error}
          retry={identities.refetch}
        />
      )}
      {!members.isError && additionalIdentities.isError && (
        <RequestFailure
          message="Could not load all existing account details."
          refused="Some existing account details are unavailable."
          error={additionalIdentities.error}
          retry={additionalIdentities.refetch}
        />
      )}
      {!members.isError && identities.isPending && (
        <p role="status">Loading account names and roles…</p>
      )}
      {!identities.isError && identities.data?.some((person) => !person.available) && (
        <p className="text-sm text-muted-foreground">
          Some accounts are unavailable for selection. Their identity must be active and verified.
        </p>
      )}
      {!members.isError && identities.data && !identities.isError && (
        <RefreshButton
          refresh={identities.refetch}
          error={identities.error}
          busy={identities.isFetching}
        >
          Refresh account details
        </RefreshButton>
      )}
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

function personLabel(person: MemberPresentation) {
  return `${person.name} — ${person.roles.map((role) => role.label).join(", ")} · ${person.email}`;
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
  person,
}: {
  scope: PharmacyScope;
  row: Grant;
  mine: boolean;
  outletName: string;
  person?: MemberPresentation;
}) {
  const [confirm, setConfirm] = useState(false);
  const change = useAccessChange(scope, "Access changed");
  const revoke = useAccessChange(scope, "Access removed");
  const busy = change.isPending || revoke.isPending;
  const error = change.error ?? revoke.error;
  const name = person?.available ? person.name! : "Account details unavailable";
  return (
    <li className="space-y-2 p-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="break-words text-sm font-medium">
          {name}
          {mine && <span className="ml-2 text-xs text-muted-foreground">You</span>}
          {person?.available && (
            <span className="mt-1 block text-xs font-normal text-muted-foreground">
              {person.roles.map((role) => role.label).join(", ")}
            </span>
          )}
        </p>
        <div className="flex items-center gap-2">
          <SelectControl
            aria-label={`Access level for ${name}`}
            value={row.level}
            options={levels}
            disabled={busy || !person?.available}
            onValueChange={(level) => {
              if (level !== row.level)
                change.mutate({ subject: row.subject, level: level as Level });
            }}
          />
          <Button
            variant="outline"
            disabled={busy || !person?.available}
            onClick={() => setConfirm(true)}
          >
            Remove <span className="sr-only">{name}</span>
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
        description={`Remove ${name}'s access to ${outletName}?${mine ? " You will lose access to this outlet." : ""}`}
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
  disabled,
  onStateChange,
}: {
  scope: PharmacyScope;
  outletName: string;
  choices: { value: string; label: string }[];
  disabled: boolean;
  onStateChange?: FormState;
}) {
  const form = useZodForm(grantSchema, { subject: "", level: "editor", confirmed: false });
  const grant = useAccessChange(scope, "Pharmacy access granted");
  useReportState(onStateChange, form.formState.isDirty, grant.isPending);
  return (
    <form
      noValidate
      className="space-y-4"
      onSubmit={form.handleSubmit(({ subject, level }) => {
        if (disabled || !choices.some((choice) => choice.value === subject)) return;
        grant.mutate(
          { subject, level },
          { onSuccess: () => form.reset({ subject: "", level: "editor", confirmed: false }) },
        );
      })}
    >
      <h4 className="text-sm font-medium">Grant access</h4>
      <p className="text-sm text-muted-foreground">
        Grant catalogue and receiving access at {outletName}. This does not approve medicines or
        confer hospital clinical access.
      </p>
      <p className="text-xs text-muted-foreground">
        People are shown with their organization roles. Choose their outlet access level separately.
      </p>
      <div className="grid gap-4 sm:grid-cols-2">
        <FormSelectField
          control={form.control}
          name="subject"
          id="outlet-grant-subject"
          label="Person"
          disabled={grant.isPending || disabled}
          options={choices}
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
      <Button type="submit" disabled={grant.isPending || disabled}>
        Grant pharmacy access
      </Button>
    </form>
  );
}
