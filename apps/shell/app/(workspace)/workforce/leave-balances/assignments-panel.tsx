"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  staffLeaveAssignmentSchema,
  useApiMutation,
  useApiPagedQuery,
  type Hospital,
  type LeaveConfig,
  type Staff,
  type StaffLeaveAssignment,
  type StaffLeaveAssignmentInput,
} from "@helivanta/api";
import { Field, SelectControl, useZodForm } from "@helivanta/ui";
import { Failure, inputClass, today } from "../roster-policy/policy-ui";
import { LeaveReview, SaveError } from "./years-panel";

export function LeaveAssignmentsPanel({
  hospital,
  writable,
}: {
  hospital: Hospital;
  writable: boolean;
}) {
  const base = `/workforce/hospitals/${hospital.id}`;
  const assignments = useApiPagedQuery<StaffLeaveAssignment>(
    ["workforce", "leave-assignments", hospital.id],
    `${base}/hr/leave-assignments`,
  );
  const [editing, setEditing] = useState<StaffLeaveAssignment | "new" | null>(null);
  const [history, setHistory] = useState<StaffLeaveAssignment | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const close = () => {
    setEditing(null);
    setHistory(null);
    opener.current?.focus();
  };
  return (
    <section
      aria-label="Leave structure assignments"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-center justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Leave structure assignments</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            Each staff member accrues leave from the structure revision pinned here.
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
            Assign leave structure
          </Button>
        )}
      </div>
      {assignments.isPending ? (
        <p role="status" className="p-5">
          Loading assignments…
        </p>
      ) : assignments.isError ? (
        <div className="p-5">
          <Failure message="Could not load leave assignments." retry={assignments.retry} />
        </div>
      ) : assignments.items.length === 0 ? (
        <p className="p-5 text-sm text-muted-foreground">No staff have a leave structure yet.</p>
      ) : (
        <ul className="divide-y">
          {assignments.items.map((a) => (
            <li key={a.staff_id} className="flex flex-wrap items-center justify-between gap-4 p-5">
              <div>
                <p className="font-medium">
                  {a.display_name} · {a.employee_code}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {a.structure_name} ({a.structure_code}) · {a.active ? "Active" : "Ended"} ·
                  effective {a.effective_on} · v{a.version}
                </p>
              </div>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  aria-label={`Leave assignment history for ${a.display_name}`}
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
                    aria-label={`Edit leave assignment for ${a.display_name}`}
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
      {history && (
        <AssignmentHistory
          key={history.staff_id}
          base={base}
          hospital={hospital.id}
          record={history}
          onClose={close}
        />
      )}
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
  record?: StaffLeaveAssignment;
  onClose: () => void;
}) {
  const [staffID, setStaffID] = useState(record?.staff_id ?? "");
  const [staffError, setStaffError] = useState("");
  const form = useZodForm(staffLeaveAssignmentSchema, {
    expected_version: record?.version ?? 0,
    structure_revision_id: record?.structure_revision_id ?? "",
    active: record?.active ?? true,
    effective_on: record?.effective_on ?? today(),
    reviewed: false,
  });
  const staff = useApiPagedQuery<Staff>(["workforce", "staff", hospital.id], `${base}/staff`, {
    enabled: !record,
  });
  const structures = useApiPagedQuery<LeaveConfig>(
    ["workforce", "leave-configs", hospital.id, "structure"],
    `${base}/hr/leave-configs?kind=structure`,
  );
  const save = useApiMutation(
    (input: StaffLeaveAssignmentInput) =>
      apiFetch<StaffLeaveAssignment>(`${base}/staff/${staffID}/leave-assignment`, {
        method: "PUT",
        body: JSON.stringify(input),
      }),
    {
      invalidate: [
        ["workforce", "leave-assignments", hospital.id],
        ["workforce", "leave-assignment-history", hospital.id],
        ["workforce", "leave-balances", hospital.id],
      ],
      successToast: "Leave structure assigned",
      suppressErrorToast: true,
      onSuccess: onClose,
    },
  );
  const section = useRef<HTMLFormElement>(null);
  useEffect(() => section.current?.scrollIntoView?.({ block: "start", behavior: "instant" }), []);
  const options = structures.items
    .filter((s) => s.active)
    .map((s) => ({
      value: s.revision_id,
      label: `${s.name} (${s.code}) · from ${s.effective_on}`,
    }));
  // A pin stays valid after its structure is revised, so keep it selectable while editing.
  if (record && !options.some((o) => o.value === record.structure_revision_id))
    options.unshift({
      value: record.structure_revision_id,
      label: `${record.structure_name} (${record.structure_code}) · pinned revision`,
    });
  const errors = form.formState.errors;
  return (
    <form
      ref={section}
      noValidate
      autoComplete="off"
      aria-label={
        record ? `Edit leave assignment for ${record.display_name}` : "Assign leave structure"
      }
      onSubmit={form.handleSubmit((input) => {
        if (!staffID) return setStaffError("Choose a staff member.");
        save.mutate(input);
      })}
      className="scroll-mt-24 space-y-6 border-t p-5"
    >
      <fieldset disabled={save.isPending} className="space-y-6">
        <div className="grid gap-5 sm:grid-cols-2">
          {record ? (
            <Field id="leave-assignment-staff" label="Staff member">
              <input
                id="leave-assignment-staff"
                className={inputClass}
                value={`${record.display_name} · ${record.employee_code}`}
                disabled
              />
            </Field>
          ) : (
            <Field id="leave-assignment-staff" label="Staff member" error={staffError || undefined}>
              <SelectControl
                id="leave-assignment-staff"
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
            id="leave-assignment-structure"
            label="Leave structure"
            error={errors.structure_revision_id?.message}
          >
            <SelectControl
              id="leave-assignment-structure"
              value={form.watch("structure_revision_id")}
              placeholder={
                structures.isPending ? "Loading structures…" : "Choose a leave structure"
              }
              onValueChange={(v) =>
                form.setValue("structure_revision_id", v, {
                  shouldDirty: true,
                  shouldValidate: true,
                })
              }
              options={options}
            />
          </Field>
          <Field
            id="leave-assignment-effective"
            label="Effective date"
            error={errors.effective_on?.message}
          >
            <input
              id="leave-assignment-effective"
              type="date"
              className={inputClass}
              aria-invalid={!!errors.effective_on}
              {...form.register("effective_on")}
            />
          </Field>
        </div>
        <label className="flex items-center gap-3 text-sm">
          <input
            type="checkbox"
            className="size-4 accent-(--hms-accent)"
            {...form.register("active")}
          />
          Assignment active (inactive staff accrue nothing)
        </label>
        <LeaveReview
          id="leave-assignment-review"
          error={errors.reviewed?.message}
          register={form.register("reviewed")}
        >
          I reviewed the structure and effective date. Months already posted keep their credits.
        </LeaveReview>
      </fieldset>
      {(staff.hasMore || structures.hasMore) && (
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
          {structures.hasMore && (
            <Button
              type="button"
              variant="outline"
              disabled={structures.isFetchingMore}
              onClick={structures.loadMore}
            >
              Load more structures
            </Button>
          )}
        </div>
      )}
      {(staff.isError || structures.isError) && (
        <Failure
          message="Could not load staff or leave structures."
          retry={() => {
            staff.retry();
            structures.retry();
          }}
        />
      )}
      {save.isError && <SaveError message={save.error.message} />}
      <div className="flex justify-end gap-2 border-t pt-5">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel changes
        </Button>
        <Button
          type="submit"
          disabled={save.isPending || structures.isPending || structures.isError}
        >
          {save.isPending ? "Saving…" : "Assign leave structure"}
        </Button>
      </div>
    </form>
  );
}

function AssignmentHistory({
  base,
  hospital,
  record,
  onClose,
}: {
  base: string;
  hospital: string;
  record: StaffLeaveAssignment;
  onClose: () => void;
}) {
  const history = useApiPagedQuery<StaffLeaveAssignment>(
    ["workforce", "leave-assignment-history", hospital, record.staff_id],
    `${base}/staff/${record.staff_id}/leave-assignment/history`,
  );
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  return (
    <div role="region" aria-label="Leave assignment history" className="border-t p-5">
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
                v{r.version} · {r.structure_name} ({r.structure_code})
              </p>
              <p className="mt-1 text-muted-foreground">
                {r.active ? "Active" : "Ended"} · effective {r.effective_on} · reviewed by{" "}
                {r.reviewed_by_subject}
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
