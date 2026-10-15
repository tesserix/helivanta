"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  dutyCancelInputContract,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  DUTY_EXCHANGE_KINDS,
  DUTY_EXCHANGE_STATUSES,
  type DutyExchange,
  type DutyExchangeDetail,
  type DutyExchangeEvent,
} from "@helivanta/api";
import { ConfirmDialog, Field, useZodForm } from "@helivanta/ui";
import { Failure, inputClass } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { describeDates, describeDuty } from "./exchanges-workspace";

const noteSchema = dutyCancelInputContract.pick({ note: true });
type Action = "approve" | "reject" | "withdraw" | "revoke";
const actions: Record<Action, { path: string; toast: string; pending: string }> = {
  approve: { path: "decision", toast: "Exchange approved", pending: "Approving…" },
  reject: { path: "decision", toast: "Exchange rejected", pending: "Rejecting…" },
  withdraw: { path: "cancellation", toast: "Exchange withdrawn", pending: "Withdrawing…" },
  revoke: { path: "revocation", toast: "Exchange revoked", pending: "Revoking…" },
};

export function DutyExchangeReview({
  hospital,
  base,
  exchange,
  filer,
  approver,
  onClose,
}: {
  hospital: string;
  base: string;
  exchange: DutyExchange;
  filer: boolean;
  approver: boolean;
  onClose: () => void;
}) {
  const path = `${base}/hr/duty-exchanges/${exchange.id}`;
  const detail = useApiQuery<DutyExchangeDetail>(
    ["workforce", "duty-exchange", hospital, exchange.id],
    path,
  );
  const history = useApiPagedQuery<DutyExchangeEvent>(
    ["workforce", "duty-exchange-history", hospital, exchange.id],
    `${path}/history`,
  );
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const current = detail.data?.exchange ?? exchange;
  return (
    <div role="region" aria-label="Duty exchange" className="border-t p-5">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          {current.requester_display_name} → {current.counterpart_display_name} ·{" "}
          {DUTY_EXCHANGE_STATUSES[current.status]}
        </h4>
        <Button variant="ghost" onClick={onClose}>
          Close exchange
        </Button>
      </div>
      {detail.isPending ? (
        <p role="status">Loading exchange…</p>
      ) : detail.isError ? (
        <Failure message="Could not load the duty exchange." retry={() => void detail.refetch()} />
      ) : (
        <div className="space-y-6">
          <div className="grid gap-4 text-sm sm:grid-cols-2">
            <p>
              {DUTY_EXCHANGE_KINDS[detail.data.exchange.kind]} ·{" "}
              {describeDates(detail.data.exchange)}
            </p>
            <p>Version {detail.data.exchange.version}</p>
            {detail.data.exchange.reason && (
              <p className="sm:col-span-2">Reason: {detail.data.exchange.reason}</p>
            )}
          </div>
          <section aria-label="Duties">
            <h5 className="mb-2 text-sm font-semibold">Duties</h5>
            <ul className="space-y-1 text-sm">
              {detail.data.allocations.map((a) => (
                <li key={a.id} className={a.active ? undefined : "text-muted-foreground"}>
                  {a.display_name} · {describeDuty(a)} · {a.roster_name}
                  {a.active ? "" : " · no longer held"}
                </li>
              ))}
            </ul>
          </section>
          <Decision
            hospital={hospital}
            path={path}
            exchange={detail.data.exchange}
            filer={filer}
            approver={approver}
          />
        </div>
      )}
      <section aria-label="Exchange history" className="mt-6">
        <h5 className="mb-2 text-sm font-semibold">History</h5>
        {history.isPending ? (
          <p role="status">Loading history…</p>
        ) : history.isError ? (
          <Failure message="Could not load the exchange history." retry={history.retry} />
        ) : (
          <ul className="space-y-2 text-sm">
            {history.items.map((e) => (
              <li key={e.id} className="rounded-xl border p-3">
                <p className="font-medium">
                  {DUTY_EXCHANGE_STATUSES[e.status]} · version {e.version}
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

function useExchangeAction(
  act: Action,
  hospital: string,
  path: string,
  version: number,
  done: () => void,
) {
  return useApiMutation(
    (note: string) =>
      apiFetch<DutyExchangeDetail>(`${path}/${actions[act].path}`, {
        method: "POST",
        body: JSON.stringify({
          expected_version: version,
          ...(actions[act].path === "decision" && { decision: act }),
          note,
        }),
      }),
    {
      invalidate: [
        ["workforce", "duty-exchanges", hospital],
        ["workforce", "duty-exchange", hospital],
        ["workforce", "duty-exchange-history", hospital],
        ["workforce", "roster-allocations", hospital],
        ["workforce", "rosters", hospital],
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
  exchange,
  filer,
  approver,
}: {
  hospital: string;
  path: string;
  exchange: DutyExchange;
  filer: boolean;
  approver: boolean;
}) {
  const [confirming, setConfirming] = useState(false);
  const form = useZodForm(noteSchema, { note: "" });
  const done = () => {
    setConfirming(false);
    form.reset({ note: "" });
  };
  const mutations: Record<Action, ReturnType<typeof useExchangeAction>> = {
    approve: useExchangeAction("approve", hospital, path, exchange.version, done),
    reject: useExchangeAction("reject", hospital, path, exchange.version, done),
    withdraw: useExchangeAction("withdraw", hospital, path, exchange.version, done),
    revoke: useExchangeAction("revoke", hospital, path, exchange.version, done),
  };
  const busy = Object.values(mutations).some((m) => m.isPending);
  const failed = Object.values(mutations).find((m) => m.isError);
  const available: Action[] =
    exchange.status === "pending"
      ? [
          ...(filer ? (["withdraw"] as const) : []),
          ...(approver ? (["reject", "approve"] as const) : []),
        ]
      : exchange.status === "approved" && approver
        ? ["revoke"]
        : [];
  if (available.length === 0) return null;
  const labels: Record<Action, string> = {
    approve: "Approve exchange",
    reject: "Reject exchange",
    withdraw: "Withdraw exchange",
    revoke: "Revoke exchange",
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
      aria-label="Decide duty exchange"
      onSubmit={(e) => e.preventDefault()}
      className="space-y-4 rounded-xl border p-4"
    >
      <fieldset disabled={busy} className="space-y-4">
        <Field
          id="exchange-decision-note"
          label="Note (required to reject or revoke)"
          error={errors.note?.message}
        >
          <textarea
            id="exchange-decision-note"
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
        title="Revoke this exchange?"
        description="Each duty returns to the person who held it before the exchange. The exchange cannot be approved again."
        confirmLabel="Revoke exchange"
        busy={mutations.revoke.isPending}
        onConfirm={() => mutations.revoke.mutate(form.getValues("note"))}
      />
    </form>
  );
}
