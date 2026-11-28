# Authenticator reset removes security keys

Issue: #1905. Related: #1703 (the reset), #1898 (native security keys).
Spans helivanta and onboarding-service.

## Problem

Since #1900 a Staff member can register a security key on Helivanta's sign-in
page as their only second step. The authenticator reset (#1703) cannot help
them if they lose it:

- onboarding-service's dispatch checks only `StaffTOTPConfigured`. A key-only
  target counts as "nothing configured", so the request is cancelled.
- The dispatch deletes only `/v2/users/{id}/totp`.
- Admission proves only a new TOTP. Both the `otp` lifecycle event and the
  session's `totp` factor are required. So a person who re-enrols with a key on
  Helivanta's enrol step (which already offers one) is refused.
- Helivanta's `iam_security_keys` registry keeps the lost key's row.

## Decisions

### D1 Who can be reset is unchanged

`ordinaryMFATarget` and every other authority check stay exactly as they are.
Owners and administrators are still refused.

### D2 Dispatch removes every second factor (onboarding-service)

- **Presence check.** `StaffSecondFactorsConfigured` reads
  `authentication_methods` and reports TOTP and U2F separately. It is still a
  presence check, not proof. Neither present means cancel, as today.
- **Removal.** `RemoveRecoveryFactors` replaces `RemoveRecoveryTOTP`, under the
  same durable claim:
  1. List the user's U2F factors (`POST /management/v1/users/{id}/auth_factors/_search`)
     in every state. A NOT_READY key is removed too: it is an unfinished
     registration, and removing it costs nothing.
  2. Delete each one (`DELETE /v2/users/{id}/u2f/{u2fId}`).
  3. Delete TOTP if it was present.
  4. Re-read `authentication_methods`. Any TOTP or U2F still there means the
     result is uncertain.
- **Acknowledgements.** Every ack is validated as `RemoveRecoveryTOTP` validates
  one today: positive sequence, matching resource owner, sane change date.
  Sequences must strictly increase.
- **Evidence.** The evidence recorded is the last ack. It carries the highest
  sequence, so it is the baseline admission compares against.
- **Failure.** Any failed, lost or inconsistent step makes the whole removal
  `unconfirmed`. Nothing is retried automatically. The runbook rule still
  applies: escalate, and don't create a replacement request. A half-done removal
  is therefore never reported as `awaiting_enrollment`.
- **Retries.** A retried dispatch hits the existing claim. A non-prepared command
  is disclosed, never re-sent.

### D3 Admission accepts either new factor (onboarding-service)

- **Enrolment proof.** `RecoveryFactorEnrollment` reads the latest lifecycle
  event across:
  - `user.human.mfa.otp.{added,verified,removed}`;
  - `user.human.mfa.u2f.token.{added,verified,removed}`.

  It applies today's baseline rule. At the baseline sequence the event must be a
  `*.removed` with the baseline change date. Only a later `*.verified` event
  counts as enrolled, and it carries its kind (`totp` or `security_key`). The
  key's sign-count event is not searched, so signing in with the new key doesn't
  hide its verification.

- **Session proof.** `VerifyRecoverySession` takes the kind and requires that
  factor on the native session: `totp`, or `webAuthN` for a key. It must be
  verified after enrolment, with today's password, creation-time and
  password-changed rules. A session that only proves the other kind is refused,
  so admission proves the person used the factor they just enrolled.
- **Recheck.** The second read of the event must match the first, as today.
- Store columns, audit actions and the contract are unchanged.

### D4 The registry rows go when the reset is fenced (helivanta)

- **Where.** The fence transaction (`staffMFAResetService.fence`) already revokes
  the target's sessions and records the fence atomically.
  - It now also deletes the target's `iam_security_keys` rows.
  - The table is global and has no RLS, so the delete runs in the same
    transaction.
  - The delete runs only on the call that inserts the fence. A retry that finds
    the fence has nothing left to do, so a retry can't leave half a reset behind.
- **Why before the Zitadel removal, not after.** After dispatch the person
  re-enrols, and a later status read would delete the new key's row.
- **Leftover rows can't count.** A key registered between the fence and the
  removal is removed by Zitadel, which orphans its row. Orphaned rows can't make
  a later key count: `Contains` requires the IDs Zitadel reports as READY, and
  Zitadel never reuses a key ID. A test pins this.

### D5 Copy

Two strings in `staff-mfa-recovery.tsx` and the runbook name only an
authenticator app. They change to name either factor. The wording needs the
user's approval (proposed in the PR):

- **The confirmation.** "They will need to sign in and set up a new
  authenticator app or security key."
- **The `awaiting_enrollment` status.** "Ask the Staff member to sign in and set
  up a new authenticator app or security key."

## Delivery

1. **onboarding-service PR.** D2 and D3, with tests:
   - a key-only target;
   - a key-plus-TOTP target;
   - a Zitadel failure partway through removal (`unconfirmed`, not
     `awaiting_enrollment`);
   - a retried dispatch;
   - admission by key;
   - admission refused when the session proves the other kind.

   This is backward compatible: TOTP-only resets behave as they do now.

2. **helivanta PR.** It includes:
   - the go.mod bump to the merged onboarding commit;
   - D4 and its tests;
   - D5;
   - the runbook;
   - an e2e: key-only Staff → reset → Zitadel shows no U2F → re-enrol with a
     key (CDP virtual authenticator) → admitted.

## Not covered

- Moving legacy `auth.tesserix.app` keys for owners (#1898 slice 3).
- Self-service key removal from the account page (#1898 slice 4).
- Passkeys, OTP email/SMS and recovery codes.
  - Helivanta can't collect them, so sign-in already refuses them as
    uncollectible.
  - The reset doesn't remove them, and the final presence read ignores them.
  - A target holding one stays refused at sign-in, as today.
