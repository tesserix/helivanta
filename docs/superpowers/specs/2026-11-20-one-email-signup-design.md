# One email: verify the address and set the password in one step

**Issues:** [#1763](https://github.com/tesserix/helivanta/issues/1763) (this repo), [onboarding-service#50](https://github.com/tesserix/onboarding-service/issues/50) (identity owner). Part of [#1767](https://github.com/tesserix/helivanta/issues/1767), decision D4.
**Keeps:** every invariant of the [customer signup spec](2026-11-10-customer-signup.md): codes only for the expected native subject, password always set with the native reset code, nothing privileged or code-free, codes never stored or logged.

## Problem

A new owner today: email 1 → click **Verify email** → "Your email is verified" → click **Send password link** → email 2 → set password → "Your password is set" countdown → **Open workspace** → password card. Two emails and four clicks for one credential. The second email proves nothing: the same mailbox was proven seconds earlier, in the same browser, against the same receipt.

## Decisions

### D1 — The verification link opens the password form

`/signup/verify#phase=email&userId=…&code=…` renders **Set your password** (password, confirm, one button). There is no separate **Verify email** click: the submit is the deliberate act that keeps a mail scanner or link prefetcher from spending the one-use code, which is the only reason that click existed.

### D2 — One request does password first, then verification

The form posts `{userId, code, password}` to `POST /api/v1/auth/signup/verify-email` (Helivanta) → `POST /internal/v1/customer-signups/verify-email` (onboarding). `Proof` already carries an optional `password`; `ProveCustomer` currently rejects it on the email phase and now accepts it. Inside that one request the identity owner:

1. Consumes one proof attempt (the existing 5-per-15-minutes budget), which now also bounds password-policy retries.
2. Requests the native reset code with `returnCode` (`SignupCode(subject, password=true)`). The code lives in memory for this request only. **Nothing is held at rest**, which is the change from #50's first draft.
3. **Sets the password first** (`SetSignupPassword(subject, resetCode, password)`). A policy refusal consumes nothing the person needs: the email code is still valid and the same link is retried with another password.
4. **Verifies the email last** (`VerifySignupEmail(subject, emailCode)`). If this fails (expired or used link) the receipt stays `email_action_required` and today's **Send a new link** path covers it. The password already set is recorded as configured; it is harmless on an unverified account because sign-in needs the verified identity.
5. Rotates the receipt as verified and password-configured, as the password phase does today.

Proven on the dev stack on 2026-11-20: a fresh unverified human, `password_reset` with `returnCode`, `POST /v2/users/{id}/password` → 200, then `email/verify` → 200, `isVerified: true`.

Why password-then-verify and not the reverse: the email code is the proof that the mailbox owner is at the keyboard. Keeping it unspent until the password is accepted means a policy failure never leaves the person with a verified account, no password and a spent link.

Why one shot and not a held code: with a held code the receipt cookie alone would be enough to set the password after verification. Today that cookie can only trigger a password email the holder cannot read. The one-shot form keeps the emailed code as a required input, so the cookie never gains that authority.

### D3 — Errors the form can act on

| Outcome                           | Identity owner        | Helivanta             | Form                                                                    |
| --------------------------------- | --------------------- | --------------------- | ----------------------------------------------------------------------- |
| Password refused by native policy | 422 `password_policy` | 422 `password_policy` | Keeps the link; shows "Choose a stronger password" with the policy line |
| Email code wrong, expired or used | 400                   | 400 `signup_refused`  | "This link has expired or was already used. Send a new link."           |
| Native identity unavailable       | 503                   | 503                   | Retry                                                                   |

The policy error is detected the way recovery already detects `ErrRecoveryPasswordPolicy`. It is the only new error shape; no Zitadel text reaches the browser.

### D4 — Straight into sign-in

On success the page starts the scoped sign-in at once (`signinForCustomerSignup(tenantId)` once the receipt is `ready`, exactly what **Open workspace** did). The "Your password is set" countdown and the **Open workspace** screen are removed. While provisioning finishes the page shows the single pending card.

### D5 — What stays

- The password link (`phase=password`) remains, as recovery for an expired hold and for existing receipts: `POST /password` with an emailed code is unchanged.
- The `password_action_required` state remains in the contract for receipts created before this ships; its screen keeps **Send password link**.
- Resend budgets, receipt rotation, audit events and the proof budget are unchanged.

## Order

1. onboarding-service: accept `password` on `/verify-email`, the 422 mapping, `MarkSignupPasswordConfigured`, tests. Deployed first; until then Helivanta's new form would get 400 for a combined submit, so Helivanta ships second.
2. Helivanta: the form, the client, the 422 pass-through, the sign-in hand-off, copy per the [copy spec](2026-11-20-signup-auth-copy-design.md).

## Verification

Identity owner (real PostgreSQL, fake native):

- combined submit → password set, then email verified, receipt rotated once, one `customer.signup.proven` audit;
- policy refusal → 422, email code **not** consumed, receipt unchanged;
- expired email code → 400, password recorded as configured, receipt `email_action_required`;
- native outage at step 2 → 503, nothing consumed;
- six attempts in 15 minutes → the sixth refused by the proof budget.

Helivanta:

- Vitest: the email link renders the password form; a GET of the link makes no request; submit posts `{userId, code, password}` to `verify-email`; 422 keeps the form and the link; 400 shows the expired line; success starts sign-in.
- e2e (`customer-signup.spec.ts`): one email, one form, dashboard; the scanner-GET assertion stays.
- The #1767 journey count: 5 screens, 1 email.

## Staff invitations (#1777, onboarding-service#54)

The same decisions apply to a staff invitation (`/staff/accept`).

- **D1 and D2.** The `phase=email` link opens the password form. Its submit sends `staff-setup` action `email` with `code` and `password`.
- **Identity owner (onboarding-service#60).**
  - It claims one attempt of the invitation's existing proof budget.
  - It sets the password with a native reset code obtained and spent inside the request.
  - It then verifies the emailed code and marks both facts on the setup.
  - A policy refusal is a 422 that spends that one attempt and leaves the link usable.
  - A wrong or expired code is a 400.
- **Helivanta's proxy** passes the 422 through as `password_policy` with the owner wording above. A 400 shows "This link has expired or been used" with a fresh-link button.
- **D4.** Success starts sign-in at once on the single email card (`purpose: "email-login"`, `prompt=login`, `max_age=0`). The page knows no tenant, so the API resolves the account after the password. A failed start leaves a **Sign in** link.
- **Kept:**
  - the ticket and proof ledger;
  - superseded-link refusal;
  - `delivery_unresolved` and `proof_unresolved`;
  - the attempt budget;
  - codes never stored or logged.
- **Links issued before this ships.** A link whose email was already verified keeps the existing password-setup email path.
- **Not covered:**
  - The second password entry at sign-in, which is #1778.
  - An e2e for the staff email journey. The CI identity owner is a fixture, not onboarding-service, so no staff invitation email is sent in e2e. The DB-backed owner test and the shell tests cover the behaviour.
