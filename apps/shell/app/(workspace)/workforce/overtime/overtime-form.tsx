"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  formatMinutes,
  overtimeMinutes,
  overtimeRequestFormSchema,
  toOvertimeInput,
  useApiMutation,
  useApiPagedQuery,
  type OvertimeRequest,
  type OvertimeRequestInput,
  type Staff,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";

export function OvertimeForm({
  hospital,
  base,
  onClose,
}: {
  hospital: string;
  base: string;
  onClose: () => void;
}) {
  // One id per form instance, so a retried filing replays instead of recording overtime twice.
  const [id] = useState(() => crypto.randomUUID());
  const form = useZodForm(overtimeRequestFormSchema, {
    staff_id: "",
    work_date: today(),
    start_time: "",
    end_time: "",
    reason: "",
  });
  const staff = useApiPagedQuery<Staff>(["workforce", "staff", hospital], `${base}/staff`);
  const save = useApiMutation(
    (input: OvertimeRequestInput) =>
      apiFetch<OvertimeRequest>(`${base}/hr/overtime/${id}`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    {
      invalidate: [
        ["workforce", "overtime", hospital],
        ["workforce", "overtime-summary", hospital],
      ],
      successToast: "Overtime recorded",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  const section = useRef<HTMLFormElement>(null);
  useEffect(() => {
    section.current?.scrollIntoView?.({ block: "start", behavior: "instant" });
  }, []);
  const [person, start, end] = form.watch(["staff_id", "start_time", "end_time"]);
  const span = overtimeMinutes(start, end);
  const errors = form.formState.errors;
  return (
    <form
      ref={section}
      noValidate
      autoComplete="off"
      aria-label="Record overtime"
      onSubmit={form.handleSubmit((values) => save.mutate(toOvertimeInput(values)))}
      className="scroll-mt-24 space-y-6 border-b p-5"
    >
      <fieldset disabled={save.isPending} className="space-y-6">
        <div className="grid gap-5 sm:grid-cols-2">
          <Field id="overtime-staff" label="Staff member" error={errors.staff_id?.message}>
            <SelectControl
              id="overtime-staff"
              value={person}
              placeholder={staff.isPending ? "Loading staff…" : "Choose a staff member"}
              onValueChange={(v) =>
                form.setValue("staff_id", v, { shouldDirty: true, shouldValidate: true })
              }
              options={staff.items
                .filter((s) => s.employment_status === "active")
                .map((s) => ({ value: s.id, label: `${s.display_name} · ${s.employee_code}` }))}
            />
          </Field>
          <Field id="overtime-date" label="Work date" error={errors.work_date?.message}>
            <input
              id="overtime-date"
              type="date"
              className={inputClass}
              aria-invalid={!!errors.work_date}
              {...form.register("work_date")}
            />
          </Field>
          <Field id="overtime-start" label="Start (24-hour)" error={errors.start_time?.message}>
            <input
              id="overtime-start"
              placeholder="18:00"
              className={inputClass}
              aria-invalid={!!errors.start_time}
              {...form.register("start_time")}
            />
          </Field>
          <Field id="overtime-end" label="End (24-hour)" error={errors.end_time?.message}>
            <input
              id="overtime-end"
              placeholder="20:30"
              className={inputClass}
              aria-invalid={!!errors.end_time}
              {...form.register("end_time")}
            />
          </Field>
        </div>
        {span > 0 && (
          <p role="status" className="text-sm text-muted-foreground">
            {formatMinutes(span)}
            {end <= start ? ", ending the next day" : ""} in the hospital&apos;s timezone.
          </p>
        )}
        {staff.isError && (
          <SaveError message="Could not load staff. Close the form and open it again to retry." />
        )}
        <Field id="overtime-reason" label="Reason" error={errors.reason?.message}>
          <textarea
            id="overtime-reason"
            rows={3}
            className={inputClass}
            aria-invalid={!!errors.reason}
            {...form.register("reason")}
          />
        </Field>
        <p className="text-xs text-muted-foreground">
          The reason is shown only to people who record or approve overtime. Leave out diagnoses and
          other clinical detail.
        </p>
      </fieldset>
      {save.isError && <SaveError message={save.error.message} />}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel recording
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Recording…" : "Record overtime"}
        </Button>
      </div>
    </form>
  );
}
