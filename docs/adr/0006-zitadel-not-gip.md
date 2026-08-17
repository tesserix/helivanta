# ADR-0006: Zitadel, not Google Identity Platform

- **Status:** Accepted (2026-08-15). **Supersedes [ADR-0002](0002-gip-not-keycloak.md).**
- **Issue:** [#838](https://github.com/tesserix/helivanta/issues/838)

## Context

ADR-0002 chose Google Identity Platform on the strength of an argument that was
about the organisation rather than the product: the org already ran per-product
GIP tenants with canonical onboarding scripts, so GIP was the path of least
resistance and Keycloak meant operating an IdP. That reasoning no longer decides
the question.

It is worth being precise about what GIP actually cost us, because those costs
are the reason this is a decision and not a preference:

- **Tenant switching was built on a GIP-only primitive.** `POST /v1/iam/me/tenant`
  mints a *custom token* carrying a new `tenant_id` claim, which the browser
  exchanges via `signInWithCustomToken` (`internal/modules/iam/me.go`,
  `apps/shell/components/tenant-picker.tsx`). "Mint an arbitrary token for this
  user" is not an OIDC concept; it is a Firebase one. Nothing standards-based
  replaces it.
- **Identity is asserted by a proprietary custom claim.** The API refuses any
  token without a `tenant_id` claim parseable as a UUID.
- **GIP quota is project-wide**, so one hospital looping the mint endpoint could
  break sign-in for every other hospital — the exposure that motivated the rate
  limiter (#689).
- **The vendor lock was total.** Firebase Admin SDK server-side, Firebase Web SDK
  client-side, an emulator in the dev stack, and a token model with no portable
  equivalent.

## Decision

**All Helivanta authentication uses Zitadel.** It is standards-based OIDC/OAuth2:
tokens are verified as ordinary OIDC ID tokens rather than through a vendor SDK,
and the pieces we depend on are specified rather than proprietary.

Verified against Zitadel's documentation before adopting, because the existing
contracts depend on each:

- **`auth_time` is present in ID tokens.** This is load-bearing: `principalFromToken`
  refuses a token without it, because a credential that cannot be evaluated
  against the revocation watermark is not one that can be trusted (#781). The
  contract survives the move.
- **Custom claims are supported** via the actions "complement token" flow, so a
  `tenant_id`-equivalent claim is expressible.
- **Organizations are the multi-tenancy primitive**, selectable at authorization
  time with the `urn:zitadel:iam:org:id:{id}` scope.
- **Token exchange (RFC 8693) exists**, though it is aimed at impersonation and
  delegation — changing the *subject* — rather than at re-issuing a token for the
  same subject with a different tenant.

## Consequences

**Accepted costs.** We now operate an IdP. That is the exact consequence ADR-0002
counted as a benefit of GIP, and it is the price of the rest of this. It brings
deployment, upgrade, backup and availability obligations (#7), and an IdP outage
is a total sign-in outage.

**MFA, OTP and passkeys** come from Zitadel features rather than GIP ones.
ADR-0002 assumed GIP for these; that assumption is withdrawn, not transferred.

**Tenant switching must be redesigned.** There is no custom-token equivalent, and
Zitadel documents no claim asserting *which* organization a token was issued for
— the roles claim aggregates grants as `role → {orgId: domain}` across every org
a user belongs to. For a clinician who works at two hospitals, "which hospital is
this token for" is therefore not answerable from a stock token. The mechanism is
the subject of the design spec for #838 and is **not** decided here; this ADR
records only that GIP's mechanism is gone and something must replace it.

**The raw-ID-token session must go.** ADR-0002 already carried this as a
follow-on ("swap the raw-ID-token session cookie for Firebase session cookies
before production"). The Firebase half of that sentence is now void, so the
follow-on is not inherited — it is reopened as an open question in the same
design.

**Rate limiting stays.** #689 is provider-independent. The specific exposure that
motivated the tightest budget — project-wide GIP mint quota — changes shape, and
the `Tight` map entry for `POST /v1/iam/me/tenant` must be revisited once the
replacement mechanism exists, since it may no longer call an external service at
all.

**What must not regress**, each an invariant won by earlier work and easy to lose
in a provider swap: global per-subject sign-out (#781), the `NoTenantMembership`
route set that lets a revoked member still end their session, the tenant-less
`SubjectCredentialRevoked` broadcast the outbox policy depends on (#835), token
redaction in logs, and fail-closed behaviour when the IdP cannot be reached.

## Alternatives rejected

**Stay on GIP.** Keeps a working system, but leaves tenant switching built on a
proprietary primitive, identity on a proprietary claim, and a project-wide quota
whose exhaustion is cross-tenant. The lock-in is the problem, not the bill.

**Keycloak.** The IdP ADR-0002 declined. Operating cost is comparable to Zitadel's
and it is equally standards-based, but it does not improve on Zitadel for what we
need, and re-opening that comparison is not what this decision is for.

**Self-built.** Never seriously on the table. Authentication is the last thing to
hand-roll in a system holding clinical records.
