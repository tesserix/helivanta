"use client";

import { useState } from "react";
import { Button } from "@tesserix/web";
import { PageHeading, RequestFailure } from "@helivanta/ui";
import { useApiQuery, useTenantTransition, type PermissionsResponse } from "@helivanta/api";
import { PharmacyBindingWorkspace } from "../pharmacy-binding-workspace";

export default function PharmacyRelationships() {
  const switching = useTenantTransition();
  const [attempt, setAttempt] = useState(0);
  return (
    <>
      <PageHeading description="Inspect hospital pharmacy department relationships and their retained decisions." />
      {switching ? (
        <p role="status">Switching pharmacy relationship workspace…</p>
      ) : (
        <RelationshipAccount key={attempt} restart={() => setAttempt((value) => value + 1)} />
      )}
    </>
  );
}

function RelationshipAccount({ restart }: { restart: () => void }) {
  const [nonce] = useState(() => crypto.randomUUID());
  const [changed, setChanged] = useState(false);
  const [busy, setBusy] = useState(false);
  const identity = useApiQuery<PermissionsResponse>(
    ["pharmacy", "relationship-account", nonce],
    "/iam/me/permissions",
    {
      gcTime: 0,
      staleTime: 0,
      enabled: !busy,
      refetchOnWindowFocus: !busy,
      request: { cache: "no-store" },
    },
  );
  if (changed)
    return (
      <section role="alert" className="space-y-4">
        <p>Staff access or your account changed. Reopen the workspace to verify current access.</p>
        <Button onClick={restart}>Reopen pharmacy relationships</Button>
      </section>
    );
  if (identity.isPending || (identity.isFetching && !busy))
    return <p role="status">Checking pharmacy relationship account…</p>;
  if (identity.isError || !identity.data?.tenant_id || !identity.data.subject)
    return (
      <RequestFailure
        message="Your account could not be verified."
        retry={identity.refetch}
        // A malformed answer has no error, so it keeps the retry.
        error={identity.error}
      />
    );
  return (
    <PharmacyBindingWorkspace
      onBusyChanged={setBusy}
      scope={{ tenantId: identity.data.tenant_id, subject: identity.data.subject }}
      onContextChanged={() => setChanged(true)}
    />
  );
}
