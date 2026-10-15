"use client";
import { useEffect, useMemo, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  ROSTER_AUTO_POPULATION,
  ROSTER_MODES,
  ROSTER_STATUS,
  rosterDates,
  rosterPopulateSchema,
  rosterStatusSchema,
  useApiPagedQuery,
  useApiQuery,
  type DatedAllocation,
  type Hospital,
  type Roster,
  type RosterPopulateInput,
  type RosterRevision,
  type RosterStatusInput,
  type StaffRosterAssignment,
} from "@helivanta/api";
import { ConfirmDialog, Field, useZodForm } from "@helivanta/ui";
import { Failure, inputClass } from "../roster-policy/policy-ui";
import { ChangeEditor } from "./roster-change";
import { ReviewCheck, SaveError, useRosterWrite } from "./roster-ui";

type Panel = "change" | "populate" | "freeze" | "unfreeze" | "discard" | "history" | null;

export function RosterDetail({
  base,
  hospital,
  rosterID,
  writable,
  correctable,
  onClose,
}: {
  base: string;
  hospital: Hospital;
  rosterID: string;
  writable: boolean;
  correctable: boolean;
  onClose: () => void;
}) {
  const path = `${base}/rosters/${rosterID}`;
  const roster = useApiQuery<Roster>(["workforce", "rosters", hospital.id, rosterID], path);
  const [panel, setPanel] = useState<Panel>(null);
  const [asOf, setAsOf] = useState<number | null>(null);
  const [removals, setRemovals] = useState<Set<string>>(new Set());
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  const current = roster.data;
  const draft = current?.status === "draft";
  const show = (next: Panel) => {
    setRemovals(new Set());
    setPanel(next);
  };
  return (
    <section
      aria-label="Roster detail"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-center justify-between gap-4 border-b p-5">
        <div>
          <h3 ref={heading} tabIndex={-1} className="font-semibold outline-none">
            {current ? `${current.name} · ${ROSTER_STATUS[current.status]}` : "Roster"}
          </h3>
          {current && (
            <p className="mt-1 text-xs text-muted-foreground">
              {current.start_date} to {current.end_date} · {current.timezone} ·{" "}
              {ROSTER_AUTO_POPULATION[current.auto_population]} · v{current.version}
            </p>
          )}
        </div>
        <div className="flex flex-wrap gap-2">
          {current && (
            <Button variant="outline" onClick={() => show("history")}>
              Roster history
            </Button>
          )}
          {writable && draft && (
            <>
              <Button variant="outline" onClick={() => show("change")}>
                Change shifts
              </Button>
              {current.auto_population !== "disabled" && (
                <Button variant="outline" onClick={() => show("populate")}>
                  Populate
                </Button>
              )}
              <Button variant="outline" onClick={() => show("discard")}>
                Discard roster
              </Button>
              <Button onClick={() => show("freeze")}>Freeze roster</Button>
            </>
          )}
          {correctable && current?.status === "frozen" && (
            <Button variant="outline" onClick={() => show("unfreeze")}>
              Unfreeze for correction
            </Button>
          )}
          <Button variant="ghost" onClick={onClose}>
            Close roster
          </Button>
        </div>
      </div>
      {roster.isPending ? (
        <p role="status" className="p-5">
          Loading roster…
        </p>
      ) : roster.isError || !current ? (
        <div className="p-5">
          <Failure message="Could not load the roster." retry={() => roster.refetch()} />
        </div>
      ) : (
        <>
          {draft && (
            <p className="border-b bg-muted/30 px-5 py-3 text-xs text-muted-foreground">
              Overlapping shifts are blocked. Leave and credential conflicts are not checked yet;
              confirm them before freezing.
            </p>
          )}
          {panel === "change" && (
            <ChangeEditor
              path={path}
              base={base}
              hospital={hospital}
              roster={current}
              removals={removals}
              onClose={() => show(null)}
            />
          )}
          {panel === "populate" && (
            <PopulatePanel
              path={path}
              base={base}
              hospital={hospital}
              roster={current}
              onClose={() => show(null)}
            />
          )}
          {(panel === "freeze" || panel === "unfreeze" || panel === "discard") && (
            <StatusPanel
              key={panel}
              action={panel}
              path={path}
              hospital={hospital}
              roster={current}
              onClose={() => show(null)}
            />
          )}
          {panel === "history" && (
            <HistoryPanel
              path={path}
              hospital={hospital}
              asOf={asOf}
              onView={(version) => {
                setAsOf(version);
                setPanel(null);
              }}
              onClose={() => show(null)}
            />
          )}
          <AllocationGrid
            path={path}
            hospital={hospital}
            roster={current}
            asOf={asOf}
            onCurrent={() => setAsOf(null)}
            selectable={panel === "change" && asOf === null}
            removals={removals}
            onToggle={(id) =>
              setRemovals((prev) => {
                const next = new Set(prev);
                if (!next.delete(id)) next.add(id);
                return next;
              })
            }
          />
        </>
      )}
    </section>
  );
}

function AllocationGrid({
  path,
  hospital,
  roster,
  asOf,
  onCurrent,
  selectable,
  removals,
  onToggle,
}: {
  path: string;
  hospital: Hospital;
  roster: Roster;
  asOf: number | null;
  onCurrent: () => void;
  selectable: boolean;
  removals: Set<string>;
  onToggle: (id: string) => void;
}) {
  const allocations = useApiPagedQuery<DatedAllocation>(
    ["workforce", "rosters", hospital.id, roster.id, "allocations", asOf],
    `${path}/allocations?limit=200${asOf ? `&version=${asOf}` : ""}`,
  );
  const dates = rosterDates(roster.start_date, roster.end_date);
  const rows = useMemo(() => {
    const people = new Map<string, { name: string; cells: Map<string, DatedAllocation[]> }>();
    for (const a of allocations.items) {
      const person = people.get(a.staff_id) ?? {
        name: `${a.display_name} · ${a.employee_code}`,
        cells: new Map(),
      };
      person.cells.set(a.roster_date, [...(person.cells.get(a.roster_date) ?? []), a]);
      people.set(a.staff_id, person);
    }
    return [...people.values()].sort((x, y) => x.name.localeCompare(y.name));
  }, [allocations.items]);
  return (
    <div role="region" aria-label="Allocations" className="p-5">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-4">
        <h4 className="font-semibold">
          {asOf ? `Allocations as of v${asOf}` : `Current allocations (${roster.allocations})`}
        </h4>
        {asOf && (
          <Button variant="outline" onClick={onCurrent}>
            Show current roster
          </Button>
        )}
      </div>
      {allocations.isPending ? (
        <p role="status">Loading allocations…</p>
      ) : allocations.isError ? (
        <Failure message="Could not load allocations." retry={allocations.retry} />
      ) : rows.length === 0 ? (
        <p className="text-sm text-muted-foreground">No shifts are allocated in this version.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="min-w-full border-separate border-spacing-0 text-xs">
            <thead>
              <tr>
                <th scope="col" className="sticky left-0 bg-card p-2 text-left">
                  Staff
                </th>
                {dates.map((d) => (
                  <th key={d} scope="col" className="min-w-28 p-2 text-left font-medium">
                    {d}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.name}>
                  <th scope="row" className="sticky left-0 border-t bg-card p-2 text-left">
                    {row.name}
                  </th>
                  {dates.map((d) => (
                    <td key={d} className="border-t p-2 align-top">
                      {(row.cells.get(d) ?? []).map((a) => (
                        <Shift
                          key={a.id}
                          allocation={a}
                          selectable={selectable}
                          selected={removals.has(a.id)}
                          onToggle={onToggle}
                        />
                      ))}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {allocations.hasMore && (
        <Button
          className="mt-4"
          variant="outline"
          disabled={allocations.isFetchingMore}
          onClick={allocations.loadMore}
        >
          Load more allocations
        </Button>
      )}
    </div>
  );
}

function Shift({
  allocation: a,
  selectable,
  selected,
  onToggle,
}: {
  allocation: DatedAllocation;
  selectable: boolean;
  selected: boolean;
  onToggle: (id: string) => void;
}) {
  const label = `${a.additional ? "+ " : ""}${a.shift_code} ${a.start_time}–${a.end_time} · ${a.area_code}`;
  if (!selectable)
    return (
      <p className="mb-1 rounded-md bg-muted px-2 py-1" title={`${a.starts_at} to ${a.ends_at}`}>
        {label}
        {a.exchange_id && (
          <span className="ml-1.5 text-xs font-medium text-(--hms-accent)">· exchanged</span>
        )}
      </p>
    );
  return (
    <label className="mb-1 flex items-center gap-2 rounded-md bg-muted px-2 py-1">
      <input
        type="checkbox"
        className="size-3.5 accent-(--hms-accent)"
        checked={selected}
        onChange={() => onToggle(a.id)}
        aria-label={`Remove ${a.shift_code} for ${a.display_name} on ${a.roster_date}`}
      />
      <span className={selected ? "line-through" : undefined}>{label}</span>
    </label>
  );
}

function PopulatePanel({
  path,
  base,
  hospital,
  roster,
  onClose,
}: {
  path: string;
  base: string;
  hospital: Hospital;
  roster: Roster;
  onClose: () => void;
}) {
  const [changeID] = useState(() => crypto.randomUUID());
  const staff = useApiPagedQuery<StaffRosterAssignment>(
    ["workforce", "roster-assignments", hospital.id],
    `${base}/roster-assignments`,
  );
  const [result, setResult] = useState<RosterRevision | null>(null);
  const form = useZodForm(rosterPopulateSchema, {
    change_id: changeID,
    expected_version: roster.version,
    staff_ids: [],
    reason: "",
    reviewed: false,
  });
  const save = useRosterWrite<RosterPopulateInput>(
    `${path}/populate`,
    hospital.id,
    "Roster populated",
    (r) => setResult(r.revision),
  );
  const chosen = form.watch("staff_ids");
  const errors = form.formState.errors;
  if (result)
    return (
      <div role="status" className="space-y-3 border-b p-5 text-sm">
        <p className="font-medium">
          Added {result.added} shifts · kept {result.skipped_existing} existing · skipped{" "}
          {result.skipped_unassigned} without an active assignment · skipped{" "}
          {result.skipped_conflicts} that would overlap
        </p>
        <Button variant="outline" onClick={onClose}>
          Done
        </Button>
      </div>
    );
  return (
    <form
      noValidate
      aria-label="Populate roster"
      onSubmit={form.handleSubmit((input) => save.mutate(input))}
      className="space-y-5 border-b p-5"
    >
      <fieldset disabled={save.isPending} className="space-y-5">
        <p className="text-sm text-muted-foreground">
          Fills empty dates from weekly-template assignments under{" "}
          {ROSTER_AUTO_POPULATION[roster.auto_population].toLowerCase()}. Existing shifts are never
          replaced. Leave everyone unselected to populate all eligible staff.
        </p>
        {staff.isError ? (
          <Failure message="Could not load assigned staff." retry={staff.retry} />
        ) : (
          <fieldset className="grid gap-2 sm:grid-cols-3">
            <legend className="mb-2 text-sm font-medium">Limit to staff</legend>
            {staff.items
              .filter((s) => s.active)
              .map((s) => (
                <label key={s.staff_id} className="flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    className="size-4 accent-(--hms-accent)"
                    value={s.staff_id}
                    {...form.register("staff_ids")}
                  />
                  {s.display_name} · {s.employee_code}
                </label>
              ))}
          </fieldset>
        )}
        <ReviewCheck
          id="populate-review"
          error={errors.reviewed?.message}
          register={form.register("reviewed")}
        >
          I reviewed who will be populated ({chosen.length ? `${chosen.length} selected` : "all"})
        </ReviewCheck>
      </fieldset>
      {save.isError && <SaveError message={save.error.message} />}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Populating…" : "Populate roster"}
        </Button>
      </div>
    </form>
  );
}

const STATUS_COPY = {
  freeze: {
    title: "Freeze roster",
    body: "Publishes the roster. Changes need a correction by someone allowed to unfreeze it.",
    button: "Freeze roster",
    toast: "Roster frozen",
    review: "I reviewed the shifts, including leave and credentials",
  },
  unfreeze: {
    title: "Unfreeze for correction",
    body: "Returns the roster to draft so it can be corrected. The reason is kept in its history.",
    button: "Unfreeze roster",
    toast: "Roster unfrozen",
    review: "I reviewed why this published roster needs a correction",
  },
  discard: {
    title: "Discard roster",
    body: "Removes every current shift and frees the period. History is kept.",
    button: "Discard roster",
    toast: "Roster discarded",
    review: "I reviewed that this draft should be discarded",
  },
} as const;

function StatusPanel({
  action,
  path,
  hospital,
  roster,
  onClose,
}: {
  action: keyof typeof STATUS_COPY;
  path: string;
  hospital: Hospital;
  roster: Roster;
  onClose: () => void;
}) {
  const copy = STATUS_COPY[action];
  const [changeID] = useState(() => crypto.randomUUID());
  const [confirming, setConfirming] = useState<RosterStatusInput | null>(null);
  const form = useZodForm(rosterStatusSchema(action !== "freeze"), {
    change_id: changeID,
    expected_version: roster.version,
    reason: "",
    reviewed: false,
  });
  const save = useRosterWrite<RosterStatusInput>(
    `${path}/${action}`,
    hospital.id,
    copy.toast,
    () => {
      setConfirming(null);
      onClose();
    },
  );
  const errors = form.formState.errors;
  return (
    <form
      noValidate
      aria-label={copy.title}
      onSubmit={form.handleSubmit((input) =>
        action === "discard" ? setConfirming(input) : save.mutate(input),
      )}
      className="space-y-5 border-b p-5"
    >
      <fieldset disabled={save.isPending} className="space-y-5">
        <p className="text-sm text-muted-foreground">{copy.body}</p>
        {action !== "freeze" && (
          <Field id={`${action}-reason`} label="Reason" error={errors.reason?.message}>
            <textarea
              id={`${action}-reason`}
              rows={2}
              className={inputClass}
              aria-invalid={!!errors.reason}
              {...form.register("reason")}
            />
          </Field>
        )}
        <ReviewCheck
          id={`${action}-review`}
          error={errors.reviewed?.message}
          register={form.register("reviewed")}
        >
          {copy.review}
        </ReviewCheck>
      </fieldset>
      {save.isError && <SaveError message={save.error.message} />}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {copy.button}
        </Button>
      </div>
      <ConfirmDialog
        open={!!confirming}
        onOpenChange={(open) => !open && setConfirming(null)}
        title="Discard this roster?"
        description={`${roster.allocations} current shifts will be removed. The period becomes free and the history stays available.`}
        confirmLabel="Discard roster"
        busy={save.isPending}
        onConfirm={() => confirming && save.mutate(confirming)}
      />
    </form>
  );
}

function HistoryPanel({
  path,
  hospital,
  asOf,
  onView,
  onClose,
}: {
  path: string;
  hospital: Hospital;
  asOf: number | null;
  onView: (version: number) => void;
  onClose: () => void;
}) {
  const history = useApiPagedQuery<RosterRevision>(
    ["workforce", "rosters", hospital.id, path, "revisions"],
    `${path}/revisions`,
  );
  return (
    <div role="region" aria-label="Roster history" className="border-b p-5">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h4 className="font-semibold">Revision history</h4>
        <Button variant="ghost" onClick={onClose}>
          Close history
        </Button>
      </div>
      {history.isPending ? (
        <p role="status">Loading roster history…</p>
      ) : history.isError ? (
        <Failure message="Could not load roster history." retry={history.retry} />
      ) : (
        <ul className="space-y-3">
          {history.items.map((r) => (
            <li
              key={r.id}
              className="flex flex-wrap items-center justify-between gap-4 rounded-xl border p-4 text-sm"
            >
              <div>
                <p className="font-medium">
                  v{r.version} · {r.action}
                  {r.mode
                    ? ` · ${ROSTER_MODES[r.mode as keyof typeof ROSTER_MODES] ?? r.mode}`
                    : ""}
                </p>
                <p className="mt-1 text-muted-foreground">
                  +{r.added} / −{r.removed}
                  {r.skipped_existing + r.skipped_unassigned + r.skipped_conflicts > 0 &&
                    ` · skipped ${r.skipped_existing} existing, ${r.skipped_unassigned} unassigned, ${r.skipped_conflicts} conflicting`}{" "}
                  · {r.reviewed_by_subject} · {r.created_at}
                  {r.reason && ` · “${r.reason}”`}
                </p>
              </div>
              <Button
                variant="outline"
                disabled={asOf === r.version}
                aria-label={`View roster as of v${r.version}`}
                onClick={() => onView(r.version)}
              >
                View
              </Button>
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
