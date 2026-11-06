"use client";
import { useEffect, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  Panel,
  Field,
  FormSelectField,
  LoadMore,
  RequestFailure,
  useLeaveGuard,
  useZodForm,
} from "@helivanta/ui";
import {
  ApiError,
  newPartnerGroupId,
  partnerGroupProposalContract,
  partnerGroupsKey,
  preparePartnerGroup,
  recoverPreparedPartnerGroup,
  membershipRequestOptions,
  useApiMutation,
  useApiPagedQuery,
  type Hospital,
  type PartnerGroup,
  type PartnerGroupProposalInput,
  type PartnerGroupScope,
} from "@helivanta/api";
import { accessChanged } from "./errors";
const dateInput = z
  .string()
  .min(1, "Choose a date and time.")
  .refine((value) => Number.isFinite(Date.parse(value)), "Choose a valid date and time.");
const schema = z
  .object({
    hospital_id: partnerGroupProposalContract.shape.hospital_id,
    name: partnerGroupProposalContract.shape.name.trim(),
    purpose: partnerGroupProposalContract.shape.purpose,
    reason: partnerGroupProposalContract.shape.reason.trim(),
    valid_from: dateInput,
    expires_at: dateInput,
  })
  .superRefine((value, ctx) => {
    if (
      !Number.isFinite(Date.parse(value.valid_from)) ||
      !Number.isFinite(Date.parse(value.expires_at))
    )
      return;
    const input = {
      ...value,
      valid_from: new Date(value.valid_from).toISOString(),
      expires_at: new Date(value.expires_at).toISOString(),
    };
    const parsed = partnerGroupProposalContract.safeParse(input);
    if (!parsed.success)
      for (const issue of parsed.error.issues)
        ctx.addIssue({ code: "custom", path: issue.path, message: issue.message });
  });
function localTime(date: Date) {
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}
export function TeamProposalForm({
  scope,
  onSaved,
  onChanged,
  onBusyChanged,
  onCancel,
}: {
  scope: PartnerGroupScope;
  onSaved: (group: PartnerGroup, recovered?: boolean) => void;
  onChanged: () => void;
  onBusyChanged: (busy: boolean) => void;
  onCancel: () => void;
}) {
  const [openedAt] = useState(() => Date.now());
  const [id] = useState(() => newPartnerGroupId());
  const [retained, setRetained] = useState<PartnerGroupProposalInput | null>(null);
  const [knownRefusal, setKnownRefusal] = useState(false);
  const hospitals = useApiPagedQuery<Hospital>(
    [...partnerGroupsKey(scope), "hospital-choices"],
    "/workforce/hospitals",
    { gcTime: 0, request: { headers: membershipRequestOptions(scope).headers, cache: "no-store" } },
  );
  const form = useZodForm(schema, {
    hospital_id: "",
    name: "",
    purpose: "receiving",
    reason: "",
    valid_from: localTime(new Date(openedAt)),
    expires_at: localTime(new Date(openedAt + 30 * 86400000)),
  });
  const fail = (error: Error) => {
    if (accessChanged(error)) onChanged();
    setKnownRefusal(error instanceof ApiError && [400, 404, 409].includes(error.status));
  };
  const save = useApiMutation(
    (input: PartnerGroupProposalInput) => preparePartnerGroup(scope, id, input),
    {
      invalidate: [partnerGroupsKey(scope)],
      onSuccess: (group) => onSaved(group),
      onError: fail,
      suppressErrorToast: true,
    },
  );
  const recover = useApiMutation(
    () => {
      if (!retained) throw new Error("No team save is awaiting recovery.");
      return recoverPreparedPartnerGroup(scope, id, retained);
    },
    {
      invalidate: [partnerGroupsKey(scope)],
      onSuccess: (group) => onSaved(group, true),
      onError: (error) => {
        if (accessChanged(error)) onChanged();
      },
      suppressErrorToast: true,
    },
  );
  const busy = save.isPending || recover.isPending || (!!retained && !knownRefusal);
  useLeaveGuard(form.formState.isDirty || !!retained, "partner team proposal", busy);
  useEffect(() => onBusyChanged(busy), [busy, onBusyChanged]);
  useEffect(() => {
    if (accessChanged(hospitals.error)) onChanged();
  }, [hospitals.error, onChanged]);
  return (
    <Panel>
      <form
        aria-label="Prepare partner team"
        noValidate
        className="space-y-4"
        onSubmit={form.handleSubmit((values) => {
          const input = retained ?? {
            ...values,
            valid_from: new Date(values.valid_from).toISOString(),
            expires_at: new Date(values.expires_at).toISOString(),
          };
          setRetained(input);
          setKnownRefusal(false);
          save.mutate(input);
        })}
      >
        <h2 className="text-lg font-semibold">Prepare a hospital team</h2>
        {hospitals.isPending ? (
          <p role="status">Loading permitted hospitals…</p>
        ) : hospitals.isError ? (
          <RequestFailure
            message="Hospital choices are unavailable."
            error={hospitals.error}
            retry={hospitals.retry}
          />
        ) : (
          <>
            <FormSelectField
              control={form.control}
              name="hospital_id"
              id="partner-team-hospital"
              label="Hospital"
              options={hospitals.items
                .filter((h) => h.access_level === "manager")
                .map((h) => ({ value: h.id, label: h.name }))}
              disabled={!!retained}
            />
            <fieldset disabled={!!retained}>
              <LoadMore
                hasMore={hospitals.hasMore}
                isLoading={hospitals.isFetchingMore || hospitals.isRefreshing}
                onClick={hospitals.loadMore}
                label="Load more hospitals"
              />
            </fieldset>
          </>
        )}
        <Field id="partner-team-name" label="Team name" error={form.formState.errors.name?.message}>
          <Input id="partner-team-name" {...form.register("name")} disabled={!!retained} />
        </Field>
        <FormSelectField
          control={form.control}
          name="purpose"
          id="partner-team-purpose"
          label="Purpose"
          options={[
            { value: "procurement", label: "Procurement" },
            { value: "receiving", label: "Receiving" },
            { value: "finance", label: "Finance" },
          ]}
          disabled={!!retained}
        />
        <Field
          id="partner-team-reason"
          label="Reason"
          error={form.formState.errors.reason?.message}
        >
          <Input id="partner-team-reason" {...form.register("reason")} disabled={!!retained} />
        </Field>
        <div className="grid gap-4 md:grid-cols-2">
          {(["valid_from", "expires_at"] as const).map((name) => (
            <Field
              key={name}
              id={`partner-team-${name}`}
              label={name === "valid_from" ? "Starts" : "Expires"}
              error={form.formState.errors[name]?.message}
            >
              <Input
                type="datetime-local"
                id={`partner-team-${name}`}
                {...form.register(name)}
                disabled={!!retained}
              />
            </Field>
          ))}
        </div>
        {save.isError && (
          <p role="alert">
            {knownRefusal
              ? save.error.message
              : "The save may have been recorded. Check the saved team or retry this same save before starting another."}
          </p>
        )}
        {recover.isError && (
          <p role="alert">
            {recover.error instanceof ApiError && recover.error.status === 404
              ? "No retained team was found yet. Retry the same save to reconcile its outcome."
              : "The saved team could not be verified. Keep this proposal and retry recovery."}
          </p>
        )}
        <div className="flex flex-wrap gap-3">
          <Button
            type={retained ? "button" : "submit"}
            onClick={
              retained
                ? () => {
                    setKnownRefusal(false);
                    save.mutate(retained);
                  }
                : undefined
            }
            disabled={
              save.isPending || recover.isPending || hospitals.isError || hospitals.isPending
            }
          >
            {retained ? "Retry same save" : "Save team proposal"}
          </Button>
          {retained && (
            <Button
              type="button"
              variant="outline"
              disabled={save.isPending || recover.isPending}
              onClick={() => recover.mutate()}
            >
              Check saved team
            </Button>
          )}
          {(!retained || knownRefusal) && (
            <Button
              type="button"
              variant="outline"
              disabled={save.isPending || recover.isPending}
              onClick={onCancel}
            >
              Close proposal
            </Button>
          )}
        </div>
      </form>
    </Panel>
  );
}
