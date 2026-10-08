# A failed renewal is retried before the session it is renewing expires

**Issue:** [#941](https://github.com/tesserix/helivanta/issues/941)
**Builds on:** `2026-08-20-server-side-session-renewal-design.md` (#916) and
`2026-10-07-session-ttl-minimum-design.md` (#921, D5 filed this).

## The problem, stated precisely

`components/session-renewal.tsx` retried every `RenewalUnavailableError` (429,
408, 5xx or a network failure) after the fixed `FALLBACK_RENEWAL_INTERVAL_MS`,
which is five minutes. The first renewal fires at `SESSION_TTL/3`
(`renewAtFor`), so the first retry landed at `TTL/3 + 5m`. That is before the
cookie's `exp` only when `TTL > 7.5m`. On any shorter, valid TTL (#921's minimum
is 90s, and the e2e suite runs 3m), **one** failed renewal signed the clinician
out mid-task. A rolling API deploy is enough to cause one, because the Next
rewrite proxy turns it into a 5xx. Even at the 15m default, the margin was
exactly two retries, and nothing stated or tested that.

## Decisions

### D1 — The server tells the browser when the session expires

The browser cannot read its own session's expiry: the cookie is `HttpOnly`, and
the API only sent `renew_at`. Deriving expiry from `renew_at` would encode
`renewalFraction` into the client, an implicit coupling that drifts silently.
So `POST /v1/auth/login` and `POST /v1/auth/renew` both answer
`expires_at = expiresAtFor(now, ttl)`, and a test pins that the two endpoints
answer the same value for the same clock.

**It is never later than the cookie's real `exp`.** `now` is read before
`signer.Mint`, which stamps `exp` from a later clock read. The value is also
truncated to whole seconds, because the token's `exp` is a JWT `NumericDate`
that `jwt.NewNumericDate` truncates. Without the truncation, a sub-second `now`
put the value up to a second after the real `exp`. That is the unsafe
direction, and `TestRenewExpiresAtIsNeverAfterTheCookiesExp` caught it against
a real minted cookie. The activity endpoint also re-mints the cookie without
telling the browser, which leaves the stored value earlier than the truth. That
errs in the same safe direction.

### D2 — The retry waits a third of what is left, within the existing bounds

`retryDelayMs(expiresAt) = max(MIN_RENEWAL_DELAY_MS, min(FALLBACK, remaining/3))`:

- **Never longer than the fixed fallback.** On a long session the cadence is
  exactly what it was, so `RenewRateLimitRule`'s budget (sized against roughly
  that cadence across clustered tabs) is spent faster only on a short TTL.
- **Never shorter than 30s**, the same floor the server's `renew_at` allows. A
  session with seconds left is retried once at 30s. If it has lapsed by then,
  the backend answers 401 and the loop takes its normal sign-in-again path,
  which is the truthful outcome.
- **A third** mirrors the server's own `renewalFraction`: it leaves room for two
  more retries after this one before the session lapses.
- **Unknown expiry** (an older API, or a lost value): the fixed fallback, i.e.
  the behaviour before this change, rather than a guess.

For the 3m TTL: the renewal at t+60s fails with 120s left, so the retry fires
at t+100s, inside the session. Before, it fired at t+360s.

### D3 — Stored beside `renew_at`, cleared with it

`expires_at` is stored in `sessionStorage` (`helivanta.expires_at`) at the same
two moments as `renew_at`: the auth callback and every successful renewal. The
retry reads the value the last success stored, because the failed call minted
nothing. `clearRenewAt` removes both, so every teardown path (sign-out, idle,
refused renewal) leaves nothing behind, as #781 requires.

## Not covered

- Cross-tab coordination of retries: still none, by the existing design
  (`session-renewal.tsx`).
- The activity endpoint returning its new expiry: unnecessary, because a stale
  earlier value only makes retries earlier (D1).

## Tests

- Backend:
  - login and renew answer the same `expires_at`, equal to `frozen + ttl`;
  - against a real minted cookie, `expires_at` is never after `exp`, over 20
    iterations on the real clock;
  - mutation: dropping the truncation fails the test.
- `retryDelayMs`:
  - unknown expiry gives the fallback;
  - a long session gives the fallback (never faster than before);
  - 120s left gives 40s;
  - seconds left or already past gives 30s.
- `renewSession` parses `expires_at`, and is undefined when it is missing or
  unparseable.
- `renew-schedule`: round trip, an unparseable value is ignored, and
  `clearRenewAt` removes both keys.
- Component:
  - a failed renewal with 120s left is retried within 30–40s, and not at the
    fixed fallback;
  - a successful renewal stores its expiry.
- Callback: the login's `expires_at` is stored.
- Mutations:
  - the component retry reverted to the fixed fallback;
  - the five-minute cap removed;
  - the callback not storing `expires_at`.
  Each fails the suite.
