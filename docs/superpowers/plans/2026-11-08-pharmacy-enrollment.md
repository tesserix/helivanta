# Pharmacy Staff enrollment

Status: implementation plan for slice 4a of the
[pharmacy HR lifecycle specification](../specs/2026-11-08-pharmacy-hr-lifecycle.md).

Refs #19. Implements part of
[ADR-0054](../../adr/0054-typed-employing-establishments.md) decision 3. The branch is
`feat/19-pharmacy-enrollment`, based on `341f67f7`.

## Scope

A pharmacy HR manager reads and edits the enrollment of a pharmacy Staff
record: profile, addresses, emergency contacts, recorded credentials,
qualifications, experience and the reporting line. The six
`workforce_enrollment_*` tables admit a pharmacy establishment. Every other
Staff child table keeps refusing it.

## Actors and objects

| Actor or object           | Authority kind | Scope                  | 404                                                                       | 403                                   | 503                                         |
| ------------------------- | -------------- | ---------------------- | ------------------------------------------------------------------------- | ------------------------------------- | ------------------------------------------- |
| Pharmacy HR manager       | Administration | One pharmacy           | No manager access row, a hospital, supplier, unknown or foreign-tenant ID | Missing `workforce.enrollment.manage` | Registry, database or supervision lock down |
| Pharmacy Workforce reader | Administration | One pharmacy           | Always, as on a hospital                                                  | —                                     | —                                           |
| Pharmacy enrollment       | Workforce      | Pharmacy establishment | Staff record outside the caller's access or another establishment         | —                                     | Database down                               |
| Hospital HR manager       | Administration | One hospital           | A pharmacy ID registered after admission                                  | Unchanged                             | Unchanged                                   |

## Decisions settled by this plan

1. **Migration `0095_workforce_pharmacy_enrollment`**
   (`pharmacy_enrollment.sql`):
   - creates `workforce_require_enrollment_establishment()`. It admits a
     registered `hospital` or `pharmacy` and raises
     `23503 workforce_hospital_establishment` otherwise, so the existing
     error mapping holds;
   - drops the `workforce_hospital_establishment` trigger from the six
     enrollment tables and recreates it, with the same name, on the new
     function;
   - changes no RLS. The enrollment policies already match
     `app.workforce_hospital`, and only a manager row sets
     `app.workforce_enrollment`.
2. **The inventory pins the new set.** `pharmacyAdmittingTables` gains the six
   enrollment tables. `workforce_credential_validity` stays covered, because
   its second required key reaches the still-guarded
   `workforce_credential_reviews`. The probe tables that stood for "reached
   through a guarded table" referenced `workforce_enrollment_profile`, which
   is no longer hospital-only. They move to `workforce_staff_identifiers`. A
   new probe child of `workforce_enrollment_profile` must be reported, which
   proves a table reached only through a pharmacy-admitting table fails CI.
3. **One enrollment core, two authorities.** `getEnrollment` and
   `putEnrollment` become thin hospital wrappers over
   `readEnrollmentFor` and `writeEnrollmentFor`. Each takes an
   `enrollmentRequest` (principal, `establishment`, Staff ID, projections)
   and an authority function that opens the transaction:
   - hospitals keep `intakeTransaction`, so the legacy and delegated HR
     intake paths are unchanged;
   - pharmacies use `pharmacyEnrollmentTransaction`. It refreshes the
     session and permissions, answers 403 without
     `workforce.enrollment.manage`, then runs `enrollmentScope` inside the
     transaction. There is no scoped IAM fallback, because no pharmacy HR
     intake template exists.

   Inside the transaction both run `e.requireKind` first. A hospital route
   on a pharmacy registered after admission is therefore 404 and writes
   nothing. Before 0095 the enrollment triggers refused that write; after it
   the core does.

4. **Pharmacy request shape.** `pharmacyStaffScope` refuses `hr_intake` with
   400, then checks the expected subject. Any `profile_masters`,
   `qualification_masters` or `contact_masters` query is 400.
   `request_context=v1` is required (400 otherwise), and the PUT body must
   carry `request_context_version: "v1"`. Both expected headers must appear
   exactly once; a missing, repeated or different one is 409
   `session_changed`.
5. **Masters.** No code is added. The resolver looks a supplied revision ID
   up under the pharmacy ID, where `workforce_hr_masters` can never hold a
   row because its trigger stays hospital-only. A supplied ID is therefore
   404, and a free-text name for a selection-only field is 400, as on a
   hospital with no masters.
6. **Event and audit.** The core publishes `enrollment_changed.v1` only for
   the hospital kind. Audit rows are written under the establishment ID in
   both families. An `inactive` status revokes login links through
   `revokeInactiveStaffLinks`, which already publishes the independent
   login-link event for a pharmacy.
7. **Response.** The pharmacy view is the hospital `request_context=v1`
   document without master projections. Its `staff` is `PharmacyStaff`, with
   `pharmacy_id` and no `hospital_id`.
8. **Routes.** Under the pharmacy establishment router, both declared with
   `workforce.enrollment.manage`:
   - `GET /:pharmacy/staff/:id/enrollment`;
   - `PUT /:pharmacy/staff/:id/enrollment`.
9. **SDK.** `packages/api/src/pharmacy-enrollment.ts` adds
   `getPharmacyEnrollment` and `savePharmacyEnrollment`, with a response
   parser that requires `request_context_version`, refuses a `hospital_id`
   key and a mismatched `pharmacy_id` or Staff ID, and refuses any master
   projection marker. `usePharmacyEnrollment` reads with `no-store`,
   `gcTime: 0` and no retry, keyed by account, pharmacy and Staff ID.
10. **UI.** The hospital `EnrollmentEditor` sections move behind an
    `EnrollmentForm` that takes the loaded enrollment, the supervisor
    choices, optional master choices and a save function. The hospital
    editor keeps its queries. `PharmacyEnrollmentEditor` passes no masters,
    so every master field falls back to text and contract type, which is
    selection-only, is not offered. `usePharmacySupervisorChoices` pages the
    validated pharmacy Staff directory through `usePagedQuery`, excluding the
    record itself. `staff-source.tsx` shows "Enrollment" on a record when
    `can("workforce.enrollment.manage")` and the access level is `manager`.
    A 401, 403 or 404 clears the whole Staff source with "Your account or
    Staff access changed", as the directory and record editor already do; a
    409 keeps the draft and offers a confirmed reload.

## Numbers and failure behaviour

Planning numbers are inherited and unmeasured: at most 50,000 Staff per
establishment, human-paced edits, one transaction per request on
composite-key reads. The pharmacy path adds one registry primary-key read.

- Registry down: admission answers 503.
- Database down: 503, nothing written.
- Supervision lock busy: 503 `supervision_busy`. The lock is keyed by
  establishment, so pharmacy and hospital edits never share it.
- Audit write fails: the transaction rolls back.

## Test-first order

Each assertion is run and shown failing before the code that satisfies it.

1. **Guard.** In `TestEstablishmentTablesRefuseAPharmacyEstablishment` the
   enrollment profile moves from the refused set to an admitted one, and a
   supplier enrollment profile is refused with
   `23503 workforce_hospital_establishment`. Staff identifiers, leave routing
   and HR masters still refuse a pharmacy.
2. **Inventory.** The six tables join `pharmacyAdmittingTables`; the moved
   probes stay covered and the enrollment-child probe is reported.
3. **Pharmacy routes** (`pharmacy_enrollment_routes_test.go`):
   - a manager creates, reads, replays and updates an enrollment, and a stale
     version is 409;
   - a reader, no access row, a hospital ID, a supplier ID and a foreign
     tenant are 404 on both routes; no permission is 403;
   - `hr_intake=v1`, each master view and a missing `request_context` are
     400; a changed expected subject is 409;
   - a master revision ID is 404, a hospital Staff supervisor is 404, a
     cycle is 409 `supervisor_cycle`;
   - no `enrollment_changed` event is written, audit rows carry the pharmacy
     ID, and `inactive` revokes the person's login links;
   - a person with records at a hospital and a pharmacy keeps two
     enrollments, and neither family returns the other's.
4. **Race.** An internal test runs the core for a hospital `establishment`
   naming a registered pharmacy. Read and write answer
   `gorm.ErrRecordNotFound` and write no row.
5. **Hospital regression.** The existing enrollment, context, masters and
   intake suites pass unchanged.
6. **SDK and UI.** Vitest covers the parser refusals, the hook key and
   options, the pharmacy editor's loading, 404, 409 and save states, and
   the Enrollment action's gating on permission and manager level.

## Not covered

- Pharmacy HR masters and delegated pharmacy HR intake.
- Sub-slices 4b–4f and slice 5.
- Deployed runtime and real OIDC browser acceptance.

## Gates

From `backend/`:

- `go build ./...` and `go vet ./...`;
- `make lint-go`;
- `go test -race ./internal/modules/workforce/...`;
- `./scripts/coverage-gate.sh`;
- `go run ./cmd/contractdocs -check`.

From the root:

- `node scripts/staff-operational-access-inventory.mjs --check`;
- `pnpm turbo lint type-check test build format:check`.
