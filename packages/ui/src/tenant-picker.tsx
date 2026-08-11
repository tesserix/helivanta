"use client";

import { useApiMutation, useApiQuery, apiFetch } from "@hms/api";

type Membership = { tenant_id: string; roles: string[] };

/**
 * Lets a clinician working at more than one hospital switch tenants.
 * Renders nothing for single-tenant users, which is almost everyone.
 * The backend re-checks membership — this only offers the choices.
 */
export function TenantPicker() {
  const { data } = useApiQuery<{ data: Membership[] }>(["iam", "me", "tenants"], "/iam/me/tenants");
  const memberships = data?.data ?? [];

  const switchTenant = useApiMutation(
    (tenantId: string) =>
      apiFetch<{ tenant_id: string }>("/iam/me/tenant", {
        method: "POST",
        body: JSON.stringify({ tenant_id: tenantId }),
      }),
    {
      successToast: "Switched hospital",
      // A tenant switch changes every cached query, so reload rather than
      // trying to invalidate selectively.
      onSuccess: () => window.location.reload(),
    },
  );

  if (memberships.length < 2) return null;

  return (
    <label className="flex flex-col gap-1 px-3 py-2 text-xs">
      <span className="text-muted-foreground">Hospital</span>
      <select
        className="rounded-md border bg-transparent px-2 py-1 text-sm"
        onChange={(e) => switchTenant.mutate(e.target.value)}
        defaultValue={memberships[0].tenant_id}
      >
        {memberships.map((m) => (
          <option key={m.tenant_id} value={m.tenant_id}>
            {m.tenant_id.slice(0, 8)} — {m.roles.join(", ")}
          </option>
        ))}
      </select>
    </label>
  );
}
