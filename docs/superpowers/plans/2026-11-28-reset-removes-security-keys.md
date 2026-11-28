# Authenticator reset removes security keys — plan

Spec: docs/superpowers/specs/2026-11-28-reset-removes-security-keys-design.md (#1905)

## Task 1 — onboarding-service: removal (D2)

- `zitadel.StaffSecondFactorsConfigured(ctx, org, user) (bool, error)` replaces `StaffTOTPConfigured`, built on `authenticationMethods`.
- `zitadel.RemoveRecoveryFactors(ctx, org, subject) (FactorRemovalEvidence, error)` replaces `RemoveRecoveryTOTP`. It lists U2F factors, deletes each one, deletes TOTP if present, then re-reads presence. The evidence is the last ack, and sequences must strictly increase.
- The dispatch uses both.
- Tests (httptest Zitadel):
  - a key-only target;
  - a key-plus-TOTP target;
  - a delete that fails on the second key, which returns an error and makes the dispatch record `unconfirmed`;
  - a key still present after removal, which returns an error;
  - a retried dispatch, which makes no second write.

## Task 2 — onboarding-service: admission (D3)

- `RecoveryFactorEnrollment` searches otp and u2f lifecycle events and returns `Kind`.
- `VerifyRecoverySession(..., kind)` requires the `totp` or `webAuthN` factor.
- Tests:
  - admission by key;
  - a key enrolment with a TOTP-only session, which is refused;
  - a u2f.removed baseline, which is not enrolled;
  - the existing TOTP tests, which stay green.

## Task 3 — helivanta: registry delete after a confirmed removal (D4), bump, copy (D5), runbook, e2e

- `forgetRemovedKeys` deletes the target's rows registered at or before the fence cutoff, after an `awaiting_enrollment` or `completed` receipt in Reset and Read.
- Tests:
  - a DB-backed reset test: the rows survive a failed dispatch, go on the confirmed one, and a replacement row survives a replay and a status read;
  - a registry test: an orphan row doesn't make a different READY key count.
- e2e `staff-mfa-security-key-recovery-native.spec.ts`: a key-only colleague is reset, then re-enrols with a key and is admitted.
