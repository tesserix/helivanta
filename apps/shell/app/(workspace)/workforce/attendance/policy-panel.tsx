"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  attendanceSettingsSchema,
  formatMinutes,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  type AttendanceSettings,
  type AttendanceSettingsInput,
  type Hospital,
} from "@helivanta/api";
import { Field, useZodForm } from "@helivanta/ui";
import { Failure, inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";

export function AttendancePolicyPanel({
  hospital,
  writable,
}: {
  hospital: Hospital;
  writable: boolean;
}) {
  const path = `/workforce/hospitals/${hospital.id}/hr/attendance-settings`;
  const settings = useApiQuery<AttendanceSettings>(
    ["workforce", "attendance-settings", hospital.id],
    path,
  );
  const [mode, setMode] = useState<"view" | "edit" | "history">("view");
  // The hospital list already proved access, so 404 here means no reviewed policy exists yet.
  const missing = settings.error instanceof ApiError && settings.error.status === 404;
  const current = settings.data;
  return (
    <section
      aria-label="Attendance policy"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-center justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Attendance policy</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            {hospital.name} · {writable ? "Manager access" : "Read-only access"}
          </p>
        </div>
        <div className="flex gap-2">
          {current && (
            <Button variant="outline" onClick={() => setMode("history")}>
              Policy history
            </Button>
          )}
          {writable && (current || missing) && (
            <Button onClick={() => setMode("edit")}>
              {current ? "Edit attendance policy" : "Configure attendance policy"}
            </Button>
          )}
        </div>
      </div>
      <div className="p-5">
        {settings.isPending ? (
          <p role="status">Loading attendance policy…</p>
        ) : missing ? (
          <p className="text-sm text-muted-foreground">
            No attendance policy has been reviewed for this hospital. Punches cannot be recorded and
            shifts show no attendance until a manager sets the matching windows.
          </p>
        ) : settings.isError || !current ? (
          <Failure
            message="Could not load the attendance policy."
            retry={() => settings.refetch()}
          />
        ) : (
          <dl className="grid gap-4 text-sm sm:grid-cols-4">
            <Fact label="Early arrival" value={formatMinutes(current.early_arrival_minutes)} />
            <Fact label="Late departure" value={formatMinutes(current.late_departure_minutes)} />
            <Fact
              label="Early departure grace"
              value={formatMinutes(current.early_departure_grace_minutes)}
            />
            <Fact label="Short duty tolerance" value={formatMinutes(current.short_duty_minutes)} />
            <Fact
              label="Duplicate window"
              value={formatMinutes(current.duplicate_window_minutes)}
            />
            <Fact label="Back-dating window" value={`${current.backdate_days} days`} />
            <Fact label="Effective" value={`${current.effective_on} · v${current.version}`} />
          </dl>
        )}
      </div>
      {mode === "edit" && (
        <PolicyEditor
          path={path}
          hospital={hospital}
          record={current}
          onClose={() => setMode("view")}
        />
      )}
      {mode === "history" && (
        <PolicyHistory path={path} hospital={hospital} onClose={() => setMode("view")} />
      )}
    </section>
  );
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-1 font-medium">{value}</dd>
    </div>
  );
}

const allowanceFields = [
  ["early_arrival_minutes", "Early arrival allowance (minutes)"],
  ["late_departure_minutes", "Late departure allowance (minutes)"],
  ["early_departure_grace_minutes", "Early departure grace (minutes)"],
  ["short_duty_minutes", "Short duty tolerance (minutes)"],
  ["duplicate_window_minutes", "Duplicate punch window (minutes)"],
  ["backdate_days", "Back-dating window (days)"],
] as const;

function PolicyEditor({
  path,
  hospital,
  record,
  onClose,
}: {
  path: string;
  hospital: Hospital;
  record?: AttendanceSettings;
  onClose: () => void;
}) {
  const form = useZodForm(attendanceSettingsSchema, {
    expected_version: record?.version ?? 0,
    early_arrival_minutes: record?.early_arrival_minutes ?? 60,
    late_departure_minutes: record?.late_departure_minutes ?? 120,
    early_departure_grace_minutes: record?.early_departure_grace_minutes ?? 0,
    short_duty_minutes: record?.short_duty_minutes ?? 30,
    duplicate_window_minutes: record?.duplicate_window_minutes ?? 5,
    backdate_days: record?.backdate_days ?? 7,
    effective_on: record?.effective_on ?? today(),
    reviewed: false,
  });
  const save = useApiMutation(
    (input: AttendanceSettingsInput) =>
      apiFetch<AttendanceSettings>(path, { method: "PUT", body: JSON.stringify(input) }),
    {
      invalidate: [
        ["workforce", "attendance-settings", hospital.id],
        ["workforce", "attendance-settings-history", hospital.id],
        ["workforce", "attendance", hospital.id],
      ],
      successToast: "Attendance policy saved",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  useEffect(() => form.setFocus("early_arrival_minutes"), [form]);
  const errors = form.formState.errors;
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label="Edit attendance policy"
      onSubmit={form.handleSubmit((input) => save.mutate(input))}
      className="space-y-6 border-t p-5"
    >
      <fieldset disabled={save.isPending} className="space-y-6">
        <div className="grid gap-5 sm:grid-cols-2">
          {allowanceFields.map(([name, label]) => (
            <Field key={name} id={`attendance-${name}`} label={label} error={errors[name]?.message}>
              <input
                id={`attendance-${name}`}
                type="number"
                inputMode="numeric"
                className={inputClass}
                aria-invalid={!!errors[name]}
                {...form.register(name, { valueAsNumber: true })}
              />
            </Field>
          ))}
          <Field
            id="attendance-effective"
            label="Effective date"
            error={errors.effective_on?.message}
          >
            <input
              id="attendance-effective"
              type="date"
              className={inputClass}
              aria-invalid={!!errors.effective_on}
              {...form.register("effective_on")}
            />
          </Field>
        </div>
        <div className="rounded-xl border bg-muted/30 p-4">
          <label className="flex items-start gap-3 text-sm">
            <input
              type="checkbox"
              className="mt-0.5 size-4 shrink-0 accent-(--hms-accent)"
              aria-describedby={errors.reviewed ? "attendance-review-error" : undefined}
              {...form.register("reviewed")}
            />
            I reviewed the matching windows, tolerances and back-dating window
          </label>
          {errors.reviewed && (
            <p id="attendance-review-error" className="mt-2 text-xs text-destructive">
              {errors.reviewed.message}
            </p>
          )}
          <p className="mt-3 text-xs text-muted-foreground">
            Changes create a new revision effective on or after the current one. Shifts on earlier
            dates stay derived under the revision in force on their date.
          </p>
        </div>
      </fieldset>
      {save.isError && (
        <SaveError message={`${save.error.message} Cancel and reload to see the current policy.`} />
      )}
      <div className="flex justify-end gap-2 border-t pt-5">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel changes
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Saving…" : "Save attendance policy"}
        </Button>
      </div>
    </form>
  );
}

function PolicyHistory({
  path,
  hospital,
  onClose,
}: {
  path: string;
  hospital: Hospital;
  onClose: () => void;
}) {
  const history = useApiPagedQuery<AttendanceSettings>(
    ["workforce", "attendance-settings-history", hospital.id],
    `${path}/history`,
  );
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  return (
    <div role="region" aria-label="Attendance policy history" className="border-t p-5">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          Revision history
        </h4>
        <Button variant="ghost" onClick={onClose}>
          Close history
        </Button>
      </div>
      {history.isPending ? (
        <p role="status">Loading policy history…</p>
      ) : history.isError ? (
        <Failure message="Could not load policy history." retry={history.retry} />
      ) : (
        <ul className="space-y-3">
          {history.items.map((r) => (
            <li key={r.revision_id} className="rounded-xl border p-4 text-sm">
              <p className="font-medium">
                v{r.version} · effective {r.effective_on}
              </p>
              <p className="mt-1 text-muted-foreground">
                {formatMinutes(r.early_arrival_minutes)} early ·{" "}
                {formatMinutes(r.late_departure_minutes)} late ·{" "}
                {formatMinutes(r.early_departure_grace_minutes)} grace ·{" "}
                {formatMinutes(r.short_duty_minutes)} short duty ·{" "}
                {formatMinutes(r.duplicate_window_minutes)} duplicate window · {r.backdate_days}{" "}
                days back · reviewed by {r.reviewed_by_subject}
              </p>
            </li>
          ))}
        </ul>
      )}
      {history.hasMore && (
        <Button
          className="mt-4"
          variant="outline"
          disabled={history.isFetchingMore}
          onClick={history.loadMore}
        >
          Load more revisions
        </Button>
      )}
    </div>
  );
}
