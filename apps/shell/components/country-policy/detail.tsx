"use client";

import { useEffect, useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  changeCountryPolicyAccess,
  countryPolicyAccessSchema,
  countryPolicyKey,
  countryPolicyPath,
  countryPolicyRequest,
  countryPolicyReviewSchema,
  countryProfile,
  newCountryPolicyId,
  reviewCountryPolicy,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type CountryPolicyAccessChange,
  type CountryPolicyIdentity,
  type CountryPolicyReceipt,
  type CountryPolicyReview,
  type CountryPolicyScope,
  type CountryPolicyVersion,
  type CountryPolicyVersionSummary,
} from "@helivanta/api";
import { Field, FormSelectField, LoadMore, formatDateTime, useZodForm } from "@helivanta/ui";
import { RequestFailure } from "../request-failure";
import { useCountryPolicyCommand } from "./command";
import { CountryPolicyEditor, policyLabel } from "./editor";

export function CountryPolicyDetail({
  identity,
  id,
  onClose,
}: {
  identity: CountryPolicyIdentity;
  id: string;
  onClose: () => void;
}) {
  const scope = useApiQuery<CountryPolicyScope>(
    countryPolicyKey(identity, id, "scope"),
    countryPolicyPath(id),
    { request: countryPolicyRequest(identity), gcTime: 0, poll: true },
  );
  if (scope.isPending) return <p role="status">Loading policy scope…</p>;
  if (scope.isError || !scope.data)
    return (
      <RequestFailure
        message="Could not load this policy scope. Its access or status may have changed."
        retry={scope.refetch}
      />
    );
  return (
    <PolicyRecord
      identity={identity}
      scope={scope.data}
      onClose={onClose}
      onRefresh={scope.refetch}
    />
  );
}

function PolicyRecord({
  identity,
  scope,
  onClose,
  onRefresh,
}: {
  identity: CountryPolicyIdentity;
  scope: CountryPolicyScope;
  onClose: () => void;
  onRefresh: () => unknown;
}) {
  const { can } = usePermissions();
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.scrollIntoView({ block: "start" });
    heading.current?.focus({ preventScroll: true });
  }, []);
  const path = countryPolicyPath(scope.id);
  const request = countryPolicyRequest(identity);
  const versions = useApiPagedQuery<CountryPolicyVersionSummary>(
    countryPolicyKey(identity, scope.id, "versions"),
    `${path}/versions`,
    { request },
  );
  const history = useApiPagedQuery<CountryPolicyReceipt>(
    countryPolicyKey(identity, scope.id, "actions"),
    `${path}/actions`,
    { request },
  );
  const [selected, setSelected] = useState<string | null>(null);
  const [editing, setEditing] = useState<CountryPolicyVersion | null>(null);
  const [mode, setMode] = useState<
    "view" | "edit" | "access" | "approved" | "rejected" | "suspended"
  >("view");
  const versionId = selected ?? versions.items[0]?.id ?? scope.current_version;
  const version = useApiQuery<CountryPolicyVersion>(
    countryPolicyKey(identity, scope.id, "version", versionId, scope.revision),
    `${path}/versions/${versionId}`,
    { enabled: !!versionId, request, gcTime: 0 },
  );
  const owns = scope.access_role === "owner" && scope.owner_subject === identity.subject;
  const editable =
    can("workspace.country_policy.manage") && (owns || scope.access_role === "editor");
  const reviewer =
    can("workspace.country_policy.review") && (owns || scope.access_role === "reviewer");
  const ownVersion =
    version.data?.author === identity.subject || version.data?.submitted_by === identity.subject;
  const onSaved = () => {
    setMode("view");
    setEditing(null);
  };
  return (
    <section
      aria-label="Country policy detail"
      className="space-y-6 rounded-2xl border bg-card p-5 sm:p-7"
    >
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h3 ref={heading} tabIndex={-1} className="scroll-mt-24 text-xl font-semibold">
            {countryProfile(scope.country)?.name} · {scope.jurisdiction}
          </h3>
          <p className="mt-2 text-sm text-muted-foreground">
            {policyLabel(scope.category)} · {policyLabel(scope.operation)} · {scope.currency} ·
            revision {scope.revision}
          </p>
          <p className="mt-2 text-sm font-medium">
            {scope.status === "active" ? "Approved configuration" : policyLabel(scope.status)} ·{" "}
            {policyLabel(scope.access_role)} access
          </p>
        </div>
        <Button variant="outline" onClick={onClose}>
          Close policy
        </Button>
      </header>
      {scope.policy_decision && (
        <div className="rounded-xl bg-muted p-4">
          <p className="text-sm font-medium">
            {scope.policy_decision.allowed
              ? "Country policy is current"
              : "Country policy does not currently allow this scope"}
          </p>
          <p className="mt-2 text-sm text-muted-foreground">
            {scope.policy_decision.allowed
              ? "Organisation licences, trading relationships, stock and credit still require their own checks."
              : policyLabel(scope.policy_decision.reason)}
          </p>
        </div>
      )}
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" onClick={() => void onRefresh()}>
          Refresh policy
        </Button>
        {editable && version.data && !version.isError && (
          <Button
            variant="outline"
            onClick={() => {
              setEditing(version.data);
              setMode("edit");
            }}
          >
            Create revised version
          </Button>
        )}
        {owns && can("workspace.country_policy.manage") && (
          <Button variant="outline" onClick={() => setMode("access")}>
            Manage policy access
          </Button>
        )}
        {reviewer && scope.status === "active" && (
          <Button variant="outline" onClick={() => setMode("suspended")}>
            Suspend policy
          </Button>
        )}
      </div>
      {mode === "edit" && editable && editing && (
        <CountryPolicyEditor
          key={editing.id}
          identity={identity}
          scope={scope}
          version={editing}
          onSaved={onSaved}
          onCancel={onSaved}
        />
      )}
      {mode === "access" && owns && can("workspace.country_policy.manage") && (
        <PolicyAccessForm identity={identity} scope={scope} onSaved={onSaved} />
      )}
      {reviewer && (mode === "approved" || mode === "rejected" || mode === "suspended") && (
        <PolicyReviewForm
          key={mode}
          identity={identity}
          scope={scope}
          version={version.data}
          action={mode}
          onSaved={onSaved}
        />
      )}
      <section className="space-y-3">
        <h4 className="font-semibold">Policy versions</h4>
        {versions.isPending ? (
          <p role="status">Loading policy versions…</p>
        ) : versions.isError ? (
          <RequestFailure message="Could not load policy versions." retry={versions.retry} />
        ) : (
          <div className="flex flex-wrap gap-2">
            {versions.items.map((row) => (
              <Button
                key={row.id}
                variant={row.id === versionId ? "default" : "outline"}
                onClick={() => {
                  setSelected(row.id);
                  setMode("view");
                }}
              >
                {policyLabel(row.state)} · {row.id.slice(-8)}
              </Button>
            ))}
          </div>
        )}
        <LoadMore
          hasMore={versions.hasMore}
          isLoading={versions.isFetchingMore}
          onClick={versions.loadMore}
        />
      </section>
      {versionId &&
        (version.isPending ? (
          <p role="status">Loading reviewed sources…</p>
        ) : version.isError || !version.data ? (
          <RequestFailure
            message="Could not load the selected policy version."
            retry={version.refetch}
          />
        ) : (
          <>
            <VersionRecord
              version={version.data}
              current={scope.current_version === version.data.id && scope.status === "active"}
            />
            {version.data.state === "submitted" &&
              reviewer &&
              (ownVersion ? (
                <p className="rounded-xl border p-4 text-sm text-muted-foreground">
                  An independent reviewer must decide this version. Authors and submitters cannot
                  approve or reject their own work.
                </p>
              ) : (
                <div className="flex flex-wrap gap-3">
                  <Button onClick={() => setMode("approved")}>Approve version</Button>
                  <Button variant="outline" onClick={() => setMode("rejected")}>
                    Reject version
                  </Button>
                </div>
              ))}
          </>
        ))}
      <section className="space-y-4">
        <h4 className="font-semibold">Decision and access history</h4>
        {history.isPending ? (
          <p role="status">Loading policy history…</p>
        ) : history.isError ? (
          <RequestFailure message="Could not load policy history." retry={history.retry} />
        ) : (
          <ol className="space-y-3">
            {history.items.map((row) => (
              <li key={row.id} className="rounded-xl border p-4">
                <p className="text-sm font-medium">
                  {policyLabel(row.action)} · revision {row.result_revision}
                </p>
                <p className="mt-1 break-words text-xs text-muted-foreground">
                  {row.actor_subject} · {formatDateTime(row.created_at)}
                </p>
                {row.reason && <p className="mt-2 whitespace-pre-wrap text-sm">{row.reason}</p>}
              </li>
            ))}
          </ol>
        )}
        <LoadMore
          hasMore={history.hasMore}
          isLoading={history.isFetchingMore}
          onClick={history.loadMore}
        />
      </section>
    </section>
  );
}

function VersionRecord({ version, current }: { version: CountryPolicyVersion; current: boolean }) {
  const now = Date.now();
  const effective =
    now >= new Date(version.effective_from).getTime() &&
    now < new Date(version.effective_until).getTime();
  return (
    <section aria-label="Reviewed policy version" className="space-y-5">
      <p className="text-xs text-muted-foreground">
        Times shown in {Intl.DateTimeFormat().resolvedOptions().timeZone}
      </p>
      <div className="grid gap-4 rounded-xl bg-muted p-4 sm:grid-cols-2">
        <div>
          <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            Selected version
          </p>
          <p className="mt-1 text-sm">
            {policyLabel(version.state)}
            {current ? " · current configuration" : " · not the current configuration"}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            {effective ? "Within its effective window" : "Outside its effective window"}
          </p>
        </div>
        <div>
          <p className="text-xs text-muted-foreground">Effective window</p>
          <p className="mt-1 text-sm">
            {formatDateTime(version.effective_from)} – {formatDateTime(version.effective_until)}
          </p>
        </div>
        <div className="min-w-0">
          <p className="text-xs text-muted-foreground">Author</p>
          <p className="mt-1 break-all text-sm">{version.author}</p>
        </div>
        {version.approved_by && (
          <div className="min-w-0">
            <p className="text-xs text-muted-foreground">Independent reviewer</p>
            <p className="mt-1 break-all text-sm">{version.approved_by}</p>
          </div>
        )}
      </div>
      <div className="space-y-3">
        <h4 className="font-semibold">Source citations</h4>
        {version.sources.map((source) => (
          <div key={source.key} className="rounded-xl border p-4">
            <p className="text-sm font-medium">{source.title}</p>
            <a
              className="mt-1 block break-all text-sm text-(--hms-accent) underline"
              href={source.url}
              target="_blank"
              rel="noreferrer noopener"
            >
              {source.url}
            </a>
            <p className="mt-2 text-xs text-muted-foreground">
              Reviewed {formatDateTime(source.reviewed_at)}
            </p>
          </div>
        ))}
      </div>
      <div className="grid gap-3 md:grid-cols-2">
        {version.requirements.map((requirement) => (
          <div key={requirement.domain} className="rounded-xl border p-4">
            <p className="text-sm font-semibold">{policyLabel(requirement.domain)}</p>
            <p className="mt-1 text-xs text-muted-foreground">
              {policyLabel(requirement.applicability)}
            </p>
            <p className="mt-3 whitespace-pre-wrap text-sm leading-6">{requirement.reason}</p>
            <p className="mt-3 text-xs text-muted-foreground">
              Sources:{" "}
              {requirement.source_keys
                .map((key) => version.sources.find((source) => source.key === key)?.title ?? key)
                .join("; ")}
            </p>
          </div>
        ))}
      </div>
    </section>
  );
}

function PolicyReviewForm({
  identity,
  scope,
  version,
  action,
  onSaved,
}: {
  identity: CountryPolicyIdentity;
  scope: CountryPolicyScope;
  version?: CountryPolicyVersion;
  action: CountryPolicyReview["action"];
  onSaved: () => void;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.scrollIntoView({ block: "start" });
    heading.current?.focus({ preventScroll: true });
  }, []);
  const form = useZodForm(countryPolicyReviewSchema, {
    id: newCountryPolicyId(),
    version_id: action === "suspended" ? "" : (version?.id ?? ""),
    expected_revision: scope.revision,
    action,
    reason: "",
  });
  const verb = action === "approved" ? "Approve" : action === "rejected" ? "Reject" : "Suspend";
  const save = useCountryPolicyCommand(
    identity,
    (input: CountryPolicyReview, signal) => reviewCountryPolicy(identity, scope.id, input, signal),
    `Policy ${action}`,
    onSaved,
  );
  return (
    <form
      noValidate
      onSubmit={form.handleSubmit((value) => save.mutate(value))}
      className="space-y-4 rounded-xl border p-5"
    >
      <h4 ref={heading} tabIndex={-1} className="scroll-mt-24 font-semibold">
        {verb} policy
      </h4>
      <Field
        id="policy-review-reason"
        label="Review reason"
        error={form.formState.errors.reason?.message}
      >
        <textarea
          id="policy-review-reason"
          {...form.register("reason")}
          className="min-h-28 w-full rounded-lg border bg-background px-3 py-2 text-sm"
        />
      </Field>
      {save.error && (
        <p role="alert" className="text-sm text-destructive">
          {save.error.message}
        </p>
      )}
      <div className="flex gap-3">
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Saving…" : `${verb} policy`}
        </Button>
        <Button type="button" variant="outline" disabled={save.isPending} onClick={onSaved}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

function PolicyAccessForm({
  identity,
  scope,
  onSaved,
}: {
  identity: CountryPolicyIdentity;
  scope: CountryPolicyScope;
  onSaved: () => void;
}) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.scrollIntoView({ block: "start" });
    heading.current?.focus({ preventScroll: true });
  }, []);
  const form = useZodForm(countryPolicyAccessSchema, {
    id: newCountryPolicyId(),
    expected_revision: scope.revision,
    subject: "",
    role: "reader",
    revoke: false,
  });
  const save = useCountryPolicyCommand(
    identity,
    (input: CountryPolicyAccessChange, signal) =>
      changeCountryPolicyAccess(identity, scope.id, input, signal),
    "Policy access updated",
    onSaved,
  );
  const revoke = form.watch("revoke");
  return (
    <form
      noValidate
      onSubmit={form.handleSubmit((input) => save.mutate(input))}
      className="space-y-4 rounded-xl border p-5"
    >
      <h4 ref={heading} tabIndex={-1} className="scroll-mt-24 font-semibold">
        Policy scope access
      </h4>
      <p className="text-sm text-muted-foreground">
        Use the tenant member's sign-in subject. A scope grant does not grant platform permissions;
        the member needs both.
      </p>
      <Field
        id="policy-access-subject"
        label="Team member subject"
        error={form.formState.errors.subject?.message}
      >
        <Input id="policy-access-subject" {...form.register("subject")} />
      </Field>
      <FormSelectField
        control={form.control}
        name="role"
        id="policy-access-role"
        label="Scope role"
        disabled={revoke}
        options={[
          { value: "reader", label: "Read policy and history" },
          { value: "editor", label: "Submit policy versions" },
          { value: "reviewer", label: "Review versions and suspend policy" },
          { value: "", label: "No scope role" },
        ]}
      />
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          {...form.register("revoke")}
          onChange={(event) => {
            form.setValue("revoke", event.target.checked);
            form.setValue("role", event.target.checked ? "" : "reader");
          }}
        />
        Revoke this member's scope access
      </label>
      {save.error && (
        <p role="alert" className="text-sm text-destructive">
          {save.error.message}
        </p>
      )}
      <div className="flex gap-3">
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Saving…" : "Update policy access"}
        </Button>
        <Button type="button" variant="outline" disabled={save.isPending} onClick={onSaved}>
          Cancel
        </Button>
      </div>
    </form>
  );
}
