"use client";
import { useEffect, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  Panel,
  RefreshButton,
  Field,
  FormSelectField,
  RequestFailure,
  formatDateTime,
  useConfirmLeave,
  useLeaveGuard,
  useZodForm,
} from "@helivanta/ui";
import {
  ApiError,
  decidePartnerGroup,
  getPartnerGroupCommand,
  newPartnerGroupId,
  partnerGroupDecisionContract,
  partnerGroupsKey,
  useApiMutation,
  usePartnerGroup,
  type PartnerGroup,
  type PartnerGroupCommandInput,
  type PartnerGroupCommandReceipt,
  type PartnerGroupScope,
} from "@helivanta/api";
import { accessChanged } from "./errors";
const labels: Record<PartnerGroup["status"], string> = {
  pending_scope: "Hospital verification pending",
  pending_review: "Awaiting independent publication review",
  scope_rejected: "Hospital verification declined",
  approved: "Publication approved",
  rejected: "Publication rejected",
  withdrawn: "Proposal withdrawn",
  suspended: "Team suspended",
};
const operationLabels = {
  approve: "Approve publication",
  reject: "Reject publication",
  withdraw: "Withdraw proposal",
  suspend: "Suspend team",
};
export function TeamDetail({
  scope,
  id,
  canReview,
  busy,
  onChanged,
  onBusyChanged,
}: {
  scope: PartnerGroupScope;
  id: string;
  canReview: boolean;
  busy: boolean;
  onChanged: () => void;
  onBusyChanged: (busy: boolean) => void;
}) {
  const [openedAt] = useState(() => Date.now());
  const [epoch, setEpoch] = useState(0);
  const [message, setMessage] = useState("");
  const detail = usePartnerGroup(scope, id, busy);
  const confirm = useConfirmLeave();
  useEffect(() => {
    if (accessChanged(detail.error)) onChanged();
  }, [detail.error, onChanged]);
  if (detail.isPending) return <p role="status">Checking current team…</p>;
  if (detail.isError)
    return (
      <RequestFailure
        message="The current team could not be verified."
        error={detail.error}
        retry={detail.refetch}
      />
    );
  const group = detail.data;
  const operations: PartnerGroupCommandInput["operation"][] = [];
  if (
    group.status === "pending_review" &&
    canReview &&
    group.proposed_by !== scope.subject &&
    Date.parse(group.expires_at) > openedAt
  )
    operations.push("approve", "reject");
  if (
    (group.status === "pending_scope" || group.status === "pending_review") &&
    group.proposed_by === scope.subject
  )
    operations.push("withdraw");
  if (group.status === "approved") operations.push("suspend");
  return (
    <Panel aria-label="Current partner team" className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-semibold">{group.name}</h2>
        <RefreshButton
          error={detail.error}
          busy={busy || detail.isFetching}
          refresh={() =>
            confirm(() => {
              setMessage("");
              void detail.refetch();
            })
          }
        >
          Refresh current team
        </RefreshButton>
      </div>
      <p>{labels[group.status]}</p>
      <dl className="grid gap-3 text-sm md:grid-cols-2">
        <div>
          <dt>Purpose</dt>
          <dd>{group.purpose}</dd>
        </div>
        <div>
          <dt>Hospital reference</dt>
          <dd className="break-all">{group.hospital_id}</dd>
        </div>
        <div>
          <dt>Starts</dt>
          <dd>{formatDateTime(group.valid_from)}</dd>
        </div>
        <div>
          <dt>Expires</dt>
          <dd>{formatDateTime(group.expires_at)}</dd>
        </div>
      </dl>
      <p className="text-sm">
        Publication does not enrol staff or grant supplier, stock, patient or payment access.
        Membership and supplier disclosure require their own reviews.
      </p>
      {group.status === "pending_review" && group.proposed_by === scope.subject && (
        <p>Another administrator must review this team.</p>
      )}
      {message && <p role="status">{message}</p>}
      {operations.length > 0 && (
        <TeamDecision
          key={`${group.version}:${epoch}`}
          scope={scope}
          group={group}
          operations={operations}
          onChanged={onChanged}
          onBusyChanged={onBusyChanged}
          onFinished={(receipt, recovered) => {
            setMessage(
              recovered
                ? "Recorded team decision recovered."
                : `Team decision recorded: ${labels[receipt.group.status]}.`,
            );
            onBusyChanged(false);
            void detail.refetch();
          }}
          onDiscard={() =>
            confirm(() => {
              setEpoch((v) => v + 1);
              void detail.refetch();
            })
          }
        />
      )}
    </Panel>
  );
}
const decisionForm = z.object({
  operation: partnerGroupDecisionContract.shape.operation,
  reason: partnerGroupDecisionContract.shape.reason.trim(),
});
function TeamDecision({
  scope,
  group,
  operations,
  onChanged,
  onBusyChanged,
  onFinished,
  onDiscard,
}: {
  scope: PartnerGroupScope;
  group: PartnerGroup;
  operations: PartnerGroupCommandInput["operation"][];
  onChanged: () => void;
  onBusyChanged: (busy: boolean) => void;
  onFinished: (receipt: PartnerGroupCommandReceipt, recovered?: boolean) => void;
  onDiscard: () => void;
}) {
  const [id] = useState(() => newPartnerGroupId());
  const [retained, setRetained] = useState<PartnerGroupCommandInput | null>(null);
  const [refused, setRefused] = useState(false);
  const form = useZodForm(decisionForm, { operation: operations[0], reason: "" });
  const save = useApiMutation(
    (input: PartnerGroupCommandInput) => decidePartnerGroup(scope, group.id, id, input),
    {
      invalidate: [partnerGroupsKey(scope)],
      onSuccess: (receipt) => onFinished(receipt),
      onError: (error) => {
        if (accessChanged(error)) onChanged();
        setRefused(error instanceof ApiError && [400, 404, 409].includes(error.status));
      },
      suppressErrorToast: true,
    },
  );
  const recover = useApiMutation(
    async () => {
      if (!retained) throw new Error("No team decision is awaiting recovery.");
      const receipt = await getPartnerGroupCommand(scope, group.id, id);
      if (
        receipt.actor_subject !== scope.subject ||
        receipt.operation !== retained.operation ||
        receipt.expected_version !== retained.expected_version ||
        receipt.reason !== retained.reason
      )
        throw new Error("The retained decision did not match the submitted request.");
      return receipt;
    },
    {
      invalidate: [partnerGroupsKey(scope)],
      onSuccess: (receipt) => onFinished(receipt, true),
      onError: (error) => {
        if (accessChanged(error)) onChanged();
      },
      suppressErrorToast: true,
    },
  );
  const busy = save.isPending || recover.isPending || (!!retained && !refused);
  useLeaveGuard(form.formState.isDirty || !!retained, "partner team review", busy);
  useEffect(() => onBusyChanged(busy), [busy, onBusyChanged]);
  const retry = () => {
    if (retained) {
      setRefused(false);
      save.mutate(retained);
    }
  };
  return (
    <form
      aria-label="Review partner team"
      noValidate
      className="space-y-4 border-t pt-4"
      onSubmit={form.handleSubmit((values) => {
        if (!operations.includes(values.operation)) return;
        const input = retained ?? { ...values, expected_version: group.version };
        setRetained(input);
        setRefused(false);
        save.mutate(input);
      })}
    >
      <FormSelectField
        control={form.control}
        name="operation"
        id="partner-team-decision"
        label="Decision"
        options={operations.map((operation) => ({
          value: operation,
          label: operationLabels[operation],
        }))}
        disabled={!!retained}
      />
      <Field
        id="partner-team-review-reason"
        label="Review reason"
        error={form.formState.errors.reason?.message}
      >
        <Input id="partner-team-review-reason" {...form.register("reason")} disabled={!!retained} />
      </Field>
      {save.isError && (
        <p role="alert">
          {refused
            ? save.error.message
            : "The decision may have been recorded. Check its retained result or retry this same request."}
        </p>
      )}
      {recover.isError && (
        <p role="alert">
          {recover.error instanceof ApiError && recover.error.status === 404
            ? "This decision is not recorded yet. Retry the same request to reconcile its outcome."
            : "The recorded decision could not be verified. Keep this request and retry recovery."}
        </p>
      )}
      <div className="flex flex-wrap gap-3">
        <Button
          type={retained ? "button" : "submit"}
          onClick={retained ? retry : undefined}
          disabled={save.isPending || recover.isPending}
        >
          {retained ? "Retry same decision" : "Record decision"}
        </Button>
        {retained && (
          <Button
            type="button"
            variant="outline"
            disabled={save.isPending || recover.isPending}
            onClick={() => recover.mutate()}
          >
            Check recorded decision
          </Button>
        )}
        {refused && (
          <Button type="button" variant="outline" onClick={onDiscard}>
            Close refused request
          </Button>
        )}
      </div>
    </form>
  );
}
