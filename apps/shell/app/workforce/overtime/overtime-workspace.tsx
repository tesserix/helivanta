"use client";
import { useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  formatMinutes,
  overtimePath,
  useApiPagedQuery,
  usePermissions,
  OVERTIME_STATUSES,
  type Hospital,
  type OvertimeRequest,
} from "@helivanta/api";
import { HmsShell, Field, SelectControl } from "@helivanta/ui";
import { TenantPicker } from "@/components/tenant-picker";
import { Failure } from "../roster-policy/policy-ui";
import { OvertimePolicyPanel } from "./policy-panel";
import { OvertimeForm } from "./overtime-form";
import { OvertimeReview } from "./overtime-review";
import { OvertimeSummary } from "./summary-panel";

export function OvertimeWorkspace() {
  const hospitals = useApiPagedQuery<Hospital>(
    ["workforce", "planning-hospitals"],
    "/workforce/hospitals",
  );
  const [selected, setSelected] = useState("");
  const hospital = hospitals.items.find((h) => h.id === selected) ?? hospitals.items[0];
  return (
    <HmsShell active="/workforce/overtime" tenantPicker={<TenantPicker />}>
      <div className="mb-8 flex flex-wrap items-end justify-between gap-6">
        <div>
          <p className="mb-2 text-xs font-semibold uppercase tracking-[0.18em] text-(--hms-accent)">
            People & operations
          </p>
          <h2 className="text-3xl font-semibold tracking-tight">Overtime</h2>
          <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
            Record overtime outside rostered duty within the hospital&apos;s reviewed limits, then
            have a second person approve it. Only approved minutes are payable.
          </p>
        </div>
        {hospital && (
          <div className="w-full sm:w-72">
            <Field id="overtime-hospital" label="Hospital">
              <SelectControl
                id="overtime-hospital"
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
        <HospitalOvertime key={hospital.id} hospital={hospital} />
      ) : (
        <p className="rounded-2xl border bg-card p-8">
          Ask your tenant administrator for hospital access before managing overtime.
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

function HospitalOvertime({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  const manager = hospital.access_level === "manager";
  return (
    <div className="space-y-6">
      <OvertimePolicyPanel
        hospital={hospital}
        writable={manager && can("workforce.overtime.configure")}
      />
      <RequestBoard
        hospital={hospital}
        filer={manager && can("workforce.overtime.request")}
        approver={manager && can("workforce.overtime.approve")}
      />
      <OvertimeSummary hospital={hospital} />
    </div>
  );
}

const statusOptions = [
  { value: "all", label: "All statuses" },
  ...Object.entries(OVERTIME_STATUSES).map(([value, label]) => ({ value, label })),
];

export const describeOvertime = (r: OvertimeRequest) =>
  `${r.work_date} · ${r.start_time}–${r.end_time} · ${formatMinutes(r.requested_minutes)}`;

function RequestBoard({
  hospital,
  filer,
  approver,
}: {
  hospital: Hospital;
  filer: boolean;
  approver: boolean;
}) {
  const [status, setStatus] = useState("pending");
  const [filing, setFiling] = useState(false);
  const [open, setOpen] = useState<OvertimeRequest | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const base = `/workforce/hospitals/${hospital.id}`;
  const requests = useApiPagedQuery<OvertimeRequest>(
    ["workforce", "overtime", hospital.id, status],
    overtimePath(base, { status }),
  );
  const close = () => {
    setOpen(null);
    opener.current?.focus();
  };
  return (
    <section
      aria-label="Overtime requests"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-end justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Requests</h3>
          <p className="mt-1 text-xs text-muted-foreground">Newest filings first.</p>
        </div>
        <div className="flex flex-wrap items-end gap-3">
          <div className="w-48">
            <Field id="overtime-status" label="Status">
              <SelectControl
                id="overtime-status"
                value={status}
                onValueChange={(v) => {
                  setStatus(v);
                  setOpen(null);
                }}
                options={statusOptions}
              />
            </Field>
          </div>
          {filer && !filing && <Button onClick={() => setFiling(true)}>Record overtime</Button>}
        </div>
      </div>
      {filing && (
        <OvertimeForm hospital={hospital.id} base={base} onClose={() => setFiling(false)} />
      )}
      {requests.isPending ? (
        <p role="status" className="p-5">
          Loading overtime…
        </p>
      ) : requests.isError ? (
        <div className="p-5">
          <Failure message="Could not load overtime requests." retry={requests.retry} />
        </div>
      ) : requests.items.length === 0 ? (
        <p className="p-5 text-sm text-muted-foreground">
          {status === "all"
            ? "No overtime has been recorded in this hospital."
            : `No ${OVERTIME_STATUSES[status as keyof typeof OVERTIME_STATUSES].toLowerCase()} overtime.`}
        </p>
      ) : (
        <ul className="divide-y">
          {requests.items.map((r) => (
            <li key={r.id} className="flex flex-wrap items-start justify-between gap-4 p-5">
              <div>
                <p className="font-medium">
                  {r.display_name} · {r.employee_code}
                </p>
                <p className="mt-1 text-sm">{describeOvertime(r)}</p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {OVERTIME_STATUSES[r.status]}
                  {r.approved_minutes !== undefined
                    ? ` · ${formatMinutes(r.approved_minutes)} approved`
                    : ""}{" "}
                  · filed by {r.requested_by_subject}
                  {r.decided_by_subject ? ` · decided by ${r.decided_by_subject}` : ""}
                </p>
              </div>
              <Button
                variant="outline"
                aria-label={`Review overtime for ${r.display_name} on ${r.work_date}`}
                onClick={(ev) => {
                  opener.current = ev.currentTarget;
                  setOpen(r);
                }}
              >
                Review
              </Button>
            </li>
          ))}
        </ul>
      )}
      {requests.hasMore && (
        <div className="border-t p-5">
          <Button variant="outline" disabled={requests.isFetchingMore} onClick={requests.loadMore}>
            Load more overtime
          </Button>
        </div>
      )}
      {open && (
        <OvertimeReview
          key={open.id}
          hospital={hospital.id}
          base={base}
          request={open}
          filer={filer}
          approver={approver}
          onClose={close}
        />
      )}
    </section>
  );
}
