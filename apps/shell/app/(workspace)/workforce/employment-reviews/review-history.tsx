"use client";
import { Button } from "@tesserix/web";
import { Panel } from "@helivanta/ui";
import type { useEmploymentReviewHistory } from "@helivanta/api";

type Props = {
  history: ReturnType<typeof useEmploymentReviewHistory>;
  subject: string;
  hasPrevious: boolean;
  onPrevious: () => void;
  onNext: (cursor: string) => void;
};
export function RecordedEmploymentReviewHistory({
  history,
  subject,
  hasPrevious,
  onPrevious,
  onNext,
}: Props) {
  const next = history.data?.page.next_cursor;
  return (
    <Panel aria-label="Recorded employment review history" className="space-y-3">
      <h4 className="font-medium">Recorded review receipts</h4>
      <p className="text-sm text-muted-foreground">
        Historical receipts do not establish present effectiveness. Open the current review before
        another decision.
      </p>
      {history.isPending || history.isFetching ? (
        <p role="status">Loading protected review history…</p>
      ) : history.isError ? (
        <div role="alert">
          <p>Could not verify recorded review history.</p>
          <Button variant="outline" onClick={() => void history.refetch()}>
            Retry history
          </Button>
        </div>
      ) : (
        <>
          {history.data.data.length === 0 ? (
            <p>No recorded receipts are shown on this page.</p>
          ) : (
            <ol className="divide-y">
              {history.data.data.map((row) => (
                <li key={row.revision_id} className="space-y-1 py-3">
                  <p className="font-medium">
                    Version {row.version} · Recorded {row.status}
                  </p>
                  <p>{row.actor_subject === subject ? "You" : `Account ${row.actor_subject}`}</p>
                  <p className="text-xs text-muted-foreground">
                    {new Date(row.updated_at).toLocaleString()}
                  </p>
                  <p className="break-words">
                    {row.decision_reason || "Submission recorded for independent review."}
                  </p>
                  <details className="text-xs">
                    <summary>Receipt reference</summary>
                    <code className="break-all">{row.revision_id}</code>
                  </details>
                </li>
              ))}
            </ol>
          )}
          <div className="flex gap-2">
            {hasPrevious && (
              <Button variant="outline" onClick={onPrevious}>
                Previous history receipts
              </Button>
            )}
            {next && (
              <Button variant="outline" onClick={() => onNext(next)}>
                Next history receipts
              </Button>
            )}
          </div>
        </>
      )}
    </Panel>
  );
}
