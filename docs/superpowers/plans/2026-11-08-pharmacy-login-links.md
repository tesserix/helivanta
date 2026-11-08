# Pharmacy Staff account links

Status: implementation plan for slice 3b-i of the
[standalone pharmacy employer specification](../specs/2026-11-04-standalone-pharmacy-employers.md).

Refs #19. Implements part of
[ADR-0054](../../adr/0054-typed-employing-establishments.md) decision 3. The branch is
`feat/19-pharmacy-login-links`, based on `91ac5fec`.

## Scope

A pharmacy Workforce manager can link a tenant account to a pharmacy Staff
record. A second person reviews the link, as for a hospital. The Pharma Staff
screen opens the account-link panel for a pharmacy record.

Slice 3b is split, because the two halves have different dependencies:

- **3b-i (this plan): account links.** Their authority is a tenant permission
  plus the Workforce manager access row, so they need no IAM change.
- **3b-ii: IAM pharmacy root and identity.** Identity sources and identity cases
  run under a scoped IAM grant and need the reviewed actor link that 3b-i
  creates. 3b-ii adds the pharmacy proposal input, the `pharmacy_id` scope
  verifier version, pharmacy-assignable identity templates, `PharmacyID` on
  `authz.ScopeRequest` and the root picker.

An active pharmacy link authorizes nothing in this slice. Every link consumer
outside the link routes is hospital-only, as decision 6 shows.

## Actors and objects

| Actor or object             | Authority kind | Scope                 | 404                                                                               | 403                                                                               | 503                                     |
| --------------------------- | -------------- | --------------------- | --------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- | --------------------------------------- |
| Link proposer               | Administration | One pharmacy          | No manager access row, Staff outside the pharmacy, wrong family or foreign tenant | Missing `workforce.login_links.manage`                                            | Registry, database or IAM roles down    |
| Link reviewer               | Administration | One pharmacy          | As above                                                                          | Missing `workforce.login_links.review`, or reviewer is proposer or linked account | As above                                |
| Pharmacy Staff account link | Workforce      | Pharmacy Staff record | Outside the caller's access, wrong family, foreign tenant or a non-v7 ID          | —                                                                                 | Database down                           |
| Linked account              | IAM membership | Tenant                | Not a member of the tenant                                                        | —                                                                                 | IAM roles down (`identity_unavailable`) |
| Pharmacy link change event  | Workforce      | Pharmacy              | —                                                                                 | —                                                                                 | Outbox write fails, command rolls back  |

Both actors already exist for hospitals with the same permissions, which tenant
administrators assign through tenant roles. A pharmacy membership in Pharma
confers nothing.

## Decisions settled by this plan

1. **Migration `0093_workforce_pharmacy_login_links`**
   (`pharmacy_login_links.sql`) drops the 0092 `workforce_hospital_establishment`
   trigger from `workforce_login_links`. `workforce_login_link_history` was
   reached only through that table, so both join the reviewed
   pharmacy-admitting set. Every other table that references a link keeps its
   own guard, so posting approvals and cutoffs still refuse a pharmacy.

   The guard inventory also changes. It counted the link table as reached
   through `workforce_employment_generations`, but that foreign key uses the
   nullable `employment_generation_id`, and Postgres skips a foreign key check
   when a key column is null. An edge now carries the guard only when every
   local key column is `NOT NULL` or a stored generated string constant. Any
   other generated expression counts as nullable, so the inventory fails closed.

2. **Pharmacy links stay unbound to an employment generation.** The binding
   trigger accepts an unbound link only when the Staff record has no review
   cohort. Generations and cohorts both refuse a pharmacy row, so every pharmacy
   link is unbound until slice 4 reviews generations for pharmacies. A test
   proves the approved link stores no generation.
3. **Shared link cores take the establishment.** The four handlers become
   cores that take an `establishment`. Thin hospital and pharmacy wrappers
   build it from `:hospital` or `:pharmacy`. The link transaction runs
   `requireKind` before the access check, because admission ran before it. A
   link read needs the Staff record, which an unregistered ID never has, so
   reads use the same check and keep their current 404.
4. **Pharmacy routes.** They run under the pharmacy establishment router with
   the hospital family's permissions:
   - `PUT /:pharmacy/staff/:id/login-links/:link` needs
     `workforce.login_links.manage`;
   - `POST /:pharmacy/staff/:id/login-links/:link/decision`,
     `GET /:pharmacy/staff/:id/login-links` and
     `GET /:pharmacy/staff/:id/login-links/:link/history` declare
     `authz.Public` and check `manage` or `review` in the handler, as now.

   The contract has no establishment ID, so both families share
   `contracts/workforce-login-links.json` unchanged.

5. **A pharmacy change publishes its own subject.**
   `helivanta.in.workforce.pharmacy_login_link_changed.v1` carries
   `PharmacyLoginLinkChangedData{pharmacy_id, staff_id, link_id, version, status}`.
   It is a new subject rather than a second version of `login_link_changed`.
   That subject keeps its v1 meaning: its `hospital_id` is always a hospital.
   No module consumes either subject today. One publisher chooses the subject
   from the establishment kind and refuses any other kind. It runs for
   proposals, decisions and `revokeInactiveStaffLinks`, so making a pharmacy
   Staff record inactive revokes its links and publishes the pharmacy subject.
6. **Link discovery is hospital-only.** `leaveReviewScopeCandidates` is the
   one candidate query behind the staff-actor, leave-review, HR-intake,
   employment-review, person-review and posting scope routes. It gains
   `h.establishment_kind='hospital'`, so a pharmacy link never appears as a
   hospital choice. IAM would deny it anyway, because no hospital-rooted
   assignment can name a pharmacy. The filter keeps that from depending on IAM
   and keeps pharmacy rows out of the 20-candidate page. Other link readers
   are keyed by a hospital-family `:hospital` or a hospital-rooted IAM scope,
   so they never read a pharmacy link.
7. **SDK.** `LoginLinkScope` takes
   `establishment: { kind: "hospital" | "pharmacy"; id }` in place of
   `hospitalId`. The path and query key both follow the kind, so a hospital and
   a pharmacy with the same ID never share a cache entry. The response parsers
   are unchanged.
8. **UI.** `StaffLoginLinkPanel` moves to `components/workforce`, beside the
   shared Staff form, because two zones now use it. It takes an establishment
   `{ kind, id, name, access_level }` and a Staff
   `{ id, display_name, employee_code, employment_status }`. The hospital page
   passes its hospital.
   The pharmacy Employer view adds an `Account link` row action. It shows only
   for a `manager` who holds `manage` or `review`. The panel keeps its
   statement that a link grants no application access or clinical authority.
   A refusal clears the links, as now. A directory 404 still reports that
   access changed.

## Numbers and failure behaviour

The planning numbers are inherited and unmeasured: 50,000 Staff per
establishment, and at most one pending or active link per Staff record and per
account. Link pages default to 50 records and are capped at 200. Each command
adds one registry primary key probe. No service, cache or store is added.

- When the registry is down, admission returns 503.
- When IAM roles are down, proposal and approval return 503
  `identity_unavailable` and write nothing.
- When the database is down, the request fails and no row is written.
- When an audit or outbox write fails, the command rolls back.

## Test-first order

Each assertion is run and shown failing before the code that satisfies it.

1. **Inventory.** Adding the two link tables to `pharmacyAdmittingTables` fails
   without migration 0093, because the link table is still guarded. Migration
   0093 makes it pass only after the edge rule above, because the nullable
   generation edge had hidden the drop. Probe tables prove that a nullable key
   and a computed generated key are reported, and that a constant generated key
   still carries the guard.
2. **Unbound link.** The 0092 matrix has no login-link entry, and it still
   proves that every other Staff child refuses a pharmacy. A new SQL test
   proposes a pharmacy link and has an independent manager approve it. The link
   and both history versions store no generation, and a forged generation ID is
   refused with `workforce_login_generation`. Without 0093 the proposal fails
   with `workforce_hospital_establishment`.
3. **Routes.**
   - The pharmacy family proposes, approves, lists and reads history.
   - A hospital ID on the pharmacy family, a pharmacy ID on the hospital family
     and a foreign tenant all return 404.
   - A reader access row returns 404. A missing permission returns 403. Self
     review and review of one's own account return 403.
   - A replayed proposal returns the retained link. A conflicting replay and a
     stale `expected_version` return 409 `link_conflict`.
4. **Events.** Every pharmacy change writes one
   `pharmacy_login_link_changed.v1` row carrying `pharmacy_id` and no
   `hospital_id`, and no `login_link_changed.v1` row. Hospital changes are
   unchanged. Making a pharmacy Staff record inactive revokes its links and
   publishes the pharmacy subject.
5. **Race.** The admission race test adds the four hospital link handlers on a
   pharmacy ID. Each returns 404 and writes nothing. With `requireKind` removed,
   the proposal succeeds, which proves the check is the barrier.
6. **Discovery.** An active pharmacy link for the caller is absent from the
   candidate page. Without the filter it is present.
7. **SDK and UI.** Vitest covers:
   - the pharmacy path and a kind-distinct query key;
   - the pharmacy row action, shown only to a manager with a link permission;
   - proposal and decision against the pharmacy path;
   - the unchanged hospital panel.

## Not covered

- Everything in slice 3b-ii, listed in the scope above.
- Employment generations, `StaffActorUse` and the HR lifecycle on a pharmacy
  (slice 4), and branch postings (slice 5).
- An account-link inbox across Staff. Hospitals have none either.

## Gates

From `backend/`:

- `go build ./...` and `go vet ./...`;
- `make lint-go`;
- `go test -race ./internal/modules/workforce/... ./internal/archtest/...`;
- `./scripts/coverage-gate.sh`;
- `go run ./cmd/contractdocs -check`.

From the root:

- `node scripts/generate-workforce-login-links.mjs --check`;
- `pnpm turbo lint type-check test build format:check`.
