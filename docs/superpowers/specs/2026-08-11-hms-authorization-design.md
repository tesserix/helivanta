# Helivanta Authorization — OpenFGA Decision Layer

Date: 2026-08-11
Status: approved
Builds on: phase 1 repo setup (PR #754), phase 2 zones (PR #755), frontend standards (PR #757), backend standards (PR #758)
Resolves: issue #1 (OpenFGA as the single authorization decision point), repo-setup spec D4

## Goal

Every Helivanta route is currently reachable by any authenticated user in the
tenant. `Principal` is `{Subject, TenantID}` — no roles, no permissions,
and not one line of Go references OpenFGA even though it has run in
`docker-compose.dev.yml` since phase 1. A pharmacist can open IPD visits;
a lab technician can dispense medication.

This phase makes OpenFGA the single authorization decision point for the
backend, gates zone navigation and actions in the frontend, and adds an
`iam` module that owns tenant membership and roles as a rebuildable
system of record.

## Decisions

- **D1 — Role → action, not per-record.** Permissions are tenant-wide:
  "user X may perform `pharmacy.dispense.fulfil` in tenant T". No
  per-record relationship checks. The current domain has no patient
  entity to anchor them to, so per-record ReBAC would be modelling
  against nothing. See D9 for how the design stays open to it.
- **D2 — Resolve once per request.** `authz.Middleware` runs immediately
  after `authn.Middleware` and performs exactly one FGA call
  (`ListObjects(user, can_do, perm)`) to resolve the caller's whole
  permission set for the token's tenant. `authz.Require(perm)` is then a
  pure in-memory set lookup. One FGA round trip per request regardless of
  how many permissions a route declares, and the same resolved set is the
  response body for `GET /iam/me/permissions` at zero extra cost.
  Revocation is not instantaneous: it lands only once the outbox event
  carrying the revoke has been processed by the `iam-fga-sync` consumer
  and the FGA role tuple is deleted, so there **is** a staleness window
  bounded by outbox/consumer lag (see "Grant visibility window" below).
  There is no additional decision cache on top of that — `Resolve`
  queries FGA fresh on every request — so that outbox lag is the whole
  window, not zero. If a `member_revoked` event is permanently lost
  (never delivered, or its consumer permanently fails), that lag has a
  backstop: `platform.Reconcile` deletes any `role:`/`perm:` tuple
  Postgres does not back, closing the gap the next time it runs. It
  currently runs only at process boot (`cmd/api/main.go`), not on a
  timer, so the backstop's bound is "at the next boot of this replica,"
  not a bounded wall-clock time for a long-lived process — see the
  reconciler section of `docs/standards/backend.md` for the full
  `Reconcile`/`ReconcileTenant` split. A short-TTL cache may later be
  added *inside* `pkg/authz` as a pure optimisation without touching any
  call site.
- **D3 — Fail closed, always.** Any FGA error, timeout or unreachable
  store denies the request with `503 authz_unavailable`. There is no code
  path on which a handler runs without a resolved permission set. Failing
  open is never an acceptable degradation for this system.
- **D4 — Roles and permissions are data, not model.** The FGA
  authorization model contains no role names and no permission names (see
  "Authorization model"). Adding a zone, a permission or a role is a tuple
  write. The model file changes only if the *shape* of authorization
  changes.
- **D5 — Routes declare permissions at compile time.** `Module.Routes`
  receives a `*platform.Router` instead of a raw `*gin.RouterGroup`; every
  verb method takes an `authz.Permission` as a required argument.
  `authz.Public` is the explicit, greppable opt-out. An undeclared route
  is not expressible, which is stronger than the boot-time check the
  repo-setup spec (D4) originally proposed.
- **D6 — Postgres is the system of record; FGA is the decision point.**
  The `iam` module writes membership and role rows plus an outbox event in
  one transaction; a consumer applies tuples to FGA idempotently. FGA is
  fully rebuildable from Postgres. Grants are therefore eventually
  consistent — see "Error handling" for the window and its contract.
- **D7 — Membership is proved by resolution.** A user with no membership
  in a tenant resolves to an empty permission set, so every guarded route
  denies. There is no separate membership check to forget, and cross-tenant
  access remains blocked by forced RLS independently of authorization.
  This inversion is real, not just conceptual, everywhere membership is
  read on a request path: `GET /iam/me/tenants` and the `POST
  /iam/me/tenant` switch gate (`backend/internal/modules/iam/me.go`) both
  resolve membership from OpenFGA role tuples via `deps.Roles.ListRoles`,
  not by querying `iam_members` directly. `iam_members` is forced-RLS and
  every runtime accessor (`WithTenant`) scopes to a single tenant's GUC,
  so there is no accessor that can answer "which tenants does this
  subject belong to" without already knowing the tenant — exactly the
  question these two routes exist to answer. `iam_members` stays the
  write-side system of record (D6): the grant/revoke endpoints write it
  directly, and `platform.Reconcile` reads it at boot to keep OpenFGA's
  role tuples converged with it.
- **D8 — Tenant switching is in scope.** `tenant_id` is a claim inside
  the verified GIP token, so a clinician working at two hospitals cannot
  currently switch without a separate login. `iam_members` already holds
  every membership, so this phase adds listing, a membership-gated switch
  that re-mints the session, and a shell picker.
- **D9 — Department scoping is a named non-goal, not yet designed for.**
  `pkg/authz.Resolve(ctx, subject, tenantID)` takes no scope parameter
  today, and the FGA model (`user`/`role`/`perm` types only — see
  "Authorization model") reserves no relation or object-ID segment for a
  department or other sub-tenant scope. Because permissions are FGA
  *objects*, not model relations (D4), adding scope later is a bounded
  migration rather than a reshaping of every call site — but it is not
  free, and it touches four concrete pieces: the model (a new type or
  relation to carry scope), the reconciler (`platform.Reconcile` /
  `platform.ReconcileTenant` in `backend/internal/platform/reconcile.go`,
  which would need to write and prune scope-qualified tuples), a rewrite
  of every existing `role:<tenantID>/...` and `perm:<tenantID>/...`
  tuple into the new scoped object-ID shape (today's tuples are not
  scope-shaped and do not migrate themselves), and `authz.Resolve`'s
  signature. `authz.Require(perm)` and every route's permission
  declaration stay untouched, since they only ever see the resolved
  in-memory permission set and never construct an object ID themselves.

## Authorization model

Permissions are FGA *objects*, not relations. This is what makes D4 hold:

```
type user

type role
  relations
    define assignee: [user]

type perm
  relations
    define granted_role: [role]
    define can_do: assignee from granted_role
```

Tenant isolation lives in the object-ID namespace:

```
role:<tenantID>/pharmacist
perm:<tenantID>/pharmacy.dispense.fulfil

role:<tenantID>/pharmacist  assignee     user:<subject>
perm:<tenantID>/pharmacy.dispense.fulfil  granted_role  role:<tenantID>/pharmacist
```

Resolution is one `ListObjects(user:<subject>, can_do, perm)` filtered to
the caller's tenant prefix.

### Permission vocabulary

Named `<module>.<resource>.<action>`. Each module **declares its own** via
a new `Permissions() []authz.Permission` method on `platform.Module`, so a
new zone carries its permissions with it. The architecture test fails if a
module uses a permission it did not declare.

### System roles

Five, matching the zones that exist today:

| Role           | Permissions                                                                 |
| -------------- | --------------------------------------------------------------------------- |
| `tenant_admin` | all declared permissions, plus `iam.member.manage`                          |
| `doctor`       | `medicore.visit.create`, `medicore.visit.read`, `lab.order.read`, `pharmacy.dispense.read` |
| `nurse`        | `medicore.visit.read`, `medicore.visit.update`                              |
| `pharmacist`   | `pharmacy.dispense.read`, `pharmacy.dispense.fulfil`, `pharmacy.medication.read` |
| `lab_tech`     | `lab.order.read`, `lab.order.fulfil`                                        |

These ship as seeded system roles (`is_system = true`, not editable). Because
roles are data, a tenant-defined custom role needs no model change or
redeploy — that capability is enabled by this design but no UI for it ships
in this phase.

### Reconciler

At boot, for every tenant, a reconciler ensures the `perm` objects and
system-role grants match the module registry. Deploying a new zone
therefore grants its permissions to every existing tenant's `tenant_admin`
automatically — no migration, no manual step. The reconciler is idempotent
and safe to run on every start.

## Components

### `pkg/authz` (new)

- `Permission` — typed string constants, declared per module.
- `Client` — OpenFGA SDK wrapper. `Resolve(ctx, subject, tenantID)
  (PermissionSet, error)`; any error returns an error, never a partial set.
- `Middleware(client)` — resolves once, parks the set on the Gin context,
  denies with 503 on any failure.
- `Require(perm)` — in-memory lookup; denies with 403.
- `PermissionsFrom(c)` — reads the set back out.
- `Public` — sentinel permission for unguarded routes.

### `platform.Router` (new)

Wraps `*gin.RouterGroup`; every verb takes a permission:

```go
r.POST("/visits", authz.VisitCreate, handler)
r.GET("/medications", authz.Public, handler)
```

`Module.Routes(r *platform.Router, deps Deps)` replaces the
`*gin.RouterGroup` signature across the four existing modules
(mechanical). An architecture test fails if any module package imports
`gin.RouterGroup` directly.

### `internal/modules/iam` (new module)

- `iam_roles` (`tenant_id`, `key`, `label`, `permissions`, `is_system`) and
  `iam_members` (`tenant_id`, `subject`, `role_key`) — both with the forced
  RLS boilerplate. Migration `0001_iam`.
- Routes: grant, revoke, list members, list roles — all guarded by
  `iam.member.manage`; plus `GET /iam/me/permissions` and
  `GET /iam/me/tenants`, which any authenticated caller may reach.
- Events: `helivanta.in.iam.member_granted.v1`, `helivanta.in.iam.member_revoked.v1`,
  `helivanta.in.iam.role_changed.v1`. Consumer `iam-fga-sync` applies tuples
  idempotently.

### `internal/platform/respond`

Adds `Forbidden` alongside the existing `Unauthenticated`.

### Frontend

- `packages/api`: `usePermissions()` hook over `GET /iam/me/permissions`
  (via `useApiQuery`), and a `<Can permission="...">` guard component.
- `packages/ui/zones.ts`: each `Zone` and `ZonePage` gains a required
  permission; the sidebar renders only what the caller can enter.
- Panel actions hide or disable via `<Can>`. **The UI is a convenience
  layer only — the API remains the enforcement point.**
- Shell gains a tenant picker backed by `GET /iam/me/tenants`.

### Bootstrapping

The first `tenant_admin` cannot be granted through a route guarded by
`iam.member.manage`. `make seed` writes that first membership directly
(`WithSystem` plus a direct tuple write). This is the one sanctioned
bypass and it exists only in the seed path.

## Data flow

**Request.** authn verifies the GIP token → `authz.Middleware` resolves the
permission set for the token's tenant in one FGA call → `Require(perm)`
checks in memory → handler runs. Non-member ⇒ empty set ⇒ every guarded
route denies.

**Grant / revoke.** `iam` writes the row and an outbox event in one
transaction → `iam-fga-sync` applies tuples to FGA → subsequent requests
resolve the new set.

**Tenant switch.** `GET /iam/me/tenants` lists memberships → switch
endpoint verifies a membership row exists → re-mints the session with the
target tenant claim → shell reloads.

## Error handling

| Condition                          | Response                                    |
| ---------------------------------- | ------------------------------------------- |
| FGA unreachable, erroring, timeout | `503 authz_unavailable`, deny, abort, logged with request ID |
| Member lacking the permission      | `403 forbidden`                             |
| Non-member of the tenant           | empty set ⇒ `403 forbidden` on any guarded route |
| Cross-tenant record access         | `404` (RLS returns no row — unchanged)      |
| Consumer tuple-write failure       | retried via the existing bus retry path; tuple writes are idempotent |
| Model drift at boot                | startup fails, as with `LintRLS`            |

The `403` / `404` split is deliberate and matches the existing backend
standard: `404` means the record does not exist *for you* (tenant
isolation); `403` means you are in the right tenant but lack the
permission.

**Grant visibility window.** Tuples land through the outbox, so a grant is
applied in milliseconds but not synchronously. Grant and revoke endpoints
return `202` per the repo's async-create convention. Revocation is subject
to the same window; because there is no decision cache (D2), no additional
staleness is layered on top.

## Testing

- **Adversarial matrix suite (the gate).** Against a real OpenFGA
  testcontainer: two tenants × five roles × every guarded route, asserting
  allow/deny for each cell, plus cross-tenant denial throughout. The matrix
  is generated from the module registry, so a new module's routes are
  covered automatically — omitting a test is not possible.
- **`pkg/authz` unit tests.** Set semantics, `Require` denial, and
  fail-closed behaviour on every error class using a fake client.
- **`iam` module tests.** Grant lands as a tuple through the outbox;
  revoke removes it; consumer idempotency; membership-gating on the switch
  endpoint; bootstrap seeding.
- **Architecture tests.** No module imports `gin.RouterGroup`; every
  permission used is declared by its module; `iam` registered in
  `allModules()`.
- **Frontend.** `usePermissions`, `<Can>`, and zone filtering via
  `renderWithProviders`; existing panel tests updated for gated actions.
- **E2E.** `e2e/tests/smoke.spec.ts` stays green, plus a role-gated nav
  assertion.
- 70% per-package coverage floor as per backend standards D7.

## Out of scope

- Per-record / ReBAC authorization and department scoping (D1, D9).
- Audit logging. It is the next cross-cutting gap and a prerequisite for
  the consent milestone below, but it is its own phase.
- Custom-role management UI (the model supports custom roles; no screen
  ships).
- Cross-tenant platform staff ("Tesserix support can view tenant X"). A
  genuinely different problem; naming it here rather than half-building it.
- Patient identity, consent, and cross-hospital record exchange — see the
  appendix.

## Appendix — patient records & consent (next milestone, not specced)

Raised during this design session and deliberately deferred. Recorded so
the direction is not lost; each item still needs its own spec.

The scenario: a patient moves to another hospital, or sees a different
doctor, and records must follow with the patient's consent.

**Prerequisite that does not exist yet:** there is no patient entity.
`medicore_visits.patient_name` is free text. Consent to access "a patient's
records" is undefined until a patient exists with a stable identifier — and
for cross-hospital, one stable *across* hospitals.

**Two distinct problems**, often conflated:

- *Different doctor, same hospital* — intra-tenant: care-team
  relationships and break-glass emergency access with mandatory reason and
  audit. This is the per-record ReBAC deferred in D1/D9.
- *Different hospital* — cross-tenant: consent artefacts plus federated
  exchange. Forced RLS makes cross-tenant reads impossible by design, which
  is the strongest safety property the platform has. Consent must be a
  narrow, audited, explicitly-scoped hole — never a relaxation of RLS.

**Decisions taken during this session:**

- **Exchange model — federated pull, never copy.** Hospital B requests
  records from A at read time; A validates the consent artefact, serves
  only the consented scope, and logs the disclosure. PHI never leaves its
  owning tenant, RLS is untouched, and revocation is instant and real.
- **Patient identity — a Tesserix-minted identifier, with government IDs
  mapped onto it.** *(Supersedes an earlier decision in this session that
  made a national health ID — ABHA — the cross-hospital link.)* Every
  patient record carries **two** Tesserix identifiers plus 0..n external
  ones:
  - **Internal key** — `patients.id` UUID. Never displayed, never changes.
    **Every clinical row (visits, orders, dispenses, results) foreign-keys
    to this**, which is what makes reissue survivable.
  - **Human-facing identifier** — Medicare-shaped: a 9-digit card number
    plus a check digit, plus an IRN of 1–9 identifying the person on that
    card. Displayed `2123 4567 0 · 2`. Nine members per card maximum; a
    larger household gets a second card, as in Australia. The composite
    (card + IRN) identifies an individual, not a family.
  - **External identifiers** — 0..n rows of `{scheme, value, irn, verified,
    valid_from, valid_to}`: `in.abha`, `au.ihi`, `au.medicare.card`,
    `uk.nhs`. The Tesserix identifier lives in this **same** table as
    `tsx.card` — one mechanism, not two. Country profile decides which
    schemes apply and which are authoritative.

  On a family split or a child ageing off a card, the person is issued a
  new card number and IRN: the old identifier row gets `valid_to` set and a
  new row is inserted against the same `patients.id`. **Zero clinical
  records move**, and the old number still resolves — which matters when a
  referral letter quotes it years later.

  Demographic matching was rejected as a primary mechanism: a false match
  merges two people's medical records.

  **The cost this accepts.** A cross-tenant patient index sits outside the
  forced-RLS model that protects everything else, makes Tesserix a
  controller of identity data spanning hospitals, and produces a table more
  disclosive than the records it points at — knowing one person attended an
  oncology hospital, a psychiatric clinic and a fertility clinic is
  revealing before a single note is read. The alternative considered and
  rejected was consent-scoped linking, where the consent artefact carries
  the A↔B linkage and no standing cross-hospital identity graph ever
  exists. That trade-off was weighed and the index chosen deliberately;
  it is recorded here so the reasoning survives the decision.

- **Why a national ID cannot be the anchor.** ABHA creation is voluntary.
  The raw created-count is large but inflated by bulk generation during
  PM-JAY, CoWIN and hospital registration drives — "has an ABHA" is far
  more common than "knows they have one", and "has records linked to it" is
  smaller again. Awareness skews hard by age, literacy and urban/rural.
  Therefore: ABHA presence cannot be assumed at the front desk; ABHA
  creation must be a first-class registration flow (Aadhaar or mobile OTP,
  with consent) that handles refusal gracefully; and **within-hospital care
  must work fully with no national ID at all** — local MRN carries
  everything, national IDs gate only the cross-hospital path. *(The
  adoption characterisation above is unverified against current ABDM data
  and must be checked before it hardens into an implementation spec.)*

- **Individual vs family-scoped identifiers.** No *externally* family-scoped
  identifier may establish clinical identity on its own. An Australian
  Medicare card number is an enrolment and benefits artefact — searchable
  for billing and reception, never sufficient alone, because cards are
  reissued, split on separation, and members age off them. The Tesserix
  identifier borrows Medicare's *shape* without inheriting that lifecycle:
  the composite is individual-resolving (card **+ IRN**, never card alone),
  and the actual clinical key is the internal UUID beneath it, so a reissue
  is one insert and one update rather than a record migration. This is the
  same separation Australia itself makes between the Medicare card and the
  IHI.
- **Households confer no access.** A household groups patients for
  billing, contact details and reception search, and grants **zero** record
  access. Two adults on the same Medicare card have no clinical right to
  each other's history. Household-as-access-unit was rejected: access would
  follow membership, so anyone added later would inherit history they were
  never consented to see.
- **"Family sharing" decomposes into three per-person relationships**,
  each individually scoped, evidenced and expiring:
  - `guardian_of` — parent/guardian to a minor; auto-expires at
    country-configured ages.
  - `delegate_of` — an adult explicitly nominating another (an aging
    parent nominating a child, a patient nominating a spouse); consent with
    a long horizon and a named delegate; revocable.
  - `legal_authority` — power of attorney, guardianship order, next of kin
    in emergencies; documented and audited.
- **Lifecycle rules are country-profiled, and consent carries a data
  category scope.** Guardian access auto-expires at configured ages, and
  sensitive categories (sexual health, mental health, substance use) are
  excluded from guardian and delegate views by default from a lower age
  threshold. Adolescent confidentiality is a legal requirement in the
  target markets, and it forces consent artefacts to carry a
  **data-category scope** in addition to a validity window. This is the
  part that is genuinely hard to retrofit, so it is committed to now.
- **Consent enforcement — the consent module is truth, FGA mirrors it.**
  The artefact (requester, patient, data-category scope, purpose, validity
  window, revocation, audit) is the system of record; active consents are
  mirrored into FGA tuples so the normal authz path works unchanged, and
  the serving side re-validates the artefact at read time. FGA tuples have
  no expiry, so consent cannot live as tuples alone — and every
  relationship above expires.

## Appendix — provider identity (next milestone, not specced)

Same problem as patient identity with different vocabulary, and with a hard
dependency line back to this authorization phase.

**Australia separates three concerns Helivanta currently collapses into one**, and
the separation is the lesson worth taking:

| Identifier | Purpose | Lifecycle |
| ---------- | ------- | --------- |
| AHPRA registration (`MED0001234567`) | Right to practise | Life, but can be suspended / conditional / lapsed |
| HPI-I (16 digits) | Permanent clinical identity | Never changes |
| Medicare provider number (`123456AB`) | Billing, **per practice location** | One per (practitioner, location) |

- **Every provider gets a universal Tesserix identifier**, on the same
  internal-UUID-plus-human-facing-number pattern as patients. Issuing one
  only to unregistered roles was considered and rejected: a number that
  appears exclusively on the professions lacking a regulator reads as a
  *stand-in for the registration they lack*, which is precisely the
  "looks like a licence" risk the restriction was meant to avoid. Universal
  issuance makes it obviously an account number. It is also the rule
  already applied to patients.
- **Identifier and qualification evidence are different things.** The
  identifier is universal (who you are here). The *evidence* varies: a
  verified regulator registration where one exists, hospital-verified
  evidence where none does.

  | Role | Australia | India |
  | ---- | --------- | ----- |
  | doctor | AHPRA `MED…` | NMC + State Medical Council |
  | nurse | AHPRA `NMW…` | INC + State Nursing Council |
  | pharmacist | AHPRA `PHA…` | PCI + State Pharmacy Council |
  | lab_tech | **none — not AHPRA-registered** | NCAHP (2021 Act, rollout incomplete) |
  | tenant_admin | n/a — administrative | n/a |

- **Pharmacist is the strongest gating case, stronger than doctor.**
  Dispensing scheduled drugs legally requires a registered pharmacist in
  both countries, so `pharmacy.dispense.fulfil` maps directly onto a
  registration number.
- **Lab tech is the exception that constrains the schema.** Medical
  laboratory scientists are not AHPRA-registered in Australia at all, and
  India's NCAHP registration is new with incomplete rollout. **The model
  must tolerate a clinical role with no registration authority, and one
  whose authority exists on paper but not yet in practice.** A blanket
  "clinical role requires registration" rule would make lab work impossible
  in Australia. Enforcement, when it arrives, must therefore be **per
  (role, country) policy, not a global rule.**
- **Decision: registry now, gating later.** Build the registry and put the
  prescriber identifier on prescriptions and orders. Record registration
  `status` as data. Do **not** yet make current registration a precondition
  for a clinical role. **The gap this consciously leaves open:**
  `Principal.Subject` is a GIP UID with no professional identity behind it,
  and a role grant in `iam_members` never expires and is never checked
  against an external registry — so a suspended practitioner keeps their
  clinical role until a human revokes it by hand.
- **Registration is not binary.** Provisional, conditional and limited
  registration exist, and scope of practice varies within a profession — a
  nurse practitioner has prescribing rights a registered nurse does not.
  Registration *class* may eventually influence which permissions a role
  holds; because roles and permissions are already data rather than model
  relations (D4), that needs no FGA model change.
- **The privileging layer already exists.** Hospital governance separates
  registration (external, portable), credentialing (a hospital verifies a
  qualification), and privileging (this hospital grants specific rights
  here). Even a fully registered surgeon needs privileges granted at each
  hospital. **`iam_members` plus FGA role grants *is* the privileging
  layer**, already shipped in this phase. Only the two layers beneath it —
  the provider entity, and evidence attached to the grant — remain.
- **Two constraints on hospital-issued credentials:** never mint anything
  that reads as a licence (distinct field, distinct label, never rendered
  where a registration number appears); and keep credentials **per-tenant
  by default** — another tenant's credential is *evidence* that speeds
  Hospital B's own check, never automatic authority, or Tesserix becomes a
  de facto credentialing body.

*(The regulatory specifics above are unverified against current AHPRA,
NCAHP and PCI sources and must be checked before implementation.)*

## Appendix — public provider directory (next milestone, not specced)

A patient may look up a provider by number and see qualifications and
registration status **without consent**; a provider reading patient data
**always** requires consent. The asymmetry is principled — a provider acts
in a public professional capacity, a patient is a private individual — and
it is what AHPRA, the Indian Medical Register and the GMC already codify.

- **Publishability is per identifier scheme, not global.** AHPRA / NMC /
  PCI registration: public by design. Tesserix provider number: public — it
  is the lookup key and already appears on prescriptions. **Medicare
  provider number: not public**, publishing it enables fraudulent claiming.
  HPI-I: not public. `provider_identifiers` therefore needs a visibility
  attribute per scheme.
- **Public means the professional record, not the person.** Registration
  status, profession, specialty, qualifications, conditions, sanctions —
  yes. Home address, personal contact, DOB — no. **Do not derive "which
  hospitals they work at" from `iam_members`**; that leaks tenant data and
  is commercially sensitive. Only what a hospital or provider explicitly
  publishes.
- **Sanctions and conditions are the highest-risk field.** Showing one that
  does not exist is defamatory; omitting one that does is a safety failure.
  Mirrored regulator data must carry provenance and freshness ("as recorded
  by AHPRA on <date>") and preferably link to the regulator rather than
  assert on Tesserix's authority. Verification state and date must be
  *visible*, not merely stored — displaying an unverified registration
  number implicitly vouches for it.
- **This is the platform's first unauthenticated, cross-tenant read
  surface.** Everything built in this phase is tenant-scoped behind forced
  RLS and a fail-closed authz middleware; the directory sits outside all of
  it. It must read from a **curated published projection**, never the
  operational tables, and needs rate limiting plus enumeration protection —
  sequential provider numbers would otherwise let anyone scrape it.
- **Hospital-verified credentials in public view** must render as
  "credentialed by <hospital>", never in the position a registration number
  occupies.

## Open questions for the identity spec

1. **Who allocates Tesserix card and provider numbers** — a platform-level
   issuer service, with its own availability and audit requirements, or
   per-tenant ranges. This is the concrete operational cost of the chosen
   global index.
2. **Check-digit algorithm** — Medicare's own, Luhn, or mod-11. Pin it;
   changing it later invalidates every issued number. Keep the patient and
   provider formats visibly distinct, so a number read aloud in a hospital
   is unambiguous about whether it identifies a patient or a clinician.
3. **What evidence is sufficient to link two hospitals' patients** as the
   same person. With a self-minted index **we** own the matching decision a
   national ID would otherwise have made — and a false match merges two
   people's medical records.
4. **Whether the household number is per-tenant or platform-wide** (it may
   differ from the patient identifier).

**Indicative sequencing:** patient identity (identifier schemes, households,
issuer) → provider identity and registry → audit → intra-tenant care teams
and break-glass → consent artefact, guardianship and delegation →
cross-tenant federated exchange → public provider directory. This
authorization phase is a prerequisite for all of them.
