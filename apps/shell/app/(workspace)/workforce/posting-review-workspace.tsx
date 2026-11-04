"use client";

import { useEffect, useState } from "react";
import { Button } from "@tesserix/web";
import { Field, Panel, SelectControl, useConfirmLeave } from "@helivanta/ui";
import {
  useStaffPostingReviewScopes,
  useStaffPostingProposalInbox,
  useStaffPostingSupervisionContext,
  useStaffPostingManagementReviewContext,
  type EnrollmentRequestScope,
  type StaffPostingInboxScope,
  type StaffPostingInboxItem,
  type StaffPostingProposal,
  type StaffPostingReviewHospitalScope,
} from "@helivanta/api";
import { PostingProposalDecision } from "./posting-proposal-decision";
import { PostingSubmissionWorkspace } from "./posting-submission-workspace";
import { useStaffAccessCutoff, STAFF_ACCESS_CHANGED } from "./staff-access-cutoff";

type Props = {
  scope: EnrollmentRequestScope;
  onContextChanged: () => void;
  onBusyChanged?: (busy: boolean) => void;
};
export function StaffPostingReviewWorkspace(props: Props) {
  return <PostingReviewAccount key={`${props.scope.tenantId}:${props.scope.subject}`} {...props} />;
}
function PostingReviewAccount({ scope, onContextChanged, onBusyChanged }: Props) {
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    onBusyChanged?.(busy);
  }, [busy, onBusyChanged]);
  useEffect(() => () => onBusyChanged?.(false), [onBusyChanged]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const hospitals = useStaffPostingReviewScopes(scope, cursor, busy);
  const cutoff = useStaffAccessCutoff([hospitals.error], onContextChanged);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (hospitals.isPending || (hospitals.isFetching && !busy))
    return <p role="status">Checking posting review hospitals…</p>;
  if ((hospitals.isError && !busy) || !hospitals.data)
    return (
      <div role="alert">
        Posting review access could not be verified.{" "}
        <Button onClick={() => void hospitals.refetch()}>Retry review hospitals</Button>
      </div>
    );
  const hospital = hospitals.data.data.find((row) => row.hospital_id === selected);
  return (
    <section className="space-y-5" aria-label="Posting reviews">
      <p className="max-w-3xl text-sm text-muted-foreground">
        Examine proposed department placements within your approved hospital scope. Proposals do not
        grant operating access.
      </p>
      {hospitals.isError && (
        <div role="alert">
          Posting review admission could not be rechecked. The retained command has not been
          discarded.{" "}
          <Button disabled={hospitals.isFetching} onClick={() => void hospitals.refetch()}>
            Recheck posting admission
          </Button>
        </div>
      )}
      <div className="flex flex-wrap gap-3">
        {hospitals.data.data.map((row) => (
          <Button
            key={row.hospital_id}
            variant={selected === row.hospital_id ? "default" : "outline"}
            disabled={busy}
            aria-label={`Open posting reviews for ${row.name}`}
            onClick={() => setSelected(row.hospital_id)}
          >
            {row.name} · {row.can_review ? "Reviewer" : "Submitter"}
          </Button>
        ))}
      </div>
      {hospitals.data.data.length === 0 && (
        <p>No approved posting review hospitals on this page.</p>
      )}
      {selected && !hospital && (
        <p role="alert">This hospital is no longer available. Reopen posting reviews.</p>
      )}
      {hospital && (
        <HospitalPostingReviews
          key={hospital.hospital_id}
          scope={{ ...scope, hospitalId: hospital.hospital_id, access: "scoped" }}
          hospital={hospital}
          busy={busy}
          onBusy={setBusy}
          onContextChanged={onContextChanged}
          admissionAvailable={hospitals.isSuccess && !hospitals.isFetching}
          onAdmissionRecheck={() => void hospitals.refetch()}
        />
      )}
      <div className="flex flex-wrap gap-2">
        {hospitals.data.page.has_more && (
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => {
              setSelected(null);
              setCursor(hospitals.data.page.next_cursor);
            }}
          >
            More review hospitals
          </Button>
        )}
        {cursor && (
          <Button
            variant="ghost"
            disabled={busy}
            onClick={() => {
              setSelected(null);
              setCursor(null);
            }}
          >
            First review hospitals
          </Button>
        )}
        <Button
          variant="outline"
          disabled={busy}
          onClick={() => {
            setSelected(null);
            void hospitals.refetch();
          }}
        >
          Refresh review access
        </Button>
      </div>
    </section>
  );
}
function HospitalPostingReviews({
  scope,
  hospital,
  busy,
  onBusy,
  onContextChanged,
  admissionAvailable,
  onAdmissionRecheck,
}: {
  scope: StaffPostingInboxScope;
  hospital: StaffPostingReviewHospitalScope;
  busy: boolean;
  onBusy: (value: boolean) => void;
  onContextChanged: () => void;
  admissionAvailable: boolean;
  onAdmissionRecheck: () => void;
}) {
  const [notice, setNotice] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [status, setStatus] = useState<StaffPostingProposal["status"]>("pending");
  const [cursor, setCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<StaffPostingInboxItem | null>(null);
  const inbox = useStaffPostingProposalInbox(scope, status, cursor, busy);
  const cutoff = useStaffAccessCutoff([inbox.error], onContextChanged);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (submitting && hospital.can_submit)
    return (
      <PostingSubmissionWorkspace
        scope={scope}
        admissionAvailable={admissionAvailable}
        onAdmissionRecheck={onAdmissionRecheck}
        busy={busy}
        onBusy={onBusy}
        onClose={() => setSubmitting(false)}
        onContextChanged={onContextChanged}
        onCompleted={() => {
          setSubmitting(false);
          setSelected(null);
          setNotice(
            "Posting proposal submitted for independent review. Operating authority is unchanged.",
          );
          void inbox.refetch();
        }}
      />
    );
  if (inbox.isPending || (inbox.isFetching && !busy))
    return <p role="status">Loading posting proposals…</p>;
  if (inbox.isError)
    return (
      <div role="alert">
        Posting proposals could not be verified.{" "}
        <Button onClick={() => void inbox.refetch()}>Retry proposals</Button>
      </div>
    );
  const current = busy
    ? selected
    : selected &&
      inbox.data.data.find(
        (item) =>
          item.proposal.id === selected.proposal.id &&
          item.proposal.revision_id === selected.proposal.revision_id,
      );
  return (
    <Panel className="space-y-5">
      {notice && <p role="status">{notice}</p>}
      {hospital.can_submit && (
        <Button
          disabled={busy}
          onClick={() => {
            setSelected(null);
            setSubmitting(true);
          }}
        >
          Submit prepared posting
        </Button>
      )}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="font-semibold">{hospital.name} · Posting proposals</h2>
        <Button
          variant="outline"
          disabled={busy}
          onClick={() => {
            setSelected(null);
            void inbox.refetch();
          }}
        >
          Refresh proposals
        </Button>
      </div>
      <Field id="posting-inbox-status" label="Proposal status">
        <SelectControl
          id="posting-inbox-status"
          value={status}
          disabled={busy}
          onValueChange={(value) => {
            setSelected(null);
            setCursor(null);
            setStatus(value as StaffPostingProposal["status"]);
          }}
          options={["pending", "rejected", "withdrawn", "approved"].map((value) => ({
            value,
            label: value[0].toUpperCase() + value.slice(1),
          }))}
        />
      </Field>
      <div className="grid gap-3 md:grid-cols-2">
        {inbox.data.data.map((item) => (
          <Panel key={item.proposal.id} className="space-y-3">
            <div>
              <h3 className="font-medium">{item.staff.display_name}</h3>
              <p className="text-sm text-muted-foreground">
                {item.staff.employee_code} · {item.proposal.status}
              </p>
            </div>
            <p className="text-sm">
              Starts {new Date(item.proposal.starts_at).toISOString()} ·{" "}
              {item.proposal.ends_at
                ? `Ends ${new Date(item.proposal.ends_at).toISOString()}`
                : "Ongoing"}
            </p>
            {((item.is_submitter && hospital.can_submit) ||
              (!item.is_submitter && hospital.can_review)) && (
              <Button
                variant="outline"
                disabled={busy}
                aria-label={`Open proposal for ${item.staff.display_name}`}
                onClick={() => setSelected(item)}
              >
                Open proposal
              </Button>
            )}
          </Panel>
        ))}
      </div>
      {inbox.data.data.length === 0 && <p>No {status} proposals on this page.</p>}
      {current && (
        <PostingProposalDetail
          key={`${current.proposal.id}:${current.proposal.revision_id}`}
          scope={scope}
          item={current}
          action={
            current.is_submitter
              ? hospital.can_submit
                ? "withdraw"
                : null
              : hospital.can_review
                ? "reject"
                : null
          }
          busy={busy}
          onBusy={onBusy}
          onCompleted={(row) => {
            setSelected(null);
            setNotice(
              row.status === "approved"
                ? "Posting proposal approved."
                : row.status === "rejected"
                  ? "Posting proposal rejected."
                  : "Posting proposal withdrawn.",
            );
            void inbox.refetch();
          }}
          onClose={() => setSelected(null)}
          onContextChanged={onContextChanged}
        />
      )}
      <div className="flex flex-wrap gap-2">
        {inbox.data.page.has_more && (
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => {
              setSelected(null);
              setCursor(inbox.data.page.next_cursor);
            }}
          >
            More proposals
          </Button>
        )}
        {cursor && (
          <Button
            variant="ghost"
            disabled={busy}
            onClick={() => {
              setSelected(null);
              setCursor(null);
            }}
          >
            First proposals
          </Button>
        )}
      </div>
    </Panel>
  );
}
function PostingProposalDetail({
  scope,
  item,
  action,
  busy,
  onBusy,
  onCompleted,
  onClose,
  onContextChanged,
}: {
  scope: StaffPostingInboxScope;
  item: StaffPostingInboxItem;
  action: "reject" | "withdraw" | null;
  busy: boolean;
  onBusy: (value: boolean) => void;
  onCompleted: (row: StaffPostingProposal) => void;
  onClose: () => void;
  onContextChanged: () => void;
}) {
  const [managementReview, setManagementReview] = useState(false);
  const supervision = useStaffPostingSupervisionContext(
    { ...scope, staffId: item.staff.id },
    item.proposal,
    item.is_submitter ? "submission" : "review",
    busy,
  );
  const management = useStaffPostingManagementReviewContext(
    { ...scope, staffId: item.staff.id },
    item.proposal,
    managementReview,
    busy,
  );
  const context = managementReview ? management : supervision;
  const confirmLeave = useConfirmLeave();
  const cutoff = useStaffAccessCutoff([context.error], onContextChanged);
  return (
    <Panel className="space-y-4" aria-label="Selected posting proposal">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 className="font-semibold">{item.staff.display_name} · Proposal details</h3>
        <Button variant="ghost" onClick={() => confirmLeave(onClose)}>
          Close proposal
        </Button>
      </div>
      {cutoff ? (
        <p role="alert">{STAFF_ACCESS_CHANGED}</p>
      ) : context.isPending || context.isFetching ? (
        <p role="status">Checking protected proposal context…</p>
      ) : context.isError ? (
        <div role="alert">
          The selected proposal could not be verified.{" "}
          <Button onClick={() => void context.refetch()}>Retry protected proposal</Button>
        </div>
      ) : (
        <>
          <Panel as="div" className="space-y-2">
            <h4 className="font-medium">Managerial responsibility</h4>
            <p>
              {context.data.supervision
                ? (
                    {
                      hospital_management: "Hospital management",
                      department_supervisor: "Department supervisor",
                      department_member: "Department member",
                    } as const
                  )[context.data.supervision.kind]
                : "Not specified"}
            </p>
            {context.data.supervision &&
              context.data.supervision.kind !== "hospital_management" && (
                <dl className="grid gap-3 text-sm sm:grid-cols-3">
                  <div className="min-w-0">
                    <dt className="text-muted-foreground">Requested supervisor Staff</dt>
                    <dd className="break-all">{context.data.supervision.supervisor_staff_id}</dd>
                  </div>
                  <div className="min-w-0">
                    <dt className="text-muted-foreground">Requested supervisor posting</dt>
                    <dd className="break-all">{context.data.supervision.supervisor_proposal_id}</dd>
                  </div>
                  <div className="min-w-0">
                    <dt className="text-muted-foreground">Requested supervisor revision</dt>
                    <dd className="break-all">{context.data.supervision.supervisor_revision_id}</dd>
                  </div>
                </dl>
              )}
            {context.data.supervision?.kind === "hospital_management" && (
              <p className="text-sm text-muted-foreground">
                Independent management review is required. Approval does not grant operating or
                clinical access.
              </p>
            )}
            {action === "reject" &&
              item.proposal.status === "pending" &&
              context.data.supervision?.kind === "hospital_management" &&
              !managementReview && (
                <Button
                  type="button"
                  variant="outline"
                  disabled={busy}
                  onClick={() => setManagementReview(true)}
                >
                  Review hospital management approval
                </Button>
              )}
            {managementReview && (
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={() => setManagementReview(false)}
              >
                Back to proposal review
              </Button>
            )}
          </Panel>
          <dl className="grid gap-4 sm:grid-cols-2">
            <div>
              <dt className="text-sm text-muted-foreground">Draft rationale</dt>
              <dd className="break-words">{context.data.context.draft_reason}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Submission rationale</dt>
              <dd className="break-words">{context.data.context.submission_reason}</dd>
            </div>
            {context.data.context.decision_reason && (
              <div>
                <dt className="text-sm text-muted-foreground">Decision rationale</dt>
                <dd className="break-words">{context.data.context.decision_reason}</dd>
              </div>
            )}
            <div>
              <dt className="text-sm text-muted-foreground">Submitted department reference</dt>
              <dd className="break-all">{context.data.context.proposal.department_id}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Submitted department revision</dt>
              <dd className="break-all">{context.data.context.proposal.department_revision_id}</dd>
            </div>
            <div>
              <dt className="text-sm text-muted-foreground">Proposed placement (UTC)</dt>
              <dd>
                {new Date(context.data.context.proposal.starts_at).toISOString()} ·{" "}
                {context.data.context.proposal.ends_at
                  ? new Date(context.data.context.proposal.ends_at).toISOString()
                  : "Ongoing"}
              </dd>
            </div>
          </dl>
        </>
      )}
      {context.data && !cutoff && (
        <PostingProposalDecision
          key={managementReview ? "management" : "ordinary"}
          scope={{ ...scope, staffId: item.staff.id }}
          proposal={context.data.context.proposal}
          contextAvailable={context.isSuccess && !context.isFetching}
          onContextRecheck={() => {
            void context.refetch();
          }}
          action={managementReview ? "approve" : action}
          busy={busy}
          onBusy={onBusy}
          onCompleted={onCompleted}
          onContextChanged={onContextChanged}
        />
      )}
    </Panel>
  );
}
