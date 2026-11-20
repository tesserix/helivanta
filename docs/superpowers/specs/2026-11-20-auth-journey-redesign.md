# Onboarding and sign-in: fewer screens, one card, nothing but Helivanta

**Issue:** [#1767](https://github.com/tesserix/helivanta/issues/1767) (umbrella for the journey). Subordinate issues already open: [#1763](https://github.com/tesserix/helivanta/issues/1763) one-email signup, [#1765](https://github.com/tesserix/helivanta/issues/1765) authenticator label, [#1762](https://github.com/tesserix/helivanta/issues/1762)/[#1764](https://github.com/tesserix/helivanta/issues/1764) copy and code boxes (PR #1766), onboarding-service [#49](https://github.com/tesserix/onboarding-service/issues/49) emails, [#50](https://github.com/tesserix/onboarding-service/issues/50) held reset code, [#51](https://github.com/tesserix/onboarding-service/issues/51) Zitadel-sent mail.
**Follows:** [one look (#1255)](../../standards/frontend.md), [signup copy spec](2026-11-20-signup-auth-copy-design.md), [server-side handshake spec](2026-11-20-server-side-oidc-handshake-design.md).
**Reference:** the mark8ly admin sign-in (`apps/admin/app/login`): one centred card, no side panel during the task, server-driven steps. Overrule in the issue if a different reference is wanted.

## Problem

A review of every state on 2026-11-20 (production signup test, then the code) found three things that together read as a legacy product:

1. **Too many screens.** A new owner sees 9 screens, receives 3 emails (one from Zitadel), and makes 4 clicks that do no work: _Verify email_, _Send password link_, a 7-second "password is set" countdown, _Open workspace_.
2. **Too many words.** Each card carries a system description, a footer of reassurances ("Your credential is checked directly by Helivanta", "You will be redirected to sign in securely", "Powered by Tesserix") and a marketing panel beside it, on task screens where the person wants a field and a button.
3. **Three brands.** Helivanta's card, Tesserix's dark hosted Zitadel page and email, and "ZITADEL:owner-…" in the authenticator app.

The returning-user journeys are already right after #1754 and #1758: one card, no email code, no extra click. They are not changed here.

## Decisions

### D1 — One card per task, no side panel during a task

The marketing panel ("A little less complexity…", the care-network illustration) appears on the public entry pages only: `/`, the bare `/login` landing, and the first signup step. Every task screen (signup steps, verify-and-set-password, credential card, chooser, authenticator setup, code entry, password change, signed-out landing) is a single centred card on the plain background, max width 28rem, heading, at most one line of help, fields, one primary action, at most one secondary link.

### D2 — Footers carry one line, or none

The only footer line a task card may show is "Need access? Contact your organisation's administrator.", and only on the credential card. The following are removed everywhere: "Your credential is checked directly by Helivanta.", "You will be redirected to sign in securely.", "Your code is checked directly by Helivanta.", and "Powered by Tesserix" inside the auth flow (it stays in the public page footer).

### D3 — Signup in two short steps, the rest after sign-in

Step 1, **Your organisation**: name, type (hospital or independent pharmacy), state. Step 2, **You**: given name, family name, email. Submit creates the workspace.

Contact phone, organisation email, head office address and main branch are collected after first sign-in, in the existing business profile panel, which the dashboard prompts for until complete. The onboarding contract already allows a signup without a profile (`Setup.Profile` is optional); only Helivanta's form required it. The identity projection uses the profile only for the tenant display name when present, so nothing downstream breaks. The static "01 — 02 — 03" list is replaced by a two-step progress indicator that advances, or removed; the eyebrow "A WORKSPACE OF YOUR OWN" is removed (#1255).

### D4 — One email, one form, straight into sign-in

Per #1763 and onboarding #50: the verification link opens **Set your password**; one submit verifies the email and sets the password. On success the page starts the scoped sign-in at once. The "Your password is set" countdown and **Open workspace** disappear. The emailed password link remains only as recovery for an expired hold.

### D5 — Nothing a customer sees says Tesserix or Zitadel

- Zitadel's password-changed notification is disabled for Helivanta organisations and replaced by a Helivanta notice sent only on real changes (onboarding #51).
- The authenticator entry reads `Helivanta: <email>` (#1765).
- **One email layout for every Helivanta email** (onboarding #49). A single `signupmail` layout (wordmark, one heading, one line of context, one button, expiry, "Didn't ask for this? Ignore this email.", footer with the sending address) renders all five templates: verify email, set password, sign-in code, staff invite, password recovery. Each template supplies only its heading, line, button label, URL or code, and expiry. The text part is generated from the same fields, so HTML and text never drift. A Go test renders every template through the layout and asserts no template carries its own markup. Zitadel-sent emails are disabled for Helivanta organisations (#51), so the layout covers everything a customer receives.
- No Helivanta control links to Zitadel's hosted login. The explicit security-key continuation (ADR-0069) is the one documented exception and stays as it is.

### D6 — Smaller cuts on existing steps

- Authenticator setup: the base32 key collapses behind "Can't scan?". Help text becomes "Scan with your authenticator app, then enter the code."
- Switch account: one click from the menu; the confirm dialog is removed. The menu item label already states the effect, and sign-out is reversible by signing in.
- The signed-out landing keeps its button and wording (#847).

## Out of scope

Server-side OIDC start (its own spec), passkeys (#422), SSO (#423), the new-device email code (#1751 D11), and mobile layout. Mobile is checked for no regression only.

## Order

1. #1766 (copy, code boxes) and #1765 (label): in flight.
2. onboarding #50 then #1763 (D4), with onboarding #49 and #51 (D5) alongside.
3. D1–D2 shell strip and D6: one Helivanta PR, mostly deletion, after D4 so no removed screen is restyled.
4. D3 two-step signup: last, needs the dashboard prompt for the deferred profile.

## Verification

- A new owner reaches the dashboard in **5 screens, 1 email, 0 idle clicks**: signup step 1, step 2, set password, authenticator setup, dashboard. Pinned by a Playwright journey test counting navigations.
- No task screen renders the marketing panel or a removed footer line (Vitest on each step).
- A signup without a profile provisions, and the dashboard shows the profile prompt until the panel is saved (Go + Vitest).
- `e2e/tests/csp.spec.ts` and the #847 sign-out test stay green.
- A production signup after release produces no Tesserix-branded email and no "ZITADEL:" authenticator entry (manual, recorded in the issue).
