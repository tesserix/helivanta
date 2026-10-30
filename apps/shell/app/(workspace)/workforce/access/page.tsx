"use client";

import { useEffect, useState, useSyncExternalStore } from "react";
import { Button } from "@tesserix/web";
import { PageHeading, Panel, LoadMore, SelectControl, Field, formatDateTime } from "@helivanta/ui";
import {
  useApiQuery,
  useStaffAssignments,
  useStaffAssignmentHistory,
  useStaffAssignmentDecisions,
  type StaffAssignment,
  type StaffAssignmentDecision,
  type StaffAssignmentScope,
  type StaffAssignmentFamily,
  staffAccessTemplates,
} from "@helivanta/api";
import { AssignmentProposalForm, AssignmentDecisionForm } from "./assignment-forms";
import { StaffMembershipForm } from "./membership-form";
import { MembershipManagement } from "./membership-management";

type Identity = { tenant_id: string; subject: string; data: string[] };
const labels: Record<StaffAssignment["status"], string> = {
  pending_scope: "Hospital verification pending",
  pending_review: "Awaiting independent review",
  scope_rejected: "Hospital verification declined",
  pending_decision_scope: "Decision verification pending",
  approved_pending_activation: "Approved; activation pending",
  active: "Assignment active",
  rejected: "Rejected",
  withdrawn: "Withdrawn",
};

const subscribeHydration = () => () => {};

export default function StaffAccessPage() {
  // Permission data may resolve before this page hydrates inside the shared shell.
  const hydrated = useSyncExternalStore(
    subscribeHydration,
    () => true,
    () => false,
  );
  const identity = useApiQuery<Identity>(["iam", "me", "permissions"], "/iam/me/permissions");
  return (
    <>
      <PageHeading description="Propose and independently review hospital-scoped assignments." />
      {!hydrated || identity.isPending ? (
        <p role="status">Checking assignment access…</p>
      ) : identity.isError || !identity.data?.tenant_id || !identity.data.subject ? (
        <p role="alert">Could not verify assignment access. Refresh this page to try again.</p>
      ) : !identity.data.data.some(
          (p) =>
            p === "iam.assignment.manage" ||
            p === "iam.assignment.review" ||
            p === "iam.member.manage",
        ) ? (
        <p>Assignment administration is not available for this account.</p>
      ) : (
        <div key={`${identity.data.tenant_id}:${identity.data.subject}`}>
          {identity.data.data.includes("iam.member.manage") && (
            <>
              <StaffMembershipForm tenantId={identity.data.tenant_id} />
              <MembershipManagement
                scope={{ tenantId: identity.data.tenant_id, subject: identity.data.subject }}
              />
            </>
          )}
          {identity.data.data.some(
            (p) => p === "iam.assignment.manage" || p === "iam.assignment.review",
          ) && <Assignments identity={identity.data} />}
        </div>
      )}
    </>
  );
}

function Assignments({ identity }: { identity: Identity }) {
  const scope: StaffAssignmentScope = { tenantId: identity.tenant_id, subject: identity.subject };
  const [family, setFamily] = useState<StaffAssignmentFamily>("leave");
  const records = useStaffAssignments(scope, family);
  const [proposing, setProposing] = useState(false);
  const [renewal, setRenewal] = useState<StaffAssignment | undefined>();
  const [notice, setNotice] = useState("");
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 30000);
    return () => clearInterval(timer);
  }, []);
  const manage = identity.data.includes("iam.assignment.manage");
  const review = identity.data.includes("iam.assignment.review");
  return (
    <Panel className="space-y-4">
      <p>
        {family === "leave"
          ? "Active leave-review assignments enable the Review leave page when the account also has an independently approved staff link. Both must remain current. These assignments grant no hospital administration, payroll or clinical authority."
          : "Identity manager and reviewer assignments have separate permissions for one hospital. They require a current staff account link and grant no hospital administration, payroll or clinical authority."}
      </p>
      <p>
        Proposing and reviewing require current tenant administrator membership and a hospital
        manager grant. The reviewer must be different from the proposer and the account receiving
        access.
      </p>
      <Field id="assignment-area" label="Assignment area">
        <SelectControl
          id="assignment-area"
          value={family}
          onValueChange={(value) => {
            setFamily(value as StaffAssignmentFamily);
            setRenewal(undefined);
            setProposing(false);
            setNotice("");
          }}
          options={[
            { value: "leave", label: "Leave review" },
            { value: "identity", label: "Staff identity checks" },
          ]}
        />
      </Field>
      <div className="flex flex-wrap gap-2">
        <Button
          variant="outline"
          disabled={records.isFetching}
          onClick={() => void records.refetch()}
        >
          Refresh assignments
        </Button>
        {manage && (
          <Button
            onClick={() => {
              setNotice("");
              setRenewal(undefined);
              setProposing(true);
            }}
          >
            Propose assignment
          </Button>
        )}
      </div>
      {notice && <p role="status">{notice}</p>}
      {proposing && (
        <AssignmentProposalForm
          key={`${family}:${renewal?.id ?? "new"}`}
          family={family}
          renewal={renewal}
          scope={scope}
          onClose={() => setProposing(false)}
          onSaved={() => {
            setProposing(false);
            setNotice(
              "Proposal recorded. Hospital verification and independent review must complete before activation.",
            );
          }}
        />
      )}
      {records.isPending ? (
        <p role="status">Loading assignments…</p>
      ) : records.isError ? (
        <p role="alert">Could not load assignments. Refresh to try again.</p>
      ) : (
        <>
          {!records.data?.pages.some((p) => p.data.length) && <p>No assignment proposals yet.</p>}
          {records.data?.pages
            .flatMap((p) => p.data)
            .map((item) => (
              <AssignmentRecord
                key={item.id}
                item={item}
                scope={scope}
                manage={manage}
                review={review}
                now={now}
                onRenew={() => {
                  setNotice("");
                  setRenewal(item);
                  setProposing(true);
                }}
                onSaved={() =>
                  setNotice(
                    "Decision recorded. Refresh to check hospital verification and activation; a submitted approval is not yet active access.",
                  )
                }
              />
            ))}
          <LoadMore
            hasMore={records.hasNextPage}
            isLoading={records.isFetching}
            onClick={() => void records.fetchNextPage({ cancelRefetch: false })}
          />
        </>
      )}
    </Panel>
  );
}

function AssignmentRecord({
  item,
  scope,
  manage,
  review,
  now,
  onRenew,
  onSaved,
}: {
  item: StaffAssignment;
  scope: StaffAssignmentScope;
  manage: boolean;
  review: boolean;
  now: number;
  onRenew: () => void;
  onSaved: () => void;
}) {
  const [decision, setDecision] = useState<StaffAssignmentDecision["decision"] | null>(null);
  const [history, setHistory] = useState(false);
  const independent = scope.subject !== item.proposed_by && scope.subject !== item.input.subject;
  const expired = Date.parse(item.input.expires_at) <= now;
  const withdrawable = !["withdrawn", "rejected", "scope_rejected"].includes(item.status);
  return (
    <section
      aria-label={`Assignment for ${item.input.subject}`}
      className="space-y-3 border-t border-border py-4"
    >
      <h2 className="text-lg font-semibold">
        {staffAccessTemplates.find((template) => template.key === item.input.template_key)?.name ??
          item.input.template_key}{" "}
        · {item.input.subject}
      </h2>
      <p>
        {labels[item.status]} · Version {item.version}
        {expired ? " · Expired" : ""}
      </p>
      <p>
        {staffAccessTemplates.find((template) => template.key === item.input.template_key)?.name ??
          item.input.template_key}
      </p>
      <p className="break-all">Hospital: {item.input.hospital_id}</p>
      <p>
        {formatDateTime(item.input.valid_from)} – {formatDateTime(item.input.expires_at)}
      </p>
      <p>Proposed by {item.proposed_by}</p>
      <p>{item.input.reason}</p>
      <div className="flex flex-wrap gap-2">
        {manage && item.status === "active" && (
          <Button variant="outline" onClick={onRenew}>
            Renew access
          </Button>
        )}
        {review && independent && !expired && item.status === "pending_review" && (
          <>
            <Button onClick={() => setDecision("approve")}>Approve</Button>
            <Button variant="outline" onClick={() => setDecision("reject")}>
              Reject
            </Button>
          </>
        )}
        {manage && scope.subject === item.proposed_by && withdrawable && (
          <Button variant="outline" onClick={() => setDecision("withdraw")}>
            Withdraw
          </Button>
        )}
        <Button variant="outline" onClick={() => setHistory(!history)}>
          {history ? "Close history" : "History"}
        </Button>
      </div>
      {decision && (
        <AssignmentDecisionForm
          key={`${item.version}:${decision}`}
          scope={scope}
          item={item}
          decision={decision}
          onClose={() => setDecision(null)}
          onSaved={() => {
            setDecision(null);
            onSaved();
          }}
        />
      )}
      {history && <AssignmentHistory scope={scope} item={item} />}
    </section>
  );
}

function AssignmentHistory({
  scope,
  item,
}: {
  scope: StaffAssignmentScope;
  item: StaffAssignment;
}) {
  const history = useStaffAssignmentHistory(scope, item.id);
  const decisions = useStaffAssignmentDecisions(scope, item.id);
  return (
    <div className="space-y-3" aria-label="Assignment history">
      <h3 className="font-semibold">Status history</h3>
      {history.isPending ? (
        <p role="status">Loading history…</p>
      ) : history.isError ? (
        <p role="alert">
          Could not load history.{" "}
          <Button onClick={() => void history.refetch()}>Retry history</Button>
        </p>
      ) : (
        <>
          {history.data?.pages
            .flatMap((p) => p.data)
            .map((row) => (
              <p key={`${row.id}:${row.version}`}>
                Version {row.version} · {labels[row.status]} · {formatDateTime(row.created_at)}
              </p>
            ))}
          <LoadMore
            hasMore={history.hasNextPage}
            isLoading={history.isFetching}
            onClick={() => void history.fetchNextPage({ cancelRefetch: false })}
          />
        </>
      )}
      <h3 className="font-semibold">Decisions</h3>
      {decisions.isPending ? (
        <p role="status">Loading decisions…</p>
      ) : decisions.isError ? (
        <p role="alert">
          Could not load decisions.{" "}
          <Button onClick={() => void decisions.refetch()}>Retry decisions</Button>
        </p>
      ) : (
        <>
          {!decisions.data?.pages.some((p) => p.data.length) && <p>No decisions recorded.</p>}
          {decisions.data?.pages
            .flatMap((p) => p.data)
            .map((row) => (
              <div key={row.id}>
                <p>
                  {row.decision} · {row.actor_subject} · Version {row.result_version} ·{" "}
                  {formatDateTime(row.created_at)}
                </p>
                <p>{row.reason}</p>
              </div>
            ))}
          <LoadMore
            hasMore={decisions.hasNextPage}
            isLoading={decisions.isFetching}
            onClick={() => void decisions.fetchNextPage({ cancelRefetch: false })}
          />
        </>
      )}
    </div>
  );
}
