"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  leaveRequestFormSchema,
  toLeaveRequestInput,
  useApiMutation,
  useApiPagedQuery,
  LEAVE_SESSIONS,
  LEAVE_UNITS,
  type LeaveConfig,
  type LeaveRequestDetail,
  type LeaveRequestForm as LeaveRequestValues,
  type LeaveRequestInput,
  type LeaveTypeRules,
  type Staff,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { inputClass, today } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";

const unitOptions = Object.entries(LEAVE_UNITS).map(([value, label]) => ({ value, label }));
const sessionOptions = Object.entries(LEAVE_SESSIONS).map(([value, label]) => ({ value, label }));

export function LeaveRequestForm({
  hospital,
  base,
  onClose,
}: {
  hospital: string;
  base: string;
  onClose: () => void;
}) {
  // One id per form instance, so a retried filing replays instead of creating a second request.
  const [id] = useState(() => crypto.randomUUID());
  const [staffID, setStaffID] = useState("");
  const [staffError, setStaffError] = useState("");
  const staff = useApiPagedQuery<Staff>(["workforce", "staff", hospital], `${base}/staff`);
  const types = useApiPagedQuery<LeaveConfig>(
    ["workforce", "leave-configs", hospital, "leave_type"],
    `${base}/hr/leave-configs?kind=leave_type`,
  );
  const active = types.items.filter((t) => t.active && t.rules);
  // The resolver is fixed at mount, so it reads a map kept in step with the loaded types.
  const [rules] = useState(() => new Map<string, LeaveTypeRules>());
  useEffect(() => {
    rules.clear();
    for (const t of types.items) if (t.active && t.rules) rules.set(t.id, t.rules);
  }, [rules, types.items]);
  const [schema] = useState(() => leaveRequestFormSchema(rules));
  const form = useZodForm(schema, {
    leave_type_id: "",
    unit: "full_day",
    start_on: today(),
    end_on: today(),
    session: "first_half",
    minutes: 15,
    reason: "",
  });
  const save = useApiMutation(
    (input: LeaveRequestInput) =>
      apiFetch<LeaveRequestDetail>(`${base}/staff/${staffID}/leave-requests/${id}`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    {
      invalidate: [["workforce", "leave-requests", hospital]],
      successToast: "Leave request filed",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  const section = useRef<HTMLFormElement>(null);
  useEffect(() => section.current?.scrollIntoView?.({ block: "start", behavior: "instant" }), []);
  const unit = form.watch("unit");
  const errors = form.formState.errors;
  return (
    <form
      ref={section}
      noValidate
      autoComplete="off"
      aria-label="File leave request"
      onSubmit={form.handleSubmit((values: LeaveRequestValues) => {
        if (!staffID) return setStaffError("Choose a staff member.");
        save.mutate(toLeaveRequestInput(values));
      })}
      className="scroll-mt-24 space-y-6 border-b p-5"
    >
      <fieldset disabled={save.isPending} className="space-y-6">
        <div className="grid gap-5 sm:grid-cols-3">
          <Field id="leave-request-staff" label="Staff member" error={staffError || undefined}>
            <SelectControl
              id="leave-request-staff"
              value={staffID}
              placeholder={staff.isPending ? "Loading staff…" : "Choose a staff member"}
              onValueChange={(v) => {
                setStaffID(v);
                setStaffError("");
              }}
              options={staff.items
                .filter((s) => s.employment_status === "active")
                .map((s) => ({ value: s.id, label: `${s.display_name} · ${s.employee_code}` }))}
            />
          </Field>
          <Field id="leave-request-type" label="Leave type" error={errors.leave_type_id?.message}>
            <SelectControl
              id="leave-request-type"
              value={form.watch("leave_type_id")}
              placeholder={types.isPending ? "Loading leave types…" : "Choose a leave type"}
              onValueChange={(v) =>
                form.setValue("leave_type_id", v, { shouldDirty: true, shouldValidate: true })
              }
              options={active.map((t) => ({ value: t.id, label: `${t.name} (${t.code})` }))}
            />
          </Field>
          <Field id="leave-request-unit" label="Taken as" error={errors.unit?.message}>
            <SelectControl
              id="leave-request-unit"
              value={unit}
              onValueChange={(v) =>
                form.setValue("unit", v as LeaveRequestValues["unit"], {
                  shouldDirty: true,
                  shouldValidate: true,
                })
              }
              options={unitOptions}
            />
          </Field>
        </div>
        <div className="grid gap-5 sm:grid-cols-3">
          <Field
            id="leave-request-start"
            label={unit === "full_day" ? "First day" : "Date"}
            error={errors.start_on?.message}
          >
            <input
              id="leave-request-start"
              type="date"
              className={inputClass}
              aria-invalid={!!errors.start_on}
              {...form.register("start_on")}
            />
          </Field>
          {unit === "full_day" && (
            <Field id="leave-request-end" label="Last day" error={errors.end_on?.message}>
              <input
                id="leave-request-end"
                type="date"
                className={inputClass}
                aria-invalid={!!errors.end_on}
                {...form.register("end_on")}
              />
            </Field>
          )}
          {unit === "half_day" && (
            <Field id="leave-request-session" label="Session" error={errors.session?.message}>
              <SelectControl
                id="leave-request-session"
                value={form.watch("session")}
                onValueChange={(v) =>
                  form.setValue("session", v as LeaveRequestValues["session"], {
                    shouldDirty: true,
                  })
                }
                options={sessionOptions}
              />
            </Field>
          )}
          {unit === "short_leave" && (
            <Field id="leave-request-minutes" label="Minutes" error={errors.minutes?.message}>
              <input
                id="leave-request-minutes"
                type="number"
                step={15}
                className={inputClass}
                aria-invalid={!!errors.minutes}
                {...form.register("minutes", { valueAsNumber: true })}
              />
            </Field>
          )}
        </div>
        <Field id="leave-request-reason" label="Reason (optional)" error={errors.reason?.message}>
          <textarea
            id="leave-request-reason"
            rows={3}
            className={inputClass}
            aria-invalid={!!errors.reason}
            {...form.register("reason")}
          />
        </Field>
        <p className="text-xs text-muted-foreground">
          The reason is shown only to people who file or approve leave. Leave out diagnoses and
          other clinical detail.
        </p>
      </fieldset>
      {save.isError && <SaveError message={save.error.message} />}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel filing
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Filing…" : "File leave request"}
        </Button>
      </div>
    </form>
  );
}
