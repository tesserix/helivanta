"use client";

import { ShieldCheck } from "lucide-react";
import { Button } from "@tesserix/web";
import { useSupplyPlatformIdentity, type SupplyRegistryScope } from "@helivanta/api";
import { formatDateTime } from "@helivanta/ui";
import { RequestFailure } from "../request-failure";

export function PlatformIdentity({
  scope,
  partyVersion,
}: {
  scope: SupplyRegistryScope;
  partyVersion: number;
}) {
  const check = useSupplyPlatformIdentity(scope, partyVersion);
  return (
    <section
      className="space-y-4 rounded-2xl border bg-card p-5 sm:p-6"
      aria-label="Workspace identity"
    >
      <div className="flex items-start gap-3">
        <ShieldCheck className="mt-0.5 size-5 shrink-0 text-(--hms-accent)" aria-hidden="true" />
        <div className="space-y-2">
          <h4 className="font-semibold">Workspace identity</h4>
          <p className="max-w-2xl text-sm leading-6 text-muted-foreground">
            Confirm this workspace has an active platform account and product access before
            continuing organisation onboarding.
          </p>
        </div>
      </div>
      {check.isFetching ? (
        <p role="status" className="text-sm text-muted-foreground">
          Checking workspace identity…
        </p>
      ) : check.isError ? (
        <RequestFailure
          message="Workspace identity could not be confirmed. Try again or contact your platform administrator."
          retry={check.refetch}
        />
      ) : check.data ? (
        <div className="space-y-4">
          <p role="status" className="text-sm font-medium">
            Workspace identity confirmed
          </p>
          <dl className="grid gap-4 text-sm sm:grid-cols-2">
            <div className="min-w-0">
              <dt className="text-muted-foreground">Platform organisation</dt>
              <dd className="mt-1 break-all font-medium">{check.data.identity.organization_id}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Checked at</dt>
              <dd className="mt-1 font-medium">
                {formatDateTime(check.data.identity.verified_at)}
              </dd>
            </div>
          </dl>
          <Button type="button" variant="outline" onClick={() => void check.refetch()}>
            Check again
          </Button>
        </div>
      ) : (
        <Button type="button" variant="outline" onClick={() => void check.refetch()}>
          Check workspace identity
        </Button>
      )}
      <div className="rounded-xl border bg-muted/30 p-4 text-sm leading-6">
        <p className="font-medium">Trading approval is separate</p>
        <p className="mt-1 text-muted-foreground">
          A confirmed platform account does not approve this legal organisation, its licences, a
          trading relationship or access to another hospital’s records. Those checks remain
          required.
        </p>
      </div>
    </section>
  );
}
