"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  formatHalfDays,
  leaveCancelInputContract,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  LEAVE_REQUEST_STATUSES,
  type LeaveRequest,
  type LeaveRequestDetail,
  type LeaveRequestEvent,
} from "@helivanta/api";
import { ConfirmDialog, Field, useZodForm } from "@helivanta/ui";
import { Failure, inputClass } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { describeLeave } from "./requests-workspace";

const noteSchema = leaveCancelInputContract.pick({ note: true });
type Action = "approve" | "reject" | "withdraw" | "revoke";
const actions: Record<Action, { path: string; toast: string; pending: string }> = {
  approve: { path: "decision", toast: "Leave approved", pending: "Approving…" },
  reject: { path: "decision", toast: "Leave rejected", pending: "Rejecting…" },
  withdraw: { path: "cancellation", toast: "Request withdrawn", pending: "Withdrawing…" },
  revoke: { path: "revocation", toast: "Approved leave cancelled", pending: "Cancelling…" },
};

export function LeaveRequestReview({
  hospital,
  base,
  request,
  filer,
  approver,
  onClose,
}: {
  hospital: string;
  base: string;
  request: LeaveRequest;
  filer: boolean;
  approver: boolean;
  onClose: () => void;
}) {
  const path = `${base}/staff/${request.staff_id}/leave-requests/${request.id}`;
  const detail = useApiQuery<LeaveRequestDetail>(
    ["workforce", "leave-request", hospital, request.id],
    path,
  );
  const history = useApiPagedQuery<LeaveRequestEvent>(
    ["workforce", "leave-request-history", hospital, request.id],
    `${path}/history`,
  );
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const current = detail.data?.request ?? request;
  return (
    <div role="region" aria-label="Leave request" className="border-t p-5">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          {current.display_name} · {current.name} · {LEAVE_REQUEST_STATUSES[current.status]}
        </h4>
        <Button variant="ghost" onClick={onClose}>
          Close request
        </Button>
      </div>
      {detail.isPending ? (
        <p role="status">Loading request…</p>
      ) : detail.isError ? (
        <Failure message="Could not load the leave request." retry={() => void detail.refetch()} />
      ) : (
        <div className="space-y-6">
          <div className="grid gap-4 text-sm sm:grid-cols-2">
            <p>{describeLeave(detail.data.request)}</p>
            <p>
              Leave year {detail.data.request.leave_year} · version {detail.data.request.version}
            </p>
            {detail.data.request.reason && (
              <p className="sm:col-span-2">Reason: {detail.data.request.reason}</p>
            )}
          </div>
          <section aria-label="Days counted">
            <h5 className="mb-2 text-sm font-semibold">Days counted</h5>
            <ul className="flex flex-wrap gap-2 text-xs">
              {detail.data.days.map((d) => (
                <li key={d.day} className="rounded-full bg-muted px-2.5 py-1">
                  {d.day} · {d.holiday ? "holiday, not counted" : formatHalfDays(d.half_days)}
                </li>
              ))}
            </ul>
          </section>
          <section aria-label="Roster clashes">
            <h5 className="mb-2 text-sm font-semibold">Roster clashes</h5>
            {detail.data.roster_clashes.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                No published or draft roster allocations fall on these dates.
              </p>
            ) : (
              <ul className="space-y-1 text-sm">
                {detail.data.roster_clashes.map((r) => (
                  <li key={`${r.roster_id}:${r.starts_at}`}>
                    {r.roster_name} · {r.roster_date} ·{" "}
                    {new Date(r.starts_at).toLocaleTimeString([], { timeStyle: "short" })}–
                    {new Date(r.ends_at).toLocaleTimeString([], { timeStyle: "short" })}
                  </li>
                ))}
              </ul>
            )}
          </section>
          {!detail.isFetching &&
            !history.isPending &&
            !history.isError &&
            !history.isRefreshing && (
              <Decision
                hospital={hospital}
                path={path}
                request={detail.data.request}
                filer={filer}
                approver={approver}
              />
            )}
        </div>
      )}
      <section aria-label="Request history" className="mt-6">
        <h5 className="mb-2 text-sm font-semibold">History</h5>
        {detail.isError ? (
          <p className="text-sm text-muted-foreground">
            Load the request again before viewing its history.
          </p>
        ) : history.isPending ? (
          <p role="status">Loading history…</p>
        ) : history.isError ? (
          <Failure message="Could not load the request history." retry={history.retry} />
        ) : (
          <ul className="space-y-2 text-sm">
            {history.items.map((e) => (
              <li key={e.id} className="rounded-xl border p-3">
                <p className="font-medium">
                  {LEAVE_REQUEST_STATUSES[e.status]} · version {e.version}
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

function useLeaveAction(
  act: Action,
  hospital: string,
  path: string,
  version: number,
  done: () => void,
) {
  return useApiMutation(
    (note: string) =>
      apiFetch<LeaveRequestDetail>(`${path}/${actions[act].path}`, {
        method: "POST",
        body: JSON.stringify({
          expected_version: version,
          ...(actions[act].path === "decision" && { decision: act }),
          note,
        }),
      }),
    {
      invalidate: [
        ["workforce", "leave-requests", hospital],
        ["workforce", "leave-request", hospital],
        ["workforce", "leave-request-history", hospital],
        ["workforce", "leave-balances", hospital],
        ["workforce", "leave-ledger", hospital],
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
  request: LeaveRequest;
  filer: boolean;
  approver: boolean;
}) {
  const [confirming, setConfirming] = useState(false);
  const form = useZodForm(noteSchema, { note: "" });
  const done = () => {
    setConfirming(false);
    form.reset({ note: "" });
  };
  const mutations: Record<Action, ReturnType<typeof useLeaveAction>> = {
    approve: useLeaveAction("approve", hospital, path, request.version, done),
    reject: useLeaveAction("reject", hospital, path, request.version, done),
    withdraw: useLeaveAction("withdraw", hospital, path, request.version, done),
    revoke: useLeaveAction("revoke", hospital, path, request.version, done),
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
  const labels: Record<Action, string> = {
    approve: "Approve leave",
    reject: "Reject leave",
    withdraw: "Withdraw request",
    revoke: "Cancel approved leave",
  };
  const submit = (act: Action) =>
    form.handleSubmit(({ note }) => {
      for (const m of Object.values(mutations)) m.reset();
      if ((act === "reject" || act === "revoke") && !note)
        return form.setError("note", { message: "Add a note explaining the decision." });
      if (act === "revoke") return setConfirming(true);
      mutations[act].mutate(note);
    })();
  const errors = form.formState.errors;
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label="Decide leave request"
      onSubmit={(e) => e.preventDefault()}
      className="space-y-4 rounded-xl border p-4"
    >
      <fieldset disabled={busy} className="space-y-4">
        <Field
          id="leave-decision-note"
          label="Note (required to reject or cancel approved leave)"
          error={errors.note?.message}
        >
          <textarea
            id="leave-decision-note"
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
            {mutations[act].isPending ? actions[act].pending : labels[act]}
          </Button>
        ))}
      </div>
      <ConfirmDialog
        open={confirming && !mutations.revoke.isError}
        onOpenChange={(open) => !open && setConfirming(false)}
        title="Cancel approved leave?"
        description="The days return to the staff member's balance through a reversal entry in the leave ledger. The request cannot be approved again."
        confirmLabel="Cancel approved leave"
        busy={mutations.revoke.isPending}
        onConfirm={() => mutations.revoke.mutate(form.getValues("note"))}
      />
    </form>
  );
}
