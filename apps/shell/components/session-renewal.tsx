"use client";

import { useEffect } from "react";
import { usePathname } from "next/navigation";
import { useApiQuery } from "@helivanta/api";
import { renewSession, RENEWAL_INTERVAL_MS } from "@/lib/renew";
import { AUTH_CALLBACK_PATH, SILENT_RENEW_PATH } from "@/lib/oidc";

type Membership = { tenant_id: string; roles: string[]; current: boolean };

// PUBLIC_PATHS mirrors middleware.ts's own list, minus "/login" (which
// this component also skips, for the same "no session yet" reason) — kept
// as a literal array here rather than imported, since middleware.ts runs
// in the Edge runtime and this component runs in the browser; sharing a
// constant across that boundary is not worth the coupling for two short
// path prefixes.
function isPublicPath(pathname: string): boolean {
  return (
    pathname.startsWith("/login") ||
    pathname.startsWith(AUTH_CALLBACK_PATH) ||
    pathname.startsWith(SILENT_RENEW_PATH)
  );
}

// SessionRenewal runs the D4a renewal loop for every page the shell app
// itself serves. It is a component, not a bare effect in layout.tsx,
// specifically so it can be skipped on /login and the two auth routes —
// mounting it there would try to renew a session that does not exist yet
// and race the very login flow those pages are running.
//
// Scope, stated plainly: this only runs while the browser has a shell
// page open. apps/medicore, apps/pharmacy and apps/lab are separate
// Next.js apps (docs/superpowers/spikes doesn't cover this; it follows
// the same shell-only boundary tenantPicker/onSignOut already draw in
// packages/ui/src/hms-shell.tsx, since only the shell carries the Zitadel
// client config) and do not run this loop themselves. A clinician who
// stays on a zone page for longer than SessionTTL without returning to a
// shell-rendered page will hit an expired session on their next API call
// and be redirected to /login by middleware.ts — the existing, unchanged
// fail-closed behaviour, not a new regression. Extending renewal to the
// zone apps is a real follow-on, out of scope here.
export function SessionRenewal() {
  const pathname = usePathname();
  const skip = isPublicPath(pathname);

  const { data } = useApiQuery<{ data: Membership[] }>(
    ["iam", "me", "tenants"],
    "/iam/me/tenants",
    { poll: false, enabled: !skip },
  );
  const currentTenantId = data?.data.find((m) => m.current)?.tenant_id;

  useEffect(() => {
    if (skip || !currentTenantId) return;

    let cancelled = false;
    const interval = setInterval(() => {
      renewSession(currentTenantId).catch(() => {
        // D4a's stated fallback: a deactivated account, a revoked
        // membership, or an unreachable Zitadel all surface here
        // identically — renewal cannot distinguish them, and must not
        // try to (that would be a second place holding "is this account
        // still good", the exact drift spec D3 of the tenancy topology
        // design warns against). Any failure means visible re-auth.
        if (!cancelled) window.location.href = "/login";
      });
    }, RENEWAL_INTERVAL_MS);

    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [skip, currentTenantId]);

  return null;
}
