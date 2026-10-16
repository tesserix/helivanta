"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  formatMinutes,
  noteRequiredFor,
  overtimeDecisionFormSchema,
  toOvertimeDecision,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  OVERTIME_STATUSES,
  type OvertimeDecisionForm,
  type OvertimeEvent,
  type OvertimeRequest,
} from "@helivanta/api";
import { ConfirmDialog, Field, useZodForm } from "@helivanta/ui";
import { Failure, inputClass } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { describeOvertime } from "./overtime-workspace";

type Action = "approve" | "reject" | "withdraw" | "revoke";
const actions: Record<Action, { path: string; toast: string; pending: string; label: string }> = {
  approve: {
    path: "decision",
    toast: "Overtime approved",
    pending: "Approving…",
    label: "Approve overtime",
  },
  reject: {
    path: "decision",
    toast: "Overtime rejected",
    pending: "Rejecting…",
    label: "Reject overtime",
  },
  withdraw: {
    path: "cancellation",
    toast: "Overtime withdrawn",
    pending: "Withdrawing…",
    label: "Withdraw overtime",
  },
  revoke: {
    path: "revocation",
    toast: "Overtime revoked",
    pending: "Revoking…",
    label: "Revoke overtime",
  },
};

export function OvertimeReview({
  hospital,
  base,
  request,
  filer,
  approver,
  onClose,
}: {
  hospital: string;
  base: string;
  request: OvertimeRequest;
  filer: boolean;
  approver: boolean;
  onClose: () => void;
}) {
  const path = `${base}/hr/overtime/${request.id}`;
  const detail = useApiQuery<OvertimeRequest>(
    ["workforce", "overtime-request", hospital, request.id],
    path,
  );
  const history = useApiPagedQuery<OvertimeEvent>(
    ["workforce", "overtime-history", hospital, request.id],
    `${path}/history`,
  );
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const current = detail.data ?? request;
  return (
    <div role="region" aria-label="Overtime request" className="border-t p-5">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          {current.display_name} · {OVERTIME_STATUSES[current.status]}
        </h4>
        <Button variant="ghost" onClick={onClose}>
          Close overtime
        </Button>
      </div>
      {detail.isPending ? (
        <p role="status">Loading overtime…</p>
      ) : detail.isError ? (
        <Failure
          message="Could not load the overtime request."
          retry={() => void detail.refetch()}
        />
      ) : (
        <div className="space-y-6">
          <div className="grid gap-4 text-sm sm:grid-cols-2">
            <p>
              {describeOvertime(detail.data)} · {detail.data.timezone}
            </p>
            <p>Version {detail.data.version}</p>
            <p>
              Approved{" "}
              {detail.data.approved_minutes === undefined
                ? "—"
                : formatMinutes(detail.data.approved_minutes)}{" "}
              · payable {formatMinutes(detail.data.payable_minutes)}
            </p>
            {detail.data.reason && <p className="sm:col-span-2">Reason: {detail.data.reason}</p>}
          </div>
          <Decision
            hospital={hospital}
            path={path}
            request={detail.data}
            filer={filer}
            approver={approver}
          />
        </div>
      )}
      <section aria-label="Overtime history" className="mt-6">
        <h5 className="mb-2 text-sm font-semibold">History</h5>
        {history.isPending ? (
          <p role="status">Loading history…</p>
        ) : history.isError ? (
          <Failure message="Could not load the overtime history." retry={history.retry} />
        ) : (
          <ul className="space-y-2 text-sm">
            {history.items.map((e) => (
              <li key={e.id} className="rounded-xl border p-3">
                <p className="font-medium">
                  {OVERTIME_STATUSES[e.status]} · version {e.version}
                  {e.approved_minutes !== undefined
                    ? ` · ${formatMinutes(e.approved_minutes)} approved`
                    : ""}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {new Date(e.created_at).toLocaleString()} · by {e.actor_subject}
                  {e.note ? ` · ${e.note}` : ""}
                </p>
              </li>
            ))}
          </ul>
        )}
        {history.hasMore && (
          <Button
            className="mt-3"
            variant="outline"
            disabled={history.isFetchingMore}
            onClick={history.loadMore}
          >
            Load more history
          </Button>
        )}
      </section>
    </div>
  );
}

function useOvertimeAction(
  act: Action,
  hospital: string,
  path: string,
  version: number,
  done: () => void,
) {
  return useApiMutation(
    (form: OvertimeDecisionForm) =>
      apiFetch<OvertimeRequest>(`${path}/${actions[act].path}`, {
        method: "POST",
        body: JSON.stringify(
          act === "approve" || act === "reject"
            ? toOvertimeDecision(version, act, form)
            : { expected_version: version, note: form.note },
        ),
      }),
    {
      invalidate: [
        ["workforce", "overtime", hospital],
        ["workforce", "overtime-request", hospital],
        ["workforce", "overtime-history", hospital],
        ["workforce", "overtime-summary", hospital],
      ],
      successToast: actions[act].toast,
      suppressErrorToast: true,
      onSuccess: done,
    },
  );
}

function Decision({
  hospital,
  path,
  request,
  filer,
  approver,
}: {
  hospital: string;
  path: string;
  request: OvertimeRequest;
  filer: boolean;
  approver: boolean;
}) {
  const [confirming, setConfirming] = useState(false);
  const blank = { approved_minutes: request.requested_minutes, note: "" };
  const form = useZodForm(overtimeDecisionFormSchema(request.requested_minutes), blank);
  const done = () => {
    setConfirming(false);
    form.reset(blank);
  };
  const mutations: Record<Action, ReturnType<typeof useOvertimeAction>> = {
    approve: useOvertimeAction("approve", hospital, path, request.version, done),
    reject: useOvertimeAction("reject", hospital, path, request.version, done),
    withdraw: useOvertimeAction("withdraw", hospital, path, request.version, done),
    revoke: useOvertimeAction("revoke", hospital, path, request.version, done),
  };
  const busy = Object.values(mutations).some((m) => m.isPending);
  const failed = Object.values(mutations).find((m) => m.isError);
  const available: Action[] =
    request.status === "pending"
      ? [
          ...(filer ? (["withdraw"] as const) : []),
          ...(approver ? (["reject", "approve"] as const) : []),
        ]
      : request.status === "approved" && approver
        ? ["revoke"]
        : [];
  if (available.length === 0) return null;
  const submit = (act: Action) =>
    form.handleSubmit((values) => {
      for (const m of Object.values(mutations)) m.reset();
      if (noteRequiredFor(act, values, request.requested_minutes) && !values.note)
        return form.setError("note", {
          message:
            act === "approve"
              ? "Add a note explaining why fewer minutes are approved."
              : "Add a note explaining the decision.",
        });
      if (act === "revoke") return setConfirming(true);
      mutations[act].mutate(values);
    })();
  const errors = form.formState.errors;
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label="Decide overtime"
      onSubmit={(e) => e.preventDefault()}
      className="space-y-4 rounded-xl border p-4"
    >
      <fieldset disabled={busy} className="grid gap-4 sm:grid-cols-2">
        {request.status === "pending" && approver && (
          <Field
            id="overtime-approved-minutes"
            label={`Minutes to approve (of ${request.requested_minutes})`}
            error={errors.approved_minutes?.message}
          >
            <input
              id="overtime-approved-minutes"
              type="number"
              inputMode="numeric"
              className={inputClass}
              aria-invalid={!!errors.approved_minutes}
              {...form.register("approved_minutes", { valueAsNumber: true })}
            />
          </Field>
        )}
        <Field
          id="overtime-decision-note"
          label="Note (required to reject, revoke or approve fewer minutes)"
          error={errors.note?.message}
        >
          <textarea
            id="overtime-decision-note"
            rows={2}
            className={inputClass}
            aria-invalid={!!errors.note}
            {...form.register("note")}
          />
        </Field>
      </fieldset>
      {failed && <SaveError message={failed.error?.message ?? ""} />}
      <div className="flex flex-wrap justify-end gap-2">
        {available.map((act) => (
          <Button
            key={act}
            type="button"
            variant={act === "approve" ? "default" : act === "withdraw" ? "ghost" : "outline"}
            disabled={busy}
            onClick={() => submit(act)}
          >
            {mutations[act].isPending ? actions[act].pending : actions[act].label}
          </Button>
        ))}
      </div>
      <ConfirmDialog
        open={confirming && !mutations.revoke.isError}
        onOpenChange={(open) => !open && setConfirming(false)}
        title="Revoke this overtime?"
        description="The approved minutes stop being payable. The request cannot be approved again."
        confirmLabel="Revoke overtime"
        busy={mutations.revoke.isPending}
        onConfirm={() =>
          mutations.revoke.mutate({ ...form.getValues(), note: form.getValues("note").trim() })
        }
      />
    </form>
  );
}
