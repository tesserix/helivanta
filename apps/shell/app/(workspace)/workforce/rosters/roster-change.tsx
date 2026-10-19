"use client";
import { useEffect, useState } from "react";
import { Button } from "@tesserix/web";
import {
  ApiError,
  ROSTER_MODES,
  rosterChangeSchema,
  rosterDates,
  useApiPagedQuery,
  type DatedAllocationInput,
  type Hospital,
  type Roster,
  type RosterChangeInput,
  type RosterConfig,
  type Staff,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { Failure, inputClass } from "../roster-policy/policy-ui";
import { ReviewCheck, SaveError, useRosterWrite } from "./roster-ui";

type Mode = RosterChangeInput["mode"];
const modeOptions = Object.entries(ROSTER_MODES).map(([value, label]) => ({ value, label }));
const blank = { staff_id: "", roster_date: "", area_revision_id: "", shift_revision_id: "" };

export function ChangeEditor({
  path,
  base,
  hospital,
  roster,
  removals,
  onClose,
}: {
  path: string;
  base: string;
  hospital: Hospital;
  roster: Roster;
  removals: Set<string>;
  onClose: () => void;
}) {
  // One change ID per editor keeps retries idempotent; allocation IDs are minted once per row for the same reason.
  const [changeID] = useState(() => crypto.randomUUID());
  const staff = useApiPagedQuery<Staff>(
    ["workforce", "staff", hospital.id],
    `${base.replace(/\/hr$/, "")}/staff`,
  );
  const areas = useApiPagedQuery<RosterConfig>(
    ["workforce", "roster-configs", hospital.id, "area"],
    `${base}/roster-configs?kind=area`,
  );
  const [row, setRow] = useState(blank);
  const form = useZodForm(rosterChangeSchema, {
    change_id: changeID,
    expected_version: roster.version,
    mode: "roster" as Mode,
    add: [],
    remove: [],
    reason: "",
    reviewed: false,
  });
  useEffect(() => form.setValue("remove", [...removals]), [form, removals]);
  const save = useRosterWrite<RosterChangeInput>(
    `${path}/changes`,
    hospital.id,
    "Roster changed",
    onClose,
  );
  const mode = form.watch("mode");
  const added = form.watch("add");
  const errors = form.formState.errors;
  const people = staff.items.filter((s) => s.employment_status === "active");
  const activeAreas = areas.items.filter((a) => a.active);
  const area = activeAreas.find((a) => a.revision_id === row.area_revision_id);
  const nameOf = (id: string) => people.find((s) => s.id === id)?.display_name ?? id;
  const areaOf = (id: string) => activeAreas.find((a) => a.revision_id === id);
  const conflicts =
    save.error instanceof ApiError && Array.isArray(save.error.detail.allocation_ids)
      ? new Set(save.error.detail.allocation_ids as string[])
      : new Set<string>();
  const complete = Object.values(row).every(Boolean);
  const addRow = () => {
    const next: DatedAllocationInput = { id: crypto.randomUUID(), ...row };
    form.setValue("add", [...added, next], { shouldValidate: form.formState.isSubmitted });
    setRow((r) => ({ ...r, roster_date: "" }));
  };
  return (
    <form
      noValidate
      aria-label="Change shifts"
      onSubmit={form.handleSubmit((input) => save.mutate(input))}
      className="space-y-5 border-b p-5"
    >
      <fieldset disabled={save.isPending} className="space-y-5">
        <div className="grid gap-5 sm:grid-cols-2">
          <Field id="change-mode" label="Change type">
            <SelectControl
              id="change-mode"
              value={mode}
              onValueChange={(v) => form.setValue("mode", v as Mode)}
              options={modeOptions}
            />
          </Field>
          <p className="self-end text-sm text-muted-foreground">
            {mode === "additional"
              ? "Adds a second shift on a date where the person already has a primary shift."
              : "Tick shifts in the grid below to remove them, and add new shifts here."}
          </p>
        </div>
        {staff.isError || areas.isError ? (
          <Failure
            message="Could not load staff or roster areas."
            retry={() => {
              staff.retry();
              areas.retry();
            }}
          />
        ) : (
          <div className="grid items-end gap-4 sm:grid-cols-5">
            <Field id="change-staff" label="Staff">
              <SelectControl
                id="change-staff"
                value={row.staff_id}
                placeholder="Choose staff"
                onValueChange={(v) => setRow((r) => ({ ...r, staff_id: v }))}
                options={people.map((s) => ({
                  value: s.id,
                  label: `${s.display_name} · ${s.employee_code}`,
                }))}
              />
            </Field>
            <Field id="change-date" label="Date">
              <SelectControl
                id="change-date"
                value={row.roster_date}
                placeholder="Choose date"
                onValueChange={(v) => setRow((r) => ({ ...r, roster_date: v }))}
                options={rosterDates(roster.start_date, roster.end_date).map((d) => ({
                  value: d,
                  label: d,
                }))}
              />
            </Field>
            <Field id="change-area" label="Area">
              <SelectControl
                id="change-area"
                value={row.area_revision_id}
                placeholder="Choose area"
                onValueChange={(v) =>
                  setRow((r) => ({ ...r, area_revision_id: v, shift_revision_id: "" }))
                }
                options={activeAreas.map((a) => ({ value: a.revision_id, label: a.name }))}
              />
            </Field>
            <Field id="change-shift" label="Shift">
              <SelectControl
                id="change-shift"
                value={row.shift_revision_id}
                placeholder="Choose shift"
                disabled={!area}
                onValueChange={(v) => setRow((r) => ({ ...r, shift_revision_id: v }))}
                options={(area?.shifts ?? []).map((s) => ({
                  value: s.revision_id,
                  label: `${s.code} ${s.start_time}–${s.end_time}`,
                }))}
              />
            </Field>
            <Button type="button" variant="outline" disabled={!complete} onClick={addRow}>
              Add shift
            </Button>
          </div>
        )}
        {(staff.hasMore || areas.hasMore) && (
          <Button
            type="button"
            variant="ghost"
            disabled={staff.isFetchingMore || areas.isFetchingMore}
            onClick={() => {
              if (staff.hasMore) staff.loadMore();
              if (areas.hasMore) areas.loadMore();
            }}
          >
            Load more staff and areas
          </Button>
        )}
        {added.length > 0 && (
          <ul aria-label="Shifts to add" className="space-y-2">
            {added.map((a) => {
              const config = areaOf(a.area_revision_id);
              const shift = config?.shifts.find((s) => s.revision_id === a.shift_revision_id);
              return (
                <li
                  key={a.id}
                  className={`flex items-center justify-between gap-4 rounded-xl border p-3 text-sm ${
                    conflicts.has(a.id) ? "border-destructive bg-destructive/5" : ""
                  }`}
                >
                  <span>
                    {nameOf(a.staff_id)} · {a.roster_date} · {config?.name} · {shift?.code}{" "}
                    {shift?.start_time}–{shift?.end_time}
                    {conflicts.has(a.id) && " · overlaps an existing shift"}
                  </span>
                  <Button
                    type="button"
                    variant="ghost"
                    aria-label={`Drop ${nameOf(a.staff_id)} on ${a.roster_date}`}
                    onClick={() =>
                      form.setValue(
                        "add",
                        added.filter((x) => x.id !== a.id),
                      )
                    }
                  >
                    Drop
                  </Button>
                </li>
              );
            })}
          </ul>
        )}
        <p className="text-sm text-muted-foreground">
          {added.length} to add · {removals.size} to remove
        </p>
        {(errors.add?.message || errors.remove?.message) && (
          <p role="alert" className="text-xs text-destructive">
            {errors.add?.message ?? errors.remove?.message}
          </p>
        )}
        <Field id="change-reason" label="Reason (optional)" error={errors.reason?.message}>
          <textarea
            id="change-reason"
            rows={2}
            className={inputClass}
            aria-invalid={!!errors.reason}
            {...form.register("reason")}
          />
        </Field>
        <ReviewCheck
          id="change-review"
          error={errors.reviewed?.message}
          register={form.register("reviewed")}
        >
          I reviewed these shift changes
        </ReviewCheck>
      </fieldset>
      {save.isError && <SaveError message={save.error.message} />}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Saving…" : "Save changes"}
        </Button>
      </div>
    </form>
  );
}
