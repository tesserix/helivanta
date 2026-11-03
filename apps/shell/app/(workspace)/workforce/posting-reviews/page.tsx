"use client";

import { useState } from "react";
import { Button } from "@tesserix/web";
import { PageHeading } from "@helivanta/ui";
import { useApiQuery, useTenantTransition, type PermissionsResponse } from "@helivanta/api";
import { StaffPostingReviewWorkspace } from "../posting-review-workspace";

export default function PostingReviews() {
  const switching = useTenantTransition();
  const [attempt, setAttempt] = useState(0);
  return (
    <>
      <PageHeading description="Examine proposed Staff department placements within your approved hospital scope." />
      {switching ? (
        <p role="status">Switching posting review workspace…</p>
      ) : (
        <PostingReviewAccount key={attempt} restart={() => setAttempt((value) => value + 1)} />
      )}
    </>
  );
}
function PostingReviewAccount({ restart }: { restart: () => void }) {
  const [nonce] = useState(() => crypto.randomUUID());
  const [changed, setChanged] = useState(false);
  const [busy, setBusy] = useState(false);
  const identity = useApiQuery<PermissionsResponse>(
    ["workforce", "posting-review-account", nonce],
    "/iam/me/permissions",
    {
      gcTime: 0,
      staleTime: 0,
      refetchOnWindowFocus: !busy,
      request: { cache: "no-store" },
    },
  );
  if (changed)
    return (
      <section role="alert" className="space-y-4">
        <p>
          Posting review access or your account changed. Reopen the workspace to verify current
          access.
        </p>
        <Button onClick={restart}>Reopen posting reviews</Button>
      </section>
    );
  if (identity.isPending || (identity.isFetching && !busy))
    return <p role="status">Checking posting review account…</p>;
  if (identity.isError || !identity.data?.tenant_id || !identity.data.subject)
    return (
      <section role="alert">
        Your posting review account could not be verified.{" "}
        <Button onClick={() => void identity.refetch()}>Retry posting review account</Button>
      </section>
    );
  return (
    <StaffPostingReviewWorkspace
      scope={{ tenantId: identity.data.tenant_id, subject: identity.data.subject }}
      onBusyChanged={setBusy}
      onContextChanged={() => setChanged(true)}
    />
  );
}
