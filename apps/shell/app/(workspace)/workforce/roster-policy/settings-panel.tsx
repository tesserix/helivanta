"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  rosterSettingsSchema,
  ROSTER_AUTO_POPULATION,
  WEEKDAYS,
  type Hospital,
  type RosterSettings,
  type RosterSettingsInput,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { Failure, ReviewBox, inputClass, today, weekdayOptions } from "./policy-ui";

export function SettingsPanel({ hospital, writable }: { hospital: Hospital; writable: boolean }) {
  const path = `/workforce/hospitals/${hospital.id}/hr/roster-settings`;
  const settings = useApiQuery<RosterSettings>(["workforce", "roster-settings", hospital.id], path);
  const [mode, setMode] = useState<"view" | "edit" | "history">("view");
  // The hospital list already proved access, so 404 here means no reviewed policy exists yet.
  const missing = settings.error instanceof ApiError && settings.error.status === 404;
  const current = settings.data;
  return (
    <section
      aria-label="Roster settings"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-center justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Roster settings</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            {hospital.name} · {writable ? "Manager access" : "Read-only access"}
          </p>
        </div>
        <div className="flex gap-2">
          {current && (
            <Button variant="outline" onClick={() => setMode("history")}>
              Settings history
            </Button>
          )}
          {writable && (current || missing) && (
            <Button onClick={() => setMode("edit")}>
              {current ? "Edit roster settings" : "Configure roster settings"}
            </Button>
          )}
        </div>
      </div>
      <div className="p-5">
        {settings.isPending ? (
          <p role="status">Loading roster settings…</p>
        ) : missing ? (
          <p className="text-sm text-muted-foreground">
            Roster settings have not been reviewed for this hospital. Rosters cannot be generated
            until a manager sets the timezone, week start and automatic population.
          </p>
        ) : settings.isError || !current ? (
          <Failure message="Could not load roster settings." retry={() => settings.refetch()} />
        ) : (
          <dl className="grid gap-4 text-sm sm:grid-cols-4">
            <Fact label="Timezone" value={current.timezone} />
            <Fact label="Week starts" value={WEEKDAYS[current.week_start] ?? ""} />
            <Fact
              label="Automatic population"
              value={ROSTER_AUTO_POPULATION[current.auto_population]}
            />
            <Fact label="Effective" value={`${current.effective_on} · v${current.version}`} />
          </dl>
        )}
      </div>
      {mode === "edit" && (
        <SettingsEditor
          path={path}
          hospital={hospital}
          record={current}
          onClose={() => setMode("view")}
        />
      )}
      {mode === "history" && (
        <SettingsHistory path={path} hospital={hospital} onClose={() => setMode("view")} />
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
function SettingsEditor({
  path,
  hospital,
  record,
  onClose,
}: {
  path: string;
  hospital: Hospital;
  record?: RosterSettings;
  onClose: () => void;
}) {
  const form = useZodForm(rosterSettingsSchema, {
    expected_version: record?.version ?? 0,
    timezone: record?.timezone ?? "",
    week_start: record?.week_start ?? 0,
    auto_population: record?.auto_population ?? "disabled",
    effective_on: record?.effective_on ?? today(),
    reviewed: false,
  });
  const save = useApiMutation(
    (input: RosterSettingsInput) =>
      apiFetch<RosterSettings>(path, { method: "PUT", body: JSON.stringify(input) }),
    {
      invalidate: [
        ["workforce", "roster-settings", hospital.id],
        ["workforce", "roster-settings-history", hospital.id],
      ],
      successToast: "Roster settings saved",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  useEffect(() => form.setFocus("timezone"), [form]);
  const errors = form.formState.errors;
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label="Edit roster settings"
      onSubmit={form.handleSubmit((input) => save.mutate(input))}
      className="space-y-6 border-t p-5"
    >
      <fieldset disabled={save.isPending} className="space-y-6">
        <div className="grid gap-5 sm:grid-cols-2">
          <Field id="settings-timezone" label="Timezone" error={errors.timezone?.message}>
            <input
              id="settings-timezone"
              className={inputClass}
              placeholder="Asia/Kolkata"
              aria-invalid={!!errors.timezone}
              {...form.register("timezone")}
            />
          </Field>
          <Field
            id="settings-effective"
            label="Effective date"
            error={errors.effective_on?.message}
          >
            <input
              id="settings-effective"
              type="date"
              className={inputClass}
              aria-invalid={!!errors.effective_on}
              {...form.register("effective_on")}
            />
          </Field>
          <Field id="settings-week-start" label="Week starts on">
            <SelectControl
              id="settings-week-start"
              value={String(form.watch("week_start"))}
              onValueChange={(v) => form.setValue("week_start", Number(v), { shouldDirty: true })}
              options={weekdayOptions}
            />
          </Field>
          <Field id="settings-population" label="Automatic population">
            <SelectControl
              id="settings-population"
              value={form.watch("auto_population")}
              onValueChange={(v) =>
                form.setValue("auto_population", v as RosterSettingsInput["auto_population"], {
                  shouldDirty: true,
                })
              }
              options={Object.entries(ROSTER_AUTO_POPULATION).map(([value, label]) => ({
                value,
                label,
              }))}
            />
          </Field>
        </div>
        <ReviewBox
          id="settings-review"
          error={errors.reviewed?.message}
          register={form.register("reviewed")}
        >
          I reviewed the timezone, roster week and automatic population
        </ReviewBox>
      </fieldset>
      {save.isError && (
        <p
          role="alert"
          className="rounded-xl border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive"
        >
          {save.error.message} Your input is preserved. Cancel and reload to see current settings.
        </p>
      )}
      <div className="flex justify-end gap-2 border-t pt-5">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel changes
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Saving…" : "Save roster settings"}
        </Button>
      </div>
    </form>
  );
}
function SettingsHistory({
  path,
  hospital,
  onClose,
}: {
  path: string;
  hospital: Hospital;
  onClose: () => void;
}) {
  const history = useApiPagedQuery<RosterSettings>(
    ["workforce", "roster-settings-history", hospital.id],
    `${path}/history`,
  );
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  return (
    <div role="region" aria-label="Roster settings history" className="border-t p-5">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          Revision history
        </h4>
        <Button variant="ghost" onClick={onClose}>
          Close history
        </Button>
      </div>
      {history.isPending ? (
        <p role="status">Loading settings history…</p>
      ) : history.isError ? (
        <Failure message="Could not load settings history." retry={history.retry} />
      ) : (
        <ul className="space-y-3">
          {history.items.map((r) => (
            <li key={r.revision_id} className="rounded-xl border p-4 text-sm">
              <p className="font-medium">
                v{r.version} · effective {r.effective_on}
              </p>
              <p className="mt-1 text-muted-foreground">
                {r.timezone} · week starts {WEEKDAYS[r.week_start]} ·{" "}
                {ROSTER_AUTO_POPULATION[r.auto_population]} · reviewed by {r.reviewed_by_subject}
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
