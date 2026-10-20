"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  attendanceCorrectionFormSchema,
  attendanceCorrectionPath,
  toAttendanceCorrection,
  useApiMutation,
  useApiPagedQuery,
  zonedClock,
  ATTENDANCE_CORRECTION_KINDS,
  type AttendanceCorrection,
  type AttendanceCorrectionInput,
  type AttendancePunch,
  type Staff,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { inputClass } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";

// Filing changes nothing until approval, but the board and ledger both show pending requests.
export const correctionInvalidations = (hospital: string) => [
  ["workforce", "attendance", hospital],
];

type Kind = keyof typeof ATTENDANCE_CORRECTION_KINDS;

// With a punch the request voids or retimes it; without one it adds a missing punch for anyone on the staff list.
export function CorrectionForm({
  hospital,
  base,
  timezone,
  date,
  punch,
  onClose,
}: {
  hospital: string;
  base: string;
  timezone: string;
  date: string;
  punch?: AttendancePunch;
  onClose: () => void;
}) {
  // One id per form instance, so a retried filing replays instead of filing twice.
  const [id] = useState(() => crypto.randomUUID());
  const form = useZodForm(attendanceCorrectionFormSchema, {
    staff_id: punch?.staff_id ?? "",
    kind: punch ? "replace" : "add",
    target_punch_id: punch?.id ?? "",
    direction: punch?.direction ?? "in",
    date: punch?.local_date ?? date,
    time: punch ? zonedClock(punch.punched_at, punch.timezone) : "",
    reason: "",
  });
  // Corrections fix past facts, so people who have since left can still be chosen.
  const staff = useApiPagedQuery<Staff>(["workforce", "staff", hospital], `${base}/staff`, {
    enabled: !punch,
  });
  const save = useApiMutation(
    (input: AttendanceCorrectionInput) =>
      apiFetch<AttendanceCorrection>(attendanceCorrectionPath(base, id), {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    {
      invalidate: correctionInvalidations(hospital),
      successToast: "Correction requested",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  const section = useRef<HTMLFormElement>(null);
  useEffect(() => section.current?.scrollIntoView?.({ block: "start", behavior: "instant" }), []);
  const [person, kind, direction] = form.watch(["staff_id", "kind", "direction"]);
  const errors = form.formState.errors;
  const set = (name: "staff_id" | "kind" | "direction", value: string) =>
    form.setValue(name, value as never, { shouldDirty: true, shouldValidate: true });
  return (
    <form
      ref={section}
      noValidate
      autoComplete="off"
      aria-label="Request correction"
      onSubmit={form.handleSubmit((values) => {
        const body = toAttendanceCorrection(values, timezone);
        if (body) save.mutate(body);
        else
          form.setError("time", {
            message: `This time does not exist in ${timezone} because the clocks changed.`,
          });
      })}
      className="scroll-mt-24 space-y-6 border-t p-5"
    >
      {punch && (
        <p className="text-sm">
          Correct the {punch.direction} punch for {punch.display_name} on {punch.local_date} at{" "}
          {zonedClock(punch.punched_at, punch.timezone)}. The punch stays on the ledger; an approved
          correction voids it and, for a retime, records the corrected punch.
        </p>
      )}
      <fieldset disabled={save.isPending} className="space-y-6">
        <div className="grid gap-5 sm:grid-cols-2">
          {punch ? (
            <Field id="correction-kind" label="Correction" error={errors.kind?.message}>
              <SelectControl
                id="correction-kind"
                value={kind}
                onValueChange={(v) => set("kind", v)}
                options={(["replace", "void"] as Kind[]).map((k) => ({
                  value: k,
                  label: ATTENDANCE_CORRECTION_KINDS[k],
                }))}
              />
            </Field>
          ) : (
            <Field id="correction-staff" label="Staff member" error={errors.staff_id?.message}>
              <SelectControl
                id="correction-staff"
                value={person}
                placeholder={staff.isPending ? "Loading staff…" : "Choose a staff member"}
                onValueChange={(v) => set("staff_id", v)}
                options={staff.items.map((s) => ({
                  value: s.id,
                  label: `${s.display_name} · ${s.employee_code}`,
                }))}
              />
            </Field>
          )}
          {kind !== "void" && (
            <>
              <Field id="correction-direction" label="Direction" error={errors.direction?.message}>
                <SelectControl
                  id="correction-direction"
                  value={direction}
                  onValueChange={(v) => set("direction", v)}
                  options={[
                    { value: "in", label: "In" },
                    { value: "out", label: "Out" },
                  ]}
                />
              </Field>
              <Field id="correction-date" label="Date" error={errors.date?.message}>
                <input
                  id="correction-date"
                  type="date"
                  className={inputClass}
                  aria-invalid={!!errors.date}
                  {...form.register("date")}
                />
              </Field>
              <Field id="correction-time" label="Time (24-hour)" error={errors.time?.message}>
                <input
                  id="correction-time"
                  placeholder="08:05"
                  className={inputClass}
                  aria-invalid={!!errors.time}
                  {...form.register("time")}
                />
              </Field>
            </>
          )}
        </div>
        {kind !== "void" && (
          <p className="text-sm text-muted-foreground">
            Times are read in the hospital&apos;s timezone, {timezone}.
          </p>
        )}
        {!punch && staff.isError && (
          <SaveError message="Could not load staff. Close the form and open it again to retry." />
        )}
        <Field id="correction-reason" label="Reason" error={errors.reason?.message}>
          <textarea
            id="correction-reason"
            rows={3}
            className={inputClass}
            aria-invalid={!!errors.reason}
            {...form.register("reason")}
          />
        </Field>
        <p className="text-xs text-muted-foreground">
          A second person approves the correction before it counts. The reason is shown only to
          people who record, correct or approve punches; leave out clinical detail.
        </p>
      </fieldset>
      {save.isError && <SaveError message={save.error.message} />}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Discard request
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Requesting…" : "Request correction"}
        </Button>
      </div>
    </form>
  );
}
