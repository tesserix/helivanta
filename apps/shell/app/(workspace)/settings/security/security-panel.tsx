"use client";
import { useState } from "react";
import { z } from "zod";
import { useApiQuery, useOwnerSecurity, useTenantTransition } from "@helivanta/api";
import { Button, PageHeading, Panel, RequestFailure, RefreshButton } from "@helivanta/ui";
const identityContract = z.object({
  tenant_id: z.string().uuid(),
  subject: z.string().min(1).max(200),
  data: z.array(z.string()),
});

export function SecurityPanel() {
  const [nonce] = useState(() => crypto.randomUUID());
  const switching = useTenantTransition();
  const identity = useApiQuery<unknown>(["owner-security-identity", nonce], "/iam/me/permissions", {
    gcTime: 0,
    staleTime: 0,
    poll: true,
    request: { cache: "no-store" },
  });
  const parsed = identityContract.safeParse(identity.data);
  if (switching || identity.isPending) return <p role="status">Checking workspace owner access…</p>;
  if (identity.isError || !parsed.success)
    return <Panel role="alert">Could not verify workspace owner access.</Panel>;
  if (!parsed.data.data.includes("iam.member.manage"))
    return <Panel role="alert">Workspace owner access is required.</Panel>;
  const scope = { tenantId: parsed.data.tenant_id, subject: parsed.data.subject };
  return <OwnerSecurity key={`${scope.tenantId}:${scope.subject}`} scope={scope} />;
}
function OwnerSecurity({ scope }: { scope: { tenantId: string; subject: string } }) {
  const { query, reconcile } = useOwnerSecurity(scope);
  return (
    <div className="space-y-6">
      <PageHeading
        title="Workspace security"
        description="Manage the sign-in requirements for your organisation."
      />
      <Panel className="space-y-4">
        <h2 className="text-lg font-semibold">Multi-factor authentication</h2>
        <p>
          All workspace users must use MFA, including users signing in through an external provider.
          New users must enrol without a grace period.
        </p>
        {query.isPending ? (
          <p role="status">Checking identity requirements…</p>
        ) : query.isError || !query.data ? (
          <RequestFailure
            message="Could not verify workspace security."
            retry={query.refetch}
            error={query.error}
          />
        ) : (
          <>
            <p role="status" className="font-medium">
              {query.data.state === "applied"
                ? "Required MFA is active"
                : query.data.state === "pending"
                  ? "Waiting for identity confirmation"
                  : "MFA requirements need attention"}
            </p>
            {query.data.state === "pending" && (
              <p>
                The request is pending. This page checks the current identity policy before
                confirming it is active.
              </p>
            )}
            {query.data.canReconcile ? (
              <Button
                disabled={reconcile.isPending || query.data.state === "pending"}
                onClick={() => reconcile.mutate()}
              >
                {reconcile.isPending ? "Requesting…" : "Reapply required MFA"}
              </Button>
            ) : (
              <p>
                This workspace retains its existing managed policy. Contact your platform
                administrator to arrange a policy change.
              </p>
            )}
            <RefreshButton refresh={query.refetch} busy={query.isFetching} error={query.error}>
              Refresh security
            </RefreshButton>
          </>
        )}
        {reconcile.isError && (
          <p role="alert">
            Could not request the change. Refresh security, or sign out and sign in again if your
            authentication is older than five minutes.
          </p>
        )}
        <p className="text-sm text-muted-foreground">
          To register an authenticator, use the MFA enrolment step during sign-in. These workspace
          settings never display authenticator secrets or recovery codes.
        </p>
      </Panel>
    </div>
  );
}
