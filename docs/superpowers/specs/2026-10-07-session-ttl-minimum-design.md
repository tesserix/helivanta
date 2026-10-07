# SESSION_TTL must refuse to boot below the renewal schedule's own minimum

**Issue:** [#921](https://github.com/tesserix/helivanta/issues/921)
**Sibling precedent:** `config.RequireIdleTimeout` (`internal/config/idletimeout.go`, #848)
**Not covered by this slice, filed separately:** the failed-renewal retry
cadence on short TTLs (D5 below) and the duplicated dev defaults for the
Zitadel-facing URLs raised in #921's comment (D6 below).

## The problem, stated precisely — and where the issue's own claim was wrong

#921 says `SESSION_TTL=0`, `-5m` and `5s` "all parse … boot cleanly, pass
every test, and sign every clinician out permanently". The parsing part is
true: `getenvDuration` (`config.go`) only falls back on an **unparseable**
value, so all three reach `Config.SessionTTL` unchanged.

Checked against the code rather than taken from the issue, the boot part
splits into two cases:

1. **Non-positive (`0`, `-5m`) already refuses to boot — misleadingly, and
   late.** `session.NewSigner` refuses `ttl <= 0`
   (`pkg/session/signer.go`), and `cmd/api/main.go` returns that error.
   But it returns it as `session: no signing key configured: ttl must be
positive`, which names the _signing key_, not `SESSION_TTL`, and only
   after the process has opened three database pools, run migrations, linted
   RLS, connected to NATS and fetched Zitadel's OIDC discovery document. An
   operator reading that line is sent to the wrong secret.
2. **Positive but tiny (`5s`, `20s`) boots cleanly and signs everyone out.**
   This is the real, silent half of the defect. `renewAtFor`
   (`iam/renew.go`) answers `max(TTL/3, 30s)` and the client clamps to the
   same 30s (`apps/shell/lib/renew.ts`, `MIN_RENEWAL_DELAY_MS`). With any
   TTL ≤ 30s the first renewal is scheduled at or after the cookie's own
   `exp`, so every session dies before it is ever renewed — estate-wide,
   with nothing logged.

## Decisions

### D1 — A single minimum, refused at boot, never clamped

`config.RequireSessionTTL()` returns the configured TTL or refuses with
`ErrInvalidSessionTTL` when `SessionTTL < MinSessionTTL`. Non-positive values
are a subset of "below the minimum" and need no separate branch; the error
text still states the value it got and names `SESSION_TTL`.

Refuse, not clamp — the same reasoning `RequireIdleTimeout` gives: silently
substituting a different value hides the operator's mistake and leaves the
deployed policy differing from the declared one. A session lifetime is an
access-control bound (it is the upper bound on how long a user deactivated
in Zitadel keeps working), so the direction is fail closed, and the only open
question is _where_ the failure surfaces. A boot refusal surfaces it at the
deploy, in front of the person who changed the value.

### D2 — The minimum is 90 seconds, and it is derived, not chosen

`MinSessionTTL = 90s` is `renewalFraction × renewAtFloor` (3 × 30s): the
smallest TTL at which `renewAtFor` still answers its designed value, `TTL/3`,
rather than its floor. Below it the floor engages and the two-thirds margin
the schedule was designed around (two retry windows before `exp`) shrinks;
at or below 30s it is gone entirely and the defect above is total.

So a TTL under 90s is not "a short session", it is a configuration in which
the renewal schedule no longer means what its own code says. That is the
boundary this guard enforces — not a round number someone liked.

The derivation is enforced structurally, not by comment: `iam`'s tests carry
a **compile-time** assertion that `config.MinSessionTTL / renewalFraction >=
renewAtFloor`. Raising `renewAtFloor` or `renewalFraction` without raising the
minimum makes `go test ./internal/modules/iam/...` (and `make lint-go`, which
type-checks test files) fail to compile.

The lowest value any environment uses today is the e2e suite's
`SESSION_TTL_TEST_VALUE ?= 3m` (`Makefile`), which is above the minimum.

### D3 — Checked beside RequireIdleTimeout, before any network round trip

`cmd/api/main.go` calls `RequireSessionTTL` immediately after
`RequireIdleTimeout`, before the database, NATS and Zitadel are touched, and
uses the **returned** value for every consumer (`session.NewSigner`,
`iam.LoginDeps`, the `/me` handler, the activity handler, the renewal
handler). Reading through the accessor rather than `cfg.SessionTTL` is what
makes the validated value the one actually used.

`session.NewSigner`'s own `ttl <= 0` check stays: it is the package's own
contract, independent of how any one binary configures it.

### D4 — The renew_at floor stays, its justification changes

`renewAtFloor` and the client's `MIN_RENEWAL_DELAY_MS` are no longer the only
thing standing between a typo and a hot polling loop — the boot guard is. They
stay as defence in depth (the client's also covers a clock-skewed or
already-past `renew_at`, which no server config can prevent). The comments
that justified them by "config.go applies no minimum to SESSION_TTL (#921)"
are rewritten to point at `RequireSessionTTL` instead, everywhere they appear:

- `backend/internal/modules/iam/renew.go` (two)
- `backend/internal/modules/iam/login.go`
- `backend/internal/modules/iam/renew_test.go`
- `apps/shell/components/session-renewal.tsx` and its test
- `packages/api/src/renew-schedule.ts`
- `e2e/tests/session-renewal.spec.ts`

### D5 — Not covered: failed-renewal retries on a short TTL

Every `RenewalUnavailableError` retry (`session-renewal.tsx`) waits the fixed
`FALLBACK_RENEWAL_INTERVAL_MS` (5 minutes). For any TTL under ~7.5 minutes a
single failed renewal therefore cannot be retried before `exp`. That is real,
but it is not a SESSION_TTL floor problem: raising the minimum to 7.5 minutes
would only encode a client constant into a server boot rule, and would
forbid the 3-minute TTL the e2e suite needs. The correct fix is a retry
cadence bounded by the session's remaining lifetime, which is client work.
Filed as [#941](https://github.com/tesserix/helivanta/issues/941).

### D6 — Not covered: the duplicated Zitadel URL dev defaults

#921's comment points out that `ZITADEL_HOSTED_LOGIN_URL`'s and
`DevHelivantaWebOrigin`'s dev defaults exist both in the `Makefile` and as Go
literals. Same family (a value derived in one place and silently defaulted in
another), different mechanism, no shared code with this change. Filed
separately as [#942](https://github.com/tesserix/helivanta/issues/942) rather than widening this slice.

## Tests

- `internal/config/sessionttl_test.go`: `0s`, `-5m`, `5s`, `89s` refuse with
  `ErrInvalidSessionTTL` and name `SESSION_TTL`; exactly `MinSessionTTL` and
  `3m` (the e2e value) are accepted unchanged; unset yields
  `DefaultSessionTTL` (15m); an unparseable value still falls back to the
  default (the existing capacity-control behaviour is unchanged).
- `internal/modules/iam/renew_test.go`: compile-time assertion (D2), plus a
  runtime test that `renewAtFor(now, MinSessionTTL)` is exactly
  `now + MinSessionTTL/3` — the floor does not engage at the minimum.
- Each refusal test is proven able to fail by mutating the guard's
  comparison and observing the failure.
