"use client";
import { useRef, useState } from "react";
import { Button } from "@tesserix/web";
import { ShieldCheck } from "lucide-react";
import {
  benefitEnrollmentHistoryPath,
  benefitEnrollmentsKey,
  benefitEnrollmentsPath,
  benefitPlansKey,
  benefitPlansPath,
  useApiPagedQuery,
  usePermissions,
  type BenefitEnrollment,
  type BenefitPlan,
  type BenefitProposal,
  type BenefitScope,
  type Dependent,
  type Staff,
  type UseApiPagedQueryResult,
} from "@helivanta/api";
import { EmptyState } from "@helivanta/ui";
import { Failure } from "../roster-policy/policy-ui";
import { RevisionHistory } from "../payroll-setup/payroll-workspace";
import { PROPOSAL_STATUSES, period, personLabel } from "./benefits-ui";
import { CoverageProposal } from "./coverage-proposal";
import { ReviewForm, WithdrawForm } from "./coverage-decision";
import { CoverageEvidence } from "./coverage-evidence";

type View =
  | { kind: "new" }
  | {
      kind: "change" | "review" | "withdraw" | "history" | "evidence";
      enrollment: BenefitEnrollment;
    };

export function EnrollmentsSection({
  scope,
  member,
  dependents,
  writable,
  adding,
  approver,
}: {
  scope: BenefitScope;
  member: Staff;
  dependents: UseApiPagedQueryResult<Dependent>;
  writable: boolean;
  adding: boolean;
  approver: boolean;
}) {
  const { subject } = usePermissions();
  const plans = useApiPagedQuery<BenefitPlan>(
    benefitPlansKey(scope.hospitalId),
    benefitPlansPath(scope.hospitalId),
  );
  const enrollments = useApiPagedQuery<BenefitEnrollment>(
    benefitEnrollmentsKey(scope),
    benefitEnrollmentsPath(scope),
  );
  const planNames = new Map(plans.items.map((p) => [p.id, p.name]));
  const people = new Map(dependents.items.map((d) => [d.id, d]));
  const person = (id?: string) => {
    if (!id) return personLabel(member);
    const dependent = people.get(id);
    return dependent ? personLabel(member, dependent) : "Unlisted dependent";
  };
  const title = (e: BenefitEnrollment) =>
    `${planNames.get(e.plan_id) ?? "Unlisted plan"} · ${person(e.dependent_id)}`;
  const [view, setView] = useState<View | null>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const open = (ev: React.MouseEvent<HTMLButtonElement>, next: View) => {
    opener.current = ev.currentTarget;
    setView(next);
  };
  const close = () => {
    setView(null);
    opener.current?.focus();
  };
  const action = (
    e: BenefitEnrollment,
    kind: Exclude<View["kind"], "new">,
    label: string,
    joiner = "for",
  ) => (
    <Button
      variant="outline"
      aria-label={`${label} ${joiner} ${title(e)}`}
      onClick={(ev) => open(ev, { kind, enrollment: e })}
    >
      {label}
    </Button>
  );
  const chosen = view && view.kind !== "new" ? view.enrollment : undefined;
  return (
    <section aria-label="Coverage" className="border-t">
      <div className="flex flex-wrap items-center justify-between gap-4 p-5">
        <h4 className="font-semibold">Coverage</h4>
        {adding && view === null && (
          <Button variant="outline" onClick={(ev) => open(ev, { kind: "new" })}>
            Add coverage
          </Button>
        )}
      </div>
      {(view?.kind === "new" || view?.kind === "change") && (
        <CoverageProposal
          scope={scope}
          member={member}
          plans={plans}
          dependents={dependents}
          record={chosen}
          title={chosen && title(chosen)}
          onClose={close}
        />
      )}
      {view?.kind === "review" && view.enrollment.pending && (
        <ReviewForm
          key={view.enrollment.pending.id}
          scope={scope}
          enrollment={view.enrollment}
          proposal={view.enrollment.pending}
          title={title(view.enrollment)}
          onClose={close}
        />
      )}
      {view?.kind === "withdraw" && view.enrollment.pending && (
        <WithdrawForm
          key={view.enrollment.pending.id}
          scope={scope}
          enrollment={view.enrollment}
          proposal={view.enrollment.pending}
          title={title(view.enrollment)}
          onClose={close}
        />
      )}
      {view?.kind === "evidence" && (
        <CoverageEvidence
          key={view.enrollment.id}
          scope={scope}
          enrollment={view.enrollment}
          title={title(view.enrollment)}
          writable={writable}
          onClose={close}
        />
      )}
      {enrollments.isPending ? (
        <p role="status" className="px-5 pb-5">
          Loading coverage…
        </p>
      ) : enrollments.isError ? (
        <div className="px-5 pb-5">
          <Failure message="Could not load coverage." retry={enrollments.retry} />
        </div>
      ) : enrollments.items.length === 0 ? (
        <EmptyState
          icon={ShieldCheck}
          title="No coverage recorded"
          hint={
            adding
              ? "Propose coverage under a plan for the employee or a covered dependent. It applies once a second person approves it."
              : "Coverage your HR team approves appears here."
          }
        />
      ) : (
        <ul className="divide-y border-t">
          {enrollments.items.map((e) => {
            const pending = e.pending;
            const mine = !!pending && pending.proposed_by_subject === subject;
            return (
              <li key={e.id} className="flex flex-wrap items-start justify-between gap-4 p-5">
                <div className="min-w-0">
                  <p className="font-medium">{title(e)}</p>
                  <p className="mt-1 text-sm">
                    {e.coverage
                      ? `${PROPOSAL_STATUSES.approved} · v${e.coverage.version} · coverage ${period(e.coverage.coverage_from, e.coverage.coverage_until)}`
                      : "No approved coverage yet"}
                  </p>
                  {pending && (
                    <p className="mt-1 text-xs text-muted-foreground">
                      {`${PROPOSAL_STATUSES.pending} · coverage ${period(pending.coverage_from, pending.coverage_until)} · proposed by ${mine ? "you" : pending.proposed_by_subject}`}
                    </p>
                  )}
                </div>
                <div className="flex flex-wrap gap-2">
                  {action(e, "history", "History", "of")}
                  {(e.coverage || pending) && action(e, "evidence", "Evidence")}
                  {pending && approver && !mine && action(e, "review", "Review proposal")}
                  {pending && writable && mine && action(e, "withdraw", "Withdraw proposal")}
                  {writable && e.coverage && !pending && action(e, "change", "Change coverage")}
                </div>
              </li>
            );
          })}
        </ul>
      )}
      {enrollments.hasMore && (
        <div className="border-t p-5">
          <Button
            variant="outline"
            disabled={enrollments.isFetchingMore}
            onClick={enrollments.loadMore}
          >
            Load more coverage
          </Button>
        </div>
      )}
      {view?.kind === "history" && (
        <RevisionHistory<BenefitProposal>
          key={view.enrollment.id}
          label={`${title(view.enrollment)} history`}
          queryKey={[...benefitEnrollmentsKey(scope), "history", view.enrollment.id]}
          path={benefitEnrollmentHistoryPath(scope, view.enrollment.id)}
          rowKey={(r) => r.id}
          onClose={close}
          render={(r) => (
            <>
              <p className="font-medium">
                {`${PROPOSAL_STATUSES[r.status]}${r.approved_version ? ` · v${r.approved_version}` : ""} · coverage ${period(r.coverage_from, r.coverage_until)}`}
              </p>
              <p className="mt-1 text-muted-foreground">
                {`${r.eligibility_basis} · proposed by ${r.proposed_by_subject}${r.decided_by_subject ? ` · decided by ${r.decided_by_subject}` : ""}${r.decision_note ? ` · “${r.decision_note}”` : ""}`}
              </p>
            </>
          )}
        />
      )}
    </section>
  );
}
