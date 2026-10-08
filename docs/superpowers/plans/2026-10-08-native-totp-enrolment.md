# Plan — Native TOTP enrolment (#948)

Spec: `docs/superpowers/specs/2026-10-08-native-totp-enrolment-design.md`

## Task 1 — loginclient

- [x] `OutcomeEnrollmentRequired`; `RefusalMFAEnrollmentRequired` deleted;
      `CompleteIfSufficient` answers the new outcome under `forceMfa` with
      nothing enrolled.
- [x] `RegisterTOTP(ctx, sessionID) (TOTPEnrollment, error)`:
      `POST /v2/users/{id}/totp` via `sessionSubject`.
- [x] `VerifyTOTPEnrollment(ctx, sessionID, code) error`:
      `POST /v2/users/{id}/totp/verify`, 400 → `ErrBadCredentials`.
- [x] Unit tests for all three; the forced-MFA test asserts the new outcome
      and no finalize.

## Task 2 — iam handlers and store

- [x] Migration `0006_iam`: `login_attempt.enrolling boolean NOT NULL DEFAULT false`.
- [x] `loginAttempt.Enrolling`; `Put` writes it.
- [x] Password: on `OutcomeEnrollmentRequired` register, write an enrolling
      row, answer `{enrollment_required, totp:{uri,secret}}`; no secret in logs.
- [x] `Enroll` handler per spec D3; shared `completeWithCode` tail used by
      `Factor` too; D4 cross-guards in both.
- [x] Unit tests per spec; fake Zitadel gains `/totp` and `/totp/verify`.
- [x] Integration test with a forced-MFA org; three existing force-MFA tests
      updated; TOTP generator ported to Go test helper.

## Task 3 — routing and config

- [x] `bootstrap.UnauthenticatedRoutes` + `MountUnauthenticated` gain
      `POST /v1/auth/login/enroll`; all callers updated; allowlist tests say
      five.
- [x] `main.go` wiring and comment; `enrollRateBucket` under `FactorRateLimitRule`.

## Task 4 — shell

- [x] `qrcode.react` dependency.
- [x] `login-client.ts`: `enrollmentRequired` outcome, `verifyEnrollment`,
      `mfa_enrollment_required` removed from blocked codes.
- [x] `page.tsx`: `EnrollStep` per spec D5.
- [x] Vitest per spec.

## Task 5 — docs

- [x] Amend the three specs named in the header; `main.go` comment.

## Mutations (each must fail a test, then be reverted)

- `enrolling` guard removed from `Factor`.
- Row written before register with register failing.
- Secret included in the password log line.

## Gates

- `make lint-go`; `GOTOOLCHAIN=go1.26.6 ./scripts/coverage-gate.sh`.
- `pnpm turbo lint type-check test build format:check`.
