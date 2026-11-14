"use client";
import { useState, useSyncExternalStore } from "react";
import { PageHeading, RequestFailure } from "@helivanta/ui";
import { useApiQuery, type OperatingAuthorityScope } from "@helivanta/api";
import { GovernanceOperatingAccess } from "@/components/workforce/governance-operating-access";
type Identity = { tenant_id: string; subject: string; data: string[] };
const subscribeHydration = () => () => {};
export default function OperatingAccessPage() {
  const hydrated = useSyncExternalStore(
    subscribeHydration,
    () => true,
    () => false,
  );
  const identity = useApiQuery<Identity>(["iam", "me", "permissions"], "/iam/me/permissions");
  const [heldScope, setHeldScope] = useState<OperatingAuthorityScope | null>(null);
  const value = identity.data;
  const current =
    hydrated &&
    value &&
    typeof value.tenant_id === "string" &&
    value.tenant_id &&
    typeof value.subject === "string" &&
    value.subject &&
    Array.isArray(value.data)
      ? { tenantId: value.tenant_id, subject: value.subject }
      : null;
  const scope = heldScope ?? current;
  const permission =
    Array.isArray(value?.data) &&
    value.data.some(
      (key) =>
        key === "iam.governance.operating.manage" || key === "iam.governance.operating.review",
    );
  const suspended =
    identity.isError ||
    !current ||
    !permission ||
    current.tenantId !== scope?.tenantId ||
    current.subject !== scope?.subject;
  return (
    <>
      <PageHeading
        title="Independent Staff access"
        description="Prepare and independently review pharmacy and supplier duties for current Staff accounts."
      />
      {!scope ? (
        identity.isPending ? (
          <p role="status">Verifying your current account…</p>
        ) : (
          <RequestFailure
            message="Your current account could not be verified."
            error={identity.error}
            retry={identity.refetch}
          />
        )
      ) : (
        <>
          {suspended && (
            <RequestFailure
              message="Current account access could not be confirmed. Restore your original account to recover an unconfirmed command."
              error={identity.error}
              retry={identity.refetch}
            />
          )}
          <GovernanceOperatingAccess
            key={`${scope.tenantId}:${scope.subject}`}
            scope={scope}
            accessSuspended={suspended}
            onBusyChange={(busy) => setHeldScope(busy ? scope : null)}
          />
        </>
      )}
    </>
  );
}
