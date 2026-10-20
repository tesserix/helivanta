"use client";
import { useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  formatHalfDays,
  useApiPagedQuery,
  usePermissions,
  LEAVE_REQUEST_STATUSES,
  LEAVE_SESSIONS,
  LEAVE_UNITS,
  type Hospital,
  type LeaveRequest,
} from "@helivanta/api";
import { Field, SelectControl } from "@helivanta/ui";
import { Failure } from "../roster-policy/policy-ui";
import { LeaveRequestForm } from "./request-form";
import { LeaveRequestReview } from "./request-review";

export function LeaveRequestsWorkspace() {
  const hospitals = useApiPagedQuery<Hospital>(
    ["workforce", "planning-hospitals"],
    "/workforce/hospitals",
  );
  const [selected, setSelected] = useState("");
  const hospital = hospitals.items.find((h) => h.id === selected) ?? hospitals.items[0];
  return (
    <>
      <div className="mb-8 flex flex-wrap items-end justify-between gap-6">
        <div>
          <p className="mb-2 text-xs font-semibold uppercase tracking-[0.18em] text-(--hms-accent)">
            People & operations
          </p>
          <h2 className="text-3xl font-semibold tracking-tight">Leave requests</h2>
          <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
            File leave for staff, then have a second person approve it against the balance and the
            published roster. Approval and cancellation post to the leave ledger.
          </p>
        </div>
        {hospital && (
          <div className="w-full sm:w-72">
            <Field id="requests-hospital" label="Hospital">
              <SelectControl
                id="requests-hospital"
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
        <RequestBoard key={hospital.id} hospital={hospital} />
      ) : (
        <p className="rounded-2xl border bg-card p-8">
          Ask your tenant administrator for hospital access before managing leave requests.
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

const statusOptions = [
  { value: "all", label: "All statuses" },
  ...Object.entries(LEAVE_REQUEST_STATUSES).map(([value, label]) => ({ value, label })),
];

export const describeLeave = (r: LeaveRequest) =>
  r.unit === "full_day"
    ? `${r.start_on === r.end_on ? r.start_on : `${r.start_on} to ${r.end_on}`} · ${formatHalfDays(r.half_days)}`
    : r.unit === "half_day"
      ? `${r.start_on} · ${LEAVE_SESSIONS[r.session ?? "first_half"]}`
      : `${r.start_on} · ${LEAVE_UNITS.short_leave} for ${r.minutes} minutes`;

function RequestBoard({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  const manager = hospital.access_level === "manager";
  const filer = manager && can("workforce.leave.request");
  const approver = manager && can("workforce.leave.approve");
  const [status, setStatus] = useState("pending");
  const [filing, setFiling] = useState(false);
  const [open, setOpen] = useState<LeaveRequest | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const base = `/workforce/hospitals/${hospital.id}`;
  const requests = useApiPagedQuery<LeaveRequest>(
    ["workforce", "leave-requests", hospital.id, status],
    `${base}/hr/leave-requests${status === "all" ? "" : `?status=${status}`}`,
  );
  const close = () => {
    setOpen(null);
    opener.current?.focus();
  };
  return (
    <section
      aria-label="Leave requests"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-end justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Requests</h3>
          <p className="mt-1 text-xs text-muted-foreground">Newest filings first.</p>
        </div>
        <div className="flex flex-wrap items-end gap-3">
          <div className="w-48">
            <Field id="requests-status" label="Status">
              <SelectControl
                id="requests-status"
                value={status}
                onValueChange={(v) => {
                  setStatus(v);
                  setOpen(null);
                }}
                options={statusOptions}
              />
            </Field>
          </div>
          {filer && !filing && <Button onClick={() => setFiling(true)}>File leave request</Button>}
        </div>
      </div>
      {filing && (
        <LeaveRequestForm hospital={hospital.id} base={base} onClose={() => setFiling(false)} />
      )}
      {requests.isPending ? (
        <p role="status" className="p-5">
          Loading leave requests…
        </p>
      ) : requests.isError ? (
        <div className="p-5">
          <Failure message="Could not load leave requests." retry={requests.retry} />
        </div>
      ) : requests.items.length === 0 ? (
        <p className="p-5 text-sm text-muted-foreground">
          {status === "all"
            ? "No leave requests have been filed in this hospital."
            : `No ${LEAVE_REQUEST_STATUSES[status as keyof typeof LEAVE_REQUEST_STATUSES].toLowerCase()} leave requests.`}
        </p>
      ) : (
        <ul className="divide-y">
          {requests.items.map((r) => (
            <li key={r.id} className="flex flex-wrap items-start justify-between gap-4 p-5">
              <div>
                <p className="font-medium">
                  {r.display_name} · {r.employee_code}
                </p>
                <p className="mt-1 text-sm">
                  {r.name} ({r.code}) · {describeLeave(r)}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {LEAVE_REQUEST_STATUSES[r.status]} · filed by {r.requested_by_subject}
                  {r.decided_by_subject ? ` · decided by ${r.decided_by_subject}` : ""}
                </p>
              </div>
              <Button
                variant="outline"
                aria-label={`Review leave for ${r.display_name} from ${r.start_on}`}
                onClick={(e) => {
                  opener.current = e.currentTarget;
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
            Load more requests
          </Button>
        </div>
      )}
      {open && (
        <LeaveRequestReview
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
