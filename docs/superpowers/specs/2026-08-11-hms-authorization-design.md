# HMS Authorization — OpenFGA Decision Layer

Date: 2026-08-11
Status: approved
Builds on: phase 1 repo setup (PR #754), phase 2 zones (PR #755), frontend standards (PR #757), backend standards (PR #758)
Resolves: issue #1 (OpenFGA as the single authorization decision point), repo-setup spec D4

## Goal

Every HMS route is currently reachable by any authenticated user in the
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
  Revocation takes effect on the caller's next request; there is no
  decision cache and therefore no staleness window. A short-TTL cache may
  later be added *inside* `pkg/authz` as a pure optimisation without
  touching any call site.
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
- **D8 — Tenant switching is in scope.** `tenant_id` is a claim inside
  the verified GIP token, so a clinician working at two hospitals cannot
  currently switch without a separate login. `iam_members` already holds
  every membership, so this phase adds listing, a membership-gated switch
  that re-mints the session, and a shell picker.
- **D9 — Department scoping is a named non-goal, designed for.**
  `pkg/authz` takes scope as a parameter that is always tenant-wide in
  this phase, and the FGA model reserves the scoping shape. Departments
  and per-record relations drop in later without reshaping call sites.

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
- Events: `hms.in.iam.member_granted.v1`, `hms.in.iam.member_revoked.v1`,
  `hms.in.iam.role_changed.v1`. Consumer `iam-fga-sync` applies tuples
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
- **Patient identity — pluggable multi-scheme identifiers.** A patient
  holds 0..n identifiers, each `{scheme, value, verified, valid_from,
  valid_to}` — for example `in.abha`, `au.ihi`, `uk.nhs`,
  `au.medicare.card` (with IRN). The country profile decides which schemes
  apply and which are authoritative. Consistent with the ABDM adapter and
  country-profile direction already in `docs/sdk/architecture.md`.
  Demographic matching was rejected as a primary mechanism: a false match
  merges two people's medical records.
- **Individual vs family-scoped identifiers.** Only *individual* schemes
  (`au.ihi`, `in.abha`, `uk.nhs`) may establish clinical identity.
  Family-scoped identifiers such as an Australian Medicare card number are
  enrolment and benefits artefacts — searchable for billing and reception,
  never sufficient alone to identify a patient clinically. Cards are
  reissued, split on separation, and members age off them; clinical
  identity must not inherit that lifecycle.
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

**Indicative sequencing:** patient identity (including identifier schemes
and households) → audit → intra-tenant care teams and break-glass →
consent artefact, guardianship and delegation → cross-tenant federated
exchange. This authorization phase is a prerequisite for all of them.
