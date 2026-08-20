"use client";

import { useEffect } from "react";
import { usePathname } from "next/navigation";
import { renewSession, nextRenewalDelayMs, RenewalUnavailableError } from "@/lib/renew";
import { clearPermissionsCache, clearRenewAt, loadRenewAt, storeRenewAt } from "@helivanta/api";
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
//  3. RenewalUnavailableError (429/408/any 5xx/network failure) — the
//     backend, or something in front of it, could not answer the
//     question, it did not answer "no". Critically, renew.go's
//     Zitadel/OpenFGA checks are fail-closed BY DESIGN (spec D3): an
//     outage on either dependency makes every renewal, from every
//     connected clinician, come back exactly like this — and the same
//     is true of the far more routine case of an ordinary rolling
//     deploy of the API, which the Next.js rewrite proxy turns into a
//     5xx from the shell's own server, never a network-level fetch
//     failure (see renew.ts's RenewalUnavailableError comment). Treating
//     case 3 the same as case 2 — which is what this component used to
//     do, back when the only way renewal failed WAS an auth reason —
//     would mean an outage OR a routine deploy, lasting longer than one
//     renewal interval, evicts every signed-in clinician simultaneously,
//     mid-consultation. That is the SAME user-visible harm #916 exists
//     to prevent, reproduced from a different cause. So case 3 does NOT
//     redirect; it retries at the bounded fallback interval instead.
//
//     The retry-termination bound differs by sub-case, and both are
//     worth stating precisely rather than one glossing the other:
//
//     - An HTTP-status failure (429/408/5xx — renewSession got a
//       response) is bounded by the session cookie's own `exp`, set at
//       the last successful renewal (or login) and never extended while
//       renewals keep failing. If the outage/deploy outlasts the
//       cookie's remaining lifetime, the very next renewal attempt
//       naturally moves from "unreadable" to "invalid/expired" (a 401)
//       and lands in case 2 instead — the backend's own fail-closed
//       bound already gives this retry loop a ceiling without this
//       component needing to count attempts itself.
//     - A pure network failure (renewSession's fetch() itself rejects —
//       offline, DNS down, the Next.js server unreachable before it can
//       even produce a status code) has NO such backstop: no request
//       ever reaches the backend to expire anything against, so this
//       retries at FALLBACK_RENEWAL_INTERVAL_MS indefinitely until
//       connectivity returns. That is correct here, not a bug — the
//       alternative is guessing a client-side timeout for "how long is
//       too long to be offline", which is exactly the kind of
//       unenumerated judgement call this file's whole point is to avoid
//       making silently.
//
//     Every RenewalUnavailableError retry — both sub-cases — uses
//     FALLBACK_RENEWAL_INTERVAL_MS rather than remembering the last
//     server-given cadence: a shorter retry would poll harder against
//     whatever is already unavailable, and RenewRateLimitRule's
//     Burst=10 (see the cross-tab comment above) is sized against
//     roughly this cadence, not a tighter one — a faster retry loop
//     across several open tabs would risk 429ing itself into a second,
//     self-inflicted RenewalUnavailableError.
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
          // Persist BEFORE scheduling, so a reload between now and the
          // next tick resumes from the newest server answer rather than
          // from whatever login left behind (packages/api/src/renew-schedule.ts).
          storeRenewAt(renewAt);
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
          //
          // Drop the browser-held session state first. #781's rule — and
          // renew-schedule.ts's own clearRenewAt comment — is that a
          // finished session must not be reconstructable from anything
          // the browser kept, and this is a third teardown path beside
          // HmsShell's sign-out and idle-timeout ones. Without these two
          // calls a clinician's cached permission set and renewal
          // schedule survived into whoever signed in next on a shared
          // ward terminal, and the "no exceptions" claim in
          // renew-schedule.ts was simply untrue of this path.
          clearPermissionsCache();
          clearRenewAt();
          // What this path deliberately does NOT do, and why — stated so
          // the difference from HmsShell's teardown is a decision rather
          // than an omission:
          //   - No POST /logout. The backend has ALREADY ended this
          //     session; that is what the 401/404 being handled here
          //     means. Asking it to revoke a session it just refused
          //     would answer 401 and change nothing.
          //   - No endZitadelSession(). A refused renewal is not a
          //     sign-out: the same human is still at the keyboard and is
          //     being sent to /login to authenticate again. Ending
          //     Zitadel's SSO session here would force a full credential
          //     prompt on every transient membership change, and
          //     HmsShell's sign-out button remains the way to leave a
          //     shared workstation.
          window.location.href = "/login";
        });
    };

    // The FIRST renewal is scheduled from the server's own answer too
    // (#916 Task 4, F3) — POST /v1/auth/login now returns `renew_at`
    // from the same helper POST /v1/auth/renew uses
    // (backend/.../iam/renew.go's renewAtFor), and the auth-callback
    // page stored it (packages/api/src/renew-schedule.ts) before navigating here.
    //
    // This closes the one interval spec D5 did not. Previously this line
    // read `nextRenewalDelayMs(undefined)` — the bounded five-minute
    // fallback — which meant the server's SESSION_TTL governed every
    // renewal EXCEPT the first. Any deployment with SESSION_TTL under
    // about five minutes lost every session before its first renewal
    // fired, silently, because config.go applies no minimum to
    // SESSION_TTL (#921). Nothing coupled the two; that is exactly the gap D5
    // claimed to have closed structurally.
    //
    // loadRenewAt() returning undefined still yields the identical old
    // behaviour through the identical code path, so the fallback keeps
    // its other two documented cases (a 200 with no parseable
    // `renew_at`, and every RenewalUnavailableError retry) unchanged.
    scheduleNext(nextRenewalDelayMs(loadRenewAt()));

    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [skip]);

  return null;
}
