# Helivanta collects the second factor itself

**Issue:** [#867](https://github.com/tesserix/helivanta/issues/867)
**Rests on:** `docs/superpowers/spikes/2026-08-17-zitadel-login-client-mfa.md` —
every protocol claim below was observed against our own v4.15.3 stack.
**Amends:** D3 of `2026-08-16-hms-login-client-design.md`. The MFA-required and
enrolled-factor rows stop being handoffs **for TOTP only**; every other row is
unchanged.
**Depends on:** [#866](https://github.com/tesserix/helivanta/issues/866)
(`@tesserix/web` 2.2.1), merged.
**Superseded in part (2026-10-07):** every **handoff** row in this document is
replaced by `2026-10-07-no-hosted-login-handoff-design.md` (#947). Helivanta no
longer sends any user to Zitadel's hosted login; those cases are refused with a
reason, and an unreadable policy or factor set is a retryable 503.
**Advances:** [#35](https://github.com/tesserix/helivanta/issues/35) (MFA).
**Does not touch:** [#422](https://github.com/tesserix/helivanta/issues/422)
(passkeys) — see "Out of scope".

Today a clinician whose account has a second factor is redirected to Zitadel's
stock hosted page to enter it. This spec makes Helivanta collect the TOTP code
on its own themed page, using `@tesserix/web`'s auth components, while Zitadel
remains the authority that checks the factor.

---

## What the spike settled, and what it changes

| Observation | Consequence |
|---|---|
| `PATCH /v2/sessions/{id}` with `checks:{totp:{code}}` returns **200 under the login-client PAT** | Native MFA is possible with the only Zitadel credential Helivanta holds |
| The session then reports `factors.totp.verifiedAt` | Sufficiency can assert on verified factors, not infer from policy |
| **`PATCH` returns a NEW `sessionToken`** | D3 below exists entirely because of this |
| `POST` to a session id is 405 | The method is `PATCH` |
| TOTP verify is `/totp/verify`, not `/totp/_verify` | Only relevant to enrolment, which is out of scope |
| Email/SMS OTP delivery unverified (no SMTP in dev) | D1 scopes to TOTP |

---

## D1 — TOTP only; every other case keeps handing off

This slice collects **TOTP** codes. The D3 handoff table is otherwise intact:

| Case | Behaviour |
|---|---|
| Org requires MFA, user has TOTP | **Native prompt** (new) |
| Org requires MFA, user has nothing enrolled | Handoff at the time; a branded refusal after #947. **Corrected 2026-10-08 (#948):** native TOTP enrolment, then the native prompt. See `2026-10-08-native-totp-enrolment-design.md`. |
| User voluntarily enrolled TOTP | **Native prompt** (new) |
| User enrolled `OTP_EMAIL` / `OTP_SMS` | Handoff, unchanged |
| User enrolled passkey / U2F | Handoff, unchanged |
| User's account carries an IdP link | **Corrected 2026-10-08 (#950):** neutral — completes, or prompts for TOTP, exactly as if the link were absent. See `2026-10-08-idp-link-is-not-a-factor-design.md` D1. (Originally listed with passkey/U2F as a handoff.) |
| Federated hospital IdP | Handoff, unchanged |
| Password change required | Not handled ([#856](https://github.com/tesserix/helivanta/issues/856)), unchanged. **Closed 2026-10-08 (#856):** a native change step after every factor (after the TOTP code when one is enrolled) — see `2026-10-08-password-change-required-design.md`. |
| Locked out / wrong password / unknown user | Unchanged (D5's shared refusal) |

Scoping down is fine; being incorrect for excluded cases is not. Email and SMS
codes are excluded because the spike could not exercise delivery — the dev stack
has no SMTP, so the send-and-verify round trip is **unobserved**. Shipping a
factor whose delivery path nobody has run would be the same mistake as the
original `passwordChangeRequired` row, which was designed as a handoff and later
found to be unimplementable as specified.

The handoff therefore stays in place underneath, exactly as D3 intended: each
future factor replaces one row at a time.

## D2 — The page still never sees a Zitadel token

D2 of the login-client spec holds unchanged: the browser calls Helivanta's own
API and never receives a Zitadel session token.

That is what makes the second step non-trivial. Between the password check and
the factor check, the server holds a Zitadel session (`sessionId` plus a
**rotating** token). It cannot hand that to the browser, so it must keep it.

**A `login_attempt` row holds it**, keyed by the Zitadel auth request id the
browser already has:

```
0004_iam:
  login_attempt(
    auth_request_id   text primary key,
    zitadel_session_id text not null,
    zitadel_session_token text not null,
    subject           text not null,
    factor_attempts   int  not null default 0,
    expires_at        timestamptz not null
  )
```

**This table is deliberately NOT tenant-scoped, and must NOT get the forced-RLS
boilerplate** that `docs/standards/backend.md` requires of every tenant table.
Login happens before a tenant is known — there is no `tenant_id` to scope on, and
adding one would mean inventing a value before the user has selected anything. A
reviewer applying the RLS convention here by reflex would produce a policy that
either matches nothing or requires a fabricated tenant.

### The alternative that lost, and why

The spike proved `POST /v2/sessions {checks:{user,password,totp}}` works in one
call (§4). That admits a **stateless** design: the factor endpoint re-sends the
password alongside the code, creating a fresh two-factor session, and no table
exists at all.

It was rejected for one reason: **there is nowhere to count attempts.** A TOTP
code is six digits — a 10⁶ space — and the earlier spike established that
Zitadel applies a *delay*, not a lockout, with no `maxPasswordAttempts` policy
configured. Without a per-attempt record, bounding guesses falls to the
route-level rate limiter, which is keyed by caller and cannot distinguish "this
login attempt has now had five wrong codes" from ordinary traffic — and a
hospital behind one NAT'd egress IP is precisely the case where caller-keyed
limiting is weakest ([#861](https://github.com/tesserix/helivanta/issues/861)).

The stateless variant also costs a second password hash per MFA login (~0.7s
observed) and requires the browser to hold and resend the password across a step
transition. Neither is fatal; the missing attempt counter is.

## D3 — The rotating session token is threaded, never stored once

**The single most likely defect in this spec, and it produces a message that
blames the user.**

The spike observed that every `PATCH` returns a **new** `sessionToken`, and
finalize takes `session:{sessionId, sessionToken}`. An implementation that keeps
the token from session creation and reuses it after adding a factor sends a
superseded token. Finalize then fails **after** the clinician has entered a
correct code, and the only thing they can see is that their TOTP was rejected.

So:

- `loginclient.Session` carries the token as the *current* value, and any call
  that returns a new one **replaces** it. The type must make the stale value
  unreachable rather than merely discouraged — a factor check returns the
  updated `Session`, and the old value is not retained anywhere a finalize could
  read it.
- The `login_attempt` row is updated with the new token in the same transaction
  as the attempt counter.
- **A test must drive password → factor → finalize and assert finalize
  succeeds**, and it must be observed failing against an implementation that
  reuses the creation token. A green MFA test that never finalizes proves
  nothing, because the password-only path already finalizes fine.

## D4 — Sufficiency stays server-side, and gets stronger

`loginclient.CompleteIfSufficient` remains the only decider, behind the existing
arch test pinning the single finalize call site. Zitadel does not enforce
`forceMfa` for a login client — observed, and unchanged by this spec — so the
component that *renders* an MFA step must never become the thing that decides
one was required.

What improves: the sufficiency check currently reasons from policy plus enrolled
methods. After a factor check it can read `factors` off the session and assert
`totp.verifiedAt` is present. It moves from "the policy says MFA is required and
the user has TOTP, so prompt" to "the session now demonstrably carries a
verified TOTP factor, so finalize" — an assertion about observed state rather
than an inference.

Fail closed is unchanged: if the policy cannot be read, hand off.

## D5 — The policy is mapped to the neutral shape in Go

`@tesserix/web`'s components consume a provider-neutral `AuthPolicies` object;
`zitadel.ts` is a pure adapter that maps Zitadel's policy objects onto it.
Helivanta does that mapping **in Go** and returns the neutral shape from the
existing `GET /v1/auth/login/request/:id`.

The adapter is therefore unused, deliberately. Two reasons:

1. **D2.** Returning raw Zitadel policy objects to the browser so the adapter
   can map them there leaks the vendor's shape into the page.
2. **One reader.** `loginclient` already reads this policy for the sufficiency
   decision, list-driven through `mfaPolicyKeys` with a rename guard, because a
   missed field is a silent MFA bypass. A second mapping in TypeScript would be
   a second thing that reads the same policy — and if the two disagree, the
   renderer and the enforcer disagree. That is the failure class this codebase
   keeps meeting.

Only fields the form needs cross the wire: `allowPassword`, `requireMfa`,
`secondFactors`, `ignoreUnknownUsernames`. The policy is per-org, not per-user,
so exposing it reveals nothing about whether a given account exists.

**`requireMfaLocalOnly` is deliberately NOT exposed, even though the neutral
shape has a slot for it.** `loginclient.LoginPolicy` models a single folded
`ForceMFA`, set when *either* Zitadel key is true — that fold is D4 of the
login-client spec, taken on the explicitly recorded assumption that every
session Helivanta evaluates is a local (password) one. (**Corrected 2026-10-08,
#950:** the assumption was previously worded as "no external IdP is
configured", which is false on the shared instance; see
`2026-10-08-idp-link-is-not-a-factor-design.md` D2.) Unfolding
it here to populate two neutral fields would create a distinction Go does not
make and cannot currently make correctly, and the component would then render
from a value the enforcer never consults. `requireMfa` carries the folded
result. If Helivanta ever federates a hospital IdP, D4 says the fold must be
revisited — and this field is part of what has to change with it.

**The MFA step is only ever revealed after a correct password.** Anything else
would turn the prompt into an enumeration oracle — "this account has TOTP" is
information about the account, and D5 of the login-client spec exists to deny
exactly that class of signal.

## D6 — Factor attempts are bounded per login attempt, and exhaustion abandons the session

`factor_attempts` is incremented on every wrong code. At **five**, the
`login_attempt` row is deleted and the Zitadel session is abandoned — *abandoned*,
not revoked. Nothing calls `DELETE /v2/sessions/{id}`, so the session lives out
its own lifetime, unreachable because the only token for it is gone. This
section's heading originally said "destroys the session", which overclaimed. The user
starts again from the credential form.

Five, not three: a clinician mistyping a rolling six-digit code on a ward
terminal is ordinary, and a limit that fires on ordinary use trains people to
distrust the system. Five, not fifty: at fifty the 10⁶ space is meaningfully
eroded by a determined attacker who already has the password.

This is per **login attempt**, not per caller, which is what makes it work where
the route limiter cannot — see D2's rejected alternative. The route limiter
still applies on top, with its own bucket: a six-digit guessing endpoint must not
share a budget with anything else.

`expires_at` is short — the auth request itself expires, and a pending factor
step that outlives it is unusable anyway. Expired rows are deleted on read, so
no cleanup job is required for correctness; a periodic sweep is housekeeping,
not a control.

## D7 — The accessible-name contract survives, explicitly

`Email`, `Password`, and a button named `Sign in` remain the contract
(login-client spec D6). `e2e/tests/support/login.ts` drives sign-in by
accessible name, so a rename fails all twelve specs at the login step and reads
as a broken application.

`AuthCredentialForm` accepts `loginNameLabel`, `passwordLabel` and
`submitLabel`. **All three are passed explicitly.** Its default login-name label
is derived from the policy (`describeLoginName(methodPolicy)`) and would *not*
say `Email`; relying on the default is the concrete way this contract breaks.

The MFA step introduces new names. They are new, not renamed, so no existing
selector depends on them — but the e2e spec added for this flow pins them, so
they become a contract the moment it lands.

## D8 — The API surface

| Route | Change |
|---|---|
| `GET /v1/auth/login/request/:id` | Also returns the neutral `AuthPolicies` subset (D5) |
| `POST /v1/auth/login/password` | Gains a third outcome: `factor_required` with the factor kinds to collect |
| `POST /v1/auth/login/factor` | **New.** `{auth_request_id, factor:"totp", code}` → `callback_url`, `handoff_url`, a shared refusal, or `auth_request_invalid` |
| `POST /v1/auth/login/enroll` | **Added 2026-10-08 (#948).** Same body as `/factor`; confirms a TOTP the password step registered (`enrollment_required` outcome) and completes the sign-in. Five unauthenticated login routes since then. See `2026-10-08-native-totp-enrolment-design.md` D3. |

**Corrected after implementation**, per CLAUDE.md's rule that superseded docs are
fixed in the same change. Three drifts from what this section first claimed:

1. The refusal-for-an-unusable-attempt wire shape is `auth_request_invalid`, not
   `attempts_exhausted`. One shape covers "no such attempt", "expired" and
   "exhausted" deliberately, so none is distinguishable from the others.
2. **`handoff_url` is a real outcome of the factor endpoint**, originally omitted
   here. `CompleteAfterFactor` returns `OutcomeHandoff` when the user's enrolled
   methods include something Helivanta cannot collect, so a *correct* TOTP code
   can still legitimately end in a handoff — reachable when enrollment changes
   mid-flow. The Task 5 brief inherited this omission and would have shipped a
   three-way client union that silently mishandled it; the implementer caught it.
3. There are now **four** unauthenticated login routes, not three.

All four remain unauthenticated, mounted via `bootstrap.MountUnauthenticated`
and enumerated in `bootstrap.UnauthenticatedRoutes` with their reason — routes
outside `platform.Router` get no rate limiting for free, so the new endpoint
takes its own budget explicitly.

The password endpoint's outcome type becomes a three-way discriminated union.
The page must treat `factor_required` as **neither success nor failure** — the
same trap D3 of the login-client spec documents for `handoff_url`, where
rendering a handoff as a failed sign-in would send a clinician to reset a
password that was correct.

---

## Errors and failure handling

- **Wrong TOTP code** → the shared refusal wording, the attempt counter
  increments. Never "your code is wrong, 3 attempts left": a countdown tells an
  attacker how much budget remains.
- **Attempts exhausted** → the session is destroyed and the form says the
  sign-in attempt ended and to start again. Distinguishable from a wrong code,
  because the user must now re-enter their password and needs to know that.
- **Zitadel unreachable during the factor check** → 503 with a retry-able
  message, and the `login_attempt` row is left intact so a retry can proceed.
  Never a wrong-code message; telling a clinician their authenticator is wrong
  when the IdP is down sends them to re-enrol a working factor.
- **Finalize fails after a verified factor** → the user is not told they are
  signed in. The session is only real once the callback exchange succeeds
  (unchanged).
- **`login_attempt` missing or expired** → the sign-in attempt expired, start
  again. Reachable in normal use: a browser left on the MFA step overnight.
- **Never logged:** the TOTP code, the password, the Zitadel session token, or
  the login name on a failed attempt.

## Testing

- **Go, faked Zitadel:** one row per D1 case, asserting the outcome and that
  finalize was *not* called for the handoff rows.
- **The D3 regression test, which is the point of this spec:** password →
  factor → finalize succeeds, **observed failing** against an implementation
  that reuses the session token from creation. A passing MFA test that never
  finalizes proves nothing.
- **The D6 test:** five wrong codes destroy the row and the session; the sixth
  attempt gets the expired-attempt answer, not another refusal.
- **A D4 test:** a session whose read reports no `totp.verifiedAt` is never
  finalized, even when the policy read succeeded.
- **Integration against the real dev Zitadel:** enrol TOTP on a throwaway user,
  complete a full two-factor login, assert a Helivanta session is minted. Clean
  the user up with the **seed** PAT — the spike established the login-client PAT
  cannot delete users (403) and left one behind.
- **Vitest:** the credential form and the OTP step via `renderWithProviders`,
  including that `factor_required` does not render as an error.
- **E2E:** a TOTP login end to end, plus all twelve existing specs unchanged —
  D7's contract doing its job.

## Risks

- **Token rotation** (D3). Highest-likelihood defect, and it blames the user.
- **A second policy reader appearing later.** D5 keeps mapping in Go; nothing
  structurally prevents a future PR importing `zitadel.ts` in the page. Worth an
  import lint, which belongs with #713.
- **The MFA step becoming an enumeration oracle** if it is ever revealed before
  a correct password (D5).
- **`login_attempt` gaining a `tenant_id`** because a reviewer applies the RLS
  convention by reflex (D2).
- **TOTP clock skew.** Zitadel validates the code; Helivanta does not compute
  it, so skew presents as a wrong code. Not mitigated here, and stated so that a
  support report of "my code is always rejected" has a documented first
  suspect.

## Out of scope

- **Passkeys / WebAuthn** (#422). The session API exposes a `webAuthN` check but
  the spike deliberately did not exercise challenge issuance or the browser
  assertion round trip.
- **Email and SMS codes.** Delivery is unverified (D1).
- **Factor enrolment.** `POST /v2/users/{id}/totp` and `/totp/verify` were
  observed working, so a native enrolment screen is feasible, but a user with no
  factor is not blocked today and adding enrolment is a separate product
  decision.
- **`passwordChangeRequired`** (#856) — still unsignalled by Zitadel; closed 2026-10-08 by reading the user before finalize — see `2026-10-08-password-change-required-design.md`.
- **Account lockout** (#855) — D6 bounds factor guesses for one login attempt;
  it is not an account lockout and must not be described as one.
