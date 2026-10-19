"use client";
import { useRef, useState } from "react";
import { Button } from "@tesserix/web";
import { HeartHandshake } from "lucide-react";
import {
  benefitPlanHistoryPath,
  benefitPlansKey,
  benefitPlansPath,
  useApiPagedQuery,
  BENEFIT_PLAN_KINDS,
  type BenefitPlan,
  type BenefitPlanRevision,
  type Hospital,
} from "@helivanta/api";
import { EmptyState } from "@helivanta/ui";
import { Failure } from "../roster-policy/policy-ui";
import { RevisionHistory } from "../payroll-setup/payroll-workspace";
import { Badge, PLAN_STATUSES, period, useRelationships } from "./benefits-ui";
import { PlanEditor } from "./plan-editor";

export const coverageText = (
  plan: Pick<BenefitPlan, "covers_dependents" | "covered_relationships">,
  names: Map<string, string>,
) =>
  plan.covers_dependents
    ? `Covers the employee and ${plan.covered_relationships.map((id) => names.get(id) ?? "an unlisted relationship").join(", ")}`
    : "Covers the employee only";

export function PlansPanel({ hospital, writable }: { hospital: Hospital; writable: boolean }) {
  const plans = useApiPagedQuery<BenefitPlan>(
    benefitPlansKey(hospital.id),
    benefitPlansPath(hospital.id),
  );
  const relationships = useRelationships(hospital.id);
  const names = new Map(relationships.items.map((r) => [r.id, r.name]));
  const [editing, setEditing] = useState<BenefitPlan | "new" | null>(null);
  const [history, setHistory] = useState<BenefitPlan | null>(null);
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
    <section
      aria-label="Benefit plans"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-end justify-between gap-4 border-b p-5">
        <div>
          <h3 className="font-semibold">Benefit plans</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            {hospital.name} · {writable ? "Manager access" : "Read-only access"}
          </p>
        </div>
        {writable && editing === null && (
          <Button onClick={(ev) => open(ev, "new", null)}>Add plan</Button>
        )}
      </div>
      {editing !== null && (
        <PlanEditor
          key={editing === "new" ? "new" : editing.revision_id}
          hospital={hospital.id}
          record={editing === "new" ? undefined : editing}
          relationships={relationships}
          onClose={close}
        />
      )}
      {plans.isPending ? (
        <p role="status" className="p-5">
          Loading benefit plans…
        </p>
      ) : plans.isError ? (
        <div className="p-5">
          <Failure message="Could not load benefit plans." retry={plans.retry} />
        </div>
      ) : plans.items.length === 0 ? (
        <EmptyState
          icon={HeartHandshake}
          title="No benefit plans yet"
          hint={
            writable
              ? "Add each plan your HR team offers, with the dates it runs and who it covers."
              : "Plans your HR team adds appear here."
          }
        />
      ) : (
        <ul className="divide-y">
          {plans.items.map((p) => (
            <li key={p.id} className="flex flex-wrap items-start justify-between gap-4 p-5">
              <div className="min-w-0">
                <p className="font-medium">
                  {p.name} · {p.code}
                  {p.status === "retired" && <Badge>{PLAN_STATUSES.retired}</Badge>}
                </p>
                <p className="mt-1 text-sm">
                  {BENEFIT_PLAN_KINDS[p.kind]} · v{p.version} · effective{" "}
                  {period(p.effective_from, p.effective_until)}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">{coverageText(p, names)}</p>
                {p.description && (
                  <p className="mt-1 text-xs text-muted-foreground">{p.description}</p>
                )}
              </div>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  aria-label={`History of ${p.name}`}
                  onClick={(ev) => open(ev, null, p)}
                >
                  History
                </Button>
                {writable && p.status === "active" && (
                  <Button
                    variant="outline"
                    aria-label={`Edit ${p.name}`}
                    onClick={(ev) => open(ev, p, null)}
                  >
                    Edit
                  </Button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      {plans.hasMore && (
        <div className="border-t p-5">
          <Button variant="outline" disabled={plans.isFetchingMore} onClick={plans.loadMore}>
            Load more plans
          </Button>
        </div>
      )}
      {history && (
        <RevisionHistory<BenefitPlanRevision>
          key={history.id}
          label={`${history.name} history`}
          queryKey={[...benefitPlansKey(hospital.id), "history", history.id]}
          path={benefitPlanHistoryPath(hospital.id, history.id)}
          rowKey={(r) => r.id}
          onClose={close}
          render={(r) => (
            <>
              <p className="font-medium">
                v{r.version} · {PLAN_STATUSES[r.status]} · effective{" "}
                {period(r.effective_from, r.effective_until)}
              </p>
              <p className="mt-1 text-muted-foreground">
                {r.name} · {BENEFIT_PLAN_KINDS[r.kind]} · {coverageText(r, names)} · changed by{" "}
                {r.changed_by_subject}
              </p>
            </>
          )}
        />
      )}
    </section>
  );
}
