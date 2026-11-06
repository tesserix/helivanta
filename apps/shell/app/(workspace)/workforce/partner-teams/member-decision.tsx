"use client";
import { useEffect, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  Panel,
  Field,
  FormSelectField,
  useLeaveGuard,
  useZodForm,
  formatDateTime,
} from "@helivanta/ui";
import {
  ApiError,
  PartnerMemberCommandInputContract,
  decidePartnerMember,
  recoverPartnerMemberCommand,
  newPartnerGroupId,
  partnerMembersKey,
  useApiMutation,
  type PartnerGroupScope,
  type PartnerGroupMemberProposal,
  type PartnerMemberCommandInput,
} from "@helivanta/api";
import { accessChanged } from "./errors";
const schema = z
  .object({
    operation: PartnerMemberCommandInputContract.shape.operation,
    reason: PartnerMemberCommandInputContract.shape.reason.trim(),
    sources_examined: z.boolean(),
    purpose_eligibility_confirmed: z.boolean(),
  })
  .superRefine((values, ctx) => {
    const parsed = PartnerMemberCommandInputContract.safeParse({ ...values, expected_version: 1 });
    if (!parsed.success)
      for (const issue of parsed.error.issues)
        ctx.addIssue({ code: "custom", path: issue.path, message: issue.message });
  });
const labels = {
  approve: "Approve membership",
  reject: "Reject nomination",
  withdraw: "Withdraw nomination",
  suspend: "Suspend membership",
};
export function MemberDecision({
  scope,
  member,
  operations,
  onChanged,
  onBusyChanged,
  onFinished,
  onCancel,
}: {
  scope: PartnerGroupScope;
  member: PartnerGroupMemberProposal;
  operations: PartnerMemberCommandInput["operation"][];
  onChanged: () => void;
  onBusyChanged: (busy: boolean) => void;
  onFinished: (recovered: boolean) => void;
  onCancel: () => void;
}) {
  const [id] = useState(() => newPartnerGroupId());
  const [retained, setRetained] = useState<PartnerMemberCommandInput | null>(null);
  const [refused, setRefused] = useState(false);
  const form = useZodForm(schema, {
    operation: operations[0],
    reason: "",
    sources_examined: false,
    purpose_eligibility_confirmed: false,
  });
  const save = useApiMutation(
    (input: PartnerMemberCommandInput) =>
      decidePartnerMember(scope, member.hospital_id, member.group_id, member.id, id, input),
    {
      invalidate: [partnerMembersKey(scope, member.hospital_id, member.group_id)],
      onSuccess: () => onFinished(false),
      onError: (error) => {
        if (accessChanged(error)) onChanged();
        setRefused(error instanceof ApiError && [400, 404, 409].includes(error.status));
      },
      suppressErrorToast: true,
    },
  );
  const recover = useApiMutation(
    () => {
      if (!retained) throw new Error("No membership decision awaits recovery.");
      return recoverPartnerMemberCommand(
        scope,
        member.hospital_id,
        member.group_id,
        member.id,
        id,
        retained,
      );
    },
    {
      invalidate: [partnerMembersKey(scope, member.hospital_id, member.group_id)],
      onSuccess: () => onFinished(true),
      onError: (error) => {
        if (accessChanged(error)) onChanged();
      },
      suppressErrorToast: true,
    },
  );
  const pending = save.isPending || recover.isPending;
  const busy = pending || (!!retained && !refused);
  useLeaveGuard(form.formState.isDirty || !!retained, "membership decision", busy);
  useEffect(() => onBusyChanged(busy), [busy, onBusyChanged]);
  const operation = form.watch("operation");
  return (
    <Panel>
      <form
        aria-label="Review hospital membership"
        noValidate
        className="space-y-4"
        onSubmit={form.handleSubmit((values) => {
          if (!operations.includes(values.operation)) return;
          const input = retained ?? { ...values, expected_version: member.version };
          setRetained(input);
          setRefused(false);
          save.mutate(input);
        })}
      >
        <p className="font-medium">Review {member.member_display_name}</p>
        <p>Selected account: {member.input.subject}</p>
        <p>Nomination reason: {member.input.reason}</p>
        <p>
          Source observed {formatDateTime(member.source_checked_at)}; recorded source validity until{" "}
          {formatDateTime(member.source_until)}.
        </p>
        <p>
          Approval rechecks the current account, employment generation and posting. Recorded
          membership alone grants no supplier access.
        </p>

        <FormSelectField
          control={form.control}
          name="operation"
          id="membership-operation"
          label="Membership decision"
          options={operations.map((value) => ({ value, label: labels[value] }))}
          disabled={!!retained}
        />
        <Field
          id="membership-review-reason"
          label="Review reason"
          error={form.formState.errors.reason?.message}
        >
          <Input id="membership-review-reason" {...form.register("reason")} disabled={!!retained} />
        </Field>
        {operation === "approve" && (
          <>
            <label className="flex items-center gap-2">
              <input type="checkbox" {...form.register("sources_examined")} disabled={!!retained} />
              I examined the current Staff and posting sources
            </label>
            {form.formState.errors.sources_examined && (
              <p role="alert">{form.formState.errors.sources_examined.message}</p>
            )}
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                {...form.register("purpose_eligibility_confirmed")}
                disabled={!!retained}
              />
              I confirm this Staff can participate in the team purpose
            </label>
            {form.formState.errors.purpose_eligibility_confirmed && (
              <p role="alert">{form.formState.errors.purpose_eligibility_confirmed.message}</p>
            )}
          </>
        )}
        {save.isError && (
          <p role="alert">
            {refused
              ? save.error.message
              : "Decision outcome unconfirmed. Recover its retained receipt or retry the same command."}
          </p>
        )}
        {recover.isError && (
          <p role="alert">
            The retained decision is unavailable. Keep this command and retry recovery or the same
            request.
          </p>
        )}
        <div className="flex flex-wrap gap-3">
          <Button
            type={retained ? "button" : "submit"}
            disabled={pending}
            onClick={
              retained
                ? () => {
                    setRefused(false);
                    save.mutate(retained);
                  }
                : undefined
            }
          >
            {retained ? "Retry same membership decision" : "Record membership decision"}
          </Button>
          {retained && (
            <Button
              type="button"
              variant="outline"
              disabled={pending}
              onClick={() => recover.mutate(undefined)}
            >
              Recover membership decision
            </Button>
          )}
          {(!retained || refused) && (
            <Button type="button" variant="outline" disabled={pending} onClick={onCancel}>
              Close membership review
            </Button>
          )}
        </div>
      </form>
    </Panel>
  );
}
