"use client";

import { useEffect, useRef, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Field, useLeaveGuard, useZodForm } from "@helivanta/ui";
import {
  ApiError,
  StaffPostingDecisionInputContract,
  decideStaffPostingProposal,
  getStaffPostingDecisionReceipt,
  newStaffPostingId,
  useApiMutation,
  useStaffPostingReviewSource,
  onTenantTransition,
  tenantTransitionPending,
  type StaffPostingProposalScope,
  type StaffPostingProposal,
  type StaffPostingDecisionInput,
} from "@helivanta/api";
import { STAFF_ACCESS_CHANGED, useStaffAccessCutoff } from "./staff-access-cutoff";

const formSchema = z.object({
  reason: StaffPostingDecisionInputContract.shape.reason,
  sources_examined: StaffPostingDecisionInputContract.shape.sources_examined,
});
type Command = { id: string; input: StaffPostingDecisionInput };

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
}: {
  contextAvailable: boolean;
  onContextRecheck: () => void;
  scope: StaffPostingProposalScope;
  proposal: StaffPostingProposal;
  action: "reject" | "withdraw" | null;
  busy: boolean;
  onBusy: (value: boolean) => void;
  onCompleted: (row: StaffPostingProposal) => void;
  onContextChanged: () => void;
}) {
  const [retained, setRetained] = useState<Command | null>(null);
  const original = useRef<Command | null>(null);
  const active = useRef<AbortController | null>(null);
  const source = useStaffPostingReviewSource(scope, busy);
  const form = useZodForm(formSchema, { reason: "", sources_examined: false });
  const save = useApiMutation(
    async ({ command, recover }: { command: Command; recover: boolean }) => {
      const controller = new AbortController();
      active.current = controller;
      const row = await (recover ? getStaffPostingDecisionReceipt : decideStaffPostingProposal)(
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
      },
      onSuccess: (row) => {
        original.current = null;
        setRetained(null);
        form.reset();
        onBusy(false);
        onCompleted(row);
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
  const available = contextAvailable && sourceAvailable;
  const matches =
    sourceAvailable &&
    source.data.generation_state === "current" &&
    source.data.generation_id === proposal.generation_id &&
    source.data.staff_version === proposal.staff_version &&
    source.data.employment_epoch === proposal.employment_epoch;
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
      {action && proposal.status === "pending" && (
        <form
          noValidate
          aria-label="Posting decision"
          className="space-y-4"
          onSubmit={(event) => {
            void form.handleSubmit((values) => {
              if (original.current || save.isPending || !available) return;
              const parsed = StaffPostingDecisionInputContract.safeParse({
                ...values,
                decision: action,
                expected_version: proposal.version,
                expected_revision_id: proposal.revision_id,
              });
              if (!parsed.success) {
                parsed.error.issues.forEach((issue) => {
                  const key = issue.path[0];
                  if (key === "reason" || key === "sources_examined")
                    form.setError(key, { message: issue.message });
                });
                return;
              }
              const command = { id: newStaffPostingId(), input: parsed.data };
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
              />
            </Field>
          </fieldset>
          {save.error && (
            <p role="alert">
              The decision outcome could not be confirmed. Recover its receipt or retry the same
              decision. A missing receipt may still belong to an in-flight request.
            </p>
          )}
          <div className="flex flex-wrap gap-2">
            {retained ? (
              <>
                <Button
                  type="button"
                  disabled={save.isPending}
                  onClick={() => save.mutate({ command: retained, recover: false })}
                >
                  Retry exact decision
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={save.isPending}
                  onClick={() => save.mutate({ command: retained, recover: true })}
                >
                  Recover decision receipt
                </Button>
              </>
            ) : (
              <Button type="submit" disabled={!available || save.isPending}>
                {action === "reject" ? "Reject proposal" : "Withdraw proposal"}
              </Button>
            )}
          </div>
        </form>
      )}
    </section>
  );
}
