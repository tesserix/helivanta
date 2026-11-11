# HR lifecycle on a pharmacy employer (#19 slice 4)

Issue: #19.
Decisions consumed: ADR-0049, ADR-0054 and ADR-0063.
Parent: slice 4 of the
[standalone pharmacy employer specification](2026-11-04-standalone-pharmacy-employers.md).

## Problem

A registered standalone pharmacy can keep a Staff directory (slice 3a). It
cannot hold anything an HR department records about those people: no
enrollment profile, no employment period, no credential, no document and no
leave. Migration 0092 refuses a pharmacy row in every Staff child table, so
the database enforces the gap.

## Decomposition

The parent specification mounts each HR surface only after its own handler
review. One PR cannot review them all correctly, so slice 4 is split. Each
sub-slice is a vertical PR with its own migration, routes, SDK, UI, tests,
contractdocs and inventory, and its own approved rollout.

| Sub-slice | Surface                                                                                                                        | Tables admitted to a pharmacy                               | Depends on |
| --------- | ------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------- | ---------- |
| **4a**    | Enrollment: profile, addresses, emergency contacts, credentials as recorded, qualifications, experience and the reporting line | The six `workforce_enrollment_*` tables                     | 3a         |
| 4b        | Employment periods: drafts, evidence bindings and history                                                                      | Period drafts, evidence bindings                            | 4a, 4d     |
| 4c        | Employment reviews and generations, which make a period effective (ADR-0049)                                                   | Reviews, cohorts, contributors, evidence, generations       | 4b         |
| 4d        | Staff documents: record choices, upload, scan, download, retry and revocation                                                  | Documents, document contributors                            | 4a         |
| 4e        | Credential reviews, validity and reminders                                                                                     | Credential reviews, reminder policies                       | 4a, 4d     |
| 4f        | Leave: setup, assignments, ledger, requests and reporting-line routing                                                         | Leave configuration, routing, ledger, requests, route steps | 4a, 4c     |

Payroll, duty rosters, attendance, overtime, benefits and tax stay unmounted,
and their tables keep refusing a pharmacy. Each needs a separate review.

The lifecycle tests in the parent specification are met once 4c and 4f have
both shipped. Those tests are: exit, suspension and credential expiry end
dependent authority on the effective date. A person employed by a hospital
and a pharmacy keeps two records, and neither grants access to the other.

## 4a — Pharmacy Staff enrollment

### Actors and authority

| Actor                     | Authority kind | Scope                      | Preconditions                                                                                                                                            |
| ------------------------- | -------------- | -------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Pharmacy HR manager       | Administration | One pharmacy establishment | Tenant permission `workforce.enrollment.manage`. A `manager` row in `workforce_hospital_access` for the pharmacy. Both are read again inside the command |
| Pharmacy Workforce reader | Administration | One pharmacy               | None. A reader cannot read enrollment, exactly as on a hospital                                                                                          |

Pharma membership, a pharmacy outlet grant and the PharmaConnect pharmacist
role confer nothing here.

The hospital family also has a delegated HR intake path (`?hr_intake=v1`),
admitted through hospital-rooted IAM scopes. It stays hospital-only. ADR-0063
roots IAM grants by establishment kind, and no pharmacy HR-intake template
exists. The pharmacy family answers that query with 400, as slice 3a does for
the directory.

### Routes

| Route                                                         | Permission                    | Effect                                                               |
| ------------------------------------------------------------- | ----------------------------- | -------------------------------------------------------------------- |
| `GET /v1/workforce/pharmacies/:pharmacy/staff/:id/enrollment` | `workforce.enrollment.manage` | Read the enrollment of one pharmacy Staff record, and audit the read |
| `PUT /v1/workforce/pharmacies/:pharmacy/staff/:id/enrollment` | `workforce.enrollment.manage` | Create or replace it under `expected_version`                        |

Both routes run through the existing establishment admission. A hospital ID
on the pharmacy family is 404, and so is a pharmacy ID on the hospital family.

- **Shared core.** The hospital and pharmacy handlers call one enrollment core
  with an `establishment` argument, the pattern slice 3a used for the
  directory. Validation, replay, version checks, supervision locks,
  credential-review constraints and the audit row are therefore identical.
- **Masters.** A pharmacy has no HR masters. Profile, qualification and
  contact master IDs must be empty, and a selection-only field must not carry
  a free-text name. A supplied master ID is 404, because no master exists for
  that establishment. The `*_masters` query views are refused with 400.
- **Reporting line.** `supervisor_id` names Staff of the same pharmacy. The
  existing composite foreign key, acyclic trigger and supervision lock
  enforce this. A hospital Staff ID is 404.
- **Credentials.** Credentials are recorded but unreviewed. Credential reviews
  stay hospital-only until 4e, so `workforce_credential_reviewed` can never
  be satisfied for a pharmacy row. Recording a credential confers no
  professional authority, which stays in Pharma.
- **Request context.** Both routes require `?request_context=v1`, and the PUT
  body carries `request_context_version: "v1"`. `X-Helivanta-Expected-Tenant`
  and `X-Helivanta-Expected-Subject` must each appear once and equal the
  verified principal; otherwise the answer is 409 `session_changed`. The
  pharmacy family has no unversioned client, as in slice 3a.
- **Employment status.** The PUT may set `active` or `inactive`. This is the
  same direct status the slice 3a directory PUT already allows. It
  remains a legacy status, not a reviewed employment generation (4c).
  `inactive` revokes the person's login links, as on a hospital.
- **Response.** The same document as the hospital view. `staff` is the
  pharmacy Staff shape, with `pharmacy_id` and no `hospital_id`. Master names
  are never present.
- **Event.** None. The hospital `enrollment_changed.v1` payload carries
  `hospital_id`, and it has no consumer. Publishing a pharmacy ID in that
  field would misstate its meaning. The pharmacy directory write in slice 3a
  publishes nothing either. A `pharmacy_id` event version is added only when
  a consumer needs one.
- **Audit.** The `workforce.enrollment.read` and `workforce.enrollment.changed`
  rows are written under the pharmacy ID, in the same transaction as the read
  or write.

### Status codes

| Case                                                                              | Status                 |
| --------------------------------------------------------------------------------- | ---------------------- |
| Unknown, foreign-tenant, hospital or supplier establishment                       | 404                    |
| No `manager` access row, or the access row is removed before commit               | 404                    |
| Staff record missing on GET, or present under another establishment               | 404                    |
| Missing `workforce.enrollment.manage`                                             | 403                    |
| `hr_intake` or a `*_masters` view requested, or `request_context=v1` missing      | 400                    |
| Expected tenant or subject header missing, repeated or different from the session | 409 `session_changed`  |
| Master ID supplied                                                                | 404                    |
| Invalid fields or a free-text name for a selection-only field                     | 400                    |
| `expected_version` stale                                                          | 409                    |
| Supervisor cycle                                                                  | 409 `supervisor_cycle` |
| Registry, database or supervision lock unavailable                                | 503                    |

### Data and enforcement

Migration `0095_workforce_pharmacy_enrollment` (append-only):

- A new guard function, `workforce_require_enrollment_establishment()`. It
  admits a registered `hospital` or `pharmacy` establishment. It refuses a
  supplier, an unregistered ID and any other kind with the existing
  `workforce_hospital_establishment` constraint name, so the error mapping is
  unchanged.
- The six `workforce_enrollment_*` triggers move to that function. Every other
  Staff child table keeps the hospital-only guard.
- The establishment inventory test names these six tables as reviewed for a
  pharmacy. A seventh table that starts admitting a pharmacy fails it.

The enrollment RLS policies are already establishment-neutral. They require
`app.workforce_enrollment='authorized'`, which only a manager access row sets,
and they match `app.workforce_hospital` to the row.

### UI

- The pharmacy Staff screen (`/pharmacy/staff`) gains an "Enrollment" action
  on each record, shown when the caller holds `workforce.enrollment.manage`
  and the manager level. The API remains the boundary.
- The pharmacy editor reuses the hospital enrollment sections, without master
  pickers or HR-intake scope. Its supervisor picker reads the pharmacy Staff
  directory.
- A 401, 403 or 404 ends the editor and clears the whole Staff source with
  "Your account or Staff access changed", with no retry, as the pharmacy
  directory already does. A 409 keeps the draft and offers a confirmed reload.

### Not covered by 4a

- Pharmacy HR masters (designation, grade, qualification and contact
  masters). Free-text values remain available where the field allows them.
- Delegated HR intake for pharmacies, which needs a pharmacy-rooted IAM
  template.
- Sub-slices 4b–4f, and slice 5.
- Deployed runtime acceptance and real OIDC browser acceptance.

### Numbers and failure behaviour

- **Load.** Enrollment edits are human-paced. A pharmacy holds at most 50,000
  Staff (parent specification). Each request is one transaction on
  composite-key reads, as on a hospital.
- **Failure.** When Postgres or the registry is unavailable, both families
  answer 503. Pharmacy and hospital requests share no lock, because the
  supervision lock is keyed by establishment.
- **Rollback.** Revert the code, then restore the six triggers to the
  hospital-only function. The second step has to be a new forward migration,
  and only if a pharmacy enrollment row must stop being writable. Existing
  rows stay readable.
- **Rejected alternative.** A separate `workforce_pharmacy_enrollment_*` table
  set. It would duplicate the profile master, the validation and the
  supervision triggers for one new establishment kind.

### Acceptance tests (red first)

1. A pharmacy manager with `workforce.enrollment.manage` creates, reads,
   replays and updates an enrollment. A stale version is 409.
2. Without an access row, with a reader row, on a hospital ID, on a supplier
   ID and on a foreign tenant, both routes answer 404. Without the permission
   they answer 403.
3. `hr_intake=v1`, each `*_masters` view and a missing `request_context=v1`
   answer 400. A changed expected subject answers 409. A master ID answers 404. A hospital Staff supervisor answers 404. A cycle answers 409.
4. A direct SQL insert of a pharmacy row into a non-enrollment Staff child
   table is still refused, and a supplier row in an enrollment table is
   refused.
5. The hospital enrollment routes keep their behaviour, through the existing
   enrollment suites.
6. A person with Staff records at a hospital and at a pharmacy has two
   enrollments. Neither family's GET returns the other's record.
7. SDK schema and hook tests, and an editor panel test using
   `renderWithProviders`, for loading, 404, 409 and success.

## 4d — Pharmacy Staff documents

### Actors and authority

| Actor                     | Authority kind | Scope                      | Preconditions                                                                                                                                      |
| ------------------------- | -------------- | -------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| Pharmacy HR manager       | Administration | One pharmacy establishment | Tenant permission `workforce.documents.manage`. A `manager` row in `workforce_hospital_access` for the pharmacy. Both are read again in each phase |
| Pharmacy Workforce reader | Administration | One pharmacy               | None. A reader cannot list, upload or download Staff files, exactly as on a hospital                                                               |

The files are HR evidence about a person: identity, employment, credential and
qualification evidence. Pharma membership, an outlet grant and the
PharmaConnect pharmacist role confer nothing. Delegated HR intake stays
hospital-only, as in 4a, so `hr_intake` answers 400.

### Routes

All routes sit under `/v1/workforce/pharmacies/:pharmacy/staff/:id` and
require `workforce.documents.manage` plus the pharmacy manager row.

| Route                                  | Effect                                                                                          |
| -------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `GET document-records`                 | Enrollment, plus up to 20 credentials and 20 qualifications, each with its permitted categories |
| `GET documents`                        | Cursor list of the Staff record's files                                                         |
| `PUT documents/:file`                  | Register an upload intent; an identical replay returns it, a different one is 409               |
| `PUT documents/:file/content`          | Upload the bytes, quarantine them and queue the scan                                            |
| `GET documents/:file/content`          | Download released bytes, auditing the read before any byte leaves                               |
| `PUT documents/:file/revocation`       | Revoke an available file                                                                        |
| `PUT documents/:file/retries/:attempt` | Start the next scan attempt after an error or the 45-minute processing deadline                 |

- **Shared core.** The hospital and pharmacy handlers call the existing
  gate-generic document core. Validation, intent replay, size and digest
  checks, content inspection, quarantine, scan, release, retry, revocation
  and download headers are therefore identical. The core's list, revocation
  and record-choice paths become gate-generic too.
- **Gate.** A pharmacy document gate authorizes each phase with
  `enrollmentScope` (the manager row, read under share lock), requires the
  pharmacy establishment, and locks the Staff row. It never consults HR
  intake. Its files bind only to `enrollment`, `credential` and
  `qualification` records of the same pharmacy Staff record.
- **Account assertions.** `X-Helivanta-Expected-Tenant` and
  `X-Helivanta-Expected-Subject` must each appear once and equal the verified
  principal; otherwise 409 `session_changed`. The pharmacy family has no
  headerless client.
- **Response.** The same document view as the hospital family, with
  `pharmacy_id` in place of `hospital_id`. Record choices are unchanged.
- **Events.** The internal
  `helivanta.in.workforce.document_scan_requested.v1` and
  `document_changed.v1` events keep their `hospital_id` field. That field
  already names the owning establishment row: the worker's
  `app.workforce_hospital`, the worker RLS function, the restrictive outbox
  policy and the storage key all key on it, and no module outside workforce
  consumes either event. The Go contract comment and the API document say so;
  the payload shape does not change.
- **Storage.** Object keys keep the `t/<tenant>/h/<establishment>/o/staff/...`
  layout. A pharmacy ID and a hospital ID never collide, because both are
  rows of one registry keyed by `(tenant_id, id)`.
- **Audit.** `workforce.document.downloaded` and the table audit rows are
  written under the pharmacy ID, in the same transaction as the change or
  the final download authorization.

### Status codes

| Case                                                                                | Status                      |
| ----------------------------------------------------------------------------------- | --------------------------- |
| Unknown, foreign-tenant, hospital or supplier establishment                         | 404                         |
| No `manager` access row, or the row is removed before a later phase commits         | 404                         |
| Staff record, record choice or file missing, or present under another establishment | 404                         |
| Missing `workforce.documents.manage`                                                | 403                         |
| `hr_intake` requested                                                               | 400                         |
| Expected tenant or subject header missing, repeated or different from the session   | 409 `session_changed`       |
| Invalid intent, bytes, content type or attempt                                      | 400                         |
| Intent conflict, expired intent, stale record version, wrong state or attempt       | 409                         |
| Document storage not configured or unavailable                                      | 503 `documents_unavailable` |
| Registry, database or authorization unavailable                                     | 503                         |

### Data and enforcement

Migration `0098_workforce_pharmacy_documents` (append-only):

- A new guard function, `workforce_require_document_establishment()`. It
  admits a hospital row of any record kind, and a pharmacy row only for the
  `enrollment`, `credential` and `qualification` record kinds. Tax
  declaration and benefit proposal evidence therefore stay hospital-only in
  the database, not only in the handlers. It refuses with the existing
  `workforce_hospital_establishment` constraint name.
- The `workforce_documents` establishment trigger moves to that function.
- The `workforce_document_contributors` trigger moves to
  `workforce_require_enrollment_establishment()`. Contributor rows exist only
  for enrollment identity evidence, which a pharmacy may now hold.
- The establishment inventory test names these two tables beside the six
  enrollment tables. A ninth table that starts admitting a pharmacy fails it.

The document RLS policy is already establishment-neutral: it requires
`app.workforce_enrollment='authorized'`, which only a manager row sets, and it
matches `app.workforce_hospital`. The worker's scoped claim and the outbox
policy key on the same establishment ID.

### Defect fixed in the shared core

`requeueDocument` chose its transaction with `staffDocuments{}` instead of the
calling gate. A payroll or benefits evidence retry sent with `?hr_intake=v1`
was therefore routed into the HR intake transaction and refused. The core now
uses the caller's gate, proven by regression tests on the tax declaration and
benefits evidence retries.

### UI

- Each pharmacy Staff record gains a "Documents" action, shown when the caller
  holds `workforce.documents.manage` and the manager level. The API remains
  the boundary.
- It opens the shared document workspace (record choice, upload, list,
  retry, revoke, download) bound to the pharmacy route family. File
  classification is not offered.
- A 401, 403 or 404 ends the workspace and clears the Staff source with "Your
  account or Staff access changed", with no retry, as 4a does. Another 409
  shows the server message and keeps the workspace open, as on a hospital.

### Not covered by 4d

- File classification. It references HR `document_category` master
  revisions, and a pharmacy has no HR masters (4a). A cleared classification
  names no master revision, so the database refuses a pharmacy
  classification revision directly.
- Identity review of pharmacy identity evidence (person reviews), which stays
  hospital-only until a reviewer path exists for a pharmacy.
- Employment evidence bindings (4b) and credential reviews (4e).
- Deployed runtime acceptance and real OIDC browser acceptance.

### Numbers and failure behaviour

- **Load.** Uploads are human-paced PNG or JPEG files of at most 10 MiB
  (`attachments.MaxFileBytes`), with the hospital family's limits. A pharmacy
  holds at most 50,000 Staff; list pages are cursor-bounded.
- **Failure.** When storage is unavailable, registration and upload answer
  503 and no file advances to a downloadable state. When the scanner is down,
  files stay `scan_pending` and become retryable after 45 minutes. Pharmacy
  and hospital requests share no lock.
- **Rollback.** Revert the code. Restoring the hospital-only guard needs a new
  forward migration, and only if pharmacy files must stop being writable.
  Existing rows and stored objects stay readable to the worker.
- **Rejected alternative.** A `pharmacy_id` v2 of the scan events. The worker,
  outbox policy and storage key would each need a second path for one
  establishment kind, with no consumer that needs the distinction.

### Acceptance tests (red first)

1. A pharmacy manager with `workforce.documents.manage` reads record choices,
   registers an intent, uploads bytes, sees one scan request whose
   `hospital_id` is the pharmacy ID, downloads the released file and revokes
   it. Replays return the same response.
2. Without an access row, with a reader row, on a hospital ID, on a supplier
   ID and on a foreign tenant, every route answers 404. Without the
   permission every route answers 403. `hr_intake=v1` answers 400 and a
   changed expected subject answers 409.
3. A hospital Staff ID, a hospital credential ID and a hospital file ID under
   the pharmacy family answer 404, and the reverse holds on the hospital
   family.
4. A direct SQL insert of a pharmacy `tax_declaration` or `benefit_proposal`
   document is refused, and a supplier document row is refused.
5. A benefits evidence retry with `?hr_intake=v1` behaves as without it.
6. The hospital document suites keep their behaviour.
7. SDK schema and hook tests, and a workspace panel test using
   `renderWithProviders`, for loading, 404, 409 and success.

## 4b — Pharmacy employment periods

### Actors and authority

| Actor                     | Authority kind | Scope                      | Preconditions                                                                                                                                                                                             |
| ------------------------- | -------------- | -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Pharmacy HR manager       | Administration | One pharmacy establishment | Tenant permission `workforce.enrollment.manage`. Evidence also needs `workforce.documents.manage`. A `manager` row in `workforce_hospital_access` for the pharmacy. All are read again inside the command |
| Pharmacy Workforce reader | Administration | One pharmacy               | None. A reader cannot read or change employment terms, exactly as on a hospital                                                                                                                           |

An employment period draft records proposed terms: engagement kind, duration,
dates, contracting party and sponsor. It does not employ anyone, grant access
or end employment. Only an employment review and generation (4c) make terms
effective, so until 4c ships a pharmacy draft stays a draft. Pharma
membership, an outlet grant and the PharmaConnect pharmacist role confer
nothing. Delegated HR intake stays hospital-only, as in 4a, so `hr_intake`
answers 400.

### Routes

All routes sit under `/v1/workforce/pharmacies/:pharmacy/staff/:id` and
require the pharmacy manager row.

| Route                                                                   | Permission                                         | Effect                                                                   |
| ----------------------------------------------------------------------- | -------------------------------------------------- | ------------------------------------------------------------------------ |
| `GET employment-source`                                                 | `workforce.enrollment.manage`                      | Current Staff version and employment epoch, audited                      |
| `GET employment-periods?lifecycle=withdrawal-v1`                        | `workforce.enrollment.manage`                      | Cursor list of drafts and withdrawn drafts                               |
| `PUT employment-periods/:period`                                        | `workforce.enrollment.manage`                      | Create version 1 or save the next version; a replayed command returns it |
| `GET employment-periods/:period?lifecycle=withdrawal-v1`                | `workforce.enrollment.manage`                      | One draft                                                                |
| `POST employment-periods/:period/withdraw`                              | `workforce.enrollment.manage`                      | Withdraw an open draft with a reason; terms are preserved                |
| `GET employment-periods/:period/history?lifecycle=withdrawal-v1`        | `workforce.enrollment.manage`                      | Cursor list of every saved revision                                      |
| `PUT .../:period/revisions/:version/evidence/:binding`                  | `workforce.enrollment.manage` + `documents.manage` | Bind a released employment evidence file to one exact draft revision     |
| `POST .../evidence/:binding/withdraw`                                   | `workforce.enrollment.manage` + `documents.manage` | Withdraw a binding with a reason                                         |
| `GET .../evidence`, `GET .../evidence/:binding`, `.../:binding/history` | `workforce.enrollment.manage` + `documents.manage` | Cursor list, one binding and its revisions                               |
| `GET .../evidence/:binding/content`                                     | `workforce.enrollment.manage` + `documents.manage` | Download the bound bytes, auditing the disclosure before any byte leaves |

- **Shared core.** The hospital and pharmacy handlers call one period and
  evidence core. Validation, intent hashing, replay, version checks, Staff
  version and epoch checks, the sponsor rule, the 20-binding limit, exact
  released bytes and audit actions are therefore identical. The core takes the
  establishment the route admitted and an authority for that family.
- **Kind re-check.** The core requires the admitted establishment kind inside
  the command, before the supervision lock and the Staff row lock. A hospital
  route on an ID registered as a pharmacy after admission answers 404 and
  writes nothing, and the reverse holds. The hospital evidence download gate
  gains the same check; it previously authorized through `documentOwner`
  alone.
- **Pharmacy authority.** The pharmacy authority from 4a takes the permissions
  each route needs: enrollment for terms, enrollment and documents for
  evidence. It refreshes the session and permissions, then opens the pharmacy
  transaction under `enrollmentScope` (the manager row, under share lock). It
  never consults HR intake or scoped IAM grants.
- **Sponsor.** A contractor or agency worker needs an accountable sponsor:
  an active Staff record of the same pharmacy. The existing foreign key and
  guard key the sponsor by `(tenant_id, hospital_id)`, so a hospital's Staff
  cannot sponsor a pharmacy draft, and the reverse.
- **Evidence.** A binding names a released `enrollment` file in the
  `employment_evidence` category of the same pharmacy Staff record at the
  draft's Staff version (4d). The guard function is unchanged.
- **Versioned views only.** Pharmacy period reads require
  `lifecycle=withdrawal-v1`, because the pharmacy family has no clients that
  predate withdrawal. Without it they answer 400.
- **Account assertions.** `X-Helivanta-Expected-Tenant` and
  `X-Helivanta-Expected-Subject` must each appear once and equal the verified
  principal; otherwise 409 `session_changed`.
- **Response.** The same source, draft and binding views as the hospital
  family, with `pharmacy_id` in place of `hospital_id`. The views are
  generated from the canonical period models.
- **Audit.** `workforce.employment_source.read`,
  `workforce.employment_period.*` and `workforce.employment_evidence.*` rows
  are written under the pharmacy ID, in the same transaction as the read or
  change. No event is published, on either family.

### Status codes

| Case                                                                                            | Status                          |
| ----------------------------------------------------------------------------------------------- | ------------------------------- |
| Unknown, foreign-tenant, hospital or supplier establishment                                     | 404                             |
| No `manager` access row, or the row is removed before the command commits                       | 404                             |
| Staff record, sponsor, draft, revision, binding or file missing, or under another establishment | 404                             |
| Missing `workforce.enrollment.manage`, or `workforce.documents.manage` on an evidence route     | 403                             |
| `hr_intake` requested, or a period read without `lifecycle=withdrawal-v1`                       | 400                             |
| Expected tenant or subject header missing, repeated or different from the session               | 409 `session_changed`           |
| Invalid terms, dates, reason, command ID or version                                             | 400                             |
| Stale draft or Staff version, changed epoch, withdrawn draft, replay with other input           | 409 `employment_draft_conflict` |
| Evidence source changed, bytes not released, binding set full, duplicate active file            | 409                             |
| Lock timeout, deadlock or serialization failure on evidence                                     | 503 `employment_evidence_busy`  |
| Registry, database or authorization unavailable                                                 | 503                             |

### Data and enforcement

Migration `0099_workforce_pharmacy_employment_periods` (append-only):

- The `workforce_hospital_establishment` triggers on
  `workforce_employment_period_drafts` and
  `workforce_employment_evidence_bindings` move to
  `workforce_require_enrollment_establishment()`. A supplier or unregistered
  establishment is still refused.
- The two history tables carry no trigger of their own. They are reached
  through foreign keys to those tables, so they admit exactly what their
  parents admit.
- The establishment inventory test names the four tables among those a
  pharmacy writes, and the enrollment guard list names the two parents.
  Reviews, cohorts, contributors, review evidence and generations keep the
  hospital guard until 4c.

The period and evidence RLS policies are already establishment-neutral. They
require `workforce_enrollment_allowed`, which holds only after
`enrollmentScope` sets `app.workforce_enrollment='authorized'` from a manager
row of that establishment. Evidence additionally requires the
`app.workforce_employment_evidence` mode the core sets.

### UI

- Each pharmacy Staff record gains an "Employment" action, shown when the
  caller holds `workforce.enrollment.manage` and the manager level. The API
  remains the boundary.
- It opens the shared employment terms panel bound to the pharmacy route
  family: source, list, editor, history and withdrawal. The sponsor picker
  reads the pharmacy's own Staff directory and is labelled "Sponsor".
- With `workforce.documents.manage`, each draft revision offers the shared
  evidence panel. It lists the pharmacy record's released employment evidence
  files and offers no "Prepare employment review", because reviews are 4c.
- A 401, 403, 404 or 409 `session_changed` ends the panel and clears the
  Staff source with "Your account or Staff access changed", as 4a does. Any
  other 409 keeps the editor and its values open and asks the user to reopen
  and review the current record, as on a hospital.
- The generated sponsor validation message becomes "Choose an accountable
  sponsor.", true for both families.

### Not covered by 4b

- Employment reviews, cohorts and generations (4c). A pharmacy draft cannot
  become effective, and nothing in this slice grants, renews or ends
  employment or access.
- Delegated HR intake for a pharmacy.
- Deployed runtime acceptance and real OIDC browser acceptance.

### Numbers and failure behaviour

- **Load.** Human-paced HR edits. A draft command body is at most 8 KiB and an
  evidence command at most 4 KiB. A revision holds at most 20 active bindings.
  A pharmacy holds at most 50,000 Staff; every list is cursor-bounded.
- **Failure.** When the database or authorization is unavailable, every route
  answers 503 and writes nothing. A lock wait over 5 seconds on evidence
  answers 503 `employment_evidence_busy`, and the same command can be
  retried. Pharmacy and hospital commands take different supervision locks.
- **Rollback.** Revert the code. Restoring the hospital-only guard needs a new
  forward migration, and only if pharmacy drafts must stop being writable.
  Existing rows stay as history.
- **Rejected alternative.** One handler that looks up the kind and accepts
  either. The route family is what names the kind; a dual-kind handler would
  let a hospital-rooted IAM scope or HR intake grant reach a pharmacy.

### Acceptance tests (red first)

1. A pharmacy manager with `workforce.enrollment.manage` reads the source,
   saves version 1, saves version 2, replays version 2, lists and reads with
   the lifecycle view, reads history and withdraws. Each response names
   `pharmacy_id`, and the audit rows name the pharmacy.
2. With `workforce.documents.manage` as well, an employment evidence file
   uploaded through 4d binds to the draft revision, lists, reads, downloads
   with an audited disclosure and withdraws. Replays return the same receipt.
3. Without an access row, with a reader row, on a hospital ID, on a supplier
   ID and on a foreign tenant, every route answers 404. Without the
   enrollment permission every route answers 403, and without the documents
   permission every evidence route answers 403. `hr_intake=v1` and a missing
   lifecycle view answer 400, and a changed expected subject answers 409.
4. A hospital Staff, period, binding or sponsor ID under the pharmacy family
   answers 404, and the reverse holds on the hospital family.
5. A hospital route on an ID registered as a pharmacy after admission answers
   404 and writes no draft, binding or audit row.
6. A direct SQL insert of a supplier period draft is refused; the inventory
   test lists the four pharmacy-admitting tables.
7. The hospital period, withdrawal, evidence and review suites keep their
   behaviour.
8. SDK schema and receipt tests, and panel tests using `renderWithProviders`,
   for loading, 404, 409, success and the hidden review action.

## 4e — Pharmacy credential reviews, validity and reminders

### Actors and authority

| Actor                        | Authority kind | Scope                      | Preconditions                                                                                                                                                    |
| ---------------------------- | -------------- | -------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Pharmacy HR manager          | Administration | One pharmacy establishment | Tenant permission `workforce.enrollment.manage` and a `manager` row in `workforce_hospital_access` for the pharmacy. Both are read again inside the command      |
| Pharmacy credential reviewer | Administration | One pharmacy               | Tenant permission `workforce.credentials.review` and a `manager` row for the pharmacy. Not the proposer, and not the subject linked to that Staff record's login |
| Pharmacy Workforce reader    | Administration | One pharmacy               | None. A reader cannot read or change credential reviews, exactly as on a hospital                                                                                |
| Reminder worker              | Service        | One credential revision    | Database role `helivanta_document_worker`, scoped by transaction settings to one establishment, Staff record and credential                                      |

A credential review is HR checking a licence or registration that 4a recorded
against the evidence files that 4d stored. An approval records reviewed
validity dates. It is not a statutory verification and does not register
anyone as a pharmacist in PharmaConnect, admit a dispensing role or grant
access. An expiry reminder is a prompt to HR, not a change of authority. Pharma
membership, an outlet grant and the PharmaConnect pharmacist role confer
nothing here. Delegated HR intake stays hospital-only, so `hr_intake` answers 400.

### Routes

Staff routes sit under `/v1/workforce/pharmacies/:pharmacy/staff/:id`, and HR
routes under `/v1/workforce/pharmacies/:pharmacy/hr`. Every route requires the
pharmacy manager row.

| Route                                                    | Permission                     | Effect                                                                      |
| -------------------------------------------------------- | ------------------------------ | --------------------------------------------------------------------------- |
| `GET credentials`                                        | `workforce.enrollment.manage`  | Recorded credentials with reviewed validity, standing and pending reviews   |
| `GET credentials/:credential/reviews`                    | `workforce.enrollment.manage`  | Cursor list of a credential's reviews                                       |
| `PUT credentials/:credential/reviews/:review`            | `workforce.enrollment.manage`  | Propose a review citing 1 to 5 credential evidence files; replays return it |
| `POST credentials/:credential/reviews/:review/decision`  | `workforce.credentials.review` | Approve or reject a pending review                                          |
| `POST credentials/:credential/reviews/:review/withdraw`  | `workforce.enrollment.manage`  | The proposer withdraws a pending review                                     |
| `GET hr/credentials?expiring_within=`                    | `workforce.enrollment.manage`  | Cursor register of reviewed credentials, optionally by expiry               |
| `GET hr/credential-reminder-policy`, `PUT` the same      | `workforce.enrollment.manage`  | Read or version the reminder thresholds                                     |
| `GET hr/credential-reminders?status=`                    | `workforce.enrollment.manage`  | Cursor inbox of open, acknowledged or superseded reminders                  |
| `POST hr/credential-reminders/:reminder/acknowledgement` | `workforce.enrollment.manage`  | Acknowledge an open reminder                                                |
| `GET hr/time-zone`, `PUT` the same                       | `workforce.enrollment.manage`  | Read or version the pharmacy's HR time zone                                 |

- **Shared core.** The hospital and pharmacy handlers call one credential
  core. Validation, replay, version checks, the one-pending rule, evidence
  checks, separation of duties, the subject-conflict check, approval, standing,
  the register filter, reminder policy, reminder acknowledgement and audit
  actions are therefore identical. The core takes the establishment the route
  admitted and an authority for that family.
- **Kind re-check.** The core requires the admitted establishment kind inside
  the command, before the per-Staff or per-reminder advisory lock. A hospital
  route on an ID registered as a pharmacy after admission answers 404 and
  writes nothing, and the reverse holds.
- **Authority.** The hospital authority keeps today's checks: the route
  permission, then the manager row under share lock, which sets
  `app.workforce_enrollment`. The pharmacy authority from 4a refreshes the
  session and permissions, requires each permission the route names, then opens
  the pharmacy transaction under `enrollmentScope`. It never consults HR intake
  or scoped IAM grants.
- **Lock order.** Manager row, establishment kind, then the advisory key
  (`<staff id>` for reviews, shared with enrollment; `credential-reminder-policy`;
  `credential-reminder:<id>`; `time-zone`). It matches enrollment, so a review
  and an enrollment save never interleave.
- **Time zone.** Standing and expiry are calendar dates. A hospital reads its
  zone from roster settings. A pharmacy has no roster, and its outlets each keep
  their own zone in PharmaConnect, which Workforce cannot read. The pharmacy HR
  time zone is therefore a Workforce setting of its own. Without it, standing is
  `date_unknown`, the expiry filter answers 409 `time_zone_unset`, and the
  reminder worker waits. A changed zone takes effect on each schedule's next
  daily wake.
- **Account assertions.** `X-Helivanta-Expected-Tenant` and
  `X-Helivanta-Expected-Subject` must each appear once and equal the verified
  principal; otherwise 409 `session_changed`.
- **Response.** The same credential, review, register, policy and reminder
  views as the hospital family. None of them names an establishment ID. The
  time-zone view is `{version, time_zone?, updated_by_subject?, updated_at?}`.
- **Audit.** `workforce.credentials.read`, `workforce.credential_review.*`,
  `workforce.credentials.register_read`,
  `workforce.credential_reminder_policy.changed`,
  `workforce.credential_reminders.read`,
  `workforce.credential_reminder.acknowledged` and
  `workforce.pharmacy_time_zone.changed` rows are written under the pharmacy ID,
  in the same transaction as the read or change. The time-zone row carries the
  previous and new zone.
- **Events.** An approval on a pharmacy publishes no `enrollment_changed.v1`,
  as in 4a. `credential_review_changed.v1`, `credential_validity_changed.v1`,
  `credential_reminder_policy_changed.v1` and `credential_reminder_changed.v1`
  are published as on a hospital. Their `hospital_id` names the owning
  establishment row, as `document_changed.v1` does since 4d. Only Workforce
  consumes them: the validity event starts the reminder schedule. The time-zone
  setting publishes nothing, because the schedule reads it on each wake.

### Status codes

| Case                                                                                    | Status                    |
| --------------------------------------------------------------------------------------- | ------------------------- |
| Unknown, foreign-tenant, hospital or supplier establishment                             | 404                       |
| No `manager` access row, or the row is removed before the command commits               | 404                       |
| Staff record, credential, review or reminder missing, or under another establishment    | 404                       |
| Missing `workforce.enrollment.manage`, or `workforce.credentials.review` on a decision  | 403                       |
| `hr_intake` requested                                                                   | 400                       |
| Expected tenant or subject header missing, repeated or different from the session       | 409 `session_changed`     |
| Invalid number, dates, reason, evidence list, note, thresholds, page limit or time zone | 400                       |
| Stale version, pending review, unchanged proposal, replay with other input              | 409                       |
| Self review, a reviewer linked to the Staff record, or a withdrawal by someone else     | 409                       |
| Evidence not this credential's, not yet released at approval, or credential removed     | 409                       |
| Expiry filter without a pharmacy time zone                                              | 409 `time_zone_unset`     |
| Reminder already superseded                                                             | 409 `reminder_superseded` |
| Registry, database or authorization unavailable                                         | 503                       |

### Data and enforcement

Migration `0100_workforce_pharmacy_credential_reviews` (append-only):

- The `workforce_hospital_establishment` triggers on
  `workforce_credential_reviews` and `workforce_credential_reminder_policies`
  move to `workforce_require_enrollment_establishment()`. A supplier or
  unregistered establishment is still refused.
- `workforce_credential_validity` and `workforce_credential_reminders` carry no
  trigger of their own. They are reached through foreign keys to reviews,
  enrollment credentials and policies, so they admit exactly what those admit.
- `workforce_roster_credential_standing` is roster data. It gains the hospital
  guard, and `workforce_roster_standing_sync()` copies a validity row only for a
  hospital. A pharmacy approval therefore writes no standing row, and a direct
  pharmacy standing insert is refused.
- New table `workforce_pharmacy_time_zones(tenant_id, hospital_id, timezone,
version, updated_by_subject, created_at, updated_at)`, keyed by establishment
  and referencing `workforce_hospitals`:
  - a new `workforce_require_pharmacy_establishment()` guard admits only a
    registered pharmacy;
  - the zone is checked by the existing `workforce_roster_timezone_valid()`;
  - identity is immutable and each update advances the version by one;
  - rows cannot be deleted;
  - forced RLS requires `workforce_enrollment_allowed`. The reminder worker gets
    a read-only branch for its one establishment and a column grant on
    `(tenant_id, hospital_id, timezone)`, exactly as on roster settings.
- `credentialClock` reads roster settings for a hospital and the pharmacy time
  zone for a pharmacy. The guards make the two disjoint, so one establishment
  never has both.
- The establishment inventory test lists reviews and reminder policies among
  the enrollment-guarded tables, and validity, reminders and the time-zone
  table among those a pharmacy writes. The time-zone table is the only table
  under the pharmacy guard.

The review, validity, policy and reminder RLS policies are already
establishment-neutral. They require `workforce_enrollment_allowed`, which holds
only after the manager check sets `app.workforce_enrollment='authorized'` from a
manager row of that establishment. The subject-conflict function already reads
a pharmacy's independent login links (0094).

### UI

- Each pharmacy Staff record gains a "Credentials" action, shown when the caller
  holds `workforce.enrollment.manage` and the manager level. The API remains the
  boundary.
- It opens the shared credential panel bound to the pharmacy route family:
  recorded credentials with standing, proposing a review with the record's
  credential evidence files from 4d, history and withdrawal. Approve and reject
  are shown with `workforce.credentials.review` and hidden on the caller's own
  proposal.
- The pharmacy employer gains a "Credential register" action with the same
  gate. It opens the shared register, the reminder inbox and the reminder
  policy, bound to the pharmacy, plus the pharmacy time-zone setting. The zone
  is an IANA name checked by the same `isSupportedTimezone` rule as roster
  settings before saving; the server checks it again.
- Without a zone, standing reads "Expiry standing needs this pharmacy's time
  zone" and the expiry filter is disabled with the same reason.
- A 401, 403, 404 or 409 `session_changed` ends the panel and clears the Staff
  source with "Your account or Staff access changed", as 4a does. Any other 409
  keeps the form and its values open and shows the reason.

### Not covered by 4e

- Statutory licence verification with a pharmacy council or registry, and any
  effect on PharmaConnect pharmacist roles or dispensing authority.
- Suspending or ending access or employment when a credential expires; that
  needs effective employment (4c).
- Roster credential clashes for a pharmacy, which has no roster.
- Delegated HR intake for a pharmacy.
- Deployed runtime acceptance and real OIDC browser acceptance.

### Numbers and failure behaviour

- **Load.** Human-paced HR reviews. A proposal body is at most 4 KiB, a decision
  or withdrawal 2 KiB, and a policy or time-zone body 1 KiB. A review cites 1 to
  5 files, a policy holds 1 to 6 thresholds, and every list is cursor-bounded at
  100 rows. A pharmacy holds at most 50,000 Staff. Each approved revision runs
  one reminder schedule that wakes at most daily.
- **Failure.** When the database or authorization is unavailable, every route
  answers 503 and writes nothing. When the reminder worker or Temporal is
  unavailable, approval still commits and the outbox retries the schedule start.
  Without a zone the schedule waits and writes nothing.
- **Rollback.** Revert the code. Restoring the hospital-only guards needs a new
  forward migration, and only if pharmacy reviews must stop being writable.
  Existing rows stay as history. The time-zone table stays and is unused.
- **Rejected alternatives.** Admitting a pharmacy to roster settings would give
  it week-start and auto-population fields with no roster behind them. Reading
  an outlet zone from PharmaConnect would cross a module boundary, and a pharmacy
  with outlets in two zones has no single answer. One handler that looks up the
  kind would let a hospital-rooted IAM scope reach a pharmacy, as 4b recorded.

### Acceptance tests (red first)

1. A pharmacy manager with `workforce.enrollment.manage` lists an unreviewed
   credential and proposes a review citing a credential evidence file uploaded
   through 4d. A second manager with `workforce.credentials.review` approves it
   once the file is released. The list then shows the reviewed dates and
   standing in the pharmacy's zone, the enrollment row mirrors them and the
   Staff version advances. Audit rows name the pharmacy. The outbox holds the
   review and validity events with the pharmacy ID, and no
   `enrollment_changed.v1`.
2. The proposer's own decision, and a decision by the subject linked to that
   Staff record, answer 409 and write nothing. Withdrawal works for the
   proposer only, and rejection needs a note. Replays return the same review.
3. The time zone reads as version 0, saves version 1, replays unchanged, refuses
   a stale version with 409 and an unsupported zone with 400. Before it is set,
   standing is `date_unknown` and the expiry filter answers 409
   `time_zone_unset`; afterwards the filter returns the expiring credential.
4. A reminder policy is saved; the worker advancing a pharmacy schedule writes
   the due reminder, which lists, acknowledges and is superseded by the next
   approval. Without a zone the worker writes nothing and waits.
5. Without an access row, with a reader row, on a hospital ID, on a supplier ID
   and on a foreign tenant, every route answers 404. Without the enrollment
   permission every route answers 403, and a decision without the review
   permission answers 403. `hr_intake=v1` answers 400, and a changed expected
   subject answers 409.
6. A hospital Staff, credential, review or reminder ID under the pharmacy
   family answers 404, and the reverse holds. A hospital route on an ID
   registered as a pharmacy after admission answers 404 and writes nothing.
7. A pharmacy approval writes no roster standing row, a direct pharmacy
   standing insert and a direct hospital time-zone insert are refused, and a
   supplier review insert is refused. The inventory test lists the new
   pharmacy-admitting tables.
8. The hospital credential, reminder, reminder worker and roster clash suites
   keep their behaviour.
9. SDK path and schema tests, and panel tests using `renderWithProviders`, for
   loading, unset zone, 404, 409, success and the hidden decision on one's own
   proposal.
