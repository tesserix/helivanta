"use client";

import { signInWithCustomToken } from "firebase/auth";
import { useApiMutation, useApiQuery, apiFetch } from "@hms/api";
import { firebaseAuth } from "@/lib/firebase";

type Membership = { tenant_id: string; roles: string[]; current: boolean };

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
 *     else, so this is the step that actually moves the user. Before
 *     trusting the result, `getIdTokenResult()` is checked against the
 *     `tenant_id` we asked to switch to: this environment cannot verify
 *     that Google Identity Platform's custom-token claim always wins over
 *     a persisted `customAttributes.tenant_id` on the account (see
 *     `scripts/seed-dev.mjs`, and production provisioning likely sets the
 *     same attribute) rather than being overridden by it. If GIP ever let
 *     the persisted attribute win, this step would silently mint a token
 *     for the OLD tenant — the exact bug this control replaces, just
 *     reintroduced one layer down. A mismatch here throws instead of
 *     proceeding, so that failure mode is loud (an error toast, no
 *     session POST, no reload) instead of a silent no-op switch.
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
      const idTokenResult = await cred.user.getIdTokenResult();
      if (idTokenResult.claims.tenant_id !== tenantId) {
        throw new Error(
          "Hospital switch failed: the new session did not carry the expected hospital. Please try again.",
        );
      }
      const res = await fetch("/api/session", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ idToken: idTokenResult.token }),
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
