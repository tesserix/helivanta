# Typed IAM establishment root

Status: implementation plan for slice 2 of the
[standalone pharmacy employer specification](../specs/2026-11-04-standalone-pharmacy-employers.md).
Refs #19. Implements [ADR-0054](../../adr/0054-typed-employing-establishments.md)
decision 4. Branch `feat/19-typed-iam-root`, based on `9ec1cf9c`.

## Scope

This slice gives every IAM assignment a typed establishment root and proves
that a root of one kind never authorizes the other. It writes no pharmacy
assignment in production. No route, proposal input, verifier or template
can create one yet. Slice 3 adds the first one, together with the pharmacy
route that consumes it.

## Actors and objects

| Actor or object               | Authority kind | Scope                       | 404                                                    | 403                | 503                                 |
| ----------------------------- | -------------- | --------------------------- | ------------------------------------------------------ | ------------------ | ----------------------------------- |
| Proposer, reviewer (existing) | Administration | Hospital root               | A pharmacy-rooted proposal on every hospital route     | Unchanged          | IAM or FGA down                     |
| Scoped assignment snapshot    | IAM            | Root `(kind, id)` in tenant | Invisible to every path that has not declared the kind | —                  | IAM or FGA down                     |
| Staff access template         | IAM contract   | Declared root kinds         | —                                                      | —                  | —                                   |
| OpenFGA scoped permission     | IAM projection | Root-bound object key       | —                                                      | Missing graph edge | `ErrScopedAuthorizationUnavailable` |

## Decisions settled by this plan

1. **The authz root is a separate field.** `authz.ScopedAssignment` gains
   `PharmacyID`. Exactly one of `HospitalID` and `PharmacyID` names the root,
   so a pharmacy ID can never travel as `HospitalID`. A pharmacy root admits
   one scope kind, `pharmacy`, whose scope ID is the pharmacy ID. Hospital
   roots keep `hospital`, `staff` and `department`. This replaces the
   specification's `EstablishmentKind`/`EstablishmentID` pair, which would
   carry the hospital ID twice and need a rule to keep the copies equal.
2. **Hospital object keys keep their bytes.** `PharmacyID` is `omitempty`, so
   the JSON of a hospital root is unchanged and every existing OpenFGA tuple
   still matches. A test pins the hospital, staff and department keys that the
   code on `9ec1cf9c` produced. A pharmacy root marshals an empty `HospitalID`
   and a `PharmacyID`, so it can never collide with a hospital key.
3. **The OpenFGA model is unchanged.** No `hospital` or `pharmacy` type exists
   today. The root is bound inside the content-hashed object key, so the
   projection is the only part that changes. This corrects the specification,
   which listed a model change.
4. **Migration `0033_iam_establishment_root`** (`assignment_establishment_root.sql`):
   - adds `establishment_kind text NOT NULL DEFAULT 'hospital'` to
     `iam_assignment_proposals` and `iam_assignment_snapshots`. A constant
     default is a catalog-only change, so existing rows are backfilled
     without a table rewrite. The physical `hospital_id` column stays the
     establishment key, as ADR-0054 §3 records for Workforce;
   - replaces the 0031 scope checks with root checks. A hospital root takes
     `hospital` or `department` scope. A pharmacy root takes `pharmacy` scope
     and no department. The proposal template-scope check reads a pharmacy
     scope as the template's `hospital` scope kind. Each check is added
     `NOT VALID` and then validated;
   - adds `iam_assignment_template_establishment_kinds(key)`, which answers
     `{hospital}` for every template that has permissions and `{}` otherwise.
     A new `iam_assignment_proposals_template_establishment_check` requires
     the row's kind to be declared, so every existing template refuses a
     pharmacy root;
   - adds a restrictive `establishment_root` policy on both tables. A
     non-hospital row is visible only when the transaction sets
     `app.assignment_establishment_root='typed'`, which no slice 2 path sets.
     Every existing list, decision, projection and authority query is
     therefore hospital-only by construction. A rolled-back binary is too,
     because the policy lives in the schema;
   - narrows the 0031 `department_scope` policy to department rows, so a
     pharmacy root is governed by `establishment_root` alone;
   - refuses any change to `establishment_kind` with the
     `iam_assignment_establishment_immutable` trigger. Its name sorts before
     `iam_assignment_proposal_guard`, so the root refusal fires first. The
     snapshot guard and recorder are redefined so the snapshot carries the
     proposal's kind and the guard compares it.
5. **Scope epochs and employment bases stay keyed by `hospital_id`.** That
   column is the establishment key. The Workforce registry gives one ID one
   immutable kind per tenant, and every authority read binds the root scope
   kind. A shared epoch row could therefore only over-revoke. It could never
   widen a grant.
6. **Templates declare `establishment_kinds`.** `TemplateDefinition` gains the
   field. Its values are `hospital` and `pharmacy`.
   - Templates introduced before IAM migration 0034 declare nothing and
     publish `["hospital"]`, so their definition and catalog digests do not
     change.
   - From 0034, a template must declare `["hospital"]` or
     `["hospital","pharmacy"]`. A pharmacy-assignable template must include
     the `hospital` scope kind. Department templates stay `["hospital"]`.
   - Generated SQL from 0034 regenerates the establishment-kinds function
     from the declarations. IAM 0033 is hand-written, so the generator's
     append fixture numbers new templates from 0034.
   - The Staff access UI compares the published kinds with its bundled
     definitions and refuses a catalog that differs.
7. **The Workforce scope verifier is unchanged.** Its request carries
   `hospital_id`. Migration 0088's guard refuses access rows for pharmacy
   establishments, so no pharmacy actor can be a scope manager. A pharmacy
   verifier needs a new event version, which belongs to slice 3.

## Numbers and failure behaviour

The planning numbers are inherited and unmeasured. The 0033 column adds touch
only the catalog, and validation reads each table once under the migration's
5 s lock timeout. Authority queries keep their existing indexes, and nothing
is added to the request path.

- When FGA is down, `CheckScopedAssignment` and the projection return
  `ErrScopedAuthorizationUnavailable`, which callers map to 503.
- When the database is down, IAM authority returns 503.
- A pharmacy-rooted row that an unaware path somehow reads still fails
  closed: `objectKey` refuses a `pharmacy` scope under a `HospitalID`.

## Test-first order

Each step's test is run and shown failing before the code that satisfies it.

1. **Key bytes.** `TestHospitalRootObjectKeysAreUnchanged` pins the three keys
   above. It passes on `9ec1cf9c`, which records the baseline.
2. **Root validation.** Rejected inputs: both roots, neither root, a pharmacy
   root with `hospital`, `staff` or `department` scope, a `pharmacy` scope
   under a hospital root, a scope ID that differs from the pharmacy, and a
   nil or non-canonical pharmacy ID.
3. **Real OpenFGA.** One ID is used as both roots:
   - a pharmacy grant fails the hospital check, and the reverse;
   - revoking tenant membership ends the pharmacy grant;
   - closing the proxy returns `ErrScopedAuthorizationUnavailable`.
4. **Generator.** Each 0034 declaration rule refuses before any artifact is
   written. Historical digests are unchanged. A 0034 extension regenerates
   the function.
5. **Backfill.** A `before0033` fixture holds an active hospital assignment, its
   FGA projection, its decisions and its epochs, among 100,000 synthetic
   neighbours. After migration:
   - every row reads `hospital`;
   - versions, epochs and decisions are unchanged;
   - each table's `relfilenode` is unchanged, so no rewrite happened;
   - `AuthorizeScope` still grants with the pre-migration tuple.
6. **Template refusal.** A direct insert of a pharmacy-rooted proposal for a
   hospital-only template fails 23514, even with the typed flag set. Every
   root and scope mismatch fails the same way.
7. **Invisibility.** A test-only function admits one template on a pharmacy
   root, and a pharmacy-rooted proposal is inserted under the typed flag.
   - The leave proposal list and its decision history never show it.
   - A hospital-only session counts zero non-hospital rows.
   - Changing its kind fails.
8. **Published declarations.** For every published template, the
   `establishment_kinds` value equals the database function's answer.

## Not covered

- Pharmacy proposal input, a `pharmacy_id` wire version, the pharmacy scope
  verifier and IAM authority for pharmacy requests. Slice 3 delivers these
  with its first pharmacy route.
- `authz.ScopeRequest` stays hospital-only until that route exists.
- Pharmacy-assignable templates, and any UI for choosing a root.
- Renaming `hospital_id` in IAM tables.

## Gates

From `backend/`:

- `go build ./...` and `go vet ./...`;
- `make lint-go`;
- `go test -race ./pkg/authz/... ./internal/modules/iam/...`;
- `./scripts/coverage-gate.sh`;
- `go run ./cmd/contractdocs -check`.

From the root:

- `node scripts/generate-staff-access.mjs --check`;
- `node --test scripts/staff-template-migrations.test.mjs`;
- `pnpm turbo lint type-check test build format:check` for the regenerated
  TypeScript.
