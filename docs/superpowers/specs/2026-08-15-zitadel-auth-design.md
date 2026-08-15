# Zitadel authentication and the HMS session — design

**Issue:** [#838](https://github.com/tesserix/hms/issues/838) — Replace Google
Identity Platform with Zitadel.
**Status:** draft 2026-08-15.
**Decides:** ADR-0006. **Rests on:** `docs/superpowers/spikes/2026-08-15-zitadel-spike.md`
— every claim below marked *(observed)* was seen against a running Zitadel
v2.65.1, not read in documentation.
**Related:** #781 (revocation), #689 (rate limiting), #45 (secrets), #835
(the tenant-less broadcast this must not break), #7 (deployment).

---

## Problem

ADR-0006 replaces GIP with Zitadel. GIP was not a token verifier we can swap —
it was three couplings:

1. **Tenant switching** minted a GIP *custom token* carrying a new `tenant_id`
   claim, which the browser exchanged via `signInWithCustomToken`. Zitadel has no
   equivalent, and *(observed)* no claim asserts which organization a token was
   issued for — the roles claim aggregates grants across every org as
   `role → {orgId: domain}`. For a clinician working at two hospitals, "which
   hospital is this token for" is unanswerable from a stock token.
2. **Identity** is asserted by a `tenant_id` custom claim the API refuses tokens
   without.
3. **Revocation** depends on `auth_time` plus GIP refresh-token revocation.

## What the spike settled

- **Standard OIDC verification works with no vendor SDK** *(observed)*:
  `go-oidc` performed real discovery, fetched the real JWKS, and did a real
  RS256 check. Proved non-vacuous by rejecting a wrong audience and a tampered
  signature. **This is ADR-0006's premise and it holds.**
- **`auth_time` is present and distinct from `iat`** *(observed)*. The #781
  revocation contract survives.
- **`sub` is a stable opaque identifier** *(observed)*.
- **`profile`/`email` scopes do NOT land in the ID token** *(observed)* — they
  come from `/oidc/v1/userinfo`.
- **Deactivating a user does not invalidate an already-issued ID token**
  *(observed)*; the access token was refused at `userinfo`. **Introspection
  requires a confidential client**, which a browser never is.

That last point is the one that shapes this design.

---

## Decisions

### D1 — Zitadel authenticates; HMS issues its own session

At login, HMS verifies the Zitadel ID token once, checks membership, and mints
**its own short-lived session token**. Every subsequent request presents the HMS
session, never the Zitadel token.

This is not a convenience. Three independent reasons converge:

- **Tenant is HMS's fact, not Zitadel's.** Membership lives in OpenFGA and is
  reconciled by HMS. Asking the IdP to assert it means teaching an external
  system a fact we already own, then trusting its answer.
- **There is nowhere else to put the tenant** *(observed)* — no stock claim
  says which org a token is for, so any Zitadel-side approach needs an action
  injecting a custom claim, and switching hospital becomes a redirect through
  the hosted login. A clinician moving between hospitals mid-shift should not
  hit a full re-auth.
- **Revocation cannot be delegated anyway.** *(observed)* a deactivated user's
  ID token still verifies, and introspection is closed to public clients. HMS
  must own a watermark regardless — and it already does (#781).

It also closes a debt ADR-0002 itself recorded: "swap the raw-ID-token session
cookie for … session cookies before production". The raw IdP token was never
meant to be the session.

**The cost, stated plainly: HMS becomes a credential issuer.** Signing keys to
generate, store, rotate; a second token format; and a bug in that path is an
authentication bypass, not a defect. That cost is accepted here, and D5 is how
it is contained.

*Rejected: Zitadel actions injecting a tenant claim + org-scoped re-auth.* Keeps
one issuer and no HMS keys, but makes every hospital switch a redirect, and
depends on an action reading the requested org scope — **NOT VERIFIED** in the
spike.

*Rejected: RFC 8693 token exchange.* Aimed at impersonation — changing the
subject — not at re-issuing for the same subject in another tenant. Still needs
a tenant claim from somewhere, and Zitadel recommends restricting it to
confidential clients.

### D2 — The HMS session carries exactly what `Principal` needs

`sub`, `tenant_id`, `auth_time`, `exp`, `iat`, `iss`. Nothing else — no email,
no name, no roles.

Roles are deliberately absent: permissions are resolved per request from OpenFGA
by `authz.Middleware`, and a role baked into a token is a stale answer that
survives until expiry. Permission changes must take effect on the next request,
not the next login.

`auth_time` is **carried through from the Zitadel token, not reset to now**. It
means "when did this human last authenticate", and the revocation watermark
compares against exactly that. Re-minting on tenant switch must not launder an
old authentication into a fresh one.

### D3 — Tenant switching re-mints the HMS session

`POST /v1/iam/me/tenant` keeps its route and its tight rate-limit budget, but
changes what it does: verify the caller's current session, check membership of
the target tenant in OpenFGA, mint a new HMS session with the new `tenant_id`
and the **same `auth_time`**, set the cookie.

No IdP round trip. No redirect. Consequently the `Tight` rate-limit entry for
this route is no longer protecting a project-wide external quota — it is
protecting an FGA call and a signature. **The budget should be revisited when
this lands** (#689 assumed the GIP exposure), and this spec does not silently
inherit its reasoning.

### D4 — Revocation stays HMS's, with an explicit upstream bound

The #781 watermark is unchanged and remains authoritative: sign-out and admin
revoke write a per-subject watermark, HMS sessions with an earlier `auth_time`
are refused, and the broadcast invalidates every replica's cache. The
`SubjectCredentialRevoked` event still publishes with **no tenant** — #835's
outbox policy depends on that and it must stay true.

What is genuinely new is **upstream deactivation**: a user disabled in Zitadel
holds a valid HMS session until it expires *(observed: their ID token still
verified; introspection is unavailable to us)*.

**Decision: HMS session TTL is the upper bound on upstream deactivation latency,
and that bound is stated rather than discovered.** Short TTL with silent renewal;
renewal re-checks the Zitadel session, so a deactivated user fails at the next
renewal rather than at the next login. The exact TTL is a number the plan must
choose and justify — it trades renewal traffic against how long a disabled
account keeps working.

*Rejected: introspect per request.* Not available to a public client
*(observed)*, and a network call per request against the IdP recreates exactly
the shared-resource exposure #689 exists to prevent.

### D5 — Signing keys, and failing closed without them

Asymmetric (EdDSA or RS256), private key from the secret manager, public key
exposed for verification. Asymmetric over a shared HMAC secret because a
symmetric key means anything that can *verify* can also *mint* — and ADR-0005
anticipates services being extracted later, at which point a verifier holding
minting power is a privilege escalation waiting to happen.

**The API must refuse to boot without a signing key.** Not generate one, not
fall back to a default: an ephemeral key silently invalidates every session on
restart, and a default key is a forged-session vulnerability. This is a data
control and fails closed (§3), unlike `LOG_LEVEL`.

**This makes #45 (secrets management) a hard dependency** — the first time HMS
has one. The plan must say what dev uses (a fixed local key, clearly marked and
refused outside dev, mirroring the existing `HMS_ENV` emulator guard) and what
production requires.

Key rotation is **out of scope here** and must be filed separately; the design
must not preclude it (a `kid` header from the start).

### D6 — Dev stack and seeding

Zitadel replaces the Firebase emulator in the dev stack, on its own database
*(observed: works alongside `hms-dev` with shifted ports)*.

*(observed)* two wrinkles the plan must handle: a machine user has **no
permissions until granted an org role**, and API-created human users land
**email-verification-pending**, which blocks interactive login even with
`isEmailVerified: true`. `scripts/seed-dev.mjs`'s replacement must produce users
that can actually log in through the hosted UI, because the e2e suite drives a
real login form. **This is the single most likely thing to consume time**, and
the spike explicitly did not resolve it.

Seeded accounts must stay **one per spec file** — sign-out revokes globally per
subject, so shared accounts race (#781).

### D7 — One cutover, not a dual-provider period

Both providers live at once would mean two token formats, two revocation
models, and a `Principal` that could come from either — the branchiest possible
version of the most security-sensitive code in the repo, and precisely the
"half-built so it looks present but does not hold" §1 forbids.

The seam that makes a single cutover safe already exists: `TokenVerifier` /
`TokenMinter` / `TokenRevoker` in `pkg/authn`. GIP is deleted in the same change
that adds Zitadel.

---

## What must not regress

Each is an invariant won by earlier work, each re-proven rather than assumed:

| Invariant | Pinned by |
|---|---|
| Sign-out revokes globally per subject | `e2e/tests/signout.spec.ts` |
| A token captured before sign-out is refused after | same |
| A revoked member can still end their session | `NoTenantMembership` route set |
| Switching hospital changes what the user can do | `e2e/tests/tenant-switch.spec.ts` |
| Rate-limit exemptions name real routes | `TestRateLimitPolicyRoutesAreRegistered` |
| `SubjectCredentialRevoked` carries no tenant | #835's outbox policy + its tests |
| Tokens never reach logs | `pkg/logging` redaction |
| Auth failures deny; an unreachable IdP denies | §3 |

---

## Limitations

- **Upstream deactivation is bounded by the session TTL**, not immediate. Stated,
  not hidden; immediate would require per-request introspection, which is
  unavailable and would be its own outage risk.
- **HMS now issues credentials.** New signing-key operational surface, and a new
  class of bug whose failure mode is authentication bypass.
- **Key rotation is not designed here.**
- **`email`/`name` need a `userinfo` call** *(observed)*, so any UI showing them
  needs a fetch HMS does not do today.
- **We now operate an IdP** — ADR-0006's accepted cost. An IdP outage is a
  total sign-in outage, though existing HMS sessions survive until renewal,
  which is a modest resilience gain over today.
- **Not verified in the spike:** whether Zitadel can push deactivation events
  (which would tighten D4), whether an instance setting forces profile/email
  into the ID token, and Zitadel's behaviour at production scale.

## Out of scope

- Key rotation (file separately).
- MFA, passkeys, OTP as features — ADR-0006 withdraws the assumption that these
  come from GIP; where they come from now is a separate decision.
- OpenFGA authorization, except where the subject identifier format changes.
- Rate-limit budgets, beyond noting D3 invalidates the mint route's rationale.
