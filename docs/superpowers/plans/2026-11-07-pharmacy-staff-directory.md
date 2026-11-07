# Pharmacy Staff directory and access

Status: implementation plan for slice 3a of the
[standalone pharmacy employer specification](../specs/2026-11-04-standalone-pharmacy-employers.md).

Refs #19. Implements part of
[ADR-0054](../../adr/0054-typed-employing-establishments.md) decision 3. The branch is
`feat/19-pharmacy-staff-directory`, based on `667ec0e2`.

## Scope

A registered standalone pharmacy can now employ Staff. A Workforce manager of
that pharmacy keeps its directory: they list, read, create and edit Staff
records, and they grant or revoke reader and manager access. The Pharma Staff
screen reads the pharmacy family for a standalone organization.

Every other Staff surface stays hospital-only. These include enrollment,
identity, login links, employment, credentials, leave, rosters, payroll and
postings. Their tables refuse a pharmacy establishment in the database.

Slice 3 is split. Slice 3b-i delivers account links on the pharmacy family
([plan](2026-11-08-pharmacy-login-links.md)). Slice 3b-ii delivers IAM for
pharmacies: proposal input, the `pharmacy_id` verifier version, a
pharmacy-assignable template, the root picker, and identity on the pharmacy
family.

## Actors and objects

| Actor or object                        | Authority kind  | Scope                  | 404                                                           | 403                                 | 503                       |
| -------------------------------------- | --------------- | ---------------------- | ------------------------------------------------------------- | ----------------------------------- | ------------------------- |
| Tenant administrator                   | Governance      | Tenant                 | Unknown, foreign-tenant or hospital ID on the pharmacy family | Missing `workforce.hospital.manage` | Registry, IAM or FGA down |
| Pharmacy Workforce manager             | Administration  | One pharmacy           | No access row, or the establishment is not a pharmacy         | —                                   | Registry or database down |
| Pharmacy Workforce reader              | Administration  | One pharmacy           | As above                                                      | —                                   | As above                  |
| Pharmacy Staff record                  | Workforce       | Pharmacy establishment | Outside the caller's access, wrong family or foreign tenant   | —                                   | Database down             |
| Hospital assignment-scope verification | Workforce event | Hospital               | A pharmacy ID now answers `eligible=false`                    | —                                   | —                         |

Access rows stay the Workforce grant behind the directory, as they are for
hospitals. Pharmacy membership in Pharma confers nothing.

## Decisions settled by this plan

1. **Migration `0092_workforce_pharmacy_staff_directory`**
   (`pharmacy_staff_directory.sql`):
   - drops the 0088 hospital-only trigger from `workforce_staff` and
     `workforce_hospital_access`. Their foreign key to `workforce_hospitals`
     still requires a registered establishment;
   - attaches `workforce_require_hospital_establishment` to every table with a
     foreign key to `workforce_staff`, except `workforce_identity_epochs`, which
     a Staff insert writes. Those tables were covered through the Staff guard,
     and now each refuses a pharmacy itself;
   - changes no RLS. `app.workforce_hospital` still carries the establishment
     ID.
2. **The guard inventory pins the pharmacy-admitting set.**
   `TestEveryEstablishmentTableIsGuardedOrReachedThroughAGuardedTable` stops
   treating `workforce_staff` as a guarded root. It compares the unguarded
   tables with the reviewed set `workforce_hospital_access`,
   `workforce_identity_epochs` and `workforce_staff`. A new table under Staff
   fails CI until it is guarded or reviewed.
3. **The scope verifier answers for hospitals only.** The
   `workforce-assignment-scope` consumer adds `h.establishment_kind='hospital'`.
   A pharmacy manager could otherwise be verified for a hospital-rooted IAM
   assignment that names a pharmacy ID. The event version is unchanged. The
   pharmacy verifier is a new version in slice 3b.
4. **No distinct hospital Go type.** The specification expected one when a
   handler first serves both families. It is not needed, for these reasons:
   - hospital handlers read `:hospital`, which no pharmacy route declares, so a
     hospital handler mounted under the pharmacy family answers 404 on every
     request. The reverse holds for `:pharmacy`;
   - the shared directory and access cores take the kind from their family
     wrapper and replay the check inside the transaction, because admission
     runs before it. Commands require the ID to be registered as their kind,
     which the foreign key demanded already. Reads refuse only an ID
     registered as the other kind, as admission does, so an unregistered or
     foreign-tenant hospital list keeps its v1 empty page. Each read statement
     also filters by kind, so a registration that lands between the check and
     the read never surfaces the other family's rows.

   Changing `scope(c)` would touch 170 call sites without adding a refusal.
   The specification is corrected in this change.

5. **Pharmacy routes.** These run under the existing pharmacy establishment
   router:
   - `PUT` and `DELETE /:pharmacy/access/:subject` need
     `workforce.hospital.manage`;
   - `PUT` and `GET /:pharmacy/staff/:id`, and the list
     `GET /:pharmacy/staff`, need tenant membership, then the access row inside
     the transaction, as the hospital family does. They are declared
     `authz.Public` like the hospital Staff routes; that value keeps tenant
     membership and only skips the route permission.

   `GET /workforce/pharmacies` lists the pharmacies where the caller has an
   access row, with their `access_level`, as `GET /workforce/hospitals` does
   for hospitals. It needs tenant membership only and is paginated.

   The pharmacy family has no unversioned clients, so record `GET` and `PUT`
   require `directory_context=v1` and answer 400 without it. The list answers
   404 when the caller has no access row, because an empty page would read as
   an empty directory. A request with `hr_intake` returns 400, because the IAM
   intake projection is hospital-only until slice 3b.

6. **Contracts.** `contracts/workforce-establishments.json` gains
   `PharmacyStaff`, `PharmacyAccess` and `PharmacyStaffSource`. They carry
   `pharmacy_id` and never `hospital_id`. Go and Zod are generated, and the directory metadata
   (`version`, `directory_context_version`) is shared with the hospital
   contract.
7. **Registration grants the registrant manager access.** The UI does this as
   `HospitalSetup` does. It calls `PUT /access/{self}` at `manager` after the
   employer `PUT`, and verifies the grant names that pharmacy, subject and
   level. Both writes are idempotent. If the grant fails after registration,
   the registered panel keeps the error and a retry.
8. **Pharma Staff screen.** `/pharmacy/staff` has two sources:
   - pharmacy employers come from `GET /workforce/pharmacies`. Only an
     `independent` or `chain` organization can be registered, so this lists
     standalone pharmacies only. The chosen one reads
     `GET /workforce/pharmacies/{id}/staff`. A `manager` there can add and edit
     records; a `reader` sees no write controls. The API checks the manager row
     on every write;
   - a `hospital_owned` organization's Staff stay in the hospital family source,
     which is unchanged;
   - a 404 from the directory or a record means the caller's Workforce access
     ended. The screen says the access changed and offers to reopen, and never
     shows an empty directory.

   Discovery replaces reading the Pharma organization picker. The Staff screen
   has no picker, and discovery carries the access level the screen needs.

## Numbers and failure behaviour

The planning numbers are inherited and unmeasured: 50,000 Staff per
establishment, with 200-record pages. Each command adds one registry primary
key read. The new triggers each do one primary key probe on insert.

- When the registry is down, admission returns 503.
- When the database is down, commands return 503 and no row is written.
- When an audit write fails, the trigger raises and the command rolls back.
- The verifier answers `eligible=false` for a pharmacy, never an error, so IAM
  closes the proposal instead of retrying it.

## Test-first order

Each assertion is run and shown failing before the code that satisfies it.

1. **Inventory.** With the reviewed set pinned, the inventory test fails on
   `667ec0e2`, because Staff and access are still guarded. After the trigger
   drop it lists every unguarded Staff child. The probe migration proves that a
   new Staff child is reported.
2. **Refusal matrix.** Staff and access inserts on a pharmacy succeed. An
   insert on a pharmacy fails `23503 workforce_hospital_establishment` for
   enrollment profile, login link, leave assignment, employment period draft
   and posting draft. Hospital writes still succeed.
3. **Verifier.** A manager access row on a pharmacy plus a scope request for
   that ID publishes `eligible=false`. A hospital manager still gets `true`.
4. **Routes.**
   - The pharmacy family creates, reads, lists and edits Staff, using
     `directory_context=v1` and `expected_version`.
   - A hospital ID or a foreign-tenant pharmacy returns 404.
   - A reader cannot write, and a caller without an access row gets 404.
   - `hr_intake` returns 400.
   - Hospital Staff routes on a pharmacy ID return 404.
   - Each response carries `pharmacy_id` and no `hospital_id`.
5. **Race.** A pharmacy registered between admission and a hospital command
   or read returns 404 and writes nothing. With the read statement's kind
   filter removed, the hospital family returns the pharmacy's Staff record,
   which proves the filter is the barrier.
6. **SDK and UI.** Vitest covers:
   - the response parser refuses a mismatched `pharmacy_id`, a `hospital_id`
     key and duplicate rows;
   - a save is verified, and an unchanged save keeps its version;
   - the standalone screen reads the pharmacy family under the expected
     account, and a reader gets no write controls;
   - a manager edits a record against its current version;
   - a 404 shows that access changed, never an empty directory;
   - the hospital source keeps the hospital family;
   - registration grants manager access, verifies it and keeps a retry.

## Not covered

- IAM pharmacy roots in production, and anything else in slice 3b.
- HR lifecycle on a pharmacy (slice 4), and branch postings (slice 5).
- An access-management UI for other subjects. Hospitals have none either, and
  the access API serves both families.

## Gates

From `backend/`:

- `go build ./...` and `go vet ./...`;
- `make lint-go`;
- `go test -race ./internal/modules/workforce/... ./internal/platform/...`;
- `./scripts/coverage-gate.sh`;
- `go run ./cmd/contractdocs -check`.

From the root:

- `node scripts/generate-workforce-establishments.mjs --check`;
- `pnpm turbo lint type-check test build format:check`.
