"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  attendancePunchFormSchema,
  attendancePunchesPath,
  attendanceVoidFormSchema,
  toAttendancePunch,
  toAttendanceVoid,
  useApiMutation,
  useApiPagedQuery,
  zonedClock,
  type AttendancePunch,
  type AttendancePunchInput,
  type AttendanceVoidInput,
  type Staff,
} from "@helivanta/api";
import { ConfirmDialog, Field, SelectControl, useZodForm } from "@helivanta/ui";
import { Failure, inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";
import { CorrectionForm } from "./correction-form";

const MAX_RANGE_DAYS = 42;
const daysBetween = (from: string, to: string) =>
  (Date.parse(`${to}T00:00:00Z`) - Date.parse(`${from}T00:00:00Z`)) / 86400000;
const shiftDate = (date: string, days: number) =>
  new Date(Date.parse(`${date}T00:00:00Z`) + days * 86400000).toISOString().slice(0, 10);
const validDate = (d: string) => /^(2[01]\d\d)-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])$/.test(d);

// Punches change derived days, totals and person ranges, so every attendance read refetches.
const punchInvalidations = (hospital: string) => [["workforce", "attendance", hospital]];

export function useActiveStaff(hospital: string, base: string) {
  const staff = useApiPagedQuery<Staff>(["workforce", "staff", hospital], `${base}/staff`);
  return {
    staff,
    options: staff.items
      .filter((s) => s.employment_status === "active")
      .map((s) => ({ value: s.id, label: `${s.display_name} · ${s.employee_code}` })),
  };
}

export function PunchForm({
  hospital,
  base,
  timezone,
  date,
  onClose,
}: {
  hospital: string;
  base: string;
  timezone: string;
  date: string;
  onClose: () => void;
}) {
  // One id per form instance, so a retried punch replays instead of recording it twice.
  const [id] = useState(() => crypto.randomUUID());
  const form = useZodForm(attendancePunchFormSchema, {
    staff_id: "",
    direction: "in",
    date,
    time: "",
    reason: "",
  });
  const { staff, options } = useActiveStaff(hospital, base);
  const save = useApiMutation(
    (input: AttendancePunchInput) =>
      apiFetch<AttendancePunch>(`${base}/hr/attendance/punches/${id}`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    {
      invalidate: punchInvalidations(hospital),
      successToast: "Punch recorded",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  const section = useRef<HTMLFormElement>(null);
  useEffect(() => {
    section.current?.scrollIntoView?.({ block: "start", behavior: "instant" });
  }, []);
  const [person, direction] = form.watch(["staff_id", "direction"]);
  const errors = form.formState.errors;
  return (
    <form
      ref={section}
      noValidate
      autoComplete="off"
      aria-label="Record punch"
      onSubmit={form.handleSubmit((values) => {
        const body = toAttendancePunch(values, timezone);
        if (body) save.mutate(body);
        else
          form.setError("time", {
            message: `This time does not exist in ${timezone} because the clocks changed.`,
          });
      })}
      className="scroll-mt-24 space-y-6 border-b p-5"
    >
      <fieldset disabled={save.isPending} className="space-y-6">
        <div className="grid gap-5 sm:grid-cols-2">
          <Field id="punch-staff" label="Staff member" error={errors.staff_id?.message}>
            <SelectControl
              id="punch-staff"
              value={person}
              placeholder={staff.isPending ? "Loading staff…" : "Choose a staff member"}
              onValueChange={(v) =>
                form.setValue("staff_id", v, { shouldDirty: true, shouldValidate: true })
              }
              options={options}
            />
          </Field>
          <Field id="punch-direction" label="Direction" error={errors.direction?.message}>
            <SelectControl
              id="punch-direction"
              value={direction}
              onValueChange={(v) =>
                form.setValue("direction", v as "in" | "out", {
                  shouldDirty: true,
                  shouldValidate: true,
                })
              }
              options={[
                { value: "in", label: "In" },
                { value: "out", label: "Out" },
              ]}
            />
          </Field>
          <Field id="punch-date" label="Date" error={errors.date?.message}>
            <input
              id="punch-date"
              type="date"
              className={inputClass}
              aria-invalid={!!errors.date}
              {...form.register("date")}
            />
          </Field>
          <Field id="punch-time" label="Time (24-hour)" error={errors.time?.message}>
            <input
              id="punch-time"
              placeholder="08:05"
              className={inputClass}
              aria-invalid={!!errors.time}
              {...form.register("time")}
            />
          </Field>
        </div>
        <p className="text-sm text-muted-foreground">
          Times are read in the hospital&apos;s timezone, {timezone}.
        </p>
        {staff.isError && (
          <SaveError message="Could not load staff. Close the form and open it again to retry." />
        )}
        <Field id="punch-reason" label="Reason" error={errors.reason?.message}>
          <textarea
            id="punch-reason"
            rows={3}
            className={inputClass}
            aria-invalid={!!errors.reason}
            {...form.register("reason")}
          />
        </Field>
        <p className="text-xs text-muted-foreground">
          The reason is shown only to people who record punches. Leave out diagnoses and other
          clinical detail.
        </p>
      </fieldset>
      {save.isError && <SaveError message={save.error.message} />}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel recording
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Recording…" : "Record punch"}
        </Button>
      </div>
    </form>
  );
}

export function PunchLedger({
  hospital,
  base,
  recorder,
  corrector,
  timezone,
}: {
  hospital: string;
  base: string;
  recorder: boolean;
  corrector: boolean;
  timezone?: string;
}) {
  const [person, setPerson] = useState("");
  const [to, setTo] = useState(today);
  const [from, setFrom] = useState(() => shiftDate(today(), -6));
  const { staff, options } = useActiveStaff(hospital, base);
  const span = validDate(from) && validDate(to) ? daysBetween(from, to) : -1;
  const rangeError =
    !validDate(from) || !validDate(to)
      ? "Choose dates between 2000 and 2199."
      : span < 0
        ? "The end date must not be before the start date."
        : span >= MAX_RANGE_DAYS
          ? `Choose at most ${MAX_RANGE_DAYS} days.`
          : undefined;
  const ready = person !== "" && rangeError === undefined;
  const punches = useApiPagedQuery<AttendancePunch>(
    ["workforce", "attendance", hospital, "punches", person, from, to],
    attendancePunchesPath(base, person, from, to),
    { enabled: ready },
  );
  const [voiding, setVoiding] = useState<AttendancePunch | null>(null);
  const [correcting, setCorrecting] = useState<AttendancePunch | null>(null);
  return (
    <section
      aria-label="Punch ledger"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-end justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Punch ledger</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            Every punch one person has on record, voided punches included. Punches are never edited;
            a wrong punch is voided and recorded again.
          </p>
        </div>
        <div className="flex flex-wrap items-start gap-3">
          <div className="w-64">
            <Field id="ledger-staff" label="Staff member">
              <SelectControl
                id="ledger-staff"
                value={person}
                placeholder={staff.isPending ? "Loading staff…" : "Choose a staff member"}
                onValueChange={(v) => {
                  setPerson(v);
                  setVoiding(null);
                  setCorrecting(null);
                }}
                options={options}
              />
            </Field>
          </div>
          <div className="w-40">
            <Field id="ledger-from" label="From" error={rangeError}>
              <input
                id="ledger-from"
                type="date"
                className={inputClass}
                aria-invalid={rangeError !== undefined}
                value={from}
                onChange={(e) => setFrom(e.target.value)}
              />
            </Field>
          </div>
          <div className="w-40">
            <Field id="ledger-to" label="To">
              <input
                id="ledger-to"
                type="date"
                className={inputClass}
                aria-invalid={rangeError !== undefined}
                value={to}
                onChange={(e) => setTo(e.target.value)}
              />
            </Field>
          </div>
        </div>
      </div>
      {staff.isError ? (
        <div className="p-5">
          <Failure message="Could not load staff." retry={staff.retry} />
        </div>
      ) : !ready ? (
        person === "" && (
          <p className="p-5 text-sm text-muted-foreground">
            Choose a staff member to see their punches.
          </p>
        )
      ) : punches.isPending ? (
        <p role="status" className="p-5">
          Loading punches…
        </p>
      ) : punches.isError ? (
        <div className="p-5">
          <Failure message="Could not load punches." retry={punches.retry} />
        </div>
      ) : punches.items.length === 0 ? (
        <p className="p-5 text-sm text-muted-foreground">
          No punches between {from} and {to}.
        </p>
      ) : (
        <ul className="divide-y">
          {punches.items.map((p) => (
            <li key={p.id} className="flex flex-wrap items-start justify-between gap-4 p-5">
              <div className={p.voided ? "text-muted-foreground" : undefined}>
                <p className="font-medium">
                  {p.local_date} · {zonedClock(p.punched_at, p.timezone)} ·{" "}
                  {p.direction === "in" ? "In" : "Out"}
                  {p.voided ? " · Voided" : ""}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  Recorded by {p.recorded_by_subject}
                  {p.reason ? ` · ${p.reason}` : ""}
                  {p.voided
                    ? ` · voided by ${p.voided_by_subject ?? "unknown"}${p.void_reason ? `: ${p.void_reason}` : ""}`
                    : ""}
                </p>
              </div>
              {!p.voided && (recorder || corrector) && (
                <div className="flex flex-wrap gap-2">
                  {corrector && timezone && (
                    <Button
                      variant="outline"
                      aria-label={`Request correction to ${p.direction} punch on ${p.local_date} at ${zonedClock(p.punched_at, p.timezone)}`}
                      onClick={() => {
                        setVoiding(null);
                        setCorrecting(p);
                      }}
                    >
                      Request correction
                    </Button>
                  )}
                  {recorder && (
                    <Button
                      variant="outline"
                      aria-label={`Void ${p.direction} punch on ${p.local_date} at ${zonedClock(p.punched_at, p.timezone)}`}
                      onClick={() => {
                        setCorrecting(null);
                        setVoiding(p);
                      }}
                    >
                      Void punch
                    </Button>
                  )}
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
      {ready && punches.hasMore && (
        <div className="border-t p-5">
          <Button variant="outline" disabled={punches.isFetchingMore} onClick={punches.loadMore}>
            Load more punches
          </Button>
        </div>
      )}
      {voiding && (
        <VoidForm
          key={voiding.id}
          hospital={hospital}
          base={base}
          punch={voiding}
          onClose={() => setVoiding(null)}
        />
      )}
      {correcting && timezone && (
        <CorrectionForm
          key={correcting.id}
          hospital={hospital}
          base={base}
          timezone={timezone}
          date={correcting.local_date}
          punch={correcting}
          onClose={() => setCorrecting(null)}
        />
      )}
    </section>
  );
}

function VoidForm({
  hospital,
  base,
  punch,
  onClose,
}: {
  hospital: string;
  base: string;
  punch: AttendancePunch;
  onClose: () => void;
}) {
  const form = useZodForm(attendanceVoidFormSchema, { reason: "" });
  const [pending, setPending] = useState<AttendanceVoidInput | null>(null);
  const voidPunch = useApiMutation(
    (input: AttendanceVoidInput) =>
      apiFetch<AttendancePunch>(`${base}/hr/attendance/punches/${punch.id}/void`, {
        method: "POST",
        body: JSON.stringify(input),
      }),
    {
      invalidate: punchInvalidations(hospital),
      successToast: "Punch voided",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  useEffect(() => form.setFocus("reason"), [form]);
  const errors = form.formState.errors;
  const when = `${punch.local_date} at ${zonedClock(punch.punched_at, punch.timezone)}`;
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label="Void punch"
      onSubmit={form.handleSubmit((values) => {
        voidPunch.reset();
        setPending(toAttendanceVoid(values));
      })}
      className="space-y-4 border-t p-5"
    >
      <p className="text-sm">
        Void the {punch.direction} punch for {punch.display_name} on {when}.
      </p>
      <fieldset disabled={voidPunch.isPending}>
        <Field id="void-reason" label="Reason" error={errors.reason?.message}>
          <textarea
            id="void-reason"
            rows={2}
            className={inputClass}
            aria-invalid={!!errors.reason}
            {...form.register("reason")}
          />
        </Field>
      </fieldset>
      {voidPunch.isError && <SaveError message={voidPunch.error.message} />}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" disabled={voidPunch.isPending} onClick={onClose}>
          Keep punch
        </Button>
        <Button type="submit" disabled={voidPunch.isPending}>
          {voidPunch.isPending ? "Voiding…" : "Void punch"}
        </Button>
      </div>
      <ConfirmDialog
        open={pending !== null && !voidPunch.isError}
        onOpenChange={(open) => !open && setPending(null)}
        title="Void this punch?"
        description="The punch stops counting toward attendance. It stays on the ledger and cannot be restored; record a new punch if needed."
        confirmLabel="Void punch"
        busy={voidPunch.isPending}
        onConfirm={() => pending && voidPunch.mutate(pending)}
      />
    </form>
  );
}
