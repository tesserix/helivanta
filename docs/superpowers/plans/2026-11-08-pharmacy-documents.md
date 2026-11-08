# Pharmacy Staff documents

Status: implementation plan for slice 4d of the
[pharmacy HR lifecycle specification](../specs/2026-11-08-pharmacy-hr-lifecycle.md).

Refs #19. Implements part of
[ADR-0054](../../adr/0054-typed-employing-establishments.md) decision 3. The branch is
`feat/19-pharmacy-documents`, stacked on the 4a branch until #1631 merges.

## Scope

A pharmacy HR manager uploads, lists, downloads, retries and revokes the HR
files of a pharmacy Staff record: identity and employment evidence on the
enrollment, and evidence on its recorded credentials and qualifications.
`workforce_documents` and `workforce_document_contributors` admit a pharmacy
establishment for those three record kinds only. Classification, identity
review, employment evidence and credential review stay hospital-only.

## Actors and objects

| Actor or object           | Authority kind | Scope                  | 404                                                                       | 403                                  | 503                                               |
| ------------------------- | -------------- | ---------------------- | ------------------------------------------------------------------------- | ------------------------------------ | ------------------------------------------------- |
| Pharmacy HR manager       | Administration | One pharmacy           | No manager access row, a hospital, supplier, unknown or foreign-tenant ID | Missing `workforce.documents.manage` | Registry, database, storage or authorization down |
| Pharmacy Workforce reader | Administration | One pharmacy           | Always, as on a hospital                                                  | —                                    | —                                                 |
| Pharmacy Staff file       | Workforce      | Pharmacy establishment | File, record or Staff record under another establishment                  | —                                    | Storage not configured (`documents_unavailable`)  |
| Hospital HR manager       | Administration | One hospital           | A pharmacy ID, including one registered after admission                   | Unchanged                            | Unchanged                                         |

## Decisions settled by this plan

1. **Migration `0096_workforce_pharmacy_documents`** (`pharmacy_documents.sql`):
   - creates `workforce_require_document_establishment()`. It admits a
     registered `hospital` for every record kind, and a registered `pharmacy`
     only for `enrollment`, `credential` and `qualification`. Anything else
     raises `23503 workforce_hospital_establishment`, so the existing error
     mapping holds;
   - recreates the `workforce_hospital_establishment` trigger on
     `workforce_documents`, with the same name, on that function;
   - recreates the trigger on `workforce_document_contributors` on
     `workforce_require_enrollment_establishment()` (0095). Contributor rows
     are written only for enrollment identity and employment evidence;
   - changes no RLS. `document_isolation` already ends in
     `workforce_enrollment_allowed`, which only a manager row sets, and the
     worker paths key on `app.workforce_hospital`.
2. **Children stay hospital-only without new triggers.** Classification
   revisions also reference `workforce_hr_master_revisions`, and
   `workforce_hr_masters` refuses a pharmacy. Person review evidence
   references `workforce_person_reviews`, which refuses one. Employment
   evidence bindings and review evidence keep their own hospital trigger. The
   inventory test proves each of these.
3. **The inventory pins the new set.** `pharmacyAdmittingTables` gains the two
   document tables. A table reached only through them must be reported.
4. **Gate per establishment kind.** `staffDocuments.authorize` runs
   `establishment{hospital}.requireKind` before `documentOwner`. A new
   `pharmacyStaffDocuments` embeds `staffDocuments` and overrides `authorize`
   to require the `pharmacy` kind. It is a distinct type, so
   `intakeDocumentGate` refuses it and every pharmacy transaction is the
   plain one. A hospital route on an ID registered as a pharmacy after
   admission is therefore 404 and writes nothing.
5. **Gate-generic core.** Each hospital handler becomes a wrapper over a core
   that takes the gate and establishment ID: `registerDocument`,
   `receiveDocument`, `sendDocument`, `requeueDocument` (existing), and new
   `revokeFile`, `listFiles` and `recordChoices`. `documentGate` gains
   `view(document) any`. Every existing gate returns the row, and the pharmacy
   gate returns `pharmacyDocumentView(row)`.
6. **Defect.** `requeueDocument` opens its transaction with the caller's gate
   instead of `staffDocuments{}`. Before the fix, a benefits evidence retry
   carrying `?hr_intake=v1` ran in the HR intake transaction.
7. **Pharmacy request shape.** `pharmacyDocumentScope` runs
   `pharmacyStaffScope` (400 for `hr_intake`), then `enrollmentSession` (409
   `session_changed` unless both expected headers appear once), then parses
   the Staff ID. Classification routes are not registered.
8. **Generated view.** `scripts/generate-workforce-documents.mjs` emits
   `pharmacyDocument` (visible fields with `pharmacy_id` for `hospital_id`)
   and `pharmacyDocumentView`, and the strict TypeScript
   `pharmacyStaffDocumentSchema`. A new contract field reaches both views.
9. **Events, storage and audit.** Scan and state events keep `hospital_id`,
   which names the owning establishment row. Object keys keep
   `t/<tenant>/h/<establishment>/…`. Download audit rows are written under
   the pharmacy ID. The Go contract comment and `docs/api/workforce.md` say
   so.
10. **Routes.** Under the pharmacy establishment router, each with
    `workforce.documents.manage`:
    - `GET /:pharmacy/staff/:id/document-records`;
    - list `GET /:pharmacy/staff/:id/documents`;
    - `PUT /:pharmacy/staff/:id/documents/:file`;
    - `PUT` and `GET /:pharmacy/staff/:id/documents/:file/content`;
    - `PUT /:pharmacy/staff/:id/documents/:file/revocation`;
    - `PUT /:pharmacy/staff/:id/documents/:file/retries/:attempt`.
11. **SDK.** `DocumentScope` names exactly one of `hospitalId` and
    `pharmacyId`. A pharmacy scope cannot carry `hrIntake`. `documentPath`
    picks the family, receipts parse with the family's schema and check
    `pharmacy_id`, and the query key carries the establishment kind.
12. **UI.** `DocumentWorkspace` is exported and offers classification only for
    a hospital scope. `PharmacyStaffDocumentPanel` renders it for a pharmacy.
    `staff-source.tsx` shows "Documents" on a record when
    `can("workforce.documents.manage")` and the access level is `manager`. A
    401, 403 or 404 clears the Staff source with "Your account or Staff access
    changed", as 4a does.

## Numbers and failure behaviour

Inherited planning numbers: human-paced uploads of PNG or JPEG up to 10 MiB,
cursor-bounded lists, at most 50,000 Staff per establishment. The pharmacy
path adds one registry primary-key read per phase.

- Registry down: admission answers 503.
- Storage not configured: 503 `documents_unavailable`, no intent written.
- Scanner down: files stay `scan_pending` and become retryable after 45 minutes.
- Audit write fails: the download sends no byte.

## Test-first order

Each assertion runs and fails before the code that satisfies it.

1. **Guard.** In the establishment test a pharmacy enrollment, credential and
   qualification document is admitted. A pharmacy `tax_declaration` or
   `benefit_proposal` document and any supplier document are refused with
   `23503 workforce_hospital_establishment`. A pharmacy classification
   revision and person review stay impossible.
2. **Inventory.** The two tables join `pharmacyAdmittingTables`, and a probe
   child of `workforce_documents` is reported.
3. **Pharmacy routes** (`pharmacy_documents_routes_test.go`):
   - a manager reads record choices, registers, replays, uploads, sees one
     scan request whose `hospital_id` is the pharmacy ID, downloads after the
     scan and revokes. Each response carries `pharmacy_id` and no
     `hospital_id`, and the download audit row carries the pharmacy ID;
   - a reader, no access row, a hospital ID, a supplier ID and a foreign
     tenant are 404 on every route; no permission is 403; `hr_intake=v1` is
     400; a missing or changed expected subject is 409;
   - a hospital Staff, credential or file ID under the pharmacy family is
     404, and the reverse holds;
   - no storage is 503 `documents_unavailable`.
4. **Race.** An internal test runs the hospital gate on a registered pharmacy
   ID and gets `gorm.ErrRecordNotFound`.
5. **Defect.** A benefits evidence retry with `?hr_intake=v1` answers as
   without it.
6. **Hospital regression.** The document, classification, evidence and intake
   suites pass unchanged.
7. **SDK and UI.** Vitest covers the pharmacy path and receipt refusals, the
   key, the panel's loading, 404, 409 and success states, the hidden
   classification action and the Documents action's gating.

## Not covered

- File classification, identity review, employment evidence and credential
  review for a pharmacy.
- Sub-slices 4b, 4c, 4e, 4f and slice 5.
- Deployed runtime and real OIDC browser acceptance.

## Gates

From `backend/`:

- `go build ./...` and `go vet ./...`;
- `make lint-go`;
- `go test -race ./internal/modules/workforce/...`;
- `./scripts/coverage-gate.sh`;
- `go run ./cmd/contractdocs -check`.

From the root:

- `node scripts/generate-workforce-documents.mjs --check`;
- `node scripts/staff-operational-access-inventory.mjs --check`;
- `pnpm turbo lint type-check test build format:check`.
