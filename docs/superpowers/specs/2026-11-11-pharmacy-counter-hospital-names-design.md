# Counter hospital pickers show hospital names — design

- **Issue:** #1637 (finding F-3 of the counter acceptance plan, cases PAT-01 and the admission cases)
- **Date:** 2026-11-11
- **Status:** proposed
- **Decision recorded on the issue:** the pharmacy keeps its own copy of the hospital name, updated by event, and shows it only through a verified affiliation.

## Problem

The counter's two hospital pickers list each verified hospital by its raw ID
(`apps/shell/components/pharmacy/counter-patient.tsx:151`,
`counter-admission.tsx:165`). A pharmacist picks from UUIDs.

The pharmacy has no hospital name to show. Its affiliation rows carry only
`hospital_id` (`pharmacy_hospital_affiliations`), and the hospital's
verification decision (`patientmaster` `pharmacy_affiliation_decided.v1`) carries
IDs, outcome and time, no name.

The name is owned by **workforce**: `workforce_hospitals.name`, written in two
places:

1. `PUT /workforce/hospitals/:hospital` (`workforce/handlers.go:209`): creates
   the hospital or renames it.
2. Customer workspace initialization (`workforce/customer_workspace.go:58`):
   creates the hospital at signup.

Workforce publishes no event for either, so no other module can learn a name.
The rows of `workforce_hospitals` with `establishment_kind` `pharmacy` or
`supplier` are not hospitals and are out of this design.

## Decisions

1. **Workforce publishes the name.** New event
   `helivanta.in.workforce.hospital_named.v1` with data
   `{hospital_id, name, name_version}`, tenant-scoped in the envelope. It
   carries the name and nothing else about the hospital. It is published for
   every hospital (`establishment_kind = 'hospital'`) when the hospital is
   created and on every change of its name; an unchanged rename publishes
   nothing new.

2. **The name has a version, kept by the database.** `workforce_hospitals`
   gains `name_version bigint NOT NULL` (1 on insert, +1 when `name` changes),
   set by a trigger so no write path can forget it. Consumers order by it, not
   by arrival or clock time.

3. **Publication is driven by the row, not by each write path** (my call).
   A new workforce table `workforce_hospital_name_announcements (tenant_id,
hospital_id, announced_version)`, forced RLS, keys the last version sent.
   It is a separate table, not a column on `workforce_hospitals`, because that
   table's audit trigger (`workforce_hospital_audit`) records every UPDATE
   with a staff actor: an announcement changes no hospital fact and must not
   appear in the hospital's audit history. A workforce announcer pass, on the
   same ticker shape as pharmacy's `RunAffiliationRedelivery`, finds hospitals
   whose `name_version` is greater than their announced version (or that have
   no announcement row), and in one tenant transaction per hospital locks the
   hospital row, publishes the event through the outbox with its current name
   and version, and upserts the announcement row. The announcer runs as the
   system actor `system:workforce-hospital-name-announcer`. Why this and not a
   `Publish` call in each handler:
   - Both existing write paths, and any added later, are covered without a
     developer remembering to publish.
   - Existing hospitals are announced by the same pass: they start with no
     announcement row. There is no separate backfill.
   - Publish and marker update commit together, so an event is never lost or
     announced twice by intent; the outbox's at-least-once delivery can still
     repeat it, which the consumer absorbs.

   Cost: the name reaches the pharmacy one pass after the write (seconds,
   not inside the business transaction as `docs/standards/backend.md`
   prefers). Until then the picker shows "Name unavailable" (decision 6),
   never a wrong name. The cross-tenant scan reads only `tenant_id, id`, as
   the redelivery pass does.

4. **The pharmacy stores every hospital name in its tenant** (decided with the
   user). New table `pharmacy_hospital_names (tenant_id, hospital_id, name,
name_version, updated_at)`, primary key `(tenant_id, hospital_id)`, with the
   forced-RLS boilerplate. Consumer `pharmacy-hospital-names` upserts and keeps
   a row only if the event's `name_version` is greater than the stored one, so
   a repeated or older event changes nothing. Storing all names, not only
   affiliated ones, means the name is already present when an affiliation is
   verified, whatever order the events arrive in. Nothing crosses a tenant.

5. **The name is disclosed only through a verified affiliation.** The
   affiliation list (`GET /pharmacy/pharmacies/:pharmacy/hospital-affiliations`)
   gains `hospital_name: string | null`. It is the stored name when the
   affiliation's current status is `verified`, and `null` otherwise
   (pending, requested, declined, revoked, withdrawn, or no stored name). A
   pharmacy that declares an affiliation to an arbitrary hospital ID learns
   nothing about whether that hospital exists or what it is called. No other
   route returns the stored names.

6. **The pickers show the name, the ID where it is needed, and say when the
   name is unavailable.** For each verified hospital the option label is:
   - the name, when no other verified hospital in the list has the same name;
   - `<name> · <hospital id>`, when two or more share it;
   - `Name unavailable · <hospital id>`, when `hospital_name` is null or
     absent (older server, name not yet announced).

   Never blank, never a guessed name. Both pickers use one shared labelling
   function.

   A hospital that patientmaster knows but workforce never named (an access
   grant in `patient_hospital_access` does not require a
   `workforce_hospitals` row) stays "Name unavailable · <id>" permanently.
   That is the true state, not a defect of this slice: signup and
   `PUT /workforce/hospitals/:hospital` are the ways a hospital is created and
   both name it, and the e2e fixtures already create the workforce hospital
   before verification (`e2e/tests/support/pharmacy-reports.ts:22`). Naming the
   hospital in workforce fixes it with no pharmacy change.

## Out of scope

- Any other hospital attribute (address, registration, logo).
- Names in other pharmacy screens; the affiliation management screen keeps
  showing IDs. They can adopt `hospital_name` later without contract changes.
- Pharmacy and supplier establishments in `workforce_hospitals`.
- Moving hospital identity to the `organization` registry. Its registration
  table has no name today; if it later becomes the owner, it publishes this
  event in workforce's place and consumers do not change.
- Hospital deletion: `workforce_hospitals` rows are not removed
  (`ON DELETE RESTRICT`, no-truncate trigger), so there is no removal event.

## Contracts and docs

- New event in `workforce/contract/events.go` and the integration contract
  docs; `go run ./cmd/contractdocs` regenerated and `-check` clean.
- New migrations: workforce (`name_version`, its trigger, the announcement
  table) and
  pharmacy (`pharmacy_hospital_names`), next free IDs at implementation time,
  re-checked against `codex/*` branches before merge.
- `PharmacyHospitalAffiliation` in `packages/api/src/pharmacy.ts` gains
  `hospital_name: string | null`; `docs/api/domains/pharmacy.md` documents the
  field and its disclosure rule; `docs/api/domains/workforce.md` documents the
  event.
- The acceptance plan: F-3 is marked done by the PR; PAT-01 and the admission
  cases expect hospital names in the pickers.

## Tests

- Workforce: creating and renaming a hospital bumps `name_version` (and an
  unchanged rename does not); a pass announces each unannounced hospital once
  with the current name; a rename between two passes announces only the
  newest; pharmacy and supplier rows are never announced; signup-created
  hospitals are announced; an announcement adds no `workforce_audit` row.
- Pharmacy consumer: stores a name; a repeated event is a no-op; an older
  `name_version` after a newer one leaves the newer name; events of another
  tenant are not visible.
- Pharmacy disclosure (isolation): `hospital_name` is set only on a verified
  affiliation; null for declared, requested, declined, revoked and withdrawn;
  null for a hospital ID with a stored name but no verified affiliation;
  another tenant's names never appear.
- Frontend (both pickers): name shown; two hospitals with the same name both
  show their ID; null and absent names read "Name unavailable · <id>".
- E2E: the counter's patient lookup picker shows the hospital's name after the
  affiliation is verified.
