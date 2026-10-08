# Native TOTP enrolment: a forced-MFA user sets up an authenticator in Helivanta

**Issue:** [#948](https://github.com/tesserix/helivanta/issues/948)
**Amends:** the `mfa_enrollment_required` row of D1 and the bullet in D2 of
`2026-10-07-no-hosted-login-handoff-design.md`; the "Org requires MFA" rows of
D1 and the route table of D8 in
`2026-08-17-native-mfa-auth-components-design.md`; the `forceMfa` row in D1 of
`2026-10-08-idp-link-is-not-a-factor-design.md`. Each is corrected in the same
change to point here.

## The problem, stated precisely

Since #949, a user whose org forces MFA and who has nothing enrolled is refused
after a correct password with `mfa_enrollment_required` ("contact your
administrator"). Zitadel's hosted UI used to enrol them; the product decision
is that Helivanta's users never see Zitadel's UI. So every new user in a
forced-MFA org is locked out until enrolment happens natively.

## What was verified live, 2026-10-08, against the dev stack (Zitadel v4.15.x)

All with the **login-client PAT**, the only Zitadel credential Helivanta holds
in production, on a user whose password session had just been created:

| Call                                                         | Answer                                                                                              |
| ------------------------------------------------------------ | --------------------------------------------------------------------------------------------------- |
| `POST /v2/users/{id}/totp` `{}`                              | 200 `{uri, secret}`. `uri` is `otpauth://totp/ZITADEL:<login>?...&secret=<secret>&issuer=ZITADEL`. |
| the same call again, before verification                     | 200 with a **new** secret; the previous one no longer verifies.                                     |
| `POST /v2/users/{id}/totp/verify` `{code}` with a right code | 200. Only then does `authentication_methods` list `TOTP`.                                           |
| the same, wrong or stale code                                | 400 `EVENT-8isk2` "Invalid code".                                                                   |
| `PATCH /v2/sessions/{id}` `checks.totp` with the **same** code, after verify | 200, rotated `sessionToken`; the session then reports `factors.totp.verifiedAt`.     |
| `PATCH /v2/sessions/{id}` `checks.totp` **before** verify    | 400 `COMMAND-3Mif9s` "isn't ready".                                                                 |
| `POST /v2/users/{id}/totp` after a verified TOTP exists      | 409 `COMMAND-do9se` "already set up".                                                               |
| `POST /totp/verify` after a verified TOTP exists             | 400 `COMMAND-qx4ls` "AlreadyReady".                                                                 |
| `POST /v2/users/{id}/totp` with a wrong `x-zitadel-orgid`    | 403 `AUTH-Bs7Ds`.                                                                                   |

Three of these decide the design: a single code serves both the user-level
verify and the session check, so the clinician types one code; an unverified
registration is invisible to `authentication_methods`, so an abandoned
enrolment changes nothing about the next sign-in; and a re-registration
rotates the secret, so Helivanta must register exactly once per attempt.

## Decisions

### D1 — A fourth outcome, not a refusal

`loginclient.CompleteIfSufficient` answers `OutcomeEnrollmentRequired` with
`Factors: ["totp"]` where it answered `RefusalMFAEnrollmentRequired`: the org
forces MFA, nothing is enrolled, and nothing uncollectible is enrolled.
`RefusalMFAEnrollmentRequired` and the `mfa_enrollment_required` wire code are
deleted on both sides, not left dormant. The zero `Result` stays a refusal.

### D2 — The secret is minted inside the password step and crosses the wire once

On `OutcomeEnrollmentRequired`, `POST /v1/auth/login/password` registers the
TOTP (`loginclient.RegisterTOTP`, one `POST /v2/users/{id}/totp`), stashes the
session in `login_attempt` exactly as the factor path does (spec D2 of the
native-MFA design: the browser never sees a Zitadel token), and answers:

```json
{"enrollment_required": ["totp"], "totp": {"uri": "otpauth://…", "secret": "…"}}
```

Why here rather than a separate "start enrolment" route: the page needs the
secret the moment it learns enrolment is required, so a second round trip
only adds an unauthenticated route and a window in which "enrolment required"
is known but no secret exists. It also pins the register-once property
structurally: the only way to obtain a secret is to pass the password check
again, which creates a new attempt. There is no route that re-issues a secret
for an existing attempt.

The secret is never logged (the handler logs the outcome and the auth request
id only) and never persisted by Helivanta: `login_attempt` holds the Zitadel
session, not the secret. Zitadel holds the unverified registration, as it
would for its own UI.

If `RegisterTOTP` fails, the attempt is not written and the handler answers
through `respondLoginClientError` (503 for anything Zitadel-side, including the
409 that means a verified TOTP appeared between the classification and the
register call; a retry then takes the factor path).

### D3 — One confirming code finishes enrolment and the sign-in

`POST /v1/auth/login/enroll` `{auth_request_id, factor: "totp", code}`:

1. reads the attempt (missing or expired: `auth_request_invalid`, same as the
   factor route);
2. refuses unless the attempt is **enrolling** (D4);
3. `loginclient.VerifyTOTPEnrollment`: `POST /v2/users/{id}/totp/verify`. A
   400 is a wrong code: the shared equalised refusal and a bump of the same
   five-guess budget the factor route uses (native-MFA spec D6; a sixth wrong
   code deletes the row);
4. the same code is then checked against the session
   (`loginclient.VerifyTOTP`), the rotated token is persisted **before**
   finalize (native-MFA spec D3), and `CompleteAfterFactor` finalizes.
   Steps 4 onward are the factor route's own tail, extracted into one helper
   both handlers call, so the two cannot drift.

The route is unauthenticated for the same reason the factor route is, is
listed in `bootstrap.UnauthenticatedRoutes` with that reason, and takes its
own rate-limit bucket under `FactorRateLimitRule`: it is a six-digit
code-guessing endpoint and must share a budget with nothing else.

Why verify-then-session-check rather than session-check alone: the session
check refuses an unverified registration (`COMMAND-3Mif9s`), so verify must
come first; and `CompleteAfterFactor` re-reads `authentication_methods` and
refuses unless `TOTP` is enrolled, which is only true after verify. The order
is forced, and the fail-closed properties of `CompleteAfterFactor` are kept
rather than bypassed.

### D4 — The attempt knows whether it is enrolling, and each route refuses the other's attempt

`login_attempt` gains `enrolling boolean NOT NULL DEFAULT false` (migration
`0006_iam`, append-only). The password step sets it on
`OutcomeEnrollmentRequired` only.

- `POST /v1/auth/login/factor` on an enrolling attempt answers the equalised
  refusal and bumps the budget, without calling Zitadel.
- `POST /v1/auth/login/enroll` on a non-enrolling attempt does the same.

Zitadel would refuse both anyway (`COMMAND-3Mif9s`; `COMMAND-qx4ls`). The
column makes the refusal Helivanta's own, decided before any round trip,
and indistinguishable from a wrong code, so a caller probing which state an
attempt is in learns nothing. Fail-closed direction: a row that somehow
lacks the flag (the default) can only take the factor path, which cannot
succeed without a verified TOTP.

### D5 — The page renders the QR, the secret, and one code input

`checkPassword` gains the outcome `enrollmentRequired` carrying `factors` and
`totp: {uri, secret}`. The page moves to an enrolment step that shows:

- the `otpauth://` URI as a QR code (`qrcode.react`, SVG, rendered in the
  browser; the API ships no image), with an accessible name;
- the secret as text for manual entry, in a `<code>` element;
- the same OTP input the factor step uses (`AuthOtpStep`, D7's accessible-name
  contract), labelled as a confirmation code, submitting to
  `verifyEnrollment` (`POST /v1/auth/login/enroll`).

Outcomes mirror the factor step: `complete` navigates; `refused` shows the
API's message and clears the input; `expired` and `blocked` end on the
start-again landing. A refresh loses the secret with the rest of the page
state, exactly as the factor step loses its attempt; the clinician signs in
again and receives a new one (D2).

The secret lives in component state only. It is not written to storage, the
URL, or any log.

### D6 — What enrols is bounded by what the sign-in needed

Enrolment is offered only on `OutcomeEnrollmentRequired`: forced MFA, nothing
enrolled. A user whose org does not force MFA is not offered enrolment here;
managing factors after sign-in is out of scope. The attempt TTL
(`loginAttemptTTL`, five minutes) bounds the whole enrolment, which is ample
for scanning a code and typing six digits, and a lapsed attempt is cleaned up
as today.

## Tests

- **loginclient unit:** forced MFA with nothing enrolled answers
  `OutcomeEnrollmentRequired` and never finalizes; `RegisterTOTP` decodes
  `{uri, secret}`; `VerifyTOTPEnrollment` maps 400 to `ErrBadCredentials`.
- **Handler unit (fake Zitadel, real Postgres):** the password step answers
  `enrollment_required` with `totp.uri` and `totp.secret`, writes an enrolling
  row, and the log line carries no secret; a right code on `/enroll` returns
  `callback_url` and deletes the row; a wrong code is byte-identical to a
  wrong password; five wrong codes exhaust; `/factor` on an enrolling attempt
  and `/enroll` on a factor attempt both answer the equalised refusal without
  reaching Zitadel; `/enroll` with an unknown id answers `auth_request_invalid`.
- **Integration (real Zitadel):** a throwaway org with `forceMfa`, a throwaway
  user: password answers `enrollment_required`; a code computed from the
  returned secret on `/enroll` answers 200 with `callback_url` carrying `code`
  and `state`; the user's `authentication_methods` then lists `TOTP`. The
  three existing force-MFA integration tests change their expectation from the
  refusal to this outcome.
- **Shell (Vitest):** the enrolment step renders an SVG QR with an accessible
  name and the secret text; a right code navigates; a wrong code shows the
  message and clears; `mfa_enrollment_required` is removed from the blocked
  codes and its test.
- **Mutations:** `enrolling` guard removed from `/factor`; register call
  moved after the row write with a failing register; secret logged. Each must
  fail a test.

## What this slice does not cover

- Passkeys (#422), email/SMS OTP (#35), signing in with an IdP (#423).
- Managing or re-enrolling factors after sign-in. A user who loses their
  authenticator still needs an administrator to remove the factor in Zitadel.
- Offering enrolment when the org does not force MFA.
