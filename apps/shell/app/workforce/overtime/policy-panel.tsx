"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  formatMinutes,
  overtimeSettingsSchema,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  type Hospital,
  type OvertimeSettings,
  type OvertimeSettingsInput,
} from "@helivanta/api";
import { Field, useZodForm } from "@helivanta/ui";
import { Failure, inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";

export function OvertimePolicyPanel({
  hospital,
  writable,
}: {
  hospital: Hospital;
  writable: boolean;
}) {
  const path = `/workforce/hospitals/${hospital.id}/hr/overtime-settings`;
  const settings = useApiQuery<OvertimeSettings>(
    ["workforce", "overtime-settings", hospital.id],
    path,
  );
  const [mode, setMode] = useState<"view" | "edit" | "history">("view");
  // The hospital list already proved access, so 404 here means no reviewed policy exists yet.
  const missing = settings.error instanceof ApiError && settings.error.status === 404;
  const current = settings.data;
  return (
    <section
      aria-label="Overtime policy"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-center justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Overtime policy</h3>
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
              {current ? "Edit overtime policy" : "Configure overtime policy"}
            </Button>
          )}
        </div>
      </div>
      <div className="p-5">
        {settings.isPending ? (
          <p role="status">Loading overtime policy…</p>
        ) : missing ? (
          <p className="text-sm text-muted-foreground">
            No overtime policy has been reviewed for this hospital. Overtime cannot be recorded
            until a manager sets the limits and filing window.
          </p>
        ) : settings.isError || !current ? (
          <Failure message="Could not load the overtime policy." retry={() => settings.refetch()} />
        ) : (
          <dl className="grid gap-4 text-sm sm:grid-cols-5">
            <Fact label="Daily limit" value={formatMinutes(current.daily_limit_minutes)} />
            <Fact label="Weekly limit" value={formatMinutes(current.weekly_limit_minutes)} />
            <Fact label="Monthly limit" value={formatMinutes(current.monthly_limit_minutes)} />
            <Fact label="Filing window" value={`${current.filing_window_days} days`} />
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

const limitFields = [
  ["daily_limit_minutes", "Daily limit (minutes)"],
  ["weekly_limit_minutes", "Weekly limit (minutes)"],
  ["monthly_limit_minutes", "Monthly limit (minutes)"],
  ["filing_window_days", "Filing window (days back)"],
] as const;

function PolicyEditor({
  path,
  hospital,
  record,
  onClose,
}: {
  path: string;
  hospital: Hospital;
  record?: OvertimeSettings;
  onClose: () => void;
}) {
  const form = useZodForm(overtimeSettingsSchema, {
    expected_version: record?.version ?? 0,
    daily_limit_minutes: record?.daily_limit_minutes ?? 240,
    weekly_limit_minutes: record?.weekly_limit_minutes ?? 720,
    monthly_limit_minutes: record?.monthly_limit_minutes ?? 2400,
    filing_window_days: record?.filing_window_days ?? 7,
    effective_on: record?.effective_on ?? today(),
    reviewed: false,
  });
  const save = useApiMutation(
    (input: OvertimeSettingsInput) =>
      apiFetch<OvertimeSettings>(path, { method: "PUT", body: JSON.stringify(input) }),
    {
      invalidate: [
        ["workforce", "overtime-settings", hospital.id],
        ["workforce", "overtime-settings-history", hospital.id],
      ],
      successToast: "Overtime policy saved",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  useEffect(() => form.setFocus("daily_limit_minutes"), [form]);
  const errors = form.formState.errors;
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label="Edit overtime policy"
      onSubmit={form.handleSubmit((input) => save.mutate(input))}
      className="space-y-6 border-t p-5"
    >
      <fieldset disabled={save.isPending} className="space-y-6">
        <div className="grid gap-5 sm:grid-cols-2">
          {limitFields.map(([name, label]) => (
            <Field key={name} id={`overtime-${name}`} label={label} error={errors[name]?.message}>
              <input
                id={`overtime-${name}`}
                type="number"
                inputMode="numeric"
                className={inputClass}
                aria-invalid={!!errors[name]}
                {...form.register(name, { valueAsNumber: true })}
              />
            </Field>
          ))}
          <Field
            id="overtime-effective"
            label="Effective date"
            error={errors.effective_on?.message}
          >
            <input
              id="overtime-effective"
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
              aria-describedby={errors.reviewed ? "overtime-review-error" : undefined}
              {...form.register("reviewed")}
            />
            I reviewed the overtime limits and filing window
          </label>
          {errors.reviewed && (
            <p id="overtime-review-error" className="mt-2 text-xs text-destructive">
              {errors.reviewed.message}
            </p>
          )}
          <p className="mt-3 text-xs text-muted-foreground">
            Changes create a new revision effective on or after the current one. Overtime on earlier
            dates stays governed by the revision in force on its work date.
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
          {save.isPending ? "Saving…" : "Save overtime policy"}
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
  const history = useApiPagedQuery<OvertimeSettings>(
    ["workforce", "overtime-settings-history", hospital.id],
    `${path}/history`,
  );
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  return (
    <div role="region" aria-label="Overtime policy history" className="border-t p-5">
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
                {formatMinutes(r.daily_limit_minutes)} a day ·{" "}
                {formatMinutes(r.weekly_limit_minutes)} a week ·{" "}
                {formatMinutes(r.monthly_limit_minutes)} a month · {r.filing_window_days} days to
                file · reviewed by {r.reviewed_by_subject}
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
