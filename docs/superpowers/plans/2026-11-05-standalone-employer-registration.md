# Standalone pharmacy employer registration

Status: implementation plan for slice 1 of the
[standalone pharmacy employer specification](../specs/2026-11-04-standalone-pharmacy-employers.md).
Refs #19. Implements [ADR-0054](../../adr/0054-typed-employing-establishments.md)
decisions 1–3 and 7. Branch `feat/19-establishment-kind-registration`, based on
`01d4a284`.

## Scope

This slice lets a tenant register an `independent` or `chain` Pharma
organization as a Workforce employing establishment. It does not create Staff,
access grants, postings or HR records in that establishment. Slices 2–6 add
those surfaces one reviewed table and route at a time.

## Actors and objects

| Actor or object        | Authority kind | Permission                  | Scope  | 404                                                                                  | 403                | 503                                  |
| ---------------------- | -------------- | --------------------------- | ------ | ------------------------------------------------------------------------------------ | ------------------ | ------------------------------------ |
| Tenant administrator   | Governance     | `workforce.hospital.manage` | Tenant | —                                                                                    | Missing permission | IAM or FGA down                      |
| Pharmacy establishment | Workforce row  | `workforce.hospital.manage` | Tenant | Unknown, foreign-tenant, non-UUIDv7, `hospital_owned` or registered as a hospital ID | Missing permission | Pharma guard or Workforce store down |
| Hospital establishment | Workforce row  | Unchanged                   | Tenant | Any ID registered as a pharmacy establishment, on every hospital-family route        | Unchanged          | Workforce store down                 |

The two-owner rule (ORG-D7) is unchanged. Registration confers no Staff, HR,
dispensing or outlet authority.

## Decisions settled by this plan

1. **Migration `0088_workforce_establishment_kind`.** It adds
   `establishment_kind text NOT NULL DEFAULT 'hospital'` and nullable
   `pharmacy_ownership_type` to `workforce_hospitals`. A CHECK requires an
   ownership type of `independent` or `chain` exactly when the kind is
   `pharmacy`. The name CHECK widens from 150 to 200 characters to admit every
   Pharma organization name. The hospital route still binds 150. The constant
   default makes the column add a catalogue-only change, so every existing row
   reads as `hospital`.
2. **Immutability.** A `BEFORE UPDATE` trigger refuses any change to kind or
   ownership type. It also refuses a name change on a pharmacy row, because the
   name mirrors the immutable Pharma organization.
3. **Every establishment table refuses a pharmacy establishment.** A
   `BEFORE INSERT OR UPDATE OF tenant_id, hospital_id` trigger,
   `workforce_require_hospital_establishment()`, is attached to 18 tables:
   - the 17 tables with a foreign key to `workforce_hospitals`, including
     `workforce_staff` and `workforce_hospital_access`;
   - `workforce_leave_routing_revisions`, which has no foreign key and is
     reached only through an access grant.

   Every other establishment table is reached through one of them by a foreign
   key that carries `hospital_id`. No Workforce row can name a pharmacy
   establishment until a later slice reviews and lifts the guard for that
   table. A missing or invisible registry row is refused too, so the guard
   fails closed. It raises SQLSTATE `23503`, which existing handlers already
   answer with 404. Kind and identity are immutable, so the check cannot be
   invalidated before commit.

4. **The catalogue enforces the inventory.** A real-Postgres test walks
   `pg_constraint` and `pg_trigger`. It fails when any Workforce table with a
   `hospital_id` column is neither guarded nor reached through a foreign key
   that maps its `hospital_id` to a guarded or reached table. Two tables are
   exempt by name:
   - `workforce_audit`, because registration must audit;
   - `workforce_organization_observations`, which records an Organization
     hospital fact and is not a Workforce establishment row.

   The Organization observation consumer reads only rows of kind `hospital`,
   so a pharmacy employer is never reported as an existing Workforce hospital.

5. **Route families admit only their own kind.** `platform.Router` gains
   `Establishment(kind, param, admission)`. It returns a nested router whose
   routes run the admission after membership, permission and scope admission,
   and before the handler. It records the kind on each `DeclaredRoute`, and it
   panics at registration when the path lacks the parameter. Workforce mounts
   every `/workforce/hospitals/:hospital/...` route through a hospital
   establishment router and the new family through a pharmacy one. The
   admission answers 404 for an ID registered with the other kind and leaves
   unregistered IDs to the handler. When the registry cannot be read it answers
   503 `establishment_unavailable` with `no-store`, because an unreadable row
   could hide an establishment of the other kind. The router accepts
   `authz.Public` routes, whose handlers authorize the hospital themselves, and
   refuses `NoTenantMembership` and an empty permission. An arch test in
   `internal/archtest` fails if a route under either prefix is declared without
   its kind. The hospital list route has no ID, so it stays on the plain
   `/workforce/hospitals` router and filters on `establishment_kind = 'hospital'`
   instead.
6. **Typed scope extractors.** The existing `scope(c)` keeps its name and
   still reads only the `hospital` parameter. The new `pharmacyScope(c)` reads
   only `pharmacy` and returns a distinct `pharmacyEstablishment` type, which is
   the only type the pharmacy transaction accepts. A hospital handler mounted
   under `:pharmacy` therefore answers 404, and the admission and the guard
   trigger refuse it as well. The hospital ID stays `uuid.UUID` in this slice.
   Slice 3 adds the distinct hospital type when one handler first serves both
   families. Converting 153 call sites now would add no protection that the
   three layers above do not already give.
7. **Hospital registration cannot touch a pharmacy row.** The `putHospital`
   upsert updates only rows of kind `hospital`. It then reads the kind and
   answers 404 when the ID belongs to a pharmacy. `deleteAccess` counts only
   hospital rows. Both close the race in which the admission sees no row.
8. **Pharma employer guard.** `pkg/authz.PharmacyEmployerGuard` has the method
   `CurrentPharmacyEmployer(ctx, tenant, pharmacy)`, which returns the ID, name
   and ownership type. Pharma implements it in `employer_guard.go`:
   - It refuses outside a guarded command, which fails closed as unavailable.
   - It reads the organization `FOR KEY SHARE` in the caller's transaction.
   - It answers `ErrPharmacyEmployerHidden` for a missing, non-UUIDv7 or
     `hospital_owned` organization.
   - Any other error is unavailable.

   `platform.PharmacyEmployers(modules, db)` binds, through the `soleOwner`
   helper it shares with department references, exactly one module named
   `pharmacy`, else nil. A nil guard answers 503. `Deps.PharmacyEmployers`
   carries it, wired in `cmd/api/main.go` and the test harness.

9. **Registration command.** `PUT /workforce/pharmacies/{pharmacy}` takes `{}`.
   - Inside one guarded Workforce command, it calls the guard, inserts the row
     with `ON CONFLICT DO NOTHING`, then reads the row back.
   - A row of the other kind answers 404.
   - A retry returns the retained row with its original `created_at`, and
     writes no second audit row.
   - Name and ownership type come only from Pharma, never from the client.
   - Registration publishes no event, like hospital registration. No consumer
     needs one. The existing `workforce_hospital_audit` trigger records the
     insert in the same transaction. This corrects the spec's outbox sentence.

   `GET /workforce/pharmacies/{pharmacy}` reads the Workforce row only, without
   the guard. Both routes declare `workforce.hospital.manage` and set
   `Cache-Control: no-store`. Both return 200 with the same body.

10. **Contract.** `contracts/workforce-establishments.json` defines
    `PharmacyEmployer`. `scripts/generate-workforce-establishments.mjs`
    generates the Go model and the SDK Zod schema, and CI runs its `--check`.
    The SDK adds `pharmacyEmployerPath`, `getPharmacyEmployer` and
    `registerPharmacyEmployer`, which parse the response strictly and refuse a
    row for another pharmacy. `getPharmacyEmployer` treats 404 as "not
    registered" and surfaces every other failure. `usePharmacyEmployer` reads
    it with no cache.
11. **UI.** Pharmacy setup shows an "Employer" panel for the selected
    organization:
    - An `independent` or `chain` organization shows its registration state and
      a "Register as employer" button, gated by `workforce.hospital.manage`.
    - A `hospital_owned` organization says that its staff are employed by the
      owning hospital.

    Without `workforce.hospital.manage` the panel makes no request and says who
    manages registration. An unreadable registration shows a retry, never the
    register button. A Vitest test covers these states.

## Numbers and failure behaviour

The budgets are inherited and unmeasured: 50 peak requests/s, 80:20 reads to
writes, p99 below 500 ms. Each hospital-family request adds one primary-key
probe, and each establishment-table write adds one probe in its trigger. Both
are indexed lookups on a table with one row per establishment. No new service,
cache or store is added.

- When the Pharma guard is down or unbound, registration returns 503.
- When the Workforce store is down, the admission returns 503 and the handlers
  return 500 as before.
- When the audit write fails, the registration rolls back.

## Test-first order

Each step shows the failing test before the code that satisfies it.

1. Real Postgres, migration:
   - existing rows read as `hospital`;
   - CHECK refusals;
   - immutability;
   - the guard on `workforce_staff`, `workforce_hospital_access`,
     `workforce_hr_masters` and `workforce_leave_routing_revisions` under a
     pharmacy row;
   - the observation consumer reports a pharmacy ID as absent;
   - hospital writes still pass;
   - the catalogue inventory test.
2. Pharma guard against real Postgres:
   - independent and chain facts;
   - `hospital_owned`, missing and non-v7 IDs are hidden;
   - unguarded calls are unavailable.
3. Platform:
   - provider binding is nil without a DB, without Pharma or with a duplicate
     Pharma;
   - router admission order;
   - panic on a missing parameter;
   - the kind is recorded.
4. Workforce HTTP against real Postgres. Module tests cannot import another
   module, so the tests that need Pharma live in `internal/journey`:
   - registration for independent and chain organizations, exact retry and a
     single audit row;
   - `hospital_owned`, unknown, foreign-tenant and hospital-kind IDs return 404
     on both families;
   - a pharmacy ID returns 404 on the hospital `PUT`, access, staff and list
     routes;
   - the hospital list excludes pharmacy rows;
   - missing permission returns 403;
   - a nil or failing guard returns 503;
   - the arch test covers kinds.
5. SDK, generator `--check` and the UI panel.

## Not covered

- Pharmacy-family Staff, access grants, IAM roots and postings (slices 2–5).
- The distinct Go type for hospital IDs (slice 3).
- Name refresh. Pharma organizations are immutable.
- Unregistering an employer.
- Branch grouping (ORG-D9).

## Gates

- `go build ./...`, `go vet ./...` and `go test -race` for the affected
  packages, plus the coverage gate.
- `make lint-go`.
- `go run ./cmd/contractdocs` and `-check`.
- The staff access and operational inventory checks.
- The new generator `--check`.
- `pnpm turbo lint type-check test build format:check`.
