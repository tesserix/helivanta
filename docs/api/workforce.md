# Workforce API: directory and protected enrollment

## Scoped employment-review workspace candidate

The normal HR preparation and independent-review workspace uses these additive
owner routes, all through the Global API with exact original tenant/subject
assertion headers and current reviewed active Staff/login actor checks:

- `GET /v1/workforce/employment-review-scopes`: at most 20 reviewed actor-link
  candidates per page. `can_submit` requires both scoped HR intake grants;
  `can_review` requires the independent employment-review grant. Each capability
  is separately guarded and audited; neither grants a subsequent command.
- `GET /v1/workforce/hospitals/:hospital/employment-review-inbox`: exactly one
  `hr_intake=v1` or `employment_review=v1`, optional recorded `status` from
  pending/approved/rejected/withdrawn/revoked, and canonical cursor/limit. Minimal
  summaries contain review/hospital/Staff IDs, Staff code/name, recorded
  status/version and creation time, without private terms, files or reasons.
- `GET /v1/workforce/hospitals/:hospital/staff/:id/employment-person-findings`:
  preparer-only `hr_intake=v1`, exact positive safe-integer `staff_version` and
  `employment_epoch`, and optional cursor/limit capped at 20. Both guarded phases
  pin current source and actor authority; returned approved finding options have
  their exact receipt, source hash/epoch, policy/finding reference, validity and
  account intent revalidated against released evidence. No identity files or
  author profiles are returned, and identity-review authority is not granted.

The filtered scope/finding scans preserve empty-page continuation; clients must
follow the returned cursor. Inbox pages reauthorize current original authority
and commit sensitive-read audit before returning data. The inbox and finding
selector refuse unknown or duplicated query fields/modes with 400; scope
discovery retains the shared pagination parser. Missing/changed workspace assertions
with 409, denied authority with 403, invisible Staff with 404, uncertain authority
with 503 and failed persistence/audit with 500. Finding selection with a changed
Staff version/epoch returns 409. A recorded inbox status is distinct from current
review effectiveness and operational activation.

The UI starts preparation from a saved exact terms revision and attached released
employment evidence, selects a current approved person finding and requires policy,
reason, selected evidence, examination and association attestations. Independent
decisions consume current owner capabilities, preserve exact uncertain retries,
and clear private data after access cutoff. History and byte release retain
separate current authorization. See the [local acceptance evidence](../qa/2026-11-01-employment-review-ui-acceptance.md).
Four persisted forward migrations add only read contexts and concurrent indexes;
write/delete restrictions remain. This candidate is undeployed and does not
complete operational activation, legacy/governance migration or lifecycle cutoff.

Implementation: `backend/internal/modules/workforce`. Design: [ADR 0009](../adr/0009-workforce-foundation.md). This is the hospital-local staff directory foundation, not a complete HR, payroll, practitioner credentialing or rostering system.

## Protected selected employment-evidence candidate

The local candidate adds
`GET /v1/workforce/hospitals/:hospital/staff/:id/employment-reviews/:review/evidence/:file/content`.
Exactly one explicit `hr_intake=v1` or `employment_review=v1` projection and the
original tenant/subject assertions are required. Paging and unknown query fields
are refused. Both phases resolve current compound HR-intake or separate scoped
employment-review authority and a reviewed active actor link, without generic
document permission or legacy fallback. Only a selected file in a currently
effective pending/approved frozen review is eligible. The existing current
person, Staff, terms, classification, binding and complete-provenance checks apply.

The owner commits authorization audit and releases database locks before the
five-second, login-idle-bounded GCS read. Released generation, metadata/actual
length and metadata/actual SHA-256 must match the authorized file. A second
transaction rechecks the original grants, actor link/employment epoch and frozen
source/review/file basis and commits attributable release audit before returning
any bytes. Assignment/link replacement, review ending, unavailable person/source
or changed file cannot authorize an in-flight request. The existing `downloaded`
audit denotes authorization to release bytes, not proof of client delivery.

Return 400 for invalid projection/query, 403 for lost authority, 404 for an
invisible/unselected file, 409 for unavailable or changed review/evidence, 503 for
provider/authority uncertainty or full transfer admission, and 500 for an
unclassified persistence failure such as failed release audit. These failures
return no file bytes. Owner responses are private/no-store attachments with
nosniff and sandbox headers; Global API normalizes cache policy to `no-store`.
No storage URL or redirect is returned. Four transfers per owner-handler process
bound in-flight payload buffers to 40 MiB under the existing 10 MiB file maximum;
provider/HTTP/GC overhead and load remain measurement gates, not a capacity claim.

The typed `downloadEmploymentReviewEvidence` consumer validates the canonical
owned current view and selected file before dispatch and preserves the original
workspace and cancellation signal. A shared person/employment byte validator
checks MIME and declared length, streams into one bounded authorized-size buffer,
cancels overflow, verifies raw length/hash, and discards cancelled responses before
creating a Blob. No new schema, permission or migration is added. This API/SDK
slice does not implement employment-review discovery/inbox, preparation selection,
normal reviewer UI acceptance or operational activation and is not deployed here.

## Protected employment-review history candidate

The local candidate adds
`GET /v1/workforce/hospitals/:hospital/staff/:id/employment-reviews/:review/history`.
Use exactly one explicit `hr_intake=v1` or `employment_review=v1` projection,
plus optional `limit`/`cursor`; duplicate modes, unknown query fields and duplicate
paging values are refused. Other employment commands retain their strict query
shape. The platform list envelope defaults to 50 receipts and caps at 200;
current legal transitions bound one review to at most three immutable receipts.
The existing scoped history primary key bounds lookup before ordering, without
a new migration or infrastructure cost.

Each page reauthorizes the current original workspace, hospital-scoped assignment
and reviewed active actor link using the same no-legacy-fallback authority as
protected detail. HR preparation requires both enrollment and document grants;
reviewer mode requires the separate employment-review permission. Return 403 for
missing authority, 404 for an invisible selected review/Staff, 409 for changed
workspace and 503 when current authority is unavailable. Owner SQL always scopes
verified tenant, hospital, Staff and review and retains existing forced RLS.
The owner loads the frozen terms/evidence once, attaches them to historical
receipts and commits sensitive-read audit before returning the cursor page.
Ended reviews and unavailable current evidence retain their recorded history.
Receipt status is historical, without current capabilities, evidence bytes or
employment/access activation.

`listEmploymentReviewHistory` uses the canonical generated `EmploymentReview`
contract through Global API, no-store and original tenant/subject assertions.
It rejects foreign scopes/review IDs, duplicate or out-of-order revisions,
inconsistent pagination and changed frozen sources within a page, and discards
cancelled responses. Consumers must keep history separate from present
effectiveness and obtain current detail before a new command. The endpoint/SDK
candidate does not implement the normal employment-review workspace, inbox,
source/evidence selection or protected bytes and is not deployed by this note.

## Pre-employment current-person review candidate

The current source adds a private human person-finding review for inactive
joiners and workers requesting no login. This does not activate employment,
create an operational account link, enroll a tenant member, issue an ID card or
grant business permissions. Existing active-employment identity-case and login
guards remain unchanged. Normal UI acceptance and employment consumption are
still required. These foundations were deployed in PR #1501; the
[local browser checkpoint](../qa/2026-11-01-person-review-ui-acceptance.md) now
records positive approval and ending cases. Neither deployment nor that checkpoint
establishes a completed employment journey.

Workforce owns the source identity hash/epoch, immutable review receipts,
prospective identity-document contributors and released-evidence pins. Canonical
input: `contracts/workforce-person-reviews.json`; generator:
`node scripts/generate-workforce-person-reviews.mjs [--check]`. API-client methods
are exported from `@helivanta/api` in `person-reviews.ts`; all requests go through
Global API and include the original expected tenant/subject, with no-store caching.

| Registered owner route under `/v1/workforce/hospitals/:hospital/staff/:id/person-reviews` | Scope and behavior                                                                                                                                                                             |
| ----------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| PUT `/:review`                                                                            | Current identity-check manager plus own reviewed active actor link; complete immutable pending submission, 8 KiB limit.                                                                        |
| GET `/:review`                                                                            | Current identity manager/reviewer plus reviewed actor link; audited original receipt **and separate current effectiveness**.                                                                   |
| GET `/:review/history`                                                                    | Same read authority; audited cursor pagination, default 50/max 200. Frozen evidence is bounded to ten files per receipt.                                                                       |
| POST `/:review/decision`                                                                  | Current identity reviewer and independent actor; approve/reject with exact expected version and UUIDv7 command.                                                                                |
| POST `/:review/withdrawal`                                                                | Current identity manager who submitted it; reasoned pending withdrawal, preserving history.                                                                                                    |
| POST `/:review/revocation`                                                                | Current identity reviewer; reasoned revocation of an approved finding, including after a source correction.                                                                                    |
| GET `/:review/evidence/:file/content`                                                     | Current identity manager/reviewer plus own reviewed active actor link; one frozen identity image, capped at 10 MiB; original authority and review/source/file lineage rechecked after storage. |

`GET /v1/workforce/person-review-scopes` now discovers hospitals from the
caller's own reviewed active actor links and held current identity assignments.
The canonical response row is `{hospital_id,name,can_submit,can_review}`;
capabilities are independent and confer no authority on later commands. Legacy
hospital administration alone yields no row. Both original workspace assertions
are mandatory. The scan defaults/caps at 20 candidates plus a probe, with a
caller/tenant/route-bound cursor. Empty filtered pages may still have
`has_more:true`; follow the opaque continuation. Excessive limits and cross-actor/
route cursors return 400, absent/mismatched workspace 409, unavailable authorization 503. Checks have a five-second budget capped by session idleness and four-way
concurrency; each visible hospital has a locked current actor/grant audit.
`listPersonReviewScopes` verifies canonical capabilities and continuation through
Global API, with no-store and cancellation checks. No generic document/HR grants
or additional database policies/migrations are introduced by this read route.

Two additional registered reads support human target selection and review work:

| Owner read under `/v1/workforce/hospitals/:hospital` | Authority and response                                                                                                                                                                                                                                                                                           |
| ---------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| POST `/person-review-targets/search`                 | Current identity manager and reviewed active actor link; strict body `{employee_code:""}` browses or a nonempty Staff ID performs case-insensitive exact lookup. Rows contain Staff ID/name, hospital/record IDs, employment status, source Staff version and creation time. Inactive joiners remain selectable. |
| GET `/person-review-inbox`                           | Current identity manager/reviewer and reviewed active actor link; defaults to recorded `pending`, with one explicit `status` from the existing review statuses. Rows contain review/Staff/hospital IDs, current Staff ID/name, recorded status/version/validity and creation time.                               |

Both use the existing cursor envelope (default 50/max 200), mandatory original
workspace assertions, current held assignment plus supervision/actor locks and
per-disclosed-row audit. A cursor is a position, not authority; every SQL query
scopes the verified tenant and hospital. Clients reset paging on scope/filter
changes. Exact Staff ID terms belong only in the 1 KiB strict JSON body; omitted,
null, invalid and unknown input fields return 400. An unknown exact code gives an
empty authorized list. Missing authority/current actor returns 403; unavailable
authorization or bounded lock failure returns 503. SQL/audit failures return no
successful list. Lists do not authorize subsequent commands.

Summary rows omit private findings, policy, declared account associations, decision
reasons and evidence. Recorded status does not establish present effectiveness:
open the protected detail before review/decision. Migrations
`0056_workforce_person_review_browse` and
`0057_workforce_person_review_inbox_index` add purpose-scoped reads and the
concurrent hospital/status/time keyset index. Existing WITH CHECK expressions,
forced RLS, one permissive policy and deletion restrictions remain. Browse context
does not confer profile, document, review-history or contributor access and does
not permit Staff edits or review transitions. The owner clients
`searchPersonReviewTargets` and `listPersonReviewInbox` validate canonical rows,
original hospital, requested Staff ID/status, pagination consistency and cancellation.

`POST /v1/workforce/hospitals/:hospital/identity-staff/:id/person-review-evidence/search`
now supports manager preparation. Its strict 1 KiB body pins `staff_version`,
`employment_epoch` and `identity_hash`; all are required. Only the current scoped
identity manager with an active reviewed actor link can select metadata. Original
workspace assertions and live identity refresh precede held source/actor checks.
A changed basis returns 409. The paginated projection includes file/hospital/Staff
IDs, record version, filename, MIME, size and creation time, excluding storage
keys, object generations and uploader/account associations. Only scan-released
PNG/JPEG enrollment identity evidence for that Staff/source version is listed.
Migration `0058_workforce_person_evidence_selection` extends one permissive
read policy with an explicit selection context, preserving old WITH CHECK and
delete guards; it grants no generic document/upload/classification/contributor or
binary authority. Per-row audits commit with the read. Listing scan release does
not establish person identity or complete contributor provenance; submission still
freezes and revalidates the evidence. `searchPersonReviewEvidence` verifies scope,
source version, canonical fields, pagination and cancellation through Global API.

The `/workforce/person-reviews` UI candidate now supports manager Staff-ID lookup,
current identity-basis review, purpose-bound evidence selection and mandatory
policy/finding/date/validity/association attestations. Digital account intent is an
explicit unverified account declaration, never provisioning or linking. No default
validity or policy is fabricated. Local validity input is converted to an instant;
observation dates remain explicit UTC dates. Source change/refusal clears private
context; uncertain submissions retain the same review ID, command, source and
payload until a verified receipt. The inbox/detail supports independent reviewer
approval/rejection, submitting-manager withdrawal and reviewer revocation.

Protected immutable history now uses `listPersonReviewHistory`: canonical, ordered
receipts bind the original review/Staff/hospital/workspace, reject duplicate or
out-of-order versions and preserve opaque continuation/cancellation. The history
UI labels historical receipts separately from effectiveness. Seeing a newer
receipt disables new actions and evidence release until the finding is reopened;
that restriction survives paging back and closing history. Current detail is
labelled as the finding when opened; mutations still require fresh owner authority
and exact version. History does not resolve an uncertain command or grant access.
Positive normal-browser/scanner approval and withdrawal/rejection/revocation are
recorded in the local checkpoint above. Staff-specific collection/filtering,
remaining browser failure/concurrency/performance gates and employment-review UI
acceptance remain open.
The evidence route authorizes only its review's frozen file, never general document downloads.
It pins the original assignment, actor/link/employment epoch, review revision,
identity source/epoch and full file/scan/classification lineage. IAM assignment or
actor-link replacement does not authorize an in-flight disclosure; a fresh request
is required. Storage runs outside database locks with a five-second budget capped
by session idleness, followed by fresh held owner authorization and an attributable
disclosure audit before bytes are returned. Missing/inactive actors return 403,
while genuinely invisible target objects remain 404. HR intake alone cannot submit
or decide a person finding.

Source/evidence/finding revocation or revision changes during storage return 409
without bytes or a successful-download audit. Storage failure or corrupt content
returns 503. Global API enforces no-store responses; the owner also uses attachment,
nosniff and sandbox headers. The client validates the original review/workspace,
current effectiveness, bound file, type/length and SHA-256, and checks cancellation
before publishing a Blob. Published evidence metadata is bounded to the owner's
10 MiB file limit and 1–10 frozen files per review.

Submissions require the exact source Staff version, employment epoch and identity
hash; policy/finding references, UTC observation date and future validity instant;
explicit account intent and declared subject; positive human-examination and
known-association attestations; and 1–10 scan-released enrollment identity files.
For `non_digital`, explicitly send `declared_subject: ""`; omitted/null is invalid.
A digital subject is a recorded declaration, not independently verified account
ownership or an operational login link. The review preserves that distinction.

Frozen evidence includes record version, content hash, quarantine/released object
generations, scan attempt/signatures and classification revision. Bound RLS reads
exclude unrelated files, employment evidence and another tenant. Contributor
history cannot be appended after the original submission transaction; snapshots,
evidence and history are immutable. Existing files without sufficient attributable
identity-document provenance return an unresolved-source conflict; do not infer
legacy uploader identity or silently mark a finding successful. Prospective
provenance collection is limited to enrollment `identity_evidence`, preserving
benefit/tax and other evidence-owner authorization.

Submission and decision replays reauthorize the current actor and return the
original immutable receipt for the same normalized intent. Changed intent under
the command ID, reused IDs or stale versions return 409. Missing/null/invalid
required input returns 400; invisible objects 404; missing action or a
self/contributor reviewer 403; missing/changed workspace assertions 409; unavailable
authorization 503. Scoped command/lock budgets and current-owner-time checks
remain in force. Sensitive reads are audited as well as mutations.

Detail has `{ review, current_state }`. Persisted receipt status is `pending`,
`approved`, `rejected`, `withdrawn` or `revoked`; current state can additionally be
`expired`, `source_changed` or `evidence_unavailable`. An old approved receipt never
implies present eligibility. Human review remains a policy-attributable finding,
not automated government/biometric verification or proof that an undisclosed
second identity cannot exist. Historical target subjects and frozen contributors
remain excluded from approval even after operational links change.

Migration `0054_workforce_person_reviews` owns the head/history/evidence/contributor
storage and forced RLS; `0055_workforce_person_provenance_index` uses the owning
concurrent-index mechanism for the populated audit source. Neither has been applied
to the existing UI fixture or production. Minimized audit and identifier-only
`helivanta.in.workforce.person_review_changed.v1` outbox facts commit together.
The event carries hospital/Staff/review/revision IDs and version, with no human
finding, policy, account association, filename, content hash or decision reason.

See the [implementation design](../superpowers/specs/2026-10-31-pre-employment-person-review.md)
and [delivery plan](../superpowers/plans/2026-10-31-pre-employment-person-review.md)
for the outstanding manager/reviewer UI, remaining failure/concurrency, real-scanner/
browser and employment-generation acceptance work. Full A01–A12
remains open.

## Scoped HR intake (unreleased candidate)

Assignment administrators explicitly use
`GET /v1/iam/intake-assignment-proposals` for the intake family. Existing leave
and identity lists retain their original membership. All assignment proposal,
decision and history routes honor an optional original-account assertion as well
as the original-tenant assertion: a mismatched, empty or repeated supplied
`X-Helivanta-Expected-Subject` returns 409 `session_changed` before owner access
or mutation. The assignment SDK sends both original workspace assertions.
Legacy clients omitting the account assertion retain their existing admission;
this compatibility does not establish full old-client lifecycle acceptance.

IAM's independently reviewed `workforce.hr_intake.v1` assignment carries exactly
`workforce.enrollment.manage` and `workforce.documents.manage` at one hospital.
It creates no hospital manager/reader grant, global business permission, or
employment activation authority. The actor also requires a current independently
reviewed login link to active Staff. This is the existing actor policy; complete
reviewed employment-generation eligibility remains an A01–A12 release gate.

`GET /v1/workforce/hr-intake-scopes` scans at most 20 current actor-link candidates
per page and reauthorizes both permissions with held IAM guards and the reviewed
actor source. It returns hospital ID/name and explicit `can_prepare_staff` and
`can_manage_staff_documents` capabilities. Those fields are generated from
`contracts/workforce-directory.json`; they are discovery observations, never
receipts authorizing a subsequent command. Cursor scope binds the original
account and route; filtered empty pages retain continuation. Missing authorization
dependencies deny rather than report a successful empty capability list.

Discovery requires exact original `X-Helivanta-Expected-Tenant` and
`X-Helivanta-Expected-Subject` headers. Staff directory list/detail and enrollment,
employment preparation/evidence and Staff-file consumers select `hr_intake=v1`
and require the same original workspace. Unknown, empty or repeated projection
selectors return 400; missing workspace returns 409; missing current authority
returns 403; out-of-scope Staff returns 404; unavailable scoped authority returns 503. Legacy directory discovery remains unchanged without this projection.
Directory reads require current enrollment authority and enforce hospital RLS.
HR setup list/history reads also accept this projection under current enrollment
authority. They expose existing versioned choices for onboarding; the migration
retains the original write checks and restrictive deletion behavior. Intake
cannot maintain setup records, including when an internal master-context flag
is present. Selecting a department revision uses the existing enrollment
reference checks and preserves the owner-resolved label after reload.
Status activation/change through intake returns 409 `employment_review_required`.

Private file operations retain original credential, assignment and reviewed actor
references across storage I/O; a newly approved replacement cannot authorize the
old request. Files stay quarantined until the separate worker's scan/release.
Hospital discovery and owner API tests use real IAM/Postgres/OpenFGA; protocol
storage fixtures do not establish real-scanner or browser acceptance. The assignment UI verifies the explicit frozen server catalog before offering
Staff onboarding assignments; missing or incompatible definitions stay
unavailable. The intake user workspace, full lifecycle enforcement, real-scanner
and normal browser/deployed acceptance are still required before release.

## Reviewed account links (worktree implementation)

Workforce owns the hospital-local binding and immutable review history; IAM owns
login membership and application access. A link never grants a permission or
clinical authority. This prerequisite for #1132/#1053 does not implement scoped
IAM assignments or self-service leave. See the [binding design](../superpowers/specs/2026-10-27-staff-login-links.md).

The base path is `/v1/workforce/hospitals/{hospital}/staff/{staff}/login-links`,
reached by web clients through Global API. All operations require verified tenant
membership and a current hospital manager grant. The existing tenant administrator
has the two new administrative permissions; other system roles do not gain them.

| Method and suffix       | Permission                                                  | Behavior                                                                                                                     |
| ----------------------- | ----------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `PUT /{link}`           | `workforce.login_links.manage`                              | UUIDv7 command ID; `{subject, reason}` proposes an active staff member's immutable account binding                           |
| `POST /{link}/decision` | `.review` for approve/reject; `.manage` for withdraw/revoke | `{expected_version, decision, reason}`; independent review excludes proposer and target subject; withdrawal is proposer-only |
| `GET /`                 | Either permission                                           | Private, audited cursor page                                                                                                 |
| `GET /{link}/history`   | Either permission                                           | Immutable version history; private, audited cursor page                                                                      |

Bodies are limited to 8 KiB. Account subjects are opaque, case-sensitive strings
of 1–200 characters, without whitespace, NUL or `*#:`; clients reject malformed
subjects without trimming them. Reasons are required and limited to 500 characters.
Generated models come from `contracts/workforce-login-links.json`. Lists use the
platform default 50 / maximum 200 rows and return `Cache-Control: no-store`.
The client sends `X-Helivanta-Expected-Tenant`; if supplied, exactly one value must
match the verified tenant or the handler returns 404. It never selects a tenant.

Statuses are pending, active, rejected, withdrawn and revoked. Only one pending
or active link may exist per staff member or per subject within a hospital.
Inactivation revokes pending/active links atomically, including through enrollment;
reactivation does not restore them. Reusing a proposal ID with different content,
conflicting bindings or stale decisions returns 409 `link_conflict`. Identical
immediate decision retries preserve the committed version; proposal and approval
retries still recheck candidate membership. A removed candidate returns 404 and an
unavailable membership dependency returns 503 `identity_unavailable`. Refetch
authorized history after an ambiguous outcome. Missing permissions return 403;
foreign/missing objects return 404. The binding cannot authorize self-service.

`helivanta.in.workforce.login_link_changed.v1` carries only `hospital_id`,
`staff_id`, `link_id`, `version` and `status`. Account subjects, names and reasons
stay behind protected reads. State, history, minimized audit and outbox commit
together. The additive migration preserves records and blocks inactive staff with
current links; older writers that omit revocation fail this guard after links exist.
No live migration or deployment is established by this document.

## Authentication and scope

Use an existing Helivanta session obtained through the Zitadel login flow. Every route checks the session and current OpenFGA tenant membership. The session's tenant is authoritative; request bodies cannot set tenant, actor or role. Unknown JSON properties are rejected. No route creates a tenant or a Zitadel identity.

The public gateway prefix is `https://helivanta.app/api/v1`; Istio rewrites it to the service's `/v1` prefix used below.

Tenant creation remains with the existing onboarding service's `POST /v1/orgs` contract (name, owner email, requested registered products). Its returned tenant ID must be used by the Helivanta tenant/membership projection. The operator created YES HOSPITAL. Its active synthetic owner is now `yes-hospital-test-owner@example.invalid` (Zitadel v2 initialization verified); the original INITIAL identity is retained without organization membership. The existing bootstrap command granted the active owner HMS tenant_admin. Automatic product registration and membership projection remain pending. A product grant in Zitadel is not automatically a Helivanta hospital grant. See [deployment evidence](../deployment/workforce-release.md) for the verified identifiers. Never manually invent a live tenant ID or bypass the operator with database inserts.

| Method and path (under `/v1`)                                 | Authority                            | Result                                                          |
| ------------------------------------------------------------- | ------------------------------------ | --------------------------------------------------------------- |
| `PUT /workforce/hospitals/{hospital}`                         | `workforce.hospital.manage`          | Create/update hospital metadata; 200                            |
| `PUT /workforce/hospitals/{hospital}/access/{subject}`        | `workforce.hospital.manage`          | Set explicit staff access to `reader` or `manager`; 200         |
| `DELETE /workforce/hospitals/{hospital}/access/{subject}`     | `workforce.hospital.manage`          | Revoke staff access; 204, also if already absent                |
| `PUT /workforce/hospitals/{hospital}/staff/{id}`              | Hospital `manager` grant             | Create/replace hospital-local staff record; 200                 |
| `GET /workforce/hospitals/{hospital}/staff/{id}`              | Hospital `reader` or `manager` grant | Staff record; 200, inaccessible/missing record 404              |
| `GET /workforce/hospitals/{hospital}/staff?limit=50&cursor=…` | Hospital `reader` or `manager` grant | Cursor page; inaccessible/missing hospital yields an empty page |

Hospital and staff IDs are non-nil UUIDs. Subject is the immutable identity subject, not an email address. Access grants do not create identity accounts or grant tenant membership. Tenant administrators can explicitly administer grants, but have no implicit staff-data access. Job category and employment status never confer permissions. Inactivating an employment record does not revoke a separate login or grant; access revocation is explicit in this slice.

POST is intentionally absent: clients choose UUIDs once and retry the same PUT URI after a timeout. Identical retries keep the original timestamps and do not add a second mutation audit. Changed PUTs replace the listed mutable fields; concurrent different updates currently use last-commit-wins semantics. Legacy consumers retain last-commit-wins behavior; the basic Staff editor uses the opt-in versioned directory contract below. Codes are unique within a hospital and can repeat in isolated sibling hospitals.

The basic directory UI follows the same explicit hospital manager grant as the
owner API. It does not require an additional `workforce.staff.manage` permission
(which is not registered). Private enrollment remains separately gated by
`workforce.enrollment.manage` and the hospital manager grant. A reader grant
never permits basic Staff writes, including when global enrollment permission
is present.

## Versioned basic directory editing

Design: [safe basic editing](../superpowers/specs/2026-10-29-staff-directory-safe-editing.md).
GET/PUT on the basic Staff resource may opt into `directory_context=v1`.
Exactly one expected-tenant and expected-subject header must match the verified
session; missing/mismatched/duplicate assertions return `409 session_changed`.
Responses add `version` and `directory_context_version: "v1"`. Legacy responses
omit both. The basic editor requires this acknowledgment and matching hospital
and Staff IDs before opening the form; a successful list read is insufficient.

Projected PUT requires the same body marker plus `expected_version`, a safe
nonnegative integer (0 for creation). Canonical metadata is generated from
`workforce-directory.json`. Missing/null/invalid metadata, duplicate/unsupported
projection and metadata without projection return 400. Older strict owners
reject the new fields before mutation. All basic GET/PUT operations now also
honor optional account assertions without the projection.

The owner locks the current hospital manager grant and Staff row before changing
state. A stale different edit returns `409 staff_version_conflict` without writes.
An identical current value is an authorized replay: timestamps, revision and
audit count remain unchanged. Concurrent creation under one Staff UUID cannot
overwrite another payload. Legacy PUT still replaces its four fields; it cannot
claim the new optimistic guarantee. Other protected enrollment APIs retain their
own documented contracts.

On ordinary conflicts/errors the UI keeps the draft. Reloading requires explicit
discard confirmation and fetches a newly acknowledged snapshot. Account/access
loss removes the workspace. Inactivation still revokes reviewed Staff links in
the existing transaction; it is not suspension, termination, contractor expiry,
rehire or complete cross-owner offboarding.

## Payloads

Hospital:

```json
{ "name": "Synthetic Hospital" }
```

Explicit grant:

```json
{ "level": "manager" }
```

Staff:

```json
{
  "employee_code": "SYN-EMP-001",
  "display_name": "Synthetic Staff",
  "job_category": "nurse",
  "employment_status": "active"
}
```

Staff responses add `id`, `hospital_id`, `created_at` and `updated_at`. Timestamps are RFC 3339. Responses omit tenant credentials, login bindings and unrelated HR details. List envelopes use `data` and `page: {next_cursor, has_more}`. Default limit 50, maximum 200. Invalid cursors/limits and payloads return 400; duplicate employee codes return 409. Missing authentication returns 401, absent current tenant membership or hospital-administration permission returns 403, and unavailable authorization returns 503. Internal failures return a generic 500. Existing platform error envelopes are `{error, message}` with request correlation in the response headers.

Request bodies are capped at 8 KiB. Hospital name and staff display name allow 1–150 nonblank characters. Employee codes allow 1–40 ASCII letters, digits, underscores or hyphens. Local job categories are `doctor`, `nurse`, `pharmacist`, `lab_technician`, `receptionist`, `administration`, `other`; employment status is `active` or `inactive`.

## Legacy reference and international mapping

Read-only inspection on 2026-10-13 confirmed distinct HR MANAGEMENT, ADMIN and ACCESS CONTROL areas. HR navigation separates Employees and Consultants, shift allocation, leave requests/allocation, duty/overtime adjustments, missing punches, attendance, performance and payroll. The employee directory exposes name/code search and department, employee status, grade, category and user-type filters. Existing employee personal/contact/employment/qualification/experience/dependent/document tabs were subsequently reviewed on 2026-10-14; blank enrollment validation and consultant detail differences remain unverified. See ADR 0015 for the complete field mapping. No staff rows or exports were copied.

| Observed legacy concept                            | Helivanta treatment                                                                                                   |
| -------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| Employee name and code                             | `display_name` and hospital-local `employee_code` in this slice                                                       |
| Employee status                                    | Explicit `active` / `inactive`; legacy status vocabulary still requires mapping before import                         |
| Employees versus consultants                       | Protected enrollment engagement type, separate from clinical profession and permissions                               |
| Department, grade and category                     | Enrollment department, designation and grade; future versioned HR taxonomy. No inferred equivalence to `job_category` |
| User type                                          | Identity/access concept requiring a verified mapping; never automatically grants authority from a personnel field     |
| Shifts, leave, attendance, performance and payroll | Separate later workflows and protected HR data; not represented as generic staff attributes                           |

We use the legacy as workflow evidence, not a schema to reproduce. This foundation leaves room for international names (a display label does not require a Western given-name/family-name split), separates opaque IDs from local employee codes, uses explicit tenant/hospital scope and stores UTC-aware timestamps. The following are conceptual interoperability mappings, not implemented FHIR endpoints or a conformance claim:

| Helivanta concept                    | Standards reference / next modeling step                                                                                                                                  |
| ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Customer tenant                      | SaaS security boundary; not automatically a FHIR clinical Organization                                                                                                    |
| Hospital                             | [FHIR Organization](https://hl7.org/fhir/R4/organization.html); physical departments/wards require separate [Location](https://hl7.org/fhir/R4/location.html) records     |
| Staff personal/professional identity | [FHIR Practitioner](https://hl7.org/fhir/R4/practitioner.html) for clinical practitioners; not every employee is a practitioner                                           |
| Hospital-local job assignment        | [FHIR PractitionerRole](https://hl7.org/fhir/R4/practitionerrole.html) for applicable clinical assignments; terminology system/code must be explicit when exchanging data |
| Employee code                        | Local business identifier scoped to its issuer/hospital; not a medical registration licence or global identifier                                                          |
| Login identity and hospital grant    | Zitadel subject and authorization facts, separate from Practitioner/PractitionerRole and from job category                                                                |

Verified follow-up field mapping should cover structured names, preferred name, work contacts, department/location, employment periods, multiple assignments, professional identifiers with issuer/country, qualifications and credential validity. Personal addresses, government identifiers and payroll require separately authorized HR records rather than this shared directory. Country, timezone, address and language conventions must be explicit when those fields are introduced. Sharing and identity linking need their own authorization and review; matching employee codes across hospitals does not establish either.

## Verification and release

Tests exercise the HTTP handlers with real PostgreSQL and NATS, plus a network HTTP test with signed Helivanta sessions and real OpenFGA membership revocation. The broader permission matrix checks the new administration permission against OpenFGA. These do not substitute for a live Zitadel login and post-deployment smoke test.

Migration `0001_workforce` creates four forced-RLS tables plus grant predicates and audit triggers. It does not change old clinical tables. Mutation audit is atomic and append-only for application roles; personal field values are excluded. Staff removal and table truncation are rejected; use inactive status. No patient or legacy data is migrated.

Production rollout uses the existing image pipeline and `helivanta-api` Argo CD application with the matching migration binary. See [release checklist](../deployment/workforce-release.md) for current observations and remaining gates.

## Protected enrollment

`GET` and `PUT /v1/workforce/hospitals/{hospital}/staff/{id}/enrollment` require `workforce.enrollment.manage` (tenant administrator initially) **and** a hospital manager grant. Ordinary directory managers/readers cannot access private profiles. Foreign-tenant/hospital objects return 404; a missing route permission returns 403. Every protected GET is audited. No personal profile is included in directory lists, outbox events, error bodies or persisted browser caches.

The current Staff workspace opts into `?request_context=v1`. Both reads and writes
then require exactly one `X-Helivanta-Expected-Tenant` and
`X-Helivanta-Expected-Subject`, matching the verified account. A missing or mismatched
assertion returns `409 session_changed` before private reads or writes. These
assertions never select a tenant or grant permission. Enrollment, directory lists, HR masters and identity handlers also honor
assertions when supplied without the projection. Existing document and account-link
workspace mismatches retain their opaque 404 contract. Duplicate, empty or
unsupported projection values return `400 invalid_request`.

A projected PUT additionally carries `request_context_version: "v1"` in its JSON
body. The query and body marker must agree; a marker without the query is invalid.
Older owners reject the unknown body field before mutation, including when a GET
in a mixed deployment reached a newer owner. Successful projected responses
acknowledge `request_context_version: "v1"`; legacy responses omit it. Legacy
requests without projection/marker preserve optional-header compatibility.

The browser captures the account before loading Staff hospitals, pins enrollment
and choice requests to it, and requires the acknowledgment and matching Staff ID
before displaying private fields. Account/scope/access loss closes the private
workspace; reopening resolves the current account explicitly. Ordinary optimistic
conflicts and temporary failures retain the draft. Directory Add/Edit also require
the corresponding Staff/enrollment permission in addition to hospital management;
document-only roles retain their independent document controls.

PUT carries `expected_version`, the existing `staff` directory fields, `profile`, and bounded arrays `addresses` (2), `emergency_contacts` (5), `credentials`, `qualifications`, `experiences` (20 each). Use version 0 for a new UUID; an edit uses the response `version`. Identical retries succeed without changing version/timestamps or appending an audit/event, even after the original write committed. Different stale content returns 409; reload explicitly rather than overwriting another editor. Staff codes are unique case-insensitively within their tenant and hospital. Child UUIDs must be stable across retries; duplicate IDs/natural keys are rejected.

The profile supports structured/preferred names, optional DOB/gender, separate work/personal email, E.164 mobile/work phone, communication language, optional **reported** blood group, engagement type, department/designation/grade, same-hospital supervisor and employment/probation/confirmation/end dates. Addresses use country/region/postal fields. Emergency contacts have name, relationship, international phone, optional email and distinct priorities 1–5. Credentials carry type/number/issuer/country/specialty and validity. Qualifications and prior employment are repeatable. No government ID, salary, scanned document URL or clinical verification fields are accepted.

All private tables have forced tenant/hospital RLS and composite staff/supervisor foreign keys. The new `helivanta.in.workforce.enrollment_changed.v1` event contains only hospital ID, staff ID and revision, and is inserted in the same transaction through the existing outbox/JetStream pipeline. Private data is not cached in Valkey. Temporal is reserved for a later multi-step review/upload/onboarding workflow; simple enrollment is one PostgreSQL transaction.

Field definitions are shared through `contracts/workforce-enrollment.json` and `scripts/generate-workforce-enrollment.mjs`, which generate backend field types and frontend schemas. Run `node scripts/generate-workforce-enrollment.mjs --check` to detect drift. Applied SQL migrations remain immutable; future database changes get a new migration.

See [ADR 0015](../adr/0015-staff-enrollment.md), [editable diagram](../diagrams/staff-enrollment.drawio), and `scripts/verify-staff-enrollment.mjs` for repeatable designated-synthetic-tenant verification. This enrollment release does not complete payroll, rosters, leave, dependent benefits, jurisdiction-specific IDs, document quarantine/release integration, legacy import or all of #1035/#979.

Enrollment supervisor links form an acyclic hospital-local reporting chain.
The owner command serializes graph changes before staff locks; an additive
PostgreSQL constraint also protects direct writes. A circular chain returns 409
`supervisor_cycle` with no reporting-path disclosure, version change, success
audit or outbox event. Clearing a supervisor can repair an invalid chain. A busy
supervision lock returns 503 `supervision_busy`; retain the draft and retry after
refresh. Unsupported snapshot isolation modes fail closed with 503
`supervision_unavailable`; non-null supervisor writes require PostgreSQL
read-committed semantics. Supervisor selection grants no access or clinical authority. Migration
`0039_workforce_supervision` is included in the owner registry deployed with
`e0a9e6af4a56ccb796a82e06914ef654490b030b`; that rollout's migration job succeeded.
This is rollout evidence, not a per-migration database audit. Existing-cycle
review and correction must use attributable HR commands. Inactive supervisor
choices are labeled; profile relationships do not implement dated postings or
employment offboarding.

## Versioned HR setup

Design: [ADR 0016](../adr/0016-versioned-hr-masters.md). Diagram: [editable draw.io](../diagrams/versioned-hr-masters.drawio).

| Endpoint                                                         | Authorization                                                 | Behavior                                                                                                     |
| ---------------------------------------------------------------- | ------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `GET /workforce/hospitals/{hospital}/hr/masters?kind=department` | Tenant membership and explicit hospital reader/manager grant  | Keyset-paginated current records; optional allowlisted category                                              |
| `PUT /workforce/hospitals/{hospital}/hr/masters/{id}`            | `workforce.master.manage` and explicit hospital manager grant | Stable client UUID; normalized unique code/name; optimistic version; immutable revision on meaningful change |
| `GET /workforce/hospitals/{hospital}/hr/masters/{id}/history`    | Tenant membership and explicit hospital reader/manager grant  | Keyset-paginated immutable revision history                                                                  |

PUT input: `expected_version` (0 for creation), `kind`, `code` (1–40 ASCII letters/digits/underscore/hyphen), `name` (1–80 characters), `description` (0–500), `active` (boolean). Code/category cannot change. Name whitespace is collapsed; code is uppercase. Duplicate normalized name/code returns 409, even when a record is retired. An identical retry preserves revision/timestamps and emits no new event. A different stale edit returns 409. Invalid fields return 400; inaccessible objects return 404. Tenant/hospital identity and unknown JSON fields are rejected. All responses explicitly use `Cache-Control: no-store`.

Categories: department, designation, grade, qualification, document_category, exit_reason, relationship, language, contract_type. This does not implement salary, tax, leave policy or KRA/KPI models. Retire/reactivate with `active`; no delete endpoint exists. List/history support the shared cursor and limit contract.

Profile connections also accept optional `preferred_language_revision_id`,
`exit_reason_revision_id`, `contract_type` (0–80 characters) and
`contract_type_revision_id`. Language/exit-reason text remains compatible with
legacy requests. Contract type is separate from engagement type and is a
server-derived classification requiring its typed master reference when set;
it does not activate employment, access, payroll rules or legal contract terms.
The new fields can be omitted without clearing existing pins; explicit empty
reference strings clear them. Null is rejected for these new reference fields
and the new contract-type text. Existing reference null compatibility is
unchanged. Owner lookup derives labels and rejects new inactive/stale references;
existing historical pins remain editable during unrelated enrollment changes.

The default GET/PUT enrollment response retains its original profile shape for
older strict editors. Updated clients request `?profile_masters=v1` on both
methods to receive the four extension properties and top-level
`profile_masters_version: "v1"`. An empty, unsupported or repeated selector
returns 400 before any enrollment write. The selector changes representation,
not authority, stored facts, concurrency or retry behavior. Omitted input
references preserve pins even when the legacy response hides them. The updated
editor checks the response marker and separates its versioned cache key; an
older API that ignores the selector produces an explicit unavailable view,
without exposing an editable profile with unknown pins shown as blank.

Migrations `0040_workforce_profile_masters` and
`0041_workforce_profile_master_indexes` add typed tenant/hospital/category foreign
keys, concurrent reference indexes and a distinct older-writer guard. This is
source behavior, not a production deployment claim. Older binaries cannot use
the existing generic HR-reference marker to erase the new pins. Deploy the
schema and updated API fleet before exposing the new fields; retain the columns
and guard on application rollback. No existing free text is bulk matched.

Enrollment profiles can select `department_revision_id`, `designation_revision_id`, `grade_revision_id`. Foreign-key scope includes tenant, hospital and category. The server derives the assignment name from the immutable revision; supplied label text cannot forge a snapshot. New assignments must select the active current revision. An existing assignment may retain its older/retired revision. Explicit empty reference clears it; omitted reference fields from older clients preserve an existing assignment. Free-text legacy assignments remain supported without silently matching names. A database guard refuses older API writers attempting to remove a pinned profile during rolling deployment.

Qualification rows can select optional `qualification_revision_id`, restricted to
the same tenant, hospital and `qualification` category. The server derives the
qualification label from that immutable revision. New selections require the
active current revision; existing historical pins remain valid. Omitting the
field preserves the stored reference **by the existing qualification row ID**;
an explicit empty string clears it and permits historical free text. Null and
malformed UUIDs return 400. Changing/removing a row ID is a row replacement/removal,
not an instruction to copy another row's reference. Qualification row IDs and
their existing private-evidence anchors are retained on ordinary amendments.
Duplicate qualification/institution/year combinations are checked again after
canonical labels resolve, before any staff write or audit/outbox publication.

Default enrollment responses, including `?profile_masters=v1` alone, retain the
original qualification shape for older strict editors. Request
`?qualification_masters=v1` on GET/PUT to receive `qualification_revision_id` and
top-level `qualification_masters_version: "v1"`. The two selectors are independent
and may be combined. Empty, unsupported or repeated selectors return 400 before
writes. Updated editors require both response markers and version their query
cache; a profile-aware API without qualification support cannot open a writable
existing record. Neither selector grants enrollment/hospital authority.

Migrations `0042_workforce_qualification_masters` and
`0043_workforce_qualification_master_index` add a typed composite foreign key,
constant category check, concurrent reference index and a distinct older-writer
guard. Intermediate profile-aware binaries cannot delete or update pinned
qualification rows using their existing profile support marker. Deploy schema
and updated API fleet before exposing these controls; preserve the columns and
guard on application rollback. This is source behavior, not evidence of
production deployment. No new route, permission or event field is introduced.

Emergency-contact relationship integration retains the existing 60-code-point
consumer limit and the HR master's 80-code-point owner limit. A valid HR master
with a 61–80-code-point name remains available in HR setup but cannot be newly
assigned to an emergency contact: the selector explains its unavailable state,
and the server rejects the assignment before writes. Names are never truncated,
and v1 is not widened. Existing free-text relationships remain supported.
Optional `relationship_revision_id` binds a same-tenant/hospital `relationship`
revision; historical pins and older-client omission are preserved by contact row
ID. This is contact information, not clinical authority or next-of-kin consent.

Request `?contact_masters=v1` on enrollment GET/PUT to receive contact reference
fields and `contact_masters_version: "v1"`. This selector is independent of the
profile and qualification selectors; any combination is valid. Default and
older opt-in responses retain their original contact shape. Empty, unsupported
or repeated selectors return 400 before writes. Updated editors require all
three response markers and a distinct cache identity. Omitted contact references
preserve the existing row's pin; explicit empty clears it, null fails, and a
different staff record cannot inherit a pin merely by supplying the same child
ID. Supplied relationship labels are replaced by the scoped immutable revision's
label before the consumer limit is checked.

Migrations `0044_workforce_contact_masters` and
`0045_workforce_contact_master_index` add the typed tenant/hospital/category
reference, constant category check, concurrent index and a distinct contact
writer marker. A qualification-aware older binary cannot delete/update a pinned
contact with its existing support markers. Apply schema and updated APIs before
exposing the UI; retain columns/guard on application rollback. No new permission,
route or event field is introduced, and this source contract is not a production
deployment claim.

`helivanta.in.workforce.master_changed.v1` contains hospital/master/revision UUIDs, category and revision number only. Audit, master head, immutable revision and outbox intent commit together. Neither employee attributes nor master labels are broadcast. The existing JetStream relay publishes; no Valkey caching or Temporal workflow is required for this transaction.

Synthetic verification: run `scripts/verify-hr-masters.mjs` with authenticated verification scope supplied privately via the same variables as `verify-staff-enrollment.mjs`. Its stable reserved IDs/codes are separate from manual browser fixtures. It restores its own synthetic baseline through versioned API operations on repeat runs and does not delete records.

## Staff document workspace assertions

All ten Staff enrollment/credential/qualification document routes use the shared
Staff principal precondition: record choices, document list/registration,
upload/download, retry/revocation and classification current/history/writes.
Optional `X-Helivanta-Expected-Tenant` and `X-Helivanta-Expected-Subject` assertions
pin the client workspace to the verified principal. A supplied empty, malformed,
repeated, comma-joined or mismatching assertion returns `409 session_changed`
with `Cache-Control: no-store`, before object/body/storage/transaction work.
Headerless clients retain their behavior; matching assertions confer no authority.
Current permission, hospital manager, exact Staff/file/purpose and scan/release
checks remain mandatory, including final owner reauthorization after storage I/O.
The updated Staff UI and credential-evidence selector inherit the original
verified Staff account; every document request sends both assertions. Document
and classification caches and form keys include the subject. A same-tenant
account change aborts pending file preparation, and a session/scope denial hides
private state and returns the parent Staff workspace to verification. Uncertain
commands remain retryable only inside their original account context.
Actual foreign tenant/hospital/object scope still returns 404; missing permission
returns 403.

This candidate explicitly changes the old expected-tenant mismatch response from
404 to the shared 409 precondition response and closes ignored expected-account
and ambiguous-header cases. The response is independent of target existence.
It changes no route, permission, schema, event or file-release policy. It is not
proof of employment cutoff or deployed acceptance. See the
[design and compatibility note](../superpowers/specs/2026-10-31-staff-document-workspace-binding.md).

## Staff file classification

Classification is optional versioned HR metadata beside an existing staff file.
The fixed evidence `category` and owner record binding, upload intent, bytes,
scan/release state and download authorization are unchanged. Hospital-defined
`document_category` masters cannot replace those security-purpose values.
Benefits and tax evidence are excluded from these staff classification routes.
PR #1411 merged after local owner/reviewer UI checks and full local and remote
gates passed. Production rollout and deployed acceptance remain open. See [ADR 0045](../adr/0045-staff-document-classification.md).

| Method | Owner-relative route                                                            | Authority / behavior                                                                                                        |
| ------ | ------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| GET    | `/workforce/hospitals/{hospital}/staff/{staff}/documents/{file}/classification` | `workforce.documents.manage` and explicit hospital manager grant; exact staff/file binding; protected metadata read audited |
| PUT    | Same                                                                            | Same authority; optimistic versioned amendment or explicit clear                                                            |
| GET    | Same + `/history`                                                               | Same authority; bounded standard cursor pagination; immutable revisions and read audit                                      |

Route traffic uses the existing Global API product destination. Inaccessible
objects return 404; a missing route permission returns 403. Current output is
`file_id`, `hospital_id`, `staff_id`, `version`, `classification_revision_id`,
`classification_name` and nullable `updated_at`. An authorized file with no
classification returns version 0, empty reference/name and null timestamp.
History adds its immutable `id`, `actor_subject` and `created_at` in place of
`updated_at`; all response IDs retain verified owner scope.

PUT requires exactly `expected_version` (safe nonnegative integer) and
`classification_revision_id` (nonzero UUID or explicit empty string). Null,
omission, unknown fields and bodies above 1 KiB return 400. Labels are derived
from same-tenant/hospital immutable `document_category` revisions. New selections
require the active current revision; an unchanged retained historical reference
remains valid after retirement. An empty reference clears the selection while
preserving history. No legacy name is matched automatically.

An unchanged current request is a no-op. An exact retry against the immediately
preceding expected version returns the same response; other stale commands return 409. First writes and amendments serialize on the authorized parent file lock.
Migration `0046_workforce_document_classification` adds scoped current/history
tables, typed master/file foreign keys, forced RLS, immutable history, monotonic
head guards and reference/history indexes. Old upload clients cannot erase the
separate metadata. Retain these tables on application rollback.

Each meaningful amendment commits history, attributed audit and
`helivanta.in.workforce.document_classification_changed.v1` in the owner outbox.
The payload contains only hospital, staff, file, classification-receipt revision
IDs and metadata version; no category selection, label, name, filename, file
content or storage location. This changes no credential, identity, background,
consent, clinical privilege or employee outcome.

## Recovery of stalled private-file processing

`PUT /workforce/hospitals/{hospital}/staff/{staff}/documents/{file}/retries/{attempt}`
requires `workforce.documents.manage` and an explicit current hospital manager
grant for the exact staff/file. The same recovery rule applies behind the existing
payroll/benefits evidence routes, with their own permissions, parent revision and
pending/frozen checks. Frozen evidence cannot change its attempt, including a
pending-to-pending transition after the deadline. This command does not review identity, approve evidence or
release bytes.

The attempt is a canonical integer of at least 2, exactly one above the stored
attempt. `scan_error` permits immediate retry. `scan_pending` permits recovery only
after 2700 seconds (45 minutes) since its committed `updated_at`, measured on the
database clock, matching the Temporal execution deadline. Recent pending, skipped
attempts and terminal files return 409. Inaccessible scope remains 404; decided
proposal evidence remains 409 `not_pending`. The database prevents rewriting an
unchanged pending attempt's waiting clock and stamps new queue/retry entry itself.

A new command returns 202; the exact repeated command returns its current receipt
with 200 after reauthorization, without another scan-request event. File lock,
monotonic attempt, state, audit and outbox commit atomically. A timed-out or late
worker for an old attempt cannot commit a release after recovery, even if storage
copy completed. Current bytes become downloadable only after the new attempt's
valid matching scanner/release proof. Dependency failure remains protected and
retryable; no direct Temporal restart or business-data patch is required.

The UI derives the recovery affordance from the existing timestamp. A client clock
skew can cause an early command to receive 409; it cannot shorten the server guard.
There is no new response field, purpose enum or permission. Migration
`0047_workforce_document_recovery` replaces the guard function without changing
file content, history, ownership or RLS. Old clients remain compatible but do not
offer recovery for expired pending files. See [ADR 0046](../adr/0046-staff-document-stalled-processing.md).
This development candidate has not been rolled out to production.

## Credential validity reviews

Design: [credential validity spec](../superpowers/specs/2026-10-20-credential-validity-design.md) (#1108 slice A). UI: Credentials on each staff row of `/workforce`, and `/workforce/credentials` for the register.

| Route                                                                 | Permission                                                 | Behaviour                                                                                                                                                                                                                                                                                                         |
| --------------------------------------------------------------------- | ---------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /workforce/hospitals/{hospital}/staff/{id}/credentials`          | `workforce.enrollment.manage` and hospital manager         | `time_zone`, `local_date` and each enrolled credential with its `reviewed` record, `status`, `days_remaining` and `pending` review; audited                                                                                                                                                                       |
| `GET …/staff/{id}/credentials/{credential}/reviews`                   | `workforce.enrollment.manage` and hospital manager         | Keyset pages up to 100 of every review, newest first; 404 for an unknown credential                                                                                                                                                                                                                               |
| `PUT …/staff/{id}/credentials/{credential}/reviews/{review}`          | `workforce.enrollment.manage` and locked hospital manager  | Proposes `expected_version`, `number`, `valid_from`, `valid_until`, `evidence_document_ids` (1–5 of this credential's scanned or scanning files) and `reason`; an identical replay returns the saved review; 409 `review_pending`, `version_conflict`, `nothing_to_change`, `review_conflict`, `evidence_invalid` |
| `POST …/reviews/{review}/decision`                                    | `workforce.credentials.review` and locked hospital manager | `expected_version`, `decision` (`approve`/`reject`), `note` (required to reject); 409 `self_review`, `evidence_unavailable`, `not_pending`, `version_conflict`, `credential_removed`                                                                                                                              |
| `POST …/reviews/{review}/withdraw`                                    | `workforce.enrollment.manage` and locked hospital manager  | `note`; proposer only, 409 `not_proposer`                                                                                                                                                                                                                                                                         |
| `GET /workforce/hospitals/{hospital}/hr/credentials?expiring_within=` | `workforce.enrollment.manage` and hospital manager         | Reviewed credentials, soonest `valid_until` first, optionally within 0–365 days of the hospital date; 409 `time_zone_unset` when filtering without a roster time zone; audited                                                                                                                                    |

Status is computed per request in the hospital roster time zone: `unreviewed`, `not_yet_valid`, `valid`, `expires_today`, `expired`, or `date_unknown` when no zone is set. The expiry day counts as valid. Once a credential is reviewed, an enrollment `PUT` that changes its type, number, issuer or dates, or removes it, answers 409 `credential_reviewed`. Responses are `Cache-Control: no-store`. Each proposal and decision publishes `credential_review_changed.v1`, and an approval also publishes `credential_validity_changed.v1` and `enrollment_changed.v1`, all through the outbox and carrying identifiers, status and version only.

## Credential renewal reminders

Design: [credential renewal reminders spec](../superpowers/specs/2026-10-18-credential-renewal-reminders-design.md) (#1108 slice B). UI: `/workforce/credential-reminders`.

| Route                                                                 | Permission                                                | Behaviour                                                                                                                                                     |
| --------------------------------------------------------------------- | --------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /workforce/hospitals/{hospital}/hr/credential-reminder-policy`   | `workforce.enrollment.manage` and hospital manager        | Current policy; `version: 0, thresholds: []` when reminders are off                                                                                           |
| `PUT /workforce/hospitals/{hospital}/hr/credential-reminder-policy`   | `workforce.enrollment.manage` and locked hospital manager | `expected_version`, `thresholds` (1–6 distinct whole days, 0–365), stored descending; identical thresholds return the current version; 409 `version_conflict` |
| `GET /workforce/hospitals/{hospital}/hr/credential-reminders?status=` | `workforce.enrollment.manage` and hospital manager        | `open` (default), `acknowledged` or `superseded`; keyset pages, soonest `due_on` first; never includes number or issuer; audited                              |
| `POST …/hr/credential-reminders/{reminder}/acknowledgement`           | `workforce.enrollment.manage` and locked hospital manager | Records the acknowledging subject; idempotent for an acknowledged reminder; 409 `reminder_superseded`                                                         |

Each approved revision gets one Temporal schedule (`workforce-credential-reminders-v1` on the document worker), started by the `workforce-credential-reminders` consumer of `credential_validity_changed.v1`. Threshold _t_ is due when `valid_until − t` is on or before the hospital's local date; only the smallest due threshold is written, superseding larger open ones, and approving a new revision supersedes the old revision's open reminders. With no policy or no roster time zone nothing is written. Responses are `Cache-Control: no-store`. Policy saves publish `credential_reminder_policy_changed.v1`; reminder creation, acknowledgement and supersession publish `credential_reminder_changed.v1`, through the outbox with identifiers, status, threshold and version only.

## Dependents and benefit enrollment

Design: [dependents and benefits spec](../superpowers/specs/2026-10-18-dependents-benefits-design.md) (#1106 slice A) and [benefits screens spec](../superpowers/specs/2026-10-19-benefits-screens-design.md) (slice B), [benefit evidence and export spec](../superpowers/specs/2026-10-19-benefit-evidence-export-design.md) (slice C). UI: `/workforce/benefits`. Paths are relative to `/workforce/hospitals/{hospital}`.

| Route                                                                    | Permission                                               | Behaviour                                                                                                                                                                                                                                                                                                                                                                                     |
| ------------------------------------------------------------------------ | -------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/hr/benefits/plans`                                                | `workforce.benefits.view` and reader/manager grant       | Keyset pages up to 100 of current plans; audited                                                                                                                                                                                                                                                                                                                                              |
| `PUT …/hr/benefits/plans/{plan}`                                         | `workforce.benefits.manage` and locked hospital manager  | `expected_version`, `code`, `name`, `kind`, `description`, `covers_dependents`, `covered_relationships`, `effective_from`, `effective_until`, `status`. Unchanged retries return the plan; 409 `version_conflict`, `duplicate_plan`, `plan_retired`, `coverage_outside_plan`                                                                                                                  |
| `GET …/hr/benefits/plans/{plan}/history`                                 | `workforce.benefits.view` and reader/manager grant       | Revisions, newest first; audited                                                                                                                                                                                                                                                                                                                                                              |
| `GET …/staff/{id}/dependents`                                            | `workforce.benefits.view` and reader/manager grant       | Current dependents with relationship name; audited                                                                                                                                                                                                                                                                                                                                            |
| `PUT …/staff/{id}/dependents/{dependent}`                                | `workforce.benefits.manage` and locked hospital manager  | `expected_version`, `full_name`, `relationship_revision_id` (current, active `relationship` master), `birth_date`, `phone` (E.164 or empty), `status` (`active`/`removed`), `reason`; 409 `version_conflict`, `duplicate_dependent`, `dependent_covered`, `staff_inactive`                                                                                                                    |
| `GET …/staff/{id}/dependents/{dependent}/history`                        | `workforce.benefits.view` and reader/manager grant       | Revisions with their reason; audited                                                                                                                                                                                                                                                                                                                                                          |
| `GET …/staff/{id}/benefit-enrollments`                                   | `workforce.benefits.view` and reader/manager grant       | Enrollments with approved `coverage` and any `pending` proposal; audited                                                                                                                                                                                                                                                                                                                      |
| `PUT …/staff/{id}/benefit-enrollments/{enrollment}/proposals/{proposal}` | `workforce.benefits.manage` and locked hospital manager  | `expected_version`, `plan_id`, `dependent_id` (omit for the employee), `coverage_from`, `coverage_until`, `eligibility_basis`. An identical replay returns the proposal; 409 `proposal_pending`, `proposal_conflict`, `version_conflict`, `nothing_to_change`, `plan_retired`, `coverage_outside_plan`, `coverage_overlap`, `relationship_not_covered`, `dependent_removed`, `staff_inactive` |
| `POST …/proposals/{proposal}/decision`                                   | `workforce.benefits.approve` and locked hospital manager | `expected_version`, `decision`, `note` (required to reject); 409 `self_review`, `plan_changed`, `dependent_changed`, `evidence_scanning` and the proposal checks above                                                                                                                                                                                                                        |
| `POST …/proposals/{proposal}/withdraw`                                   | `workforce.benefits.manage` and locked hospital manager  | Proposer only; 409 `not_proposer`, `evidence_scanning`                                                                                                                                                                                                                                                                                                                                        |
| `GET …/staff/{id}/benefit-enrollments/{enrollment}/history`              | `workforce.benefits.view` and reader/manager grant       | Proposals and decisions, newest first; audited                                                                                                                                                                                                                                                                                                                                                |

Benefits tables need the `app.workforce_benefits` RLS scope, which only these handlers set, so enrollment, directory and payroll access never reads them. An enrollment keeps the plan revision it was first approved under. A retired plan may still shorten existing coverage. Events (`benefit_plan_changed.v1`, `dependent_changed.v1`, `benefit_enrollment_changed.v1`) and audit rows carry identifiers, status and version only. Every response is `Cache-Control: no-store`.

### Benefit evidence and coverage export

Eligibility evidence is a workforce document whose record is a coverage proposal (`record_kind` `benefit_proposal`, category `benefit_eligibility_evidence`). It uses the staff document pipeline for quarantine, malware scanning and release. Row-level security admits these rows only under the `app.workforce_benefits` scope, so staff document, enrollment and payroll routes never list, download or revoke them. Evidence paths are relative to `…/staff/{id}/benefit-enrollments/{enrollment}/proposals/{proposal}`.

| Route                                                          | Permission                                              | Behaviour                                                                                                                                                                                                                                                                                                                         |
| -------------------------------------------------------------- | ------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/evidence`                                               | `workforce.benefits.view` and reader/manager grant      | `{evidence, uploads_available}`: at most 10 files of that proposal, newest first. Expired intents are left out. 404 unless the proposal belongs to this enrollment                                                                                                                                                                |
| `PUT …/evidence/{file}`                                        | `workforce.benefits.manage` and locked hospital manager | `filename`, `content_type` (`image/png`/`image/jpeg`), `size` (≤ 10 MiB), `sha256`. 201 when created, 200 for an identical replay. 409 `not_pending` once the proposal is decided, `evidence_limit` for an 11th file                                                                                                              |
| `PUT …/evidence/{file}/content`                                | `workforce.benefits.manage` and locked hospital manager | Streams the bytes, verifies them against size and digest, and queues the scan (202). 409 `not_pending` once decided                                                                                                                                                                                                               |
| `PUT …/evidence/{file}/retries/{attempt}`                      | `workforce.benefits.manage` and locked hospital manager | Re-queues `scan_error` or `scan_pending` after the 45-minute server deadline. 409 `not_pending` once decided                                                                                                                                                                                                                      |
| `GET …/evidence/{file}/content`                                | `workforce.benefits.view` and reader/manager grant      | Released bytes of an `available` file. Audited as `workforce.benefits.evidence_downloaded`, `Cache-Control: private, no-store`                                                                                                                                                                                                    |
| `GET /workforce/hospitals/{hospital}/hr/benefits/coverage.csv` | `workforce.benefits.export` and reader/manager grant    | `as_of` (required, `YYYY-MM-DD`), `plan_id` (optional), each at most once. Approved coverage in force on `as_of`. `X-Export-Row-Count` and `X-Export-Sha256` describe the body. 400 `invalid_request` for unknown, repeated or empty parameters; 413 `benefits_export_too_large` above 1,000 rows or 8 MiB, before any file bytes |

A decision or withdrawal answers 409 `evidence_scanning` while any of the proposal's files is `scan_pending`. A decision freezes the proposal's evidence. Approval keeps available files readable as the record of what was approved, while rejection and withdrawal revoke them, each with a `document_changed.v1` event. The database refuses to unfreeze a file or scan it again.

No other benefits permission implies `workforce.benefits.export`. Each file writes one `workforce.benefits.exported` audit row in the same transaction, carrying the report id `workforce.benefits.coverage`, the filters, the row count and the SHA-256 of the bytes, never cell values. Rows hold the enrollment, employee code, name and employment status, the plan, who is covered (the employee, or the dependent's name, relationship and status), coverage dates, approved version and approval time. Birth dates, phones, eligibility bases, decision notes and evidence are excluded. Cells are neutralised against spreadsheet formulas. The audit records that a file was generated, not that it was opened or kept; exports above 1,000 rows and scheduled exports are not in this slice.

## Versioned shift definitions

| Route                                                        | Access                                                      | Behavior                                                                                                                  |
| ------------------------------------------------------------ | ----------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `GET /workforce/hospitals/{hospital}/hr/shifts`              | Authenticated tenant membership + explicit hospital grant   | Keyset pagination; optional allowlisted `shift_type` filter; private `no-store`                                           |
| `PUT /workforce/hospitals/{hospital}/hr/shifts/{id}`         | `workforce.roster.manage` + explicit hospital manager grant | Stable client UUID/code, normalized scoped uniqueness, expected version; unchanged retries preserve revision/audit/outbox |
| `GET /workforce/hospitals/{hospital}/hr/shifts/{id}/history` | Authenticated tenant membership + explicit hospital grant   | Immutable numbered snapshots, bounded keyset history                                                                      |

Input: `expected_version` (0 to create), `code`, `name`, `description`, `shift_type` (`day`, `evening`, `night`, `rotating`, `on_call`), `active`, `start_time`, `end_time` (strict `HH:MM`), explicit `break_minutes`, `grace_minutes`, `full_day_ot_minutes`, `half_day_ot_minutes`, and `locum`. All fields are allowlisted; body-supplied scope/unknown fields are rejected. Code cannot change. Response adds `id`, `revision_id`, `version`, timestamps and generated `duration_minutes` (local wall-clock overnight duration). History uses `shift_id` and immutable revision `id`.

Equal start/end is rejected rather than interpreted as a 24-hour shift. Break must be shorter than the shift; grace is 0–240; OT fields are 0–1440 with half ≤ full. These are configuration values, not payroll calculations. Dated rosters must pin a revision and apply an explicitly configured hospital timezone/DST policy; dated allocation is not delivered by this endpoint.

Tenant-admin holds the default management permission; an independent hospital manager grant is still required. Other fixed system roles receive no roster-management permission. A foreign/missing hospital or row is hidden with 404, missing route permission yields 403, invalid input 400 and stale/duplicate/changed-code conflicts 409. State/revision/audit/identifier-only `helivanta.in.workforce.shift_changed.v1` outbox commit atomically. No Valkey HR cache or unnecessary Temporal transaction introduced. See [ADR-0017](../adr/0017-versioned-shift-definitions.md) and [HR coverage](../hr-capability-coverage.md).

## Versioned roster areas, groups and weekly templates

Design: [ADR 0018](../adr/0018-versioned-roster-configuration.md). UI: `/workforce/roster-setup`. Editable diagram: [roster configuration](../diagrams/versioned-roster-configuration.drawio).

| Endpoint                                                             | Authorization                                                | Behavior                                                                             |
| -------------------------------------------------------------------- | ------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| `GET /workforce/hospitals/{hospital}/hr/roster-configs`              | Tenant membership and explicit hospital reader/manager grant | Bounded keyset pagination; optional `kind` filter                                    |
| `PUT /workforce/hospitals/{hospital}/hr/roster-configs/{id}`         | `workforce.roster.manage` and locked hospital manager grant  | Stable UUID/code/kind, expected version, reviewed effective date, immutable revision |
| `GET /workforce/hospitals/{hospital}/hr/roster-configs/{id}/history` | Tenant membership and explicit hospital reader/manager grant | Bounded keyset history with original membership and weekly allocations               |

Shared input: `expected_version`, `kind` (`area`, `group`, `weekly_template`), `code`, `name`, `description`, `active`, `effective_on` (ISO date), `reviewed: true`, `shift_revision_ids` and `days`. Areas/groups require one to 100 distinct pinned shift revisions and no days. Weekly templates require no direct shift membership and seven distinct weekdays (Monday=0), each with `weekly_off` and `allocations`. Working days have one or two `{shift_revision_id, area_revision_id}` pairs; off days have none. At least one day must work. The entire weekly cycle is checked for overlapping work, including overnight work entering an off day and Sunday/Monday overlap. A template cannot predate its pinned area.

New references must be current and active; unchanged historical references remain pinned. Reactivation and effective-date changes require current references. The server supplies reviewer identity from authentication. Clients cannot supply tenant, hospital or actor. Foreign scope is hidden with 404; missing management permission yields 403, invalid input 400 and stale/duplicate/immutable identity conflicts 409. Successful unchanged retries return the existing revision with no duplicate audit/event. Lists/history are private `no-store`.

Four normalized tables use forced RLS and scoped foreign keys. Revisions and their children cannot be amended later. State, immutable aggregate, audit and identifier-only `helivanta.in.workforce.roster_config_changed.v1` outbox commit together. JetStream publication resumes after broker failure. This configuration transaction needs no Temporal workflow or HR-data cache. Dated allocation, hospital timezone/DST policy, automatic population, leave/credential conflict checks and roster freeze/correction remain under #1036.

Repeatable synthetic acceptance: `scripts/verify-roster-config.mjs`, using the same privately supplied verification scope as the enrollment verifier. Stable reserved IDs distinguish its fixtures from manual UI records; reruns restore baseline through versioned writes without deleting history.

## Roster settings and staff weekly-template assignments

Design: [ADR 0019](../adr/0019-roster-settings-and-staff-assignments.md). UI: `/workforce/roster-policy`.

| Endpoint                                                                   | Authorization                                                | Behavior                                                                                       |
| -------------------------------------------------------------------------- | ------------------------------------------------------------ | ---------------------------------------------------------------------------------------------- |
| `GET /workforce/hospitals/{hospital}/hr/roster-settings`                   | Tenant membership and explicit hospital reader/manager grant | Current reviewed policy; 404 until a manager reviews one                                       |
| `PUT /workforce/hospitals/{hospital}/hr/roster-settings`                   | `workforce.roster.manage` and locked hospital manager grant  | Expected version, IANA timezone, week start, auto population, monotonic effective date         |
| `GET /workforce/hospitals/{hospital}/hr/roster-settings/history`           | Tenant membership and explicit hospital reader/manager grant | Bounded keyset history of immutable revisions                                                  |
| `GET /workforce/hospitals/{hospital}/hr/roster-assignments`                | Tenant membership and explicit hospital reader/manager grant | Bounded keyset list with staff and pinned template code/name                                   |
| `PUT /workforce/hospitals/{hospital}/staff/{id}/roster-assignment`         | `workforce.roster.manage` and locked hospital manager grant  | Expected version, pinned weekly-template revision, `fixed`, `active`, monotonic effective date |
| `GET /workforce/hospitals/{hospital}/staff/{id}/roster-assignment/history` | Tenant membership and explicit hospital reader/manager grant | Bounded keyset history; 404 when the staff member has no assignment                            |

Settings input: `expected_version`, `timezone` (canonical IANA name such as `Asia/Kolkata` or `UTC`; `Local`, aliases and unknown zones are rejected), `week_start` (Monday=0…Sunday=6), `auto_population` (`disabled`, `fixed_shift`, `all_assigned`), `effective_on` and `reviewed: true`. There is no default policy.

Assignment input: `expected_version`, `weekly_template_revision_id`, `fixed`, `active`, `effective_on` and `reviewed: true`. A new pin must be the current revision of an active weekly template effective on or before the assignment; an unchanged pin stays valid after the template is revised. Active assignments require active staff. Bad references return 400 `invalid_reference`.

Effective dates cannot move earlier than the current revision (400). Stale versions return 409, foreign scope 404, missing permission 403. Unchanged retries return the existing revision with no new audit or event. State, revision, audit and identifier-only `helivanta.in.workforce.roster_settings_changed.v1` / `helivanta.in.workforce.staff_roster_assignment_changed.v1` outbox rows commit together; Valkey and Temporal are not involved. Synthetic acceptance: `scripts/verify-roster-policy.mjs` (run after the enrollment and roster-config verifiers, whose fixtures it pins).

## Dated rosters

Design: [ADR 0020](../adr/0020-dated-rosters.md). UI: `/workforce/rosters`.

| Endpoint                                                                 | Authorization                                                | Behavior                                                                                                         |
| ------------------------------------------------------------------------ | ------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------- |
| `GET /workforce/hospitals/{hospital}/hr/rosters`                         | Tenant membership and explicit hospital reader/manager grant | Bounded keyset list with status, period, pinned settings and current allocation count                            |
| `PUT /workforce/hospitals/{hospital}/hr/rosters/{id}`                    | `workforce.roster.manage` and locked hospital manager grant  | Idempotent create of a reviewed draft; name, start and end date                                                  |
| `GET /workforce/hospitals/{hospital}/hr/rosters/{id}`                    | Tenant membership and explicit hospital reader/manager grant | Current head                                                                                                     |
| `GET /workforce/hospitals/{hospital}/hr/rosters/{id}/allocations`        | Tenant membership and explicit hospital reader/manager grant | Bounded keyset list; `?version=N` returns the allocations as of that version; `?staff_id=` narrows to one person |
| `GET /workforce/hospitals/{hospital}/hr/rosters/{id}/revisions`          | Tenant membership and explicit hospital reader/manager grant | Bounded keyset history with action, mode, counts (including `credential_clashes`), reason and reviewer           |
| `POST /workforce/hospitals/{hospital}/hr/rosters/{id}/changes`           | `workforce.roster.manage` and locked hospital manager grant  | `mode` `roster`, `employee` or `additional`; up to 500 additions and 500 removals                                |
| `POST /workforce/hospitals/{hospital}/hr/rosters/{id}/populate`          | `workforce.roster.manage` and locked hospital manager grant  | Fill empty staff-dates from active weekly-template assignments; optional `staff_ids` (≤500)                      |
| `POST /workforce/hospitals/{hospital}/hr/rosters/{id}/freeze`            | `workforce.roster.manage` and locked hospital manager grant  | Draft → frozen; `acknowledged_clashes` (≤2,000 allocation ids) must cover every current credential clash         |
| `GET /workforce/hospitals/{hospital}/hr/rosters/{id}/credential-clashes` | `workforce.roster.manage` and hospital manager grant         | Current allocations outside a reviewed credential's validity, by date; type, dates and standing only; audited    |
| `POST /workforce/hospitals/{hospital}/hr/rosters/{id}/discard`           | `workforce.roster.manage` and locked hospital manager grant  | Draft → discarded with a reason; removes current allocations, keeps history, frees the period                    |
| `POST /workforce/hospitals/{hospital}/hr/rosters/{id}/unfreeze`          | `workforce.roster.correct` and locked hospital manager grant | Frozen → draft with a reason, for audited corrections                                                            |

Rosters cover one to six whole weeks starting on the reviewed week start and pin the settings revision in force on their first day; without reviewed settings, creation returns 400 `roster_settings_missing`. Non-discarded periods cannot overlap (409 `roster_overlap`). Every write carries `change_id`, `expected_version` and `reviewed: true`: a replayed `change_id` returns the original revision, reuse for another action returns 409 `change_reused`, and stale versions return 409 `stale_version`.

Allocations name `id`, `staff_id`, `roster_date`, `shift_revision_id` and `area_revision_id`. Staff must be active, the area revision current and effective on the date, and the shift one of the area's shifts (400 `invalid_reference`). Shift times resolve through the pinned IANA timezone; `end ≤ start` ends the next day, and a DST gap or repeated hour returns 400 `dst_unresolvable`. Each staff-date has one primary; `additional` adds non-overlapping extra shifts to an existing primary. Overlapping time for one person, in any roster, returns 409 with `reason: "allocation_overlap"` and the conflicting `allocation_ids`. Population follows the pinned `auto_population`, never overwrites existing staff-dates, reports skipped counts, returns 400 `population_disabled` when disabled and `population_too_large` above 20,000 rows. Population skips approved full-day leave (#1037).

A credential clash (#1108) is a current allocation dated after a reviewed credential's `valid_until` (`expired`) or before its `valid_from` (`not_yet_valid`); the boundary days are valid and typed enrollment dates never count. Clashes are computed on read and never block changes or population. Freezing answers 409 `reason: "credential_clashes"` with the `outstanding` count until every clash is acknowledged, 409 `too_many_clashes` above 2,000, and 400 if `acknowledged_clashes` is sent to discard or unfreeze. Each acknowledged pair is kept in an append-only, audited table with its validity version; frozen rosters keep frozen and show later clashes on the next read. Design: [roster credential clashes spec](../superpowers/specs/2026-10-18-roster-credential-clashes-design.md).

State, revision, audit and an identifier-only `helivanta.in.workforce.roster_changed.v1` outbox row commit together; Valkey and Temporal are not involved. Responses are `Cache-Control: no-store`. Synthetic acceptance: `scripts/verify-rosters.mjs` (run after `verify-roster-policy.mjs`).

## Leave setup: types, holiday calendars and structures

Design: [ADR 0021](../adr/0021-leave-management.md), [spec](../superpowers/specs/2026-10-15-leave-setup-design.md). UI: `/workforce/leave-setup`. Contract: `contracts/workforce-leave.json`, generated by `scripts/generate-workforce-leave.mjs`.

| Endpoint                                                            | Authorization                                                 | Behavior                                                                                  |
| ------------------------------------------------------------------- | ------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| `GET /workforce/hospitals/{hospital}/hr/leave-configs`              | Tenant membership and explicit hospital reader/manager grant  | Bounded keyset pagination; optional `kind` filter                                         |
| `PUT /workforce/hospitals/{hospital}/hr/leave-configs/{id}`         | `workforce.leave.configure` and locked hospital manager grant | Stable UUID/code/kind/year, expected version, reviewed effective date, immutable revision |
| `GET /workforce/hospitals/{hospital}/hr/leave-configs/{id}/history` | Tenant membership and explicit hospital reader/manager grant  | Bounded keyset history with each revision's rules, holidays or entries                    |

Shared input: `expected_version`, `kind` (`leave_type`, `holiday_calendar`, `structure`), `code`, `name`, `description`, `active`, `effective_on`, `reviewed: true`, plus kind-specific fields:

- Leave types require `rules`: `paid`, `balance_tracked`, `half_day_allowed`, `short_leave_minutes` (0–480 in 15-minute steps; 0 disables short leave) and `clubbable`.
- Holiday calendars require `calendar_year` (one calendar per year per hospital, fixed once created), take effect no later than 1 January of that year, and list up to 100 unique `holidays` (`holiday_date` within the year, `name`, `category`: `regular`, `government`, `optional`).
- Structures list 1–30 `entries`, one per leave type: `leave_type_revision_id`, `annual_half_days` and `carry_forward_half_days` (0–730 half days), `accrual` (`upfront`, `monthly`), `proration` (`none`, `join_month`) and `probation_eligible`. Untracked leave types carry zero allowance. Allowances are in half days so half-day leave needs no fractions.

New structure references must be current, active leave-type revisions effective on or before the structure (400 `invalid_reference`); an unchanged structure keeps the revisions it pinned. Effective dates never move backwards. Code, kind and calendar year are identity: changing them returns 409, as do stale versions and duplicate codes, names or years. Unchanged retries return the existing revision with no new audit or event.

Five forced-RLS tables hold heads, immutable revisions and their children; children can only be written in their revision's creating transaction, and a deferred trigger rejects incomplete aggregates. State, revision, audit `workforce.leave_config.changed` and an identifier-only `helivanta.in.workforce.leave_config_changed.v1` outbox row commit together; Valkey and Temporal are not involved. Balances and requests are below; reports (slice 4) follow under #1037. Synthetic acceptance: `scripts/verify-leave-setup.mjs`.

## Leave balances: settings, assignments, postings and corrections

Design: [ADR 0021](../adr/0021-leave-management.md). UI: `/workforce/leave-balances`. Contract: `contracts/workforce-leave.json`.

| Route                                                                      | Access                                                  | Behaviour                                                                               |
| -------------------------------------------------------------------------- | ------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| `GET /workforce/hospitals/{hospital}/hr/leave-settings`                    | Tenant membership and hospital reader/manager grant     | 404 until set                                                                           |
| `PUT /workforce/hospitals/{hospital}/hr/leave-settings`                    | `workforce.leave.configure` and locked hospital manager | `year_start_month` (1–12), `reviewed: true`; set once, a different month returns 409    |
| `GET /workforce/hospitals/{hospital}/hr/leave-assignments`                 | Reader/manager grant                                    | Keyset pagination of each staff member's current structure assignment                   |
| `PUT /workforce/hospitals/{hospital}/staff/{id}/leave-assignment`          | `workforce.leave.allocate` and locked hospital manager  | `expected_version`, `structure_revision_id`, `active`, `effective_on`, `reviewed: true` |
| `GET /workforce/hospitals/{hospital}/staff/{id}/leave-assignment/history`  | Reader/manager grant                                    | Immutable assignment revisions                                                          |
| `GET /workforce/hospitals/{hospital}/hr/leave-years`                       | Reader/manager grant                                    | Opened years with `posted_months` and `closed`                                          |
| `PUT /workforce/hospitals/{hospital}/hr/leave-years/{year}`                | `workforce.leave.allocate` and locked hospital manager  | Opens a year from the fixed start month; reopening returns it unchanged                 |
| `POST /workforce/hospitals/{hospital}/hr/leave-years/{year}/postings`      | `workforce.leave.allocate` and locked hospital manager  | `{month}` posts the next month; reposting a posted month returns its run                |
| `POST /workforce/hospitals/{hospital}/hr/leave-years/{year}/close`         | `workforce.leave.allocate` and locked hospital manager  | Carries forward and expires once; repeating returns the close run                       |
| `GET /workforce/hospitals/{hospital}/hr/leave-years/{year}/runs`           | Reader/manager grant                                    | Posting and close runs with entry counts                                                |
| `GET /workforce/hospitals/{hospital}/hr/leave-balances`                    | Reader/manager grant                                    | `leave_year` required, optional `as_of` inside it; paged staff balances                 |
| `GET /workforce/hospitals/{hospital}/staff/{id}/leave-balances`            | Reader/manager grant                                    | One staff member's balances for `leave_year`/`as_of`                                    |
| `GET /workforce/hospitals/{hospital}/staff/{id}/leave-ledger`              | Reader/manager grant                                    | `leave_year` required; append-only entries                                              |
| `PUT /workforce/hospitals/{hospital}/staff/{id}/leave-corrections/{entry}` | `workforce.leave.correct` and locked hospital manager   | Client-chosen entry id; replay returns the entry, reuse with other values returns 409   |

Amounts are integer half-days. Posting month _k_ credits each assigned, active staff member under the assignment in force on the month's first day, or on the day an assignment starts mid-month: monthly accrual credits `floor(annual·k/12) − floor(annual·(k−1)/12)` so twelve months sum exactly to the allowance; upfront types are allocated once a year, prorated from the joining month when configured, and a switch between modes never double-credits. Every credit has a deterministic idempotency key, so a retried or concurrent posting adds nothing. Closing needs all twelve months posted and the next year open: the positive balance up to the type's carry-forward limit moves into the next year (`carry_out`/`carry_in`) and the rest expires.

Time guards use the date anywhere on Earth (UTC−12) and return 409: a year can be opened at most one year ahead, a month posted only once it starts, and a year closed only after it ends. Assignment changes cannot start inside posted months or closed years. Corrections need `workforce.leave.correct`, a non-zero amount, a reason of 10–500 characters, an open year and a balance-tracked type. Reasons stay in the ledger, are returned only to callers holding `workforce.leave.correct` or `workforce.leave.allocate` (redacted for everyone else), and never enter events, logs or audit payloads.

Every write takes one hospital advisory lock and commits state, an append-only `workforce_audit` row and an identifier-only outbox event together: `helivanta.in.workforce.staff_leave_assignment_changed.v1` and `helivanta.in.workforce.leave_ledger_changed.v1` (source, reference and entry count only). Six forced-RLS tables hold the data: settings, revisions, runs and ledger entries are append-only, and the assignment head and year counter change only through guarded updates. Valkey and Temporal are not involved. Synthetic acceptance: `scripts/verify-leave-ledger.mjs` (run `scripts/verify-leave-setup.mjs` first; set `HELIVANTA_VERIFY_STAFF` to a synthetic staff member).

## Leave requests and approvals

### Reviewer scope discovery (development slice)

`GET /workforce/leave-review-scopes` requires a verified session and current tenant
membership. It derives the caller's own active reviewed account-link candidates,
then requires current IAM leave-review authority and locked active employment/link
validation for each returned hospital. A reviewed link alone is insufficient.
The optional expected-tenant header is checked before owner queries. The response
is `{data:[{hospital_id,name}],page:{has_more,next_cursor}}`, generated from
`contracts/workforce-leave.json`; data rows expose only the hospital ID/name and no staff profiles or authority
receipts. The opaque cursor carries the caller-bound candidate pagination key. Successful scope reads have minimized attributable audits.

Discovery defaults to twenty candidates and accepts limits 1–20. Its cursor is
bound to tenant, caller and route; it is not an authorization credential. Filtering
may leave an empty page with `has_more:true`; callers must retain the supplied
continuation rather than infer completion from `data.length`. Graph checks run
outside transactions, at most four concurrently. Each disclosed scope is checked
inside the existing IAM guard and product staff/link locks. Dependency failure
returns an error without a partial scope page. Revoked credentials return 401;
withdrawn/expired assignments or revoked/inactive links are absent from discovery.
A separate concurrent-index migration supports the subject/status/keyset lookup.
This is not the pending-request queue/detail API or a released reviewer UI.

### Reviewer queue and detail (development slice)

`GET /workforce/hospitals/{hospital}/leave-review-requests` is a scoped, keyset-paged
pending queue (default 50, maximum 100). It requires a current reviewed IAM leave
assignment plus the caller's locked active employment/account link. A legacy
permission alone does not admit this purpose-bound queue. Linked self and original
filer are excluded by both the query and forced RLS. The canonical
`leaveReviewRequest` model exposes minimal staff display, leave type, dates/unit,
amount, status, expected version and paging identity. Reasons and filer subjects
are absent from queue rows.

`GET /workforce/hospitals/{hospital}/staff/{id}/leave-review-requests/{request}`
rechecks the same authority and the exact pending target, then releases the existing
`leaveRequestDetail` model with reason, counted days and bounded roster clashes.
Own, completed or inaccessible requests return 404; the endpoint does not expose
request history or enrollment. Both reads use the IAM guard's transaction and
locked actor/link validation, with minimized per-request disclosure audits. Their
read-only RLS context cannot authorize decisions, ledger writes, payroll, private
enrollment or unrelated staff access. Read-audit failure rolls back the read
transaction and returns an error rather than a partial response.

Scoped GET and POST gates check the optional expected-tenant assertion before
scope lookup/product work; stale, malformed or multiple assertions return 409
`session_changed`. Queue/detail use additive read policies in unreleased migration
`0036`; `0038_workforce_leave_review_queue_index` builds the hospital/status/keyset
index concurrently. Production migration and release are not established here.

The development UI is `/workforce/leave-review`, linked as **Review leave** in
Staff navigation. It discovers usable scopes, loads the pending queue and protected
detail, and posts decisions to the existing decision endpoint. It needs no Staff
directory lookup or tenant-wide business grant. Query keys bind tenant/subject and
requests carry the expected tenant. The page clears during workspace transition;
a known access/version refusal hides private detail and requires refresh. After
an uncertain response, it preserves the exact decision/version/note, freezes edits
and offers an identical retry. A 200 result refreshes the queue; errors never
present success. Browser and live two-person acceptance remain required.

### Request and decision contract

The scoped decision consumer is implemented in the current development slice;
reviewer discovery/queue UI and production acceptance remain outstanding. A scoped
reviewer receives no hospital-manager grant or general directory, enrollment,
payroll, cancellation or revocation authority. General request-list/detail/history
routes retain their existing authorization until separately implemented.

Before a scoped decision, IAM verifies the graph outside database locks, then
checks current assignment, membership generation, acknowledged epoch, credential
watermark and validity under owner locks. Workforce joins the same transaction,
checks and locks the actor's active employment and reviewed account link, and
restricts RLS to the exact target request and necessary leave rules. Own-filer and
linked-self exclusions apply before decisions and retries. Decision, event, audit,
outbox and any balance consumption commit together. Audit records the assignment
revision/epochs and actor link revision without copying private leave reasons.

Withdrawal that commits before a waiting guard prevents product work. Expiry
while waiting for the leave-ledger lock cancels the transaction and returns 403;
the request stays pending with no consumption or success audit. An obsolete link
returns 404, revoked credentials return 401, stale versions return 409 and
unavailable scoped authority returns 503. A credential denial cannot fall back to
a legacy grant within the request. These guarantees apply to this guarded
consumer, not all employment or session-disablement workflows.

Design: [spec](../superpowers/specs/2026-10-15-leave-requests-design.md), [ADR 0021](../adr/0021-leave-management.md). UI: `/workforce/leave-requests`. Contract: `contracts/workforce-leave.json`.

| Route                                                                                   | Access                                                                                                                                           | Behaviour                                                                                |
| --------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------- |
| `GET /workforce/hospitals/{hospital}/hr/leave-requests`                                 | Reader/manager grant                                                                                                                             | Optional `status`, `staff_id`, `leave_year`; newest filing first, keyset pages up to 100 |
| `PUT /workforce/hospitals/{hospital}/staff/{id}/leave-requests/{request}`               | `workforce.leave.request` and locked hospital manager                                                                                            | Client-chosen id; replay returns the request, reuse with other values returns 409        |
| `GET /workforce/hospitals/{hospital}/staff/{id}/leave-requests/{request}`               | Reader/manager grant                                                                                                                             | Request, counted days and roster allocations it clashes with                             |
| `GET /workforce/hospitals/{hospital}/staff/{id}/leave-requests/{request}/history`       | Reader/manager grant                                                                                                                             | Append-only transitions                                                                  |
| `POST /workforce/hospitals/{hospital}/staff/{id}/leave-requests/{request}/decision`     | `workforce.leave.approve` and locked hospital manager, or current reviewed hospital-scoped leave authority plus active reviewed staff-login link | `expected_version`, `decision` (`approve`, `reject`), `note` (required to reject)        |
| `POST /workforce/hospitals/{hospital}/staff/{id}/leave-requests/{request}/cancellation` | `workforce.leave.request` and locked hospital manager                                                                                            | Withdraws a pending request; `expected_version`, optional `note`                         |
| `POST /workforce/hospitals/{hospital}/staff/{id}/leave-requests/{request}/revocation`   | `workforce.leave.approve` and locked hospital manager                                                                                            | Cancels approved leave and writes its reversal; `expected_version`, `note` required      |

Filing takes `leave_type_id`, `unit` (`full_day` over `start_on`–`end_on`; `half_day` on one date with `session` `first_half`/`second_half`; `short_leave` on one date with `minutes` in 15-minute steps) and an optional `reason` of up to 500 characters. A request lies inside one open leave year and pins the leave-type, assignment and holiday-calendar revisions in force; regular and government holidays count zero, a request of only holidays is 400 and a year without an active calendar is 409. The type must be active, in the staff member's structure and allow the unit (400). Overlapping pending or approved leave (a PostgreSQL exclusion over half-day slots), non-clubbable neighbours, an insufficient balance after pending holds, a closed year and a stale version return 409. Types closed during probation need `workforce.enrollment.manage` to read the probation end (403 without it, 409 when missing or not before the start).

Approval re-checks every rule, requires a ledger balance covering the request and posts one `consumption` entry; revocation posts one `reversal`. Both replay safely. The decider must differ from the filer (403, also a table check) and from the staff member currently bound by an active reviewed login link. Approval, rejection, revocation and decision retries lock the target staff row and recheck that binding inside the owner transaction; no account-link or HR details are disclosed by this check. A leave year cannot close while requests are pending (409). Roster population skips staff-days covered by approved full-day leave and reports them as `skipped_leave`.

Reasons and notes may hold health information: they are returned only to holders of `workforce.leave.request` or `workforce.leave.approve` and never enter events, logs or audit payloads. Every write takes the hospital leave-ledger lock and commits the request, an event row, any ledger entry, a `workforce_audit` row and an identifier-only `helivanta.in.workforce.leave_request_changed.v1` outbox event together. Valkey and Temporal are not involved. Synthetic acceptance: `scripts/verify-leave-requests.mjs` after `scripts/verify-leave-ledger.mjs`; set `HELIVANTA_VERIFY_APPROVER_COOKIE` to a second synthetic session to cover approval and reversal.

## Duty exchanges

Design: [spec](../superpowers/specs/2026-10-16-duty-exchanges-design.md), [ADR 0022](../adr/0022-duty-adjustments.md). UI: `/workforce/duty-exchanges`. Contract: `contracts/workforce-duty.json`.

| Route                                                                            | Access                                               | Behaviour                                                                                |
| -------------------------------------------------------------------------------- | ---------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `GET /workforce/hospitals/{hospital}/hr/duty-exchanges`                          | Reader/manager grant                                 | Optional `status`, `staff_id`, `from`, `to`; newest filing first, keyset pages up to 100 |
| `PUT /workforce/hospitals/{hospital}/hr/duty-exchanges/{exchange}`               | `workforce.duty.request` and locked hospital manager | Client-chosen id; replay returns the exchange, reuse with other values returns 409       |
| `GET /workforce/hospitals/{hospital}/hr/duty-exchanges/{exchange}`               | Reader/manager grant                                 | Exchange and the allocations it names                                                    |
| `GET /workforce/hospitals/{hospital}/hr/duty-exchanges/{exchange}/history`       | Reader/manager grant                                 | Append-only transitions                                                                  |
| `POST /workforce/hospitals/{hospital}/hr/duty-exchanges/{exchange}/decision`     | `workforce.duty.approve` and locked hospital manager | `expected_version`, `decision` (`approve`, `reject`), `note` (required to reject)        |
| `POST /workforce/hospitals/{hospital}/hr/duty-exchanges/{exchange}/cancellation` | `workforce.duty.request` and locked hospital manager | Withdraws a pending exchange; `expected_version`, optional `note`                        |
| `POST /workforce/hospitals/{hospital}/hr/duty-exchanges/{exchange}/revocation`   | `workforce.duty.approve` and locked hospital manager | Hands each duty back to its earlier holder; `expected_version`, `note` required          |

Filing takes `kind` (`swap` or `cover`), `requester_allocation_id`, `counterpart_staff_id`, `counterpart_allocation_id` (swaps only), `counterpart_consent: true` and a `reason` of 1 to 500 characters (400 otherwise). Named allocations must be current primary duties in frozen rosters of the hospital, held by the people named, for two different active staff (400 `invalid_reference`, 409 `staff_inactive`). Duties that have started (409 `exchange_started`) or already sit in a pending or approved exchange (409 `allocation_in_exchange`, also a unique index) cannot be exchanged.

Approval re-checks every rule and fails with 409 `primary_taken`, `on_leave` (approved full-day leave), `orphan_additional` or `roster_changed`. It then moves the duties as one `exchanged` revision of the frozen roster, advancing its version without reopening it; allocations it placed carry `exchange_id`. Revocation needs a note and only applies to duties that have not started; it hands each duty back in a second `exchanged` revision. Every transition replays safely and stale versions return 409 `stale_version`. The decider must differ from the filer (403, also a table check). A frozen roster cannot be unfrozen or discarded while it has pending exchanges (409 `pending_exchanges`).

Reasons and notes are returned only to holders of `workforce.duty.request` or `workforce.duty.approve` and never enter events, logs or audit payloads. Every write takes the hospital duty lock and commits the exchange, an event row, any roster revision, a `workforce_audit` row and an identifier-only `helivanta.in.workforce.duty_exchange_changed.v1` outbox event together. Valkey and Temporal are not involved. Responses are `Cache-Control: no-store`. Synthetic acceptance: `scripts/verify-duty-exchanges.mjs` after `scripts/verify-roster-config.mjs` and `scripts/verify-roster-policy.mjs`; set `HELIVANTA_VERIFY_APPROVER_COOKIE` to a second synthetic session to cover approval and revocation.

## Overtime

Design: [spec](../superpowers/specs/2026-10-16-overtime-design.md), [ADR 0022](../adr/0022-duty-adjustments.md). UI: `/workforce/overtime`. Contract: `contracts/workforce-overtime.json`.

| Route                                                                     | Access                                                     | Behaviour                                                                                                                                                                                                                                                                          |
| ------------------------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /workforce/hospitals/{hospital}/hr/overtime-settings`                | Reader/manager grant                                       | Current policy revision; 404 until one is reviewed                                                                                                                                                                                                                                 |
| `PUT /workforce/hospitals/{hospital}/hr/overtime-settings`                | `workforce.overtime.configure` and locked hospital manager | `daily_limit_minutes` (1–1440) ≤ `weekly_limit_minutes` (≤ 10080) ≤ `monthly_limit_minutes` (≤ 44640), `filing_window_days` (0–92), `effective_on` (never earlier than the current revision), `reviewed: true`, `expected_version`; an identical body returns the current revision |
| `GET /workforce/hospitals/{hospital}/hr/overtime-settings/history`        | Reader/manager grant                                       | Immutable revisions, newest first                                                                                                                                                                                                                                                  |
| `GET /workforce/hospitals/{hospital}/hr/overtime`                         | Reader/manager grant                                       | Optional `status`, `staff_id`, `from`, `to` (work date); newest filing first, keyset pages up to 100                                                                                                                                                                               |
| `PUT /workforce/hospitals/{hospital}/hr/overtime/{request}`               | `workforce.overtime.request` and locked hospital manager   | Client-chosen id; `staff_id`, `work_date`, `start_time`, `end_time` (`HH:MM`, an end at or before the start is the next day), `reason` (1–500); replay returns the request, reuse with other values returns 409 `request_reused`                                                   |
| `GET /workforce/hospitals/{hospital}/hr/overtime/{request}`               | Reader/manager grant                                       | One request                                                                                                                                                                                                                                                                        |
| `GET /workforce/hospitals/{hospital}/hr/overtime/{request}/history`       | Reader/manager grant                                       | Append-only transitions with approved minutes                                                                                                                                                                                                                                      |
| `POST /workforce/hospitals/{hospital}/hr/overtime/{request}/decision`     | `workforce.overtime.approve` and locked hospital manager   | `expected_version`, `decision` (`approve`, `reject`), `approved_minutes` (approve only, 1 to requested), `note` (required to reject or approve fewer minutes)                                                                                                                      |
| `POST /workforce/hospitals/{hospital}/hr/overtime/{request}/cancellation` | `workforce.overtime.request` and locked hospital manager   | Withdraws pending overtime; `expected_version`, optional `note`                                                                                                                                                                                                                    |
| `POST /workforce/hospitals/{hospital}/hr/overtime/{request}/revocation`   | `workforce.overtime.approve` and locked hospital manager   | Approved only; `expected_version`, `note` required                                                                                                                                                                                                                                 |
| `GET /workforce/hospitals/{hospital}/hr/overtime-summary?month=YYYY-MM`   | Reader/manager grant                                       | Per person with overtime in the month: pending, approved-requested and payable minutes; keyset pages up to 100                                                                                                                                                                     |

Filing and approval each re-check, under the hospital overtime lock: active staff (409 `staff_inactive`); start and end exist once in the hospital timezone on the work date and span 15 to 1440 minutes (400); a policy and roster settings in force on the work date (409 `no_overtime_policy`); the work date no more than `filing_window_days` back and 92 days ahead (409 `outside_filing_window`); no overlap with active roster duty (409 `overlaps_duty` with `allocation_ids`), approved leave on the work date (409 `on_leave`) or other pending or approved overtime (409 `overtime_overlap`, also an exclusion constraint); and the day, hospital week and calendar month stay within the limits, counting approved minutes of approved requests and requested minutes of pending ones (409 `limit_exceeded` with `period`, `limit_minutes`, `counted_minutes`). The decision records the policy revision applied. Payable minutes are the approved minutes of `approved` requests; rejected, cancelled and revoked requests pay nothing. Every transition replays safely, stale versions return 409 `stale_version`, and the decider and revoker must differ from the filer (403, also a table check).

Reasons and notes are returned only to holders of `workforce.overtime.request` or `workforce.overtime.approve` and never enter events, logs or audit payloads. Every write commits the change, an event row, a `workforce_audit` row and an identifier-only `helivanta.in.workforce.overtime_changed.v1` or `overtime_settings_changed.v1` outbox event together. Valkey and Temporal are not involved. Responses are `Cache-Control: no-store`. Synthetic acceptance: `scripts/verify-overtime.mjs` after `scripts/verify-roster-config.mjs`; set `HELIVANTA_VERIFY_APPROVER_COOKIE` to a second synthetic session to cover partial approval, payable minutes and revocation.

## Attendance

Design: [spec](../superpowers/specs/2026-10-16-attendance-design.md), [corrections spec](../superpowers/specs/2026-10-16-attendance-corrections-design.md), [timesheets spec](../superpowers/specs/2026-10-16-attendance-timesheets-design.md), [ADR 0028](../adr/0028-attendance.md). UI: `/workforce/attendance`. Contract: `contracts/workforce-attendance.json`.

| Route                                                                                                              | Access                                                       | Behaviour                                                                                                                                                                                                                                                                                                       |
| ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /workforce/hospitals/{hospital}/hr/attendance-settings`                                                       | Reader/manager grant                                         | Current policy revision; 404 until one is reviewed                                                                                                                                                                                                                                                              |
| `PUT /workforce/hospitals/{hospital}/hr/attendance-settings`                                                       | `workforce.attendance.configure` and locked hospital manager | `expected_version`, early-arrival (0–240) and late-departure (0–720) allowances, early-departure grace (0–240), short-duty tolerance (0–720), duplicate window (1–60) in minutes, back-dating window (0–92 days), `effective_on` not before the current revision's, `reviewed: true`; an identical body replays |
| `GET /workforce/hospitals/{hospital}/hr/attendance-settings/history`                                               | Reader/manager grant                                         | Policy revisions, newest first                                                                                                                                                                                                                                                                                  |
| `GET /workforce/hospitals/{hospital}/hr/attendance/punches?staff_id=&from=&to=`                                    | Reader/manager grant                                         | One person's punches by hospital-local date, voided included; at most 42 days; keyset pages up to 100                                                                                                                                                                                                           |
| `PUT /workforce/hospitals/{hospital}/hr/attendance/punches/{punch}`                                                | `workforce.attendance.record` and locked hospital manager    | Client-chosen id; `staff_id`, `direction` (`in`, `out`), whole-minute `punched_at` no more than five minutes ahead, `reason` (1–500); identical replay returns the punch, different values 409 `request_reused`                                                                                                 |
| `GET /workforce/hospitals/{hospital}/hr/attendance/punches/{punch}`                                                | Reader/manager grant                                         | One punch with its void, if any                                                                                                                                                                                                                                                                                 |
| `POST /workforce/hospitals/{hospital}/hr/attendance/punches/{punch}/void`                                          | `workforce.attendance.record` and locked hospital manager    | `reason` (1–500); same reason replays, a different one is 409 `punch_voided`                                                                                                                                                                                                                                    |
| `GET /workforce/hospitals/{hospital}/hr/attendance?date=YYYY-MM-DD`                                                | Reader/manager grant                                         | Derived day per person rostered, on leave or punched that date; keyset pages up to 100                                                                                                                                                                                                                          |
| `GET /workforce/hospitals/{hospital}/hr/attendance-totals?date=YYYY-MM-DD`                                         | Reader/manager grant                                         | People, shifts, payable minutes, approved overtime, unpaid half-days and status/exception counts for the date                                                                                                                                                                                                   |
| `GET /workforce/hospitals/{hospital}/staff/{id}/attendance?from=&to=`                                              | Reader/manager grant                                         | One person's derived days and totals over at most 42 days                                                                                                                                                                                                                                                       |
| `GET /workforce/hospitals/{hospital}/hr/attendance/corrections?status=&staff_id=&from=&to=`                        | Reader/manager grant                                         | Correction requests by affected local date, newest filing first; keyset pages up to 100                                                                                                                                                                                                                         |
| `PUT /workforce/hospitals/{hospital}/hr/attendance/corrections/{correction}`                                       | `workforce.attendance.correct` and locked hospital manager   | Client-chosen id; `staff_id`, `kind` (`add`, `void`, `replace`), `direction` and whole-minute `punched_at` for add/replace, `target_punch_id` for void/replace, `reason` (1–500); identical replay returns the request, different values 409 `request_reused`                                                   |
| `GET /workforce/hospitals/{hospital}/hr/attendance/corrections/{correction}`                                       | Reader/manager grant                                         | The request with the target's before values, the corrected after values and the applied punch id                                                                                                                                                                                                                |
| `GET /workforce/hospitals/{hospital}/hr/attendance/corrections/{correction}/history`                               | Reader/manager grant                                         | Event trail, newest first                                                                                                                                                                                                                                                                                       |
| `POST /workforce/hospitals/{hospital}/hr/attendance/corrections/{correction}/decision`                             | `workforce.attendance.approve` and locked hospital manager   | `expected_version`, `decision` (`approve`, `reject`), `note` (required to reject); never on one's own filing (403)                                                                                                                                                                                              |
| `POST /workforce/hospitals/{hospital}/hr/attendance/corrections/{correction}/cancellation`                         | `workforce.attendance.correct` and locked hospital manager   | `expected_version`, optional `note`                                                                                                                                                                                                                                                                             |
| `GET /workforce/hospitals/{hospital}/hr/attendance/periods`                                                        | Reader/manager grant                                         | Finalised and reopened periods, newest first; keyset pages up to 100                                                                                                                                                                                                                                            |
| `PUT /workforce/hospitals/{hospital}/hr/attendance/periods/{period}`                                               | `workforce.attendance.finalise` and locked hospital manager  | Client-chosen id; `from`, `to` (at most 31 hospital days, all ended) and optional `note` (0–500); writes timesheet 1; identical replay returns the period, different values 409 `request_reused`                                                                                                                |
| `GET /workforce/hospitals/{hospital}/hr/attendance/periods/{period}`                                               | Reader/manager grant                                         | The period and its current timesheet's totals                                                                                                                                                                                                                                                                   |
| `POST /workforce/hospitals/{hospital}/hr/attendance/periods/{period}/finalisation`                                 | `workforce.attendance.finalise` and locked hospital manager  | `expected_version`, `note` (1–500); writes the next timesheet, recalculating a finalised period or locking a reopened one again                                                                                                                                                                                 |
| `POST /workforce/hospitals/{hospital}/hr/attendance/periods/{period}/reopening`                                    | `workforce.attendance.finalise` and locked hospital manager  | `expected_version`, `note` (1–500); only from `finalised` (409 `request_state`)                                                                                                                                                                                                                                 |
| `GET /workforce/hospitals/{hospital}/hr/attendance/periods/{period}/history`                                       | Reader/manager grant                                         | Transition trail, newest first                                                                                                                                                                                                                                                                                  |
| `GET /workforce/hospitals/{hospital}/hr/attendance/periods/{period}/timesheets`                                    | Reader/manager grant                                         | Every version with totals and added/changed/removed day counts, newest first                                                                                                                                                                                                                                    |
| `GET /workforce/hospitals/{hospital}/hr/attendance/periods/{period}/timesheets/{timesheet}/people`                 | Reader/manager grant                                         | Per-person totals in staff-list order; keyset pages up to 100                                                                                                                                                                                                                                                   |
| `GET /workforce/hospitals/{hospital}/hr/attendance/periods/{period}/timesheets/{timesheet}/days?staff_id=&change=` | Reader/manager grant                                         | Day rows, newest day first, each with the derived day exactly as finalised (`removed` rows carry none)                                                                                                                                                                                                          |
| `GET /workforce/hospitals/{hospital}/hr/attendance/punch-report?from=&to=&staff_id=`                               | Reader/manager grant                                         | Hospital-wide punches over at most 31 days, most recently recorded first, with job category, source, recorder and void state; never reasons                                                                                                                                                                     |

A punch takes the roster timezone in force on its local date and the attendance policy in force then (409 `no_attendance_policy` without both); its local date must sit inside the policy's back-dating window (409 `outside_backdate_window`), the person must be active (409 `staff_inactive`), and a live same-direction punch within the duplicate window is refused (409 `duplicate_punch` with `punch_id`). Punches and voids are append-only at the database. Attendance is derived on read from frozen-roster allocations, punches, approved leave, approved overtime and the policy, and never stored: statuses are `present`, `absent`, `on_leave`, `missing_in`, `missing_out`, `in_progress`, `upcoming`, `unpublished` and `no_policy`; exceptions are `late`, `early_departure`, `short_duty`, `missing_punch`, `absent`, `duplicate_punch`, `punched_on_leave` and `stray_punch`. Each shift names its allocation, shift revision, policy revision and the punches used and ignored. Payable minutes are worked minutes inside the shift less its break; absence and missing punches pay nothing and an unexcused absence counts as leave without pay. Reasons are returned only to holders of `workforce.attendance.record` and never enter events, logs or audit payloads. Every write commits the change, a `workforce_audit` row and an identifier-only `helivanta.in.workforce.attendance_punch_changed.v1` or `attendance_settings_changed.v1` outbox event together. Valkey and Temporal are not involved. Responses are `Cache-Control: no-store`. Device and import sources are a later slice. Synthetic acceptance: `scripts/verify-attendance.mjs` after `scripts/verify-roster-config.mjs`.

Corrections change punches only through a second person. A pending request changes nothing. Approval writes, in one transaction, a punch with `source: "correction"` and `correction_id` (add, replace) and/or a void of the target linked by `void_correction_id` (void, replace); the original punch stays as recorded. Rejected and cancelled requests write nothing. A target must be a live punch of the same person (409 `punch_voided`) with no other open request (409 `correction_exists`); new times follow the punch rules (409 `duplicate_punch`, `outside_backdate_window`, `no_attendance_policy`) and a pending same-direction add within the duplicate window is 409 `duplicate_correction`. Approval re-checks the target and duplicates under the hospital lock and measures the back-dating window from the filing time. Transitions follow overtime: 409 `stale_version` and `request_state`, and replaying the step that produced the current version returns the request. Separation of duties and every transition are enforced by database constraints and triggers as well as the handlers. Corrections do not require the person to be active. Reasons and notes are returned only to holders of `workforce.attendance.record`, `.correct` or `.approve`. Writes emit `attendance_correction_changed.v1` and, on approval, `attendance_punch_changed.v1`. Synthetic acceptance: `scripts/verify-attendance-corrections.mjs` after `scripts/verify-attendance.mjs`; set `HELIVANTA_VERIFY_APPROVER_COOKIE` to a second synthetic session to cover rejection, approval and the applied punches.

Finalisation freezes a closed range for payroll. It is refused, in order, when a day has not ended in the hospital timezone (409 `period_not_closed`), another period covers a day (409 `period_overlap` with `period_id`), a pending correction affects a day (409 `pending_corrections` with `count`), or any shift is `upcoming`, `in_progress`, `unpublished` or `no_policy` (409 `period_not_ready` with per-status `statuses`). The snapshot stores every derived day, per-person and hospital totals and the policy revisions used; a deferred database trigger refuses the commit unless totals reconcile to people and people to days. Each recalculation writes a new numbered version marking every day `added`, `changed`, `unchanged` or `removed` against the previous one; versions, days and events are append-only. While a period is finalised the database refuses punches, voids and correction filings dated inside it and policy revisions effective on or before any of its days (409 `period_locked`); reopening with a reason lifts the lock. Leave, overtime and roster changes are not locked and are adopted only by a recalculation. Notes are returned only to holders of `finalise`, `record`, `correct` or `approve`. Every transition commits a `workforce_audit` row and an identifier-only `helivanta.in.workforce.attendance_period_changed.v1` event. Synthetic acceptance: `scripts/verify-attendance-timesheets.mjs` with `HELIVANTA_VERIFY_PERIOD_DAY` set to a reserved past day.

## Leave reports

Design: [spec](../superpowers/specs/2026-10-16-leave-reports-design.md), [ADR 0021](../adr/0021-leave-management.md). UI: `/workforce/leave-reports`. Contract: `contracts/workforce-leave.json`.

| Route                                                          | Access               | Behaviour                                                                                                                                                                        |
| -------------------------------------------------------------- | -------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /workforce/hospitals/{hospital}/hr/leave-reports/monthly` | Reader/manager grant | Report 949: approved leave per staff member and calendar month, with leave dates and half-days and short-leave minutes per type; keyset pages by staff up to 100                 |
| `GET /workforce/hospitals/{hospital}/hr/leave-reports/status`  | Reader/manager grant | Report 954: requests in every status that overlap the period, newest filing first, with counted half-days, filer and (approved or rejected only) decider; keyset pages up to 100 |

Both require inclusive `from` and `to` dates, no more than 366 days apart and within 2000–2199, and accept `q` (case-insensitive name or employee-code substring, 1–100 characters, wildcards escaped), `staff_id` and `department_id`. The status report also accepts `leave_type_id` and `status`. Invalid values return 400. In 949, a request that crosses a period or month boundary is split by day, and holidays count zero. For a period covering a whole request, its half-days equal the negated `consumption` ledger entry. `department` and `joined_on` columns are returned only to holders of `workforce.enrollment.manage`; filtering by `department_id` without that permission is 403. Foreign hospitals return 404. Reports never carry reasons or notes, are read-only (no audit or outbox rows) and are `no-store`. Valkey and Temporal are not involved. Synthetic acceptance: `scripts/verify-leave-reports.mjs` after `scripts/verify-leave-requests.mjs`.

## Payroll setup: salary heads and structures

Design: [spec](../superpowers/specs/2026-10-16-salary-heads-structures-design.md), [ADR 0029](../adr/0029-payroll-configuration.md), [statutory reference](../payroll-india-statutory-reference-2026-10-16.md). UI: `/workforce/payroll-setup`. Contract: `contracts/workforce-payroll.json`.

Nothing is pre-loaded. Every head, rate, cap, exemption and wage classification is entered and reviewed by the hospital; statutory rates and tax slabs are not in source.

| Route                                                                            | Access                                                    | Behaviour                                                                                                                                                                                                        |
| -------------------------------------------------------------------------------- | --------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /workforce/hospitals/{hospital}/hr/payroll/salary-heads`                    | `workforce.payroll.view` and reader/manager grant         | Optional `kind` (`earning`, `deduction`, `employer_contribution`); keyset pages up to 100                                                                                                                        |
| `PUT /workforce/hospitals/{hospital}/hr/payroll/salary-heads/{id}`               | `workforce.payroll.configure` and locked hospital manager | Client-chosen id; full head, `effective_on` (never earlier than the current revision), `reviewed: true`, `expected_version` (0 to create); an identical body replays; code and kind never change (409)           |
| `GET /workforce/hospitals/{hospital}/hr/payroll/salary-heads/{id}/history`       | `workforce.payroll.view` and reader/manager grant         | Immutable revisions, newest first                                                                                                                                                                                |
| `GET /workforce/hospitals/{hospital}/hr/payroll/salary-structures`               | `workforce.payroll.view` and reader/manager grant         | Keyset pages up to 100, each with its ordered lines                                                                                                                                                              |
| `PUT /workforce/hospitals/{hospital}/hr/payroll/salary-structures/{id}`          | `workforce.payroll.configure` and locked hospital manager | As for heads; optional active department, designation and grade masters; `lines` 1–60, each head once, `amount_paise` on fixed heads only                                                                        |
| `GET /workforce/hospitals/{hospital}/hr/payroll/salary-structures/{id}/history`  | `workforce.payroll.view` and reader/manager grant         | Immutable revisions with their lines                                                                                                                                                                             |
| `POST /workforce/hospitals/{hospital}/hr/payroll/salary-structures/{id}/preview` | `workforce.payroll.view` and reader/manager grant         | `on` (2000–2200) and `payable_half_days` (0 to twice the days in that month); lines, totals and the wage test from the revisions in force on that date; nothing is stored; 409 `not_in_force` outside a revision |

Amounts are integer paise and rates integer parts per million; no amount passes through a float. Percentage heads take the sum of their bases' computed amounts, and bases must be active earnings (400 `invalid_reference`). Every write re-checks, under the hospital payroll lock, that no head depends on itself (409 `cycle` with `head_id`), that each base stays an active earning on its date (409 `base_unavailable`), and that every active structure stays computable (409 `structure_incomplete` with `structure_id`). The preview's wage test adds back excluded heads above half of total remuneration.

Every write commits the revision, a `workforce_audit` row carrying identifiers and versions only, and an identifier-only `helivanta.in.workforce.salary_config_changed.v1` outbox event together. Valkey and Temporal are not involved. Responses are `Cache-Control: no-store`. Synthetic acceptance: `scripts/verify-salary-config.mjs`.

## Payroll setup: statutory PF, ESI, professional tax, LWF and income-tax rules

Design: [PF and ESI spec](../superpowers/specs/2026-10-16-statutory-rules-pf-esi-design.md), [PT and LWF spec](../superpowers/specs/2026-10-16-statutory-rules-pt-lwf-design.md), [income-tax spec](../superpowers/specs/2026-10-17-income-tax-rule-sets-design.md), [income-tax claims spec](../superpowers/specs/2026-10-19-income-tax-claims-design.md), [ADR 0029](../adr/0029-payroll-configuration.md). UI: the Statutory contributions panel on `/workforce/payroll-setup`.

Nothing is pre-loaded. A hospital has at most one rule per kind (`pf`, `esi`, `pt`, `lwf`, `income_tax_new`, `income_tax_old`); every revision is either `covered` with that kind's full parameters or explicitly not covered, and cites the notification it relies on. A rule changes only by proposal and approval from a different person.

An income-tax revision may list up to 24 claim heads (`deduction`, `house_property_interest`, `hra`) and 8 shared cap groups; a claim the revision does not list is reported `not_allowed` and never reduces tax.

| Route                                                                           | Access                                                    | Behaviour                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| ------------------------------------------------------------------------------- | --------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /workforce/hospitals/{hospital}/hr/payroll/statutory-rules`                | `workforce.payroll.view` and reader/manager grant         | Keyset pages up to 100; each rule with its `current` revision and any `pending` proposal                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `PUT /workforce/hospitals/{hospital}/hr/payroll/statutory-rules/{id}`           | `workforce.payroll.configure` and locked hospital manager | Proposes a revision on `expected_version` (0 to create); an identical pending body replays; 409 `proposal_pending` while one awaits a decision; kind never changes; 400 when `effective_on` predates the revision in force                                                                                                                                                                                                                                                                                                                                  |
| `POST /workforce/hospitals/{hospital}/hr/payroll/statutory-rules/{id}/decision` | `workforce.payroll.approve` and locked hospital manager   | `proposal_id`, `expected_version`, `decision` (`approve`/`reject`), `note` (required to reject); 403 for the proposer; the same decision replays; 409 `not_pending` once decided                                                                                                                                                                                                                                                                                                                                                                            |
| `POST /workforce/hospitals/{hospital}/hr/payroll/statutory-rules/{id}/withdraw` | `workforce.payroll.configure` and locked hospital manager | Proposer only (403 otherwise); leaves the revision in force unchanged                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `GET /workforce/hospitals/{hospital}/hr/payroll/statutory-rules/{id}/history`   | `workforce.payroll.view` and reader/manager grant         | Every proposal with its outcome, newest first                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `POST /workforce/hospitals/{hospital}/hr/payroll/statutory-preview`             | `workforce.payroll.view` and reader/manager grant         | `structure_id`, `on`, `payable_half_days`, `disability`, optional `gender`; the salary preview plus each kind from the approved revisions in force that day (`applied`, `not_covered`, `above_ceiling`, `not_configured`, `not_due`, `gender_required`); nothing is stored                                                                                                                                                                                                                                                                                  |
| `POST /workforce/hospitals/{hospital}/hr/payroll/income-tax-preview`            | `workforce.payroll.view` and reader/manager grant         | `on`, `annual_income_paise`, optional `age`, optional `claims` (`{code, amount_paise}`, codes once each) and `hra` (`received_paise`, `salary_paise`, `rent_paid_paise`, `metro`); each regime's annual liability under its approved revision in force that day (`applied`, `not_covered`, `not_configured`, `tax_year_missing`, `age_required`) with the HRA exemption, standard deduction, home loan interest, deductions, each claim's `allowed`/`capped`/`not_allowed` outcome, taxable income, slab tax, rebate, surcharge and cess; nothing is stored |

PF caps the contribution base when `contribution_basis` is `capped`; the pension (EPS) share is carved out of the employer share on its own ceiling, so the employer EPF share never goes negative; EDLI and admin charges are employer components. ESI reports `above_ceiling` rather than contributing, uses the disability ceiling when asked, and exempts the employee share at or below the configured average daily wage. Whether ESI coverage persists through a contribution period, and the per-establishment PF admin minimum, are decided by the payroll run (#1040).

Professional tax and LWF revisions record the `state_code` (for example `IN-MH`) for review; the calculation does not read it. PT `slabs` carry `months`, `genders`, `from_paise` (inclusive), optional `to_paise` (exclusive) and `amount_paise`; in every month any slab names, each gender's slabs must run from ₹0 without gaps or overlaps and end open-ended, and a month no slab names is `not_due`. PT answers `gender_required` rather than guessing when the amount at the employee's wages differs by gender and none was given. LWF has `deduction_months`, rounding, and slabs partitioning ₹0 upward with per-party fixed amount, rate and optional cap: min(fixed + rounded rate × wages, cap). Incomplete tables answer 400, and a deferred constraint trigger refuses them when the API is bypassed. PT configuration supports `monthly`, `half_yearly` and `annual` cadence. Half-yearly rules require two period starts six months apart; annual rules require one. Each configured period must have exactly one deduction month. The preview projects period wages from its supplied monthly wage input; aggregation of actual period wages and payroll posting remain part of #1040. These configurable rules do not establish an approved state-specific policy.

Income-tax revisions cover one tax year (`tax_year`, `YYYY-YY`, 1 April to 31 March), which must contain `effective_on`; a date in a later year answers `tax_year_missing` rather than reusing last year's rates. Each carries a `standard_deduction_paise`, rounding for taxable income and for the tax, one to four `age_bands` (one starting at `min_age` 0, none repeated) each with slabs partitioning taxable income from ₹0 upward, an optional `rebate` (`income_limit_paise`, `max_paise`, `marginal_relief`), up to eight rising `surcharge_bands` (`above_paise`, `rate_ppm`) and `cess_rate_ppm`. Tax is computed in the Act's order: deduction, rounding, slabs, rebate, the highest surcharge band exceeded with marginal relief against the previous band, cess, final rounding. With several age bands and no `age`, the preview answers `age_required` rather than guessing. The preview applies explicitly supplied claims and HRA inputs only under the approved revision's configured claim heads and caps; it does not automatically consume a staff declaration. Staff regime elections and declarations have separate reviewed workflows below; monthly TDS and payroll posting remain part of payroll runs. Income tax is annual, so the monthly statutory preview leaves both regimes out.

The database enforces the separation itself: a decision by the proposer, a withdrawal by anyone else, a rejection without a note, an approval not put in force, a change to decided content and parameters added outside their revision's transaction are all refused by constraints and triggers. Every proposal and decision commits a `workforce_audit` row and an identifier-only `helivanta.in.workforce.statutory_rule_changed.v1` outbox event with the change; neither carries rates. Responses are `Cache-Control: no-store`. Synthetic acceptance: `scripts/verify-statutory-rules.mjs` (run after `verify-salary-config.mjs`, with `HELIVANTA_VERIFY_APPROVER_COOKIE` for a second person).

## Payroll setup: staff salary assignment and tax-regime election

Design: [staff salary assignment spec](../superpowers/specs/2026-10-17-staff-salary-assignment-design.md), [ADR 0029](../adr/0029-payroll-configuration.md). UI: the Staff pay panel on `/workforce/payroll-setup`.

Each staff member has at most one assignment: a structure plus optional per-person amounts for its fixed heads, effective-dated with a reason, changed only by proposal and approval from a different person. Nothing is assigned by default; payroll does not pay a staff member until an assignment is approved.

| Route                                                                                         | Access                                                    | Behaviour                                                                                                                                                                                                                                                                                                 |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /workforce/hospitals/{hospital}/hr/payroll/salary-assignments`                           | `workforce.payroll.view` and reader/manager grant         | Keyset pages up to 100; assignments with a current or pending revision, newest first                                                                                                                                                                                                                      |
| `GET /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/salary-assignment`              | `workforce.payroll.view` and reader/manager grant         | `current` and `pending`; 404 when the staff member has none                                                                                                                                                                                                                                               |
| `PUT /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/salary-assignment`              | `workforce.payroll.configure` and locked hospital manager | Proposes on `expected_version` (0 for the first): `structure_id`, `effective_on`, `reason` (1–500), `amounts` (0–60 `{head_id, amount_paise}`, fixed lines of that structure only, each once); an identical body replays; 409 `proposal_pending`, `staff_inactive` or `assignment_incomplete`             |
| `POST /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/salary-assignment/decision`    | `workforce.payroll.approve` and locked hospital manager   | `proposal_id`, `expected_version`, `decision` (`approve`/`reject`), `note` (required to reject); 403 for the proposer; 409 `not_pending` once decided                                                                                                                                                     |
| `POST /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/salary-assignment/withdraw`    | `workforce.payroll.configure` and locked hospital manager | Proposer only (403 otherwise); the assignment in force is unchanged                                                                                                                                                                                                                                       |
| `GET /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/salary-assignment/history`      | `workforce.payroll.view` and reader/manager grant         | Every revision with its amounts and outcome, newest first                                                                                                                                                                                                                                                 |
| `POST /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/salary-preview`                | `workforce.payroll.view` and reader/manager grant         | `on`, `payable_half_days`, `disability`, optional `gender`; the approved assignment in force that day through the calculator and monthly statutory rules, with `tax_year` and `tax_regime_status` (`elected` with `tax_regime`, or `regime_not_elected`); 409 `not_assigned` when none; nothing is stored |
| `GET /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/tax-regimes`                    | `workforce.payroll.view` and reader/manager grant         | Keyset pages; elections, most recently recorded first                                                                                                                                                                                                                                                     |
| `PUT /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/tax-regimes/{tax_year}`         | `workforce.payroll.configure` and locked hospital manager | `expected_version` (0 to record), `regime` (`new`/`old`), `intimated_on` (no later than 31 March of the tax year), `note` (required to change); an identical replay returns the election                                                                                                                  |
| `GET /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/tax-regimes/{tax_year}/history` | `workforce.payroll.view` and reader/manager grant         | Every version of that election                                                                                                                                                                                                                                                                            |

With no election the system reports `regime_not_elected` and never assumes the statutory default; that is decided by the payroll run (#1040) after owner review. A head or structure change that would leave an approved assignment with an inactive structure or an override on a head that is no longer a fixed line is refused with 409 `assignment_incomplete`, enforced by a deferred database check at every later date. Separation of duties, rejection notes, withdrawal by the proposer and immutable decided content are table constraints, as for statutory rules. Every write commits a `workforce_audit` row and an identifier-only `helivanta.in.workforce.salary_assignment_changed.v1` or `tax_regime_changed.v1` outbox event; neither carries amounts, structures or regimes. Bank account, PAN and UAN use the separately reviewed, encrypted [staff identifier workflow](#payroll-setup-staff-bank-and-tax-identifiers); they are not fields of a salary assignment or included in its events. Responses are `Cache-Control: no-store`.

## Payroll setup: staff bank and tax identifiers

Design: [staff identifiers spec](../superpowers/specs/2026-10-18-staff-identifiers-design.md), [ADR 0029](../adr/0029-payroll-configuration.md). UI: the Bank and tax identifiers section of the Staff pay panel on `/workforce/payroll-setup`.

A staff member's salary bank account, PAN and UAN are encrypted in OpenBao Transit under a per-tenant key and bound to the hospital, staff member and field; the API stores only ciphertext and masked forms. Changes need a proposal and approval from a different person. Seeing a full value needs its own permission and a reason, and is audited.

| Route                                                                                | Access                                                             | Behaviour                                                                                                                                                                                                                                                                                                                                                                                  |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `GET /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/identifiers`           | `workforce.payroll.view` and reader/manager grant                  | `current` and `pending`, masked only; 404 when none                                                                                                                                                                                                                                                                                                                                        |
| `PUT /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/identifiers`           | `workforce.payroll.configure` and locked hospital manager          | Proposes on `expected_version` (0 for the first): `reason` (1–500) and `bank`, `pan`, `uan`, each `{action}` of `keep`, `set` or `clear`; `set` takes `account_number` (9–18 digits), `ifsc` and `holder_name` (1–150) for the bank, or `value` for PAN (individual, fourth letter `P`) and UAN (12 digits); 409 `proposal_pending`, `staff_inactive`, `pan_in_use` or `nothing_to_change` |
| `POST /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/identifiers/decision` | `workforce.payroll.approve` and locked hospital manager            | `proposal_id`, `expected_version`, `decision` (`approve`/`reject`), `note` (required to reject); 403 for the proposer; a rejection erases the proposal's ciphertext                                                                                                                                                                                                                        |
| `POST /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/identifiers/withdraw` | `workforce.payroll.configure` and locked hospital manager          | `proposal_id`, `note`; proposer only (403 otherwise); erases the proposal's ciphertext                                                                                                                                                                                                                                                                                                     |
| `GET /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/identifiers/history`   | `workforce.payroll.view` and reader/manager grant                  | Keyset pages up to 100; every revision, masked, with its outcome, newest first                                                                                                                                                                                                                                                                                                             |
| `POST /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/identifiers/reveal`   | `workforce.payroll.identifiers.reveal` and locked hospital manager | `revision_id`, `field` (`bank_account`, `pan`, `uan`), `reason` (1–200); the revision in force, or a pending one when the caller is not its proposer (403); 409 `not_revealable` otherwise; `Cache-Control: no-store`                                                                                                                                                                      |

When OpenBao is unreachable, refusing or not configured, proposals that set a value and reveals return 503 `identifier_vault_unavailable` while masked reads keep working; a ciphertext that fails authentication is 500 and never returned. Each change and reveal writes an audit row and an identifier-only event (`staff_identifiers_changed.v1`, `staff_identifier_revealed.v1`) in the same transaction.

## Payroll setup: staff income-tax declarations

Design: [income-tax declarations spec](../superpowers/specs/2026-10-18-income-tax-declarations-design.md), [ADR 0029](../adr/0029-payroll-configuration.md). UI: the Tax declarations section of the Staff pay panel on `/workforce/payroll-setup`.

A staff member's Form 124 declaration for a tax year holds claimed amounts by claim code, HRA rent with up to four landlords and home loan interest with up to four lenders. Landlord and lender PANs are encrypted in OpenBao Transit under the tenant's staff key, bound to hospital, staff member, year, role and position; only ciphertext and masked forms are stored. Every change is a proposal approved by a different person. A landlord's PAN is required when rent crosses the threshold in force for the year, or when no threshold is set.

| Route                                                                                                | Access                                                             | Behaviour                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| ---------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/tax-declarations/{tax_year}`           | `workforce.payroll.view` and reader/manager grant                  | `current` and `pending`, PANs masked; 404 when neither exists                                                                                                                                                                                                                                                                                                                                                                                     |
| `PUT /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/tax-declarations/{tax_year}`           | `workforce.payroll.configure` and locked hospital manager          | Proposes on `expected_version` (0 for the first): `reason` (1–500), `claims` (up to 24 unique codes with `amount_paise`), optional `hra` (`metro`, 1–4 `landlords` with `name`, `address`, `rent_paise`, `pan`) and `home_loan` (`interest_paise`, 1–4 `lenders`); each `pan` is `{action}` `none`, `keep` or `set` with `value`; 400 `landlord_pan_required`; 409 `proposal_pending`, `staff_inactive`, `nothing_to_keep` or `nothing_to_change` |
| `POST /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/tax-declarations/{tax_year}/decision` | `workforce.payroll.approve` and locked hospital manager            | `proposal_id`, `expected_version`, `decision` (`approve`/`reject`), `note` (required to reject); 403 for the proposer; rejection erases the proposal's ciphertext                                                                                                                                                                                                                                                                                 |
| `POST /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/tax-declarations/{tax_year}/withdraw` | `workforce.payroll.configure` and locked hospital manager          | `proposal_id`, `note`; proposer only (403 otherwise); erases the proposal's ciphertext                                                                                                                                                                                                                                                                                                                                                            |
| `GET /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/tax-declarations/{tax_year}/history`   | `workforce.payroll.view` and reader/manager grant                  | Keyset pages up to 100; every revision, masked, with its outcome, newest first                                                                                                                                                                                                                                                                                                                                                                    |
| `POST /workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/tax-declarations/{tax_year}/reveal`   | `workforce.payroll.identifiers.reveal` and locked hospital manager | `revision_id`, `role` (`landlord`/`lender`), `position` (1–4), `reason` (1–200); the proposer cannot view a pending proposal's PAN (403); 409 `not_revealable` for anything but the declaration in force or a pending proposal; `Cache-Control: no-store`                                                                                                                                                                                         |

When OpenBao is unreachable, refusing or not configured, proposals that set a PAN and reveals return 503 `identifier_vault_unavailable` while masked reads keep working; a ciphertext that fails authentication is 500 and never returned. Each change and reveal writes an audit row and an identifier-only event (`tax_declaration_changed.v1`, `tax_declaration_party_revealed.v1`) in the same transaction; neither carries amounts, names or PANs. TDS computation from declarations is #1040.

### Declaration evidence

Design: [tax declaration evidence spec](../superpowers/specs/2026-10-19-tax-declaration-evidence-design.md). Evidence is a workforce document whose record is a declaration revision (`record_kind` `tax_declaration`, category `tax_declaration_evidence`). It uses the staff document pipeline for quarantine, malware scanning and release. Row-level security admits these rows only to the payroll evidence routes, so the staff document routes never list, download or revoke them.

| Route                                                                 | Permission                                                | Behaviour                                                                                                                                                                                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/tax-declarations/{tax_year}/evidence?revision_id=`             | `workforce.payroll.view` and reader/manager grant         | `{evidence, uploads_available}`: at most 10 files of that revision, newest first. Expired intents are left out. 404 unless the revision belongs to this staff member and year                                                       |
| `PUT …/tax-declarations/{tax_year}/evidence/{file}`                   | `workforce.payroll.configure` and locked hospital manager | `revision_id`, `filename`, `content_type` (`image/png`/`image/jpeg`), `size` (≤ 10 MiB), `sha256`. 201 when created, 200 for an identical replay. 409 `not_pending` once the proposal is decided, `evidence_limit` for an 11th file |
| `PUT …/tax-declarations/{tax_year}/evidence/{file}/content`           | `workforce.payroll.configure` and locked hospital manager | Streams the bytes, verifies them against size and digest, and queues the scan (202). 409 `not_pending` once decided                                                                                                                 |
| `PUT …/tax-declarations/{tax_year}/evidence/{file}/retries/{attempt}` | `workforce.payroll.configure` and locked hospital manager | Re-queues `scan_error` or `scan_pending` after the 45-minute server deadline. 409 `not_pending` once decided                                                                                                                        |
| `GET …/tax-declarations/{tax_year}/evidence/{file}/content`           | `workforce.payroll.view` and reader/manager grant         | Released bytes of an `available` file. Audited as `workforce.tax_declaration.evidence_downloaded`, `Cache-Control: private, no-store`                                                                                               |

A decision or withdrawal answers 409 `evidence_scanning` while any of the proposal's files is `scan_pending`. A decision freezes the proposal's evidence. Approval keeps available files readable, while rejection and withdrawal revoke them, each with a `document_changed.v1` event. The database refuses to unfreeze a file or scan it again.

### Identity review source (preparation)

`GET /workforce/hospitals/:hospital/identity-staff/:id` returns the canonical
`IdentitySource` from `contracts/workforce-identity-checks.json`. Workforce owns
this hospital-scoped Staff/profile projection. It includes recorded names, staff
code, date of birth, employment status, current Staff version, identity fingerprint
and employment generation; it excludes contact details and private documents.
The fingerprint excludes contact changes and binds tenant/hospital/Staff IDs,
employee code, display/given/family names and DOB. A missing profile returns empty
profile fields, not fabricated identity evidence.

Every read requires a current IAM `workforce.identity_manager.v1` or
`workforce.identity_reviewer.v1` assignment,
active independently reviewed Staff/login binding, live credential/membership and
current hospital scope. A legacy hospital manager or tenant permission alone is
insufficient. IAM's transaction guard revalidates authority and holds it through
Workforce source locks and sensitive-read audit commit. Missing assignment is 403;
invisible target or inactive actor is 404; unavailable authority is 503. Responses
use `Cache-Control: no-store` and contain no source data before successful commit.

Migration `0048_workforce_identity_source` initializes the internal employment
generation to 1 and increments it on every employment-status transition. Clients
cannot set the counter. This is an observed-state generation, not a hire date or
employment-period history. It does not implement exit/offboarding or verify an
identity. Forward-only rollback retains the generation and audit data while
stopping the consumer. RLS additions retain existing write/delete boundaries.

IAM's canonical template catalogue now includes separate identity manager and
reviewer templates. The existing assignment list remains leave-only; the additive
`GET /iam/identity-assignment-proposals` lists identity templates and
`GET /iam/staff-access-templates` exposes governance metadata. Existing assignment
proposal/decision/history/activation commands retain independent review, current
membership, scope epochs and immutable permission snapshots. The Staff access UI
selects the appropriate assignment family. Both HR templates can read the current
identity source; neither read performs a review decision. Identity/background/photo cases,
evidence review and card issue remain separate unfinished workflows (#1399).

### Identity review drafts

The additive `?projection=identity_drafts` source projection includes
`manage_checks_allowed`, derived from the current owner transaction guard. The
bare source response omits it for older strict consumers. Unknown, duplicate or
additional source query keys return 400. This capability is UI guidance; every
case operation independently checks current authority.

Authenticated routes below use the Global API Workforce destination. Workforce
owns both case heads and immutable revision snapshots, scoped to the verified
tenant, hospital and existing staff UUID:

| Method | Path suffix under `/workforce/hospitals/:hospital/identity-staff/:id` | Result                                        |
| ------ | --------------------------------------------------------------------- | --------------------------------------------- |
| PUT    | `/checks/:check`                                                      | Create or edit a draft using a UUIDv7 case ID |
| GET    | `/checks/:check`                                                      | Current case                                  |
| GET    | `/checks`                                                             | Bounded cursor-paginated current cases        |
| GET    | `/checks/:check/history`                                              | Bounded cursor-paginated immutable revisions  |

Every case request requires exactly one `X-Helivanta-Expected-Tenant` and one
`X-Helivanta-Expected-Subject` header, matching the verified principal. Missing,
duplicate, malformed or mismatched preconditions return 409 before disclosure or
mutation. These values are preconditions, never identity or authorization. Global
API forwards both headers without collapsing duplicates. An older gateway that
strips the subject precondition fails closed. Private responses are `no-store`;
the UI pins both values and removes case pages after their last viewer unmounts.

PUT requires the current hospital identity-manager assignment and an active,
independently reviewed actor staff/login binding. Reads permit either the current
manager or reviewer assignment; only an authoritative denial permits trying the
other role, while dependency outage returns 503. Legacy tenant-admin permissions
alone are insufficient. Inactive target employment retains readable history but
cannot accept new draft edits.

Canonical `IdentityCaseDraftInput` and `IdentityCase` are generated from
`contracts/workforce-identity-checks.json`. A command supplies kind (`identity`,
`background` or `photograph`), mandatory policy/check references, an observed UTC
calendar date, explicit validity instant, current source fingerprint/employment
generation, expected version and UUIDv7 command ID. Commands reject unknown fields
and bodies over 16 KiB. New cases expect version 0; edits advance exactly one
version. Original kind, case ID and proposer are immutable. Output timestamps are
UTC and observation dates are `YYYY-MM-DD`.

Within the existing IAM transaction guard, Workforce locks the case, verifies
current source/employment, and commits head, history, sensitive-access audit and
outbox together. Exact command replay reauthorizes and returns the original
immutable receipt even after a later edit. Conflicting command reuse or stale
versions return 409 `identity_case_conflict`; changed source returns 409
`identity_source_changed`; inactive target returns 409 `staff_inactive`. A receipt
is evidence of that command, not a current verification decision.

Migration `0049_workforce_identity_cases` adds forced-RLS heads/history, immutable
snapshot triggers, scoped indexes and draft-only write constraints. Direct SQL
cannot fabricate an approved status, rewrite history or use a stale employment
generation. No physical deletion is allowed. The event
`helivanta.in.workforce.identity_case_changed.v1` contains only hospital, staff,
case, version and status; policy/check text, names, birth dates, identifiers,
subjects and file locations are excluded. Audit metadata also excludes policy and
check text. There is no new event consumer or service.

This slice does not bind or upload evidence, submit or decide checks, revoke a
verification, issue a card, or implement employment exits. Migration 0049 has only
been applied to the synthetic local QA database; production rollout needs its
own named migration approval. Reverting the UI must preserve accepted drafts,
history and audit data.

## Employment-term draft preparation

Workforce owns private employment-term drafts under verified tenant, hospital and
Staff scope. These are preparation records, separate from accepted employment,
posting, access, contract renewal, suspension and separation. No draft command
changes directory employment status, a login link, IAM assignment, card or payroll.
This is the intake slice of the reviewed employment-period design and ADR-0049;
submission, protected evidence binding and independent approval remain open.

All routes below require `workforce.enrollment.manage` and a current explicit
hospital manager grant. A hospital reader cannot access these private records.
Missing permission is 403; unavailable Staff/hospital/sponsor scope is 404. Reads,
list/history and recovered receipts are attributable in `workforce_audit` and all
responses use `Cache-Control: no-store`. Expected tenant/subject headers pin the
UI request to its opening workspace; headers do not grant authority.

| Route beneath `/v1/workforce/hospitals/:hospital/staff/:id` | Behavior                                                                                             |
| ----------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `GET /employment-source`                                    | Minimal scoped Staff version and employment generation; no demographics or evidence contents         |
| `PUT /employment-periods/:period`                           | Create/edit a draft using UUIDv7 period/command IDs, expected draft version and current Staff source |
| `GET /employment-periods/:period`                           | Current private draft                                                                                |
| `GET /employment-periods`                                   | Bounded cursor page, newest creation first                                                           |
| `GET /employment-periods/:period/history`                   | Immutable revisions, newest update first                                                             |

The canonical input and response models are
`contracts/workforce-employment-periods.json`; regenerate with
`node scripts/generate-workforce-employment-periods.mjs`. Commands are bounded to
8 KiB. Required fields include engagement, explicit fixed/ongoing duration,
effective start, reason and appointment/contract reference. Fixed terms require
an explicit end later than start; ongoing terms forbid an end. Individual
contractors and agency workers additionally require a named contracting party
and another active Staff member in the same hospital as accountable sponsor.
A sponsor is not automatically the clinical supervisor or approving authority.
References do not establish verified documents, statutory compliance or identity.
Timestamps carry explicit offsets on the wire; the UI converts device-local input
to UTC and displays it in the device timezone.

An exact command retry returns its original immutable receipt after current
access authorization, including after later edits. Reusing a command for another
actor, period, Staff member or intent returns 409. Stale draft/source versions
also return 409. The UI freezes an uncertain submitted command for exact retry;
conflicts preserve entered terms and require reopening current state. Database
constraints retain scoped identity, legal term shape and sponsor/source checks;
triggers maintain immutable history. No delete/truncate route is provided.

Lock order follows enrollment: current hospital manager row, hospital supervision
advisory lock, target Staff SHARE row, optional sponsor SHARE row, then draft/head
write and owner-maintained snapshot/audit in one transaction. This draft path
calls no remote provider while holding locks and publishes no integration event.
Future employment decisions must add the reviewed identity/link and consumer
activation guards; this intake permission is not an employment-approval grant.

Migration `0050_workforce_employment_period_drafts` is additive and backfills or
approves no legacy record. Rolling application code back leaves its tables and
history intact; removal requires a separate approved retention/migration action.
Production rollout/migration of this new slice is a separate release gate.

## Protected evidence for employment terms revisions

Workforce owns these private bindings as well as the source Staff documents. All
routes below are beneath
`/v1/workforce/hospitals/:hospital/staff/:id/employment-periods/:period/revisions/:version/evidence`.
UI requests use the Global API and pin the expected tenant and subject. Every
route requires both freshly resolved `workforce.enrollment.manage` and
`workforce.documents.manage`, plus a current explicit hospital manager grant.
The two permissions are checked in the same refreshed permission set. Neither
permission grants employment approval or operational access.

| Route suffix              | Behavior                                                                            |
| ------------------------- | ----------------------------------------------------------------------------------- |
| `PUT /:binding`           | Create-only UUIDv7 binding; command UUIDv7, protected document UUID and reason only |
| `POST /:binding/withdraw` | Terminal, reasoned withdrawal with command UUIDv7 and expected binding version      |
| `GET /:binding`           | Current private binding metadata                                                    |
| `GET /`                   | Cursor-paginated bindings for this exact terms revision                             |
| `GET /:binding/history`   | Cursor-paginated immutable binding revisions                                        |
| `GET /:binding/content`   | Two-phase reauthorized download of the exact released file generation               |

Canonical models are in `contracts/workforce-employment-periods.json`; regenerate
with `node scripts/generate-workforce-employment-periods.mjs`. Evidence commands
are bounded to 4 KiB. The attachment command does not accept client-supplied scan,
checksum, generation or release facts. It accepts only an available, scanned
`enrollment` / `employment_evidence` PNG/JPEG owned by the same Staff member and
hospital, at the terms' Staff source version. The terms revision must still be
current and its Staff version/employment epoch must match the current source.
No upload format, document purpose, scanner bypass or storage copy is introduced.

Bindings pin the immutable period revision ID, Staff source/generation, document
record version, filename/type/size/checksum, scan attempt/time and exact source
and released object generations. Scanner implementation/definition facts stay
internal. At most 20 active bindings may exist per terms revision; the same file
cannot have two active bindings there. Version 1 is attached; version 2 is
withdrawn. Binding contents cannot be rewritten or reactivated. A new binding
requires a new reasoned command. Exact retries return their original immutable
receipts after current authorization, including after withdrawal, without
reattachment. Intent/actor/source conflicts return 409; transient lock conflicts
return 503 and require retrying the same command. Wrong or unauthorized object
scope is not disclosed.

Historical metadata and withdrawal remain available to currently authorized HR
even after the Staff basis changes. A download additionally requires an attached
binding and an available matching document. Authorization, release and binding
are checked before storage I/O and again before bytes leave the API; withdrawal
or revocation during storage retrieval prevents release. The browser also checks
size and SHA-256 before offering a download. Metadata availability does not
promise storage availability or validate the truth of the employment claim.

Migration `0051_workforce_employment_evidence` adds bindings and immutable history
with forced private RLS, scoped foreign keys and trigger-enforced snapshots. The
write lock order is hospital manager grant, hospital advisory lock, Staff, terms,
document, then binding/history/audit. The content gate follows the existing
file-owner two-phase document-before-binding order. No remote call runs within a
write transaction. Application rollback preserves the new private tables; no
legacy evidence is automatically bound, approved or deleted. Production rollout
and migration remain a separate exact-revision approval gate.

### Immutable employment submissions and independent decisions — source candidate

The current implementation candidate adds these owner routes through Global API:

| Route                                                                                                               | Behavior                                                                                                   |
| ------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `PUT /v1/workforce/hospitals/{hospital}/staff/{staff}/employment-reviews/{review}?hr_intake=v1`                     | Create an immutable pending submission under UUIDv7 review and command IDs                                 |
| `GET /v1/workforce/hospitals/{hospital}/staff/{staff}/employment-reviews/{review}?hr_intake=v1`                     | Protected frozen record, recorded/current states and owner-computed preparer capabilities                  |
| `GET /v1/workforce/hospitals/{hospital}/staff/{staff}/employment-reviews/{review}?employment_review=v1`             | Review-only selected source metadata and owner-computed decision capabilities                              |
| `POST /v1/workforce/hospitals/{hospital}/staff/{staff}/employment-reviews/{review}/decision?employment_review=v1`   | Independent approval/rejection, exact expected version, UUIDv7 command, reason and both human attestations |
| `POST /v1/workforce/hospitals/{hospital}/staff/{staff}/employment-reviews/{review}/withdrawal?hr_intake=v1`         | Original preparer withdraws pending terms, including stale submissions                                     |
| `POST /v1/workforce/hospitals/{hospital}/staff/{staff}/employment-reviews/{review}/revocation?employment_review=v1` | Independent reviewer revokes approved terms, including unavailable evidence/source                         |

Preparation requires exact original expected-tenant/subject headers, both live
scoped HR-intake permissions, and an active independently reviewed actor Staff/login
link. Reviewer GET/decision/revocation instead require the separate current
hospital-scoped `workforce.employment.review` permission and reviewed actor link.
Exactly one projection is required; missing, mixed and duplicate modes are refused. Neither legacy administration nor identity-review permission supplies the
new command authority. An administrator with separately reviewed scoped HR
permissions uses those grants, with no legacy fallback. The target may be inactive
or non-digital; submission neither activates employment nor creates a login,
IAM grant, card or clinical privilege.

Canonical `workforce-employment-reviews.json` generates the Go/Zod contract. An
8 KiB command pins current Staff version/epoch/hash, exact open terms ID/revision/
version, exact current approved person finding ID/revision/version, one to twenty
explicit binding ID/revision/version selections, policy reference/reason and both
required human attestations. Unknown fields, missing/null versions, duplicate
selections and absent attestations are rejected. Policy references are recorded
human references, not automatic legal-policy activation or certification.

Workforce migration `0060_workforce_employment_review_submissions` retains head,
immutable history/receipt, selected immutable evidence-history references,
classification revision and exact contributor set. Its owner SQL source gathers
Staff preparation, historical terms authors, selected binding/document authors
and the person finding's frozen preparers. A withdrawn role does not erase an
older terms author. Incomplete document origin, unreleased evidence, changed
references, expired/revoked person findings and inactive sponsors fail closed.
Child sets cannot be added or rewritten after their submission transaction.

Head/history/children, minimized success audit and identifier-only
`helivanta.in.workforce.employment_review_changed.v1` outbox commit together.
Exact replay reauthorizes and returns the immutable command receipt, including
when current applicability later changed. Changed intent or command reuse returns
409; a receipt never restores evidence or authority. Later terms edits, sponsor
inactivation, finding revocation and file revocation change current applicability
while retaining recorded facts. Sensitive reads are audited. Scoped person-finding
read purpose is limited to the selected finding and cannot mutate it.

Workforce forward-only migration `0061_workforce_employment_review_decisions`
adds pending → approved/rejected/withdrawn and approved → revoked transitions.
The immutable source, selected children and original transaction ID cannot change;
every revision/receipt remains attributable. Both decisions require a trimmed
reason and actual terms-examined/independence attestations. Self, declared-subject,
frozen-contributor and historical Staff/login associations cannot review or revoke.
Withdrawal/revocation clear current-action examination flags; earlier approval
attestations remain in history. Ending a stale record does not require eligible
source evidence. Approval revalidates current Staff/draft/finding/binding/release/
scan/classification/provenance in the application and owner SQL. Head, history,
minimized decision audit and identifier-only outbox commit atomically.

IAM additive migration `0021_iam_employment_templates` declares only the new
review permission in `workforce.employment_reviewer.v1`. `employment-v1` exposes
five exact definitions; existing `legacy-v1` and `hr-intake-v1` retain three and
four. Staff Access offers a separate employment assignment area only after exact
server catalog verification, and blocks retained actions when verification fails.
The tenant-admin registry declaration does not replace scoped owner authority.

GET distinguishes recorded status (`pending`, `approved`, `rejected`, `withdrawn`,
`revoked`) from current applicability, including `source_changed`,
`person_unavailable` and `evidence_unavailable`. It returns `can_approve`,
`can_reject`, `can_withdraw`, `can_revoke`; capabilities require current authority,
independence and legal state. Later draft edits invalidate pending submissions;
they do not alone invalidate approved historical terms. Receipt retries never
restore revoked authority/evidence or activate employment.

Review-purpose RLS permits selected terms/binding histories, selected file and
classification metadata, selected finding/contributors and its specific sponsor.
It preserves original writes and restrictive deletes, granting no general
enrollment or private-file byte authority. Migration rollback must preserve
history: use forward correction/application rollback, not a destructive down
migration or restoration of old permissions.

This remains backend/SDK and assignment-setup source with local protocol-fixture
evidence, not deployed or accepted through the normal browser/provider flow.
Reviewer-specific protected bytes, review inbox/detail/history workspace,
preparer/reviewer UI, explicit operational activation and every lifecycle/consumer
cutoff remain open. See the [review design](../superpowers/specs/2026-10-31-independent-employment-review.md).

### Evidence contributor provenance — source candidate

Workforce migration `0059_workforce_employment_document_provenance` extends
immutable, private contributor capture to enrollment `employment_evidence` as
well as `identity_evidence`. Attributable document creation records the private
`creation_captured` marker; authorized later state changes retain their subjects
without replacing the creator. Processing workers are excluded from person
preparer attribution. Existing records remain unproven: neither migration nor a
later upload/scan reconstructs missing historical preparers.

This changes no public request/response fields or permission grants. Identity
person-review submission refuses selected files without captured creation with
`409 person_review_provenance_unavailable`; current review effectiveness and
protected release derive `evidence_unavailable` when that proof is missing.
Historical review receipts remain intact. Independent employment submission and
approval must consume complete frozen contributor evidence in their own workflow;
capture alone does not approve employment, create an operational generation or
change access. Migration 0059 is a source/test candidate, not evidence of an
existing QA or production upgrade. See the [lifecycle design](../superpowers/specs/2026-10-29-reviewed-employment-periods.md).

## Withdrawal of prepared employment terms

`POST /v1/workforce/hospitals/:hospital/staff/:id/employment-periods/:period/withdraw`
closes an unsubmitted draft. It uses the same enrollment permission and explicit
current hospital manager check as draft preparation, and the same expected
subject/tenant request headers and `no-store` response. It changes no employment,
login, role, payroll, source Staff record or document-release state.

The canonical `EmploymentPeriodWithdrawInput` requires UUIDv7 `command_id`,
`expected_version` >= 1 and a nonblank reason of at most 500 characters. The
command body is bounded to 4 KiB. Only `draft -> withdrawn` is legal. It advances
one revision and freezes the terms, proposer, source generations and appointment
reference. The terminal revision's `reason` records cancellation; earlier reasons
remain in immutable history. A current manager may close obsolete preparation
after the target Staff source or sponsor changed without restoring that source.

Exact actor/object/intent retries recover their original immutable receipt after
current access authorization. Changed intent, stale versions, reused commands,
editing or withdrawing a terminal draft return 409. A missing route permission
returns 403, a foreign/unavailable Staff or hospital returns 404, invalid input
returns 400, and unavailable persistence fails closed. Existing audit records
include withdrawal and receipt recovery. No delete or reopen command is provided.

The new lifecycle read projection is explicit: append `lifecycle=withdrawal-v1`
to the existing detail/list/history reads. It includes withdrawn records and their
terminal revisions. Unknown or duplicate lifecycle values return 400. Without
this parameter, current/list/history responses retain their original draft-only
status contract; detail of a withdrawn current record returns 404. The UI opts
in, displays the reason and retained history, and removes edit/withdraw/attachment
controls for the terminal revision. Historical protected evidence remains subject
to its existing current authorization and audited download checks.

Migration `0052_workforce_employment_withdrawal` expands status constraints and
adds trigger-enforced finality, immutable withdrawal terms and rejection of new
evidence on a closed draft. It changes no legacy business records. Migration and
application are delivered together; rollback retains the schema/history and old
application writes cannot reopen terminal records. The older read contract stays
draft-only. There is no destructive down migration. Approval for the exact
production revision and migration remains required before deployment.
