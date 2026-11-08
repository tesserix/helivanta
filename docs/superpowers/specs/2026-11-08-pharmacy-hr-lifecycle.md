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
| 4d        | Staff documents and their classification                                                                                       | Documents, contributors                                     | 3a         |
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
