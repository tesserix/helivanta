"use client";

import { useEffect, useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  Field,
  Panel,
  RequestFailure,
  RefreshButton,
  SelectControl,
  useLeaveGuard,
  useZodForm,
} from "@helivanta/ui";
import {
  ApiError,
  capabilityForPanel,
  governanceTemplates,
  GovernanceGrantInputContract,
  issueGovernanceGrant,
  newGovernanceCommandId,
  readGovernanceGrant,
  readMemberPresentations,
  useApiMutation,
  useMembershipCandidates,
  useReleaseCapabilities,
  useStaffHospitalsPage,
  usePharmacyStaffSourcesPage,
  useTenantTransition,
  type GovernanceGrantInput,
  type GovernanceRequestScope,
} from "@helivanta/api";

const schema = z.object({
  subject: z.string().min(1, "Choose a current member."),
  scope_id: z.string(),
  reason: z.string().trim().min(1, "Enter an appointment reason.").max(500),
  start: z.string().min(1, "Choose a start time."),
  end: z.string().min(1, "Choose an expiry."),
  acknowledged: z.boolean().refine(Boolean, "Review the appointment first."),
});
type Command = { id: string; input: GovernanceGrantInput; name: string; scopeName: string };
type Kind = GovernanceGrantInput["scope_kind"];
const recoverySchema = z.object({ id: z.uuidv7() });

export function GovernanceAppointments({ scope }: { scope: GovernanceRequestScope }) {
  const switching = useTenantTransition();
  const release = useReleaseCapabilities();
  if (switching || !release.available(capabilityForPanel("staff.governance.scope.tenant")))
    return null;
  return <Appointments key={`${scope.tenantId}:${scope.subject}`} scope={scope} />;
}

function Appointments({ scope }: { scope: GovernanceRequestScope }) {
  const prefix = useId();
  const release = useReleaseCapabilities();
  const templates = governanceTemplates.filter(
    (t) =>
      release.available(capabilityForPanel(`staff.governance.template.${t.key}`)) &&
      t.scope_kinds.some(
        (k) =>
          k !== "supplier" && release.available(capabilityForPanel(`staff.governance.scope.${k}`)),
      ),
  );
  const [template, setTemplate] = useState<GovernanceGrantInput["template_key"]>(
    "workforce.account_link_reviewer.v1",
  );
  const definition = templates.find((t) => t.key === template);
  const kinds = (definition?.scope_kinds ?? []).filter(
    (kind) =>
      kind !== "supplier" &&
      release.available(capabilityForPanel(`staff.governance.scope.${kind}`)),
  );
  const [kind, setKind] = useState<Kind>("hospital");
  const allowed = Boolean(definition && kinds.some((value) => value === kind));
  const form = useZodForm(schema, {
    subject: "",
    scope_id: "",
    reason: "",
    start: "",
    end: "",
    acknowledged: false,
  });
  const recovery = useZodForm(recoverySchema, { id: "" });
  const [command, setCommand] = useState<Command | null>(null);
  const [inputError, setInputError] = useState<string | null>(null);
  const [source, setSource] = useState<{ id: string; name: string } | null>(null);
  const members = useMembershipCandidates(scope);
  const candidates =
    members.data?.pages
      .flatMap((p) => p.data)
      .filter((m) => m.role_key === "staff_member" && m.subject !== scope.subject) ?? [];
  const subjects = [...new Set(candidates.map((m) => m.subject))].sort();
  const identities = useQuery({
    queryKey: [
      "iam",
      "reviewer-presentations",
      scope.tenantId,
      scope.subject,
      subjects,
      candidates.map((m) => [m.id, m.membership_epoch]),
    ],
    queryFn: ({ signal }) => readMemberPresentations(scope, subjects, signal),
    enabled: !members.isPending && !members.isError,
    gcTime: 0,
    staleTime: 0,
    retry: false,
  });
  const names =
    !members.isError && !identities.isError
      ? (identities.data?.filter((m) => m.available) ?? [])
      : [];
  const save = useApiMutation(
    (value: Command) => issueGovernanceGrant(scope, value.id, value.input),
    { suppressErrorToast: true },
  );
  const read = useApiMutation((id: string) => readGovernanceGrant(scope, id), {
    suppressErrorToast: true,
  });
  const refused =
    command !== null && save.error instanceof ApiError && [400, 409].includes(save.error.status);
  const receipt = read.isError ? undefined : (read.data ?? save.data);
  const busy = save.isPending || read.isPending;
  const uncertain =
    command !== null &&
    save.isError &&
    !(save.error instanceof ApiError && [400, 401, 403, 404, 409].includes(save.error.status));
  const locked = command !== null || busy || Boolean(receipt);
  useLeaveGuard(
    form.formState.isDirty && !receipt,
    "reviewer appointment; retain the command reference",
    busy,
  );
  const selected = names.find((m) => m.subject === form.watch("subject"));
  const member = candidates.find((m) => m.subject === selected?.subject);
  const change = () => {
    form.setValue("scope_id", "");
    form.setValue("acknowledged", false);
    setSource(null);
    setInputError(null);
  };

  return (
    <Panel className="my-5 space-y-4">
      <h2 className="text-lg font-semibold">Reviewer appointments</h2>
      <p>
        Appoint a current ordinary member to a bounded nonclinical review role. This does not grant
        clinical authority, outlet operations or native organization administration. A single-owner
        appointment may be provisional; other appointments require independent review.
      </p>
      {command && (
        <p>
          Appointment command reference: <code>{command.id}</code>. Keep this reference to recover
          the result.
        </p>
      )}
      {receipt ? (
        <div role="status" className="space-y-2">
          <p>
            {receipt.status === "provisional_active"
              ? "Provisional authority: single-owner source, not independently approved."
              : receipt.status === "pending_review"
                ? "Appointment retained; awaiting independent review."
                : `Current appointment status: ${receipt.status}.`}
          </p>
          <p>
            Version {receipt.version} · expires {receipt.input.expires_at}
          </p>
          {command && (
            <p>
              {command.name} · {command.scopeName}
            </p>
          )}
          <Button variant="outline" disabled={busy} onClick={() => read.mutate(receipt.id)}>
            Refresh current appointment
          </Button>
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => {
              setCommand(null);
              save.reset();
              read.reset();
              form.reset();
              setSource(null);
            }}
          >
            Prepare another appointment
          </Button>
        </div>
      ) : (
        <form
          aria-label="Reviewer appointment"
          noValidate
          className="space-y-4"
          onSubmit={form.handleSubmit((values) => {
            if (
              !allowed ||
              !member ||
              !selected ||
              members.isError ||
              identities.isError ||
              (kind !== "tenant" && (!source || source.id !== values.scope_id))
            ) {
              setInputError("Refresh and choose a currently verified account and scope.");
              return;
            }
            const start = new Date(values.start + "Z"),
              end = new Date(values.end + "Z");
            const parsed = GovernanceGrantInputContract.safeParse({
              subject: member.subject,
              expected_membership_epoch: member.membership_epoch,
              template_key: template,
              scope_kind: kind,
              ...(kind === "tenant" ? {} : { scope_id: source?.id }),
              reason: values.reason,
              valid_from: Number.isNaN(start.valueOf()) ? "" : start.toISOString(),
              expires_at: Number.isNaN(end.valueOf()) ? "" : end.toISOString(),
            });
            if (!parsed.success) {
              setInputError(parsed.error.issues.map((issue) => issue.message).join(" "));
              return;
            }
            setInputError(null);
            const value = {
              id: newGovernanceCommandId(),
              input: parsed.data,
              name: selected.name!,
              scopeName: kind === "tenant" ? "This workspace" : source!.name,
            };
            setCommand(value);
            save.mutate(value);
          })}
        >
          {members.isError || identities.isError ? (
            <RequestFailure
              message="Could not verify current reviewer accounts."
              error={members.error ?? identities.error}
              retry={() => {
                void members.refetch();
                void identities.refetch();
              }}
            />
          ) : members.isPending || identities.isPending ? (
            <p role="status">Checking current member names…</p>
          ) : (
            <>
              <Field
                id={`${prefix}-subject`}
                label="Reviewer account"
                error={form.formState.errors.subject?.message}
              >
                <SelectControl
                  id={`${prefix}-subject`}
                  value={selected?.subject ?? ""}
                  disabled={locked || members.isFetching || identities.isFetching}
                  onValueChange={(value) => {
                    form.setValue("subject", value, { shouldDirty: true });
                    form.setValue("acknowledged", false);
                  }}
                  options={names.map((m) => ({
                    value: m.subject,
                    label: `${m.name} · ${m.email} · ${m.roles.map((r) => r.label).join(", ")}`,
                  }))}
                />
              </Field>
              {!names.length && <p>No eligible named ordinary members on the loaded pages.</p>}
              {members.hasNextPage && (
                <Button
                  type="button"
                  variant="outline"
                  disabled={locked || members.isFetching}
                  onClick={() => {
                    void members.fetchNextPage();
                  }}
                >
                  Load more member accounts
                </Button>
              )}
            </>
          )}
          <RefreshButton
            error={members.error ?? identities.error}
            busy={locked || members.isFetching || identities.isFetching}
            refresh={() => {
              form.setValue("subject", "");
              form.setValue("acknowledged", false);
              void members.refetch();
              void identities.refetch();
            }}
          >
            Refresh reviewer accounts
          </RefreshButton>
          <Field id={`${prefix}-template`} label="Review role">
            <SelectControl
              id={`${prefix}-template`}
              value={definition?.key ?? ""}
              disabled={locked}
              options={templates.map((t) => ({ value: t.key, label: t.name }))}
              onValueChange={(value) => {
                const t = templates.find((item) => item.key === value);
                if (!t) return;
                const nextKind = t.scope_kinds.find(
                  (k) =>
                    k !== "supplier" &&
                    release.available(capabilityForPanel(`staff.governance.scope.${k}`)),
                );
                if (!nextKind) return;
                setTemplate(t.key);
                setKind(nextKind);
                change();
              }}
            />
          </Field>
          <Field id={`${prefix}-kind`} label="Appointment scope">
            <SelectControl
              id={`${prefix}-kind`}
              value={allowed ? kind : ""}
              disabled={locked}
              options={kinds.map((k) => ({
                value: k,
                label: k === "tenant" ? "This workspace" : k[0].toUpperCase() + k.slice(1),
              }))}
              onValueChange={(value) => {
                setKind(value as Kind);
                change();
              }}
            />
          </Field>
          {allowed && (kind === "hospital" || kind === "pharmacy") && (
            <Source
              key={kind}
              scope={scope}
              kind={kind}
              disabled={locked}
              selected={source?.id ?? ""}
              onChange={(value) => {
                setSource(value);
                form.setValue("scope_id", value?.id ?? "", { shouldDirty: true });
                form.setValue("acknowledged", false);
              }}
            />
          )}
          {(
            [
              ["start", "Starts at (UTC)"],
              ["end", "Expires at (UTC)"],
              ["reason", "Appointment reason"],
            ] as const
          ).map(([key, label]) => (
            <Field
              key={key}
              id={`${prefix}-${key}`}
              label={label}
              error={form.formState.errors[key]?.message}
            >
              <Input
                id={`${prefix}-${key}`}
                type={key === "reason" ? "text" : "datetime-local"}
                disabled={locked}
                {...form.register(key)}
              />
            </Field>
          ))}
          <label className="flex items-start gap-2">
            <input
              type="checkbox"
              disabled={locked || !allowed}
              {...form.register("acknowledged")}
            />
            I have reviewed the account, role, exact scope and expiry.
          </label>
          {form.formState.errors.acknowledged && (
            <p role="alert">{form.formState.errors.acknowledged.message}</p>
          )}
          {inputError && <p role="alert">{inputError}</p>}
          {save.isError && (
            <RequestFailure
              message="Could not confirm the appointment. Keep the original command reference."
              error={save.error}
              retry={() => read.mutate(command!.id)}
            />
          )}
          <Button
            type="submit"
            disabled={
              locked ||
              !allowed ||
              !selected ||
              members.isError ||
              identities.isError ||
              members.isFetching ||
              identities.isFetching
            }
          >
            Submit appointment
          </Button>
          {refused && (
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                setCommand(null);
                save.reset();
                read.reset();
                form.setValue("acknowledged", false);
              }}
            >
              Prepare corrected appointment
            </Button>
          )}
          {uncertain && (
            <Button type="button" disabled={busy || !allowed} onClick={() => save.mutate(command!)}>
              Retry original appointment
            </Button>
          )}
          {command && (
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={() => read.mutate(command.id)}
            >
              Check original appointment
            </Button>
          )}
        </form>
      )}
      {read.isError && (
        <RequestFailure
          message="Could not verify the current appointment."
          error={read.error}
          retry={() => read.mutate(command?.id ?? recovery.getValues("id"))}
        />
      )}
      {!command && !receipt && (
        <form
          aria-label="Recover reviewer appointment"
          noValidate
          onSubmit={recovery.handleSubmit(({ id }) => read.mutate(id))}
          className="space-y-3"
        >
          <Field
            id={`${prefix}-recover`}
            label="Saved appointment command reference"
            error={recovery.formState.errors.id?.message}
          >
            <Input id={`${prefix}-recover`} disabled={busy} {...recovery.register("id")} />
          </Field>
          <Button variant="outline" disabled={busy}>
            Recover appointment
          </Button>
        </form>
      )}
    </Panel>
  );
}

function Source({
  scope,
  kind,
  disabled,
  selected,
  onChange,
}: {
  scope: GovernanceRequestScope;
  kind: "hospital" | "pharmacy";
  disabled: boolean;
  selected: string;
  onChange: (value: { id: string; name: string } | null) => void;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  return kind === "hospital" ? (
    <HospitalSource {...{ scope, disabled, selected, onChange, cursor, setCursor }} />
  ) : (
    <PharmacySource {...{ scope, disabled, selected, onChange, cursor, setCursor }} />
  );
}
type SourceProps = Omit<Parameters<typeof Source>[0], "kind"> & {
  cursor: string | null;
  setCursor: (cursor: string | null) => void;
};
function HospitalSource(props: SourceProps) {
  const query = useStaffHospitalsPage(props.scope, props.cursor);
  return (
    <SourceChoice
      {...props}
      label="Hospital"
      query={query}
      options={query.data?.data.map((r) => ({ id: r.id, name: r.name })) ?? []}
    />
  );
}
function PharmacySource(props: SourceProps) {
  const query = usePharmacyStaffSourcesPage(props.scope, props.cursor);
  return (
    <SourceChoice
      {...props}
      label="Pharmacy"
      query={query}
      options={query.data?.data.map((r) => ({ id: r.pharmacy_id, name: r.name })) ?? []}
    />
  );
}
function SourceChoice({
  label,
  query,
  options,
  selected,
  onChange,
  cursor,
  setCursor,
  disabled,
}: Omit<SourceProps, "scope"> & {
  label: string;
  query: {
    isPending: boolean;
    isError: boolean;
    isFetching: boolean;
    error: Error | null;
    refetch: () => unknown;
    data?: { page: { has_more: boolean; next_cursor: string | null } };
  };
  options: { id: string; name: string }[];
}) {
  const prefix = useId();
  useEffect(() => {
    if (selected && (query.isError || !options.some((option) => option.id === selected)))
      onChange(null);
  }, [selected, query.isError, options, onChange]);
  if (query.isPending) return <p role="status">Checking {label.toLowerCase()} names…</p>;
  if (query.isError)
    return (
      <RequestFailure
        message={`Could not verify ${label.toLowerCase()} choices.`}
        error={query.error}
        retry={() => {
          onChange(null);
          void query.refetch();
        }}
      />
    );
  return (
    <div className="space-y-2">
      <Field id={prefix} label={label}>
        <SelectControl
          id={prefix}
          disabled={disabled || query.isFetching}
          value={options.some((o) => o.id === selected) ? selected : ""}
          onValueChange={(value) => onChange(options.find((o) => o.id === value) ?? null)}
          options={options.map((o) => ({ value: o.id, label: o.name }))}
        />
      </Field>
      {cursor && (
        <Button
          type="button"
          variant="outline"
          disabled={disabled || query.isFetching}
          onClick={() => {
            onChange(null);
            setCursor(null);
          }}
        >
          First {label.toLowerCase()} page
        </Button>
      )}
      {query.data?.page.has_more && (
        <Button
          type="button"
          variant="outline"
          disabled={disabled || query.isFetching}
          onClick={() => {
            onChange(null);
            setCursor(query.data!.page.next_cursor);
          }}
        >
          Next {label.toLowerCase()} page
        </Button>
      )}
    </div>
  );
}
