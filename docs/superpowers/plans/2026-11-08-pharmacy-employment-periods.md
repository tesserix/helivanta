# Pharmacy employment periods

Status: implementation plan for slice 4b of the
[pharmacy HR lifecycle specification](../specs/2026-11-08-pharmacy-hr-lifecycle.md).

Refs #19. Implements part of
[ADR-0054](../../adr/0054-typed-employing-establishments.md) decision 3. The branch is
`feat/19-pharmacy-employment-periods`, cut from `main` after slice 4d merged.

## Scope

A pharmacy HR manager proposes employment terms for a pharmacy Staff record,
revises and withdraws them, and binds released employment evidence files to an
exact draft revision. Drafts do not become effective: reviews and generations
stay hospital-only until 4c.

## Actors and objects

| Actor or object           | Authority kind | Scope        | 404                                                              | 403                                                                                  | 503                                       |
| ------------------------- | -------------- | ------------ | ---------------------------------------------------------------- | ------------------------------------------------------------------------------------ | ----------------------------------------- |
| Pharmacy HR manager       | Administration | One pharmacy | No manager row, hospital, supplier, unknown or foreign-tenant ID | Missing `workforce.enrollment.manage`; on evidence also `workforce.documents.manage` | Registry, database or authorization down  |
| Pharmacy Workforce reader | Administration | One pharmacy | Always, as on a hospital                                         | —                                                                                    | —                                         |
| Pharmacy employment draft | Workforce      | Pharmacy     | Draft, revision or sponsor under another establishment           | —                                                                                    | —                                         |
| Pharmacy evidence binding | Workforce      | Pharmacy     | Binding or file under another establishment, or not released     | —                                                                                    | Lock timeout (`employment_evidence_busy`) |
| Hospital HR manager       | Administration | One hospital | A pharmacy ID, including one registered after admission          | Unchanged                                                                            | Unchanged                                 |

## Decisions settled by this plan

1. **Migration `0099_workforce_pharmacy_employment_periods`**
   (`pharmacy_employment_periods.sql`) recreates the
   `workforce_hospital_establishment` triggers on
   `workforce_employment_period_drafts` and
   `workforce_employment_evidence_bindings`, with the same names, on
   `workforce_require_enrollment_establishment()` (0095). No RLS change: both
   policies already rest on `workforce_enrollment_allowed`. The two history
   tables are reached through their parents' foreign keys.
2. **Inventory.** `pharmacyAdmittingTables` gains the two parents and their
   two history tables; the enrollment guard list gains the two parents. Review,
   cohort, contributor, review evidence and generation tables keep the
   hospital guard, and the test proves each refuses a pharmacy.
3. **One core, one target.** `employmentTarget{p, owner establishment, staff,
period, authority}` replaces the loose `(p, hospital, staff, id)` tuple.
   `employmentTarget.transaction(required, fn)` runs the family authority,
   then `owner.requireKind`, then `workforce_lock_supervision`, then the Staff
   share lock. Every period and evidence handler becomes a core taking a target,
   wrapped by a hospital handler and a pharmacy handler.
4. **Authorities.** `employmentAuthority func(required []authz.Permission, fn
func(*gorm.DB) error) error`. The hospital authority is `intakeTransaction`,
   unchanged. The pharmacy authority generalises 4a's
   `pharmacyEnrollmentAuthority` to the permissions it is given: refresh the
   session, refresh permissions, require every permission (403 otherwise),
   then `h.transaction` under `enrollmentScope`. 4a passes
   `PermEnrollmentManage`, so its behaviour is unchanged.
5. **Request shape.** `pharmacyEmploymentScope` runs `pharmacyStaffScope`
   (400 on `hr_intake`), `enrollmentSession` (409 `session_changed`), parses
   Staff and period IDs, and for reads requires `lifecycle=withdrawal-v1`
   exactly once (400 otherwise). It sets `Cache-Control: no-store`.
6. **Evidence download.** `pharmacyEmploymentEvidenceGate` embeds the hospital
   gate's binding check and download action, but `authorize` calls
   `staffDocumentOwner` with the pharmacy kind and `view` returns
   `pharmacyDocumentView`. It is not an intake gate. The hospital
   `employmentEvidenceGate.authorize` moves onto `staffDocumentOwner` with the
   hospital kind.
7. **Views.** `scripts/generate-workforce-employment-periods.mjs` emits, from
   the canonical contract, `pharmacyEmploymentPeriodSource`,
   `pharmacyEmploymentPeriodDraft`, `pharmacyEmploymentEvidenceBinding`, their
   view functions and strict Zod schemas carrying `pharmacy_id` and no
   `hospital_id`. Handlers never hand-build a pharmacy response.
8. **Routes** (module.go, pharmacy group, after the document routes): the
   permission on the route, then the manager row in the command.

   | Method and path under `/:pharmacy/staff/:id`                 | Permission                                               |
   | ------------------------------------------------------------ | -------------------------------------------------------- |
   | `GET employment-source`                                      | `PermEnrollmentManage`                                   |
   | `GET employment-periods` (list)                              | `PermEnrollmentManage`                                   |
   | `PUT`, `GET employment-periods/:period`                      | `PermEnrollmentManage`                                   |
   | `POST employment-periods/:period/withdraw`                   | `PermEnrollmentManage`                                   |
   | `GET employment-periods/:period/history` (list)              | `PermEnrollmentManage`                                   |
   | evidence `PUT`, `GET`, `POST withdraw`, lists, `GET content` | `PermDocumentManage`, then enrollment inside the command |

9. **SDK.** `EmploymentPeriodScope` becomes a union naming exactly one of
   `hospitalId` and `pharmacyId`; a pharmacy scope cannot carry `hrIntake`.
   Paths, query keys and receipt checks follow the family. Receipts parse with
   the family's strict schema. `EmploymentEvidence*` follows the same scope.
10. **Shell.** The pharmacy Staff source gains an Employment action, gated by
    `workforce.enrollment.manage`, that opens the shared terms panel with a
    pharmacy scope and the pharmacy Staff directory as sponsor source. The
    evidence panel offers review preparation only for a hospital HR intake
    scope and a receipt carrying `hospital_id`, so a pharmacy never sees it.

## Tasks (each red before green)

1. **Guard inventory.** Extend `establishment_kind_test.go` with the four
   tables and a direct insert of a pharmacy and a supplier draft. Red: the
   pharmacy insert is refused by the 0092 trigger. Green: 0099.
2. **Pharmacy terms lifecycle.** `TestPharmacyEmploymentTermsLifecycle`:
   source, version 1, version 2, replay, list and read with the lifecycle
   view, history, withdraw; `pharmacy_id` in every body, no `hospital_id`;
   audit rows under the pharmacy. Red: 404 route not found.
3. **Pharmacy evidence lifecycle.** Upload an employment evidence file through
   the 4d routes, bind, replay, list, read, history, download (disclosure audit
   before bytes), withdraw.
4. **Refusal matrix.** No row, reader row, hospital ID, supplier ID, foreign
   tenant (404); missing enrollment or documents permission (403);
   `hr_intake=v1` and missing lifecycle (400); changed expected subject (409);
   hospital Staff, period, binding or sponsor under the pharmacy family (404).
5. **Kind race.** A hospital period command and a hospital evidence download
   on an ID registered as a pharmacy after admission answer 404 and write no
   draft, binding or audit row. Red: after 0099 without the core check, the
   draft is written.
6. **Hospital suites unchanged.** Period, withdrawal, evidence, review and
   activation suites pass with the refactor.
7. **Generator and SDK.** `--check` fails until the generated views exist;
   SDK tests for the pharmacy paths, keys and receipt rejection of a
   `hospital_id` body.
8. **Shell.** `renderWithProviders` tests: loading, 404 clears the source,
   409 keeps the editor, save success, no "Prepare employment review".
9. **Contracts.** `go run ./cmd/contractdocs`, re-pin the 18 `module.go` pins,
   regenerate the access inventory, update `docs/api/workforce.md`.

## Gates

`make lint-go`, `./scripts/coverage-gate.sh`, `go build ./...`,
`go vet ./...`, `go run ./cmd/contractdocs -check`, the workforce generators
with `--check`, `node scripts/staff-operational-access-inventory.mjs --check`,
`pnpm turbo lint type-check test build format:check` and `make lint-shell`.
