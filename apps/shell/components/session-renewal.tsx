"use client";

import { useEffect } from "react";
import { usePathname } from "next/navigation";
import { renewSession, nextRenewalDelayMs, RenewalUnavailableError } from "@/lib/renew";
import { AUTH_CALLBACK_PATH } from "@/lib/oidc";

// PUBLIC_PATHS mirrors middleware.ts's own list, minus "/login" (which
// this component also skips, for the same "no session yet" reason) — kept
// as a literal here rather than imported, since middleware.ts runs in the
// Edge runtime and this component runs in the browser; sharing a
// constant across that boundary is not worth the coupling for a short
// path prefix.
function isPublicPath(pathname: string): boolean {
  return pathname.startsWith("/login") || pathname.startsWith(AUTH_CALLBACK_PATH);
}

// SessionRenewal runs the server-side renewal loop (design spec D1/D5,
// #916) for every page the shell app itself serves. It is a component,
// not a bare effect in layout.tsx, specifically so it can be skipped on
// /login and the auth callback route — mounting it there would try to
// renew a session that does not exist yet and race the very login flow
// those pages are running.
//
// Scope, stated plainly: this only runs while the browser has a shell
// page open. apps/medicore, apps/pharmacy and apps/lab are separate
// Next.js apps and do not run this loop themselves, the same shell-only
// boundary tenantPicker/onSignOut already draw in
// packages/ui/src/hms-shell.tsx. A clinician who stays on a zone page for
// longer than SessionTTL without returning to a shell-rendered page will
// hit an expired session on their next API call and be redirected to
// /login by middleware.ts — the existing, unchanged fail-closed
// behaviour, not a new regression. Extending renewal to the zone apps is
// a real follow-on, out of scope here.
//
// No cross-tab coordination (BroadcastChannel debounce, single-tab
// election, etc.) is added here, and that is a load-bearing choice, not
// an oversight: RenewRateLimitRule's Burst=10
// (backend/internal/bootstrap/ratelimit.go) is sized explicitly on the
// assumption that every open tab renews independently. Adding
// coordination here would invalidate that budget and require it to
// shrink back toward the activity endpoint's Burst=3 — a backend change
// this task does not make.
//
// --- The failure-handling policy (the judgement call this file turns
// on) ---
//
// POST /v1/auth/renew answers with one of three shapes, and this
// component must treat them differently:
//
//  1. Success — schedule the NEXT renewal from the server's `renew_at`
//     hint (spec D5), not a client-invented constant.
//  2. RenewalFailedError (401/404) — the backend has affirmatively
//     decided this session is over: the cookie is missing/invalid/
//     expired, the subject was deactivated in Zitadel, or tenant
//     membership was revoked. Re-authenticating visibly is correct.
//  3. RenewalUnavailableError (429/503/network failure) — the backend
//     could not answer the question, it did not answer "no". Critically,
//     renew.go's Zitadel/OpenFGA checks are fail-closed BY DESIGN (spec
//     D3): an outage on either dependency makes every renewal, from
//     every connected clinician, come back exactly like this. Treating
//     case 3 the same as case 2 — which is what this component used to
//     do, back when the only way renewal failed WAS an auth reason —
//     would mean a Zitadel or OpenFGA blip lasting longer than one
//     renewal interval evicts every signed-in clinician simultaneously,
//     mid-consultation. That is the SAME user-visible harm #916 exists
//     to prevent, reproduced from a different cause. So case 3 does NOT
//     redirect; it retries at the bounded fallback interval instead.
//
//     This is not an unbounded retry-forever: the session cookie this
//     endpoint runs on has its own `exp`, set at the last successful
//     renewal (or login) and never extended while renewals keep failing.
//     If the outage outlasts the cookie's remaining lifetime, the very
//     next renewal attempt naturally moves from "unreadable" to
//     "invalid/expired" and lands in case 2 instead — the backend's own
//     fail-closed bound (case 2) already gives this retry loop a ceiling
//     without this component needing to count attempts itself.
export function SessionRenewal() {
  const pathname = usePathname();
  const skip = isPublicPath(pathname);

  useEffect(() => {
    if (skip) return;

    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;

    const scheduleNext = (delayMs: number) => {
      timer = setTimeout(runRenewal, delayMs);
    };

    const runRenewal = () => {
      renewSession()
        .then(({ renewAt }) => {
          if (cancelled) return;
          scheduleNext(nextRenewalDelayMs(renewAt));
        })
        .catch((err: unknown) => {
          if (cancelled) return;
          if (err instanceof RenewalUnavailableError) {
            // See the policy comment above: retry, do not evict.
            scheduleNext(nextRenewalDelayMs(undefined));
            return;
          }
          // RenewalFailedError, or anything unexpected: treat as
          // "session is over" — fail closed toward visible re-auth
          // rather than silently keep a possibly-dead session running.
          // Neither RenewalFailedError nor an unexpected error carries
          // any data this branch needs beyond "log out".
          window.location.href = "/login";
        });
    };

    // First renewal has no prior server response to derive a cadence
    // from, so it uses the bounded fallback — the same value the old
    // hardcoded RENEWAL_INTERVAL_MS held.
    scheduleNext(nextRenewalDelayMs(undefined));

    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [skip]);

  return null;
}
