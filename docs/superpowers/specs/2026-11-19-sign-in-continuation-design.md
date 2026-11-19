# Sign-in continuation after account discovery

Resolves tesserix/helivanta#1750. Refines
[tenant-account-chooser](../../design/tenant-account-chooser.md) and
[session lifetime and remembered login](../../architecture/session-lifetime-and-remembered-login.md).

## Problem

1. The emailed discovery code appears on almost every sign-in. It is mailbox
   proof for account discovery, not a second factor, and should appear only on
   a device that has not chosen an account before. The routing hint is written
   only after a completed sign-in, expires after 48 hours, and is lost on every
   start-again landing, so most real sign-ins pay the code again.
2. After the code and the account choice, the credential card shows platform
   branding, "Sign in to continue." and an empty login-name field. It reads as
   a second login. It is the password step and must stay: mailbox proof never
   replaces the password or the organisation's MFA policy.

## Decisions

- **D1. Remember at selection.** `rememberWorkspace` runs when an account is
  chosen (discovery choice, remembered-account button, workspace-link button),
  not only after session exchange. A start-again landing then offers "Sign in to
  your previous account". The hint remains a UUID routing preference with no
  proof and no access; "Use another account" and "Switch account" still clear it.
- **D2. 30-day hint.** `rememberedWorkspaceDuration` becomes 30 days. Nothing
  authenticates from it, so a longer life costs nothing but saves a mailbox code.
- **D3. Carry the proven email as `login_hint`.** Discovery passes the verified
  email to `signinRedirect` as the OIDC `login_hint`. The backend reads
  `loginHint` from Zitadel's auth request and exposes it as `login_hint` on
  `GET /v1/auth/login/request/:id` (omitted when empty). The credential card
  prefills the login name from it, ahead of a remembered username, bounded to
  320 characters. It is display-only: anyone can mint an auth request with any
  hint, so it never selects or authenticates a user, and the field stays
  editable. The remembered-account path sends no hint.
- **D4. Branding follows the chosen account.** `LoginBrandingProvider` reads
  `workspace` as well as `tenant`, and discovery selection retains the chosen
  tenant in the per-tab branding key before the identity-provider round trip.
- **D5. Copy.** The credential card says "Enter your password to continue."
  under the organisation heading. Field labels, the e2e contract, are unchanged.

## Not in scope

Replacing mailbox proof with an authenticator code (impossible before the
account is known), password reset (#425), passkeys (#422), deep-link return
after sign-in, and the other findings of the 2026-11-19 login audit.

## Verification

- Vitest: discovery passes `(tenantId, email)` and writes the hint; workspace
  link writes the hint; remembered account retains branding and sends no hint;
  credential card prefers `login_hint` over the remembered username and shows
  the new copy; hint lifetime is 30 days.
- Go: Zitadel client parses `loginHint`; `/v1/auth/login/request/:id` answers
  `login_hint`.
- Gates: `pnpm turbo lint type-check test build format:check`, `make lint-go`,
  `go test -race ./internal/modules/iam/...`, `go run ./cmd/contractdocs -check`.
