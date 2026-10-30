# Spike: Zitadel topology for Helivanta on the shared instance — orgs, identity, provisioning

- **Issue:** [#838](https://github.com/tesserix/helivanta/issues/838)
- **Supports:** [ADR-0006](../../adr/0006-zitadel-not-gip.md), the [Zitadel auth design](2026-08-15-zitadel-auth-design.md) (D1–D7)
- **Date:** 2026-08-15
- **Rule followed:** separate OBSERVED (a command and its real output, run this
  session) from READ (documentation, including Zitadel's own docs) from
  INFERRED (a conclusion this spike draws but did not directly test).
  Everything unverified says **NOT VERIFIED**, not silently.

**HARD CONSTRAINT observed:** no writes were made to the org's production
Zitadel (`https://auth.tesserix.app`, GKE `tesseract-prod-in-gke`, namespace
`zitadel`, v4.15.3). Production contact was limited to two unauthenticated
`GET`s of public OIDC metadata — `/.well-known/openid-configuration` and
`/oauth/v2/keys` — both read-only, both logged below. The `iam-admin-pat`
secret in that namespace was never read. Every `POST`, every login, every
org/project/user created below ran against a fresh **local** v4.15.3 stack,
`spike/zitadel-838/docker-compose.zitadel-v4.yml`, brought up for this session
(ports 20081/15434). `hms-dev` (postgres 15432, GIP 19099, nats/redis/openfga)
was confirmed untouched throughout by `docker ps`, matching prior spike
practice.

---

## Q1 — Can one Zitadel user act across multiple hospital orgs, keeping one `sub`?

**Answer: yes for the mechanism that matters (user grants), no for the
mechanism I originally suspected (`urn:zitadel:iam:org:id:{id}` scope) — and
the difference between those two matters enough to reverse the original
assumption stated in the brief.**

### Setup (OBSERVED, all against the local stack)

1. Bootstrapped the local v4.15.3 instance's first org (`helivanta-spike-v4`,
   `id=386374261784248323` — call it **org A**) by driving the real hosted
   login UI with Playwright for the bootstrap admin, capturing the access
   token from Console's own `sessionStorage` — the same method the existing
   spike doc used and justified (Console is itself a PKCE client of the same
   `/oauth/v2/authorize` endpoint Helivanta's login page will use).
2. Created a second org, **org B** (`hospital-b`, `id=386374532367122435`),
   via `POST /management/v1/orgs` using that bearer token.
3. Created a project in org B (`hospital-b-project`,
   `id=386374549295333379`), with a role `clinician` and
   `projectRoleAssertion: true`.
4. Created a **human user in org A only** (`clinician-crossorg@hospital-a.dev`,
   `userId=386374561777582083`) via the `POST /management/v1/users/human/_import`
   recipe the prior spike already proved (`isEmailVerified: true` +
   `passwordChangeRequired: false`, both load-bearing).
5. Granted that org-A user a **user grant** on org B's project — a
   cross-org grant, org A's user against org B's resource:
   ```
   POST /management/v1/users/386374561777582083/grants
   x-zitadel-orgid: 386374532367122435   (org B — the resource owner)
   {"projectId":"386374549295333379","roleKeys":["clinician"]}
   → 200 {"userGrantId":"386374571760025603", ...}
   ```
   Confirmed active by reading it back:
   ```
   POST /management/v1/users/grants/_search
   → {"roleKeys":["clinician"],"state":"USER_GRANT_STATE_ACTIVE",
      "orgId":"386374532367122435","orgName":"hospital-b",
      "grantedOrgId":"386374261784248323","grantedOrgName":"helivanta-spike-v4"}
   ```
6. Created an OIDC web app **directly inside org B's project**
   (`clientId=386374881618493443`) — the client the org-A user will
   authenticate against, to prove the grant, not org membership, is what lets
   the login through.

### Does `sub` stay the same across both org contexts?

**Yes — OBSERVED, identically, across every variant tried.** The org-A
user's `sub` (`386374561777582083`) was read back unchanged from: the
`_import` creation response, `GET /management/v1/users/{id}` after every
subsequent operation (grant, org-membership grant, IdP link — see Q2), and
every ID token decoded below, including the one issued for a client that
lives entirely inside org B's project:

```json
// ID token, org-A user, authenticating against org-B's client, after
// requesting scope "openid profile urn:zitadel:iam:org:project:roles"
{
  "iss": "http://localhost:20081",
  "sub": "386374561777582083",
  "aud": ["386374881618493443", "386374549295333379"],
  "exp": 1786836587,
  "iat": 1786793387,
  "auth_time": 1786793385,
  "amr": ["pwd"],
  "azp": "386374881618493443",
  "client_id": "386374881618493443",
  "at_hash": "y-UMEWsgj6H_n7y7Q1G9pA",
  "sid": "V1_386374898143985667"
}
```

**No redirect, no second authentication pass.** The single hosted-login flow
(login name → password, `prompt=login`) completed once and produced a valid
token for a client owned by an org the user has no membership in — only a
grant. `resourceOwner` on the user record stayed `386374261784248323` (org A)
throughout every step in this section.

### What does the token look like acting in org B — how would an app tell which org?

**There is no "current org" claim, exactly as the ADR already found — but
here is the concrete shape of what IS there, which the ADR did not yet
show.** The ID token above carries no org signal at all. The roles/org
information only appears in **`/oidc/v1/userinfo`**, not the ID token — which
matches the existing spike's separate finding that `profile`/`email` claims
need a userinfo round trip, now shown to be true of the roles claim too:

```json
// GET /oidc/v1/userinfo, same access token as above
{
  "sub": "386374561777582083",
  "name": "Cross Org",
  "preferred_username": "clinician-crossorg@hospital-a.dev",
  "urn:zitadel:iam:org:project:roles": {
    "clinician": { "386374532367122435": "hospital-b.localhost" }
  }
}
```

This is exactly the shape ADR-0006 described from documentation
(`role → {orgId: domain}`) — now independently reproduced, not just
asserted from docs. **An application cannot ask "which org is this token
for" — it can only ask "what does userinfo say this user's roles are,
aggregated across every org they hold a grant in" and then pick the entry
matching whatever org it already believes it's operating in.** That means
the app (Helivanta) must already know which hospital it's acting for by some
other means before this claim is useful — it cannot discover it from the
token. This directly confirms D1's premise: **Zitadel genuinely has nothing
to offer for "which tenant is this," and Helivanta's own OpenFGA-backed session is
not a workaround for a missing feature — it is the only workable design.**

### Does this require the `urn:zitadel:iam:org:id:{id}` scope — and what does requesting it do?

**This is the one place this spike overturns something the brief assumed.
The `org:id` scope is not a mechanism for "act in this org" — it is a login
RESTRICTION requiring org MEMBERSHIP, and requesting it for a user who only
holds a project grant fails the login outright, not just "requires
re-authorization."**

Adding `urn:zitadel:iam:org:id:386374532367122435` to the same request
(org-A user, org-B's client, same grant as above) changed the **login
screen's own copy** before any credential was even entered:

```
Welcome Back!
Enter your login details. The user must be a member of the hospital-b organization.
```

Submitting the org-A user's login name against this produced, on the same
screen, in real time:

```
User could not be found
```

**This is a hard failure, not a redirect or a step-up.** I then tested
whether granting the user an *org role* (`ORG_USER_MANAGER`, via
`POST /management/v1/orgs/me/members`, org-B context) — which is Zitadel's
closest thing to "add this user to the org" short of recreating them —
would satisfy the check. **It did not.** The same "User could not be found"
recurred. `GET /management/v1/users/{id}` continued to show
`resourceOwner: 386374261784248323` (org A) throughout. **Conclusion,
observed rather than inferred: the `org:id` scope checks the user's home
org (`resourceOwner`) — a property fixed at user creation — not any grant or
role. A project user-grant, however broad, does not satisfy it.**

**Practical consequence for Helivanta:** don't reach for `org:id` at all. It
answers a different question ("restrict login to this org's own users") than
the one Helivanta needs ("let this user act for a hospital they don't live in").
The user-grant mechanism above is the right primitive, and it composes
cleanly with zero redirects — which is good news, because D1 already decided
Helivanta never sends this scope during normal operation (D3's re-mint is
entirely local to Helivanta + OpenFGA). This finding matters for the topology
question below more than for D1–D7, which already avoided the trap by
accident.

---

## Q2 — Can SSO/IdP federation be added to an org after password users already exist?

**Answer: yes, without recreating anyone and without changing `sub` — both
OBSERVED.**

1. Org A already had the password user from Q1
   (`clinician-crossorg@hospital-a.dev`, `sub=386374561777582083`,
   `state=USER_STATE_ACTIVE`).
2. Created a custom login policy for org A (required first — a fresh org
   has none of its own and inherits the instance default, which rejected the
   IdP-activation call with `Login Policy not found (Org-Ffgw2)` until this
   step):
   ```
   POST /management/v1/policies/login   (x-zitadel-orgid: org A)
   → 200
   ```
3. Registered a generic OIDC IdP on org A:
   ```
   POST /management/v1/idps/generic_oidc   (x-zitadel-orgid: org A)
   {"name":"hospital-a-sso","issuer":"http://localhost:20081", ...}
   → 200 {"id":"386375133410885635"}
   ```
   (Pointed the dummy IdP config at this same local instance as a
   stand-in issuer — sufficient to prove the linking mechanics; a full live
   external-IdP login redirect through it was **not** attempted — see "not
   verified" below.)
4. Activated it on org A's login policy:
   ```
   POST /management/v1/policies/login/idps
   {"idpId":"386375133410885635","ownerType":"IDP_OWNER_TYPE_ORG"}
   → 200
   ```
5. Linked the **existing** password user to the new IdP — the exact
   operation the question is about:
   ```
   POST /v2/users/386374561777582083/links   (v1 path 404s; the v2
     UserService.AddIDPLink endpoint is the one that exists on v4.15.3 — a
     detail found by trial after the doc summary from a web search only
     gave the endpoint name, not confirmed path; the working path was
     found by testing, not copied)
   {"idpLink":{"idpId":"386375133410885635",
     "userId":"external-id-from-sso-provider-001",
     "userName":"clinician-crossorg@hospital-a.dev"}}
   → 200
   ```
6. Confirmed the link exists and reads back correctly:
   ```
   POST /management/v1/users/386374561777582083/idps/_search
   → {"idpId":"386375133410885635","idpName":"hospital-a-sso",
      "providedUserId":"external-id-from-sso-provider-001", ...}
   ```
7. Re-fetched the user record: `id` and `resourceOwner` **unchanged**
   (`386374561777582083` / `386374261784248323`), `state` still
   `USER_STATE_ACTIVE`.

**So: existing users can be linked to a newly added IdP via one Management
API call, no recreation, and `sub` does not change** — Zitadel's `sub` is
the internal user object's ID, fixed at creation; adding a credential
method to that same object is additive, not identity-replacing. This
directly answers the brief's framing: **adding hospital SSO later is a
config change (new login policy + IdP + a link call per user, scriptable),
not a migration.**

**NOT VERIFIED in this session:** an actual end-to-end login *through* the
linked IdP (i.e., completing the external authorization-code redirect and
confirming the resulting session correlates to the same `sub` at the token
level, not just via the Management API's own read-back). The self-referential
dummy IdP setup makes a live external round trip more work than the
question needed to answer with confidence — the linking mechanism and its
effect on `sub`/`resourceOwner` were verified directly against the account
record, which is the part that answers "must users be recreated," but a live
login through a real external IdP (e.g. an actual hospital's Entra ID/Okta)
should be spiked separately before any hospital-specific SSO ships.

---

## Q3 — Recommended topology for Helivanta

### Recommendation, stated first

**One Zitadel org for Helivanta as a product. Hospitals stay a pure Helivanta/OpenFGA
concept — never a Zitadel org, never a Zitadel project-per-hospital. Helivanta
keys identity on the Zitadel `sub` directly, with no internal-id
indirection layer.** Each numbered point below is the reasoning, not a
separate option to weigh evenly — this is a recommendation, not a survey.

### Does Helivanta get its own org, or a project in a shared org?

**Helivanta's own org**, distinct from whatever org kora, mark8ly, and
tesserix-home end up under. Reasoning:

- Org is the boundary this spike found real teeth on: login policy,
  IdP configuration, and org membership are all org-scoped (Q1, Q2). Helivanta
  operates in a regulated clinical context; sharing an org with other
  products means sharing that policy surface, and a login-policy change
  made for a different product's needs (e.g. mark8ly wanting a different
  password policy or MFA requirement) would apply to Helivanta's org too if they
  shared one.
- Machine users and PATs (the seeding mechanism both spikes proved) are
  org-scoped by the `ORG_OWNER`-role grant pattern used throughout — a
  shared org multiplies the blast radius of a leaked or over-scoped PAT
  across products.
- This is **INFERRED**, not observed: I did not test whether a project
  actually leaks any capability across a shared org boundary in practice
  (Zitadel's project-level isolation might be sufficient on its own). The
  recommendation leans on the org being the cheaper, clearer boundary to
  reason about for a clinical-data product, not on a demonstrated leak.

**What would change my mind:** if whoever administers `auth.tesserix.app`
already has a firm "one org per environment, one project per product"
convention in place for the other products — **NOT VERIFIED, this spike did
not and should not inspect production's existing org list** — Helivanta should
follow that existing convention rather than open a second pattern. Check
before provisioning.

### Do hospitals map to Zitadel orgs, or stay purely a Helivanta/OpenFGA concept?

**Purely Helivanta/OpenFGA.** This is the highest-value conclusion of this spike,
and it reverses the framing implicit in the brief's Q1 (which assumed
org-per-hospital was likely and asked whether it would fracture identity).

The case against org-per-hospital, each point grounded in what was
observed above:

1. **D1 already decided Zitadel answers "who," never "which hospital."**
   Org-per-hospital would only earn its cost if Helivanta needed Zitadel to
   answer a tenant question — and D1, D2, D3 already committed to OpenFGA
   and Helivanta's own session for that, before this spike ran. Org-per-hospital
   buys D1's design nothing.
2. **The one place org boundaries have real teeth — the `org:id` scope —
   is actively hostile to a clinician working at two hospitals.** Q1 showed
   it enforces *home-org membership*, not grants, and fails login outright
   ("User could not be found") for a user who only holds a project grant on
   the other org. A clinician's natural Helivanta shape — one identity, many
   hospitals via FGA-style grants — is exactly the shape that scope
   refuses to authenticate. The safe pattern (user grants, no `org:id`
   scope) works, but it means org-per-hospital's supposed multi-tenancy
   primitive is never actually exercised by Helivanta — it would be inert
   infrastructure kept only for a feature (org-scoped login restriction)
   that actively conflicts with Helivanta's clinician-usage pattern.
3. **Provisioning cost compounds per hospital for no return.** Q2 showed a
   fresh org has *no* login policy of its own and needs one explicitly
   created before IdP config is even possible. Org-per-hospital means every
   hospital onboarding (a workflow Helivanta already treats as a first-class,
   frequent operation — onboarding is explicitly named in the product's
   "Core Value") carries a Zitadel org-creation and login-policy-creation
   step, for a boundary Helivanta's own tenant_id/RLS pattern
   (`docs/standards/backend.md`) already provides at the application layer,
   the same way every other multi-tenant surface in this org (marketplace
   included) already does it.
4. **The one legitimate future reason for org-per-hospital — hospital-owned
   SSO branding/IdP — does not require deciding this now.** Q2 proved SSO
   can be added to an org *after* users and passwords already exist, with no
   recreation and no `sub` change. If a specific hospital later needs its
   own SAML/OIDC IdP with hospital-specific branding, **that hospital**
   can be migrated to its own org **at that time**, for **that hospital
   only** — a real, bounded, opt-in migration triggered by a real
   requirement, not a blanket policy paid for by every hospital that will
   never ask for it. This is the actual application of "scope down, never
   quality down": start correct-and-small (one org, FGA-scoped hospitals),
   decompose the *next* correct slice (per-hospital org + IdP) only when a
   hospital's SSO requirement makes it necessary.

**What would change my mind:** a near-term (not speculative) commitment
from the business that hospital IT departments will self-administer their
own clinician accounts directly in Zitadel Console — a real B2B
delegated-admin pattern. Nothing in the product context here suggests
that; Helivanta's own admin surface is the expected place hospital staff are
managed, matching the existing Helivanta backend pattern of application-owned
tenant data. If that assumption is wrong, org-per-hospital becomes
directly justified and this recommendation should be revisited before
building anything on top of it.

### Should Helivanta key identity on `sub` directly, or on its own internal id?

**On `sub` directly — same pattern as the GIP-era `Principal.Subject`, no
new indirection.**

- Q1 and Q2 both showed `sub`/`resourceOwner` staying fixed across every
  operation tried: cross-org grants, org-membership grants, and IdP
  linking. Nothing short of deleting and recreating the Zitadel user
  object changes it. An internal-id layer would only pay for itself if
  something routinely *did* change `sub` out from under a stable person —
  observed evidence says nothing routine does.
- **This is the moment to decide it, not defer it — precisely because of
  the "audit trail cannot be rewritten later" constraint the brief calls
  out.** #54 (the audit trail) is filed and open but **not yet built**
  (confirmed: `gh issue view 54` → state OPEN). That means there is
  currently zero audit data keyed on anything — the cost of choosing wrong
  today is zero, and the cost of choosing wrong after #54 ships is
  "cannot be rewritten." Get it right now, while it's free.
- Adding an internal-id indirection layer does not remove the
  irreversibility risk the brief is worried about — it relocates it. The
  scenario that would make `sub` unstable (migrating off this Zitadel
  instance entirely, or a masterkey/data-loss event) would equally corrupt
  an internal-id-to-`sub` mapping table, which would then need the exact
  same "who is this really" reconciliation the indirection was meant to
  avoid — except now there are two records to reconcile instead of one,
  and the mapping table itself becomes a second thing that must never be
  lost.
- **What would change my mind:** a concrete near-term plan for a clinician
  to authenticate via two different credentials into the *same* Helivanta
  account (e.g., password today, hospital SSO added later, and the
  business wants both to resolve to one person without a forced
  re-registration) — Q2 shows this is a **link**, not a new user, so `sub`
  stays singular even in that case; the indirection layer would only earn
  its cost for a genuinely different scenario: two separate Zitadel
  *user objects* that must be merged into one Helivanta identity post hoc (e.g.,
  after a botched dual-signup). That is a real but narrow failure mode, not
  the common case, and does not justify the indirection today.

### What Helivanta needs provisioned to start

Minimal, matching what D1–D7 actually require and nothing org-per-hospital
would have added:

- **One org** for Helivanta (name TBD against whatever convention the shared
  instance's other products settle on — **NOT VERIFIED**, don't assume).
- **One project** inside it. No project roles need defining — D2
  deliberately excludes roles from the Helivanta session, and D1/OpenFGA already
  own authorization, so Zitadel's project-role/grant machinery (exercised
  in Q1 only to *test* cross-org behavior) is not something Helivanta's real
  project needs to use.
- **One OIDC web app** (auth-code + PKCE, public client, `authMethodType:
  NONE`) for the browser-facing shell, redirect URIs to the real Helivanta shell
  callback. `devMode: true` only for local/dev instances, `false` in
  production (this spike's local app used `devMode: true` to allow the
  `localhost` HTTP redirect used for testing — production requires HTTPS
  redirect URIs and compliant OIDC config, which the existing spike doc's
  `noneCompliant`/`complianceProblems` response already demonstrates
  Zitadel checks for).
- **One machine user + PAT**, org-scoped, for Helivanta's own seeding/admin
  scripts (`scripts/seed-dev.mjs`'s replacement, per the existing spike
  doc's proven recipe) — least-privilege within Helivanta's own org, not an
  instance-level role.
- **No IdP configuration at launch.** Q2 shows this is safe to defer; add
  it only when a specific integration is asked for.
- **No custom login policy required at launch either**, unless Helivanta wants a
  branding/behavior difference from the instance default — Q2 showed a
  fresh org has none by default and inherits the instance's; only IdP
  activation forced creating one in this spike's test path.

---

## What is NOT VERIFIED

- Whether the org's actual production convention already has an
  established org/project pattern for other products (kora, mark8ly,
  tesserix-home) that Helivanta should match instead of introducing its own
  — deliberately not inspected, per the "no production reads beyond public
  metadata" constraint.
- A live login completed *through* a linked external IdP (Q2) — only the
  Management-API-level link-and-readback was verified; a real SAML/OIDC
  hospital IdP round trip needs its own spike before shipping hospital SSO.
- Whether Zitadel's project-level isolation inside a shared org is actually
  weaker than a dedicated org in some concrete way (used only as supporting
  reasoning for "Helivanta gets its own org," not as an observed leak).
- Whether an org can later be renamed/merged if the "Helivanta gets one org"
  decision needs revisiting — not tested, and not needed for this
  recommendation since the recommendation is to *avoid* creating
  per-hospital orgs in the first place.
- The exact production org/project/client IDs and whatever naming
  convention should apply — this spike created disposable local IDs only
  and made no claim about what production should be named.

## Contradictions found against the existing design docs

**None, on the substance.** ADR-0006 and the design spec's D1–D7 already
avoided the `org:id`-scope trap (D1 explicitly rejects the "Zitadel actions
injecting a tenant claim + org-scoped re-auth" approach, flagging the
scope's action-reading behavior as unverified — this spike now independently
shows that scope is worse than merely unverified for Helivanta's use case: it
actively refuses login for a grant-only cross-org user, which is the
clinician-at-two-hospitals case D2 names directly). Nothing here overturns
D1–D7; this spike answers the topology question those decisions deliberately
left open (D1: "Tenant switching must be redesigned... not decided here").

One correction to the **brief itself**, not the design docs: the brief's Q1
was phrased assuming org-per-hospital was the working hypothesis and asked
only whether it would fracture identity. It would not fracture identity
(`sub` is stable) — but it would fight the `org:id` scope for the exact
clinician-at-two-hospitals case the whole design exists to support, which is
a stronger objection to org-per-hospital than "does it fracture identity,"
and is the reason this spike recommends against org-per-hospital outright
rather than concluding "safe, proceed."

---

## Local artifacts

- Stack: `spike/zitadel-838/docker-compose.zitadel-v4.yml` (already existed,
  reused unmodified this session).
- No new files were left in the repo — the throwaway Playwright driver
  scripts used to complete real hosted-UI logins for this session
  (bootstrap admin login, PKCE authorization-code flows with/without the
  `org:id` scope, the IdP-link verification) were written under `e2e/` to
  reuse its installed Playwright, and deleted after use, matching the prior
  spike's convention of not committing throwaway verification code
  (`/tmp/zitadel-verify-spike`, "not part of the repo; not committed").
- Local stack containers (`helivanta-spike-zitadel-v4-*`) were left running for
  this session; `hms-dev` was confirmed untouched throughout via `docker ps`.
