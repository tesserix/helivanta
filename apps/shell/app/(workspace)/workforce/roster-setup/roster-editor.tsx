"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  useApiMutation,
  useApiPagedQuery,
  rosterConfigSchema,
  type Hospital,
  type RosterConfig,
  type RosterConfigInput,
  type RosterShift,
  type ShiftDefinition,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm, useLeaveGuard, GuardedLink } from "@helivanta/ui";
import { WeeklyFields } from "./weekly-fields";
import { inputClass, configNoun } from "./roster-ui";

export function RosterEditor({
  path,
  hospital,
  kind,
  record,
  onClose,
}: {
  path: string;
  hospital: Hospital;
  kind: RosterConfig["kind"];
  record?: RosterConfig;
  onClose: () => void;
}) {
  const [id] = useState(() => record?.id ?? crypto.randomUUID());
  const form = useZodForm(rosterConfigSchema, {
    expected_version: record?.version ?? 0,
    kind,
    code: record?.code ?? "",
    name: record?.name ?? "",
    description: record?.description ?? "",
    active: record?.active ?? true,
    effective_on: record?.effective_on ?? new Date().toISOString().slice(0, 10),
    reviewed: false,
    shift_revision_ids: record?.shifts.map((s) => s.revision_id) ?? [],
    days:
      kind === "weekly_template"
        ? (record?.days.map((d) => ({
            weekday: d.weekday,
            weekly_off: d.weekly_off,
            allocations: d.allocations.map((a) => ({
              shift_revision_id: a.shift_revision_id,
              area_revision_id: a.area_revision_id,
            })),
          })) ??
          Array.from({ length: 7 }, (_, weekday) => ({
            weekday,
            weekly_off: true,
            allocations: [],
          })))
        : [],
  });
  const shifts = useApiPagedQuery<ShiftDefinition>(
    ["workforce", "shifts", hospital.id],
    `/workforce/hospitals/${hospital.id}/hr/shifts`,
  );
  const areas = useApiPagedQuery<RosterConfig>(
    ["workforce", "roster-configs", hospital.id, "area"],
    `${path}?kind=area`,
    { enabled: kind === "weekly_template" },
  );
  const section = useRef<HTMLElement>(null);
  const save = useApiMutation(
    (input: RosterConfigInput) =>
      apiFetch<RosterConfig>(`${path}/${id}`, { method: "PUT", body: JSON.stringify(input) }),
    {
      invalidate: [
        ["workforce", "roster-configs", hospital.id],
        ["workforce", "roster-areas", hospital.id],
      ],
      successToast: "Roster configuration saved",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  const { confirm } = useLeaveGuard(form.formState.isDirty, "roster configuration", save.isPending);
  useEffect(() => {
    section.current?.scrollIntoView?.({ block: "start", behavior: "instant" });
    form.setFocus("name");
  }, [form]);
  const selected = form.watch("shift_revision_ids");
  const snapshots = new Map<string, RosterShift>();
  for (const s of record?.shifts ?? []) snapshots.set(s.revision_id, s);
  for (const s of shifts.items) snapshots.set(s.revision_id, { ...s, shift_id: s.id });
  return (
    <section
      ref={section}
      className="mt-6 scroll-mt-24 rounded-2xl border bg-card p-6 shadow-sm"
      aria-label={`${record ? "Edit" : "Add"} ${configNoun[kind]}`}
    >
      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div>
          <h3 className="text-xl font-semibold">
            {record ? "Edit" : "Add"} {configNoun[kind]}
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">{hospital.name}</p>
        </div>
        <Button variant="ghost" disabled={save.isPending} onClick={() => confirm(onClose)}>
          Cancel changes
        </Button>
      </div>
      <form
        noValidate
        autoComplete="off"
        onSubmit={form.handleSubmit((input) => save.mutate(input))}
        className="space-y-6"
      >
        <fieldset disabled={save.isPending} className="space-y-6">
          <div className="grid gap-5 sm:grid-cols-2">
            <Field id="config-name" label="Name" error={form.formState.errors.name?.message}>
              <input
                id="config-name"
                className={inputClass}
                aria-invalid={!!form.formState.errors.name}
                aria-describedby={form.formState.errors.name ? "config-name-error" : undefined}
                {...form.register("name")}
              />
            </Field>
            <Field id="config-code" label="Code" error={form.formState.errors.code?.message}>
              <input
                id="config-code"
                className={inputClass}
                disabled={!!record}
                aria-invalid={!!form.formState.errors.code}
                aria-describedby={form.formState.errors.code ? "config-code-error" : undefined}
                {...form.register("code")}
              />
            </Field>
          </div>
          <Field
            id="config-effective"
            label="Effective date"
            error={form.formState.errors.effective_on?.message}
          >
            <input
              id="config-effective"
              type="date"
              className={`${inputClass} sm:max-w-sm`}
              aria-invalid={!!form.formState.errors.effective_on}
              aria-describedby={
                form.formState.errors.effective_on ? "config-effective-error" : undefined
              }
              {...form.register("effective_on")}
            />
          </Field>
          {shifts.isPending ? (
            <p role="status">Loading shift choices…</p>
          ) : shifts.isError ? (
            <div role="alert" className="rounded-lg border p-4 text-sm">
              Shift choices could not be loaded.{" "}
              <Button type="button" variant="outline" onClick={shifts.retry}>
                Try again
              </Button>
            </div>
          ) : kind === "weekly_template" ? (
            areas.isPending ? (
              <p role="status">Loading roster areas…</p>
            ) : areas.isError ? (
              <div role="alert" className="rounded-lg border p-4 text-sm">
                Roster areas could not be loaded.{" "}
                <Button type="button" variant="outline" onClick={areas.retry}>
                  Try again
                </Button>
              </div>
            ) : (
              <WeeklyFields form={form} areas={areas.items} shifts={shifts.items} record={record} />
            )
          ) : (
            <div className="space-y-3">
              <Field
                id="config-add-shift"
                label="Applicable shifts"
                error={form.formState.errors.shift_revision_ids?.message}
              >
                <SelectControl
                  id="config-add-shift"
                  value=""
                  placeholder="Choose a shift to add"
                  disabled={save.isPending || selected.length >= 100}
                  onValueChange={(v) => {
                    if (!selected.includes(v))
                      form.setValue("shift_revision_ids", [...selected, v], {
                        shouldDirty: true,
                        shouldValidate: true,
                      });
                  }}
                  options={shifts.items
                    .filter((s) => s.active && !selected.includes(s.revision_id))
                    .map((s) => ({
                      value: s.revision_id,
                      label: `${s.name} · ${s.code} · ${s.start_time}–${s.end_time}`,
                    }))}
                />
              </Field>
              {!shifts.items.some((s) => s.active) && (
                <p className="text-sm text-muted-foreground">
                  Define an active{" "}
                  <GuardedLink className="font-medium underline" href="/workforce/shifts">
                    shift
                  </GuardedLink>{" "}
                  before adding membership.
                </p>
              )}
              <ul className="space-y-2">
                {selected.map((id) => {
                  const s = snapshots.get(id);
                  const current = shifts.items.some((c) => c.revision_id === id && c.active);
                  return (
                    <li
                      key={id}
                      className="flex flex-wrap items-center justify-between gap-3 rounded-xl border p-3"
                    >
                      <div>
                        <p className="text-sm font-medium">
                          {s?.name ?? "Saved shift"} · {s?.code ?? "Historical choice"}
                        </p>
                        <p className="mt-1 text-xs text-muted-foreground">
                          {s
                            ? `${s.start_time}–${s.end_time}${s.end_time < s.start_time ? " · next day" : ""}`
                            : "Original timing preserved"}
                          {!current ? " · Earlier revision retained" : ""}
                        </p>
                      </div>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        aria-label={`Remove ${s?.name ?? "saved shift"}`}
                        onClick={() =>
                          form.setValue(
                            "shift_revision_ids",
                            selected.filter((v) => v !== id),
                            { shouldDirty: true, shouldValidate: true },
                          )
                        }
                      >
                        Remove
                      </Button>
                    </li>
                  );
                })}
              </ul>
              <p className="text-xs text-muted-foreground">
                Membership pins the selected shift rules. New choices must use active current
                revisions.
              </p>
            </div>
          )}
          {shifts.hasMore && (
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={shifts.isFetchingMore}
              onClick={shifts.loadMore}
            >
              Load more shift choices
            </Button>
          )}
          {kind === "weekly_template" && areas.hasMore && (
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={areas.isFetchingMore}
              onClick={areas.loadMore}
            >
              Load more area choices
            </Button>
          )}
          <Field
            id="config-description"
            label="Description"
            error={form.formState.errors.description?.message}
          >
            <textarea
              id="config-description"
              rows={3}
              className={inputClass}
              {...form.register("description")}
            />
          </Field>
          <label className="flex items-center gap-3 text-sm">
            <input
              type="checkbox"
              className="size-4 accent-(--hms-accent)"
              {...form.register("active")}
            />
            Available for new rosters
          </label>
          <div className="rounded-xl border bg-muted/30 p-4">
            <label className="flex items-start gap-3 text-sm">
              <input
                type="checkbox"
                className="mt-0.5 size-4 shrink-0 accent-(--hms-accent)"
                aria-describedby={
                  form.formState.errors.reviewed ? "config-review-error" : undefined
                }
                {...form.register("reviewed")}
              />
              I reviewed the effective date and scheduling rules
            </label>
            {form.formState.errors.reviewed && (
              <p id="config-review-error" className="mt-2 text-xs text-destructive">
                {form.formState.errors.reviewed.message}
              </p>
            )}
            <p className="mt-3 text-xs text-muted-foreground">
              Changes create a new revision. Published rosters keep their original rules. Changing
              the effective date or reactivating a configuration requires current active references.
            </p>
          </div>
        </fieldset>
        {save.isError && (
          <p
            role="alert"
            className="rounded-xl border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive"
          >
            {save.error.message} Your input is preserved. Cancel and refresh configurations to load
            current values.
          </p>
        )}
        <div className="flex justify-end border-t pt-5">
          <Button
            type="submit"
            disabled={
              save.isPending ||
              shifts.isPending ||
              shifts.isError ||
              (kind === "weekly_template" && (areas.isPending || areas.isError))
            }
          >
            {save.isPending ? "Saving…" : "Save configuration"}
          </Button>
        </div>
      </form>
    </section>
  );
}
