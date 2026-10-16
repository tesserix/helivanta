"use client";
import { useRef, useState } from "react";
import { Button } from "@tesserix/web";
import {
  formatPaise,
  salaryHeadsPath,
  salaryStructuresPath,
  useApiPagedQuery,
  type HRMaster,
  type Hospital,
  type SalaryHead,
  type SalaryStructure,
} from "@helivanta/api";
import { Failure } from "../roster-policy/policy-ui";
import { headsKey, RevisionHistory, structuresKey } from "./payroll-workspace";
import { StructureEditor } from "./structure-editor";
import { StructurePreview } from "./structure-preview";

export const describeLines = (s: SalaryStructure) =>
  s.lines
    .map((l) => `${l.name}${l.amount_paise === undefined ? "" : ` ${formatPaise(l.amount_paise)}`}`)
    .join(" · ");

export function StructuresPanel({ hospital, writable }: { hospital: Hospital; writable: boolean }) {
  const base = `/workforce/hospitals/${hospital.id}`;
  const [editing, setEditing] = useState<SalaryStructure | "new" | null>(null);
  const [open, setOpen] = useState<{
    structure: SalaryStructure;
    view: "preview" | "history";
  } | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const structures = useApiPagedQuery<SalaryStructure>(
    structuresKey(hospital.id),
    salaryStructuresPath(base),
  );
  const heads = useApiPagedQuery<SalaryHead>(
    [...headsKey(hospital.id), "all"],
    salaryHeadsPath(base),
  );
  const masters = useApiPagedQuery<HRMaster>(
    ["workforce", "master-choices", hospital.id],
    `${base}/hr/masters`,
  );
  const masterNames = new Map(masters.items.map((m) => [m.id, m.name]));
  const scope = (s: SalaryStructure) =>
    [s.department_id, s.designation_id, s.grade_id]
      .filter((id): id is string => !!id)
      .map((id) => masterNames.get(id) ?? "an unavailable choice")
      .join(" · ") || "Any staff";
  const close = () => {
    setEditing(null);
    setOpen(null);
    opener.current?.focus();
  };
  const show =
    (structure: SalaryStructure, view: "preview" | "history") =>
    (ev: React.MouseEvent<HTMLButtonElement>) => {
      opener.current = ev.currentTarget;
      setEditing(null);
      setOpen({ structure, view });
    };
  return (
    <section
      aria-label="Salary structures"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-end justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Salary structures</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            A structure lists the heads a group of staff is paid, with amounts for fixed heads.
          </p>
        </div>
        {writable && editing === null && (
          <Button
            disabled={heads.isPending || heads.isError || heads.items.length === 0}
            onClick={(ev) => {
              opener.current = ev.currentTarget;
              setOpen(null);
              setEditing("new");
            }}
          >
            Add salary structure
          </Button>
        )}
      </div>
      {editing !== null && (
        <StructureEditor
          key={editing === "new" ? "new" : editing.revision_id}
          hospital={hospital.id}
          base={base}
          record={editing === "new" ? undefined : editing}
          heads={heads.items}
          masters={masters.items}
          onClose={close}
        />
      )}
      {structures.isPending ? (
        <p role="status" className="p-5">
          Loading salary structures…
        </p>
      ) : structures.isError ? (
        <div className="p-5">
          <Failure message="Could not load salary structures." retry={structures.retry} />
        </div>
      ) : structures.items.length === 0 ? (
        <p className="p-5 text-sm text-muted-foreground">
          {heads.items.length === 0
            ? "No salary structures yet. Add salary heads first, then group them into a structure."
            : "No salary structures yet. Group the reviewed heads into a structure for each role or grade."}
        </p>
      ) : (
        <ul className="divide-y">
          {structures.items.map((s) => (
            <li key={s.id} className="flex flex-wrap items-start justify-between gap-4 p-5">
              <div className="min-w-0">
                <p className="font-medium">
                  {s.name} · {s.code}
                  {!s.active && " · inactive"}
                </p>
                <p className="mt-1 text-sm">{describeLines(s)}</p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {scope(s)} · v{s.version} effective {s.effective_on}
                </p>
              </div>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  aria-label={`Preview ${s.name}`}
                  onClick={show(s, "preview")}
                >
                  Preview
                </Button>
                <Button
                  variant="outline"
                  aria-label={`History of ${s.name}`}
                  onClick={show(s, "history")}
                >
                  History
                </Button>
                {writable && (
                  <Button
                    variant="outline"
                    aria-label={`Edit ${s.name}`}
                    onClick={(ev) => {
                      opener.current = ev.currentTarget;
                      setOpen(null);
                      setEditing(s);
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
      {structures.hasMore && (
        <div className="border-t p-5">
          <Button
            variant="outline"
            disabled={structures.isFetchingMore}
            onClick={structures.loadMore}
          >
            Load more salary structures
          </Button>
        </div>
      )}
      {(heads.hasMore || masters.hasMore) && editing !== null && (
        <div className="flex gap-2 border-t p-5">
          {heads.hasMore && (
            <Button variant="outline" disabled={heads.isFetchingMore} onClick={heads.loadMore}>
              Load more salary heads
            </Button>
          )}
          {masters.hasMore && (
            <Button variant="outline" disabled={masters.isFetchingMore} onClick={masters.loadMore}>
              Load more HR choices
            </Button>
          )}
        </div>
      )}
      {open?.view === "preview" && (
        <StructurePreview
          key={open.structure.revision_id}
          base={base}
          structure={open.structure}
          onClose={close}
        />
      )}
      {open?.view === "history" && (
        <RevisionHistory<SalaryStructure>
          key={open.structure.id}
          label={`${open.structure.name} history`}
          queryKey={[...structuresKey(hospital.id), "history", open.structure.id]}
          path={`${salaryStructuresPath(base)}/${open.structure.id}/history`}
          onClose={close}
          render={(r) => (
            <>
              <p className="font-medium">
                v{r.version} · effective {r.effective_on}
                {!r.active && " · inactive"}
              </p>
              <p className="mt-1 text-muted-foreground">
                {describeLines(r)} · reviewed by {r.reviewed_by_subject}
              </p>
            </>
          )}
        />
      )}
    </section>
  );
}
