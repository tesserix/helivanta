"use client";

import Link from "next/link";
import {
  pharmacyKey,
  pharmacyRoot,
  useApiPageQuery,
  useApiQuery,
  type PharmacyOutlet,
  type PharmacyScope,
} from "@helivanta/api";
import { Panel, RequestFailure, formatDateTime, formatTime } from "@helivanta/ui";

type OutletOverview = {
  as_of: string;
  time_zone: string;
  stock?: { expired: number; due_30: number; due_90: number };
  safety?: { recent: number };
  bills?: { overdue: number; week: number };
  /** Absent where the outlet's country has no tax scheme: no gap is real there. */
  setup?: { unclassified: number; registration_in_force?: boolean };
};

type Waiting = { id: string; created_at: string };

// A wait that began today reads as a time; an older one needs its date.
function sinceLabel(iso: string) {
  return new Date(iso).toDateString() === new Date().toDateString()
    ? formatTime(iso)
    : formatDateTime(iso);
}

// One count and the list it opens. The link carries the same filter the
// server counted with (#576 D3/D4), so the number and the rows agree.
function Count({ value, label, href }: { value: number; label: string; href: string }) {
  return (
    <Link
      href={href}
      className="flex items-baseline justify-between gap-3 rounded-md px-2 py-1.5 transition-colors hover:bg-accent"
    >
      <span className="text-sm text-muted-foreground">{label}</span>
      <span
        className={`text-xl font-semibold tabular-nums ${value > 0 ? "text-foreground" : "text-muted-foreground"}`}
      >
        {value}
      </span>
    </Link>
  );
}

function Tile({
  title,
  note,
  children,
}: {
  title: string;
  note?: string;
  children: React.ReactNode;
}) {
  return (
    <Panel aria-label={title} className="flex flex-col gap-2">
      <div>
        <h3 className="font-semibold">{title}</h3>
        {note && <p className="text-xs text-muted-foreground">{note}</p>}
      </div>
      <div className="-mx-2 flex flex-col">{children}</div>
    </Panel>
  );
}

/**
 * What needs attention, most urgent first (#576). Every number comes from
 * the server; a tile the person may not see is absent, never "0", and a
 * failure says so rather than showing zeros (D5).
 */
export function OverviewTiles({ scope, outlet }: { scope: PharmacyScope; outlet: PharmacyOutlet }) {
  const overview = useApiQuery<OutletOverview>(
    pharmacyKey(scope, "overview"),
    `${pharmacyRoot(scope)}/overview`,
    { poll: true },
  );
  const data = overview.data;
  const asOf = data
    ? `As of ${new Intl.DateTimeFormat(undefined, { timeStyle: "short", timeZone: data.time_zone }).format(new Date(data.as_of))} at ${outlet.name} (${data.time_zone})`
    : undefined;

  return (
    <div className="space-y-3">
      {asOf && <p className="text-xs text-muted-foreground">{asOf}.</p>}
      <div data-testid="overview-tiles" className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {overview.isError ? (
          <div className="sm:col-span-2 xl:col-span-2">
            <RequestFailure
              message="Could not load this outlet's counts."
              retry={overview.refetch}
            />
          </div>
        ) : overview.isPending ? (
          <p role="status" className="text-sm text-muted-foreground">
            Loading this outlet's counts…
          </p>
        ) : (
          data && (
            <>
              {data.stock && (
                <Tile title="Stock expiring" note="Stock lines with stock on hand">
                  <Count
                    value={data.stock.expired}
                    label="Expired"
                    href="/pharmacy/stock?balance=on_hand&expiry=expired"
                  />
                  <Count
                    value={data.stock.due_30}
                    label="Within 30 days"
                    href="/pharmacy/stock?balance=on_hand&expiry=due_30"
                  />
                  <Count
                    value={data.stock.due_90}
                    label="Within 90 days"
                    href="/pharmacy/stock?balance=on_hand&expiry=due_90"
                  />
                </Tile>
              )}
              {data.safety && (
                <Tile title="Safety concerns">
                  <Count
                    value={data.safety.recent}
                    label="Recorded in the last 30 days"
                    href="/pharmacy/safety?recorded=30d"
                  />
                </Tile>
              )}
              {data.bills && (
                <Tile title="Supplier bills" note="Bills with an amount outstanding">
                  <Count
                    value={data.bills.overdue}
                    label="Overdue"
                    href="/pharmacy/accounts?due=overdue"
                  />
                  <Count
                    value={data.bills.week}
                    label="Due within 7 days"
                    href="/pharmacy/accounts?due=week"
                  />
                </Tile>
              )}
              {data.setup && (
                <Tile title="Setup gaps">
                  {data.setup!.unclassified > 0 ? (
                    <Count
                      value={data.setup!.unclassified}
                      label="Not classified for tax"
                      href="/pharmacy/catalogue?tax=unclassified"
                    />
                  ) : (
                    <p className="px-2 py-1.5 text-sm text-muted-foreground">
                      Every product has a tax rate in force.
                    </p>
                  )}
                  {data.setup!.registration_in_force !== undefined && (
                    <Link
                      href="/pharmacy/setup"
                      className="rounded-md px-2 py-1.5 text-sm transition-colors hover:bg-accent"
                    >
                      {data.setup!.registration_in_force
                        ? "GSTIN in force"
                        : "No GSTIN in force: record the outlet's registration"}
                    </Link>
                  )}
                </Tile>
              )}
            </>
          )
        )}
      </div>
    </div>
  );
}

// Hospital-wide: dispenses are not outlet-scoped. The counted queue's total
// is the number; its first row, oldest first, is the longest wait.
export function WaitingTile() {
  const waiting = useApiPageQuery<Waiting>(
    ["dispenses", "waiting"],
    "/pharmacy/dispenses/waiting",
    null,
    {
      poll: true,
      limit: 1,
    },
  );
  const oldest = waiting.items[0];
  return (
    <Tile title="Patients waiting" note="Hospital-wide dispense queue">
      {waiting.isError ? (
        <RequestFailure message="Could not load the dispense queue." retry={waiting.retry} />
      ) : waiting.isPending ? (
        <p role="status" className="px-2 text-sm text-muted-foreground">
          Loading the queue…
        </p>
      ) : (
        <>
          <Count
            value={waiting.total ?? 0}
            label="Waiting now"
            href="/pharmacy/dispense?tab=waiting"
          />
          <p className="px-2 text-xs text-muted-foreground">
            {oldest
              ? `Oldest waiting since ${sinceLabel(oldest.created_at)}`
              : "No patients waiting"}
          </p>
        </>
      )}
    </Tile>
  );
}
