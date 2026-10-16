"use client";
import { useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  describeSalaryHead,
  formatPaise,
  halfDaysToDays,
  salaryHeadsPath,
  useApiPagedQuery,
  SALARY_HEAD_KINDS,
  SALARY_TAXABILITY,
  SALARY_WAGE_CLASSES,
  type Hospital,
  type SalaryHead,
} from "@helivanta/api";
import { Field, SelectControl } from "@helivanta/ui";
import { Failure } from "../roster-policy/policy-ui";
import { HeadEditor } from "./head-editor";
import { headsKey, RevisionHistory } from "./payroll-workspace";

const kindOptions = [
  { value: "all", label: "All kinds" },
  ...Object.entries(SALARY_HEAD_KINDS).map(([value, label]) => ({ value, label })),
];

export function HeadsPanel({ hospital, writable }: { hospital: Hospital; writable: boolean }) {
  const base = `/workforce/hospitals/${hospital.id}`;
  const [kind, setKind] = useState("all");
  const [editing, setEditing] = useState<SalaryHead | "new" | null>(null);
  const [history, setHistory] = useState<SalaryHead | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const heads = useApiPagedQuery<SalaryHead>(
    [...headsKey(hospital.id), kind],
    salaryHeadsPath(base, kind),
  );
  // Percentage bases are named from the unfiltered list so a filter never hides them.
  const all = useApiPagedQuery<SalaryHead>(
    [...headsKey(hospital.id), "all"],
    salaryHeadsPath(base),
  );
  const names = new Map(all.items.map((h) => [h.id, h.name]));
  const close = () => {
    setEditing(null);
    setHistory(null);
    opener.current?.focus();
  };
  return (
    <section
      aria-label="Salary heads"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-end justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Salary heads</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            {hospital.name} · {writable ? "Manager access" : "Read-only access"}
          </p>
        </div>
        <div className="flex flex-wrap items-end gap-3">
          <div className="w-48">
            <Field id="salary-head-kind-filter" label="Kind">
              <SelectControl
                id="salary-head-kind-filter"
                value={kind}
                onValueChange={setKind}
                options={kindOptions}
              />
            </Field>
          </div>
          {writable && editing === null && (
            <Button
              onClick={(ev) => {
                opener.current = ev.currentTarget;
                setHistory(null);
                setEditing("new");
              }}
            >
              Add salary head
            </Button>
          )}
        </div>
      </div>
      {editing !== null && (
        <HeadEditor
          key={editing === "new" ? "new" : editing.revision_id}
          hospital={hospital.id}
          base={base}
          record={editing === "new" ? undefined : editing}
          heads={all.items}
          onClose={close}
        />
      )}
      {heads.isPending ? (
        <p role="status" className="p-5">
          Loading salary heads…
        </p>
      ) : heads.isError ? (
        <div className="p-5">
          <Failure message="Could not load salary heads." retry={heads.retry} />
        </div>
      ) : heads.items.length === 0 ? (
        <p className="p-5 text-sm text-muted-foreground">
          {kind === "all"
            ? "No salary heads are defined for this hospital. Add each earning, deduction and employer contribution your payroll team has reviewed."
            : `No ${SALARY_HEAD_KINDS[kind as keyof typeof SALARY_HEAD_KINDS].toLowerCase()} heads.`}
        </p>
      ) : (
        <ul className="divide-y">
          {heads.items.map((h) => (
            <li key={h.id} className="flex flex-wrap items-start justify-between gap-4 p-5">
              <div>
                <p className="font-medium">
                  {h.name} · {h.code}
                  {!h.active && " · inactive"}
                </p>
                <p className="mt-1 text-sm">
                  {SALARY_HEAD_KINDS[h.kind]} · {describeSalaryHead(h, names)}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {SALARY_TAXABILITY[h.taxability]} · {SALARY_WAGE_CLASSES[h.wage_class]}
                  {h.min_amount_paise !== undefined &&
                    ` · floor ${formatPaise(h.min_amount_paise)}`}
                  {h.max_amount_paise !== undefined && ` · cap ${formatPaise(h.max_amount_paise)}`}
                  {h.minimum_payable_half_days > 0 &&
                    ` · needs ${halfDaysToDays(h.minimum_payable_half_days)} payable days`}{" "}
                  · v{h.version} effective {h.effective_on}
                </p>
              </div>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  aria-label={`History of ${h.name}`}
                  onClick={(ev) => {
                    opener.current = ev.currentTarget;
                    setEditing(null);
                    setHistory(h);
                  }}
                >
                  History
                </Button>
                {writable && (
                  <Button
                    variant="outline"
                    aria-label={`Edit ${h.name}`}
                    onClick={(ev) => {
                      opener.current = ev.currentTarget;
                      setHistory(null);
                      setEditing(h);
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
      {heads.hasMore && (
        <div className="border-t p-5">
          <Button variant="outline" disabled={heads.isFetchingMore} onClick={heads.loadMore}>
            Load more salary heads
          </Button>
        </div>
      )}
      {all.hasMore && editing !== null && (
        <div className="border-t p-5">
          <Button variant="outline" disabled={all.isFetchingMore} onClick={all.loadMore}>
            Load more base choices
          </Button>
        </div>
      )}
      {history && (
        <RevisionHistory<SalaryHead>
          key={history.id}
          label={`${history.name} history`}
          queryKey={[...headsKey(hospital.id), "history", history.id]}
          path={`${base}/hr/payroll/salary-heads/${history.id}/history`}
          onClose={close}
          render={(r) => (
            <>
              <p className="font-medium">
                v{r.version} · effective {r.effective_on}
                {!r.active && " · inactive"}
              </p>
              <p className="mt-1 text-muted-foreground">
                {describeSalaryHead(r, names)} · {SALARY_TAXABILITY[r.taxability]} · reviewed by{" "}
                {r.reviewed_by_subject}
              </p>
            </>
          )}
        />
      )}
    </section>
  );
}
