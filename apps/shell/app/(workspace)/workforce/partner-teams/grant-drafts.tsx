"use client";
import { useEffect, useState } from "react";
import { Button } from "@tesserix/web";
import { LoadMore, Panel, RefreshButton, RequestFailure, useConfirmLeave } from "@helivanta/ui";
import {
  usePartnerGrantDrafts,
  type PartnerGrantDraft,
  type PartnerGrantDraftScope,
} from "@helivanta/api";
import { accessChanged } from "./errors";
import { PartnerGrantDraftForm } from "./grant-draft-form";

export function PartnerGrantDraftWorkspace({
  scope,
  onChanged,
}: {
  scope: PartnerGrantDraftScope;
  onChanged: () => void;
}) {
  const drafts = usePartnerGrantDrafts(scope);
  const [preparing, setPreparing] = useState(false);
  const [message, setMessage] = useState("");
  const confirm = useConfirmLeave();
  useEffect(() => {
    if (accessChanged(drafts.error)) onChanged();
  }, [drafts.error, onChanged]);
  const saved = (_draft: PartnerGrantDraft, recovered: boolean) => {
    setPreparing(false);
    setMessage(
      recovered
        ? "Supplier access proposal recovered. Verification pending; no external access is active."
        : "Supplier access proposal saved. Verification pending; no external access is active.",
    );
  };
  return (
    <section aria-label="Supplier access proposals" className="space-y-5">
      <h2 className="text-lg font-semibold">Supplier access preparation</h2>
      <p className="max-w-3xl text-sm text-muted-foreground">
        These are private supplier-owned proposals. Recipient eligibility, the bilateral
        relationship, disclosure and independent approvals remain to be verified. A proposal cannot
        authorize trade or share another customer’s records.
      </p>
      {message && <p role="status">{message}</p>}
      <div className="flex flex-wrap gap-3">
        <Button disabled={preparing} onClick={() => confirm(() => setPreparing(true))}>
          Prepare supplier access
        </Button>
        <RefreshButton
          error={drafts.error}
          busy={drafts.isFetching}
          refresh={() => drafts.refetch()}
        >
          Refresh access proposals
        </RefreshButton>
      </div>
      {preparing && (
        <PartnerGrantDraftForm
          scope={scope}
          onSaved={saved}
          onChanged={onChanged}
          onCancel={() => confirm(() => setPreparing(false))}
        />
      )}
      {drafts.isPending ? (
        <p role="status">Loading supplier access proposals…</p>
      ) : drafts.isError ? (
        <RequestFailure
          message="Supplier access proposals are unavailable."
          error={drafts.error}
          retry={drafts.refetch}
        />
      ) : (
        <>
          {(drafts.data?.pages.flatMap((page) => page.data) ?? []).map((draft) => (
            <Panel key={draft.id}>
              <h3 className="font-semibold">
                {draft.input.owner_product === "pharmacy"
                  ? "PharmaConnect outlet"
                  : "SupplyConnect premise"}{" "}
                ·{" "}
                {draft.input.recipient_kind === "group"
                  ? "Hospital team"
                  : "Named hospital account"}
              </h3>
              <p>Verification pending · No external access</p>
              <p className="text-sm">{draft.input.reason}</p>
              <p className="break-all text-sm text-muted-foreground">
                Proposal reference: {draft.id}
              </p>
            </Panel>
          ))}
          {drafts.data?.pages.every((page) => page.data.length === 0) && (
            <p>No supplier access proposals have been prepared.</p>
          )}
          <LoadMore
            hasMore={drafts.hasNextPage}
            isLoading={drafts.isFetchingNextPage}
            onClick={() => void drafts.fetchNextPage()}
            label="Load more supplier access proposals"
          />
        </>
      )}
    </section>
  );
}
