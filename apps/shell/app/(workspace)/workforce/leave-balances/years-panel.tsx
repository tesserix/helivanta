"use client";
import { useState } from "react";
import { Button } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  leaveMonthName,
  leaveSettingsSchema,
  leaveYearOpenSchema,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  type Hospital,
  type LeaveRun,
  type LeaveSettings,
  type LeaveSettingsInput,
  type LeaveYear,
  type LeaveYearOpenInput,
  type UseApiPagedQueryResult,
} from "@helivanta/api";
import { ConfirmDialog, Field, SelectControl, useZodForm } from "@helivanta/ui";
import { Failure, inputClass, today } from "../roster-policy/policy-ui";

const monthOptions = Array.from({ length: 12 }, (_, i) => ({
  value: String(i + 1),
  label: leaveMonthName(i + 1),
}));

export function LeaveSettingsPanel({
  hospital,
  writable,
}: {
  hospital: Hospital;
  writable: boolean;
}) {
  const path = `/workforce/hospitals/${hospital.id}/hr/leave-settings`;
  const settings = useApiQuery<LeaveSettings>(["workforce", "leave-settings", hospital.id], path);
  // The hospital list already proved access, so 404 here means the leave year was never set.
  const missing = settings.error instanceof ApiError && settings.error.status === 404;
  return (
    <section aria-label="Leave year" className="rounded-2xl border bg-card p-5 shadow-sm">
      <h3 className="font-semibold">Leave year</h3>
      {settings.isPending ? (
        <p role="status" className="mt-3">
          Loading leave year…
        </p>
      ) : settings.data ? (
        <p className="mt-2 text-sm text-muted-foreground">
          Leave years start on 1 {leaveMonthName(settings.data.year_start_month)}. The start month
          is fixed so posted balances keep their meaning.
        </p>
      ) : missing && writable ? (
        <SettingsForm hospital={hospital.id} path={path} />
      ) : missing ? (
        <p className="mt-2 text-sm text-muted-foreground">
          A hospital manager has not set the leave year yet.
        </p>
      ) : (
        <div className="mt-3">
          <Failure message="Could not load the leave year." retry={() => void settings.refetch()} />
        </div>
      )}
    </section>
  );
}

function SettingsForm({ hospital, path }: { hospital: string; path: string }) {
  const form = useZodForm(leaveSettingsSchema, { year_start_month: 1, reviewed: false });
  const save = useApiMutation(
    (input: LeaveSettingsInput) =>
      apiFetch<LeaveSettings>(path, { method: "PUT", body: JSON.stringify(input) }),
    {
      invalidate: [["workforce", "leave-settings", hospital]],
      successToast: "Leave year saved",
      suppressErrorToast: true,
    },
  );
  const errors = form.formState.errors;
  return (
    <form
      noValidate
      aria-label="Set leave year"
      onSubmit={form.handleSubmit((input) => save.mutate(input))}
      className="mt-4 space-y-5"
    >
      <fieldset disabled={save.isPending} className="space-y-5">
        <div className="max-w-xs">
          <Field id="leave-year-start" label="Leave year starts in">
            <SelectControl
              id="leave-year-start"
              value={String(form.watch("year_start_month"))}
              onValueChange={(v) =>
                form.setValue("year_start_month", Number(v), { shouldDirty: true })
              }
              options={monthOptions}
            />
          </Field>
        </div>
        <LeaveReview
          id="leave-settings-review"
          error={errors.reviewed?.message}
          register={form.register("reviewed")}
        >
          I reviewed the start month and understand it cannot change once saved
        </LeaveReview>
      </fieldset>
      {save.isError && <SaveError message={save.error.message} />}
      <div className="flex justify-end">
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Saving…" : "Save leave year"}
        </Button>
      </div>
    </form>
  );
}

export function LeaveYearsPanel({
  hospital,
  years,
  writable,
}: {
  hospital: Hospital;
  years: UseApiPagedQueryResult<LeaveYear>;
  writable: boolean;
}) {
  const base = `/workforce/hospitals/${hospital.id}/hr/leave-years`;
  const [runs, setRuns] = useState<LeaveYear | null>(null);
  const [closing, setClosing] = useState<LeaveYear | null>(null);
  const invalidate = [
    ["workforce", "leave-years", hospital.id],
    ["workforce", "leave-runs", hospital.id],
    ["workforce", "leave-balances", hospital.id],
    ["workforce", "leave-ledger", hospital.id],
  ];
  const post = useApiMutation(
    (y: LeaveYear) =>
      apiFetch<LeaveRun>(`${base}/${y.leave_year}/postings`, {
        method: "POST",
        body: JSON.stringify({ month: y.posted_months + 1 }),
      }),
    { invalidate, successToast: "Month posted" },
  );
  const close = useApiMutation(
    (y: LeaveYear) => apiFetch<LeaveRun>(`${base}/${y.leave_year}/close`, { method: "POST" }),
    { invalidate, successToast: "Leave year closed", onSuccess: () => setClosing(null) },
  );
  const busy = post.isPending || close.isPending;
  return (
    <section
      aria-label="Leave years"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="border-b p-5">
        <h3 className="font-semibold">Leave years</h3>
        <p className="mt-1 text-xs text-muted-foreground">
          Post each month in order once it starts. Close a year after it ends: balances carry into
          the next open year up to each limit and the rest expire.
        </p>
      </div>
      {writable && <OpenYearForm hospital={hospital.id} base={base} />}
      {years.isPending ? (
        <p role="status" className="p-5">
          Loading leave years…
        </p>
      ) : years.isError ? (
        <div className="p-5">
          <Failure message="Could not load leave years." retry={years.retry} />
        </div>
      ) : years.items.length === 0 ? (
        <p className="p-5 text-sm text-muted-foreground">No leave year has been opened.</p>
      ) : (
        <ul className="divide-y">
          {years.items.map((y) => (
            <li key={y.id} className="flex flex-wrap items-center justify-between gap-4 p-5">
              <div>
                <p className="font-medium">Leave year {y.leave_year}</p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {y.start_on} to {y.end_on} · {y.posted_months} of 12 months posted ·{" "}
                  {y.closed ? "Closed" : "Open"}
                </p>
              </div>
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  aria-label={`Runs for leave year ${y.leave_year}`}
                  onClick={() => setRuns(y)}
                >
                  Runs
                </Button>
                {writable &&
                  !y.closed &&
                  y.posted_months < 12 &&
                  monthStart(y, y.posted_months + 1) <= today() && (
                    <Button variant="outline" disabled={busy} onClick={() => post.mutate(y)}>
                      Post {leaveMonthName(monthOf(y, y.posted_months + 1))}
                    </Button>
                  )}
                {writable && !y.closed && y.posted_months === 12 && y.end_on < today() && (
                  <Button variant="outline" disabled={busy} onClick={() => setClosing(y)}>
                    Close leave year {y.leave_year}
                  </Button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      {years.hasMore && (
        <div className="border-t p-5">
          <Button variant="outline" disabled={years.isFetchingMore} onClick={years.loadMore}>
            Load more leave years
          </Button>
        </div>
      )}
      {runs && (
        <RunHistory
          key={runs.id}
          base={base}
          hospital={hospital.id}
          year={runs}
          onClose={() => setRuns(null)}
        />
      )}
      <ConfirmDialog
        open={!!closing}
        onOpenChange={(open) => !open && setClosing(null)}
        title={`Close leave year ${closing?.leave_year ?? ""}?`}
        description="Balances up to each leave type's carry-forward limit move into the next leave year and the remainder expires. Closed years cannot be reopened or reposted."
        confirmLabel="Close leave year"
        busy={close.isPending}
        onConfirm={() => closing && close.mutate(closing)}
      />
    </section>
  );
}

// Posting step k is the k-th month counted from the year's start date.
const monthOf = (y: LeaveYear, step: number) =>
  ((Number(y.start_on.slice(5, 7)) + step - 2) % 12) + 1;
// The server holds the final say; this only hides months that plainly have not started.
const monthStart = (y: LeaveYear, step: number) => {
  const d = new Date(`${y.start_on}T00:00:00Z`);
  d.setUTCMonth(d.getUTCMonth() + step - 1);
  return d.toISOString().slice(0, 10);
};

function OpenYearForm({ hospital, base }: { hospital: string; base: string }) {
  const form = useZodForm(leaveYearOpenSchema, { leave_year: new Date().getFullYear() });
  const open = useApiMutation(
    (input: LeaveYearOpenInput) =>
      apiFetch<LeaveYear>(`${base}/${input.leave_year}`, { method: "PUT" }),
    {
      invalidate: [["workforce", "leave-years", hospital]],
      successToast: "Leave year opened",
      suppressErrorToast: true,
      onSuccess: () => form.reset(),
    },
  );
  const error = form.formState.errors.leave_year?.message;
  return (
    <form
      noValidate
      aria-label="Open leave year"
      onSubmit={form.handleSubmit((input) => open.mutate(input))}
      className="flex flex-wrap items-end gap-3 border-b p-5"
    >
      <div className="w-40">
        <Field id="leave-year-open" label="Leave year" error={error}>
          <input
            id="leave-year-open"
            type="number"
            className={inputClass}
            aria-invalid={!!error}
            {...form.register("leave_year", { valueAsNumber: true })}
          />
        </Field>
      </div>
      <Button type="submit" disabled={open.isPending}>
        {open.isPending ? "Opening…" : "Open leave year"}
      </Button>
      {open.isError && <SaveError message={open.error.message} />}
    </form>
  );
}

function RunHistory({
  base,
  hospital,
  year,
  onClose,
}: {
  base: string;
  hospital: string;
  year: LeaveYear;
  onClose: () => void;
}) {
  const runs = useApiPagedQuery<LeaveRun>(
    ["workforce", "leave-runs", hospital, year.leave_year],
    `${base}/${year.leave_year}/runs`,
  );
  return (
    <div role="region" aria-label="Posting runs" className="border-t p-5">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h4 className="font-semibold">Leave year {year.leave_year} · posting runs</h4>
        <Button variant="ghost" onClick={onClose}>
          Close runs
        </Button>
      </div>
      {runs.isPending ? (
        <p role="status">Loading runs…</p>
      ) : runs.isError ? (
        <Failure message="Could not load posting runs." retry={runs.retry} />
      ) : runs.items.length === 0 ? (
        <p className="text-sm text-muted-foreground">Nothing has been posted for this year.</p>
      ) : (
        <ul className="space-y-2 text-sm">
          {runs.items.map((r) => (
            <li key={r.id} className="rounded-xl border p-3">
              {r.step === 13 ? "Year-end close" : leaveMonthName(monthOf(year, r.step))} ·{" "}
              {r.entries} ledger entries · by {r.actor_subject} ·{" "}
              {new Date(r.created_at).toLocaleString()}
            </li>
          ))}
        </ul>
      )}
      {runs.hasMore && (
        <Button
          className="mt-4"
          variant="outline"
          disabled={runs.isFetchingMore}
          onClick={runs.loadMore}
        >
          Load more runs
        </Button>
      )}
    </div>
  );
}

export function LeaveReview({
  id,
  error,
  children,
  register,
}: {
  id: string;
  error?: string;
  children: React.ReactNode;
  register: object;
}) {
  return (
    <div className="rounded-xl border bg-muted/30 p-4">
      <label className="flex items-start gap-3 text-sm">
        <input
          type="checkbox"
          className="mt-0.5 size-4 shrink-0 accent-(--hms-accent)"
          aria-describedby={error ? `${id}-error` : undefined}
          {...register}
        />
        {children}
      </label>
      {error && (
        <p id={`${id}-error`} className="mt-2 text-xs text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}

export function SaveError({ message }: { message: string }) {
  return (
    <p
      role="alert"
      className="w-full rounded-xl border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive"
    >
      {message} Your input is preserved.
    </p>
  );
}
