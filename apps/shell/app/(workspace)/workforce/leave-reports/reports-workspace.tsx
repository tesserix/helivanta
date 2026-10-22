"use client";
import { useState } from "react";
import { Button } from "@tesserix/web";
import {
  leaveReportFilterSchema,
  leaveReportPath,
  useApiPagedQuery,
  usePermissions,
  LEAVE_REQUEST_STATUSES,
  type Hospital,
  type HRMaster,
  type LeaveConfig,
  type LeaveReportFilter,
} from "@helivanta/api";
import { Field, PageHeading, SelectControl, useZodForm } from "@helivanta/ui";
import { Failure, inputClass, today } from "../roster-policy/policy-ui";
import { LeaveReportTable } from "./report-table";

export function LeaveReportsWorkspace() {
  const hospitals = useApiPagedQuery<Hospital>(
    ["workforce", "planning-hospitals"],
    "/workforce/hospitals",
  );
  const [selected, setSelected] = useState("");
  const hospital = hospitals.items.find((h) => h.id === selected) ?? hospitals.items[0];
  return (
    <>
      <PageHeading
        description="Approved leave by month and type, and every request with its status, for a period of up to 366 days. Department and joining date appear only for enrollment managers."
        actions={
          hospital && (
            <div className="w-full sm:w-72">
              <Field id="reports-hospital" label="Hospital">
                <SelectControl
                  id="reports-hospital"
                  value={hospital.id}
                  onValueChange={setSelected}
                  options={hospitals.items.map((h) => ({ value: h.id, label: h.name }))}
                />
              </Field>
            </div>
          )
        }
      />
      {hospitals.isPending ? (
        <p role="status">Loading hospitals…</p>
      ) : hospitals.isError ? (
        <Failure message="Could not load hospitals." retry={hospitals.retry} />
      ) : hospital ? (
        <ReportBoard key={hospital.id} hospital={hospital} />
      ) : (
        <p className="rounded-2xl border bg-card p-8">
          Ask your tenant administrator for hospital access before viewing leave reports.
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

const reportOptions = [
  { value: "monthly", label: "Leave by month" },
  { value: "status", label: "Leave status" },
];
const statusOptions = [
  { value: "all", label: "All statuses" },
  ...Object.entries(LEAVE_REQUEST_STATUSES).map(([value, label]) => ({ value, label })),
];

function ReportBoard({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  const profiles = hospital.access_level === "manager" && can("workforce.enrollment.manage");
  const base = `/workforce/hospitals/${hospital.id}`;
  const form = useZodForm(leaveReportFilterSchema, {
    report: "monthly",
    from: `${today().slice(0, 8)}01`,
    to: today(),
    q: "",
    department_id: "",
    leave_type_id: "",
    status: "all",
  });
  const [applied, setApplied] = useState<LeaveReportFilter | null>(null);
  const types = useApiPagedQuery<LeaveConfig>(
    ["workforce", "leave-configs", hospital.id, "leave_type"],
    `${base}/hr/leave-configs?kind=leave_type`,
  );
  const departments = useApiPagedQuery<HRMaster>(
    ["workforce", "hr-masters", hospital.id, "department"],
    `${base}/hr/masters?kind=department`,
    { enabled: profiles },
  );
  const report = form.watch("report");
  const errors = form.formState.errors;
  const choose = (field: "department_id" | "leave_type_id") => (v: string) =>
    form.setValue(field, v === "all" ? "" : v, { shouldDirty: true });
  return (
    <section aria-label="Leave reports" className="space-y-6">
      <form
        noValidate
        autoComplete="off"
        aria-label="Report filters"
        onSubmit={form.handleSubmit((values: LeaveReportFilter) => setApplied(values))}
        className="space-y-5 rounded-2xl border bg-card p-5 shadow-sm"
      >
        <div className="grid gap-5 sm:grid-cols-3">
          <Field id="report-kind" label="Report">
            <SelectControl
              id="report-kind"
              value={report}
              onValueChange={(v) =>
                form.setValue("report", v as LeaveReportFilter["report"], { shouldDirty: true })
              }
              options={reportOptions}
            />
          </Field>
          <Field id="report-from" label="From" error={errors.from?.message}>
            <input
              id="report-from"
              type="date"
              className={inputClass}
              aria-invalid={!!errors.from}
              {...form.register("from")}
            />
          </Field>
          <Field id="report-to" label="To" error={errors.to?.message}>
            <input
              id="report-to"
              type="date"
              className={inputClass}
              aria-invalid={!!errors.to}
              {...form.register("to")}
            />
          </Field>
        </div>
        <div className="grid gap-5 sm:grid-cols-3">
          <Field id="report-search" label="Name or employee code" error={errors.q?.message}>
            <input
              id="report-search"
              type="search"
              className={inputClass}
              aria-invalid={!!errors.q}
              {...form.register("q")}
            />
          </Field>
          {profiles && (
            <Field id="report-department" label="Department">
              <SelectControl
                id="report-department"
                value={form.watch("department_id") || "all"}
                onValueChange={choose("department_id")}
                options={[
                  { value: "all", label: "All departments" },
                  ...departments.items.map((d) => ({ value: d.id, label: d.name })),
                ]}
              />
            </Field>
          )}
          {report === "status" && (
            <>
              <Field id="report-type" label="Leave type">
                <SelectControl
                  id="report-type"
                  value={form.watch("leave_type_id") || "all"}
                  onValueChange={choose("leave_type_id")}
                  options={[
                    { value: "all", label: "All leave types" },
                    ...types.items.map((t) => ({ value: t.id, label: `${t.name} (${t.code})` })),
                  ]}
                />
              </Field>
              <Field id="report-status" label="Status">
                <SelectControl
                  id="report-status"
                  value={form.watch("status")}
                  onValueChange={(v) =>
                    form.setValue("status", v as LeaveReportFilter["status"], { shouldDirty: true })
                  }
                  options={statusOptions}
                />
              </Field>
            </>
          )}
        </div>
        <div className="flex justify-end">
          <Button type="submit">Run report</Button>
        </div>
      </form>
      {applied ? (
        <LeaveReportTable
          key={leaveReportPath(base, applied)}
          hospital={hospital.id}
          base={base}
          filter={applied}
        />
      ) : (
        <p className="rounded-2xl border bg-card p-5 text-sm text-muted-foreground">
          Choose a period and run a report.
        </p>
      )}
    </section>
  );
}
