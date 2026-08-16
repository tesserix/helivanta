# HMS renders the login form: HMS as Zitadel's login client

**Issue:** [#854](https://github.com/tesserix/hms/issues/854)
**Supersedes:** D5a of `2026-08-15-zitadel-tenancy-topology-design.md`
**Rests on:** `docs/superpowers/spikes/2026-08-16-zitadel-login-client.md` —
every protocol claim below was observed against our own v4.15.3 stack, not read
from documentation.

Clinicians should sign in on a page that is HMS's, with our theme. Today they are
redirected to Zitadel's stock hosted login. This spec makes HMS the **login
client** for its own OIDC app, so the credential form is ours while Zitadel
remains the authority that checks the credential.

---

## What is actually changing

Less than it sounds. The flow, with the new part in bold:

```
Sign in  →  Zitadel /oauth/v2/authorize          302, nothing rendered
         →  **HMS /login?authRequest=…**          our form, our theme
         →  **HMS API /v1/auth/login/password**   drives Zitadel's session API
         →  HMS /api/auth/callback                unchanged
         →  HMS session minted                    unchanged
```

The spike confirmed the `callbackUrl` Zitadel returns is the callback HMS
already has, carrying `code` and `state`. The browser still begins at
`oidc-client-ts`'s `signinRedirect()`, which stores `state` and the PKCE
`code_verifier` before any of this runs. **PKCE, the callback page, the login
exchange, session minting, renewal and sign-out are all untouched.** This spec
adds one backend module and replaces the body of one page.

---

## D1 — HMS is the login client for `hms-web` only

The `hms-web` app gets a per-app **Custom base URL for the new Login UI**
pointing at HMS's `/login`. The instance-wide `LOGINV2_BASEURI` is not changed.

The Zitadel instance is shared with every other Tesserix product, and the
instance-level setting applies to every app on it. Scoping per-app is what makes
this change HMS's alone: another product's login must not move because HMS
rebranded its own.

**This is the first thing to prove.** The per-app setting is confirmed to exist
in the v4 documentation but was *not* exercised in the spike. If it turns out not
to work on our instance, the whole approach is instance-wide and must go back to
the platform team before anything else is built. It is therefore task one, not a
detail discovered late.

The `zitadel-login` container stays in the dev stack. It still serves other apps,
and D3's handoff path depends on it existing.

## D2 — The Go API holds the credential; the page holds none

`IAM_LOGIN_CLIENT` is an **instance-level** role. A holder can finalise an auth
request for *any* app on the instance, including other Tesserix products. It is
the most privileged secret HMS has ever held.

It lives in the Go API, which already has secret loading, the rate limiter
(#841/#851), request-scoped logging and the `respond.*` error contract — and
which serves no untrusted browser markup. The login page calls
`POST /v1/auth/login/password` on our own API and never sees a Zitadel token.

New endpoints, in a new `login` module:

| Route | Purpose |
|---|---|
| `GET /v1/auth/login/request/:id` | Validate the auth request is real and current; return only what the form needs |
| `POST /v1/auth/login/password` | Password check, then finalise; returns `callbackUrl` |
| `POST /v1/auth/login/handoff/:id` | Hand the auth request to the hosted login (D3) |

All three are `authz.Public` — they run before any principal exists — and all
three sit behind the existing unauthenticated limiter. `POST .../password` is a
password-attempt surface and gets the tighter of the buckets.

**#45 (secrets management) is now a hard production blocker for login itself**,
not only for `SESSION_SIGNING_KEY`. It was already blocking; this raises what is
at stake if it is skipped.

## D3 — Password only, and every other case hands off rather than dead-ends

This slice implements email + password. Passkeys are #570, MFA is #41.

Scoping *down* is fine; being *incorrect* for the cases we exclude is not. Each
unsupported case hands the **same auth request** to the hosted login, which
completes it and returns to the same callback:

| Case | HMS behaviour |
|---|---|
| MFA required by org policy (D4) | Handoff. Not an error. |
| MFA: user has voluntarily enrolled a factor (D4) | Handoff. Verified live 2026-08-16 (#854 Task 8): `GET /v2/users/{id}/authentication_methods`, reachable with the login-client PAT, lists a user's enrolled methods independent of org policy — a password-only session hands off when it reports anything besides `AUTHENTICATION_METHOD_TYPE_PASSWORD`. |
| Password change required | **Not handled — reversed from the original design.** Verified live 2026-08-16 (#854 Task 8, spike §5): a user imported with `passwordChangeRequired:true` produces a session create, a session read, and a finalize that are byte-identical in shape to a normal user's — Zitadel signals this to a login client **nowhere in the flow**. HMS's own `POST /v1/auth/login/password` against such a user returns 200 with a valid `callback_url`, same as any other successful login. There is no field to branch on, so this row cannot be implemented as "handoff" without an extra `GET /v2/users/{id}` read on every login. Documented as a known limitation at the decision point in `sufficiency.go` and filed as [#856](https://github.com/tesserix/hms/issues/856) rather than fixed in this slice. |
| Federated hospital IdP | Handoff — this is the capability Zitadel was chosen for and must never break silently. |
| Locked out | Zitadel's answer, shown inline. Never worded as a wrong password. |
| Unknown user / wrong password | D5. |

The handoff is a redirect to the hosted login's URL for that `authRequest` id.
Because the auth request is the same object, the user finishes in the same flow
and lands on the same callback — no second login, no lost state.

This is what keeps the slice a slice. #41 and #570 later replace handoffs with
native screens, one at a time, with the fallback still there underneath.

## D4 — HMS enforces MFA itself, because Zitadel does not

**Observed, not assumed.** With `forceMfa: true` on the org login policy, the
spike created a **password-only** session and the finalize endpoint returned
HTTP 200 with a valid authorization code. Zitadel neither refused nor signalled a
missing factor.

So in the login-client architecture, factor enforcement belongs to the login
client. A password-only HMS login is correct only while nothing requires a second
factor — and the failure mode if that changes is the worst kind: **every user
silently bypasses MFA and nothing anywhere reports an error.**

Therefore, before finalising, the API checks whether the authentication is
sufficient — the org's `forceMfa`, and whether the user has factors configured —
and **hands off (D3) rather than finalising** when it is not. Fail closed: if the
policy cannot be read, hand off. A login that redirects unnecessarily is a minor
annoyance; one that skips a required factor is an authentication bypass.

**This must be structural, not a convention.** The finalize call is wrapped so
that it is unreachable except through the function that has already evaluated
sufficiency — a future contributor adding a second call site should have to
delete the check deliberately, not merely forget it. An arch test asserts the
Zitadel finalize call appears in exactly that one place.

## D5 — One answer, and one duration, for a wrong password and an unknown user

Observed:

| Case | Status | Body | Time |
|---|---|---|---|
| Wrong password | 400 | `COMMAND-3M0fs`, `failedAttempts` | 0.72–0.78s |
| Unknown user | 404 | `QUERY-Dfbg2` | 0.013s |

Two enumeration oracles: the status code, and a ~55× timing difference (an
unknown user never reaches the password hash). Anyone can learn which clinicians
have accounts at a hospital, which is itself information about who works there.

HMS returns **one** response for both — same status, same body, same wording —
**and equalises duration** by holding every failed attempt to a fixed floor above
the observed hash time. Normalising the status alone leaves the timing oracle
intact while looking fixed, which is worse than not trying.

`failedAttempts` is never forwarded to the browser. It may be logged.

The floor is a named constant with its measurement in a comment, because it is
tied to Zitadel's hash cost and will need revisiting if that changes.

## D6 — The page keeps D5a's accessible names

`Email`, `Password`, and a button named `Sign in`.

D5a made this a contract because `e2e/tests/support/login.ts` drives login by
accessible name, and a rename fails all eleven specs at once at the login step —
which reads as a broken application rather than a renamed label.

The contract survives this spec unchanged, and gets *better*: the names are now
in our repo instead of a foreign container's markup, so the e2e suite stops
depending on an upstream build's DOM. The suite's login helper is repointed at
our form as part of this work.

The form uses `useZodForm` + `Field` from `@hms/ui` with `noValidate`, per the
frontend standards, and `@tesserix/web`'s `AuthLayout`/`AuthCard` chrome the
login page already uses — this time *with* the credential parts.

---

## Errors and failure handling

- **Zitadel unreachable** at any step → 503 with a retry-able message. Never a
  wrong-credentials message; telling a clinician their password is wrong when the
  IdP is down sends them to reset a password that is fine.
- **Auth request unknown, expired or already used** → the form says the sign-in
  attempt expired and offers to start again. This is reachable in normal use: a
  browser left on the login page overnight.
- **Finalize fails after a successful password check** → the user is *not* told
  they are signed in. The session is only ever real once the callback exchange
  succeeds.
- Every failure is logged with the auth request id and the outcome, and **never**
  with the submitted password, the login name on a failed attempt, or
  `failedAttempts`.

## Testing

- **Go unit tests** against a faked Zitadel: a table with one row per D3 case and
  each D5 case, asserting status, body and that finalize was *not* called for the
  handoff rows.
- **A D4 regression test that is the point of the whole spec**: with the policy
  reporting `forceMfa`, assert no finalize call is made. This test must be seen
  to fail against an implementation that omits the check — a passing test here
  that cannot fail is worse than none, because D4 is invisible when broken.
- **A D5 timing test** asserting the unknown-user and wrong-password paths differ
  by less than a stated margin.
- **Integration test** through the real dev Zitadel, completing a login and
  asserting an HMS session is minted.
- **Vitest** for the form via `renderWithProviders`, including inline error
  rendering and that no native browser validation is used.
- **E2E**: `login.ts` repointed at our form; all eleven specs must pass unchanged
  otherwise, which is D6's contract doing its job.

## Risks

- **The per-app login base URL is unproven on our instance** (D1). Task one.
- **D4's policy read is confirmed feasible**: the login-client PAT reads
  `GET /management/v1/policies/login` successfully (HTTP 200, `forceMfa`
  included), so no additional credential is needed for the org-policy half of
  the check.
- **The per-user enrolled-factor half of D4 is resolved (#854 Task 8).**
  `/v2/users/{id}/authentication_factors` and
  `/management/v1/users/{id}/auth_factors` do 404, as originally recorded, but
  `GET /v2/users/{id}/authentication_methods` (found from the v4.15.3 proto's
  `google.api.http` annotations, then verified live) works — including with
  the login-client PAT, not just the seed/owner PAT — and correctly reflects a
  user's own enrolled second factor independent of org policy. Enrolling
  `otp_email` on a test user changed `authMethodTypes` from
  `["...PASSWORD"]` to `["...OTP_EMAIL","...PASSWORD"]`, proving the endpoint
  is live rather than a stub. `loginclient.CompleteIfSufficient` now hands off
  whenever a session's user has any enrolled method besides
  `AUTHENTICATION_METHOD_TYPE_PASSWORD`, with a unit test proven to fail
  without the change and a live integration test
  (`TestIntegration_UserEnrolledFactor_HandsOffInsteadOfCompleting`) alongside
  the existing forceMfa ones.
- **`passwordChangeRequired` signalling was provoked and observed
  (#854 Task 8) — and Zitadel does not send it.** A user imported via
  `POST /management/v1/users/human/_import` with `passwordChangeRequired:true`
  (confirmed to have taken via a `GET /v2/users/{id}` read-back) produces a
  session create, session read, and finalize that carry no trace of the flag;
  HMS's own login endpoint completes the login for this user exactly as it
  would for any other. D3's table row is reversed from the original design as
  a result: this case is **not** handled by this slice. It is documented as a
  known limitation at the point of decision in `sufficiency.go` and tracked as
  its own issue, [#856](https://github.com/tesserix/hms/issues/856), rather
  than silently accepted or half-implemented.
- **Lockout is unreachable to test** because the policy sets no
  `maxPasswordAttempts`. See below.
- **HMS now owns a credential surface.** A defect in the login page is a
  credential-harvesting defect. This is the cost D5a priced and the product
  decision accepted.

## Surfaced, and not part of this work

**There is no account lockout.** The login policy has no `maxPasswordAttempts`,
so password attempts against a known account are unlimited; the request rate
limiter (#841/#851) is the only brute-force control, and it limits by caller, not
by account. This is **pre-existing** — it is not introduced by this change — but
it is materially more visible once HMS owns the login form. It needs its own
issue.

## Out of scope

- MFA (#41), passkeys (#570), password reset, registration.
- Per-hospital SSO, until a customer asks.
- Branding the hosted login: it remains stock, and is now only the handoff
  target rather than the ordinary path.
- The idle timeout (#848), designed separately.
