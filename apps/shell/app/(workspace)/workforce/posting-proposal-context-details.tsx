import { formatPostingUtc } from "./posting-time";
import type { StaffPostingProposalContext } from "@helivanta/api";

export function PostingProposalContextDetails({
  context,
}: {
  context: StaffPostingProposalContext;
}) {
  return (
    <dl className="grid gap-4 sm:grid-cols-2">
      <div>
        <dt className="text-sm text-muted-foreground">Draft rationale</dt>
        <dd className="break-words">{context.draft_reason}</dd>
      </div>
      <div>
        <dt className="text-sm text-muted-foreground">Submission rationale</dt>
        <dd className="break-words">{context.submission_reason}</dd>
      </div>
      {context.decision_reason && (
        <div>
          <dt className="text-sm text-muted-foreground">Decision rationale</dt>
          <dd className="break-words">{context.decision_reason}</dd>
        </div>
      )}
      <div>
        <dt className="text-sm text-muted-foreground">Submitted department reference</dt>
        <dd className="break-all">{context.proposal.department_id}</dd>
      </div>
      <div>
        <dt className="text-sm text-muted-foreground">Submitted department revision</dt>
        <dd className="break-all">{context.proposal.department_revision_id}</dd>
      </div>
      <div>
        <dt className="text-sm text-muted-foreground">Proposed placement (UTC)</dt>
        <dd>
          {formatPostingUtc(context.proposal.starts_at)} ·{" "}
          {context.proposal.ends_at ? formatPostingUtc(context.proposal.ends_at) : "Ongoing"}
        </dd>
      </div>
    </dl>
  );
}
