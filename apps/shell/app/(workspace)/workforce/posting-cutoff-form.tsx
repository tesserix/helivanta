"use client";

import { useEffect, useRef, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Field, SelectControl, useLeaveGuard, useZodForm } from "@helivanta/ui";
import {
  ApiError,
  StaffPostingCutoffInputContract,
  newStaffPostingId,
  recordStaffPostingCutoff,
  getStaffPostingCutoffReceipt,
  useApiMutation,
  onTenantTransition,
  tenantTransitionPending,
  staffPostingInstantMicros,
  type StaffPostingProposalScope,
  type StaffPostingProposal,
  type StaffPostingCutoff,
  type StaffPostingCutoffInput,
} from "@helivanta/api";
import { STAFF_ACCESS_CHANGED, useStaffAccessCutoff } from "./staff-access-cutoff";

const cutoffFormSchema = z.object({
  reason: StaffPostingCutoffInputContract.shape.reason,
  sources_examined: StaffPostingCutoffInputContract.shape.sources_examined,
  mode: StaffPostingCutoffInputContract.shape.mode,
  scheduled_at: z.string(),
});
type CutoffCommand = { id: string; input: StaffPostingCutoffInput };
type Props = {
  scope: StaffPostingProposalScope;
  proposal: StaffPostingProposal;
  current: StaffPostingCutoff | null;
  checkedAt: string;
  available: boolean;
  onBusy: (value: boolean) => void;
  onContextChanged: () => void;
  onRecheck: () => void;
  onRecorded: () => void;
};

export function PostingCutoffForm({
  scope,
  proposal,
  current,
  checkedAt,
  available,
  onBusy,
  onContextChanged,
  onRecheck,
  onRecorded,
}: Props) {
  const form = useZodForm(cutoffFormSchema, {
    reason: "",
    sources_examined: false,
    mode: "immediate",
    scheduled_at: "",
  });
  const [retained, setRetained] = useState<CutoffCommand | null>(null);
  const original = useRef<CutoffCommand | null>(null);
  const observation = `${proposal.revision_id}:${current?.id ?? "none"}:${current?.version ?? 0}`;
  const examined = useRef<string | null>(null);
  const active = useRef<AbortController | null>(null);
  const save = useApiMutation(
    async ({ command, recover }: { command: CutoffCommand; recover: boolean }) => {
      const controller = new AbortController();
      active.current = controller;
      const row = await (recover ? getStaffPostingCutoffReceipt : recordStaffPostingCutoff)(
        scope,
        proposal.id,
        command.id,
        command.input,
        controller.signal,
      );
      controller.signal.throwIfAborted();
      return row;
    },
    {
      suppressErrorToast: true,
      networkMode: "always",
      onSuccess: () => {
        original.current = null;
        setRetained(null);
        form.reset();
        onBusy(false);
        onRecorded();
      },
      onError: (error) => {
        if (error instanceof ApiError && (error.status === 404 || error.status === 409))
          onRecheck();
      },
    },
  );
  const dirty = form.formState.isDirty || retained !== null;
  useLeaveGuard(
    dirty,
    "posting cutoff; an unconfirmed command may already be recorded",
    save.isPending,
  );
  useEffect(() => {
    onBusy(dirty || save.isPending);
  }, [dirty, save.isPending, onBusy]);
  useEffect(() => {
    if (
      !original.current &&
      form.getValues("sources_examined") &&
      examined.current !== observation
    ) {
      form.setValue("sources_examined", false, { shouldDirty: true });
      form.setError("sources_examined", {
        message: "The cutoff changed. Examine the current history again.",
      });
    }
  }, [form, observation]);
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
  const cutoff = useStaffAccessCutoff([missingReceipt ? null : save.error], onContextChanged);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  const ended =
    current !== null &&
    staffPostingInstantMicros(current.cutoff_at)! <= staffPostingInstantMicros(checkedAt)!;
  if (ended && !retained)
    return (
      <p>This posting cutoff is already effective. Its approval and cutoff history are retained.</p>
    );
  const scheduled = form.watch("scheduled_at");
  const candidate = scheduled.length === 16 ? scheduled + ":00Z" : scheduled + "Z";
  const preview = StaffPostingCutoffInputContract.shape.cutoff_at.safeParse(candidate);
  const examinationField = form.register("sources_examined");
  return (
    <form
      noValidate
      aria-label="Posting cutoff"
      className="space-y-4 border-t border-border pt-4"
      onSubmit={(event) => {
        void form.handleSubmit((values) => {
          if (original.current || save.isPending || !available) return;
          if (values.sources_examined && examined.current !== observation) {
            form.setValue("sources_examined", false, { shouldDirty: true });
            form.setError("sources_examined", {
              message: "The cutoff changed. Examine the current history again.",
            });
            return;
          }
          const cutoffAt =
            values.mode === "scheduled"
              ? values.scheduled_at.length === 16
                ? values.scheduled_at + ":00Z"
                : values.scheduled_at + "Z"
              : undefined;
          const parsed = StaffPostingCutoffInputContract.safeParse({
            expected_revision_id: proposal.revision_id,
            expected_cutoff_version: current?.version ?? 0,
            mode: values.mode,
            reason: values.reason,
            sources_examined: values.sources_examined,
            ...(cutoffAt === undefined ? {} : { cutoff_at: cutoffAt }),
          });
          if (!parsed.success) {
            for (const issue of parsed.error.issues) {
              const key = issue.path[0];
              if (key === "reason" || key === "sources_examined" || key === "mode")
                form.setError(key, { message: issue.message });
              if (key === "cutoff_at") form.setError("scheduled_at", { message: issue.message });
            }
            return;
          }
          if (
            parsed.data.mode === "scheduled" &&
            staffPostingInstantMicros(parsed.data.cutoff_at!)! <=
              staffPostingInstantMicros(new Date().toISOString())!
          ) {
            form.setError("scheduled_at", { message: "Choose a future cutoff instant." });
            return;
          }
          if (
            current &&
            parsed.data.mode === "scheduled" &&
            staffPostingInstantMicros(parsed.data.cutoff_at!)! >=
              staffPostingInstantMicros(current.cutoff_at)!
          ) {
            form.setError("scheduled_at", {
              message: "Choose a cutoff earlier than the retained cutoff.",
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
      <fieldset disabled={!available || save.isPending || retained !== null} className="space-y-4">
        <Field
          id="posting-cutoff-mode"
          label="Ending mode"
          error={form.formState.errors.mode?.message}
        >
          <SelectControl
            id="posting-cutoff-mode"
            value={form.watch("mode")}
            onValueChange={(value) =>
              form.setValue("mode", StaffPostingCutoffInputContract.shape.mode.parse(value), {
                shouldDirty: true,
              })
            }
            options={[
              { value: "immediate", label: "End immediately" },
              { value: "scheduled", label: "Schedule an earlier cutoff" },
            ]}
          />
        </Field>
        {form.watch("mode") === "scheduled" && (
          <>
            <Field
              id="posting-cutoff-at"
              label="Scheduled cutoff (UTC)"
              error={form.formState.errors.scheduled_at?.message}
            >
              <Input
                id="posting-cutoff-at"
                type="datetime-local"
                step="1"
                {...form.register("scheduled_at")}
              />
            </Field>
            {preview.success && preview.data && (
              <p className="text-sm">
                Selected instant: {new Date(preview.data).toISOString()} (UTC). Local preview (
                {Intl.DateTimeFormat().resolvedOptions().timeZone}):{" "}
                {new Date(preview.data).toLocaleString()}.
              </p>
            )}
          </>
        )}
        <Field
          id="posting-cutoff-reason"
          label="Cutoff rationale"
          error={form.formState.errors.reason?.message}
        >
          <Input id="posting-cutoff-reason" {...form.register("reason")} />
        </Field>
        <Field
          id="posting-cutoff-examined"
          label="I examined the approved posting and its current cutoff history."
          error={form.formState.errors.sources_examined?.message}
        >
          <input
            id="posting-cutoff-examined"
            type="checkbox"
            {...examinationField}
            onChange={(event) => {
              examined.current = event.target.checked ? observation : null;
              void examinationField.onChange(event);
            }}
          />
        </Field>
      </fieldset>
      {save.error && (
        <p role="alert">
          The cutoff outcome is unconfirmed. Recover its receipt or retry the same command.
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        {retained ? (
          <>
            <Button
              type="button"
              disabled={
                save.isPending ||
                !available ||
                (((current?.version ?? 0) !== retained.input.expected_cutoff_version || ended) &&
                  current?.id !== retained.id)
              }
              onClick={() => save.mutate({ command: retained, recover: false })}
            >
              Retry exact cutoff
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={save.isPending}
              onClick={() => save.mutate({ command: retained, recover: true })}
            >
              Recover cutoff receipt
            </Button>
          </>
        ) : (
          <Button type="submit" disabled={!available || save.isPending}>
            Record posting cutoff
          </Button>
        )}
      </div>
    </form>
  );
}
