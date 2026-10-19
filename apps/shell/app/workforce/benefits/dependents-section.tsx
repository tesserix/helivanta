"use client";
import { useRef, useState } from "react";
import { Button } from "@tesserix/web";
import { Users } from "lucide-react";
import {
  dependentHistoryPath,
  dependentsKey,
  type BenefitScope,
  type Dependent,
  type DependentRevision,
  type UseApiPagedQueryResult,
} from "@helivanta/api";
import { EmptyState } from "@helivanta/ui";
import { Failure } from "../roster-policy/policy-ui";
import { RevisionHistory } from "../payroll-setup/payroll-workspace";
import { Badge, DEPENDENT_STATUSES, useRelationships } from "./benefits-ui";
import { DependentEditor } from "./dependent-editor";

export function DependentsSection({
  scope,
  dependents,
  writable,
  adding,
}: {
  scope: BenefitScope;
  dependents: UseApiPagedQueryResult<Dependent>;
  writable: boolean;
  adding: boolean;
}) {
  const relationships = useRelationships(scope.hospitalId);
  const [editing, setEditing] = useState<Dependent | "new" | null>(null);
  const [history, setHistory] = useState<Dependent | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const open = (
    ev: React.MouseEvent<HTMLButtonElement>,
    edit: typeof editing,
    show: typeof history,
  ) => {
    opener.current = ev.currentTarget;
    setEditing(edit);
    setHistory(show);
  };
  const close = () => {
    setEditing(null);
    setHistory(null);
    opener.current?.focus();
  };
  return (
    <section aria-label="Dependents" className="border-t">
      <div className="flex flex-wrap items-center justify-between gap-4 p-5">
        <h4 className="font-semibold">Dependents</h4>
        {adding && editing === null && (
          <Button variant="outline" onClick={(ev) => open(ev, "new", null)}>
            Add dependent
          </Button>
        )}
      </div>
      {editing !== null && (
        <DependentEditor
          key={editing === "new" ? "new" : editing.revision_id}
          scope={scope}
          record={editing === "new" ? undefined : editing}
          relationships={relationships}
          onClose={close}
        />
      )}
      {dependents.isPending ? (
        <p role="status" className="px-5 pb-5">
          Loading dependents…
        </p>
      ) : dependents.isError ? (
        <div className="px-5 pb-5">
          <Failure message="Could not load dependents." retry={dependents.retry} />
        </div>
      ) : dependents.items.length === 0 ? (
        <EmptyState
          icon={Users}
          title="No dependents recorded"
          hint={
            adding
              ? "Add the people this employee's benefits may cover, with where each record comes from."
              : "Dependents your HR team records appear here."
          }
        />
      ) : (
        <ul className="divide-y border-t">
          {dependents.items.map((d) => (
            <li key={d.id} className="flex flex-wrap items-start justify-between gap-4 p-5">
              <div className="min-w-0">
                <p className="font-medium">
                  {d.full_name}
                  {d.status === "removed" && <Badge>{DEPENDENT_STATUSES.removed}</Badge>}
                </p>
                <p className="mt-1 text-sm">
                  {d.relationship_name} · born {d.birth_date}
                </p>
                {d.phone && <p className="mt-1 text-xs text-muted-foreground">{d.phone}</p>}
              </div>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  aria-label={`History of ${d.full_name}`}
                  onClick={(ev) => open(ev, null, d)}
                >
                  History
                </Button>
                {writable && (
                  <Button
                    variant="outline"
                    aria-label={`Edit ${d.full_name}`}
                    onClick={(ev) => open(ev, d, null)}
                  >
                    Edit
                  </Button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      {dependents.hasMore && (
        <div className="border-t p-5">
          <Button
            variant="outline"
            disabled={dependents.isFetchingMore}
            onClick={dependents.loadMore}
          >
            Load more dependents
          </Button>
        </div>
      )}
      {history && (
        <RevisionHistory<DependentRevision>
          key={history.id}
          label={`${history.full_name} history`}
          queryKey={[...dependentsKey(scope), "history", history.id]}
          path={dependentHistoryPath(scope, history.id)}
          rowKey={(r) => r.id}
          onClose={close}
          render={(r) => (
            <>
              <p className="font-medium">
                {`v${r.version} · ${DEPENDENT_STATUSES[r.status]} · ${r.full_name} (${r.relationship_name})`}
              </p>
              <p className="mt-1 text-muted-foreground">
                {r.reason} · changed by {r.changed_by_subject}
              </p>
            </>
          )}
        />
      )}
    </section>
  );
}
