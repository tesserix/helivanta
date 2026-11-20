# ADR-0074: Passkeys on the Helivanta origin

Status: proposed, 2026-11-20. Accepted only after the spec's spike confirms Zitadel accepts this relying party.
Design: [passkey sign-in spec](../superpowers/specs/2026-11-20-passkey-sign-in-design.md),
issue [#422](https://github.com/tesserix/helivanta/issues/422). Narrows
[ADR-0069](0069-explicit-native-security-key-continuation.md) to legacy U2F credentials; keeps
[ADR-0070](0070-tenant-owned-login-identities.md) unchanged.

## Context

Users asked for sign-in without a password once MFA is set up. Today a returning user gives a password and
then a TOTP code. A passkey registered by Helivanta's sign-in code is refused as uncollectible. Security keys
registered on Zitadel's own domain can only be used through the hosted-login continuation of ADR-0069.

## Options

1. **Password-free with TOTP only.** Rejected: one phishable factor, and it makes the six-digit code a target
   for lockout and guessing.
2. **Emailed magic link or code for every sign-in.** Rejected: an email per sign-in, and the mailbox becomes
   the only factor.
3. **Send passkey users to Zitadel's hosted login, as ADR-0069 does for U2F.** Rejected: it leaves Helivanta,
   shows Tesserix branding (#1767 D5), and binds credentials to Zitadel's domain.
4. **Native passkeys with relying party `helivanta.app`, stored in Zitadel and collected by Helivanta through
   Zitadel's User and Session APIs.** Chosen.

## Decision

Helivanta registers and collects passkeys on its own pages with relying-party ID `helivanta.app`. Zitadel stores
the credentials and runs the WebAuthn verification. Helivanta never stores credential material. Zitadel does not
enforce `forceMfa` for a login client, so Helivanta's sufficiency check decides that a user-verified passkey
satisfies it without a password. Finalization still goes only through the existing pinned witness
(`CompleteIfSufficient`, `CompleteAfterFactor`).

Credentials are per tenant account, as ADR-0070 requires. The account chooser is never shown before a
credential is proven. ADR-0069's continuation remains for U2F credentials registered on Zitadel's domain.

## Consequences

- One-way door: credentials are bound to `helivanta.app`. Serving the console from another registrable domain
  orphans them unless related origins are configured first. Tenant paths and subdomains of `helivanta.app` are
  covered.
- Sign-in costs no email or SMS and no new datastore. `login_attempt` gains one stage value.
- Recovery must remove passkeys as well as TOTP before enrolment ships. Until it does, enrolment stays disabled.
- Zitadel availability remains the sign-in dependency, with the same fail-closed behaviour as the password path.
