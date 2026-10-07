// The server's renewal schedule, carried across the ONE boundary the
// browser cannot carry it over in memory: the sign-in navigation, and any
// subsequent full page load (#916 Task 4, F3).
//
// Why this file exists at all. Spec D5 says the client renews on the
// SERVER's schedule rather than on a constant it invented. That was true
// of renewals 2..n — each POST /v1/auth/renew answers with `renew_at`, and
// components/session-renewal.tsx schedules the next one from it — and was
// simply absent for the FIRST renewal, which had no server answer to read
// yet and therefore fell back to FALLBACK_RENEWAL_INTERVAL_MS (5 minutes,
// lib/renew.ts). Any deployment running SESSION_TTL below about five
// minutes consequently lost every session before its first renewal ever
// fired, silently. The API now refuses to boot below 90s
// (backend/internal/config/sessionttl.go, #921), but 90s..5m is a valid
// configuration, and it is the range this module protects.
// POST /v1/auth/login now answers with `renew_at` too, from the same
// server-side helper the renewal endpoint uses, and this module is how
// that first answer reaches the component that acts on it.
//
// sessionStorage rather than a module-level variable: apps/shell's
// app/api/auth/callback/page.tsx hands off to the dashboard, and while
// today that is a client-side router.replace() that would preserve a module
// variable, a hard reload (F5, a restored tab, a crashed renderer) would
// not — and on a short SESSION_TTL the fallback that reload lands on is
// exactly the defect above, reintroduced. Per-tab scope is correct here:
// each tab runs its own renewal loop by design (see session-renewal.tsx's
// note on why no cross-tab coordination exists).
//
// It lives in @helivanta/api rather than in apps/shell because sign-out has
// to clear it, and sign-out is driven from packages/ui's HmsShell — which
// serves every app and cannot import from one of them. permissions-cache.ts
// beside this file is the exact precedent: session-scoped browser state
// with a clear* function, owned here and cleared from HmsShell's teardown
// paths. The `typeof window === "undefined"` guards match that file's, so
// these are safe to reference from a server-rendered module.
//
// The stored value is REWRITTEN after every successful renewal, not just
// at login, so a reload at any point in a session resumes from the most
// recent thing the server actually said rather than from a stale one.
//
// Everything here fails soft to `undefined`, which is not laxity: an
// absent or unreadable schedule is precisely the case
// nextRenewalDelayMs(undefined) already handles by design, and Safari's
// private mode and hardened enterprise profiles both make sessionStorage
// access throw rather than return null. Throwing out of the renewal loop's
// mount would be strictly worse than renewing on the documented fallback.
export const RENEW_AT_KEY = "helivanta.renew_at";

export function storeRenewAt(renewAt: string | Date | undefined): void {
  if (typeof window === "undefined") return;
  if (!renewAt) return;
  const iso = typeof renewAt === "string" ? renewAt : renewAt.toISOString();
  try {
    window.sessionStorage.setItem(RENEW_AT_KEY, iso);
  } catch {
    // Storage unavailable or full — the loop falls back to its own
    // bounded interval, which is a documented, safe cadence.
  }
}

// loadRenewAt returns the most recent server-provided renewal instant, or
// undefined when there is none, it is unreadable, or it is not a parseable
// date. It deliberately does NOT reject a value in the past: a stale
// timestamp (a tab restored hours later) is still information, and
// nextRenewalDelayMs already clamps a past instant up to
// MIN_RENEWAL_DELAY_MS rather than firing a zero-delay retry storm.
export function loadRenewAt(): Date | undefined {
  if (typeof window === "undefined") return undefined;
  let raw: string | null;
  try {
    raw = window.sessionStorage.getItem(RENEW_AT_KEY);
  } catch {
    return undefined;
  }
  if (!raw) return undefined;
  const parsed = new Date(raw);
  return Number.isNaN(parsed.getTime()) ? undefined : parsed;
}

// clearRenewAt drops the stored schedule. There are THREE paths that end a
// session in the browser, and it is called on all three — beside
// clearPermissionsCache, for the same reason that one is called there:
// #781's rule is that a finished session must not be reconstructable from
// anything the browser kept.
//
//  1. An explicit sign-out (packages/ui's HmsShell, handleSignOut).
//  2. An idle timeout (the same file's endIdleSession).
//  3. A renewal the backend affirmatively refused —
//     apps/shell/components/session-renewal.tsx's RenewalFailedError
//     branch, which then navigates to /login. That path was previously an
//     unstated EXCEPTION to the claim below: it cleared nothing at all.
//
// Paths 1 and 2 also POST /logout and end Zitadel's own SSO session; path 3
// does neither, on purpose — see its own comment for why (the backend has
// already ended the session, and a refused renewal is not a sign-out). So
// the claim below is about BROWSER-HELD state specifically, which is the
// scope #781 gives it.
//
// This value is not a credential and holds no PHI — it is one timestamp,
// and the next login overwrites it — so leaving it would not have been a
// vulnerability. It is cleared because "the browser keeps nothing from the
// previous session" is a property worth being able to state without
// exceptions, and because an exception that has to be re-derived by the
// next reader costs more than the removeItem does.
export function clearRenewAt(): void {
  if (typeof window === "undefined") return;
  try {
    window.sessionStorage.removeItem(RENEW_AT_KEY);
  } catch {
    // Nothing to do: the entry is unreadable anyway.
  }
}
