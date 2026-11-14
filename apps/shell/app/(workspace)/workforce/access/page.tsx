"use client";

import { useEffect, useState, useSyncExternalStore } from "react";
import { Button } from "@tesserix/web";
import {
  PageHeading,
  Panel,
  LoadMore,
  SelectControl,
  Field,
  RefreshButton,
  RequestFailure,
  formatDateTime,
} from "@helivanta/ui";
import {
  useApiQuery,
  useStaffAssignments,
  useStaffAssignmentHistory,
  useStaffAssignmentDecisions,
  useOrganizationDepartmentRevisions,
  type StaffAssignment,
  type StaffAssignmentDecision,
  type StaffAssignmentScope,
  type StaffAssignmentFamily,
  staffAccessTemplates,
  staffAccessTemplateCatalogs,
  TemplateDefinitionContract,
} from "@helivanta/api";
import { AssignmentProposalForm, AssignmentDecisionForm } from "./assignment-forms";
import { GovernanceAssignments } from "./governance-assignments";
import { GovernanceInbox } from "@/components/workforce/governance-inbox";
import { StaffAccountInitiation } from "@/components/workforce/staff-account-initiation";
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
        <RequestFailure
          message="Could not verify assignment access."
          retry={identity.refetch}
          // A malformed answer has no error, so it keeps the retry.
          error={identity.error}
        />
      ) : !identity.data.data.some(
          (p) =>
            p === "iam.assignment.manage" ||
            p === "iam.assignment.review" ||
            p === "iam.governance.assignment.manage" ||
            p === "iam.governance.assignment.review" ||
            p === "iam.member.manage" ||
            p === "iam.governance.manage",
        ) ? (
        <p>Assignment administration is not available for this account.</p>
      ) : (
        <div key={`${identity.data.tenant_id}:${identity.data.subject}`}>
          {identity.data.data.includes("iam.governance.manage") &&
            identity.data.data.includes("iam.governance.read") && (
              <GovernanceInbox
                scope={{ tenantId: identity.data.tenant_id, subject: identity.data.subject }}
              />
            )}
          {identity.data.data.includes("iam.member.manage") && (
            <>
              <StaffAccountInitiation
                scope={{ tenantId: identity.data.tenant_id, subject: identity.data.subject }}
              />
              <StaffMembershipForm
                scope={{ tenantId: identity.data.tenant_id, subject: identity.data.subject }}
              />
              <MembershipManagement
                scope={{ tenantId: identity.data.tenant_id, subject: identity.data.subject }}
              />
            </>
          )}
          {identity.data.data.some(
            (p) => p === "iam.assignment.manage" || p === "iam.assignment.review",
          ) ? (
            <Assignments identity={identity.data} />
          ) : (
            identity.data.data.some(
              (p) =>
                p === "iam.governance.assignment.manage" ||
                p === "iam.governance.assignment.review",
            ) && (
              <GovernanceAssignments
                scope={{ tenantId: identity.data.tenant_id, subject: identity.data.subject }}
              />
            )
          )}
        </div>
      )}
    </>
  );
}

function useVerifiedStaffCatalog(
  scope: StaffAssignmentScope,
  catalogName: keyof typeof staffAccessTemplateCatalogs,
) {
  const catalog = useApiQuery<unknown>(
    ["iam", "staff-access-templates", scope.tenantId, scope.subject, catalogName],
    `/iam/staff-access-templates?catalog=${catalogName}`,
    {
      gcTime: 0,
      request: {
        cache: "no-store",
        headers: {
          "X-Helivanta-Expected-Tenant": scope.tenantId,
          "X-Helivanta-Expected-Subject": scope.subject,
        },
      },
    },
  );
  const definitions = TemplateDefinitionContract.array().safeParse(catalog.data);
  const catalogKeys = staffAccessTemplateCatalogs[catalogName];
  const supported =
    !catalog.isError &&
    !catalog.isFetching &&
    definitions.success &&
    definitions.data.length === catalogKeys.length &&
    catalogKeys.every((key) => {
      const expected = staffAccessTemplates.find((template) => template.key === key);
      const actual = definitions.data.find((template) => template.key === key);
      return (
        !!expected &&
        !!actual &&
        actual.name === expected.name &&
        actual.description === expected.description &&
        actual.family === expected.family &&
        actual.permissions.length === expected.permissions.length &&
        expected.permissions.every((permission) => actual.permissions.includes(permission)) &&
        actual.scope_kinds.length === expected.scope_kinds.length &&
        expected.scope_kinds.every((kind) => actual.scope_kinds.includes(kind)) &&
        actual.establishment_kinds.length === expected.establishment_kinds.length &&
        expected.establishment_kinds.every((kind) => actual.establishment_kinds.includes(kind))
      );
    });
  return { ...catalog, supported };
}

function Assignments({ identity }: { identity: Identity }) {
  const scope: StaffAssignmentScope = { tenantId: identity.tenant_id, subject: identity.subject };
  const [family, setFamily] = useState<StaffAssignmentFamily>("leave");
  const catalog = useVerifiedStaffCatalog(scope, "hr-intake-v1");
  const employmentCatalog = useVerifiedStaffCatalog(scope, "employment-v1");
  const activationCatalog = useVerifiedStaffCatalog(scope, "employment-activation-v1");
  const postingCatalog = useVerifiedStaffCatalog(scope, "staff-postings-v1");
  const postingReviewCatalog = useVerifiedStaffCatalog(scope, "staff-posting-reviews-v1");
  const postingManagementCatalog = useVerifiedStaffCatalog(scope, "staff-posting-management-v1");
  const postingLifecycleCatalog = useVerifiedStaffCatalog(scope, "staff-posting-lifecycle-v1");
  const postingSourceCatalog = useVerifiedStaffCatalog(scope, "staff-posting-source-v1");
  const partnerMembershipCatalog = useVerifiedStaffCatalog(scope, "partner-membership-v1");
  const supplierAccessCatalog = useVerifiedStaffCatalog(scope, "hospital-supplier-access-v1");
  const pharmacyBindingCatalog = useVerifiedStaffCatalog(scope, "pharmacy-department-bindings-v1");
  const postingDepartmentCatalog = useVerifiedStaffCatalog(
    scope,
    "staff-department-posting-reviews-v1",
  );
  const leaveDepartmentCatalog = useVerifiedStaffCatalog(
    scope,
    "staff-department-leave-reviews-v1",
  );
  const postingCatalogName = postingDepartmentCatalog.supported
    ? "staff-department-posting-reviews-v1"
    : postingSourceCatalog.supported
      ? "staff-posting-source-v1"
      : postingLifecycleCatalog.supported
        ? "staff-posting-lifecycle-v1"
        : postingManagementCatalog.supported
          ? "staff-posting-management-v1"
          : postingReviewCatalog.supported
            ? "staff-posting-reviews-v1"
            : "staff-postings-v1";
  const postingSupported =
    postingCatalog.supported ||
    postingReviewCatalog.supported ||
    postingManagementCatalog.supported ||
    postingLifecycleCatalog.supported ||
    postingSourceCatalog.supported ||
    postingDepartmentCatalog.supported;
  const templateKeys =
    staffAccessTemplateCatalogs[
      family === "supplier_access"
        ? "hospital-supplier-access-v1"
        : family === "partner_membership"
          ? "partner-membership-v1"
          : family === "pharmacy_binding"
            ? "pharmacy-department-bindings-v1"
            : family === "posting"
              ? postingCatalogName
              : family === "intake"
                ? "hr-intake-v1"
                : family === "employment"
                  ? "employment-v1"
                  : family === "activation"
                    ? "employment-activation-v1"
                    : leaveDepartmentCatalog.supported
                      ? "staff-department-leave-reviews-v1"
                      : "legacy-v1"
    ];
  const intakeSupported = catalog.supported;
  const employmentSupported = employmentCatalog.supported;
  const activationSupported = activationCatalog.supported;
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
  const catalogBlocked =
    (family === "intake" && !intakeSupported) ||
    (family === "employment" && !employmentSupported) ||
    (family === "activation" && !activationSupported) ||
    (family === "posting" && !postingSupported) ||
    (family === "pharmacy_binding" && !pharmacyBindingCatalog.supported) ||
    (family === "partner_membership" && !partnerMembershipCatalog.supported) ||
    (family === "supplier_access" && !supplierAccessCatalog.supported);
  return (
    <Panel className="space-y-4">
      <p>
        {family === "supplier_access"
          ? "Supplier collaboration assignments let current hospital Staff open specifically approved supplier catalogues, shipments or invoices. Reviewers accept supplier offers for their hospital. These assignments do not approve their own access or grant supplier stock, clinical or financial decision authority."
          : family === "partner_membership"
            ? "Partner team membership roles read, prepare or independently review named hospital members against current Staff, account-link and posting sources. They cannot publish groups, self-enrol, grant supplier access, or operate stock, clinical or financial records."
            : family === "leave"
              ? "Active leave-review assignments enable the Review leave page when the account also has an independently approved staff link. Both must remain current. A department leave reviewer decides leave only for Staff whose every current posting is in that department or its sub-units. These assignments grant no hospital administration, payroll or clinical authority."
              : family === "pharmacy_binding"
                ? "Pharmacy relationship preparers and independent reviewers link a hospital-owned outlet to its canonical department. They require a current reviewed Staff account and separate outlet editor access. These assignments grant no dispensing, professional, clinical or private HR access."
                : family === "posting"
                  ? "Verified posting roles separate draft reading and preparation, proposal submission, independent review and hospital management attestation. Each requires a current reviewed digital Staff account. These assignments grant no operating or clinical access."
                  : family === "identity"
                    ? "Identity manager and reviewer assignments have separate permissions for one hospital. They require a current staff account link and grant no hospital administration, payroll or clinical authority."
                    : family === "employment"
                      ? "Employment reviewers independently examine appointment terms and selected evidence at one hospital. They require a current reviewed staff account link. Approval of terms does not activate employment, login access, payroll or clinical authority."
                      : family === "activation"
                        ? "Employment activators use independently approved terms for one hospital and require separate current enrollment authority and a reviewed staff account link. This assignment alone grants no login, clinical or payroll access."
                        : "HR intake administrators prepare Staff profiles, employment drafts and protected evidence at one hospital. They require a current reviewed staff account link and cannot approve employment, grant access, or manage payroll or clinical care."}
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
            ...(supplierAccessCatalog.supported || family === "supplier_access"
              ? [{ value: "supplier_access", label: "Supplier collaboration" }]
              : []),
            ...(partnerMembershipCatalog.supported || family === "partner_membership"
              ? [{ value: "partner_membership", label: "Partner team memberships" }]
              : []),
            ...(pharmacyBindingCatalog.supported || family === "pharmacy_binding"
              ? [{ value: "pharmacy_binding", label: "Pharmacy department relationships" }]
              : []),
            ...(intakeSupported || family === "intake"
              ? [{ value: "intake", label: "Staff onboarding" }]
              : []),
            ...(employmentSupported || family === "employment"
              ? [{ value: "employment", label: "Employment terms review" }]
              : []),
            ...(postingSupported || family === "posting"
              ? [{ value: "posting", label: "Staff posting access" }]
              : []),
            ...(activationSupported || family === "activation"
              ? [{ value: "activation", label: "Employment activation" }]
              : []),
          ]}
        />
      </Field>
      <p aria-live="polite" className="text-sm text-muted-foreground">
        {catalog.isPending
          ? "Checking Staff onboarding assignments…"
          : !intakeSupported
            ? "Staff onboarding assignments are unavailable in this workspace."
            : ""}
      </p>
      <p aria-live="polite" className="text-sm text-muted-foreground">
        {employmentCatalog.isPending
          ? "Checking employment review assignments…"
          : !employmentSupported
            ? "Employment review assignments are unavailable in this workspace."
            : ""}
      </p>
      <p aria-live="polite" className="text-sm text-muted-foreground">
        {!postingSupported &&
        (postingCatalog.isPending ||
          postingReviewCatalog.isPending ||
          postingManagementCatalog.isPending ||
          postingDepartmentCatalog.isPending)
          ? "Checking Staff posting assignments…"
          : !postingSupported
            ? "Staff posting assignments are unavailable in this workspace."
            : ""}
      </p>
      <div className="flex flex-wrap gap-2">
        <p aria-live="polite" className="w-full text-sm text-muted-foreground">
          {pharmacyBindingCatalog.isPending
            ? "Checking Pharmacy relationship assignments…"
            : !pharmacyBindingCatalog.supported
              ? "Pharmacy relationship assignments are unavailable in this workspace."
              : ""}
        </p>
        <p aria-live="polite" className="w-full text-sm text-muted-foreground">
          {activationCatalog.isPending
            ? "Checking employment activation assignments…"
            : !activationSupported
              ? "Employment activation assignments are unavailable in this workspace."
              : ""}
        </p>
        {/* Only the assignments read's refusal hides it: a refused catalog is
            shown as unavailable above and must still be refreshable. */}
        <RefreshButton
          busy={records.isFetching}
          error={records.error}
          refresh={() =>
            Promise.all([
              records.refetch(),
              catalog.refetch(),
              employmentCatalog.refetch(),
              activationCatalog.refetch(),
              postingCatalog.refetch(),
              postingReviewCatalog.refetch(),
              postingManagementCatalog.refetch(),
              pharmacyBindingCatalog.refetch(),
              supplierAccessCatalog.refetch(),
              partnerMembershipCatalog.refetch(),
              postingDepartmentCatalog.refetch(),
            ])
          }
        >
          Refresh assignments
        </RefreshButton>
        {manage && (
          <Button
            disabled={catalogBlocked}
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
      <fieldset disabled={catalogBlocked} className="min-w-0 space-y-4">
        {notice && <p role="status">{notice}</p>}
        {proposing && (
          <AssignmentProposalForm
            key={`${family}:${renewal?.id ?? "new"}`}
            family={family}
            templateKeys={templateKeys}
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
          <RequestFailure
            message="Could not load assignments."
            retry={records.refetch}
            error={records.error}
          />
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
      </fieldset>
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
      <AssignmentAppliesTo scope={scope} item={item} now={now} />
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

function AssignmentAppliesTo({
  scope,
  item,
  now,
}: {
  scope: StaffAssignmentScope;
  item: StaffAssignment;
  now: number;
}) {
  if (item.scope_kind === "hospital" || !item.input.department_id)
    return <p>Applies to: Whole hospital</p>;
  return (
    <AssignmentDepartment
      scope={scope}
      hospital={item.input.hospital_id}
      department={item.input.department_id}
      now={now}
    />
  );
}

function AssignmentDepartment({
  scope,
  hospital,
  department,
  now,
}: {
  scope: StaffAssignmentScope;
  hospital: string;
  department: string;
  now: number;
}) {
  // Revisions list newest first; a scheduled future revision is not yet the current name.
  const revisions = useOrganizationDepartmentRevisions(scope, hospital, department, null);
  const current = revisions.data?.data.find((row) => Date.parse(row.effective_at) <= now);
  if (revisions.isPending) return <p role="status">Loading department…</p>;
  if (!current)
    return (
      <RequestFailure
        message={`Applies to: department ${department}. Its current name could not be verified.`}
        retry={revisions.refetch}
        // A list with no current revision has no error, so it keeps the retry.
        error={revisions.error}
        // A refusal still names the scope the record applies to.
        refused={`Applies to: department ${department}. Its name isn't available to you.`}
      />
    );
  if (current.status !== "active")
    return (
      <p role="alert">
        Applies to: {current.name}. Department retired. This access no longer applies.
      </p>
    );
  return <p>Applies to: {current.name} and its current sub-units</p>;
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
        <RequestFailure
          message="Could not load history."
          retry={history.refetch}
          error={history.error}
        />
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
        <RequestFailure
          message="Could not load decisions."
          retry={decisions.refetch}
          error={decisions.error}
        />
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
