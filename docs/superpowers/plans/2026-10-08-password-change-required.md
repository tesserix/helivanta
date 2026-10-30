# Plan: a required password change is enforced natively (#856)

Spec: `docs/superpowers/specs/2026-10-08-password-change-required-design.md`

## Task 1: loginclient reads and the change

- [x] `passwordchange.go` adds:
  - the `PasswordChangeReason`, `PasswordComplexity` and `PasswordChange` types;
  - `passwordState` (`GET /v2/users/{id}`, using the rename guard and the
    proto3-elision rules);
  - `orgSettings`, which scopes by both `ctx.orgId` and `x-zitadel-orgid`;
  - `passwordMaxAgeDays`, `passwordComplexity`, `expired` and
    `passwordChangeDue`.
- [x] `ChangePassword` sends `POST /v2/users/{id}/password` with the
  `currentPassword` verification, taking the user id from the session. It
  refuses `ErrPasswordUnchanged` and an over-long password before any request.
  Complexity ids become `ErrPasswordPolicy`, with the rule named by
  `PasswordPolicyRule`.
- [x] `zitadelerror.go`: the five `DOMAIN-*` complexity ids are added to
  `badRequestKinds`.
- [x] `client.go`:
  - an injectable clock (`now`);
  - `refuseIfKeyRenamedOrRecased` and `readOptionalBool` take a `where`, so the
    user read reuses the login-policy guards.

## Task 2: sufficiency

- [x] Add `OutcomePasswordChangeRequired`, `RefusalPasswordChangeUnconfirmed`
  and `Result.PasswordChange`.
- [x] `passwordGate` runs last, immediately before finalize, in
  `CompleteIfSufficient` and `CompleteAfterFactor`.
- [x] `CompleteAfterPasswordChange` re-runs the enrolled-methods check, then
  the verified TOTP or forced-MFA check, then the gate, and only then
  finalizes.
- [x] KNOWN LIMITATIONS §1 is replaced by the gate's section. The `sufficient`
  witness docs now name three producers.
- [x] `archtest`: `TestSufficientWitnessConstructionIsPinned` and
  `TestFinalizeCallSiteIsUnique` name `CompleteAfterPasswordChange`.

## Task 3: storage

- [x] Migration `0007_iam` (after #948's `0006_iam` `enrolling` flag) adds
  `login_attempt.stage`, with
  `NOT NULL DEFAULT 'factor'` and a `CHECK` on the two stages.
- [x] `loginAttemptStore`:
  - `Get` takes the stage, and a wrong stage reads as not found;
  - `BumpAndGet` counts only factor rows;
  - `AdvanceToPasswordChange` is new.
- [x] The `migrateLoginAttempt` test helper replaces four copies of the
  0004-only setup.

## Task 4: HTTP

- [x] `Password`: on `OutcomePasswordChangeRequired`, holds the row at
  `password_change` and answers `password_change_required`.
- [x] `Factor`: reads only factor rows. When the outcome after a verified TOTP
  is `OutcomePasswordChangeRequired`, it advances the row and answers
  `password_change_required`.
- [x] `PasswordChange` (`loginui_passwordchange.go`) maps errors as follows:

  | Case | Answer |
  |---|---|
  | `ErrPasswordUnchanged` | 422 `password_unchanged` |
  | `ErrPasswordPolicy` | 422 `password_rejected`, naming the rule |
  | credential refusal | row deleted, 403 `sign_in_incomplete` |
  | outage before the change | 503, row kept |
  | any failure after the change | `password_changed_sign_in_again`, row deleted |
  | missing or wrong-stage attempt | attempt-expired, after the floor |

- [x] Mounting:
  - `bootstrap.UnauthenticatedRoutes` and `MountUnauthenticated` gain the
    fifth handler, which panics if nil;
  - `cmd/api/main.go` wires it;
  - the arch, bootstrap and integration harnesses are updated.
- [x] Rate limiting: `login_password_change:` is its own bucket on the shared
  login rule.

## Task 5: shell

- [x] `login-client.ts`:
  - adds `PasswordChangeRequired` (`reason`, `policy`) to `checkPassword` and
    `checkFactor`;
  - adds `changePassword`, whose outcomes are `complete | rejected | expired |
    blocked`;
  - adds `password_changed_sign_in_again` to `SIGN_IN_BLOCKED_CODES`.
- [x] `page.tsx`:
  - `LoginStep` becomes a union carrying the typed password in memory only;
  - a `PasswordChangeStep` on `AuthSetPasswordForm` maps the policy, describes
    the reason, and refuses an unchanged password locally.

## Task 6: reconcile with #948 (native TOTP enrolment, merged first)

- [x] `stage` sits beside `enrolling`. `Enroll` reads factor-stage rows, and
  `AdvanceToPasswordChange` clears `enrolling`.
- [x] `completeWithVerifiedCode`, shared by Factor and Enroll, advances the
  row on `OutcomePasswordChangeRequired`.
- [x] `CompleteAfterPasswordChange`: forced MFA with nothing enrolled is
  `RefusalFactorNotVerified`, since #948 removed `RefusalMFAEnrollmentRequired`.
- [x] Shell:
  - the `enroll` step carries the typed password;
  - `verifyEnrollment` parses `password_change_required`;
  - `EnrollStep` hands over to the change step.
- [x] Tests:
  - the enrolment fake serves the gate's reads;
  - `TestPasswordChangeAfterANativeEnrolment`, which fails without the
    `enrolling` reset;
  - a Vitest test for enrolment followed by a change.

## Task 7: docs

- [x] Closure notes on:
  - the login-client spec (D3 row and Risks);
  - the native-MFA spec (outcome table and Not covered);
  - the no-hosted-login spec (D1 table);
  - spike §5.

## Tests

- [x] `loginclient/passwordchange_test.go`, per spec §Tests.
- [x] `loginclient/sufficiency_test.go`: every fixture serves a current password
  (`servePasswordCurrent`). Without it, the gate fails closed: five completing
  tests failed until it was added.
- [x] `loginattempt_test.go`:
  - `Get` is bound to the stage;
  - `AdvanceToPasswordChange` works;
  - `BumpAndGet` ignores a password-change row;
  - the stage `CHECK` rejects bad values;
  - rows in flight keep `factor` across `0007_iam`.
- [x] `loginui_passwordchange_test.go`: the HTTP flows over real Postgres.
- [x] `login.test.tsx`: the change step, the OTP-then-change path, rejection,
  each start-again ending, and the local unchanged-password check.

## Mutations

Each mutation must fail the suite, and is then reverted.

- [x] Gate skipped in `CompleteAfterFactor`.
- [x] Gate moved before the TOTP branch in `CompleteIfSufficient`.
- [x] Stage ignored by `loginAttemptStore.Get`.
- [x] Unchanged-password pre-check removed from `ChangePassword`.
- [x] Expiry comparison inverted.
- [x] Shell navigates on `passwordChangeRequired`.
- [x] Shell drops the local unchanged check.
- [x] OTP path forgets the typed password.

## Gates

- `make lint-go`, `go test -race ./...` and `./scripts/coverage-gate.sh`.
- `pnpm turbo lint type-check test build format:check`.

## Not covered

- The expiry warning (`expireWarnDays`).
- Users in `INITIAL` state.
- Self-service reset.
- A live dev-stack integration test. The dev Zitadel is not reachable from the
  environment this was built in. The wire shapes come from Zitadel's source at
  `8a54a2a` (proto, `internal/command`, and `apps/login`'s own
  `checkPasswordChangeRequired`).
