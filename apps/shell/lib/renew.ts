// RenewalFailedError means the backend has affirmatively decided this
// session is OVER: POST /v1/auth/renew answered 401 (no/invalid/expired
// cookie, or the subject is no longer active in Zitadel — see
// backend/internal/modules/iam/renew.go's `renew` handler) or 404 (the
// tenant this session is bound to is no longer accessible — the same
// "no accessible tenant" answer Login uses, see respondNoAccessibleTenant
// in login.go). Both are the backend telling the caller, on purpose, that
// re-authenticating visibly is the correct next step. The caller
// (components/session-renewal.tsx) treats this, and only this, as "send
// the user to /login".
export class RenewalFailedError extends Error {}

// RenewalUnavailableError means the backend could not answer the
// question at all, not that it answered "no": a 503 (renew.go returns
// this for `identity_unavailable`, `authz_unavailable`, and
// `session_unavailable` — Zitadel or OpenFGA was unreachable, or the
// session signer was not configured; each of those is deliberately
// fail-closed per spec D3, refusing rather than granting a renewal it
// cannot verify), a 429 (RenewRateLimitRule,
// backend/internal/bootstrap/ratelimit.go, Burst=10 per subject), or a
// network-level failure reaching this same-origin endpoint at all
// (offline, a mid-deploy blip on the Next.js server itself).
//
// This is intentionally NOT treated as "log the user out" — see the long
// comment in session-renewal.tsx for why treating it that way would
// reproduce #916's own harm (mass, simultaneous eviction) from a
// different cause (an availability blip instead of a cookie that never
// travels).
export class RenewalUnavailableError extends Error {}

export interface RenewalResult {
  // renewAt is the server's `renew_at` hint (renew.go's renewResponse),
  // parsed. Undefined when the field was missing or unparseable — a
  // malformed-but-200-OK response still means the session itself WAS
  // renewed (the cookie was set); it must not be treated as a reason to
  // stop renewing, only as a reason to fall back to a default cadence.
  // See nextRenewalDelayMs.
  renewAt: Date | undefined;
}

// FALLBACK_RENEWAL_INTERVAL_MS is used in exactly two cases: (1) the
// very first renewal after mount, before any server response exists to
// derive a cadence from, and (2) a 200 OK response whose `renew_at` is
// missing or unparseable. It mirrors the value this file's own comment
// used to hardcode as RENEWAL_INTERVAL_MS before D5: one third of the
// API's default SESSION_TTL (15m, backend/internal/config/config.go),
// which is also exactly renewAtFloor's `renewalFraction` in
// renew.go — so an environment running the default TTL sees the SAME
// cadence whether or not this fallback ever fires.
export const FALLBACK_RENEWAL_INTERVAL_MS = 5 * 60 * 1000;

// MIN/MAX_RENEWAL_DELAY_MS bound whatever `renew_at` the server sends
// (or the fallback above) before it is ever passed to setTimeout. Two
// independent things could otherwise go wrong with an unbounded delay:
//
// - Too small: renew.go's own renewAtFloor (30s) already stops the
//   SERVER from ever promising a delay shorter than that, guarding
//   against a mistyped SESSION_TTL turning this into a hot polling loop
//   against Zitadel. MIN_RENEWAL_DELAY_MS restates that same floor on
//   the client so a clock-skewed or already-past `renew_at` (the browser
//   clock running behind the server's) can never produce a
//   near-zero/negative setTimeout delay and retry-storm this endpoint,
//   independent of whether the server-side floor also held.
// - Too large: a `renew_at` computed against a misconfigured, very long
//   SESSION_TTL should still result in the client checking in at some
//   bounded cadence, rather than trusting an operator's config value
//   with an unbounded sleep — a renewal that only fires once a day would
//   turn a same-day account deactivation in Zitadel into a day-long
//   window instead of the "within one TTL" bound spec D3 promises.
export const MIN_RENEWAL_DELAY_MS = 30 * 1000;
export const MAX_RENEWAL_DELAY_MS = 30 * 60 * 1000;

// nextRenewalDelayMs turns a (possibly absent) server-provided renewAt
// into a setTimeout-safe delay, clamped to the bounds above. `now` is
// injectable so tests can pin the current time instead of racing
// Date.now().
export function nextRenewalDelayMs(
  renewAt: Date | undefined,
  now: () => number = Date.now,
): number {
  if (!renewAt) return FALLBACK_RENEWAL_INTERVAL_MS;
  const delay = renewAt.getTime() - now();
  if (!Number.isFinite(delay)) return FALLBACK_RENEWAL_INTERVAL_MS;
  if (delay < MIN_RENEWAL_DELAY_MS) return MIN_RENEWAL_DELAY_MS;
  if (delay > MAX_RENEWAL_DELAY_MS) return MAX_RENEWAL_DELAY_MS;
  return delay;
}

// renewSession re-mints the caller's Helivanta session by calling
// POST /v1/auth/renew — a same-origin, cookie-only call with NO body and
// NO token (design spec D1/D5, backend/internal/modules/iam/renew.go).
// The existing `helivanta_session` cookie IS the credential; there is no
// Zitadel round trip through the browser at all, unlike the iframe-based
// `signinSilent()` flow this replaces, which depended on Zitadel's
// SameSite=Lax session cookie travelling on a cross-site iframe
// request — it never does, which is #916's whole defect.
//
// Raw fetch, not `apiFetch` from @helivanta/api: this is the renewal
// counterpart to auth-exchange.ts's exchangeIdToken, which documents the
// same reasoning — this route sits alongside POST /v1/auth/login as an
// auth-lifecycle call outside the ordinary `/api/v1` panel-data pattern
// (docs/standards/frontend.md §3's sanctioned raw-fetch exception), and
// it must NOT go through TanStack Query's retry/cache machinery: a
// renewal is a scheduled side effect on its own timer
// (components/session-renewal.tsx), not data a component reads.
//
// credentials: "same-origin" is required, not fetch's default-by-luck:
// see auth-exchange.ts's identical note on why this is asserted rather
// than assumed.
export async function renewSession(): Promise<RenewalResult> {
  let res: Response;
  try {
    res = await fetch("/api/v1/auth/renew", {
      method: "POST",
      credentials: "same-origin",
    });
  } catch (err) {
    // The endpoint itself could not be reached at all (offline, a
    // mid-deploy blip on this Next.js server) — the same "could not
    // answer" bucket as a 503 from the backend, not a verdict that the
    // session is over.
    throw new RenewalUnavailableError(
      err instanceof Error ? err.message : "renewal request failed to reach the server",
    );
  }

  // 429 (RenewRateLimitRule) and 503 (renew.go's identity_unavailable /
  // authz_unavailable / session_unavailable, each fail-closed per D3) are
  // both "try again", never "log out" — see RenewalUnavailableError's
  // doc comment.
  if (res.status === 429 || res.status === 503) {
    throw new RenewalUnavailableError(`renewal temporarily unavailable (status ${res.status})`);
  }

  if (!res.ok) {
    // Every remaining non-2xx from this endpoint (401 unauthenticated,
    // 404 no accessible tenant) is the backend affirmatively refusing —
    // see RenewalFailedError's doc comment for exactly which statuses
    // land here and why.
    let message = "Session renewal failed.";
    try {
      const body: unknown = await res.json();
      if (
        body &&
        typeof body === "object" &&
        "message" in body &&
        typeof (body as { message: unknown }).message === "string"
      ) {
        message = (body as { message: string }).message;
      }
    } catch {
      // non-JSON error body — fall back to the generic message
    }
    throw new RenewalFailedError(message);
  }

  const body = (await res.json()) as { tenant_id?: string; renew_at?: string };
  const parsed = body.renew_at ? new Date(body.renew_at) : undefined;
  const renewAt = parsed && !Number.isNaN(parsed.getTime()) ? parsed : undefined;
  return { renewAt };
}
