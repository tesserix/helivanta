"use client";

import { signInWithCustomToken } from "firebase/auth";
import { useApiMutation, useApiQuery, apiFetch } from "@hms/api";
import { firebaseAuth } from "@/lib/firebase";

type Membership = { tenant_id: string; roles: string[] };

type SwitchResponse = { tenant_id: string; custom_token: string };

/**
 * Lets a clinician working at more than one hospital switch tenants.
 * Renders nothing for single-tenant users, which is almost everyone.
 *
 * This lives in the shell, not in `@hms/ui`, because completing a switch
 * needs the Firebase client config (`lib/firebase.ts`) and the session
 * route (`app/api/session/route.ts`) — both shell-owned. A picker in the
 * shared package could render the control but not finish the exchange,
 * which is exactly the bug this replaces: a switcher that toasted
 * "Switched hospital", reloaded, and left the user in the old tenant
 * because nothing re-minted the session.
 *
 * The switch is three steps and all three must happen, in order:
 *  1. `POST /iam/me/tenant` — the backend re-checks membership (it is the
 *     authority; this control only offers the choices) and mints a custom
 *     token carrying the new `tenant_id` claim.
 *  2. `signInWithCustomToken` — exchanges it for a fresh ID token. The
 *     tenant a request runs in comes from that token's claim and nowhere
 *     else, so this is the step that actually moves the user.
 *  3. `POST /api/session` — replaces the `hms_session` cookie with the new
 *     ID token, the same handler `app/login/page.tsx` posts to after
 *     sign-in. Raw `fetch` here is the sanctioned session-route exception
 *     (docs/standards/frontend.md section 3) — it is an auth route outside
 *     the `/api/v1` envelope, so `apiFetch` does not apply.
 *
 * Any step throwing leaves the old session intact and surfaces the error
 * toast from `useApiMutation`; only a completed exchange reloads.
 */
export function TenantPicker() {
  const { data } = useApiQuery<{ data: Membership[] }>(["iam", "me", "tenants"], "/iam/me/tenants");
  const memberships = data?.data ?? [];

  const switchTenant = useApiMutation(
    async (tenantId: string) => {
      const { custom_token } = await apiFetch<SwitchResponse>("/iam/me/tenant", {
        method: "POST",
        body: JSON.stringify({ tenant_id: tenantId }),
      });
      const cred = await signInWithCustomToken(firebaseAuth(), custom_token);
      const idToken = await cred.user.getIdToken();
      const res = await fetch("/api/session", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ idToken }),
      });
      if (!res.ok) throw new Error("Could not start a session for that hospital.");
      return tenantId;
    },
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
