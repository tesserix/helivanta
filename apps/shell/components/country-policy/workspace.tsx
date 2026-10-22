"use client";

import { useState } from "react";
import { Button } from "@tesserix/web";
import { ChevronRight, Plus, ShieldCheck } from "lucide-react";
import {
  COUNTRY_POLICY_ROOT,
  countryPolicyKey,
  countryPolicyRequest,
  countryProfile,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type CountryPolicyIdentity,
  type CountryPolicyScope,
} from "@helivanta/api";
import { LoadMore, PageHeading, Panel, RequestFailure } from "@helivanta/ui";
import { CountryPolicyEditor, policyLabel } from "./editor";
import { CountryPolicyDetail } from "./detail";

export function CountryPolicyWorkspace() {
  const identity = useApiQuery<{ tenant_id: string; subject: string }>(
    ["iam", "me", "permissions"],
    "/iam/me/permissions",
  );
  if (identity.isError)
    return (
      <RequestFailure message="Could not verify your active tenant." retry={identity.refetch} />
    );
  if (!identity.data) return <p role="status">Loading country policy workspace…</p>;
  return (
    <TenantCountryPolicies
      key={`${identity.data.tenant_id}:${identity.data.subject}`}
      identity={{ tenantId: identity.data.tenant_id, subject: identity.data.subject }}
    />
  );
}

function TenantCountryPolicies({ identity }: { identity: CountryPolicyIdentity }) {
  const { can } = usePermissions();
  const [selected, setSelected] = useState<string | null>(null);
  const policies = useApiPagedQuery<CountryPolicyScope>(
    countryPolicyKey(identity, "scopes"),
    COUNTRY_POLICY_ROOT,
    { enabled: can("workspace.country_policy.read"), request: countryPolicyRequest(identity) },
  );
  if (!can("workspace.country_policy.read"))
    return (
      <Panel>
        <h2 className="text-xl font-semibold">Country policy access required</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          Ask your administrator for country-policy permission and access to the appropriate scope.
        </p>
      </Panel>
    );
  return (
    <div className="space-y-6">
      <PageHeading description="The rules applied in each country, with their sources, who may change them and their review history." />
      <Panel as="div" className="flex items-start gap-3">
        <ShieldCheck className="mt-0.5 size-5 shrink-0 text-(--hms-accent)" />
        <p className="text-sm leading-6 text-muted-foreground">
          Approval applies only to the named country, jurisdiction, product category and operation.
          It does not verify an organisation's licence or authorise another country's transactions.
        </p>
      </Panel>
      <Panel aria-label="Country policy directory" flush>
        <div className="flex flex-wrap items-center justify-between gap-3 border-b p-5">
          <div>
            <h3 className="font-semibold">Your policy scopes</h3>
            <p className="mt-1 text-sm text-muted-foreground">
              Owned by you or explicitly shared with you.
            </p>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={policies.retry}>
              Refresh policies
            </Button>
            {can("workspace.country_policy.manage") && (
              <Button onClick={() => setSelected("new")}>
                <Plus className="me-2 size-4" />
                Create policy
              </Button>
            )}
          </div>
        </div>
        <div className="p-5">
          {policies.isPending ? (
            <p role="status">Loading country policies…</p>
          ) : policies.isError ? (
            <RequestFailure message="Could not load country policies." retry={policies.retry} />
          ) : policies.items.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No country policies have been configured for your access.
            </p>
          ) : (
            <ul className="grid gap-3 lg:grid-cols-2">
              {policies.items.map((policy) => (
                <li key={policy.id}>
                  <Button
                    variant="outline"
                    className="h-auto w-full justify-between whitespace-normal p-4 text-start"
                    onClick={() => setSelected(policy.id)}
                  >
                    <span className="min-w-0 space-y-1">
                      <span className="block font-semibold">
                        {countryProfile(policy.country)?.name} · {policy.jurisdiction}
                      </span>
                      <span className="block text-xs font-normal text-muted-foreground">
                        {policyLabel(policy.category)} · {policyLabel(policy.operation)} ·{" "}
                        {policy.currency}
                      </span>
                      <span className="block text-xs font-normal">
                        {policy.status === "active"
                          ? "Approved configuration"
                          : policyLabel(policy.status)}{" "}
                        · {policyLabel(policy.access_role)}
                      </span>
                    </span>
                    <ChevronRight className="ms-3 size-4 shrink-0 rtl:rotate-180" />
                  </Button>
                </li>
              ))}
            </ul>
          )}
          <LoadMore
            hasMore={policies.hasMore}
            isLoading={policies.isFetchingMore}
            onClick={policies.loadMore}
          />
        </div>
      </Panel>
      {selected === "new" ? (
        <CountryPolicyEditor
          identity={identity}
          onSaved={(receipt) => setSelected(receipt.scope_id)}
          onCancel={() => setSelected(null)}
        />
      ) : selected ? (
        <CountryPolicyDetail
          key={selected}
          identity={identity}
          id={selected}
          onClose={() => setSelected(null)}
        />
      ) : null}
    </div>
  );
}
