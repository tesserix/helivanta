"use client";

import { useApiMutation, useApiQuery, apiFetch, clearPermissionsCache } from "@helivanta/api";

type Membership = { tenant_id: string; roles: string[]; current: boolean };

type SwitchResponse = { tenant_id: string };

/**
 * Lets a clinician working at more than one hospital switch tenants.
 * Renders nothing for single-tenant users, which is almost everyone.
 *
 * This lives in the shell, not in `@helivanta/ui`, for the same reason
 * `onSignOut` does (see `packages/ui/src/hms-shell.tsx`) — historically
 * because completing a switch needed shell-owned Firebase client config;
 * that coupling is gone now (design spec D3: switching re-mints the HMS
 * session server-side, no client-side token exchange at all), but the
 * shell remains the natural owner since it is where the dashboard and the
 * rest of the auth surface already live.
 *
 * The switch is one call: `POST /iam/me/tenant` re-checks membership (it
 * is the authority; this control only offers the choices the caller's own
 * `/iam/me/tenants` listing already named) and, on success, re-mints the
 * session and replaces the `helivanta_session` cookie itself via the response's
 * Set-Cookie — the same way `POST /v1/auth/login` does for a fresh login.
 * There is no `custom_token` any more, and nothing for this component to
 * exchange: unlike the old GIP-backed design, a 200 here IS the switch.
 *
 * A non-member target tenant answers 404 (backend/internal/modules/iam/me.go,
 * reconciled to the codebase's standard cross-tenant rule: 404, never 403 —
 * a 403 would confirm the tenant exists). `useApiMutation`'s `onError`
 * surfaces that as a toast built from the response body's `message`
 * (`"tenant not found"`, byte-identical for a nonexistent tenant and one
 * the caller cannot access, so the toast text itself never distinguishes
 * them) — no special handling is needed here beyond letting the mutation's
 * default error path run: it never reloads, and the old session/tenant
 * stays exactly as it was.
 */
export function TenantPicker() {
  const { data } = useApiQuery<{ data: Membership[] }>(["iam", "me", "tenants"], "/iam/me/tenants");
  const memberships = data?.data ?? [];

  const switchTenant = useApiMutation(
    async (tenantId: string) => {
      const res = await apiFetch<SwitchResponse>("/iam/me/tenant", {
        method: "POST",
        body: JSON.stringify({ tenant_id: tenantId }),
      });
      return res.tenant_id;
    },
    {
      successToast: "Switched hospital",
      // A tenant switch changes every cached query, so reload rather than
      // trying to invalidate selectively. The permission cache lives in
      // localStorage and would survive that reload, so it is dropped
      // explicitly first — otherwise the new tenant's first paint would
      // show the old tenant's nav.
      onSuccess: () => {
        clearPermissionsCache();
        window.location.reload();
      },
    },
  );

  if (memberships.length < 2) return null;

  // The backend marks the one membership matching the caller's tenant_id
  // token claim `current: true` (see groupByTenant in
  // backend/internal/modules/iam/me.go) — that, not array order, is the
  // tenant the caller is actually in. `memberships[0]` is a last-resort
  // fallback for the render before the query settles, not a substitute:
  // trusting array order here was the bug (a picker that kept showing
  // the pre-switch tenant after a reload, and that couldn't fire a
  // change event to switch back to it because the DOM already matched).
  const currentTenantId = memberships.find((m) => m.current)?.tenant_id ?? memberships[0].tenant_id;

  return (
    <label className="flex items-center gap-2 text-xs">
      <span className="sr-only">Hospital</span>
      <select
        className="w-44 rounded-md border bg-transparent px-2 py-1 text-sm"
        onChange={(e) => switchTenant.mutate(e.target.value)}
        value={currentTenantId}
        title="Hospital"
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
