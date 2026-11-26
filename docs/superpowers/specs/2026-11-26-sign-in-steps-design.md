# Sign-in steps: a way back from every step, and focus that follows the step — design

Issue: #1854 (found by the 2026-11-25 auth-page audit; related to #1767)
Date: 2026-11-26

## Why

The audit found that some sign-in steps cannot be left and that focus is lost between steps. Checked against `main` after #1855:

- **Two-step code step** (`OtpStep`, `app/login/page.tsx`): it renders only `MfaCodeForm`. A clinician without their authenticator can leave only by reloading the page. The security-key step already has "Start again".
- **Account-choice step** (`AccountChoiceStep`): no way back.
- **Account discovery** (`components/account-discovery.tsx`):
  - Its email step's button says "Sign in", but it sends a code.
  - "Retry account selection" is jargon. Its wording is #1856's.
  - When it moves from email to code to choices, the pressed button unmounts and focus drops to `<body>`.
  - Its code field is a plain input, where every other code entry uses `MfaCodeForm`.
- **"Remember my username"**: a native 16px checkbox placed after the submit and "Forgot password?" controls.
- **Recovery without an auth request** already links to "Start sign-in" (`components/password-recovery.tsx`). That part of the issue is already resolved.

## Decisions

### D1 Every step has a visible way back

`OtpStep`, `EnrollStep` and `AccountChoiceStep` gain a "Start again" outline button, as the security-key step already has. It calls the existing `end(...)`, which drops the password and any TOTP secret.

- The two code steps add one line above it: "Can't use your authenticator? Ask your organisation's administrator to reset it." The workforce zone already has the reset, "Reason for authenticator reset".
- The landing message after "Start again" is the security-key step's existing sentence, "Start a new sign-in attempt to use another account."
- No new path is added. "Start again" is the same as an attempt ending: back to the start-again landing, never to Zitadel's hosted login.

### D2 Focus follows the step

When the sign-in step changes, focus moves to what the step is for:

- **Code steps** (two-step code, authenticator set-up): the first digit, which `MfaCodeForm` focuses itself. The step's one job is its code, and the digit group's label announces the step, so typing can start at once.
- **Every other step** (account choice, security key, password change, the start-again landing): the step's heading.
- **Account discovery:** the email field when it returns to the email phase, and the first digit on the code phase. The choices phase gets its "Choose an account" heading. The email and code phases have no headings, and adding them would be new copy.

- A focused heading takes `tabIndex={-1}`, the pattern the country-policy editor uses. The credential step's heading stays unfocusable, and its own autofocus on the login name is kept.
- Account discovery's code field becomes `MfaCodeForm`, with the same contract (`AccountDiscoveryCodeContract`, 8 digits) and the same request.

### D3 Buttons say what they do

The discovery email step's button changes from "Sign in" to "Send code", and its pending label stays "Sending code…".

### D4 The remember-username control

- **This repo:** the checkbox gets a target of at least 24px (WCAG 2.5.8). The 16px box sits inside a `min-h-6` label row, so the whole row is the target, and it uses design tokens.
- **Not this repo:** moving it before Submit needs a slot in `@tesserix/web`'s `AuthCredentialForm`, which has none. It is filed against that package; this repo does not fork the form.

## Not covered

- Recovery and discovery wording (#1856).
- The shared sign-in card (#1853).
- The checkbox's position before Submit (needs the `@tesserix/web` slot).

## Copy for approval

- "Start again" on the code, set-up and account-choice steps.
- The authenticator line in D1.
- "Send code" in D3.
