"use client";
import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { Button } from "@tesserix/web";
import {
  ApiError,
  ROSTER_STATUS,
  WEEKDAYS,
  addDays,
  apiFetch,
  rosterSchema,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  weekdayOf,
  type Hospital,
  type Roster,
  type RosterInput,
  type RosterSettings,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { Failure, inputClass } from "../roster-policy/policy-ui";
import { ReviewCheck, SaveError } from "./roster-ui";
import { RosterDetail } from "./roster-detail";

export function RostersWorkspace() {
  const hospitals = useApiPagedQuery<Hospital>(
    ["workforce", "planning-hospitals"],
    "/workforce/hospitals",
  );
  const [selected, setSelected] = useState("");
  const hospital = hospitals.items.find((h) => h.id === selected) ?? hospitals.items[0];
  const { can } = usePermissions();
  const manager = hospital?.access_level === "manager";
  const writable = manager && can("workforce.roster.manage");
  const correctable = manager && can("workforce.roster.correct");
  return (
    <>
      <div className="mb-8 flex flex-wrap items-end justify-between gap-6">
        <div>
          <p className="mb-2 text-xs font-semibold uppercase tracking-[0.18em] text-(--hms-accent)">
            People & operations
          </p>
          <h2 className="text-3xl font-semibold tracking-tight">Rosters</h2>
          <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
            Plan dated shifts by area or person, populate from weekly templates, and freeze the
            roster once it is published.
          </p>
        </div>
        {hospital && (
          <div className="w-full sm:w-72">
            <Field id="rosters-hospital" label="Hospital">
              <SelectControl
                id="rosters-hospital"
                value={hospital.id}
                onValueChange={setSelected}
                options={hospitals.items.map((h) => ({ value: h.id, label: h.name }))}
              />
            </Field>
          </div>
        )}
      </div>
      {hospitals.isPending ? (
        <p role="status">Loading hospitals…</p>
      ) : hospitals.isError ? (
        <Failure message="Could not load hospitals." retry={hospitals.retry} />
      ) : hospital ? (
        <HospitalRosters
          key={hospital.id}
          hospital={hospital}
          writable={writable}
          correctable={correctable}
        />
      ) : (
        <p className="rounded-2xl border bg-card p-8">
          Ask your tenant administrator for hospital access before planning rosters.
        </p>
      )}
      {hospitals.hasMore && (
        <Button
          className="mt-4"
          variant="outline"
          disabled={hospitals.isFetchingMore}
          onClick={hospitals.loadMore}
        >
          Load more hospitals
        </Button>
      )}
    </>
  );
}

function HospitalRosters({
  hospital,
  writable,
  correctable,
}: {
  hospital: Hospital;
  writable: boolean;
  correctable: boolean;
}) {
  const base = `/workforce/hospitals/${hospital.id}/hr`;
  const settings = useApiQuery<RosterSettings>(
    ["workforce", "roster-settings", hospital.id],
    `${base}/roster-settings`,
  );
  const rosters = useApiPagedQuery<Roster>(
    ["workforce", "rosters", hospital.id],
    `${base}/rosters`,
  );
  const [open, setOpen] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const opener = useRef<HTMLButtonElement | null>(null);
  // The hospital list already proved access, so 404 means nobody has reviewed roster settings.
  const unconfigured = settings.error instanceof ApiError && settings.error.status === 404;
  const close = () => {
    setOpen(null);
    setCreating(false);
    opener.current?.focus();
  };
  return (
    <div className="space-y-8">
      <section
        aria-label="Rosters"
        className="overflow-hidden rounded-2xl border bg-card shadow-sm"
      >
        <div className="flex flex-wrap items-center justify-between gap-4 border-b p-5">
          <div>
            <h3 className="font-semibold">Rosters</h3>
            <p className="mt-1 text-xs text-muted-foreground">
              {hospital.name} · {writable ? "Manager access" : "Read-only access"}
              {settings.data &&
                ` · ${settings.data.timezone} · weeks start ${WEEKDAYS[settings.data.week_start]}`}
            </p>
          </div>
          {writable && settings.data && (
            <Button
              onClick={(e) => {
                opener.current = e.currentTarget;
                setOpen(null);
                setCreating(true);
              }}
            >
              New roster
            </Button>
          )}
        </div>
        {unconfigured && (
          <p className="border-b p-5 text-sm text-muted-foreground">
            Roster settings have not been reviewed for this hospital, so rosters cannot be planned.{" "}
            <Link
              className="font-medium text-(--hms-accent) underline"
              href="/workforce/roster-policy"
            >
              Review roster policy
            </Link>
          </p>
        )}
        {rosters.isPending ? (
          <p role="status" className="p-5">
            Loading rosters…
          </p>
        ) : rosters.isError ? (
          <div className="p-5">
            <Failure message="Could not load rosters." retry={rosters.retry} />
          </div>
        ) : rosters.items.length === 0 ? (
          <p className="p-5 text-sm text-muted-foreground">No rosters have been planned yet.</p>
        ) : (
          <ul className="divide-y">
            {rosters.items.map((r) => (
              <li key={r.id} className="flex flex-wrap items-center justify-between gap-4 p-5">
                <div>
                  <p className="font-medium">
                    {r.name} · {ROSTER_STATUS[r.status]}
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {r.start_date} to {r.end_date} · {r.allocations} shifts · v{r.version}
                  </p>
                </div>
                <Button
                  variant="outline"
                  aria-label={`Open ${r.name}`}
                  onClick={(e) => {
                    opener.current = e.currentTarget;
                    setCreating(false);
                    setOpen(r.id);
                  }}
                >
                  Open
                </Button>
              </li>
            ))}
          </ul>
        )}
        {rosters.hasMore && (
          <div className="border-t p-5">
            <Button variant="outline" disabled={rosters.isFetchingMore} onClick={rosters.loadMore}>
              Load more rosters
            </Button>
          </div>
        )}
        {creating && settings.data && (
          <CreateRoster
            base={base}
            hospital={hospital}
            settings={settings.data}
            onClose={close}
            onCreated={(id) => {
              setCreating(false);
              setOpen(id);
            }}
          />
        )}
      </section>
      {open && (
        <RosterDetail
          key={open}
          base={base}
          hospital={hospital}
          rosterID={open}
          writable={writable}
          correctable={correctable}
          onClose={close}
        />
      )}
    </div>
  );
}

const weekOptions = [1, 2, 3, 4, 5, 6].map((n) => ({
  value: String(n),
  label: n === 1 ? "1 week" : `${n} weeks`,
}));

function nextWeekStart(weekStart: number) {
  let date = new Date().toISOString().slice(0, 10);
  while (weekdayOf(date) !== weekStart) date = addDays(date, 1);
  return date;
}

function CreateRoster({
  base,
  hospital,
  settings,
  onClose,
  onCreated,
}: {
  base: string;
  hospital: Hospital;
  settings: RosterSettings;
  onClose: () => void;
  onCreated: (id: string) => void;
}) {
  // One ID per form keeps retries idempotent: the API returns the same draft instead of a duplicate.
  const [id] = useState(() => crypto.randomUUID());
  const [weeks, setWeeks] = useState(2);
  const start = nextWeekStart(settings.week_start);
  const form = useZodForm(rosterSchema, {
    name: "",
    start_date: start,
    end_date: addDays(start, 13),
    reviewed: false,
  });
  const startDate = form.watch("start_date");
  useEffect(() => {
    if (/^\d{4}-\d{2}-\d{2}$/.test(startDate))
      form.setValue("end_date", addDays(startDate, weeks * 7 - 1));
  }, [form, startDate, weeks]);
  const save = useApiMutation(
    (input: RosterInput) =>
      apiFetch<Roster>(`${base}/rosters/${id}`, { method: "PUT", body: JSON.stringify(input) }),
    {
      invalidate: [["workforce", "rosters", hospital.id]],
      successToast: "Roster created",
      suppressErrorToast: true,
      onSuccess: () => onCreated(id),
    },
  );
  useEffect(() => form.setFocus("name"), [form]);
  const errors = form.formState.errors;
  return (
    <form
      noValidate
      autoComplete="off"
      aria-label="New roster"
      onSubmit={form.handleSubmit((input) => {
        if (weekdayOf(input.start_date) !== settings.week_start) {
          form.setError("start_date", {
            message: `Start on a ${WEEKDAYS[settings.week_start]}, the configured week start.`,
          });
          return;
        }
        save.mutate(input);
      })}
      className="space-y-6 border-t p-5"
    >
      <fieldset disabled={save.isPending} className="space-y-6">
        <div className="grid gap-5 sm:grid-cols-3">
          <Field id="roster-name" label="Name" error={errors.name?.message}>
            <input
              id="roster-name"
              className={inputClass}
              placeholder="Ward plan, weeks 42–43"
              aria-invalid={!!errors.name}
              {...form.register("name")}
            />
          </Field>
          <Field id="roster-start" label="Starts on" error={errors.start_date?.message}>
            <input
              id="roster-start"
              type="date"
              className={inputClass}
              aria-invalid={!!errors.start_date}
              {...form.register("start_date")}
            />
          </Field>
          <Field id="roster-weeks" label="Length" error={errors.end_date?.message}>
            <SelectControl
              id="roster-weeks"
              value={String(weeks)}
              onValueChange={(v) => setWeeks(Number(v))}
              options={weekOptions}
            />
          </Field>
        </div>
        <p className="text-sm text-muted-foreground">
          Ends {form.watch("end_date")} · shift times resolve in {settings.timezone}.
        </p>
        <ReviewCheck
          id="roster-review"
          error={errors.reviewed?.message}
          register={form.register("reviewed")}
        >
          I reviewed the roster name and period
        </ReviewCheck>
      </fieldset>
      {save.isError && <SaveError message={save.error.message} />}
      <div className="flex justify-end gap-2 border-t pt-5">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Creating…" : "Create roster"}
        </Button>
      </div>
    </form>
  );
}
