# A required password change is enforced natively, not silently skipped

**Issue:** [#856](https://github.com/tesserix/helivanta/issues/856)
**Amends:** `2026-08-16-hms-login-client-design.md` (D3's table, Risks),
`2026-08-17-native-mfa-auth-components-design.md` (the outcome set) and
`2026-10-07-no-hosted-login-handoff-design.md` (D1's outcome table).

## The problem, stated precisely

Zitadel tells a login client nothing about a password that must change. Verified
live on v4.15.3 (#854 Task 8, spike §5): for a user whose
`human.passwordChangeRequired` is true, `POST /v2/sessions`,
`GET /v2/sessions/{id}` and the finalize call
(`POST /v2/oidc/auth_requests/{id}`) all have the same shape as they do for any
other user. Finalize issues a real authorization code. Helivanta's
`POST /v1/auth/login/password` therefore answers `200 {callback_url}`, and an
administrator's "change this password before continuing" is silently bypassed.

The same holds for a password older than the org's **password expiry** policy
(`maxAgeDays`). Zitadel's own login UI enforces both conditions client-side
(`apps/login/src/lib/verify-helper.ts`, `checkPasswordChangeRequired`, at
`8a54a2a`): it reads the user and the expiry settings and redirects to its
`/password/change` page. Helivanta replaced that UI (#947: users never see
Zitadel's pages), so Helivanta inherits the duty. Leaving expiry out would close
the issue's case while leaving its twin open, so both are in scope.

## Decisions

### D1 — Detection: read the user, and the org's expiry policy, before finalize

`loginclient` gains one read, `passwordState(userID)`, from `GET /v2/users/{id}`
with the login-client PAT. That is the same endpoint `UserState` already reads
(renewal, #908), so it needs no new permission. The read yields:

- `human.passwordChangeRequired`. A false bool is elided from proto3 JSON, so an
  absent key means false. A key that only differs by case or `_` is refused,
  using the same rename guard `loginPolicy` applies to `forceMfa`.
- `human.passwordChanged` (RFC 3339). An absent key means no change was ever
  recorded.

If `passwordChangeRequired` is false and `passwordChanged` is set, the org's
expiry settings are read: `GET /v2/settings/password/expiry`, scoped to the
user's org. The scope is sent both as `ctx.orgId` and as the `x-zitadel-orgid`
header, so neither can silently fall back to the PAT's own org (the #913 bug
class). The password is due when `maxAgeDays > 0` and
`now > passwordChanged + maxAgeDays × 24h`. This is exactly Zitadel's own rule,
including that an absent `passwordChanged` is not expired.

**Fail closed.** These cases are an `ErrUnavailable` error, which is a retryable
503:

- a transport failure or a 5xx;
- a 404 (the user vanished mid-login);
- a 200 with no `user.human` object;
- an unparsable `passwordChanged`;
- an expiry response with no `settings` object, or a non-numeric `maxAgeDays`.

None of these may complete the login, and none may be read as "no change due".

**Cost.** One user read on every login that has passed its factors, and the
expiry read whenever the user has a `passwordChanged` (nearly always). That is
two Zitadel round trips more than before. This is the price #856 named. There
is no cheaper source, because the flag appears nowhere on the session or
finalize wire.

**Read-your-writes.** Zitadel's v2 `GetUserByID` queries with
`shouldTriggerBulk=true` (`internal/api/grpc/user/v2/user_query.go`), so the
projection is brought up to date before answering. A read made right after a
password change sees the change. D5 still refuses rather than loops if it ever
does not.

### D2 — The gate runs last, after every factor

The password gate runs **immediately before finalize** on every path that
finalizes: after the password-only decision (`CompleteIfSufficient`), after a
verified TOTP (`CompleteAfterFactor`, which also completes #948's native
enrolment), and after the change itself (D5). It never runs before a required
second factor is proven. A change step offered before TOTP would let someone who
holds only the password rotate it, locking the real user out of an account whose
second factor the intruder never had. Every factor decision outranks a due
change: an uncollectible factor still refuses, and forced MFA with nothing
enrolled still answers #948's `OutcomeEnrollmentRequired`. The change is asked
for after the enrolment's first code, never instead of it.

### D3 — A new outcome, and a held session with a stage

`loginclient.OutcomePasswordChangeRequired` joins the outcome set.
`Result.PasswordChange` carries:

- `Reason`: `required`, meaning an administrator set the flag, or `expired`,
  meaning `maxAgeDays` elapsed.
- `Policy`: the org's complexity policy (`GET /v2/settings/password/complexity`,
  org-scoped like D1). The form shows it as a requirements checklist. Zitadel
  remains the authority.

The handlers hold the Zitadel session in `login_attempt`, as the factor step
already does (native-MFA spec D2: the browser never sees a Zitadel token). A new
`stage` column (`0007_iam`; `0006_iam` is #948's `enrolling` flag) records which
step the row is waiting for:

| stage | written by | accepted by |
|---|---|---|
| `factor` | Password, on `OutcomeFactorRequired` or (#948, with `enrolling`) `OutcomeEnrollmentRequired` | `POST /v1/auth/login/factor`, or `/enroll` when `enrolling` |
| `password_change` | Password, or Factor/Enroll via `AdvanceToPasswordChange`, on `OutcomePasswordChangeRequired` | `POST /v1/auth/login/password-change` |

`enrolling` stays #948's sub-mode of the factor step, deciding between the two
code routes. `stage` decides between the factor step and the change step.
Advancing a row to `password_change` clears `enrolling`, because the enrolment
is confirmed by then.

The column is `NOT NULL DEFAULT 'factor'` with a `CHECK` constraint, so rows in
flight at deploy time keep their meaning. An endpoint given a row at the wrong
stage answers exactly as it does for a missing row: attempt-expired, with the
same timing floor. A row at `password_change` can only exist after a full
sufficiency decision passed every factor. Changing a password is therefore
structurally unreachable for a session that has not proven them.

The browser receives
`200 {"password_change_required": {"reason", "policy": {min_length, requires_uppercase, requires_lowercase, requires_number, requires_symbol}}}`.
It carries no `callback_url`, so it cannot be mistaken for success, and no
`factor_required`.

### D4 — `POST /v1/auth/login/password-change`

The body is `{auth_request_id, current_password, new_password}`. The endpoint is
unauthenticated for the same reason as its siblings: no Helivanta session exists
yet. It is listed in `bootstrap.UnauthenticatedRoutes` and mounted through
`MountUnauthenticated`, which panics on a nil handler. It is rate limited per
client IP under its own `login_password_change:` bucket, on the shared login
rule.

1. **Validation before Zitadel.**
   - Both fields are required.
   - `new_password` longer than 200 characters gets 422 `password_rejected`.
     That is Zitadel's own proto limit.
   - **`new_password == current_password` gets 422 `password_unchanged`.** This
     is not cosmetic. Zitadel's `ChangePassword` passes both to passwap's
     `VerifyAndUpdate`, which returns `ErrPasswordNoChange`. Zitadel's
     `convertLoginPasswapErr` turns that into an *internal* error (500,
     `COMMAND-CahN2`) after pushing a password-check-**failed** event that counts
     toward the user's lockout (`verifyPasswordWithLockoutPolicy`). Letting it
     through would answer an honest "I typed the same password" with an outage
     and a lockout strike.
2. **Load the row at `password_change`**, or answer attempt-expired.
3. **Change it in Zitadel** with `POST /v2/users/{userId}/password`, body
   `{"newPassword":{"password","changeRequired":false},"currentPassword"}`. The
   user id comes from the held session (`GET /v2/sessions/{id}`), never from the
   browser. The `currentPassword` verification makes Zitadel itself prove the
   caller knows the current password, under the user's lockout policy. This is
   the same proof Zitadel's own login takes (`checkSessionAndSetPassword`
   re-checks it before setting). The PAT's broader `user.write` permission path
   (no verification) is deliberately not used: it would let anyone holding an
   `auth_request_id` at this stage set the password with no knowledge of the old
   one. The browser re-sends the password it typed at the credential step. It
   lives only in page memory and is never persisted (D6).
4. **Map Zitadel's answer.**

   | Zitadel | Helivanta |
   |---|---|
   | 400 `DOMAIN-HuJf6` / `co3Xw` / `VoaRj` / `ZBv4H` / `ZDLwA` (complexity: length, lower, upper, number, symbol) | 422 `password_rejected`, naming the rule. Row kept; the user tries again. |
   | any credential refusal (`IsCredentialRefusal`: wrong current password, locked, …) | Row deleted; 403 `sign_in_incomplete`. The browser sent what the user typed a moment ago, so a mismatch is anomalous and the attempt ends rather than becoming a guessing surface. |
   | `ErrUnavailable` | 503, row kept. |
5. **Re-decide, then finalize.** `CompleteAfterPasswordChange` (D5) runs.
   `OutcomeComplete` deletes the row and answers `{callback_url}`. A refusal
   deletes the row and answers 403, as the other paths do.

### D5 — After the change, the whole decision runs again

`CompleteAfterPasswordChange` re-runs every check the first decision made. It
does not trust that the earlier decision still holds (the same reasoning as
#867's Finding 2):

- the enrolled methods (an uncollectible factor still refuses);
- the factor: a verified TOTP on the session when TOTP is enrolled. Otherwise
  the org must not force MFA; if it does, the factor it requires is unproven
  and the answer is `RefusalFactorNotVerified`. Enrolment (#948) belongs to the
  password step, never this one;
- the password gate.

If the gate still says due after a change Zitadel accepted, the answer is
`RefusalPasswordChangeUnconfirmed` (403 `sign_in_incomplete`, logged as a
defect), never another change prompt. A prompt that cannot clear would loop the
user forever. `finalize` keeps its `sufficient` witness. The new function is a
third producer, and the archtest's finalize call-site pin is unchanged.

### D6 — The shell: a change step built from the design system

- **`login-client.ts`.**
  - `checkPassword` and `checkFactor` gain the
    `{outcome: "passwordChangeRequired", reason, policy}` union member.
  - A new `changePassword` returns `complete | blocked | rejected | expired`.
    `rejected` carries the API's message for `password_rejected` and
    `password_unchanged`.
- **`page.tsx`.**
  - `LoginFlow` gains a `passwordChange` step rendering `@tesserix/web`'s
    `AuthSetPasswordForm`.
  - The form's `passwordPolicy` maps from the API's policy. It shows the live
    requirements checklist and disables submit until the policy and the
    confirmation are met.
  - The description says why: "required" or "expired".
  - A zod check, consistent with the credential form, refuses a new password
    equal to the current one before any request is made. The API refuses it
    too (D4).
- **The current password.** `LoginFlow` keeps the password typed at the
  credential step in React state, so the change step can send it as
  `current_password`, including after an OTP step in between. It is held only
  until the flow ends and is never written to storage.

### D7 — What a reviewer can rely on

- A due password never yields a `callback_url` without a successful change.
  This is pinned by fixtures whose finalize route fails the test if reached.
- No path changes a password before every factor is proven. The stage guard
  pins this with a test that drives `password-change` against a `factor`-stage
  row.

## Not covered

- **The expiry *warning*** (`expireWarnDays`). This is a notice before expiry,
  not a gate. Zitadel's own login does not show it either.
- **Users in `INITIAL` state** (no password yet). They cannot reach this flow,
  because a password session cannot be created for them.
- **Self-service password reset** ("forgot password"). It is a separate flow
  with its own issue.

## Tests

- **loginclient.**
  - The user read covers: required → due; absent key → not due; a renamed key;
    a missing `human`; a 404; a 5xx; an unparsable `passwordChanged`. Each case
    other than the first two is `ErrUnavailable`.
  - Expiry covers: elapsed → due with reason `expired`; not elapsed; `maxAgeDays`
    absent or 0; `maxAgeDays` sent as a JSON string and as a number; the org
    scope sent as both the query parameter and the header.
  - Each finalizing path (`CompleteIfSufficient`, `CompleteAfterFactor`,
    `CompleteAfterPasswordChange`) answers `OutcomePasswordChangeRequired` with
    the policy and never calls finalize while a change is due.
  - A sufficiency refusal outranks a due change.
  - `ChangePassword` sends the documented body with the session's user id, maps
    the complexity ids to `ErrPasswordPolicy` with the rule, and maps a wrong
    current password to `ErrBadCredentials`.
  - `CompleteAfterPasswordChange` still refuses an unverified TOTP and still
    refuses forced MFA with nothing enrolled. A still-due gate is
    `RefusalPasswordChangeUnconfirmed`.
- **HTTP (real Postgres via testcontainers).**
  - Password → `password_change_required` writes a `password_change` row.
  - A good change returns the callback and deletes the row.
  - `password_unchanged` and `password_rejected` keep the row and never call
    Zitadel's set-password for the unchanged case.
  - A wrong current password deletes the row with 403.
  - A `factor`-stage row is refused by `password-change`, never reaching
    Zitadel's set-password, and a `password_change`-stage row is refused by
    `factor`.
  - Factor → `password_change_required` after a good code moves the row's stage.
  - The route is mounted and allowlisted.
- **Migration.** `0007_iam` applies on top of `0004_iam` and `0006_iam`, and an existing row
  reads back as `factor`.
- **Shell (Vitest).**
  - The change step renders after the password step and after the OTP step.
  - The current password is sent from memory.
  - `rejected` stays on the step with the API's message.
  - `complete` navigates.
  - `blocked`/`expired` land on the start-again page.
  - An unchanged password is refused without a request.
- **Mutations** (each must fail the suite):
  - the gate skipped on one finalizing path;
  - the gate moved before the TOTP check;
  - the stage guard removed;
  - the equality pre-check removed;
  - the expiry comparison inverted;
  - the shell navigating on `passwordChangeRequired`.
