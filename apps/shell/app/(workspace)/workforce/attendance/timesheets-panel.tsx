"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  attendancePeriodFormSchema,
  attendancePeriodHistoryPath,
  attendancePeriodPath,
  attendancePeriodStepPath,
  attendancePeriodsPath,
  attendancePeriodTransitionFormSchema,
  attendanceTimesheetDaysPath,
  attendanceTimesheetPeoplePath,
  attendanceTimesheetsPath,
  formatMinutes,
  toAttendancePeriod,
  toAttendancePeriodTransition,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  ATTENDANCE_PERIOD_STATUSES,
  ATTENDANCE_STATUSES,
  TIMESHEET_CHANGES,
  type AttendancePeriod,
  type AttendancePeriodEvent,
  type AttendancePeriodForm,
  type AttendancePeriodTransitionForm,
  type AttendanceTimesheet,
  type AttendanceTimesheetDay,
  type AttendanceTimesheetPerson,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { Failure, inputClass } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";

// Finalising locks punches and corrections, so every attendance read refetches.
const periodInvalidations = (hospital: string) => [["workforce", "attendance", hospital]];

const changeOptions = [
  { value: "all", label: "All days" },
  ...Object.entries(TIMESHEET_CHANGES).map(([value, label]) => ({ value, label })),
];

// Explains a refused finalisation with the server's counts, so the planner knows what to settle.
export const periodFailure = (error: Error) => {
  if (!(error instanceof ApiError)) return error.message;
  const d = error.detail;
  if (d.reason === "period_not_ready" && d.statuses && typeof d.statuses === "object") {
    const counts = Object.entries(d.statuses as Record<string, number>)
      .map(
        ([code, n]) =>
          `${ATTENDANCE_STATUSES[code as keyof typeof ATTENDANCE_STATUSES] ?? code} ${n}`,
      )
      .join(", ");
    return `${error.message} ${counts}.`;
  }
  if (d.reason === "pending_corrections" && typeof d.count === "number")
    return `${error.message} ${d.count} pending.`;
  return error.message;
};

const totals = (t: AttendanceTimesheet) =>
  `${t.people} people · ${t.shifts} shifts · ${formatMinutes(t.payable_minutes)} payable · ${t.unpaid_half_days} unpaid half-days`;

const changes = (t: AttendanceTimesheet) =>
  t.number === 1
    ? `${t.added_days} days`
    : `${t.changed_days} changed · ${t.added_days} added · ${t.removed_days} removed`;

export function TimesheetsPanel({
  hospital,
  base,
  finaliser,
}: {
  hospital: string;
  base: string;
  finaliser: boolean;
}) {
  const [creating, setCreating] = useState(false);
  const [open, setOpen] = useState<string | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const periods = useApiPagedQuery<AttendancePeriod>(
    ["workforce", "attendance", hospital, "periods"],
    attendancePeriodsPath(base),
  );
  const close = () => {
    setOpen(null);
    opener.current?.focus();
  };
  return (
    <section
      aria-label="Timesheets"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-end justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Timesheets</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            Finalised periods freeze derived attendance for payroll and lock punches and corrections
            on those days. Recalculating writes a new version and marks every changed day.
          </p>
        </div>
        {finaliser && !creating && (
          <Button onClick={() => setCreating(true)}>Finalise a period</Button>
        )}
      </div>
      {creating && (
        <PeriodForm
          hospital={hospital}
          base={base}
          onClose={() => setCreating(false)}
          onDone={(p) => {
            setCreating(false);
            setOpen(p.id);
          }}
        />
      )}
      {periods.isPending ? (
        <p role="status" className="p-5">
          Loading periods…
        </p>
      ) : periods.isError ? (
        <div className="p-5">
          <Failure message="Could not load periods." retry={periods.retry} />
        </div>
      ) : periods.items.length === 0 ? (
        <p className="p-5 text-sm text-muted-foreground">No period has been finalised yet.</p>
      ) : (
        <ul className="divide-y">
          {periods.items.map((p) => (
            <li key={p.id} className="flex flex-wrap items-start justify-between gap-4 p-5">
              <div>
                <p className="font-medium">
                  {p.from} to {p.to} · {ATTENDANCE_PERIOD_STATUSES[p.status]}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  Version {p.timesheet.number} · {totals(p.timesheet)}
                </p>
              </div>
              <Button
                variant="outline"
                aria-label={`Open period ${p.from} to ${p.to}`}
                onClick={(ev) => {
                  opener.current = ev.currentTarget;
                  setOpen(p.id);
                }}
              >
                Open
              </Button>
            </li>
          ))}
        </ul>
      )}
      {periods.hasMore && (
        <div className="border-t p-5">
          <Button variant="outline" disabled={periods.isFetchingMore} onClick={periods.loadMore}>
            Load more periods
          </Button>
        </div>
      )}
      {open && (
        <PeriodDetail
          key={open}
          hospital={hospital}
          base={base}
          id={open}
          finaliser={finaliser}
          onClose={close}
        />
      )}
    </section>
  );
}

function PeriodForm({
  hospital,
  base,
  onClose,
  onDone,
}: {
  hospital: string;
  base: string;
  onClose: () => void;
  onDone: (p: AttendancePeriod) => void;
}) {
  // One id per form instance, so a retried finalisation replays instead of failing as an overlap.
  const [id] = useState(() => crypto.randomUUID());
  const form = useZodForm(attendancePeriodFormSchema, { from: "", to: "", note: "" });
  const save = useApiMutation(
    (values: AttendancePeriodForm) =>
      apiFetch<AttendancePeriod>(attendancePeriodPath(base, id), {
        method: "PUT",
        body: JSON.stringify(toAttendancePeriod(values)),
      }),
    {
      invalidate: periodInvalidations(hospital),
      successToast: "Period finalised",
      suppressErrorToast: true,
      onSuccess: onDone,
    },
  );
  const section = useRef<HTMLFormElement>(null);
  useEffect(() => section.current?.scrollIntoView?.({ block: "start", behavior: "instant" }), []);
  const errors = form.formState.errors;
  return (
    <form
      ref={section}
      noValidate
      autoComplete="off"
      aria-label="Finalise period"
      onSubmit={form.handleSubmit((values) => save.mutate(values))}
      className="scroll-mt-24 space-y-6 border-b p-5"
    >
      <fieldset disabled={save.isPending} className="space-y-6">
        <div className="grid gap-5 sm:grid-cols-2">
          <Field id="period-from" label="First day" error={errors.from?.message}>
            <input
              id="period-from"
              type="date"
              className={inputClass}
              aria-invalid={!!errors.from}
              {...form.register("from")}
            />
          </Field>
          <Field id="period-to" label="Last day" error={errors.to?.message}>
            <input
              id="period-to"
              type="date"
              className={inputClass}
              aria-invalid={!!errors.to}
              {...form.register("to")}
            />
          </Field>
        </div>
        <p className="text-sm text-muted-foreground">
          Up to 31 days that have already ended. Every shift must be settled and every correction
          decided first.
        </p>
        <Field id="period-note" label="Note (optional)" error={errors.note?.message}>
          <textarea
            id="period-note"
            rows={2}
            className={inputClass}
            aria-invalid={!!errors.note}
            {...form.register("note")}
          />
        </Field>
      </fieldset>
      {save.isError && <SaveError message={periodFailure(save.error)} />}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel finalising
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Finalising…" : "Finalise period"}
        </Button>
      </div>
    </form>
  );
}

function PeriodDetail({
  hospital,
  base,
  id,
  finaliser,
  onClose,
}: {
  hospital: string;
  base: string;
  id: string;
  finaliser: boolean;
  onClose: () => void;
}) {
  const period = useApiQuery<AttendancePeriod>(
    ["workforce", "attendance", hospital, "period", id],
    attendancePeriodPath(base, id),
  );
  const versions = useApiPagedQuery<AttendanceTimesheet>(
    ["workforce", "attendance", hospital, "timesheets", id],
    attendanceTimesheetsPath(base, id),
  );
  const history = useApiPagedQuery<AttendancePeriodEvent>(
    ["workforce", "attendance", hospital, "period-history", id],
    attendancePeriodHistoryPath(base, id),
  );
  const [chosen, setChosen] = useState<string | null>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const timesheet =
    versions.items.find((t) => t.id === chosen) ??
    (period.data ? period.data.timesheet : undefined);
  return (
    <div role="region" aria-label="Attendance period" className="border-t p-5">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          {period.data
            ? `${period.data.from} to ${period.data.to} · ${ATTENDANCE_PERIOD_STATUSES[period.data.status]}`
            : "Attendance period"}
        </h4>
        <Button variant="ghost" onClick={onClose}>
          Close period
        </Button>
      </div>
      {period.isPending ? (
        <p role="status">Loading period…</p>
      ) : period.isError ? (
        <Failure message="Could not load the period." retry={() => void period.refetch()} />
      ) : (
        <div className="space-y-6">
          {finaliser && (
            <PeriodSteps
              hospital={hospital}
              base={base}
              period={period.data}
              onDone={() => setChosen(null)}
            />
          )}
          <section aria-label="Timesheet versions">
            <h5 className="mb-2 text-sm font-semibold">Versions</h5>
            {versions.isPending ? (
              <p role="status">Loading versions…</p>
            ) : versions.isError ? (
              <Failure message="Could not load the versions." retry={versions.retry} />
            ) : (
              <ul className="space-y-2 text-sm">
                {versions.items.map((t) => (
                  <li
                    key={t.id}
                    className="flex flex-wrap items-start justify-between gap-3 rounded-xl border p-3"
                  >
                    <div>
                      <p className="font-medium">
                        Version {t.number} · {changes(t)}
                      </p>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {totals(t)} · {new Date(t.created_at).toLocaleString()} · by{" "}
                        {t.finalised_by_subject}
                        {t.note ? ` · ${t.note}` : ""}
                      </p>
                    </div>
                    <Button
                      variant={timesheet?.id === t.id ? "default" : "outline"}
                      aria-pressed={timesheet?.id === t.id}
                      aria-label={`Show version ${t.number}`}
                      onClick={() => setChosen(t.id)}
                    >
                      Show
                    </Button>
                  </li>
                ))}
              </ul>
            )}
            {versions.hasMore && (
              <Button
                className="mt-3"
                variant="outline"
                disabled={versions.isFetchingMore}
                onClick={versions.loadMore}
              >
                Load more versions
              </Button>
            )}
          </section>
          {timesheet && (
            <TimesheetDetail
              key={timesheet.id}
              hospital={hospital}
              base={base}
              period={id}
              timesheet={timesheet}
            />
          )}
        </div>
      )}
      <section aria-label="Period history" className="mt-6">
        <h5 className="mb-2 text-sm font-semibold">History</h5>
        {history.isPending ? (
          <p role="status">Loading history…</p>
        ) : history.isError ? (
          <Failure message="Could not load the period history." retry={history.retry} />
        ) : (
          <ul className="space-y-2 text-sm">
            {history.items.map((e) => (
              <li key={e.id} className="rounded-xl border p-3">
                <p className="font-medium">
                  {ATTENDANCE_PERIOD_STATUSES[e.status]} · version {e.version}
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

type Step = "finalisation" | "reopening";
const steps: Record<Step, { toast: string; pending: string; label: string }> = {
  finalisation: { toast: "Period finalised", pending: "Finalising…", label: "Finalise period" },
  reopening: { toast: "Period reopened", pending: "Reopening…", label: "Reopen period" },
};

function useStep(
  step: Step,
  hospital: string,
  base: string,
  period: AttendancePeriod,
  done: () => void,
) {
  return useApiMutation(
    (form: AttendancePeriodTransitionForm) =>
      apiFetch<AttendancePeriod>(attendancePeriodStepPath(base, period.id, step), {
        method: "POST",
        body: JSON.stringify(toAttendancePeriodTransition(period.version, form)),
      }),
    {
      invalidate: periodInvalidations(hospital),
      successToast: steps[step].toast,
      suppressErrorToast: true,
      onSuccess: done,
    },
  );
}

// A finalised period can be recalculated or reopened; a reopened one can only be finalised again.
function PeriodSteps({
  hospital,
  base,
  period,
  onDone,
}: {
  hospital: string;
  base: string;
  period: AttendancePeriod;
  onDone: () => void;
}) {
  const form = useZodForm(attendancePeriodTransitionFormSchema, { note: "" });
  const done = () => {
    form.reset({ note: "" });
    onDone();
  };
  const mutations: Record<Step, ReturnType<typeof useStep>> = {
    finalisation: useStep("finalisation", hospital, base, period, done),
    reopening: useStep("reopening", hospital, base, period, done),
  };
  const available: Step[] =
    period.status === "finalised" ? ["reopening", "finalisation"] : ["finalisation"];
  const busy = Object.values(mutations).some((m) => m.isPending);
  const failed = Object.values(mutations).find((m) => m.isError);
  const label = (step: Step) =>
    step === "finalisation" && period.status === "finalised"
      ? "Recalculate period"
      : steps[step].label;
  const submit = (step: Step) =>
    form.handleSubmit((values) => {
      for (const m of Object.values(mutations)) m.reset();
      mutations[step].mutate(values);
    })();
  const errors = form.formState.errors;
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label="Change period"
      onSubmit={(e) => e.preventDefault()}
      className="space-y-4 rounded-xl border p-4"
    >
      <p className="text-sm text-muted-foreground">
        {period.status === "finalised"
          ? "Reopen to accept late punches and corrections on these days, or recalculate to adopt leave, overtime and roster changes into a new version."
          : "The days are open for punches and corrections. Finalise again to lock them and write the next version."}
      </p>
      <fieldset disabled={busy}>
        <Field id="period-step-note" label="Reason" error={errors.note?.message}>
          <textarea
            id="period-step-note"
            rows={2}
            className={inputClass}
            aria-invalid={!!errors.note}
            {...form.register("note")}
          />
        </Field>
      </fieldset>
      {failed?.error && <SaveError message={periodFailure(failed.error)} />}
      <div className="flex flex-wrap justify-end gap-2">
        {available.map((step) => (
          <Button
            key={step}
            type="button"
            variant={step === "finalisation" ? "default" : "outline"}
            disabled={busy}
            onClick={() => submit(step)}
          >
            {mutations[step].isPending ? steps[step].pending : label(step)}
          </Button>
        ))}
      </div>
    </form>
  );
}

function TimesheetDetail({
  hospital,
  base,
  period,
  timesheet,
}: {
  hospital: string;
  base: string;
  period: string;
  timesheet: AttendanceTimesheet;
}) {
  const [change, setChange] = useState("all");
  const people = useApiPagedQuery<AttendanceTimesheetPerson>(
    ["workforce", "attendance", hospital, "timesheet-people", timesheet.id],
    attendanceTimesheetPeoplePath(base, period, timesheet.id),
  );
  const days = useApiPagedQuery<AttendanceTimesheetDay>(
    ["workforce", "attendance", hospital, "timesheet-days", timesheet.id, change],
    attendanceTimesheetDaysPath(base, period, timesheet.id, { change }),
  );
  const names = new Map(
    people.items.map((p) => [p.staff_id, `${p.display_name} · ${p.employee_code}`]),
  );
  return (
    <div className="space-y-6">
      <section aria-label={`Version ${timesheet.number} people`}>
        <h5 className="mb-2 text-sm font-semibold">Version {timesheet.number} · people</h5>
        {people.isPending ? (
          <p role="status">Loading people…</p>
        ) : people.isError ? (
          <Failure message="Could not load the people." retry={people.retry} />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-left text-xs text-muted-foreground">
                <tr>
                  <th className="p-2">Staff</th>
                  <th className="p-2">Days</th>
                  <th className="p-2">Shifts</th>
                  <th className="p-2">Payable</th>
                  <th className="p-2">Overtime</th>
                  <th className="p-2">Unpaid half-days</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {people.items.map((p) => (
                  <tr key={p.staff_id}>
                    <td className="p-2">
                      {p.display_name} · {p.employee_code}
                    </td>
                    <td className="p-2">{p.days}</td>
                    <td className="p-2">{p.shifts}</td>
                    <td className="p-2">{formatMinutes(p.payable_minutes)}</td>
                    <td className="p-2">{formatMinutes(p.approved_overtime_minutes)}</td>
                    <td className="p-2">{p.unpaid_half_days}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {people.hasMore && (
          <Button
            className="mt-3"
            variant="outline"
            disabled={people.isFetchingMore}
            onClick={people.loadMore}
          >
            Load more people
          </Button>
        )}
      </section>
      <section aria-label={`Version ${timesheet.number} days`}>
        <div className="mb-2 flex flex-wrap items-end justify-between gap-3">
          <h5 className="text-sm font-semibold">Version {timesheet.number} · days</h5>
          <div className="w-48">
            <Field id="timesheet-change" label="Change">
              <SelectControl
                id="timesheet-change"
                value={change}
                onValueChange={setChange}
                options={changeOptions}
              />
            </Field>
          </div>
        </div>
        {days.isPending ? (
          <p role="status">Loading days…</p>
        ) : days.isError ? (
          <Failure message="Could not load the days." retry={days.retry} />
        ) : days.items.length === 0 ? (
          <p className="text-sm text-muted-foreground">No days match.</p>
        ) : (
          <ul className="space-y-2 text-sm">
            {days.items.map((d) => (
              <li key={`${d.staff_id}-${d.date}`} className="rounded-xl border p-3">
                <p className="font-medium">
                  {d.date} ·{" "}
                  {d.day
                    ? `${d.day.display_name} · ${d.day.employee_code}`
                    : (names.get(d.staff_id) ?? d.staff_id)}{" "}
                  · {TIMESHEET_CHANGES[d.change]}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {d.day
                    ? `${d.shifts} shifts · ${formatMinutes(d.payable_minutes)} payable · ${d.day.shifts.map((s) => ATTENDANCE_STATUSES[s.status]).join(", ") || "no shift"}`
                    : "No attendance on this day any more."}
                </p>
              </li>
            ))}
          </ul>
        )}
        {days.hasMore && (
          <Button
            className="mt-3"
            variant="outline"
            disabled={days.isFetchingMore}
            onClick={days.loadMore}
          >
            Load more days
          </Button>
        )}
      </section>
    </div>
  );
}
