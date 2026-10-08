// RenewalFailedError means the backend has affirmatively decided this
// session is OVER: POST /v1/auth/renew answered 401 (no/invalid/expired
// cookie, or the subject is no longer active in Zitadel — see
// backend/internal/modules/iam/renew.go's `renew` handler) or 404 (the
// tenant this session is bound to is no longer accessible — the same
// "no accessible tenant" answer Login uses, see respondNoAccessibleTenant
// in login.go), or any other 4xx this endpoint has not been seen to
// return. Both known cases are the backend telling the caller, on
// purpose, that re-authenticating visibly is the correct next step. The
// caller (components/session-renewal.tsx) treats this, and only this, as
// "send the user to /login".
export class RenewalFailedError extends Error {}

// RenewalUnavailableError means the backend (or something in front of
// it) could not answer the question at all, not that it answered "no":
//
// - 503 — renew.go's own `identity_unavailable` / `authz_unavailable` /
//   `session_unavailable`: Zitadel or OpenFGA was unreachable, or the
//   session signer was not configured, each deliberately fail-closed per
//   spec D3.
// - 429 — RenewRateLimitRule (backend/internal/bootstrap/ratelimit.go,
//   Burst=10 per subject).
// - 408 — a request timeout, from whatever sits between the browser and
//   this handler.
// - ANY 5xx (500/502/504/…) — Review Round 1 CRITICAL finding: a bare
//   500 (a panic caught by gin.Recovery, cmd/api/main.go, or
//   respond.InternalErr from an unrelated middleware) and a 502/504 (an
//   ingress or load balancer mid-rolling-deploy, or a gateway timeout on
//   a slow Zitadel call) never reach renew.go's own deliberate 503
//   translation — they are a DIFFERENT layer failing in a way renew.go
//   never gets a chance to categorize. The routine event most likely to
//   produce one of these is an ordinary rolling deploy of the API: every
//   pod briefly returns connection-refused/timeouts to the Next.js
//   rewrite proxy (next.config.ts's `/api/:path*` → `${API_URL}/:path*`),
//   which itself then answers the browser with a 5xx of its own — this
//   never reaches the browser as a network-level fetch failure, so it
//   MUST be classified by status code, not assumed away as
//   "unreachable = catch block". Treating an unenumerated 5xx as a
//   verdict (the pre-fix behaviour) would mean every rolling deploy logs
//   out every clinician with a shell tab open, simultaneously, mid-
//   consultation — exactly the harm this task exists to prevent.
//
// This is why the classification below is `>= 500` rather than an
// allow-list of the two statuses renew.go happens to document today:
// the safe default for a status this client has not been told the
// meaning of is "the server could not answer", not "log out everyone".
// A 4xx this endpoint has never been seen to return is still classified
// as RenewalFailedError (see that class's own comment) — 4xx is
// conventionally "the request was wrong", which renewal's own retries
// cannot fix by trying again, unlike a 5xx, which is conventionally "the
// server ran into a problem" and IS worth retrying.
//
// A network-level failure to reach this same-origin endpoint at all
// (offline, a mid-deploy blip on the Next.js server itself, before any
// HTTP status even exists) lands here too — see renewSession's own
// catch block.
export class RenewalUnavailableError extends Error {}

export interface RenewalResult {
  // renewAt is the server's `renew_at` hint (renew.go's renewResponse),
  // parsed. Undefined when the field was missing or unparseable — a
  // malformed-but-200-OK response still means the session itself WAS
  // renewed (the cookie was set); it must not be treated as a reason to
  // stop renewing, only as a reason to fall back to a default cadence.
  // See nextRenewalDelayMs.
  renewAt: Date | undefined;
  // expiresAt is when the session this renewal just minted stops being
  // honoured (#941; renew.go's expiresAtFor). Undefined when missing or
  // unparseable — retryDelayMs then falls back to the fixed interval.
  expiresAt: Date | undefined;
}

// FALLBACK_RENEWAL_INTERVAL_MS is used in three cases: (1) the very
// first renewal after mount, before any server response exists to
// derive a cadence from, (2) a 200 OK response whose `renew_at` (or
// whose body entirely — see the res.json() try/catch below) is
// missing/unparseable, and (3) the UPPER bound of every
// RenewalUnavailableError retry, which retryDelayMs below shortens when the
// session has less than three of these intervals left (#941) — see
// session-renewal.tsx's retry-cadence comment. It mirrors the value this file's own comment
// used to hardcode as RENEWAL_INTERVAL_MS before D5: one third of the
// API's default SESSION_TTL (15m, backend/internal/config/config.go) —
// which is exactly what renew.go's renewAtFor computes for that TTL,
// `SESSION_TTL / renewalFraction` (renewalFraction is 3). So an
// environment running the default TTL sees the SAME cadence whether or
// not this fallback ever fires. On any OTHER SESSION_TTL the two diverge —
// that is the point of the server sending `renew_at` at all, and why
// this value is a fallback rather than the schedule.
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

// RETRY_FRACTION_OF_REMAINING is the share of the session's remaining
// lifetime a retry after a failed renewal waits (#941). A third mirrors the
// server's own renewal cadence (renew.go's renewalFraction): it leaves room
// for two more retries after this one before the session lapses, where the
// fixed FALLBACK_RENEWAL_INTERVAL_MS alone left none on any SESSION_TTL under
// ~7.5 minutes — one failed renewal (a rolling deploy is enough) signed the
// clinician out.
export const RETRY_FRACTION_OF_REMAINING = 3;

// retryDelayMs is how long to wait before retrying after a
// RenewalUnavailableError (#941): the shorter of the fixed fallback and a
// third of what is left of the session, never less than
// MIN_RENEWAL_DELAY_MS.
//
// - Never LONGER than FALLBACK_RENEWAL_INTERVAL_MS: on a long session the
//   cadence is exactly what it was before, so RenewRateLimitRule's budget
//   (sized against roughly this cadence across clustered tabs —
//   session-renewal.tsx's retry comment) is not spent faster than it was.
// - Never SHORTER than MIN_RENEWAL_DELAY_MS: whatever is unavailable is not
//   polled harder than the server's own renew_at floor allows. A session
//   with under 30s left is therefore retried once at 30s; if it has lapsed
//   by then the backend answers 401 and the loop takes its normal
//   sign-in-again path, which is the truthful outcome.
// - Unknown expiry (an older API, a lost value): FALLBACK, the behaviour
//   before #941, rather than a guess.
export function retryDelayMs(expiresAt: Date | undefined, now: () => number = Date.now): number {
  if (!expiresAt) return FALLBACK_RENEWAL_INTERVAL_MS;
  const remaining = expiresAt.getTime() - now();
  if (!Number.isFinite(remaining)) return FALLBACK_RENEWAL_INTERVAL_MS;
  const delay = Math.min(FALLBACK_RENEWAL_INTERVAL_MS, remaining / RETRY_FRACTION_OF_REMAINING);
  return Math.max(MIN_RENEWAL_DELAY_MS, Math.floor(delay));
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
// counterpart to auth-exchange.ts's exchangeIdToken — both are
// auth-lifecycle calls (POST /v1/auth/login, POST /v1/auth/renew) that
// sit outside the ordinary `/api/v1` panel-data pattern
// docs/standards/frontend.md §3 sanctions for exactly this reason (see
// that section's own text after Review Round 1: the exception is no
// longer just "install the session cookie", it explicitly names
// renew.ts). This must also NOT go through TanStack Query's
// retry/cache machinery: a renewal is a scheduled side effect on its own
// timer (components/session-renewal.tsx), not data a component reads.
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
    // mid-deploy blip on this Next.js server BEFORE it could even
    // produce an HTTP response) — the same "could not answer" bucket as
    // a 5xx/503/429 below, not a verdict that the session is over.
    throw new RenewalUnavailableError(
      err instanceof Error ? err.message : "renewal request failed to reach the server",
    );
  }

  // 429 (RenewRateLimitRule), 408 (a request timeout), and every 5xx —
  // not only renew.go's own 503 — are "try again", never "log out". See
  // RenewalUnavailableError's doc comment for exactly why the 5xx case
  // must be `>= 500` rather than an allow-list of 503 alone.
  if (res.status === 429 || res.status === 408 || res.status >= 500) {
    throw new RenewalUnavailableError(`renewal temporarily unavailable (status ${res.status})`);
  }

  if (!res.ok) {
    // Every remaining non-2xx from this endpoint (401 unauthenticated,
    // 404 no accessible tenant, or an as-yet-unseen 4xx) is the backend
    // affirmatively refusing — see RenewalFailedError's doc comment for
    // exactly which statuses land here and why.
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

  // Also fix (Minor 5): a 200 OK with a non-JSON body must not throw out
  // of this function as an uncaught SyntaxError — RenewalResult's own
  // doc comment says a malformed-but-200 response still means the
  // session WAS renewed (renew.go already set the cookie before writing
  // the body) and must fall back to a default cadence, not to a logout.
  // Without this try/catch, session-renewal.tsx's .catch would see a
  // SyntaxError (neither RenewalFailedError nor RenewalUnavailableError)
  // and take the "log out" branch AFTER the renewal had already
  // succeeded.
  let body: { tenant_id?: string; renew_at?: string; expires_at?: string } = {};
  try {
    body = (await res.json()) as { tenant_id?: string; renew_at?: string; expires_at?: string };
  } catch {
    // non-JSON 200 body — the renewal itself still succeeded (the
    // Set-Cookie already landed); just fall through with no renew_at.
  }
  return { renewAt: parseInstant(body.renew_at), expiresAt: parseInstant(body.expires_at) };
}

function parseInstant(raw: string | undefined): Date | undefined {
  const parsed = raw ? new Date(raw) : undefined;
  return parsed && !Number.isNaN(parsed.getTime()) ? parsed : undefined;
}
