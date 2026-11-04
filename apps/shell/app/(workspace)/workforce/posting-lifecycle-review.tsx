"use client";

import { formatPostingUtc } from "./posting-time";
import { Panel } from "@helivanta/ui";
import type { StaffPostingLifecycleReviewContext } from "@helivanta/api";

export function PostingLifecycleReview({
  context,
}: {
  context: StaffPostingLifecycleReviewContext;
}) {
  return (
    <section aria-label="Current posting lifetimes" className="space-y-4">
      <dl className="grid gap-3 text-sm sm:grid-cols-2">
        <div>
          <dt className="text-muted-foreground">Effective responsibility ends (UTC)</dt>
          <dd className="font-semibold">{formatPostingUtc(context.effective_until)}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Sources checked (UTC)</dt>
          <dd>{formatPostingUtc(context.checked_at)}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Proposed source limit (UTC)</dt>
          <dd>{formatPostingUtc(context.context.proposal.eligibility_until)}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Proposed placement ends (UTC)</dt>
          <dd>
            {context.context.proposal.ends_at
              ? formatPostingUtc(context.context.proposal.ends_at)
              : "Ongoing"}
          </dd>
        </div>
      </dl>
      <p className="text-sm text-muted-foreground">
        The earliest placement, employment or retained ending applies. Approval rechecks every
        source and grants no operating access.
      </p>
      <p className="text-sm">
        Proposed posting:{" "}
        {context.cutoff.version === 0
          ? "No retained cutoff at this check."
          : formatPostingUtc(context.cutoff.cutoff_at!)}
      </p>
      {context.chain.length > 0 && (
        <section aria-label="Current supervisor chain" className="space-y-3">
          <h4 className="font-medium">Current supervisor chain</h4>
          <ol className="space-y-3">
            {context.chain.map(({ posting, cutoff }) => (
              <Panel as="li" key={posting.id} className="space-y-3 text-sm">
                <div>
                  <p className="font-medium">{posting.staff.display_name}</p>
                  <p className="text-muted-foreground">
                    {posting.staff.employee_code} ·{" "}
                    {posting.kind === "hospital_management"
                      ? "Hospital management"
                      : "Department supervisor"}
                  </p>
                </div>
                <dl className="grid gap-3 sm:grid-cols-2">
                  <div>
                    <dt className="text-muted-foreground">Placement (UTC)</dt>
                    <dd>
                      {formatPostingUtc(posting.starts_at)} ·{" "}
                      {posting.ends_at ? formatPostingUtc(posting.ends_at) : "Ongoing"}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-muted-foreground">Employment/source limit (UTC)</dt>
                    <dd>{formatPostingUtc(posting.eligibility_until)}</dd>
                  </div>
                  <div className="sm:col-span-2">
                    <dt className="text-muted-foreground">Retained ending (UTC)</dt>
                    <dd>
                      {cutoff.version === 0
                        ? "No retained cutoff at this check."
                        : `${formatPostingUtc(cutoff.cutoff_at!)} · Version ${cutoff.version}`}
                    </dd>
                  </div>
                </dl>
                <details>
                  <summary className="cursor-pointer text-muted-foreground">
                    Verified references
                  </summary>
                  <dl className="mt-2 grid gap-2 sm:grid-cols-2">
                    {[
                      ["Staff", posting.staff.id],
                      ["Posting", posting.id],
                      ["Reviewed revision", posting.revision_id],
                      ["Department", posting.department_id],
                      ...(cutoff.id ? [["Cutoff", cutoff.id]] : []),
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
        </section>
      )}
    </section>
  );
}
