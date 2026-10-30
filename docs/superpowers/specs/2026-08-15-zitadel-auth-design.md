# Zitadel authentication and the Helivanta session — design

**Issue:** [#838](https://github.com/tesserix/helivanta/issues/838) — Replace Google
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

### D1 — Zitadel authenticates; Helivanta issues its own session

At login, Helivanta verifies the Zitadel ID token once, checks membership, and mints
**its own short-lived session token**. Every subsequent request presents the Helivanta
session, never the Zitadel token.

This is not a convenience. Three independent reasons converge:

- **Tenant is Helivanta's fact, not Zitadel's.** Membership lives in OpenFGA and is
  reconciled by Helivanta. Asking the IdP to assert it means teaching an external
  system a fact we already own, then trusting its answer.
- **There is nowhere else to put the tenant** *(observed)* — no stock claim
  says which org a token is for, so any Zitadel-side approach needs an action
  injecting a custom claim, and switching hospital becomes a redirect through
  the hosted login. A clinician moving between hospitals mid-shift should not
  hit a full re-auth.
- **Revocation cannot be delegated anyway.** *(observed)* a deactivated user's
  ID token still verifies, and introspection is closed to public clients. Helivanta
  must own a watermark regardless — and it already does (#781).

It also closes a debt ADR-0002 itself recorded: "swap the raw-ID-token session
cookie for … session cookies before production". The raw IdP token was never
meant to be the session.

**The cost, stated plainly: Helivanta becomes a credential issuer.** Signing keys to
generate, store, rotate; a second token format; and a bug in that path is an
authentication bypass, not a defect. That cost is accepted here, and D5 is how
it is contained.

*Rejected: Zitadel actions injecting a tenant claim + org-scoped re-auth.* Keeps
one issuer and no Helivanta keys, but makes every hospital switch a redirect, and
depends on an action reading the requested org scope — **NOT VERIFIED** in the
spike.

*Rejected: RFC 8693 token exchange.* Aimed at impersonation — changing the
subject — not at re-issuing for the same subject in another tenant. Still needs
a tenant claim from somewhere, and Zitadel recommends restricting it to
confidential clients.

### D2 — The Helivanta session carries exactly what `Principal` needs

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

### D3 — Tenant switching re-mints the Helivanta session

`POST /v1/iam/me/tenant` keeps its route and its tight rate-limit budget, but
changes what it does: verify the caller's current session, check membership of
the target tenant in OpenFGA, mint a new Helivanta session with the new `tenant_id`
and the **same `auth_time`**, set the cookie.

No IdP round trip. No redirect. Consequently the `Tight` rate-limit entry for
this route is no longer protecting a project-wide external quota — it is
protecting an FGA call and a signature. **The budget should be revisited when
this lands** (#689 assumed the GIP exposure), and this spec does not silently
inherit its reasoning.

### D4 — Revocation stays Helivanta's, with an explicit upstream bound

The #781 watermark is unchanged and remains authoritative: sign-out and admin
revoke write a per-subject watermark, Helivanta sessions with an earlier `auth_time`
are refused, and the broadcast invalidates every replica's cache. The
`SubjectCredentialRevoked` event still publishes with **no tenant** — #835's
outbox policy depends on that and it must stay true.

What is genuinely new is **upstream deactivation**: a user disabled in Zitadel
holds a valid Helivanta session until it expires *(observed: their ID token still
verified; introspection is unavailable to us)*.

**Decision: Helivanta session TTL is the upper bound on upstream deactivation latency,
and that bound is stated rather than discovered.** Short TTL with silent renewal;
renewal re-checks the Zitadel session, so a deactivated user fails at the next
renewal rather than at the next login. The exact TTL is a number the plan must
choose and justify — it trades renewal traffic against how long a disabled
account keeps working.

*Rejected: introspect per request.* Not available to a public client
*(observed)*, and a network call per request against the IdP recreates exactly
the shared-resource exposure #689 exists to prevent.

#### D4a — renewal re-runs the exchange; Helivanta stores no IdP token

> **Amended 2026-08-20 (#916).** The mechanism below — the browser silently
> re-authenticating against Zitadel via a hidden `prompt=none` iframe — never
> worked: Zitadel's session cookie on `auth.tesserix.app` is `SameSite=Lax`
> and is never sent from a cross-site iframe in any browser, so the iframe
> could only ever answer `login_required`, and clinicians were evicted ~5
> minutes after signing in. Renewal is now `POST /v1/auth/renew`, a
> same-origin **server-side** call; the browser no longer contacts Zitadel
> for renewal at all, and the iframe and the silent-renew route are deleted.
>
> The premise this decision's mechanism hung on — "unless renewal is not a
> server-side operation at all" (below) — has since lapsed. It assumed
> Helivanta holds no IdP credential. That stopped being true: #854/#867
> introduced the login-client PAT (`ZITADEL_LOGIN_CLIENT_TOKEN`), an
> instance-level credential that can already create a session for any user.
> With that credential in hand, a server-side upstream check no longer
> requires storing a *user's* token — it was only ever the user-token
> constraint that forced renewal out to the browser.
>
> **What survives unchanged:** D4a's guarantee — the session TTL bounds
> upstream-deactivation latency — is preserved, now enforced server-side
> instead of browser-side. Both properties below still hold exactly as
> stated: Helivanta stores **no** IdP refresh token, and `offline_access` is
> still **never** requested.
>
> **Worth stating plainly:** the upstream-deactivation check this decision
> describes has never actually run in production — the cross-site round trip
> it depended on always failed before it could report anything. Server-side
> renewal, shipped by #916, is the first time this check will actually
> execute.
>
> See `docs/superpowers/specs/2026-08-20-server-side-session-renewal-design.md`
> and #916 for the root cause, the rejected alternatives, and the new
> mechanism. The reasoning below is kept as the historical record of why the
> browser-driven mechanism was chosen — it was a reasonable call given what
> was believed true at the time, and the gap it left is the story worth
> preserving, not erasing.

**Resolved 2026-08-15.** The paragraph above said "renewal re-checks the Zitadel
session" without saying *with what credential*, and that gap had a security
answer hiding in it. Helivanta verifies the Zitadel ID token at login and keeps
nothing; `userinfo` needs the **access** token. So an upstream re-check demands
Helivanta hold an IdP credential — unless renewal is not a server-side operation at
all.

**Renewal is the login exchange, run again with a fresh Zitadel token.** The
browser silently re-authenticates against Zitadel (`prompt=none` — its session
cookie on `auth.tesserix.app` is what makes this silent) and posts the fresh ID
token to the same exchange endpoint, naming the tenant it is currently in. Helivanta
verifies the token, re-checks membership in OpenFGA, and re-mints. There is no
separate renewal endpoint and no separate mechanism to get wrong.

> **This mechanism was never live in production — see the amendment above.**
> The `auth.tesserix.app` session cookie is `SameSite=Lax`, so it is never
> sent from the cross-site hidden iframe this paragraph describes; the
> "silent" re-authentication could only fail. #916 replaced it with
> server-side renewal (`POST /v1/auth/renew`).

The bound holds for the reason D4 claims: after the TTL, continuing requires a
**fresh** Zitadel token, and a deactivated user cannot obtain one — Zitadel
refuses the silent re-authentication. Deactivation therefore bites within one
TTL without Helivanta ever asking Zitadel a question directly.

> **The last sentence no longer describes the implementation — see the
> amendment above.** It was true only of the browser-driven mechanism this
> paragraph describes, which never ran. Under #916, Helivanta *does* ask
> Zitadel a question directly: every `POST /v1/auth/renew` calls
> `GET /v2/users/{id}` on the instance-level login-client PAT and refuses the
> renewal unless the subject is still active
> (`backend/internal/modules/iam/renew.go`). **The bound itself is unchanged**
> — deactivation still bites within one TTL — and the property this paragraph
> was really protecting is also unchanged: the direct question is asked with
> Helivanta's *own* instance credential, never with a stored *user* token, so
> "Helivanta stores no refresh token" below still holds exactly as written.

Two properties fall out of this that are worth having deliberately:

- **Helivanta stores no refresh token.** A refresh token is long-lived and, for a
  clinical system, roughly as dangerous as a password. Not storing one removes a
  whole class of secret-at-rest, encryption and rotation obligation — and there
  is nothing to steal from the Helivanta database that grants access to the IdP.
- **Membership is re-checked every renewal**, so a *revoked member* also loses
  access within one TTL, not only a deactivated account. That bounds OpenFGA
  staleness with the same mechanism, for free.

*Rejected: store the Zitadel refresh token server-side, encrypted.* A true
upstream check at any moment, but it makes Helivanta the custodian of credentials to
another system, needs a store, encryption and rotation, and #45 does not exist
yet. The security cost buys latency we do not need.

*Rejected: store the access token and call `userinfo` at renewal.* Access tokens
are short-lived, so by renewal time it has usually expired — and an expired token
is indistinguishable from a deactivated user, which is precisely the distinction
the check exists to make.

*Rejected: no upstream check at all.* Then the TTL bounds nothing and D4's
statement is simply false. A deactivated clinician would keep working
indefinitely so long as their browser kept renewing.

**Consequence for the frontend:** silent renewal is browser-side work — the
shell must run the OIDC silent-renew flow and fall back to a visible login when
it fails. Server-side, "renewal" needs no new code beyond the exchange endpoint
already built, which must therefore accept a tenant and re-check membership on
every call rather than only on first login.

> **Superseded — see the amendment above.** #916 deleted the shell's silent-renew
> flow and the hidden iframe entirely. Renewal is now driven by the server
> (`POST /v1/auth/renew`); the frontend's job is only to schedule the call and
> handle its response, not to run any OIDC flow itself.

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

**This makes #45 (secrets management) a hard dependency** — the first time Helivanta
has one. The plan must say what dev uses (a fixed local key, clearly marked and
refused outside dev, mirroring the existing `HELIVANTA_ENV` emulator guard) and what
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
- **Helivanta now issues credentials.** New signing-key operational surface, and a new
  class of bug whose failure mode is authentication bypass.
- **Key rotation is not designed here.**
- **`email`/`name` need a `userinfo` call** *(observed)*, so any UI showing them
  needs a fetch Helivanta does not do today.
- **We now operate an IdP** — ADR-0006's accepted cost. An IdP outage is a
  total sign-in outage, though existing Helivanta sessions survive until renewal,
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
