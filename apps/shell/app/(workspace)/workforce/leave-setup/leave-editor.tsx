"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  useApiMutation,
  useApiPagedQuery,
  leaveConfigSchema,
  LEAVE_ACCRUALS,
  LEAVE_HOLIDAY_CATEGORIES,
  LEAVE_PRORATIONS,
  type Hospital,
  type LeaveConfig,
  type LeaveConfigInput,
  type LeaveStructureEntry,
} from "@helivanta/api";
import { ConfirmDialog, Field, SelectControl, useFieldArray, useZodForm } from "@helivanta/ui";
import { Failure, inputClass, today } from "../roster-policy/policy-ui";
import { configNoun } from "./leave-summary";

type Props = {
  path: string;
  hospital: Hospital;
  kind: LeaveConfig["kind"];
  record?: LeaveConfig;
  onClose: () => void;
};

const options = (labels: Record<string, string>) =>
  Object.entries(labels).map(([value, label]) => ({ value, label }));

// Structures validate allowances against the loaded leave types, so the form mounts once they are known.
export function LeaveEditor(props: Props) {
  const types = useApiPagedQuery<LeaveConfig>(
    ["workforce", "leave-configs", props.hospital.id, "leave_type"],
    `${props.path}?kind=leave_type`,
    { enabled: props.kind === "structure" },
  );
  if (props.kind !== "structure") return <LeaveForm {...props} types={[]} />;
  if (types.isPending) return <p role="status">Loading leave types…</p>;
  if (types.isError) return <Failure message="Could not load leave types." retry={types.retry} />;
  return (
    <>
      <LeaveForm {...props} types={types.items} />
      {types.hasMore && (
        <Button
          className="mt-4"
          variant="outline"
          disabled={types.isFetchingMore}
          onClick={types.loadMore}
        >
          Load more leave types
        </Button>
      )}
    </>
  );
}

function LeaveForm({
  path,
  hospital,
  kind,
  record,
  onClose,
  types,
}: Props & { types: LeaveConfig[] }) {
  const [id] = useState(() => record?.id ?? crypto.randomUUID());
  const snapshots = new Map<
    string,
    Pick<LeaveStructureEntry, "name" | "code" | "balance_tracked">
  >();
  for (const e of record?.entries ?? []) snapshots.set(e.leave_type_revision_id, e);
  for (const t of types)
    if (t.rules) snapshots.set(t.revision_id, { ...t, balance_tracked: t.rules.balance_tracked });
  const untracked = new Set(
    [...snapshots].filter(([, s]) => !s.balance_tracked).map(([revision]) => revision),
  );
  const [schema] = useState(() => leaveConfigSchema(untracked));
  const form = useZodForm(schema, {
    expected_version: record?.version ?? 0,
    kind,
    code: record?.code ?? "",
    name: record?.name ?? "",
    description: record?.description ?? "",
    active: record?.active ?? true,
    effective_on: record?.effective_on ?? today(),
    reviewed: false,
    calendar_year:
      kind === "holiday_calendar"
        ? (record?.calendar_year ?? new Date().getUTCFullYear() + 1)
        : undefined,
    rules:
      kind === "leave_type"
        ? (record?.rules ?? {
            paid: true,
            balance_tracked: true,
            half_day_allowed: true,
            short_leave_minutes: 0,
            clubbable: false,
          })
        : undefined,
    holidays: record?.holidays ?? [],
    entries:
      record?.entries.map((e) => ({
        leave_type_revision_id: e.leave_type_revision_id,
        annual_half_days: e.annual_half_days,
        accrual: e.accrual,
        carry_forward_half_days: e.carry_forward_half_days,
        proration: e.proration,
        probation_eligible: e.probation_eligible,
      })) ?? [],
  });
  const holidays = useFieldArray({ control: form.control, name: "holidays" });
  const entries = useFieldArray({ control: form.control, name: "entries" });
  const [retiring, setRetiring] = useState<LeaveConfigInput | null>(null);
  const section = useRef<HTMLElement>(null);
  const save = useApiMutation(
    (input: LeaveConfigInput) =>
      apiFetch<LeaveConfig>(`${path}/${id}`, { method: "PUT", body: JSON.stringify(input) }),
    {
      invalidate: [["workforce", "leave-configs", hospital.id]],
      successToast: `${configNoun[kind][0].toUpperCase()}${configNoun[kind].slice(1)} saved`,
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  useEffect(() => {
    section.current?.scrollIntoView?.({ block: "start", behavior: "instant" });
    form.setFocus("name");
  }, [form]);
  const errors = form.formState.errors;
  const chosen = form.watch("entries");
  const year = form.watch("calendar_year");
  const typeOf = new Map<string, string>();
  for (const e of record?.entries ?? []) typeOf.set(e.leave_type_revision_id, e.leave_type_id);
  for (const t of types) typeOf.set(t.revision_id, t.id);
  const chosenTypes = new Set(chosen.map((e) => typeOf.get(e.leave_type_revision_id)));
  const input = (name: string, label: string, error?: string) => ({
    id: `leave-${name}`,
    label,
    error,
  });
  const halfDayInput = (index: number, name: "annual_half_days" | "carry_forward_half_days") => {
    const fixed = untracked.has(chosen[index]?.leave_type_revision_id ?? "");
    return (
      <input
        id={`leave-entry-${index}-${name}`}
        type="number"
        min={0}
        max={365}
        step={0.5}
        disabled={fixed}
        className={inputClass}
        value={(chosen[index]?.[name] ?? 0) / 2}
        onChange={(e) =>
          form.setValue(`entries.${index}.${name}`, Math.round(Number(e.target.value) * 2), {
            shouldDirty: true,
            shouldValidate: form.formState.isSubmitted,
          })
        }
      />
    );
  };
  const submit = (value: LeaveConfigInput) =>
    record?.active && !value.active ? setRetiring(value) : save.mutate(value);
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
        <Button variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel changes
        </Button>
      </div>
      <form
        noValidate
        autoComplete="off"
        onSubmit={form.handleSubmit(submit)}
        className="space-y-6"
      >
        <fieldset disabled={save.isPending} className="space-y-6">
          <div className="grid gap-5 sm:grid-cols-2">
            <Field {...input("name", "Name", errors.name?.message)}>
              <input
                id="leave-name"
                className={inputClass}
                aria-invalid={!!errors.name}
                {...form.register("name")}
              />
            </Field>
            <Field {...input("code", "Code", errors.code?.message)}>
              <input
                id="leave-code"
                className={inputClass}
                disabled={!!record}
                aria-invalid={!!errors.code}
                {...form.register("code")}
              />
            </Field>
            <Field {...input("effective", "Effective date", errors.effective_on?.message)}>
              <input
                id="leave-effective"
                type="date"
                className={inputClass}
                aria-invalid={!!errors.effective_on}
                {...form.register("effective_on")}
              />
            </Field>
            {kind === "holiday_calendar" && (
              <Field {...input("year", "Calendar year", errors.calendar_year?.message)}>
                <input
                  id="leave-year"
                  type="number"
                  min={2000}
                  max={2200}
                  disabled={!!record}
                  className={inputClass}
                  aria-invalid={!!errors.calendar_year}
                  {...form.register("calendar_year", { valueAsNumber: true })}
                />
              </Field>
            )}
          </div>
          {kind === "leave_type" && (
            <fieldset className="space-y-4 rounded-xl border p-4">
              <legend className="px-1 text-sm font-semibold">Rules</legend>
              <div className="grid gap-3 sm:grid-cols-2">
                {(
                  [
                    ["paid", "Paid leave"],
                    ["balance_tracked", "Keep a balance for this leave"],
                    ["half_day_allowed", "Allow half days"],
                    ["clubbable", "Can be combined with other leave"],
                  ] as const
                ).map(([name, label]) => (
                  <label key={name} className="flex items-center gap-3 text-sm">
                    <input
                      type="checkbox"
                      className="size-4 accent-(--hms-accent)"
                      {...form.register(`rules.${name}`)}
                    />
                    {label}
                  </label>
                ))}
              </div>
              <Field
                {...input(
                  "short-leave",
                  "Short leave limit (minutes, 0 for none)",
                  errors.rules?.short_leave_minutes?.message,
                )}
              >
                <input
                  id="leave-short-leave"
                  type="number"
                  min={0}
                  max={480}
                  step={15}
                  className={`${inputClass} sm:max-w-xs`}
                  aria-invalid={!!errors.rules?.short_leave_minutes}
                  {...form.register("rules.short_leave_minutes", { valueAsNumber: true })}
                />
              </Field>
            </fieldset>
          )}
          {kind === "holiday_calendar" && (
            <fieldset className="space-y-4 rounded-xl border p-4">
              <legend className="px-1 text-sm font-semibold">Holidays</legend>
              {holidays.fields.map((field, index) => (
                <div
                  key={field.id}
                  role="group"
                  aria-label={`Holiday ${index + 1}`}
                  className="grid items-end gap-3 sm:grid-cols-[10rem_1fr_10rem_auto]"
                >
                  <Field
                    {...input(
                      `holiday-${index}-date`,
                      "Date",
                      errors.holidays?.[index]?.holiday_date?.message,
                    )}
                  >
                    <input
                      id={`leave-holiday-${index}-date`}
                      type="date"
                      min={`${year}-01-01`}
                      max={`${year}-12-31`}
                      className={inputClass}
                      {...form.register(`holidays.${index}.holiday_date`)}
                    />
                  </Field>
                  <Field
                    {...input(
                      `holiday-${index}-name`,
                      "Holiday name",
                      errors.holidays?.[index]?.name?.message,
                    )}
                  >
                    <input
                      id={`leave-holiday-${index}-name`}
                      className={inputClass}
                      {...form.register(`holidays.${index}.name`)}
                    />
                  </Field>
                  <Field {...input(`holiday-${index}-category`, "Category")}>
                    <SelectControl
                      id={`leave-holiday-${index}-category`}
                      value={form.watch(`holidays.${index}.category`)}
                      onValueChange={(v) =>
                        form.setValue(
                          `holidays.${index}.category`,
                          v as keyof typeof LEAVE_HOLIDAY_CATEGORIES,
                          { shouldDirty: true },
                        )
                      }
                      options={options(LEAVE_HOLIDAY_CATEGORIES)}
                    />
                  </Field>
                  <Button
                    type="button"
                    variant="ghost"
                    aria-label={`Remove holiday ${index + 1}`}
                    onClick={() => holidays.remove(index)}
                  >
                    Remove
                  </Button>
                </div>
              ))}
              {errors.holidays?.message && (
                <p className="text-xs text-destructive">{errors.holidays.message}</p>
              )}
              <Button
                type="button"
                variant="outline"
                disabled={holidays.fields.length >= 100}
                onClick={() => holidays.append({ holiday_date: "", name: "", category: "regular" })}
              >
                Add holiday
              </Button>
            </fieldset>
          )}
          {kind === "structure" && (
            <fieldset className="space-y-4 rounded-xl border p-4">
              <legend className="px-1 text-sm font-semibold">Leave types</legend>
              <Field {...input("add-type", "Add a leave type", errors.entries?.message)}>
                <SelectControl
                  id="leave-add-type"
                  value=""
                  placeholder="Choose a leave type to add"
                  disabled={entries.fields.length >= 30}
                  onValueChange={(revision) =>
                    entries.append({
                      leave_type_revision_id: revision,
                      annual_half_days: 0,
                      accrual: "upfront",
                      carry_forward_half_days: 0,
                      proration: "none",
                      probation_eligible: false,
                    })
                  }
                  options={types
                    .filter((t) => t.active && !chosenTypes.has(t.id))
                    .map((t) => ({ value: t.revision_id, label: `${t.name} · ${t.code}` }))}
                />
              </Field>
              {!types.some((t) => t.active) && (
                <p className="text-sm text-muted-foreground">
                  Add an active leave type before building a structure. Choose{" "}
                  <span className="font-medium">Leave types</span> in Configuration above.
                </p>
              )}
              {entries.fields.map((field, index) => {
                const revision = chosen[index]?.leave_type_revision_id ?? "";
                const s = snapshots.get(revision);
                const current = types.some((t) => t.revision_id === revision && t.active);
                return (
                  <div
                    key={field.id}
                    role="group"
                    aria-label={s?.name ?? "Saved leave type"}
                    className="space-y-3 rounded-xl border p-4"
                  >
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <p className="text-sm font-medium">
                        {s?.name ?? "Saved leave type"} · {s?.code ?? "Historical choice"}
                        {!current && (
                          <span className="ml-2 text-xs text-muted-foreground">
                            Earlier revision retained
                          </span>
                        )}
                      </p>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        aria-label={`Remove ${s?.name ?? "saved leave type"}`}
                        onClick={() => entries.remove(index)}
                      >
                        Remove
                      </Button>
                    </div>
                    {untracked.has(revision) ? (
                      <p className="text-xs text-muted-foreground">
                        No balance is kept for this leave type, so it has no allowance.
                      </p>
                    ) : (
                      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                        <Field
                          {...input(
                            `entry-${index}-annual_half_days`,
                            "Days per year",
                            errors.entries?.[index]?.annual_half_days?.message,
                          )}
                        >
                          {halfDayInput(index, "annual_half_days")}
                        </Field>
                        <Field
                          {...input(
                            `entry-${index}-carry_forward_half_days`,
                            "Carry forward up to (days)",
                            errors.entries?.[index]?.carry_forward_half_days?.message,
                          )}
                        >
                          {halfDayInput(index, "carry_forward_half_days")}
                        </Field>
                        <Field {...input(`entry-${index}-accrual`, "Credit")}>
                          <SelectControl
                            id={`leave-entry-${index}-accrual`}
                            value={chosen[index]?.accrual ?? "upfront"}
                            onValueChange={(v) =>
                              form.setValue(
                                `entries.${index}.accrual`,
                                v as keyof typeof LEAVE_ACCRUALS,
                                { shouldDirty: true },
                              )
                            }
                            options={options(LEAVE_ACCRUALS)}
                          />
                        </Field>
                        <Field {...input(`entry-${index}-proration`, "New joiners")}>
                          <SelectControl
                            id={`leave-entry-${index}-proration`}
                            value={chosen[index]?.proration ?? "none"}
                            onValueChange={(v) =>
                              form.setValue(
                                `entries.${index}.proration`,
                                v as keyof typeof LEAVE_PRORATIONS,
                                { shouldDirty: true },
                              )
                            }
                            options={options(LEAVE_PRORATIONS)}
                          />
                        </Field>
                      </div>
                    )}
                    {errors.entries?.[index]?.leave_type_revision_id?.message && (
                      <p className="text-xs text-destructive">
                        {errors.entries[index].leave_type_revision_id.message}
                      </p>
                    )}
                    <label className="flex items-center gap-3 text-sm">
                      <input
                        type="checkbox"
                        className="size-4 accent-(--hms-accent)"
                        {...form.register(`entries.${index}.probation_eligible`)}
                      />
                      Available during probation
                    </label>
                  </div>
                );
              })}
            </fieldset>
          )}
          <Field {...input("description", "Description", errors.description?.message)}>
            <textarea
              id="leave-description"
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
            Available for new leave setup
          </label>
          <div className="rounded-xl border bg-muted/30 p-4">
            <label className="flex items-start gap-3 text-sm">
              <input
                type="checkbox"
                className="mt-0.5 size-4 shrink-0 accent-(--hms-accent)"
                aria-describedby={errors.reviewed ? "leave-review-error" : undefined}
                {...form.register("reviewed")}
              />
              I reviewed the effective date and leave rules
            </label>
            {errors.reviewed && (
              <p id="leave-review-error" className="mt-2 text-xs text-destructive">
                {errors.reviewed.message}
              </p>
            )}
            <p className="mt-3 text-xs text-muted-foreground">
              Changes create a new revision effective on or after the current one. Structures keep
              the leave type revisions they were saved with.
            </p>
          </div>
        </fieldset>
        {save.isError && (
          <p
            role="alert"
            className="rounded-xl border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive"
          >
            {save.error.message} Your input is preserved. Cancel and refresh to load current values.
          </p>
        )}
        <div className="flex justify-end border-t pt-5">
          <Button type="submit" disabled={save.isPending}>
            {save.isPending ? "Saving…" : "Save configuration"}
          </Button>
        </div>
      </form>
      <ConfirmDialog
        open={!!retiring}
        onOpenChange={(open) => !open && setRetiring(null)}
        title={`Retire ${record?.name ?? configNoun[kind]}?`}
        description="It stays in history and in structures that already use it, but cannot be chosen for new leave setup."
        confirmLabel="Retire"
        busy={save.isPending}
        onConfirm={() => {
          if (retiring) save.mutate(retiring);
          setRetiring(null);
        }}
      />
    </section>
  );
}
