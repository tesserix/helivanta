"use client";

import { useState } from "react";
import { Button } from "@tesserix/web";
import { PageHeading } from "@helivanta/ui";
import { useApiQuery, useTenantTransition, type PermissionsResponse } from "@helivanta/api";
import { StaffPostingCutoffWorkspace } from "./posting-cutoff-workspace";

export function PostingCutoffPage() {
  const switching = useTenantTransition();
  const [attempt, setAttempt] = useState(0);
  return (
    <>
      <PageHeading description="Manage posting cutoff dates and retained history for your assigned hospitals." />
      {switching ? (
        <p role="status">Switching posting cutoff workspace…</p>
      ) : (
        <CutoffAccount key={attempt} restart={() => setAttempt((value) => value + 1)} />
      )}
    </>
  );
}

function CutoffAccount({ restart }: { restart: () => void }) {
  const [nonce] = useState(() => crypto.randomUUID());
  const [changed, setChanged] = useState(false);
  const [busy, setBusy] = useState(false);
  const identity = useApiQuery<PermissionsResponse>(
    ["workforce", "posting-cutoff-account", nonce],
    "/iam/me/permissions",
    { gcTime: 0, staleTime: 0, refetchOnWindowFocus: !busy, request: { cache: "no-store" } },
  );
  if (changed)
    return (
      <section role="alert" className="space-y-4">
        <p>
          Posting cutoff access or your account changed. Reopen the workspace to verify current
          access.
        </p>
        <Button onClick={restart}>Reopen posting cutoffs</Button>
      </section>
    );
  if (identity.isPending || (identity.isFetching && !busy))
    return <p role="status">Checking posting cutoff account…</p>;
  if (identity.isError || !identity.data?.tenant_id || !identity.data.subject)
    return (
      <section role="alert">
        Your posting cutoff account could not be verified.{" "}
        <Button onClick={() => void identity.refetch()}>Retry posting cutoff account</Button>
      </section>
    );
  return (
    <StaffPostingCutoffWorkspace
      scope={{ tenantId: identity.data.tenant_id, subject: identity.data.subject }}
      onBusyChanged={setBusy}
      onContextChanged={() => setChanged(true)}
    />
  );
}
