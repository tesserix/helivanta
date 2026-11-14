"use client";

import { useState, type ReactNode } from "react";
import { Button } from "@tesserix/web";
import { RequestFailure } from "@helivanta/ui";
import {
  useStaffPostingTransferChoices,
  useStaffPostingCutoffContext,
  useStaffPostingReviewDepartmentNames,
  type StaffPostingProposalScope,
  type StaffPostingProposal,
  type StaffPostingCutoffContext,
} from "@helivanta/api";

export function PostingTransferSelection({
  scope,
  replacement,
  busy,
  children,
}: {
  scope: StaffPostingProposalScope;
  replacement: StaffPostingProposal;
  busy: boolean;
  children: (old: StaffPostingCutoffContext) => ReactNode;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [chosen, setChosen] = useState<StaffPostingProposal | null>(null);
  const choices = useStaffPostingTransferChoices(scope, cursor, busy);
  const names = useStaffPostingReviewDepartmentNames(scope, []);
  if (chosen)
    return (
      <SelectedOriginal
        key={chosen.id}
        scope={scope}
        original={chosen}
        replacement={replacement}
        busy={busy}
        onChange={() => setChosen(null)}
      >
        {children}
      </SelectedOriginal>
    );
  if (choices.isPending || choices.isFetching)
    return <p role="status">Checking original placements…</p>;
  if (choices.isError)
    return (
      <RequestFailure
        error={choices.error}
        message="Original transfer placements could not be verified."
        retry={choices.refetch}
      />
    );
  const approved = choices.data.data.filter(
    (row) => row.status === "approved" && row.id !== replacement.id,
  );
  return (
    <section className="space-y-3" aria-label="Original transfer placement">
      <p>
        Select the original approved responsibility for this Staff member. It must still be usable
        when the server records the transfer. Other concurrent responsibilities are retained.
      </p>
      {approved.map((row) => (
        <Button key={row.id} variant="outline" disabled={busy} onClick={() => setChosen(row)}>
          Choose original: {names.get(row.department_id) ?? "Department name unavailable"} ·{" "}
          {new Date(row.starts_at).toLocaleString(undefined, { timeZoneName: "short" })}
        </Button>
      ))}
      {approved.length === 0 && <p>No approved original placements on this page.</p>}
      {choices.data.page.has_more && (
        <Button
          disabled={busy}
          variant="outline"
          onClick={() => setCursor(choices.data.page.next_cursor)}
        >
          Next original placements
        </Button>
      )}
      {cursor && (
        <Button disabled={busy} variant="outline" onClick={() => setCursor(null)}>
          First original placements
        </Button>
      )}
    </section>
  );
}
function SelectedOriginal({
  scope,
  original,
  replacement,
  busy,
  onChange,
  children,
}: {
  scope: StaffPostingProposalScope;
  original: StaffPostingProposal;
  replacement: StaffPostingProposal;
  busy: boolean;
  onChange: () => void;
  children: (old: StaffPostingCutoffContext) => ReactNode;
}) {
  const ending = useStaffPostingCutoffContext(scope, original, busy);
  return (
    <section className="space-y-4" aria-label="Coordinated transfer review">
      <p>
        The original responsibility will end exactly when the replacement starts:{" "}
        {new Date(replacement.starts_at).toLocaleString(undefined, { timeZoneName: "short" })}. The
        command commits both changes together and creates no operating access.
      </p>
      <p className="text-sm text-muted-foreground">
        Recorded transfers preserve both appointments. If plans change, ending the replacement does
        not undo the original ending; continuing in the original department requires a newly
        reviewed placement.
      </p>
      <Button type="button" variant="outline" disabled={busy} onClick={onChange}>
        Choose a different original placement
      </Button>
      {ending.isPending || ending.isFetching ? (
        <p role="status">Checking original ending history…</p>
      ) : ending.isError ? (
        <RequestFailure
          error={ending.error}
          message="Original ending authority or history could not be verified."
          retry={ending.refetch}
        />
      ) : (
        <>
          <p>
            Observed ending version: {ending.data.cutoff?.version ?? 0}
            {ending.data.cutoff &&
              ` · Existing ending ${new Date(ending.data.cutoff.cutoff_at).toLocaleString(undefined, { timeZoneName: "short" })}`}
          </p>
          {children(ending.data)}
        </>
      )}
    </section>
  );
}
