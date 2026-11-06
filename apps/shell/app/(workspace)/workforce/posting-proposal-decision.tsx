"use client";

import { useEffect, useRef, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Field, useLeaveGuard, useZodForm } from "@helivanta/ui";
import {
  ApiError,
  StaffPostingDecisionInputContract,
  decideStaffPostingProposal,
  transferStaffPosting,
  type StaffPostingCutoffContext,
  decideStaffPostingLifecycleProposal,
  getStaffPostingDecisionReceipt,
  decideStaffPostingSupervisedProposal,
  getStaffPostingSupervisedDecisionReceipt,
  newStaffPostingId,
  useApiMutation,
  useStaffPostingReviewSource,
  onTenantTransition,
  tenantTransitionPending,
  type StaffPostingProposalScope,
  type StaffPostingProposal,
  type StaffPostingDecisionInput,
  type StaffPostingLifecycleReviewContext,
} from "@helivanta/api";
import { STAFF_ACCESS_CHANGED, useStaffAccessCutoff } from "./staff-access-cutoff";

const formSchema = z.object({
  reason: StaffPostingDecisionInputContract.shape.reason,
  sources_examined: StaffPostingDecisionInputContract.shape.sources_examined,
  hospital_management_reviewed: z.boolean(),
  chain_reviewed: z.boolean(),
});
type Command = {
  id: string;
  input: StaffPostingDecisionInput;
  lifecycle?: StaffPostingLifecycleReviewContext;
  transferFrom?: StaffPostingCutoffContext;
};

export function PostingProposalDecision({
  scope,
  proposal,
  action,
  busy,
  onBusy,
  onCompleted,
  onContextChanged,
  contextAvailable,
  onContextRecheck,
  approvalKind = "hospital_management",
  approvalUntil,
  lifecycleContext,
  transferFrom,
}: {
  transferFrom?: StaffPostingCutoffContext;
  lifecycleContext?: StaffPostingLifecycleReviewContext;
  approvalKind?: "hospital_management" | "supervised";
  approvalUntil?: string;
  contextAvailable: boolean;
  onContextRecheck: () => void;
  scope: StaffPostingProposalScope;
  proposal: StaffPostingProposal;
  action: "approve" | "reject" | "withdraw" | null;
  busy: boolean;
  onBusy: (value: boolean) => void;
  onCompleted: (row: StaffPostingProposal, transferred?: boolean) => void;
  onContextChanged: () => void;
}) {
  const [now, setNow] = useState(() => Date.now());
  const deadlines = [proposal.ends_at, approvalUntil]
    .filter((value): value is string => value != null)
    .map((value) => Date.parse(value));
  const endsAt = deadlines.length ? Math.min(...deadlines) : null;
  useEffect(() => {
    if (endsAt === null) return;
    let timer: ReturnType<typeof setTimeout>;
    const tick = () => {
      const current = Date.now();
      setNow(current);
      if (current < endsAt) timer = setTimeout(tick, Math.min(endsAt - current, 60_000));
    };
    tick();
    return () => clearTimeout(timer);
  }, [endsAt]);
  const ended = endsAt !== null && now >= endsAt;
  const [retained, setRetained] = useState<Command | null>(null);
  const original = useRef<Command | null>(null);
  const active = useRef<AbortController | null>(null);
  const observationKey = lifecycleContext
    ? JSON.stringify([
        transferFrom ?? null,
        lifecycleContext.context.proposal.revision_id,
        lifecycleContext.effective_until,
        lifecycleContext.cutoff,
        ...lifecycleContext.chain.map((node) => [
          node.posting.id,
          node.posting.revision_id,
          node.cutoff,
        ]),
      ])
    : null;
  const examinedObservation = useRef<string | null>(null);
  const reviewedObservation = useRef<string | null>(null);
  const source = useStaffPostingReviewSource(scope, busy);
  const form = useZodForm(formSchema, {
    reason: "",
    sources_examined: false,
    hospital_management_reviewed: false,
    chain_reviewed: false,
  });
  useEffect(() => {
    if (original.current || observationKey === null) return;
    if (examinedObservation.current !== observationKey)
      form.setValue("sources_examined", false, { shouldDirty: true });
    if (reviewedObservation.current !== observationKey) {
      form.setValue("hospital_management_reviewed", false, { shouldDirty: true });
      form.setValue("chain_reviewed", false, { shouldDirty: true });
    }
    form.clearErrors();
  }, [observationKey, form]);
  const save = useApiMutation(
    async ({ command, recover }: { command: Command; recover: boolean }) => {
      const controller = new AbortController();
      active.current = controller;
      if (command.lifecycle && command.transferFrom) {
        const old = command.transferFrom;
        const result = await transferStaffPosting(
          scope,
          command.lifecycle.context.proposal.id,
          command.id,
          {
            from_posting_id: old.proposal.id,
            from_revision_id: old.proposal.revision_id,
            from_version: old.proposal.version,
            from_cutoff_version: old.cutoff?.version ?? 0,
            decision: command.input,
          },
          command.lifecycle,
          old,
          controller.signal,
        );
        return result.replacement;
      }
      if (command.lifecycle) {
        return decideStaffPostingLifecycleProposal(
          scope,
          command.lifecycle.context.proposal.id,
          command.id,
          command.input,
          command.lifecycle,
          controller.signal,
        );
      }
      const decide =
        approvalKind === "supervised"
          ? decideStaffPostingSupervisedProposal
          : decideStaffPostingProposal;
      const receipt =
        approvalKind === "supervised"
          ? getStaffPostingSupervisedDecisionReceipt
          : getStaffPostingDecisionReceipt;
      const row = await (recover ? receipt : decide)(
        scope,
        proposal.id,
        command.id,
        command.input,
        proposal,
        controller.signal,
      );
      controller.signal.throwIfAborted();
      return row;
    },
    {
      suppressErrorToast: true,
      networkMode: "always",
      onError: (error) => {
        if (error instanceof ApiError && error.status === 404 && error.code === "not_found")
          onContextRecheck();
        if (
          error instanceof ApiError &&
          error.status === 409 &&
          [
            "posting_proposal_conflict",
            "posting_cutoff_conflict",
            "posting_lifecycle_review_required",
          ].includes(error.code)
        ) {
          void source.refetch();
          onContextRecheck();
        }
      },
      onSuccess: (row) => {
        const transferred = original.current?.transferFrom !== undefined;
        original.current = null;
        setRetained(null);
        form.reset();
        onBusy(false);
        if (transferred) onCompleted(row, true);
        else onCompleted(row);
      },
    },
  );
  const dirty = form.formState.isDirty || retained !== null;
  useLeaveGuard(
    dirty,
    "posting decision; an unconfirmed decision may already be recorded",
    save.isPending,
  );
  useEffect(() => {
    onBusy(dirty || save.isPending);
  }, [dirty, save.isPending, onBusy]);
  useEffect(() => {
    const stop = onTenantTransition(() => {
      if (tenantTransitionPending()) active.current?.abort();
    });
    return () => {
      stop();
      active.current?.abort();
      onBusy(false);
    };
  }, [onBusy]);
  const missingReceipt =
    save.variables?.recover &&
    save.error instanceof ApiError &&
    save.error.status === 404 &&
    save.error.code === "not_found";
  const cutoff = useStaffAccessCutoff(
    [source.error, missingReceipt ? null : save.error],
    onContextChanged,
  );
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  const sourceAvailable = source.isSuccess && !source.isFetching;
  const matches =
    sourceAvailable &&
    source.data.generation_state === "current" &&
    source.data.generation_id === proposal.generation_id &&
    source.data.staff_version === proposal.staff_version &&
    source.data.employment_epoch === proposal.employment_epoch;
  const available =
    contextAvailable && sourceAvailable && (action !== "approve" || (matches && !ended));
  return (
    <section
      className="space-y-4 border-t border-border pt-4"
      aria-label="Posting decision and employment source"
    >
      <h4 className="font-medium">Current employment source</h4>
      {source.isPending || source.isFetching ? (
        <p role="status">Checking current employment source…</p>
      ) : source.isError ? (
        <div role="alert">
          The current employment source could not be verified.{" "}
          <Button
            type="button"
            variant="outline"
            disabled={retained !== null}
            onClick={() => void source.refetch()}
          >
            Retry employment source
          </Button>
        </div>
      ) : (
        <>
          <p>
            {matches
              ? "The current employment source matches this submission."
              : "The employment source differs from this submission or is no longer current."}
          </p>
          <p className="text-sm text-muted-foreground">
            Source state: {source.data.generation_state.replaceAll("_", " ")} · Checked{" "}
            {new Date(source.data.checked_at).toISOString()} (UTC)
          </p>
        </>
      )}
      {lifecycleContext && (
        <Button
          type="button"
          variant="outline"
          disabled={save.isPending}
          onClick={onContextRecheck}
        >
          Recheck posting lifetimes
        </Button>
      )}
      {retained?.lifecycle && (
        <p className="text-sm text-muted-foreground">
          The retained approval uses the original examined ending of{" "}
          {new Date(retained.lifecycle.effective_until).toISOString()} (UTC). Recovery repeats that
          approval; changed sources require a new review.
        </p>
      )}
      {ended && (
        <p role="alert">
          {lifecycleContext
            ? "The effective responsibility has ended. A new approval cannot be recorded."
            : approvalUntil
              ? "The checked supervisor chain has expired. Recheck its sources."
              : "This posting has ended. A new approval cannot be recorded."}
        </p>
      )}
      {action && proposal.status === "pending" && (
        <form
          noValidate
          aria-label="Posting decision"
          className="space-y-4"
          onSubmit={(event) => {
            void form.handleSubmit((values) => {
              if (original.current || save.isPending || !available) return;
              if (
                action === "approve" &&
                lifecycleContext &&
                (examinedObservation.current !== observationKey ||
                  reviewedObservation.current !== observationKey)
              ) {
                form.setError("sources_examined", {
                  message: "Examine the current posting lifetimes again.",
                });
                return;
              }

              if (action === "approve" && endsAt !== null && Date.now() >= endsAt) {
                setNow(Date.now());
                return;
              }
              if (
                action === "approve" &&
                !(approvalKind === "supervised"
                  ? values.chain_reviewed
                  : values.hospital_management_reviewed)
              ) {
                form.setError(
                  approvalKind === "supervised" ? "chain_reviewed" : "hospital_management_reviewed",
                  {
                    message:
                      approvalKind === "supervised"
                        ? "Confirm your independent current supervisor chain review."
                        : "Confirm your independent hospital management review.",
                  },
                );
                return;
              }
              const parsed = StaffPostingDecisionInputContract.safeParse({
                reason: values.reason,
                sources_examined: values.sources_examined,
                ...(action === "approve" && approvalKind === "hospital_management"
                  ? { hospital_management_reviewed: values.hospital_management_reviewed }
                  : {}),
                ...(action === "approve" && lifecycleContext
                  ? {
                      lifecycle_review: {
                        cutoff_versions: [
                          lifecycleContext.cutoff.version,
                          ...lifecycleContext.chain.map((node) => node.cutoff.version),
                        ],
                        effective_until: lifecycleContext.effective_until,
                      },
                    }
                  : {}),
                decision: action,
                expected_version: proposal.version,
                expected_revision_id: proposal.revision_id,
              });
              if (!parsed.success) {
                parsed.error.issues.forEach((issue) => {
                  const key = issue.path[0];
                  if (
                    key === "reason" ||
                    key === "sources_examined" ||
                    key === "hospital_management_reviewed"
                  )
                    form.setError(key, { message: issue.message });
                });
                return;
              }
              const command = {
                id: newStaffPostingId(),
                input: parsed.data,
                ...(action === "approve" && lifecycleContext
                  ? {
                      lifecycle: structuredClone(lifecycleContext),
                      ...(transferFrom ? { transferFrom: structuredClone(transferFrom) } : {}),
                    }
                  : {}),
              };
              original.current = command;
              setRetained(command);
              save.mutate({ command, recover: false });
            })(event);
          }}
        >
          <fieldset
            disabled={!available || save.isPending || retained !== null}
            className="space-y-4"
          >
            <Field
              id="posting-decision-reason"
              label="Decision rationale"
              error={form.formState.errors.reason?.message}
            >
              <Input id="posting-decision-reason" {...form.register("reason")} />
            </Field>
            <Field
              id="posting-decision-examined"
              label="I examined the submitted proposal and current employment source."
              error={form.formState.errors.sources_examined?.message}
            >
              <input
                id="posting-decision-examined"
                type="checkbox"
                {...form.register("sources_examined")}
                onChange={(event) => {
                  examinedObservation.current = event.target.checked ? observationKey : null;
                  void form.register("sources_examined").onChange(event);
                }}
              />
            </Field>
            {action === "approve" && approvalKind === "hospital_management" && (
              <Field
                id="posting-management-reviewed"
                label="I independently reviewed the hospital management responsibility."
                error={form.formState.errors.hospital_management_reviewed?.message}
              >
                <input
                  id="posting-management-reviewed"
                  type="checkbox"
                  {...form.register("hospital_management_reviewed")}
                  onChange={(event) => {
                    reviewedObservation.current = event.target.checked ? observationKey : null;
                    void form.register("hospital_management_reviewed").onChange(event);
                  }}
                />
              </Field>
            )}
            {action === "approve" && approvalKind === "supervised" && (
              <Field
                id="posting-chain-reviewed"
                label="I independently reviewed the current supervisor chain."
                error={form.formState.errors.chain_reviewed?.message}
              >
                <input
                  id="posting-chain-reviewed"
                  type="checkbox"
                  {...form.register("chain_reviewed")}
                  onChange={(event) => {
                    reviewedObservation.current = event.target.checked ? observationKey : null;
                    void form.register("chain_reviewed").onChange(event);
                  }}
                />
              </Field>
            )}
          </fieldset>
          {save.error && (
            <p role="alert">
              {retained?.lifecycle
                ? "The approval outcome could not be confirmed. Recover or replay the retained approval. If it was not recorded, the same intent can succeed only while its examined sources remain valid."
                : "The decision outcome could not be confirmed. Recover its receipt or retry the same decision. A missing receipt may still belong to an in-flight request."}
            </p>
          )}
          <div className="flex flex-wrap gap-2">
            {retained ? (
              <>
                <Button
                  type="button"
                  disabled={
                    save.isPending ||
                    (action === "approve" && (ended || (sourceAvailable && !matches)))
                  }
                  onClick={() => {
                    if (action === "approve" && endsAt !== null && Date.now() >= endsAt) {
                      setNow(Date.now());
                      return;
                    }
                    save.mutate({ command: retained, recover: false });
                  }}
                >
                  Retry exact decision
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={save.isPending}
                  onClick={() => save.mutate({ command: retained, recover: true })}
                >
                  {retained.transferFrom
                    ? "Recover or replay transfer"
                    : retained.lifecycle
                      ? "Recover or replay approval"
                      : "Recover decision receipt"}
                </Button>
              </>
            ) : (
              <Button type="submit" disabled={!available || save.isPending}>
                {action === "approve"
                  ? transferFrom
                    ? "Approve coordinated transfer"
                    : "Approve proposal"
                  : action === "reject"
                    ? "Reject proposal"
                    : "Withdraw proposal"}
              </Button>
            )}
          </div>
        </form>
      )}
    </section>
  );
}
