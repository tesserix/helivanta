"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  useApiPagedQuery,
  usePermissions,
  LEAVE_CONFIG_KINDS,
  type Hospital,
  type LeaveConfig,
} from "@helivanta/api";
import { Field, SelectControl } from "@helivanta/ui";
import { Failure, inputClass } from "../roster-policy/policy-ui";
import { effectiveDate } from "../roster-setup/roster-summary";
import { LeaveEditor } from "./leave-editor";
import { LeaveSummary, configNoun } from "./leave-summary";

const kindLabels = {
  leave_type: "Leave types",
  holiday_calendar: "Holiday calendars",
  structure: "Leave structures",
} satisfies Record<LeaveConfig["kind"], string>;

export function LeaveWorkspace() {
  const hospitals = useApiPagedQuery<Hospital>(
    ["workforce", "planning-hospitals"],
    "/workforce/hospitals",
  );
  const [selected, setSelected] = useState("");
  const hospital = hospitals.items.find((h) => h.id === selected) ?? hospitals.items[0];
  const [kind, setKind] = useState<LeaveConfig["kind"]>("leave_type");
  return (
    <>
      <div className="mb-8 flex flex-wrap items-end justify-between gap-6">
        <div>
          <p className="mb-2 text-xs font-semibold uppercase tracking-[0.18em] text-(--hms-accent)">
            People & operations
          </p>
          <h2 className="text-3xl font-semibold tracking-tight">Leave setup</h2>
          <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
            Define leave types, yearly holiday calendars and the allowances each staff structure
            receives. Earlier revisions stay intact.
          </p>
        </div>
        <div className="grid w-full gap-4 sm:w-auto sm:grid-cols-2">
          {hospital && (
            <Field id="leave-hospital" label="Hospital">
              <SelectControl
                id="leave-hospital"
                value={hospital.id}
                onValueChange={setSelected}
                options={hospitals.items.map((h) => ({ value: h.id, label: h.name }))}
              />
            </Field>
          )}
          <Field id="leave-kind" label="Configuration">
            <SelectControl
              id="leave-kind"
              value={kind}
              onValueChange={(v) => setKind(v as LeaveConfig["kind"])}
              options={Object.entries(kindLabels).map(([value, label]) => ({ value, label }))}
            />
          </Field>
        </div>
      </div>
      {hospitals.isPending ? (
        <p role="status">Loading hospitals…</p>
      ) : hospitals.isError ? (
        <Failure message="Could not load hospitals." retry={hospitals.retry} />
      ) : hospital ? (
        <LeaveCatalog key={`${hospital.id}:${kind}`} hospital={hospital} kind={kind} />
      ) : (
        <p className="rounded-2xl border bg-card p-8">
          Ask your tenant administrator for hospital access before managing leave setup.
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

function LeaveCatalog({ hospital, kind }: { hospital: Hospital; kind: LeaveConfig["kind"] }) {
  const { can } = usePermissions();
  const writable = can("workforce.leave.configure") && hospital.access_level === "manager";
  const path = `/workforce/hospitals/${hospital.id}/hr/leave-configs`;
  const records = useApiPagedQuery<LeaveConfig>(
    ["workforce", "leave-configs", hospital.id, kind],
    `${path}?kind=${kind}`,
  );
  const [filter, setFilter] = useState("");
  const [editing, setEditing] = useState<LeaveConfig | "new" | null>(null);
  const [history, setHistory] = useState<LeaveConfig | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const close = () => {
    setEditing(null);
    setHistory(null);
    opener.current?.focus();
  };
  const rows = records.items.filter((r) =>
    `${r.name} ${r.code}`.toLowerCase().includes(filter.toLowerCase()),
  );
  return (
    <>
      <section className="overflow-hidden rounded-2xl border bg-card shadow-sm">
        <div className="flex flex-wrap items-center justify-between gap-4 border-b p-5">
          <div>
            <h3 className="font-semibold">{hospital.name}</h3>
            <p className="mt-1 text-xs text-muted-foreground">
              {LEAVE_CONFIG_KINDS[kind]} · {writable ? "Manager access" : "Read-only access"}
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
              Add {configNoun[kind]}
            </Button>
          )}
        </div>
        <div className="border-b p-5">
          <Field id="leave-search" label="Filter loaded configurations">
            <input
              id="leave-search"
              className={inputClass}
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Search name or code"
            />
          </Field>
        </div>
        {records.isPending ? (
          <p className="p-8" role="status">
            Loading configurations…
          </p>
        ) : records.isError ? (
          <Failure message="Could not load configurations." retry={records.retry} />
        ) : !rows.length ? (
          <div className="p-12 text-center">
            <h4 className="font-semibold">
              {filter
                ? "No matching loaded configurations"
                : `No ${kindLabels[kind].toLowerCase()} yet`}
            </h4>
            <p className="mt-2 text-sm text-muted-foreground">
              {filter
                ? "Try another name or code."
                : "Review the rules and effective date before staff leave depends on them."}
            </p>
          </div>
        ) : (
          <ul className="divide-y">
            {rows.map((r) => (
              <li key={r.id} className="p-5">
                <div className="mb-4 flex flex-wrap items-start justify-between gap-4">
                  <div>
                    <p className="font-semibold">{r.name}</p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {r.code}
                      {r.calendar_year ? ` · ${r.calendar_year}` : ""} · Effective{" "}
                      {effectiveDate(r.effective_on)} · Revision {r.version}
                    </p>
                    {r.description && (
                      <p className="mt-2 max-w-3xl whitespace-pre-wrap text-sm text-muted-foreground">
                        {r.description}
                      </p>
                    )}
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="rounded-full bg-muted px-2.5 py-1 text-xs">
                      {r.active ? "Active" : "Retired"}
                    </span>
                    {writable && (
                      <Button
                        size="sm"
                        variant="ghost"
                        aria-label={`Edit ${r.name}`}
                        onClick={(e) => {
                          opener.current = e.currentTarget;
                          setHistory(null);
                          setEditing(r);
                        }}
                      >
                        Edit
                      </Button>
                    )}
                    <Button
                      size="sm"
                      variant="ghost"
                      aria-label={`History for ${r.name}`}
                      onClick={(e) => {
                        opener.current = e.currentTarget;
                        setEditing(null);
                        setHistory(r);
                      }}
                    >
                      History
                    </Button>
                  </div>
                </div>
                <LeaveSummary record={r} />
              </li>
            ))}
          </ul>
        )}
        <div className="flex flex-wrap items-center justify-between gap-4 border-t p-5 text-xs text-muted-foreground">
          <span>{records.items.length} loaded configurations · Hospital-specific rules</span>
          <div className="flex gap-3">
            {records.hasMore && (
              <Button
                size="sm"
                variant="outline"
                disabled={records.isFetchingMore}
                onClick={records.loadMore}
              >
                Load more configurations
              </Button>
            )}
            <Button size="sm" variant="outline" onClick={records.retry}>
              Refresh configurations
            </Button>
          </div>
        </div>
      </section>
      {editing && (
        <LeaveEditor
          key={editing === "new" ? "new" : editing.id}
          path={path}
          hospital={hospital}
          kind={kind}
          record={editing === "new" ? undefined : editing}
          onClose={close}
        />
      )}
      {history && (
        <LeaveHistory
          key={history.id}
          path={`${path}/${history.id}/history`}
          hospital={hospital.id}
          record={history}
          onClose={close}
        />
      )}
    </>
  );
}

function LeaveHistory({
  path,
  hospital,
  record,
  onClose,
}: {
  path: string;
  hospital: string;
  record: LeaveConfig;
  onClose: () => void;
}) {
  const history = useApiPagedQuery<LeaveConfig>(
    ["workforce", "leave-config-history", hospital, record.id],
    path,
  );
  const section = useRef<HTMLElement>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    section.current?.scrollIntoView?.({ block: "start", behavior: "instant" });
    heading.current?.focus();
  }, []);
  return (
    <section
      ref={section}
      className="mt-6 scroll-mt-24 rounded-2xl border bg-card p-6 shadow-sm"
      aria-label="Leave configuration history"
    >
      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div>
          <h3 ref={heading} tabIndex={-1} className="text-xl font-semibold outline-none">
            Revision history
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">
            {record.name} · {record.code}
          </p>
        </div>
        <Button variant="ghost" onClick={onClose}>
          Close history
        </Button>
      </div>
      {history.isPending ? (
        <p role="status">Loading configuration history…</p>
      ) : history.isError ? (
        <Failure message="Could not load configuration history." retry={history.retry} />
      ) : (
        <ul className="space-y-4">
          {history.items.map((r) => (
            <li key={r.id} className="rounded-xl border p-4">
              <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
                <p className="font-semibold">{r.name}</p>
                <p className="text-xs text-muted-foreground">
                  Revision {r.version} · {r.active ? "Active" : "Retired"} · Effective{" "}
                  {effectiveDate(r.effective_on)}
                </p>
              </div>
              <LeaveSummary record={r} />
              <p className="mt-3 text-sm text-muted-foreground">{r.description}</p>
              <p className="mt-3 text-xs text-muted-foreground">
                Reviewed by an authorized hospital manager ·{" "}
                {new Date(r.created_at).toLocaleString()}
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
          Load earlier revisions
        </Button>
      )}
    </section>
  );
}
