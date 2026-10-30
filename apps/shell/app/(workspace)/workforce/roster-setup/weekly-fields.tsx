"use client";
import { useState } from "react";
import type { useZodForm } from "@helivanta/ui";
import type { rosterConfigSchema } from "@helivanta/api";
import { Button } from "@tesserix/web";
import { Field, SelectControl } from "@helivanta/ui";
import {
  WEEKDAYS,
  type RosterAllocationInput,
  type RosterConfig,
  type ShiftDefinition,
} from "@helivanta/api";

type Form = ReturnType<typeof useZodForm<typeof rosterConfigSchema>>;
const blank = (): RosterAllocationInput => ({ shift_revision_id: "", area_revision_id: "" });

export function WeeklyFields({
  form,
  areas,
  shifts,
  record,
}: {
  form: Form;
  areas: RosterConfig[];
  shifts: ShiftDefinition[];
  record?: RosterConfig;
}) {
  const days = form.watch("days");
  return (
    <div className="space-y-4">
      <div>
        <h4 className="font-semibold">Weekly pattern</h4>
        <p className="mt-2 max-w-3xl text-sm text-muted-foreground">
          Choose one or two shifts for each working day. Weekly off means no work within that
          calendar day, including the end of an overnight shift. The complete week must not contain
          overlaps.
        </p>
      </div>
      {form.formState.errors.days?.message && (
        <p className="text-sm text-destructive">{form.formState.errors.days.message}</p>
      )}
      {!areas.some((a) => a.active) && (
        <p className="rounded-lg border p-4 text-sm">
          Define an active roster area with current shift rules before completing a weekly template.
        </p>
      )}
      <div className="grid gap-4 lg:grid-cols-2">
        {days.map((day, index) => (
          <Weekday
            key={day.weekday}
            index={index}
            form={form}
            areas={areas}
            shifts={shifts}
            saved={record?.days.find((d) => d.weekday === day.weekday)}
          />
        ))}
      </div>
    </div>
  );
}
function Weekday({
  index,
  form,
  areas,
  shifts,
  saved,
}: {
  index: number;
  form: Form;
  areas: RosterConfig[];
  shifts: ShiftDefinition[];
  saved?: RosterConfig["days"][number];
}) {
  const day = form.watch(`days.${index}`);
  const name = WEEKDAYS[day.weekday];
  const effectiveOn = form.watch("effective_on");
  const [previous, setPrevious] = useState<RosterAllocationInput[]>(day.allocations);
  const currentShifts = new Set(shifts.filter((s) => s.active).map((s) => s.revision_id));
  const update = (allocations: RosterAllocationInput[]) =>
    form.setValue(`days.${index}.allocations`, allocations, {
      shouldDirty: true,
      shouldValidate: false,
    });
  const switchOff = (off: boolean) => {
    if (off) {
      setPrevious(day.allocations);
      update([]);
    } else update(previous.length ? previous.map((a) => ({ ...a })) : [blank()]);
    form.setValue(`days.${index}.weekly_off`, off, { shouldDirty: true, shouldValidate: false });
  };
  return (
    <fieldset aria-label={name} className="min-w-0 rounded-xl border bg-muted/15 p-4">
      <legend className="px-1 text-sm font-semibold">{name}</legend>
      <div className="mb-4 flex justify-end">
        <label className="flex items-center gap-2 text-xs">
          <input
            type="checkbox"
            className="size-4 accent-(--hms-accent)"
            aria-label={`${name} weekly off`}
            checked={day.weekly_off}
            onChange={(e) => switchOff(e.target.checked)}
          />
          Weekly off
        </label>
      </div>
      {day.weekly_off ? (
        <p className="text-sm text-muted-foreground">No scheduled work</p>
      ) : (
        <div className="space-y-4">
          {day.allocations.map((allocation, slot) => {
            const ordinal = slot === 0 ? "first" : "second";
            const old = saved?.allocations.find(
              (a) =>
                a.area_revision_id === allocation.area_revision_id &&
                a.shift_revision_id === allocation.shift_revision_id,
            );
            const area = areas.find((a) => a.revision_id === allocation.area_revision_id);
            const areaOptions = areas
              .filter((a) => a.active && a.effective_on <= effectiveOn)
              .map((a) => ({ value: a.revision_id, label: `${a.name} · ${a.code}` }));
            if (!area && old)
              areaOptions.push({
                value: old.area_revision_id,
                label: `${old.area_name} · saved revision`,
              });
            const applicable = area?.shifts.filter((s) => currentShifts.has(s.revision_id)) ?? [];
            const shiftOptions = applicable.map((s) => ({
              value: s.revision_id,
              label: `${s.name} · ${s.code} · ${s.start_time}–${s.end_time}`,
            }));
            if (old && !shiftOptions.some((o) => o.value === old.shift_revision_id))
              shiftOptions.push({
                value: old.shift_revision_id,
                label: `${old.shift.name} · ${old.shift.code} · saved revision`,
              });
            const errors = form.formState.errors.days?.[index]?.allocations?.[slot];
            const changeArea = (value: string) =>
              update(
                day.allocations.map((a, i) =>
                  i === slot ? { area_revision_id: value, shift_revision_id: "" } : a,
                ),
              );
            const changeShift = (value: string) =>
              update(
                day.allocations.map((a, i) =>
                  i === slot ? { ...a, shift_revision_id: value } : a,
                ),
              );
            return (
              <div key={slot} className="space-y-3 rounded-lg border bg-card p-3">
                <div className="flex items-center justify-between gap-3">
                  <p className="text-xs font-semibold">{slot === 0 ? "First" : "Second"} shift</p>
                  {slot === 1 && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      aria-label={`Remove second shift for ${name}`}
                      onClick={() => update(day.allocations.slice(0, 1))}
                    >
                      Remove
                    </Button>
                  )}
                </div>
                <Field
                  id={`day-${index}-${slot}-area`}
                  label={`${name} ${ordinal} area`}
                  error={errors?.area_revision_id?.message}
                >
                  <SelectControl
                    id={`day-${index}-${slot}-area`}
                    value={allocation.area_revision_id}
                    onValueChange={changeArea}
                    placeholder="Choose an area"
                    options={areaOptions}
                  />
                </Field>
                <Field
                  id={`day-${index}-${slot}-shift`}
                  label={`${name} ${ordinal} shift`}
                  error={errors?.shift_revision_id?.message}
                >
                  <SelectControl
                    id={`day-${index}-${slot}-shift`}
                    value={allocation.shift_revision_id}
                    onValueChange={changeShift}
                    placeholder="Choose an applicable shift"
                    disabled={!allocation.area_revision_id}
                    options={shiftOptions}
                  />
                </Field>
                {area && allocation.area_revision_id && !shiftOptions.length && (
                  <p className="text-xs text-muted-foreground">
                    Update this area to active current shift revisions before using it in a new
                    pattern.
                  </p>
                )}
              </div>
            );
          })}
          {form.formState.errors.days?.[index]?.allocations?.message && (
            <p className="text-xs text-destructive">
              {form.formState.errors.days?.[index]?.allocations?.message}
            </p>
          )}
          {day.allocations.length < 2 && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              aria-label={`Add second shift for ${name}`}
              onClick={() => update([...day.allocations, blank()])}
            >
              Add second shift
            </Button>
          )}
        </div>
      )}
    </fieldset>
  );
}
