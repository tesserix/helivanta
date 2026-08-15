# HMS on the shared Zitadel instance — tenancy topology

**Issue:** [#838](https://github.com/tesserix/hms/issues/838).
**Status:** draft 2026-08-15.
**Decides:** how HMS is provisioned on `auth.tesserix.app`, and where hospitals live.
**Companion to:** `2026-08-15-zitadel-auth-design.md` (which decides the *protocol* —
HMS mints its own session). This decides the *topology*.
**Rests on:** `spikes/2026-08-15-zitadel-spike.md`, `spikes/2026-08-15-zitadel-topology.md`.
Claims marked *(observed)* were seen against a running Zitadel v4.15.3.

---

## Context

The platform team runs one Zitadel instance at `https://auth.tesserix.app`
(v4.15.3, GKE `tesseract-prod-in-gke`). It is initialised but has no product
topology: **HMS is the first product on it**, so whatever HMS does becomes the
convention kora, mark8ly and the rest inherit.

HMS is multi-tenant where **a tenant is a hospital**, a clinician may work at
more than one, membership is authoritative in OpenFGA, and `Principal.Subject`
is the Zitadel `sub`.

Zitadel's three relevant concepts, since the names mislead:

| | What it actually is |
|---|---|
| **Organization** | a container of *people*, and where login policy and **IdP federation attach** |
| **Project** | a container of *applications and their roles* |
| **Application** | an OIDC client — client id, redirect URIs |

The trap is reading "project" as a tenancy unit because it sounds like one. It
is not; tenancy in Zitadel lives at the **org** level. That single fact decides
most of what follows.

---

## Decisions

### D1 — HMS is one organization, one project, one application

```
Organization:  HMS              ← clinicians live here; an IdP would attach here
  Project:     HMS              ← one product
    App:       hms-web          ← the shell's OIDC client
    Machine:   hms-seed         ← management API: seeding for dev and CI
```

Its own org rather than a project inside a shared one: products then cannot
collide on users, policy or admin surface, the blast radius stays small while
the team is new to the tool, and HMS's clinical-grade requirements do not become
the floor every other product must meet.

**This is a convention HMS is setting, not one it inherited.** The platform team
owns the instance and should bless or replace it; nothing in this document
depends on the answer beyond the org's name.

*Rejected: a project per hospital.* Roles would have to be re-declared per
hospital and kept in sync with OpenFGA's copy — two sources of truth for one
fact. Onboarding a hospital would touch Zitadel. And decisively: **IdPs attach
to organizations, not projects**, so hospital-as-project cannot deliver
per-hospital SSO, which is the only thing that would justify putting hospitals
in Zitadel at all. It carries the cost and returns none of the benefit.

### D2 — Hospitals are not Zitadel entities

A hospital is an HMS tenant. Membership lives in OpenFGA. Zitadel does not know
that Apollo exists.

Adding a hospital is therefore one `INSERT` and some OpenFGA tuples — no org, no
project, no client, no grant, no second system in the onboarding path, and no
second registry of "which hospitals exist" to drift out of sync.

This is only safe because of the companion spec: HMS mints its own session, so
Zitadel's structure is nearly irrelevant at runtime. Zitadel answers *who
authenticated and when*; HMS decides *which hospital*, having checked OpenFGA.

*Rejected: one organization per hospital.* This is Zitadel's canonical B2B shape
and it does buy real things — per-hospital login policy, per-hospital admins,
per-hospital SSO. Two findings sank it for this domain:

- *(observed)* the `urn:zitadel:iam:org:id:{id}` scope — the mechanism that
  would scope a login to one hospital — is a **membership** restriction. A user
  granted into an org, rather than resident in it, is refused outright: *"User
  could not be found"*. **The mechanism that would express "log in at this
  hospital" actively rejects the clinician who works at two.**
- It puts Zitadel in the hospital-onboarding path permanently, to serve a
  capability no customer has yet asked for.

*(observed)* cross-org access via **user grants** does work cleanly — one login,
no redirect, stable `sub`. So org-per-hospital is not *impossible*; it is simply
more machinery than the domain needs today, and D5 keeps the door open.

### D3 — Zitadel authenticates, OpenFGA authorizes, and neither does the other's job

**No roles and no grants are configured in Zitadel for HMS.** The project exists
to own the OIDC client.

Zitadel's RBAC is flat: role names attached to a user, per project. HMS's
questions are relational — may this doctor see *this* patient, given the care
team; may this pharmacist dispense in *this* hospital but only read in that one.
Zitadel cannot express those; OpenFGA is built for them, and HMS already has its
permission matrix there with a role × permission test suite over it.

Configuring both would be the failure this design is most alert to: two systems
holding "who can do what", drifting, with whichever is consulted last winning.
The companion spec already forbids roles in the HMS session for a related
reason — a role in a token is a stale answer that survives until expiry.

*Rejected: Zitadel project grants as a product-entitlement gate* ("may this
person use HMS at all"). HMS already answers it — the login exchange refuses a
subject who belongs to no tenant. A second gate means two places to grant access
and one more way to be locked out for a reason nobody can find. If the platform
team later wants central entitlement across products, HMS must still not
*depend* on it.

### D4 — Identity is keyed on `sub` now; the audit trail must not be

HMS keys identity on the Zitadel `sub`, as built. No indirection is introduced
by #838.

That is a reversal of an earlier recommendation in this conversation, on
evidence: *(observed)* `sub` is stable across org contexts, and *(observed)*
SSO can be added to an org that already has password users without changing it
(D5). The migration that indirection was insuring against is much less likely
than argued.

**But `sub` must not become the permanent identity in the one place that cannot
be rewritten.** This is not hypothetical: HMS is migrating identity providers
*right now*, and GIP subjects and Zitadel subjects are unrelated values. Had HMS
shipped an audit trail keyed on GIP subjects a year ago, this week's work would
be silently corrupting it — resolving past accesses to the wrong person, or to
nobody.

**Constraint on [#54](https://github.com/tesserix/hms/issues/54) (audit trail,
unstarted):** it must attribute actions to an HMS-owned identifier, with `sub`
as an attached credential rather than the key. Introducing that when #54 is
designed costs an indirection; retrofitting it after audit data exists is not
possible, because audit records cannot be rewritten.

### D5 — SSO is a config change, deferred until asked

*(observed)* on v4.15.3: adding a login policy and IdP to an org that already
has password users, then linking an existing user to it, leaves `sub` and
`resourceOwner` unchanged. A config change, not a migration.

Because links are per user, multiple IdPs on the HMS org can serve different
hospitals' staff — a hospital demanding federation does not require its own
organization. **NOT VERIFIED:** a complete login *through* a linked external
IdP; only link-and-readback was exercised.

**Nothing is configured now.** No IdP, no federation, no per-hospital policy.
The capability is why Zitadel was chosen over GIP; building it before a customer
asks would be speculative.

### D5a — Login is hosted by Zitadel and branded, not rendered by HMS

HMS redirects to the Zitadel login app at `auth.tesserix.app`. The branding comes
from a **design-system login component** (to be added) consumed by the
`zitadel-login` build the platform team runs. **`apps/shell/app/login/page.tsx`
is deleted, not ported.**

Today HMS renders its own form and calls the identity provider's SDK directly,
so a clinician's password is typed into an HMS page. Two reasons that must not
carry over:

- **It would block the capability Zitadel was chosen for.** When a hospital
  federates to their Active Directory, login has to hand off to that provider. A
  self-hosted form would have to detect the user's IdP and redirect — plus
  handle MFA challenges, passkeys, password reset and lockout. That is
  reimplementing Zitadel's login app, on the surface where security defects are
  most expensive.
- **Credentials stop passing through HMS.** A compromised HMS frontend cannot
  harvest passwords it never receives. For a system holding patient records that
  is a real reduction in blast radius.

The cost is a redirect off-domain and back — the pattern users meet everywhere.

**Ship against the stock login UI first.** The branded component does not exist
yet, and waiting for it would block the frontend for a cosmetic reason. Zitadel's
stock login already works — MFA, password reset and lockout included — so HMS
points at it now and the branded build swaps in later. The redirect target does
not change; only its appearance does. Nothing is thrown away, which is what makes
this an interim *appearance* rather than an interim *solution*.

**Contract on the login component, and it is load-bearing for the e2e suite.**
`e2e/tests/support/login.ts` drives the form by accessible name — currently
`getByLabel("Email")`, `getByLabel("Password")`,
`getByRole("button", { name: "Sign in" })`.

Shipping against stock first has a consequence worth stating: **the suite will
first be written against Zitadel's own markup**, not ours. Both spikes drove that
UI successfully with Playwright, so working selectors already exist — the
frontend task should take them from there rather than guess.

The contract therefore runs the other way from how it was first written: **the
branded component must preserve the accessible names the stock UI uses**, so the
swap is invisible to the suite. If it renames them, every spec fails at once, at
the login step, which reads like a broken application rather than a renamed
label. If a rename is genuinely wanted, it is a planned, single, coordinated
change — not a surprise.

This is an accessibility property rather than a test convenience: the names a
screen reader announces are the ones Playwright queries, which is why targeting
them survives restyling.

### D6 — Hospital groups and owners: model the relationship, not the permission

Two real shapes are coming, and they are structurally identical but must not
behave identically:

- **A clinical group** — Apollo Pune, Apollo Nagpur. One operation, staff
  rotate, cross-branch clinical visibility is arguably a feature.
- **A business owner** — one company owning two clinically unrelated hospitals.
  They want occupancy, revenue and staffing across both. They have **no care
  relationship with any patient**.

Both are "a parent with children". What the parent may *see* is not the same,
and conflating them has a specific cost: if the parent relationship implies
widened read access, a business owner acquires row-level access to patient
records across both hospitals. Under the DPDP Act that is a purpose-limitation
breach; in accreditation terms it is an access-control finding.

The mechanism for a clinical group already exists in outline — the tenancy
predicate was deliberately extracted into `hms_tenant_visible` so that widening
reads is one function replacement rather than an `ALTER POLICY` per table across
thirty modules, and the RLS lint **fails** any table whose `WITH CHECK` calls
that function, so writes stay pinned to exactly one tenant however far reads
widen.

**Constraint on [#13](https://github.com/tesserix/hms/issues/13)/[#14](https://github.com/tesserix/hms/issues/14)
(tenant entity, unstarted):** model the parent relationship generically — a
tenant may have a parent — and attach permissions to the parent role
**explicitly**. Do not encode "parent implies read access to children" as a
property of the relationship. A holding company and a clinical group must be
expressible differently without a schema change.

An owner's cross-hospital view should come from aggregate reporting scoped to
the owned set, not from widening row access to clinical tables. An owner who
genuinely needs to work inside a hospital gets ordinary membership there, which
already works.

---

## What to provision

| Object | Name | Purpose |
|---|---|---|
| Organization | `HMS` | holds clinicians; where an IdP would attach |
| Project | `HMS` | owns the client; **no roles defined** |
| Application | `hms-web` | OIDC client for the shell — auth code + PKCE |
| Machine user | `hms-seed` | management API for dev/CI seeding, with an org role |

Per-environment: dev runs its own local Zitadel (v4.15.3, matching production).
Staging and production client registrations, redirect URIs and the `hms-seed`
credential are platform-team operations, not HMS ones.

---

## Limitations and what is not verified

- **NOT VERIFIED: production's existing org/project layout.** Deliberately not
  inspected — it would have meant reading a production IAM-admin credential.
  If a convention already exists there, D1 should defer to it.
- **NOT VERIFIED: a login through a linked external IdP.** D5 rests on link
  creation and readback, not a completed federated sign-in.
- **The branded login UI does not exist yet, and HMS does not wait for it.**
  Production serves `zitadel-login:v4.15.3-aurora.1`; the design-system login
  component behind it is still to be written. Per D5a, HMS ships against the
  **stock** login UI, so this is not a blocker — but the branded build's first
  run against the e2e suite is the moment D5a's accessible-name contract is
  either honoured or found broken. That is a scheduled risk, not an unknown.
- **HMS shares an instance.** An instance-wide Zitadel outage is a total
  sign-in outage for every product at once. Existing HMS sessions survive until
  renewal, which is a modest mitigation, not a plan.
- **Subject uniqueness across products** is inherited from the instance. If the
  platform team later adopts a shared org, a person using two products keeps one
  `sub` — fine for HMS, but it means HMS no longer solely controls who can
  attempt a login.

## Out of scope

- The authentication protocol itself — `2026-08-15-zitadel-auth-design.md`.
- Designing #13/#14 or #54; this records constraints on them only.
- Cross-branch **patient identity** (is a patient treated at two branches one
  record?). That is master-data management, not access control: a wrong merge is
  a patient-safety event and effectively unrecoverable, a duplicate is merely
  annoying. Group *visibility* can ship without committing to it.
- Per-hospital SSO implementation, until a customer asks.
