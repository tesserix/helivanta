"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  attendanceCorrectionDecisionFormSchema,
  attendanceCorrectionPath,
  attendanceCorrectionsPath,
  toAttendanceCorrectionDecision,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  zonedClock,
  ATTENDANCE_CORRECTION_KINDS,
  ATTENDANCE_CORRECTION_STATUSES,
  type AttendanceCorrection,
  type AttendanceCorrectionDecisionForm,
  type AttendanceCorrectionEvent,
} from "@helivanta/api";
import { ConfirmDialog, Field, SelectControl, useZodForm } from "@helivanta/ui";
import { Failure, inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { CorrectionForm, correctionInvalidations } from "./correction-form";

const statusOptions = [
  { value: "all", label: "All statuses" },
  ...Object.entries(ATTENDANCE_CORRECTION_STATUSES).map(([value, label]) => ({ value, label })),
];

const punchText = (direction: string | undefined, at: string | undefined, timezone: string) =>
  direction && at ? `${direction === "in" ? "In" : "Out"} at ${zonedClock(at, timezone)}` : "";

// Before → after, so a reviewer sees exactly which punch changes.
export const describeCorrection = (c: AttendanceCorrection) => {
  const before = punchText(c.target_direction, c.target_punched_at, c.timezone);
  const after = punchText(c.direction, c.punched_at, c.timezone);
  const change =
    c.kind === "add"
      ? `add ${after}`
      : c.kind === "void"
        ? `void ${before}`
        : `${before} → ${after}`;
  return `${c.local_date} · ${ATTENDANCE_CORRECTION_KINDS[c.kind]} · ${change}`;
};

export function CorrectionsBoard({
  hospital,
  base,
  timezone,
  corrector,
  approver,
}: {
  hospital: string;
  base: string;
  timezone?: string;
  corrector: boolean;
  approver: boolean;
}) {
  const [status, setStatus] = useState("pending");
  const [filing, setFiling] = useState(false);
  const [open, setOpen] = useState<AttendanceCorrection | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const corrections = useApiPagedQuery<AttendanceCorrection>(
    ["workforce", "attendance", hospital, "corrections", status],
    attendanceCorrectionsPath(base, { status }),
  );
  const close = () => {
    setOpen(null);
    opener.current?.focus();
  };
  return (
    <section
      aria-label="Attendance corrections"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-end justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Corrections</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            Missing, wrong and mistimed punches, corrected only once a second person approves.
            Newest filings first.
          </p>
        </div>
        <div className="flex flex-wrap items-end gap-3">
          <div className="w-48">
            <Field id="correction-status" label="Status">
              <SelectControl
                id="correction-status"
                value={status}
                onValueChange={(v) => {
                  setStatus(v);
                  setOpen(null);
                }}
                options={statusOptions}
              />
            </Field>
          </div>
          {corrector && timezone && !filing && (
            <Button onClick={() => setFiling(true)}>Add missing punch</Button>
          )}
        </div>
      </div>
      {filing && timezone && (
        <CorrectionForm
          hospital={hospital}
          base={base}
          timezone={timezone}
          date={today()}
          onClose={() => setFiling(false)}
        />
      )}
      {corrections.isPending ? (
        <p role="status" className="p-5">
          Loading corrections…
        </p>
      ) : corrections.isError ? (
        <div className="p-5">
          <Failure message="Could not load corrections." retry={corrections.retry} />
        </div>
      ) : corrections.items.length === 0 ? (
        <p className="p-5 text-sm text-muted-foreground">
          {status === "all"
            ? "No corrections have been requested in this hospital."
            : `No ${ATTENDANCE_CORRECTION_STATUSES[status as keyof typeof ATTENDANCE_CORRECTION_STATUSES].toLowerCase()} corrections.`}
        </p>
      ) : (
        <ul className="divide-y">
          {corrections.items.map((c) => (
            <li key={c.id} className="flex flex-wrap items-start justify-between gap-4 p-5">
              <div>
                <p className="font-medium">
                  {c.display_name} · {c.employee_code}
                </p>
                <p className="mt-1 text-sm">{describeCorrection(c)}</p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {ATTENDANCE_CORRECTION_STATUSES[c.status]} · filed by {c.requested_by_subject}
                  {c.decided_by_subject ? ` · decided by ${c.decided_by_subject}` : ""}
                </p>
              </div>
              <Button
                variant="outline"
                aria-label={`Review correction for ${c.display_name} on ${c.local_date}`}
                onClick={(ev) => {
                  opener.current = ev.currentTarget;
                  setOpen(c);
                }}
              >
                Review
              </Button>
            </li>
          ))}
        </ul>
      )}
      {corrections.hasMore && (
        <div className="border-t p-5">
          <Button
            variant="outline"
            disabled={corrections.isFetchingMore}
            onClick={corrections.loadMore}
          >
            Load more corrections
          </Button>
        </div>
      )}
      {open && (
        <CorrectionReview
          key={open.id}
          hospital={hospital}
          base={base}
          correction={open}
          corrector={corrector}
          approver={approver}
          onClose={close}
        />
      )}
    </section>
  );
}

function CorrectionReview({
  hospital,
  base,
  correction,
  corrector,
  approver,
  onClose,
}: {
  hospital: string;
  base: string;
  correction: AttendanceCorrection;
  corrector: boolean;
  approver: boolean;
  onClose: () => void;
}) {
  const path = attendanceCorrectionPath(base, correction.id);
  const detail = useApiQuery<AttendanceCorrection>(
    ["workforce", "attendance", hospital, "correction", correction.id],
    path,
  );
  const history = useApiPagedQuery<AttendanceCorrectionEvent>(
    ["workforce", "attendance", hospital, "correction-history", correction.id],
    `${path}/history`,
  );
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const current = detail.data ?? correction;
  return (
    <div role="region" aria-label="Correction request" className="border-t p-5">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          {current.display_name} · {ATTENDANCE_CORRECTION_STATUSES[current.status]}
        </h4>
        <Button variant="ghost" onClick={onClose}>
          Close correction
        </Button>
      </div>
      {detail.isPending ? (
        <p role="status">Loading correction…</p>
      ) : detail.isError ? (
        <Failure message="Could not load the correction." retry={() => void detail.refetch()} />
      ) : (
        <div className="space-y-6">
          <div className="grid gap-4 text-sm sm:grid-cols-2">
            <p>
              {describeCorrection(detail.data)} · {detail.data.timezone}
            </p>
            <p>Version {detail.data.version}</p>
            {detail.data.reason && <p className="sm:col-span-2">Reason: {detail.data.reason}</p>}
          </div>
          <Decision
            hospital={hospital}
            path={path}
            correction={detail.data}
            corrector={corrector}
            approver={approver}
          />
        </div>
      )}
      <section aria-label="Correction history" className="mt-6">
        <h5 className="mb-2 text-sm font-semibold">History</h5>
        {history.isPending ? (
          <p role="status">Loading history…</p>
        ) : history.isError ? (
          <Failure message="Could not load the correction history." retry={history.retry} />
        ) : (
          <ul className="space-y-2 text-sm">
            {history.items.map((e) => (
              <li key={e.id} className="rounded-xl border p-3">
                <p className="font-medium">
                  {ATTENDANCE_CORRECTION_STATUSES[e.status]} · version {e.version}
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

type Action = "approve" | "reject" | "cancel";
const actions: Record<Action, { toast: string; pending: string; label: string }> = {
  approve: { toast: "Correction approved", pending: "Approving…", label: "Approve correction" },
  reject: { toast: "Correction rejected", pending: "Rejecting…", label: "Reject correction" },
  cancel: { toast: "Correction cancelled", pending: "Cancelling…", label: "Cancel correction" },
};

function useCorrectionAction(
  act: Action,
  hospital: string,
  path: string,
  version: number,
  done: () => void,
) {
  return useApiMutation(
    (form: AttendanceCorrectionDecisionForm) =>
      act === "cancel"
        ? apiFetch<AttendanceCorrection>(`${path}/cancellation`, {
            method: "POST",
            body: JSON.stringify({ expected_version: version, note: form.note }),
          })
        : apiFetch<AttendanceCorrection>(`${path}/decision`, {
            method: "POST",
            body: JSON.stringify(toAttendanceCorrectionDecision(version, act, form)),
          }),
    {
      invalidate: correctionInvalidations(hospital),
      successToast: actions[act].toast,
      suppressErrorToast: true,
      onSuccess: done,
    },
  );
}

function Decision({
  hospital,
  path,
  correction,
  corrector,
  approver,
}: {
  hospital: string;
  path: string;
  correction: AttendanceCorrection;
  corrector: boolean;
  approver: boolean;
}) {
  const [confirming, setConfirming] = useState(false);
  const form = useZodForm(attendanceCorrectionDecisionFormSchema, { note: "" });
  const done = () => {
    setConfirming(false);
    form.reset({ note: "" });
  };
  const mutations: Record<Action, ReturnType<typeof useCorrectionAction>> = {
    approve: useCorrectionAction("approve", hospital, path, correction.version, done),
    reject: useCorrectionAction("reject", hospital, path, correction.version, done),
    cancel: useCorrectionAction("cancel", hospital, path, correction.version, done),
  };
  if (correction.status !== "pending") return null;
  const available: Action[] = [
    ...(corrector ? (["cancel"] as const) : []),
    ...(approver ? (["reject", "approve"] as const) : []),
  ];
  if (available.length === 0) return null;
  const busy = Object.values(mutations).some((m) => m.isPending);
  const failed = Object.values(mutations).find((m) => m.isError);
  // Approving a void or retime voids a punch for good, so it is confirmed first.
  const confirmsApproval = correction.kind !== "add";
  const submit = (act: Action) =>
    form.handleSubmit((values) => {
      for (const m of Object.values(mutations)) m.reset();
      if (act === "reject" && !values.note)
        return form.setError("note", { message: "Add a note explaining the rejection." });
      if (act === "approve" && confirmsApproval) return setConfirming(true);
      mutations[act].mutate(values);
    })();
  const errors = form.formState.errors;
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label="Decide correction"
      onSubmit={(e) => e.preventDefault()}
      className="space-y-4 rounded-xl border p-4"
    >
      <fieldset disabled={busy}>
        <Field
          id="correction-decision-note"
          label="Note (required to reject)"
          error={errors.note?.message}
        >
          <textarea
            id="correction-decision-note"
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
            variant={act === "approve" ? "default" : act === "cancel" ? "ghost" : "outline"}
            disabled={busy}
            onClick={() => submit(act)}
          >
            {mutations[act].isPending ? actions[act].pending : actions[act].label}
          </Button>
        ))}
      </div>
      <ConfirmDialog
        open={confirming && !mutations.approve.isError}
        onOpenChange={(open) => !open && setConfirming(false)}
        title="Approve this correction?"
        description="The original punch is voided for good and stops counting toward attendance. It stays on the ledger with a link to this correction."
        confirmLabel="Approve correction"
        busy={mutations.approve.isPending}
        onConfirm={() => mutations.approve.mutate({ note: form.getValues("note").trim() })}
      />
    </form>
  );
}
