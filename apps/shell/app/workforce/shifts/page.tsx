"use client";

import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  useApiMutation,
  useApiPagedQuery,
  usePermissions,
  shiftSchema,
  shiftDuration,
  SHIFT_TYPES,
  type Hospital,
  type ShiftDefinition,
  type ShiftInput,
  type ShiftRevision,
} from "@helivanta/api";
import { HmsShell, Field, SelectControl, useZodForm } from "@helivanta/ui";
import { TenantPicker } from "@/components/tenant-picker";

const inputClass =
  "w-full rounded-lg border bg-background px-3 py-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-(--hms-accent)";
const timing = (shift: Pick<ShiftDefinition, "start_time" | "end_time" | "duration_minutes">) =>
  `${shift.start_time} – ${shift.end_time} · ${shift.duration_minutes} min${shift.end_time < shift.start_time ? " · next day" : ""}`;

export default function Shifts() {
  const hospitals = useApiPagedQuery<Hospital>(
    ["workforce", "shift-hospitals"],
    "/workforce/hospitals",
  );
  const [selected, setSelected] = useState("");
  const hospital = hospitals.items.find((h) => h.id === selected) ?? hospitals.items[0];
  return (
    <HmsShell active="/workforce/shifts" tenantPicker={<TenantPicker />}>
      <div className="mb-8 flex flex-wrap items-end justify-between gap-5">
        <div>
          <p className="mb-2 text-xs font-semibold uppercase tracking-[0.18em] text-(--hms-accent)">
            People & operations
          </p>
          <h2 className="text-3xl font-semibold tracking-tight">Shift definitions</h2>
          <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
            Clear timings for your team. Every change preserves earlier shift rules.
          </p>
        </div>
        {hospital && (
          <div className="w-full sm:w-72">
            <Field id="shift-hospital" label="Hospital">
              <SelectControl
                id="shift-hospital"
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
        <div role="alert">
          Could not load hospitals.{" "}
          <Button variant="outline" onClick={hospitals.retry}>
            Try again
          </Button>
        </div>
      ) : hospital ? (
        <ShiftCatalog key={hospital.id} hospital={hospital} />
      ) : (
        <p className="rounded-2xl border bg-card p-8">
          Ask your tenant administrator for hospital access before managing shifts.
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
    </HmsShell>
  );
}

function ShiftCatalog({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  const writable = can("workforce.roster.manage") && hospital.access_level === "manager";
  const path = `/workforce/hospitals/${hospital.id}/hr/shifts`;
  const records = useApiPagedQuery<ShiftDefinition>(["workforce", "shifts", hospital.id], path);
  const [filter, setFilter] = useState("");
  const [editing, setEditing] = useState<ShiftDefinition | "new" | null>(null);
  const [history, setHistory] = useState<ShiftDefinition | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const close = () => {
    setEditing(null);
    setHistory(null);
    opener.current?.focus();
  };
  const rows = records.items.filter((s) =>
    `${s.name} ${s.code}`.toLowerCase().includes(filter.toLowerCase()),
  );
  return (
    <>
      <section className="overflow-hidden rounded-2xl border bg-card shadow-sm">
        <div className="flex flex-wrap items-center justify-between gap-4 border-b p-5">
          <div>
            <h3 className="font-semibold">{hospital.name}</h3>
            <p className="mt-1 text-xs text-muted-foreground">
              {writable ? "Manage shift configuration" : "Read-only shift configuration"} ·
              Hospital-specific rules
            </p>
          </div>
          {writable && (
            <Button
              onClick={(e) => {
                opener.current = e.currentTarget;
                setHistory(null);
                setEditing("new");
              }}
            >
              Add shift
            </Button>
          )}
        </div>
        <div className="border-b p-5">
          <Field id="shift-search" label="Filter loaded shifts">
            <input
              id="shift-search"
              className={inputClass}
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Search name or code"
            />
          </Field>
        </div>
        {records.isPending ? (
          <p className="p-8" role="status">
            Loading shifts…
          </p>
        ) : records.isError ? (
          <div className="p-8" role="alert">
            Could not load shifts.{" "}
            <Button variant="outline" onClick={records.retry}>
              Try again
            </Button>
          </div>
        ) : !rows.length ? (
          <div className="p-12 text-center">
            <h4 className="font-semibold">
              {filter ? "No matching loaded shifts" : "No shifts yet"}
            </h4>
            <p className="mt-2 text-sm text-muted-foreground">
              {filter
                ? "Try another name or code."
                : "Define your hospital’s working hours and review the rules before using them in a roster."}
            </p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <caption className="sr-only">Shifts at {hospital.name}</caption>
              <thead className="border-b bg-muted/40 text-xs text-muted-foreground">
                <tr>
                  {["Shift", "Timing", "Rules", "Status", "Revision", "Actions"].map((h) => (
                    <th key={h} scope="col" className="px-5 py-3 font-medium">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((s) => (
                  <tr key={s.id} className="border-b last:border-0">
                    <td className="px-5 py-4">
                      <p className="font-medium">{s.name}</p>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {s.code} · {SHIFT_TYPES[s.shift_type]}
                      </p>
                    </td>
                    <td className="px-5 py-4 whitespace-nowrap">{timing(s)}</td>
                    <td className="px-5 py-4 text-xs text-muted-foreground">
                      Break {s.break_minutes} min · Grace {s.grace_minutes} min
                      {s.locum ? " · Locum" : ""}
                    </td>
                    <td className="px-5 py-4">
                      <span className="rounded-full bg-muted px-2.5 py-1 text-xs">
                        {s.active ? "Active" : "Retired"}
                      </span>
                    </td>
                    <td className="px-5 py-4">{s.version}</td>
                    <td className="px-5 py-4 whitespace-nowrap">
                      {writable && (
                        <Button
                          variant="ghost"
                          size="sm"
                          aria-label={`Edit ${s.name}`}
                          onClick={(e) => {
                            opener.current = e.currentTarget;
                            setHistory(null);
                            setEditing(s);
                          }}
                        >
                          Edit
                        </Button>
                      )}
                      <Button
                        variant="ghost"
                        size="sm"
                        aria-label={`History for ${s.name}`}
                        onClick={(e) => {
                          opener.current = e.currentTarget;
                          setEditing(null);
                          setHistory(s);
                        }}
                      >
                        History
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <div className="flex flex-wrap items-center justify-between gap-4 border-t p-5 text-xs text-muted-foreground">
          <span>{records.items.length} loaded shifts · All durations in minutes</span>
          <div className="flex gap-3">
            {records.hasMore && (
              <Button
                variant="outline"
                size="sm"
                disabled={records.isFetchingMore}
                onClick={records.loadMore}
              >
                Load more shifts
              </Button>
            )}
            <Button
              variant="outline"
              size="sm"
              onClick={records.retry}
              disabled={records.isFetchingMore}
            >
              Refresh shifts
            </Button>
          </div>
        </div>
      </section>
      {editing && (
        <ShiftEditor
          key={editing === "new" ? "new" : editing.id}
          path={path}
          hospital={hospital}
          record={editing === "new" ? undefined : editing}
          onClose={close}
        />
      )}
      {history && (
        <ShiftHistory
          key={history.id}
          path={`${path}/${history.id}/history`}
          shift={history}
          onClose={close}
        />
      )}
    </>
  );
}

function ShiftEditor({
  path,
  hospital,
  record,
  onClose,
}: {
  path: string;
  hospital: Hospital;
  record?: ShiftDefinition;
  onClose: () => void;
}) {
  const [id] = useState(() => record?.id ?? crypto.randomUUID());
  const form = useZodForm(shiftSchema, {
    expected_version: record?.version ?? 0,
    code: record?.code ?? "",
    name: record?.name ?? "",
    description: record?.description ?? "",
    shift_type: record?.shift_type ?? "day",
    active: record?.active ?? true,
    start_time: record?.start_time ?? "08:00",
    end_time: record?.end_time ?? "16:00",
    break_minutes: record?.break_minutes ?? 0,
    grace_minutes: record?.grace_minutes ?? 0,
    full_day_ot_minutes: record?.full_day_ot_minutes ?? 0,
    half_day_ot_minutes: record?.half_day_ot_minutes ?? 0,
    locum: record?.locum ?? false,
  });
  const section = useRef<HTMLElement>(null);
  const save = useApiMutation(
    (input: ShiftInput) =>
      apiFetch<ShiftDefinition>(`${path}/${id}`, { method: "PUT", body: JSON.stringify(input) }),
    {
      invalidate: [["workforce", "shifts", hospital.id]],
      successToast: "Shift saved",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  useEffect(() => {
    section.current?.scrollIntoView?.({ block: "start", behavior: "instant" });
    form.setFocus("name");
  }, [form]);
  const start = form.watch("start_time");
  const end = form.watch("end_time");
  const duration = shiftDuration(start, end);
  return (
    <section
      ref={section}
      className="mt-6 scroll-mt-24 rounded-2xl border bg-card p-6 shadow-sm"
      aria-label={record ? "Edit shift" : "Add shift"}
    >
      <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div>
          <h3 className="text-xl font-semibold">{record ? "Edit shift" : "Add shift"}</h3>
          <p className="mt-1 text-sm text-muted-foreground">{hospital.name}</p>
        </div>
        <Button variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel changes
        </Button>
      </div>
      <form
        noValidate
        autoComplete="off"
        onSubmit={form.handleSubmit((input) => save.mutate(input))}
        className="space-y-5"
      >
        <fieldset disabled={save.isPending} className="space-y-5">
          <div className="grid gap-5 sm:grid-cols-2">
            {(
              [
                ["name", "Name"],
                ["code", "Code"],
                ["start_time", "Start time"],
                ["end_time", "End time"],
              ] as const
            ).map(([name, label]) => (
              <Field
                key={name}
                id={`shift-${name}`}
                label={label}
                error={form.formState.errors[name]?.message}
              >
                <input
                  id={`shift-${name}`}
                  className={inputClass}
                  type={name.endsWith("time") ? "time" : "text"}
                  disabled={name === "code" && !!record}
                  aria-invalid={!!form.formState.errors[name]}
                  aria-describedby={form.formState.errors[name] ? `shift-${name}-error` : undefined}
                  {...form.register(name)}
                />
              </Field>
            ))}
          </div>
          <p className="text-sm text-muted-foreground" role="status">
            {Number.isFinite(duration) && duration > 0
              ? `${duration} minutes${end < start ? " · Ends the next day" : ""}`
              : "Choose distinct start and end times."}{" "}
            · Local wall-clock timings; dated rosters must apply their hospital timezone.
          </p>
          <Field
            id="shift-type"
            label="Shift type"
            error={form.formState.errors.shift_type?.message}
          >
            <SelectControl
              id="shift-type"
              value={form.watch("shift_type")}
              onValueChange={(v) =>
                form.setValue("shift_type", v as ShiftInput["shift_type"], { shouldDirty: true })
              }
              options={Object.entries(SHIFT_TYPES).map(([value, label]) => ({ value, label }))}
            />
          </Field>
          <div className="grid gap-5 sm:grid-cols-2">
            {(
              [
                ["break_minutes", "Break minutes"],
                ["grace_minutes", "Grace minutes"],
                ["full_day_ot_minutes", "Full-day OT minutes"],
                ["half_day_ot_minutes", "Half-day OT minutes"],
              ] as const
            ).map(([name, label]) => (
              <Field
                key={name}
                id={`shift-${name}`}
                label={label}
                error={form.formState.errors[name]?.message}
              >
                <input
                  id={`shift-${name}`}
                  className={inputClass}
                  type="number"
                  min={0}
                  step={1}
                  aria-invalid={!!form.formState.errors[name]}
                  aria-describedby={form.formState.errors[name] ? `shift-${name}-error` : undefined}
                  {...form.register(name, { valueAsNumber: true })}
                />
              </Field>
            ))}
          </div>
          <p className="rounded-lg bg-muted/50 p-4 text-xs text-muted-foreground">
            Break, grace and overtime values are configuration only. Review them against your
            hospital policy. This screen does not calculate pay or approve attendance.
          </p>
          <Field
            id="shift-description"
            label="Description"
            error={form.formState.errors.description?.message}
          >
            <textarea
              id="shift-description"
              className={inputClass}
              rows={3}
              {...form.register("description")}
            />
          </Field>
          {(
            [
              ["locum", "Available for locum coverage"],
              ["active", "Available for new rosters"],
            ] as const
          ).map(([name, label]) => (
            <label key={name} className="flex items-center gap-3 text-sm">
              <input
                type="checkbox"
                className="size-4 accent-(--hms-accent)"
                {...form.register(name)}
              />
              {label}
            </label>
          ))}
          <p className="text-xs text-muted-foreground">
            Codes stay fixed. Retiring a shift preserves its history. Earlier revisions retain their
            original timings.
          </p>
        </fieldset>
        {save.isError && (
          <p
            role="alert"
            className="rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive"
          >
            {save.error.message} Your input is preserved. Cancel and refresh shifts to load current
            values.
          </p>
        )}
        <div className="flex justify-end border-t pt-5">
          <Button type="submit" disabled={save.isPending}>
            {save.isPending ? "Saving…" : "Save shift"}
          </Button>
        </div>
      </form>
    </section>
  );
}
function ShiftHistory({
  path,
  shift,
  onClose,
}: {
  path: string;
  shift: ShiftDefinition;
  onClose: () => void;
}) {
  const history = useApiPagedQuery<ShiftRevision>(["workforce", "shift-history", shift.id], path);
  const section = useRef<HTMLElement>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    section.current?.scrollIntoView?.({ block: "start", behavior: "instant" });
    heading.current?.focus();
  }, []);
  return (
    <section
      ref={section}
      aria-label="Shift revision history"
      className="mt-6 scroll-mt-24 rounded-2xl border bg-card p-6 shadow-sm"
    >
      <div className="mb-6 flex items-start justify-between gap-4">
        <div>
          <h3 ref={heading} tabIndex={-1} className="text-xl font-semibold outline-none">
            Revision history
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">
            {shift.name} · {shift.code}
          </p>
        </div>
        <Button variant="ghost" onClick={onClose}>
          Close history
        </Button>
      </div>
      {history.isPending ? (
        <p role="status">Loading shift history…</p>
      ) : history.isError ? (
        <div role="alert">
          Could not load shift history.{" "}
          <Button variant="outline" onClick={history.retry}>
            Try again
          </Button>
        </div>
      ) : (
        <ul className="space-y-3">
          {history.items.map((r) => (
            <li key={r.id} className="rounded-xl border p-4">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <p className="font-medium">{r.name}</p>
                <span className="text-xs text-muted-foreground">
                  Revision {r.version} · {r.active ? "Active" : "Retired"}
                </span>
              </div>
              <p className="mt-2 text-sm">{timing(r)}</p>
              <p className="mt-1 text-xs text-muted-foreground">
                {SHIFT_TYPES[r.shift_type]} · Break {r.break_minutes} min · Grace {r.grace_minutes}{" "}
                min · Full-day OT {r.full_day_ot_minutes} min · Half-day OT {r.half_day_ot_minutes}{" "}
                min{r.locum ? " · Locum" : ""}
              </p>
              <p className="mt-2 text-sm text-muted-foreground">{r.description}</p>
              <p className="mt-2 text-xs text-muted-foreground">
                {new Date(r.created_at).toLocaleString()}
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
          Load earlier revisions
        </Button>
      )}
    </section>
  );
}
