"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  useApiMutation,
  useApiPagedQuery,
  staffRosterAssignmentSchema,
  type Hospital,
  type RosterConfig,
  type Staff,
  type StaffRosterAssignment,
  type StaffRosterAssignmentInput,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { Failure, ReviewBox, inputClass, today } from "./policy-ui";

export function AssignmentsPanel({
  hospital,
  writable,
}: {
  hospital: Hospital;
  writable: boolean;
}) {
  const base = `/workforce/hospitals/${hospital.id}`;
  const assignments = useApiPagedQuery<StaffRosterAssignment>(
    ["workforce", "roster-assignments", hospital.id],
    `${base}/hr/roster-assignments`,
  );
  const [editing, setEditing] = useState<StaffRosterAssignment | "new" | null>(null);
  const [history, setHistory] = useState<StaffRosterAssignment | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const close = () => {
    setEditing(null);
    setHistory(null);
    opener.current?.focus();
  };
  return (
    <section
      aria-label="Weekly template assignments"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-center justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Weekly template assignments</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            Fixed-shift staff follow their template when automatic population is enabled.
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
            Assign weekly template
          </Button>
        )}
      </div>
      {assignments.isPending ? (
        <p role="status" className="p-5">
          Loading assignments…
        </p>
      ) : assignments.isError ? (
        <div className="p-5">
          <Failure message="Could not load assignments." retry={assignments.retry} />
        </div>
      ) : assignments.items.length === 0 ? (
        <p className="p-5 text-sm text-muted-foreground">No staff have a weekly template yet.</p>
      ) : (
        <ul className="divide-y">
          {assignments.items.map((a) => (
            <li key={a.staff_id} className="flex flex-wrap items-center justify-between gap-4 p-5">
              <div>
                <p className="font-medium">
                  {a.display_name} · {a.employee_code}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {a.template_name} ({a.template_code}) · {a.fixed ? "Fixed shift" : "Flexible"} ·{" "}
                  {a.active ? "Active" : "Ended"} · effective {a.effective_on} · v{a.version}
                </p>
              </div>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  aria-label={`History for ${a.display_name}`}
                  onClick={(e) => {
                    opener.current = e.currentTarget;
                    setEditing(null);
                    setHistory(a);
                  }}
                >
                  History
                </Button>
                {writable && (
                  <Button
                    variant="outline"
                    aria-label={`Edit ${a.display_name}`}
                    onClick={(e) => {
                      opener.current = e.currentTarget;
                      setHistory(null);
                      setEditing(a);
                    }}
                  >
                    Edit
                  </Button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      {assignments.hasMore && (
        <div className="border-t p-5">
          <Button
            variant="outline"
            disabled={assignments.isFetchingMore}
            onClick={assignments.loadMore}
          >
            Load more assignments
          </Button>
        </div>
      )}
      {editing && (
        <AssignmentEditor
          key={editing === "new" ? "new" : editing.staff_id}
          base={base}
          hospital={hospital}
          record={editing === "new" ? undefined : editing}
          onClose={close}
        />
      )}
      {history && <AssignmentHistory base={base} record={history} onClose={close} />}
    </section>
  );
}
function AssignmentEditor({
  base,
  hospital,
  record,
  onClose,
}: {
  base: string;
  hospital: Hospital;
  record?: StaffRosterAssignment;
  onClose: () => void;
}) {
  const [staffID, setStaffID] = useState(record?.staff_id ?? "");
  const [staffError, setStaffError] = useState("");
  const form = useZodForm(staffRosterAssignmentSchema, {
    expected_version: record?.version ?? 0,
    weekly_template_revision_id: record?.weekly_template_revision_id ?? "",
    fixed: record?.fixed ?? true,
    active: record?.active ?? true,
    effective_on: record?.effective_on ?? today(),
    reviewed: false,
  });
  const staff = useApiPagedQuery<Staff>(["workforce", "staff", hospital.id], `${base}/staff`, {
    enabled: !record,
  });
  const templates = useApiPagedQuery<RosterConfig>(
    ["workforce", "roster-configs", hospital.id, "weekly_template"],
    `${base}/hr/roster-configs?kind=weekly_template`,
  );
  const save = useApiMutation(
    (input: StaffRosterAssignmentInput) =>
      apiFetch<StaffRosterAssignment>(`${base}/staff/${staffID}/roster-assignment`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    {
      invalidate: [
        ["workforce", "roster-assignments", hospital.id],
        ["workforce", "roster-assignment-history", hospital.id],
      ],
      successToast: "Assignment saved",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  const section = useRef<HTMLFormElement>(null);
  useEffect(() => {
    section.current?.scrollIntoView?.({ block: "start", behavior: "instant" });
  }, []);
  const options = templates.items
    .filter((t) => t.active)
    .map((t) => ({
      value: t.revision_id,
      label: `${t.name} (${t.code}) · from ${t.effective_on}`,
    }));
  // A pin stays valid after its template is revised, so keep it selectable while editing.
  if (record && !options.some((o) => o.value === record.weekly_template_revision_id))
    options.unshift({
      value: record.weekly_template_revision_id,
      label: `${record.template_name} (${record.template_code}) · pinned revision`,
    });
  const errors = form.formState.errors;
  return (
    <form
      ref={section}
      noValidate
      autoComplete="off"
      aria-label={record ? `Edit assignment for ${record.display_name}` : "Assign weekly template"}
      onSubmit={form.handleSubmit((input) => {
        if (!staffID) return setStaffError("Choose a staff member.");
        save.mutate(input);
      })}
      className="scroll-mt-24 space-y-6 border-t p-5"
    >
      <fieldset disabled={save.isPending} className="space-y-6">
        <div className="grid gap-5 sm:grid-cols-2">
          {record ? (
            <Field id="assignment-staff" label="Staff member">
              <input
                id="assignment-staff"
                className={inputClass}
                value={`${record.display_name} · ${record.employee_code}`}
                disabled
              />
            </Field>
          ) : (
            <Field id="assignment-staff" label="Staff member" error={staffError || undefined}>
              <SelectControl
                id="assignment-staff"
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
          )}
          <Field
            id="assignment-template"
            label="Weekly template"
            error={errors.weekly_template_revision_id?.message}
          >
            <SelectControl
              id="assignment-template"
              value={form.watch("weekly_template_revision_id")}
              placeholder={templates.isPending ? "Loading templates…" : "Choose a weekly template"}
              onValueChange={(v) =>
                form.setValue("weekly_template_revision_id", v, {
                  shouldDirty: true,
                  shouldValidate: true,
                })
              }
              options={options}
            />
          </Field>
          <Field
            id="assignment-effective"
            label="Effective date"
            error={errors.effective_on?.message}
          >
            <input
              id="assignment-effective"
              type="date"
              className={inputClass}
              aria-invalid={!!errors.effective_on}
              {...form.register("effective_on")}
            />
          </Field>
        </div>
        <div className="flex flex-wrap gap-6 text-sm">
          <label className="flex items-center gap-3">
            <input
              type="checkbox"
              className="size-4 accent-(--hms-accent)"
              {...form.register("fixed")}
            />
            Fixed shift (populated automatically)
          </label>
          <label className="flex items-center gap-3">
            <input
              type="checkbox"
              className="size-4 accent-(--hms-accent)"
              {...form.register("active")}
            />
            Assignment active
          </label>
        </div>
        <ReviewBox
          id="assignment-review"
          error={errors.reviewed?.message}
          register={form.register("reviewed")}
        >
          I reviewed the template, effective date and population mode
        </ReviewBox>
      </fieldset>
      {(staff.hasMore || templates.hasMore) && (
        <div className="flex gap-2">
          {staff.hasMore && (
            <Button
              type="button"
              variant="outline"
              disabled={staff.isFetchingMore}
              onClick={staff.loadMore}
            >
              Load more staff
            </Button>
          )}
          {templates.hasMore && (
            <Button
              type="button"
              variant="outline"
              disabled={templates.isFetchingMore}
              onClick={templates.loadMore}
            >
              Load more templates
            </Button>
          )}
        </div>
      )}
      {(staff.isError || templates.isError) && (
        <Failure
          message="Could not load staff or weekly templates."
          retry={() => {
            staff.retry();
            templates.retry();
          }}
        />
      )}
      {save.isError && (
        <p
          role="alert"
          className="rounded-xl border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive"
        >
          {save.error.message} Your input is preserved. Cancel and reload to see current
          assignments.
        </p>
      )}
      <div className="flex justify-end gap-2 border-t pt-5">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel changes
        </Button>
        <Button type="submit" disabled={save.isPending || templates.isPending || templates.isError}>
          {save.isPending ? "Saving…" : "Save assignment"}
        </Button>
      </div>
    </form>
  );
}
function AssignmentHistory({
  base,
  record,
  onClose,
}: {
  base: string;
  record: StaffRosterAssignment;
  onClose: () => void;
}) {
  const history = useApiPagedQuery<StaffRosterAssignment>(
    ["workforce", "roster-assignment-history", record.staff_id],
    `${base}/staff/${record.staff_id}/roster-assignment/history`,
  );
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  return (
    <div role="region" aria-label="Assignment history" className="border-t p-5">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          {record.display_name} · revision history
        </h4>
        <Button variant="ghost" onClick={onClose}>
          Close history
        </Button>
      </div>
      {history.isPending ? (
        <p role="status">Loading assignment history…</p>
      ) : history.isError ? (
        <Failure message="Could not load assignment history." retry={history.retry} />
      ) : (
        <ul className="space-y-3">
          {history.items.map((r) => (
            <li key={r.revision_id} className="rounded-xl border p-4 text-sm">
              <p className="font-medium">
                v{r.version} · {r.template_name} ({r.template_code})
              </p>
              <p className="mt-1 text-muted-foreground">
                {r.fixed ? "Fixed shift" : "Flexible"} · {r.active ? "Active" : "Ended"} · effective{" "}
                {r.effective_on} · reviewed by {r.reviewed_by_subject}
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
