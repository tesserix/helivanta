"use client";
import { useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  dutyExchangesPath,
  useApiPagedQuery,
  usePermissions,
  DUTY_EXCHANGE_KINDS,
  DUTY_EXCHANGE_STATUSES,
  type DutyAllocation,
  type DutyExchange,
  type Hospital,
} from "@helivanta/api";
import { Field, SelectControl } from "@helivanta/ui";
import { Failure } from "../roster-policy/policy-ui";
import { DutyExchangeForm } from "./exchange-form";
import { DutyExchangeReview } from "./exchange-review";

export function DutyExchangesWorkspace() {
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
          <p className="mb-2 text-xs font-semibold text-(--hms-accent)">People & operations</p>
          <h2 className="text-3xl font-semibold tracking-tight">Duty exchanges</h2>
          <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
            Record agreed swaps and covers on published rosters, then have a second person approve
            them. Approval moves the duty without reopening the roster.
          </p>
        </div>
        {hospital && (
          <div className="w-full sm:w-72">
            <Field id="exchanges-hospital" label="Hospital">
              <SelectControl
                id="exchanges-hospital"
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
        <ExchangeBoard key={hospital.id} hospital={hospital} />
      ) : (
        <p className="rounded-2xl border bg-card p-8">
          Ask your tenant administrator for hospital access before managing duty exchanges.
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
  ...Object.entries(DUTY_EXCHANGE_STATUSES).map(([value, label]) => ({ value, label })),
];

const time = (iso: string) => new Date(iso).toLocaleTimeString([], { timeStyle: "short" });
export const describeDuty = (a: DutyAllocation) =>
  `${a.roster_date} · ${a.shift_code} ${time(a.starts_at)}–${time(a.ends_at)} · ${a.area_code}`;
export const describeDates = (e: DutyExchange) =>
  e.duty_from === e.duty_to ? e.duty_from : `${e.duty_from} to ${e.duty_to}`;

function ExchangeBoard({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  const manager = hospital.access_level === "manager";
  const filer = manager && can("workforce.duty.request");
  const approver = manager && can("workforce.duty.approve");
  const [status, setStatus] = useState("pending");
  const [filing, setFiling] = useState(false);
  const [open, setOpen] = useState<DutyExchange | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const base = `/workforce/hospitals/${hospital.id}`;
  const exchanges = useApiPagedQuery<DutyExchange>(
    ["workforce", "duty-exchanges", hospital.id, status],
    dutyExchangesPath(base, { status }),
  );
  const close = () => {
    setOpen(null);
    opener.current?.focus();
  };
  return (
    <section
      aria-label="Duty exchanges"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-end justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Exchanges</h3>
          <p className="mt-1 text-xs text-muted-foreground">Newest filings first.</p>
        </div>
        <div className="flex flex-wrap items-end gap-3">
          <div className="w-48">
            <Field id="exchanges-status" label="Status">
              <SelectControl
                id="exchanges-status"
                value={status}
                onValueChange={(v) => {
                  setStatus(v);
                  setOpen(null);
                }}
                options={statusOptions}
              />
            </Field>
          </div>
          {filer && !filing && <Button onClick={() => setFiling(true)}>File duty exchange</Button>}
        </div>
      </div>
      {filing && (
        <DutyExchangeForm hospital={hospital.id} base={base} onClose={() => setFiling(false)} />
      )}
      {exchanges.isPending ? (
        <p role="status" className="p-5">
          Loading duty exchanges…
        </p>
      ) : exchanges.isError ? (
        <div className="p-5">
          <Failure message="Could not load duty exchanges." retry={exchanges.retry} />
        </div>
      ) : exchanges.items.length === 0 ? (
        <p className="p-5 text-sm text-muted-foreground">
          {status === "all"
            ? "No duty exchanges have been filed in this hospital."
            : `No ${DUTY_EXCHANGE_STATUSES[status as keyof typeof DUTY_EXCHANGE_STATUSES].toLowerCase()} duty exchanges.`}
        </p>
      ) : (
        <ul className="divide-y">
          {exchanges.items.map((e) => (
            <li key={e.id} className="flex flex-wrap items-start justify-between gap-4 p-5">
              <div>
                <p className="font-medium">
                  {e.requester_display_name} → {e.counterpart_display_name}
                </p>
                <p className="mt-1 text-sm">
                  {DUTY_EXCHANGE_KINDS[e.kind]} · {describeDates(e)}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {DUTY_EXCHANGE_STATUSES[e.status]} · filed by {e.requested_by_subject}
                  {e.decided_by_subject ? ` · decided by ${e.decided_by_subject}` : ""}
                </p>
              </div>
              <Button
                variant="outline"
                aria-label={`Review exchange for ${e.requester_display_name} on ${describeDates(e)}`}
                onClick={(ev) => {
                  opener.current = ev.currentTarget;
                  setOpen(e);
                }}
              >
                Review
              </Button>
            </li>
          ))}
        </ul>
      )}
      {exchanges.hasMore && (
        <div className="border-t p-5">
          <Button
            variant="outline"
            disabled={exchanges.isFetchingMore}
            onClick={exchanges.loadMore}
          >
            Load more exchanges
          </Button>
        </div>
      )}
      {open && (
        <DutyExchangeReview
          key={open.id}
          hospital={hospital.id}
          base={base}
          exchange={open}
          filer={filer}
          approver={approver}
          onClose={close}
        />
      )}
    </section>
  );
}
