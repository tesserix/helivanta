"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  useApiPagedQuery,
  usePermissions,
  ROSTER_CONFIG_KINDS,
  type Hospital,
  type RosterConfig,
} from "@helivanta/api";
import { Field, PageHeading, SelectControl, useConfirmLeave } from "@helivanta/ui";
import { RosterEditor } from "./roster-editor";
import { ConfigurationSummary, effectiveDate } from "./roster-summary";

import { inputClass, configNoun } from "./roster-ui";

export function RosterWorkspace() {
  const confirmLeave = useConfirmLeave();
  const hospitals = useApiPagedQuery<Hospital>(
    ["workforce", "planning-hospitals"],
    "/workforce/hospitals",
  );
  const [selected, setSelected] = useState("");
  const hospital = hospitals.items.find((h) => h.id === selected) ?? hospitals.items[0];
  const [kind, setKind] = useState<RosterConfig["kind"]>("area");
  return (
    <>
      <PageHeading
        description="Organize areas, shift groups and weekly patterns. Earlier scheduling rules stay intact."
        actions={
          <>
            <div className="grid w-full gap-4 sm:w-auto sm:grid-cols-2">
              {hospital && (
                <Field id="planning-hospital" label="Hospital">
                  <SelectControl
                    id="planning-hospital"
                    value={hospital.id}
                    onValueChange={(next) => confirmLeave(() => setSelected(next))}
                    options={hospitals.items.map((h) => ({ value: h.id, label: h.name }))}
                  />
                </Field>
              )}
              <Field id="planning-kind" label="Configuration">
                <SelectControl
                  id="planning-kind"
                  value={kind}
                  onValueChange={(v) => confirmLeave(() => setKind(v as RosterConfig["kind"]))}
                  options={Object.entries(ROSTER_CONFIG_KINDS).map(([value, label]) => ({
                    value,
                    label,
                  }))}
                />
              </Field>
            </div>
          </>
        }
      />
      {hospitals.isPending ? (
        <p role="status">Loading hospitals…</p>
      ) : hospitals.isError ? (
        <Failure message="Could not load hospitals." retry={hospitals.retry} />
      ) : hospital ? (
        <ConfigCatalog key={`${hospital.id}:${kind}`} hospital={hospital} kind={kind} />
      ) : (
        <p className="rounded-2xl border bg-card p-8">
          Ask your tenant administrator for hospital access before managing roster setup.
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
function Failure({ message, retry }: { message: string; retry: () => void }) {
  return (
    <div role="alert" className="rounded-xl border p-6 text-sm">
      {message}{" "}
      <Button variant="outline" onClick={retry}>
        Try again
      </Button>
    </div>
  );
}
function ConfigCatalog({ hospital, kind }: { hospital: Hospital; kind: RosterConfig["kind"] }) {
  const confirmLeave = useConfirmLeave();
  const { can } = usePermissions();
  const writable = can("workforce.roster.manage") && hospital.access_level === "manager";
  const path = `/workforce/hospitals/${hospital.id}/hr/roster-configs`;
  const records = useApiPagedQuery<RosterConfig>(
    ["workforce", "roster-configs", hospital.id, kind],
    `${path}?kind=${kind}`,
  );
  const [filter, setFilter] = useState("");
  const [editing, setEditing] = useState<RosterConfig | "new" | null>(null);
  const [history, setHistory] = useState<RosterConfig | null>(null);
  const [editorSession, setEditorSession] = useState(0);
  const opener = useRef<HTMLButtonElement | null>(null);
  const close = () => {
    setEditing(null);
    setHistory(null);
  };
  useEffect(() => {
    if (!editing && !history) opener.current?.focus();
  }, [editing, history]);
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
              {ROSTER_CONFIG_KINDS[kind]} · {writable ? "Manager access" : "Read-only access"}
            </p>
          </div>
          {writable && (
            <Button
              onClick={(e) => {
                const button = e.currentTarget;
                confirmLeave(() => {
                  opener.current = button;
                  setHistory(null);
                  setEditorSession((value) => value + 1);
                  setEditing("new");
                });
              }}
            >
              Add {configNoun[kind]}
            </Button>
          )}
        </div>
        <div className="border-b p-5">
          <Field id="config-search" label="Filter loaded configurations">
            <input
              id="config-search"
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
                : `No ${ROSTER_CONFIG_KINDS[kind].toLowerCase()} yet`}
            </h4>
            <p className="mt-2 text-sm text-muted-foreground">
              {filter
                ? "Try another name or code."
                : "Define your scheduling pattern and review its effective date before using it in a roster."}
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
                      {r.code} · Effective {effectiveDate(r.effective_on)} · Revision {r.version}
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
                          const button = e.currentTarget;
                          confirmLeave(() => {
                            opener.current = button;
                            setHistory(null);
                            setEditorSession((value) => value + 1);
                            setEditing(r);
                          });
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
                        const button = e.currentTarget;
                        confirmLeave(() => {
                          opener.current = button;
                          setEditing(null);
                          setHistory(r);
                        });
                      }}
                    >
                      History
                    </Button>
                  </div>
                </div>
                <ConfigurationSummary record={r} />
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
        <RosterEditor
          key={`${editing === "new" ? "new" : editing.id}:${editorSession}`}
          path={path}
          hospital={hospital}
          kind={kind}
          record={editing === "new" ? undefined : editing}
          onClose={close}
        />
      )}
      {history && (
        <ConfigHistory
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
function ConfigHistory({
  path,
  hospital,
  record,
  onClose,
}: {
  path: string;
  hospital: string;
  record: RosterConfig;
  onClose: () => void;
}) {
  const history = useApiPagedQuery<RosterConfig>(
    ["workforce", "roster-config-history", hospital, record.id],
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
      aria-label="Roster configuration history"
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
              <ConfigurationSummary record={r} />
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
