# Signup and sign-in copy, code entry and authenticator label

**Issues:** [#1762](https://github.com/tesserix/helivanta/issues/1762) (page copy), [#1764](https://github.com/tesserix/helivanta/issues/1764) (code boxes), [#1765](https://github.com/tesserix/helivanta/issues/1765) (authenticator label).
**Companion:** [onboarding-service#49](https://github.com/tesserix/onboarding-service/issues/49) (email copy, using the email section below) and [onboarding-service#51](https://github.com/tesserix/onboarding-service/issues/51) (Zitadel-sent emails).
**Not here:** the one-email verify-and-set-password journey ([#1763](https://github.com/tesserix/helivanta/issues/1763) and [onboarding-service#50](https://github.com/tesserix/onboarding-service/issues/50)), which gets its own spec. The copy below is for today's two-email journey, and #1763 replaces the email-verified/password-request states.

Found in a production signup test on 2026-11-20. The reader is an owner or clinician setting up a hospital or pharmacy, often on a shared terminal and often in a hurry.

## Copy rules

1. **One line per screen** says what to do next. A second line appears only if it changes the person's decision.
2. **Rate limits appear only on refusal.** The 429 already carries its own message. They are never preamble. `EmailDeliveryPolicy` is removed from every screen.
3. **No mechanism talk.** "Separate login for this workspace" and "enrol its own second factor" describe the system, not the task. The first sign-in teaches the second factor, at the moment it is needed.
4. **Non-enumerating wording stays.** "If this request can receive an email…" is kept wherever an email is requested for an address the page has not proven.
5. **Sentence case**, verb-first buttons, and "email" not "e-mail". Keep "workspace" (the product term), not "organisation account".

## Pages (`apps/shell/app/signup`)

| State                      | Heading                   | Body                                                            | Action             |
| -------------------------- | ------------------------- | --------------------------------------------------------------- | ------------------ |
| `email_action_required`    | Check your email          | We sent a verification link to your email. Open it to continue. | Resend email       |
| `password_action_required` | Your email is verified    | Next, set your password. We'll email you a link.                | Send password link |
| provisioning               | Setting up your workspace | This usually takes a few seconds.                               | —                  |
| `needs_support`            | Setup needs a hand        | Contact support. We've saved everything you entered.            | —                  |
| `ready`                    | {name} is ready           | Sign in to finish setting up security and your team.            | Open workspace     |

- **Under the action:** "Check spam if it hasn't arrived." It appears only in the two waiting-for-email states.
- **Create another workspace:** the helper sentence is dropped. The button label already says it.
- **Resend accepted:** "If this address can receive email, a new link is on its way." This is non-enumerating. The receipt does not carry the owner's email, so no screen names the address.
- **Resend refused (429):** "You've requested several emails. Try again in about an hour." This is where the limits are mentioned.

**`/signup/verify`:**

| Situation                     | Copy                                                                                                   |
| ----------------------------- | ------------------------------------------------------------------------------------------------------ |
| Link opened, before the click | Heading: **Verify your email**. Button: **Verify email**. Secondary: **Send a new link**. No preamble. |
| Password link                 | Heading: **Set your password**. Fields and submit only.                                                |
| Expired or used link          | Alert: "This link has expired or was already used. Send a new link."                                   |
| Incomplete link               | "This link is incomplete. Open the most recent email from Helivanta."                                  |

## Code entry (#1764)

**Root cause is local.** `OTPInput` (`@tesserix/web`) gives each slot `text-center`. `packages/ui/styles.css` has `.login-content .text-center { text-align: left; }` (#1090), which left-aligns the auth card's text, and that rule matched the slots too. It pushed both the placeholder and typed digits against each box's left edge.

**Fix:** narrow the rule to `.login-content .text-center:not(input)`. No upstream change is needed.

**Proof:** an e2e test in `mfa.spec.ts` asserts the computed `text-align` of all six boxes is `center` in a real browser.

## Authenticator label (#1765)

`loginclient.RegisterTOTP` receives Zitadel's `otpauth://totp/{issuer}:{account}?secret=…&issuer=…`. Before the page sees it, Helivanta rebuilds only the display parts:

- **Issuer:** `Helivanta`, in both the label prefix and the `issuer=` parameter.
- **Account:** the user's verified email from the same session subject. If no email is available, the account becomes `Helivanta account`, never the generated `owner-…` username.

Every other query parameter (`secret`, `algorithm`, `digits`, `period`) is copied unchanged, so codes stay valid. A URI that is not `otpauth://totp/`, has no `secret`, or does not parse **fails closed** with the existing unavailable error, and the page never renders a half-rewritten QR. The manual-entry secret shown under the QR is unchanged.

Workspace name in the label: deferred. It needs a tenant lookup on the password path. The email already separates the person's entries per address, and #1763's journey revisits this.

## Emails (onboarding-service#49)

Every email gets a greeting, one line of context, one button, and its expiry. Rate limits and the account model stay out of emails.

| Email        | Subject                                                         | Body                                                                                                                                               |
| ------------ | --------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| Verify email | Verify your email for {workspace}                               | Hi {name}, confirm this is your email to finish setting up {workspace} on Helivanta. **[Verify email]** · This link expires in 1 hour.             |
| Set password | Set your Helivanta password                                     | Hi {name}, set a password for {workspace}. **[Set password]** · Expires in 1 hour.                                                                 |
| Sign-in code | Your Helivanta code: {code} only in the body, never the subject | Your code is **{code}**. Enter it in the browser where you asked for it. It expires in 10 minutes.                                                 |
| Staff invite | You're invited to {workspace} on Helivanta                      | Hi {name}, {workspace} has invited you to Helivanta. **[Accept invite]** · Expires {date}. Your administrator confirms your access after you join. |
| Recovery     | Reset your Helivanta password                                   | (as today, shortened in the same pattern)                                                                                                          |

Every email ends with "Didn't ask for this? You can ignore this email." The HTML and text parts carry the same words, and codes never appear in subjects.

## Verification

- **Vitest:** each signup state renders the copy above. No screen renders the delivery-policy text before a refusal, and a 429 renders the refusal line.
- **e2e:** computed `text-align: center` on every code box (`mfa.spec.ts`).
- **Go, label:**
  - The rebuilt URI keeps `secret`, `algorithm`, `digits` and `period` byte-for-byte.
  - The issuer and account are as specified and percent-encoded.
  - A non-TOTP URI, a missing secret or an unparsable URI fails closed.
  - The integration test enrols from the rebuilt URI and verifies a code against Zitadel.
