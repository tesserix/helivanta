"use client";

import { formatPostingUtc } from "./posting-time";
import { Button } from "@tesserix/web";
import { Panel, useConfirmLeave } from "@helivanta/ui";
import {
  useStaffPostingLifecycleHistoryContext,
  type StaffPostingProposal,
  type StaffPostingProposalScope,
} from "@helivanta/api";
import { PostingProposalContextDetails } from "./posting-proposal-context-details";
import { useStaffAccessCutoff, STAFF_ACCESS_CHANGED } from "./staff-access-cutoff";

export function PostingApprovalHistory({
  scope,
  proposal,
  displayName,
  onClose,
  onContextChanged,
}: {
  scope: StaffPostingProposalScope;
  proposal: StaffPostingProposal;
  displayName: string;
  onClose: () => void;
  onContextChanged: () => void;
}) {
  const history = useStaffPostingLifecycleHistoryContext(scope, proposal);
  const cutoff = useStaffAccessCutoff([history.error], onContextChanged);
  const confirmLeave = useConfirmLeave();
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  return (
    <Panel className="space-y-4" aria-label="Selected posting approval">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 className="font-semibold">Approved posting history</h3>
        <Button variant="ghost" onClick={() => confirmLeave(onClose)}>
          Close proposal
        </Button>
      </div>
      {history.isPending || history.isFetching ? (
        <p role="status">Checking protected approval history…</p>
      ) : history.isError || !history.data ? (
        <div role="alert">
          The retained approval history could not be verified.{" "}
          <Button onClick={() => void history.refetch()}>Retry retained history</Button>
        </div>
      ) : (
        <>
          <p className="font-medium">{displayName}</p>
          <section aria-label="Retained lifecycle approval" className="space-y-4">
            <p className="text-sm text-muted-foreground">
              This is the evidence retained at approval. It does not establish current operating
              authority.
            </p>
            {history.data.review ? (
              <>
                <dl className="grid gap-3 text-sm sm:grid-cols-2">
                  <div>
                    <dt className="text-muted-foreground">Sources examined at approval (UTC)</dt>
                    <dd>{formatPostingUtc(history.data.review.checked_at)}</dd>
                  </div>
                  <div>
                    <dt className="text-muted-foreground">
                      Effective deadline recorded at approval (UTC)
                    </dt>
                    <dd className="font-semibold">
                      {formatPostingUtc(history.data.review.effective_until)}
                    </dd>
                  </div>
                </dl>
                <ol className="space-y-3" aria-label="Sources retained at approval">
                  {history.data.review.sources.map((source) => (
                    <Panel as="li" key={source.proposal_id} className="space-y-2 text-sm">
                      <h4 className="font-medium">
                        {source.hop === 0
                          ? "Selected posting at approval"
                          : `Supervisor source ${source.hop}`}
                      </h4>
                      <p>
                        {source.cutoff.version === 0
                          ? "No retained cutoff was observed at approval."
                          : `${formatPostingUtc(source.cutoff.cutoff_at!)} · Cutoff version ${source.cutoff.version}`}
                      </p>
                      <details>
                        <summary className="cursor-pointer text-muted-foreground">
                          Retained source references
                        </summary>
                        <dl className="mt-2 grid gap-2 sm:grid-cols-2">
                          {[
                            ["Staff", source.staff_id],
                            ["Posting", source.proposal_id],
                            ["Examined revision", source.revision_id],
                            ...(source.cutoff.id ? [["Cutoff", source.cutoff.id]] : []),
                          ].map(([label, value]) => (
                            <div key={label} className="min-w-0">
                              <dt className="text-muted-foreground">{label}</dt>
                              <dd className="break-all">{value}</dd>
                            </div>
                          ))}
                        </dl>
                      </details>
                    </Panel>
                  ))}
                </ol>
                <details>
                  <summary className="cursor-pointer text-sm text-muted-foreground">
                    Retained review authority
                  </summary>
                  <dl className="mt-2 grid gap-3 text-sm sm:grid-cols-2">
                    {[
                      ["Approval revision", history.data.review.revision_id],
                      ["Original proposal revision", history.data.review.initial_revision_id],
                      ["Reviewer Staff", history.data.review.reviewer_staff_id],
                      ["Reviewed login link", history.data.review.reviewer_link_id],
                      [
                        "Reviewer employment generation",
                        history.data.review.reviewer_generation_id,
                      ],
                      [
                        "Review assignment",
                        `${history.data.review.review_assignment_id} · Version ${history.data.review.review_assignment_version}`,
                      ],
                      [
                        "Supporting review",
                        history.data.review.support_permission ===
                        "workforce.posting.management.review"
                          ? "Hospital management review"
                          : "Posting source read",
                      ],
                      [
                        "Support assignment",
                        `${history.data.review.support_assignment_id} · Version ${history.data.review.support_assignment_version}`,
                      ],
                      ["Scope epoch", String(history.data.review.scope_epoch)],
                      ["Membership epoch", String(history.data.review.membership_epoch)],
                    ].map(([label, value]) => (
                      <div key={label} className="min-w-0">
                        <dt className="text-muted-foreground">{label}</dt>
                        <dd className="break-all">{value}</dd>
                      </div>
                    ))}
                  </dl>
                </details>
              </>
            ) : (
              <p>
                No retained lifecycle proof is available for this approval. Current observations
                have not been substituted.
              </p>
            )}
          </section>
          <PostingProposalContextDetails context={history.data.context} />
          <Button variant="outline" onClick={() => void history.refetch()}>
            Recheck history access
          </Button>
        </>
      )}
    </Panel>
  );
}
