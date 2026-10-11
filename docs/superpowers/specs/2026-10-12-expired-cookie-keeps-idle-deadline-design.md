# An expired session cookie still decides the idle deadline of its renewal

**Issue:** [#859](https://github.com/tesserix/helivanta/issues/859)
**Amends:** D3's "Not covered" paragraph of
`2026-08-16-idle-timeout-design.md`, corrected in the same change to point here.

## The problem, stated precisely

`POST /v1/auth/login` is both a first sign-in and the silent renewal. It tells
them apart by verifying the caller's existing Helivanta session cookie
(`iam.LoginHandlers.idleDeadlineFor`): a valid cookie for the same subject,
with no newer Zitadel authentication, carries its `idle_deadline` forward
unchanged; anything else is a genuine login and gets `now + IdleTimeout`.

`session.Verifier.Verify` refuses an expired token outright. So a cookie whose
`exp` has lapsed yields no claims, `idleDeadlineFor` cannot see the deadline,
and the request takes the genuine-login branch.

That is #848's own scenario. A ward terminal or laptop that stops renewing for
longer than `SESSION_TTL` (suspended, asleep, a throttled tab) is woken, the
shell silently re-authorises against the still-live Zitadel SSO session, and the
exchange returns the **original** `auth_time`. Nobody authenticated, yet the
session gets a fresh 15-minute window. The bystander who wakes an unattended
machine is exactly who the idle timeout exists to stop.

Only the login exchange has this hole. Renewal (`POST /v1/auth/renew`),
activity and tenant switch take the deadline from a principal
`authn.Middleware` has already verified, and the middleware refuses an expired
cookie, so those paths fail closed already.

## D1 — A separate accessor that reads past `exp`, and returns nothing that can authenticate

`pkg/session` gains `Verifier.RenewalEvidence(raw) (RenewalEvidence, error)`.

It applies every check `Verify` applies **except** expiry: EdDSA only, the
configured `kid`, a valid signature, the configured issuer, `exp` and `iat`
present, and `sub`, `tenant_id` and `idle_deadline` present. A token that fails
any of those is refused exactly as `Verify` would refuse it.

It returns a different type, not `Claims`:

```go
type RenewalEvidence struct {
	Subject      string
	AuthTime     time.Time
	IdleDeadline time.Time
}
```

No tenant and no expiry. `authn.Principal` cannot be built without a tenant, so
evidence read from an expired token cannot be turned into an authenticated
request by accident. It answers one question: what window was this browser
running against?

Rejected: a `Verify` option such as `IgnoreExpiry()`. It returns `Claims`, which
is what `authn.Middleware` consumes, so a single misplaced option would turn an
expired cookie into a live session. A separate method with a separate return
type cannot be confused with authentication.

## D2 — Only the login exchange may call it, enforced by an arch test

`internal/archtest` pins every reference to `RenewalEvidence(` outside
`pkg/session` to an allowlist containing only
`internal/modules/iam/login.go`, the same mechanism that pins
`tenantdb.WithAdmin`. Wiring it into `authn.Middleware`, or anywhere else, is a
CI failure.

## D3 — `idleDeadlineFor` reads the evidence instead of verifying

`idleDeadlineFor` calls `RenewalEvidence` where it called `Verify`. The three
ways out of the carry branch are unchanged: no usable cookie, a different
subject, or a strictly later Zitadel `auth_time`. A live cookie behaves exactly
as before, because every token `Verify` accepts, `RenewalEvidence` accepts with
the same values.

A woken machine therefore carries its old deadline, which has almost always
lapsed, so the re-minted session is still refused by `authn.Middleware` and the
clinician has to sign in. That sign-in is a new Zitadel authentication, a later
`auth_time`, and gets a full window. No lockout.

## D4 — No bound on how far past `exp` a token may be read

The issue asks whether to bound how long an expired cookie stays a renewal
candidate. It does not, and the direction of the argument is what settles it.

Carrying a deadline can only keep or shorten the window. Every deadline this
system writes is `now + IdleTimeout` at a genuine login or at the activity
endpoint, so a carried value is never later than the fresh one it replaces. An
old token therefore never grants more than a fresh login would. A bound would
make every token older than it fall back to the fresh-window branch, which
reopens this exact hole for any machine asleep longer than the bound.

What is still trusted is the signature. A token is only read if this
`Verifier`'s key signed it, so an attacker cannot forge one with a later
deadline.

## Tests

- `pkg/session`: `RenewalEvidence` returns the subject, `auth_time` and deadline
  of a live token and of an **expired** one, while `Verify` still refuses the
  expired one. It refuses, like `Verify`, a wrong key, a wrong `kid`, a wrong
  issuer, `alg: none`, a token without `exp`, and one without `idle_deadline`.
- `iam` login: the wake-from-suspend sequence with an expired cookie and the
  original `auth_time` carries the old deadline. Observed failing against the
  current code first. The same cookie with a later Zitadel `auth_time` gets a
  fresh window. An expired cookie of a different subject gets a fresh window.
- `archtest`: a reference to `RenewalEvidence(` outside the allowlist fails.
  Shown to fail by adding one to `pkg/authn`.

## What this slice does not cover

- **Key rotation.** A cookie signed with a retired `kid` is refused by both
  methods, so after a rotation an old cookie falls back to a fresh window. That
  needs the verifier to know retired keys, which is separate work.
- **No cookie at all.** Out of scope per the issue: a browser cannot omit the
  `httpOnly`, same-origin cookie. The threat model is a bystander at an
  unattended terminal.
- Changing `SESSION_TTL` or `IDLE_TIMEOUT` values.
