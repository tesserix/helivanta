"use client";

import { useState } from "react";
import { Button } from "@tesserix/web";
import { Field, Panel, SelectControl } from "@helivanta/ui";
import {
  useStaffPostingReviewScopes,
  useStaffPostingProposalInbox,
  useStaffPostingProposalContext,
  type EnrollmentRequestScope,
  type StaffPostingInboxScope,
  type StaffPostingInboxItem,
  type StaffPostingProposal,
  type StaffPostingReviewHospitalScope,
} from "@helivanta/api";
import { useStaffAccessCutoff, STAFF_ACCESS_CHANGED } from "./staff-access-cutoff";

type Props = { scope: EnrollmentRequestScope; onContextChanged: () => void };
export function StaffPostingReviewWorkspace(props: Props) {
  return <PostingReviewAccount key={`${props.scope.tenantId}:${props.scope.subject}`} {...props} />;
}
function PostingReviewAccount({ scope, onContextChanged }: Props) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const hospitals = useStaffPostingReviewScopes(scope, cursor);
  const cutoff = useStaffAccessCutoff([hospitals.error], onContextChanged);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (hospitals.isPending || hospitals.isFetching)
    return <p role="status">Checking posting review hospitals…</p>;
  if (hospitals.isError)
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
      <div className="flex flex-wrap gap-3">
        {hospitals.data.data.map((row) => (
          <Button
            key={row.hospital_id}
            variant={selected === row.hospital_id ? "default" : "outline"}
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
          onContextChanged={onContextChanged}
        />
      )}
      <div className="flex flex-wrap gap-2">
        {hospitals.data.page.has_more && (
          <Button
            variant="outline"
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
  onContextChanged,
}: {
  scope: StaffPostingInboxScope;
  hospital: StaffPostingReviewHospitalScope;
  onContextChanged: () => void;
}) {
  const [status, setStatus] = useState<StaffPostingProposal["status"]>("pending");
  const [cursor, setCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<StaffPostingInboxItem | null>(null);
  const inbox = useStaffPostingProposalInbox(scope, status, cursor);
  const cutoff = useStaffAccessCutoff([inbox.error], onContextChanged);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (inbox.isPending || inbox.isFetching) return <p role="status">Loading posting proposals…</p>;
  if (inbox.isError)
    return (
      <div role="alert">
        Posting proposals could not be verified.{" "}
        <Button onClick={() => void inbox.refetch()}>Retry proposals</Button>
      </div>
    );
  const current =
    selected &&
    inbox.data.data.find(
      (item) =>
        item.proposal.id === selected.proposal.id &&
        item.proposal.revision_id === selected.proposal.revision_id,
    );
  return (
    <Panel className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="font-semibold">{hospital.name} · Posting proposals</h2>
        <Button
          variant="outline"
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
            {(item.is_submitter || hospital.can_review) && (
              <Button
                variant="outline"
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
          onClose={() => setSelected(null)}
          onContextChanged={onContextChanged}
        />
      )}
      <div className="flex flex-wrap gap-2">
        {inbox.data.page.has_more && (
          <Button
            variant="outline"
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
  onClose,
  onContextChanged,
}: {
  scope: StaffPostingInboxScope;
  item: StaffPostingInboxItem;
  onClose: () => void;
  onContextChanged: () => void;
}) {
  const context = useStaffPostingProposalContext(
    { ...scope, staffId: item.staff.id },
    item.proposal,
    item.is_submitter ? "submission" : "review",
  );
  const cutoff = useStaffAccessCutoff([context.error], onContextChanged);
  return (
    <Panel className="space-y-4" aria-label="Selected posting proposal">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 className="font-semibold">{item.staff.display_name} · Proposal details</h3>
        <Button variant="ghost" onClick={onClose}>
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
        <dl className="grid gap-4 sm:grid-cols-2">
          <div>
            <dt className="text-sm text-muted-foreground">Draft rationale</dt>
            <dd className="break-words">{context.data.draft_reason}</dd>
          </div>
          <div>
            <dt className="text-sm text-muted-foreground">Submission rationale</dt>
            <dd className="break-words">{context.data.submission_reason}</dd>
          </div>
          {context.data.decision_reason && (
            <div>
              <dt className="text-sm text-muted-foreground">Decision rationale</dt>
              <dd className="break-words">{context.data.decision_reason}</dd>
            </div>
          )}
          <div>
            <dt className="text-sm text-muted-foreground">Proposed placement (UTC)</dt>
            <dd>
              {new Date(context.data.proposal.starts_at).toISOString()} ·{" "}
              {context.data.proposal.ends_at
                ? new Date(context.data.proposal.ends_at).toISOString()
                : "Ongoing"}
            </dd>
          </div>
        </dl>
      )}
    </Panel>
  );
}
