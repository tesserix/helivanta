"use client";
import { useState } from "react";
import { Button } from "@tesserix/web";
import { PageHeading, RequestFailure } from "@helivanta/ui";
import { useApiQuery, useTenantTransition, type PermissionsResponse } from "@helivanta/api";
import { StaffPostingWorkspace } from "../posting-workspace";
export default function StaffPostings() {
  const switching = useTenantTransition();
  const [attempt, setAttempt] = useState(0);
  return switching ? (
    <p role="status">Switching posting workspace…</p>
  ) : (
    <PostingAccount key={attempt} restart={() => setAttempt((value) => value + 1)} />
  );
}
function PostingAccount({ restart }: { restart: () => void }) {
  const [nonce] = useState(() => crypto.randomUUID());
  const [changed, setChanged] = useState(false);
  const identity = useApiQuery<PermissionsResponse>(
    ["workforce", "posting-account", nonce],
    "/iam/me/permissions",
    { gcTime: 0, refetchOnWindowFocus: false, request: { cache: "no-store" } },
  );
  if (changed)
    return (
      <section role="alert" className="space-y-4">
        <p>
          Posting access or your account changed. Reopen the workspace to verify current access.
        </p>
        <Button onClick={restart}>Reopen posting workspace</Button>
      </section>
    );
  if (identity.isPending) return <p role="status">Checking posting workspace…</p>;
  if (identity.isError || !identity.data?.tenant_id || !identity.data.subject)
    return (
      <RequestFailure
        message="Your posting account could not be verified."
        retry={identity.refetch}
        // A malformed answer has no error, so it keeps the retry.
        error={identity.error}
      />
    );
  return (
    <>
      <PageHeading description="Read and prepare hospital department placement drafts." />
      <StaffPostingWorkspace
        key={`${identity.data.tenant_id}:${identity.data.subject}`}
        scope={{ tenantId: identity.data.tenant_id, subject: identity.data.subject }}
        onContextChanged={() => setChanged(true)}
      />
    </>
  );
}
