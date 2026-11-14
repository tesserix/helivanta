# Staff reviewer governance

Status: candidate in PR #1669; not merged or deployed. The current implementation
covers owner grant admission, retained creation/decision receipts, current auditor
review, an owner inbox, hospital/pharmacy/supplier account-link review consumers,
hospital person/employment consumers and selected employment preparation UI.
The hospital assignment consumer below is a new candidate awaiting real-service
acceptance. Assignment UI, independent operating-duty review, acknowledgements
and delegation commands in the UI remain unfinished. The owner inbox is available
in Staff Access. Account initiation alone does not complete Staff onboarding.

IAM owns the governance ledger. Workforce owns the hospital/independent employer
roots and current hospital count. Native identity remains with the identity owner.
Canonical Go/Zod models derive from `contracts/staff-governance.json`; regenerate
with `node scripts/generate-staff-governance.mjs` and check with `--check`.
The [implementation design](../superpowers/specs/2026-11-13-staff-governance-bootstrap.md)
records ORG-D7, all remaining lifecycle consumers and operational assumptions.

## Candidate hospital assignment administration

IAM0049 adds a private request/provenance and owner-answer ledger. The fixed
hospital-only `iam.scoped_access_reviewer.v1` grants nonclinical assignment
management/review authority. Admission requires a current native tenant owner or
the exact current hospital grant through the existing guarded governance source.
Coarse `governance_delegate` permissions alone cannot admit a command. Ordinary
assignment endpoints, customer onboarding and owner security retain their
owner-only admission; this template grants no owner security or clinical role.

The new candidate family is `/v1/iam/hospitals/:hospital/assignment-proposals`:

| Method and suffix    | Behavior                                                                                                     |
| -------------------- | ------------------------------------------------------------------------------------------------------------ |
| `PUT /:id`           | Existing canonical `ProposalInput`; hospital must equal the selected root.                                   |
| `GET /:id`           | Current canonical `ProposalOutput`, without private source/request details.                                  |
| `GET /`              | Existing bounded keyset list; optional single `family` from the canonical template catalog, default `leave`. |
| `GET /:id/history`   | Existing immutable proposal history and paging.                                                              |
| `GET /:id/decisions` | Existing immutable decision history and paging.                                                              |
| `POST /:id/decision` | Existing expected-version approve/reject/withdraw command and canonical response.                            |

Lists have a maximum of 50 rows per page. Continuations bind the actor, tenant,
selected resource parameters and filters, including the assignment family.

Every call requires the authenticated session and exactly one matching expected
tenant and subject header. Business calls use the Global API IAM destination.
Selected-root restrictive policies fence proposal, history, decision, snapshot,
scope-epoch and owner-answer access. Sensitive reads retain an audit record.
IAM0049's support check requires installed forced RLS, exact policy/function
definitions, enabled critical triggers and no runtime TRUNCATE privilege.
Missing or altered support and unavailable authority produce 503; callers retain
the original ID, observed version and intent for retry. Stale/different commands
or source adoption conflict with 409. Unauthorized writes receive 403; hidden
read scope/objects receive 404. Revoked credentials require fresh authentication.

Canonical membership incarnation, proposer/target separation, input validation,
immutable origin/history, CAS versions and projection rules are unchanged.
Native replay also requires the original owner-membership or grant/version and
actor membership incarnation; another source cannot adopt a retained command.
Owner and reviewer source details, authentication times and membership epochs
remain private. The request ledger supplements canonical command evidence; it
does not become another assignment master or carry identity tokens.

`helivanta.in.iam.assignment_governance_scope_requested.v1` carries only canonical
proposal, request and hospital IDs. Workforce answers on
`helivanta.in.workforce.assignment_governance_scope_verified.v1` with those IDs
and an explicit boolean hospital existence/type verdict. Both DTOs derive from
`contracts/staff-access.json`. No actor, Staff identity, grant, bearer proof,
manager access or cross-tenant destination travels in this protocol. The existing
legacy manager-based verification event keeps its original semantics and cannot
settle native requests. Department proposals still require Organization's exact
department answer as well as Workforce; a refusal prevents approval immediately.
Only the first owner answer for the exact current request is retained.

The event transaction stores facts without approving the command. On every
delivery, including an already-claimed retry, the after-commit phase acquires a
fresh governance source and compares it to the original source before locking
the exact pending request. Approval and outbox publication remain atomic with
canonical snapshot creation. An unavailable source retries without approval.
Known revoked, expired or replaced authority returns a pending decision to
`pending_review`, or rejects an initial unverified scope. That denial uses a
separate bounded transaction permitted only to downgrade state; it cannot create
an approved snapshot. Projection continues to check target membership,
snapshot version, scope epoch and expiry after this approval pivot.

Local unit/build checks do not validate IAM0049 or the full workflow. The
real-service admission and interrupted-settlement regressions must pass in CI.
Department answer-order, fault/replay, browser, load and live release evidence
remain required. IAM0049 has not been deployed or approved for a live migration.

## Candidate operations

Browser calls enter Global API with the IAM destination. Backend paths below
are `/v1`; preserve the Global API client's existing path mapping.

| Operation                           | Current authority and outcome                                                                                                                                                                                                                                                      |
| ----------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT /v1/iam/governance-grants/:id` | `iam.governance.manage` plus a current IAM owner and credential watermark. A canonical UUIDv7 command admits a separately named recipient, subject to native eligibility, current membership incarnation and exact Workforce scope. Returns 202 with an original creation receipt. |
| `GET /v1/iam/governance-grants/:id` | `iam.governance.read` plus current owner or tenant-auditor source proof; returns retained origin with current stored status/version. It does not report effective route or product access.                                                                                         |
| `GET /v1/iam/governance-notices`    | `iam.governance.read` plus current owner proof; bounded cursor list limited to that owner's own recipient subject.                                                                                                                                                                 |

`PUT /v1/iam/governance-grants/:id/decisions/:command` requires
`iam.governance.review` and a current owner or tenant-auditor source. Both IDs
are canonical UUIDv7. The body contains observed `expected_version`, supported
`decision` (`approve`, `reject`, `revoke`) and reason only. The expected version
must leave room for its safe integer successor. A 202 decision receipt identifies
the authenticated actor and resulting version; it does not replace the current
head or imply product access.

Every operation refreshes authentication/route admission and requires exactly one
`X-Helivanta-Expected-Tenant` and `X-Helivanta-Expected-Subject`, matching the
verified session. They detect an unintended account/workspace switch and never
select authority. Responses are private and non-cacheable.

Creation accepts only subject, expected membership epoch, fixed template key,
scope kind, optional exact root ID, reason and effective interval. The body is
bounded to 4096 bytes. Duplicate/unknown/case-alias keys, actor/tenant/role fields,
nulls, invalid UTF-8, unsafe integers and malformed or precision-losing instants
are refused. Reasons have a shared 500 UTF16-unit bound and no control characters.
No contact, password, proof document or native organization is supplied.

The target must already have ordinary tenant membership and be an active verified
tenant-owned human. An owner cannot issue to themselves or another current owner.
Native eligibility is read outside database locks. Creation then rechecks owner,
credential, membership incarnation, exact employing root and topology under a
single guarded database command. The native read is an observation, not a
cross-service atomic transaction or proof of subsequent login.

One current owner and at most one hospital admit `provisional_active`, with
`single_owner_provisional` provenance. Multiple owners admit `pending_review`,
with `independent_review` provenance. A tenant with multiple hospitals and fewer
than two current owners is denied. No synthetic independent approval is recorded.
Second-hospital creation's owner-count admission guard remains unfinished; the
Workforce topology gate alone only serializes observations against root writes.

A command's canonicalized intent and original receipt are retained immutably.
Repeating the same command and intent returns the original 202 receipt; changed
intent or issuing actor returns 409. Receipt replay does not repeat native
provisioning, reissue a grant, or claim that its authority is still effective.
Creation history remains distinct from the mutable current head.

## Independent review and source lifetime

Issuer and recipient cannot approve/reject their own grant. A current owner may
withdraw their issued grant through `revoke`; a recipient cannot self-review.
Approval moves pending/provisional state to `active`, rejection to `rejected`,
and revocation to `revoked`. Decisions and original receipts are immutable; a
repeated command returns its original decision after later head changes. Changed
intent, actor, command namespace or observed version conflicts. Creating a new
command never bypasses a stale version.

Auditor authority comes from a current tenant-scoped
`iam.governance_auditor.v1` grant and current managed IAM membership incarnation.
Provisional authority requires exactly one current owner matching its original
issuer; active authority requires retained independent approval. Credential,
interval, owner/topology and source status are rechecked under retained locks.
The command deadline cannot outlive the auditor's source grant.

Source and target grant IDs are locked in sorted order before actor membership
and Workforce topology. This preserves revocation ordering and avoids inverted
cross-auditor locks. Private source/lock purposes disclose no parent payload to
the caller before authority validation. Source table RLS avoids recursive
self-lookups. Approval checks target native eligibility outside database locks,
then rechecks source, exact target root, membership incarnation and version.
Reject/revoke and retained receipt recovery do not re-enable or reprovision the
target account.

A retained decision records explicit owner/auditor provenance and, for delegated
review, the exact source grant/version and actor membership epoch. It does not
fabricate Staff or employment identifiers. A decision, head transition, new
notices for every current owner, audit and any routing projection change commit
together. Revocation denies from current source immediately; remaining future
or current unexpired grants retain the coarse routing identity. Generic role
creation/removal cannot manage that identity outside its source workflow.

## Atomicity, scope and routing

Grant creation, one durable notice for each current owner, audit and any managed
routing-membership projection intent commit together. Inbox evidence means a row
was recorded, not that an email was sent or read. Notice acknowledgement is not
yet exposed.

The single fixed `governance_delegate` membership is managed by this ledger.
The generic membership grant API returns 409 for it. Existing member outbox
consumers/reconciliation project this coarse routing identity asynchronously.
No direct native administrator role is assigned. A routing tuple or stored
`provisional_active` status is not effective hospital, HR, clinical, pharmacy,
stock or financial authority. Actual consumer guards must check current exact
source, interval, membership incarnation, credential and product prerequisites;
the candidate implements these checks for account-link review. HR/employment,
assignment and operating-duty consumers remain unfinished.

Candidate IAM0048 permits this source-managed routing membership both before and
after an IAM employment basis is recorded. The managed-source insert guard still
rejects raw routing-role insertion, and the generic membership API cannot grant
it. Employment continues to block legacy owner/clinical role elevation. Adding
the routing role preserves the membership incarnation; it does not advance its
epoch or establish operating authority.

IAM retains an owner-count gate before target membership and Workforce topology
locks. An owner-source lookup reads the stable owner membership under the shared
owner-count gate without taking a tuple lock; owner removal may already hold
that tuple while waiting for the exclusive gate. Workforce root insert/update/delete
statements take the exclusive topology gate before row locks; the source provider retains its shared counterpart through
the consuming callback. Missing/duplicate canonical providers, unavailable
sources, context/tenant/database escape or an expired command deny.

Missing permission/current owner returns 403. Invisible grant or employer returns 404. Changed workspace, intent or source version returns 409. Invalid body is
400; a credential watermark denies with 401; unavailable authority/source or a
command deadline is 503. Preserve v1 `{error,message}` envelopes; request IDs
remain in headers.

## Shared command guard

`platform.Deps.GovernanceSources` binds the sole IAM and Workforce owners to
`authz.CurrentGovernanceSourceGuard`. Its closed capability catalog currently
contains `GovernanceAccountLinkReview` for one exact hospital/pharmacy/supplier
employer and `GovernancePersonPrepare` / `GovernancePersonReview` for an exact
hospital only. Current tenant owners retain an explicit
owner source backed by their actual IAM owner-membership ID and epoch. Other
reviewers require the exact versioned grant for the selected capability. Neither source
requires an invented Staff actor or clinical role.
Products construct the request from the verified principal and canonical employer;
request bodies cannot choose an actor or supply source evidence.

The guard reuses the ledger's bounded source selection, sorted source locks,
current membership incarnation, credential/session gate, owner-count rules,
retained approval and current employer topology. Source expiry bounds the command.
The product callback runs in the same guarded transaction; source-use audit and
product writes roll back together. Returned grant/version/membership evidence is
for audit retention; its authority lasts only during that synchronous callback.
Asynchronous assignment settlement needs separate current-source revalidation.

Only the auditor wrapper enables private audit access to other grants. Ordinary
source consumers retain access to their selected source rows. Missing/duplicate
canonical owners or unavailable source dependencies fail closed. This provider is
wired in the API and test harness; Hospital and independent account-link review/read paths now adopt it in the
candidate. Source-only reviewers use a selected-resource database purpose;
owner/grant source evidence is retained with each reviewed link version. The
remaining HR/employment, assignment and operating-duty consumers are unfinished.
Real PostgreSQL/FGA owner and exact-employer grant consumer scenarios passed at
`ede90d7e` in CI `43391789169`, for hospital, pharmacy and supplier employers.
These scenarios use actual IAM grants and owners without private reviewer manager
rows. Current-head in-flight revocation and failure-atomicity cases still require
remote acceptance; earlier green runs do not establish their result.

## Client integration

`@helivanta/api` exposes `issueGovernanceGrant`, `readGovernanceGrant`,
`decideGovernanceGrant`, `listGovernanceNotices` and `newGovernanceCommandId`. Callers retain the command
ID and exact input for lost-response recovery. A recovered issue receipt remains
the original version; read the current head separately.

Requests use the account-scoped Global API client and cancel on workspace
transitions. The SDK validates canonical input before network I/O and binds
responses to the requested tenant, grant and decision command. Issue responses
also bind the issuer and original input; decision responses bind the reviewer,
input and successor version. Instant comparisons retain microsecond precision
and accept equivalent UTC/offset representations. These functions do not add a
delegation or decision form or authorize Workforce consumers. Staff Access exposes
the owner inbox when governance management/read permissions are present. The
server still requires current owner authority. Its 50-row pages bind every notice
to the current tenant and recipient, reject duplicate rows and invalid continuation,
and discard in-flight responses after account/workspace changes. Opening a notice
reads the current grant separately from the notice's historical grant version.
The view displays stored status and validity dates, without claiming effective
operating authority. Grant issuance, decisions and acknowledgements in the UI
remain unfinished.

## Candidate employment submission and review consumers

The hospital employment-review endpoints now use a tagged authority union:
existing current scoped Staff authority, or a current IAM owner/exact employment
preparer or reviewer grant. Governance actors do not acquire Staff/link identities,
hospital-manager rows, payroll permissions or operating assignments. Submission
requires the preparation/document bundle on the Staff path, or the separate
`workforce.employment_preparer.v1` source; decisions require the scoped review
assignment or `workforce.employment_reviewer.v1`. The routing role alone is not
authority. Both paths require exact tenant/account preconditions and share a
five-second command budget. Business-callback failures and unavailable scope
authorization cannot trigger an alternative-source retry.

New candidate `0105_workforce_employment_review_authority` binds the private
purpose to tenant, account, hospital, Staff and review. Governance preparation
freezes existing draft terms, a current approved person finding, released evidence
and contributors; it does not add general HR draft/document authoring. Governance
review reads only the selected review and its sources. Restrictive policies apply
to all existing tenant-scoped Workforce tables: master/payroll/clinical writes
remain denied during this purpose even if the owner also has legacy manager
access. The only writes are the guarded review, frozen children/history,
owner-maintained cohort record and attributable review audit. Normal scoped
commands retain their existing path.

The private `governance_authority` column on the head and immutable history retains
actual owner membership or grant/version, membership incarnation, owner count,
capability, target and lease/authentication times. The existing immutable-source
trigger runs before the recorder; the recorder runs before the history snapshot.
Absent, disabled or mismatched recording/history support, unforced tenant RLS
or a missing restrictive purpose/delete policy fails closed before disclosure or
command work. The runtime application role must have no TRUNCATE privilege on
these tables. Future tenant-scoped Workforce tables must add both policies
(default deny when unrelated); otherwise governance review stays unavailable. Evidence release reauthorizes after storage I/O and compares
both the source authority and frozen evidence/source snapshot. No private proof
is added to public review responses or events.

The remote test first reproduced a 403 at submission after real draft preparation,
scanning and grant approval. Candidate 121a3495 then passed submission, receipt
replay, exact-grant denials and protected detail before encountering the missing
canonical `workforce.employment_review.decided` audit action in the purpose
allowlist. That mapping is corrected; full current-head PostgreSQL/FGA,
concurrency and rollback acceptance remains pending. First activation, authority-aware discovery,
reviewer UI, selected draft/document authoring and full Staff/pharmacy acceptance
are separate unfinished gates. Approval of terms leaves Staff inactive.

## Candidate first employment activation consumer

The activation source and command now have a separate candidate nonclinical
union. Existing scoped actors retain their exact activation/enrollment assignment
bundle and real reviewed Staff identity. A current owner or exact hospital
`workforce.employment_activator.v1` grant may instead inspect the selected approved
basis. A mutation requires IAM's typed `GovernanceEmploymentActivationGuard`,
which supplies an exact-target writer only inside the guarded transaction. The
consumer never substitutes a Staff actor, HR-intake link or generic IAM source
for that writer. Scope dependency errors and entered business callbacks cannot
trigger a fallback to an alternative source. A shared five-second budget applies.

Candidate WF0106 gives activation its own private purpose, distinct from review.

Read access is limited to the target Staff, selected review and required frozen
source records. The only writes are the exact Staff transition, its immutable
generation and attributable activation audit. A final BEFORE UPDATE fence rejects
changes to Staff identity, job, contact or other master columns and checks the
expected version/epoch and approved basis. The existing deferred receipt guard
still requires a matching generation before commit. Ordinary IAM membership,
basis/history/proof and Workforce state remain in the same command transaction.

Each new generation privately retains the actual owner/grant source, lease,
membership incarnation, owner count and exact command target. Public generation
models/events omit that proof. Missing/mismatched recording, transition or receipt
support, missing purpose/delete policies, unforced RLS or runtime TRUNCATE
privilege denies governance activation before disclosure. New Workforce tables
must acquire both private policies. Binary rollback preserves retained proof.

The real Staff governance suite on `b0cbe6ec` passed in CI `43477936807`,
including first digital activation/replay, atomic rollback, retained authority,
revoked access and bounded employment authority discovery. Full current-head
regression, native login and operating-access acceptance remain separate. No
runtime activation flag changes are included; reconcile the deployed flag against
authorized GitOps configuration before rollout. This result does not establish
operating-duty approval, reviewed account linking, scope publication, reviewer UI
or actual pharmacy access.

Candidate WF0107 adds the distinct hospital employment-authority inbox. Its
private browse purpose admits current owners or exact preparation/review grants,
minimal summaries and an exact read audit, while denying master writes/deletes
despite inherited manager access. Its sealed cursor binds hospital and filters
as well as the account/tenant/route. The existing linked-Staff inbox remains
unchanged. No Staff/link/review identifiers are manufactured. The candidate
employment-review page now defaults to governance discovery/inbox and provides an
explicit reviewed-Staff alternative. Query keys and selected private data are
separated by path; dependency errors never trigger a source fallback. Local
component acceptance covers review opening, path changes and 403/503 discovery
failures. Real database/browser acceptance and finding/terms/document authoring
remain pending. WF0107 extends the existing single permissive tenant policy while
its restrictive purpose/delete policies bound every operation; the global RLS
validator's single-policy requirement is unchanged.

## Release and remaining acceptance

Candidate migrations are `0047_iam_governance`,
`0102_workforce_governance_topology` and `0103_workforce_login_review_authority`.
User rollout approval has been received; these migrations have not been applied
to the live environment. New candidate
`0104_workforce_person_review_authority`,
`0105_workforce_employment_review_authority`,
`0106_workforce_employment_activation_authority`,
`0107_workforce_employment_authority_inbox`,
`0108_workforce_employment_inbox_staff_index`,
`0109_workforce_employment_governance_findings` and
`0048_iam_governance_employment_bases` have not been approved for live application.
Before rollout, complete the remaining source consumers, owner-count admission,
ledger/role admission guards, frontend, remote PostgreSQL/FGA/race/recovery tests
and tested rollback. Retain identities, original receipts, notices and decisions
through rollback; never erase evidence to retry onboarding.

The real PostgreSQL governance suite passed in CI run `43377058047`, including
independent decisions, stale source rejection between preflight and commit,
original receipts, owner-notice privacy and opposing auditor revocations. Its
first decision test returned 404 before implementation. Current-head Go, web,
supplier and browser acceptance remains pending. The historical upgrade fixture
checks that current review commands return 503 before source provenance exists;
retained prerequisite approvals use the historical schema with its independence,
history and audit triggers. Once 0103 is installed, reviews use the current API.
These fixtures do not establish modern IAM authority; the real owner/grant tests
above provide that evidence. The actual preserved tenant still needs recipient-operated
private setup, real distinct reviewers and positive/negative/revoked staff and
pharmacy-access verification before this workflow is complete.

## Candidate nonclinical person-review consumers

The fixed hospital-scoped `workforce.person_preparer.v1` and
`workforce.person_reviewer.v1` templates add separate person-finding preparation
and review authority. They do not authorize identity-case changes, document
uploads, employment preparation/activation, assignments, postings or pharmacy
operations. The existing `projection=identity_drafts` source view keeps its
reviewed Staff/assignment guard; person authority cannot advertise draft rights. Ordinary reviewed Staff assignments remain supported. Only a denied
ordinary authority path, before invoking business code, can select a current
owner or exact person-governance source. Dependency failures do not become an
owner fallback. Governance requests require matching expected tenant/account
headers and refreshed authentication/route permission.

Current-person source reads, selected evidence metadata, person-review commands,
history and protected evidence release use the explicit authority union. No
Staff/link reference is fabricated for an owner or delegate. File disclosure
reauthorizes the same capability, source ID/version, membership incarnation,
owner census and expiry after storage I/O. A source change denies release even
if another grant retains coarse routing membership. Existing target association,
contributor, independent-person, evidence and legal-state checks remain.

Candidate `0104_workforce_person_review_authority` adds private provenance to the
review and its immutable history. A server-owned trigger records each governed
version; missing/disabled recording support denies the governed path. A
restrictive policy forbids this purpose from writing Staff, profile, document or
login-link masters. Public review/event payloads are unchanged. Approval of
IAM0047/WF0102/WF0103 does not authorize this new live migration. Before applying
0104, complete real PostgreSQL/failure/disclosure acceptance and rollback review.
Rollback disables the new consumers and retains provenance columns and history;
do not drop evidence to undo the code rollout.

The person-review workspace now uses a separate authority discovery route and
source-checked target/inbox reads, described in the Workforce contract. The legacy
actor-link discovery route and cursors remain unchanged. Delegation UI, employment
and activation still need completion before claiming first-staff onboarding works.
The new source/snapshot unit tests pass locally; database and protected-release
cases are candidates awaiting remote execution.

### Employment source/basis guard candidate

The canonical catalog additionally defines separate hospital-only employment
preparer, reviewer and activator templates. Their permissions are respectively
`workforce.employment.prepare`, `.review` and `.activate`; these confer no clinical,
stock, dispensing or operating assignment. Existing grant/notice/decision shapes
remain unchanged. The v1 template enum grows additively in the generated Go/Zod
contracts; consumers must refresh their catalog and fail closed on unknown keys.

IAM's `GovernanceEmploymentActivationGuard` supplies a source- and Staff-bound
basis writer only inside a current guarded callback. It acquires the same
exclusive employment-basis gate before authority/product locks as scoped
activation. It rejects foreign tenant/hospital/Staff, self activation and retained
or different command contexts. Source expiry and credential/membership checks
remain active; product failure rolls back the basis and its history/audit.

Targets must have actual ordinary membership. An additional nonclinical
`governance_delegate` membership is permitted, while delegate-only and any
clinical/owner/other legacy role combination remain ineligible. The writer never
creates membership or any product operating assignment.

This is an IAM guard and contract candidate. Workforce preparation/review,
provenance recording, activation consumers, discovery and UI still need adoption;
existing endpoint preconditions remain until that work is verified. It must not
be presented as completed first-Staff onboarding or pharmacy access.

IAM migration `0048_iam_governance_employment_bases` is an additional unapplied
candidate. It records immutable private owner/grant evidence against each
resulting basis generation and checks the current IAM source identity, version,
scope and membership epoch. It neither backfills evidence nor rewrites prior
basis/history rows. The typed writer requires the recording table and its enabled
matching trigger before admitting the product callback. A disabled/missing
recorder is unavailable, rather than a successful unattributed activation.
Rollback disables this new consumer and retains provenance/history. New IAM0048
live approval is required separately after database/rollback acceptance; no
production migration or rollout was performed here.

Legacy scoped Staff identity-source reads retain their optional workspace-header
behavior. Supplied mismatched or ambiguous expectations still fail. Governance
fallback requires both exact tenant/account headers, so this compatibility does
not admit owner/delegate operations with an unbound workspace. Staff-source audit
keeps an explicit `scoped_staff` kind alongside the six existing metadata fields;
no clinical or personal values enter that read audit.

The candidate selected-finding route `employment-person-finding-options` now
requires current hospital employment preparation authority without a linked Staff
actor. WF0109 binds private provenance and read/audit-only RLS to the selected
Staff and expected source version/epoch; no review ID is invented. Cursors bind
both actual hospital and Staff plus basis filters; the prior single-resource
inbox cursor scope is unchanged. Local proof, multi-resource cursor and SDK
routing/denial tests pass. The real owner/delegate, revoked source, missing-policy,
released-evidence and inherited-manager denial cases passed in the governance
suite on CI 43506648429 (a258bc16). Terms authoring UI is a candidate below;
governance document authoring/binding and full operational acceptance remain open. This
candidate is not approved for a live migration or rollout.

The candidate hospital `employment-preparation-source` and
`employment-preparation-periods` family now acquires real current employment
preparation authority independently of Staff intake. WF0110 binds reads/saves to
the exact Staff/period/real command, preserves canonical draft revisions and
terminal withdrawal checks, and atomically retains private source provenance in
the immutable audit ledger. A legacy receipt cannot be relabelled as governance
provenance, and revocation is rechecked before receipt reads. WF0111 supplies the
concurrent revision receipt lookup index. Required trigger/function/event,
index/policy/forced-RLS support is checked before reading or saving. This grants
no document, login, posting, review-decision or operating authority. Local proof,
replay, build and compile evidence is not remote acceptance. Strict preparation SDK functions now preserve the original expected workspace,
validate canonical scope/receipts/pages and cancellation, and never fall back to
legacy authority. The target/preparation SDK has 41 passing local tests; the
terms UI is described below, and document authoring/binding remains unfinished. The Staff/sponsor locking-read
extension preserves prior write checks and the separate all-Staff-write denial;
the corrected source passed real owner/delegate create/edit/replay/history, sponsor, revocation, recorder-failure and inherited-manager denial cases in CI 43501241981 (73afba71). Full current-head coverage, UI and deployment remain open. Both migrations require specific
live application and rollback approval after acceptance; keep historical proof
when rolling back a binary.

The separate candidate read-only POST
`/workforce/hospitals/:hospital/employment-preparation-targets/search` accepts
`{"employee_code":""}` to browse, or one exact case-insensitive Staff code.
Only `limit` (maximum 20) and a sealed `cursor` may appear in its query. The
canonical minimal `PersonReviewTarget` projection is reused; employee codes and
names never enter URLs. Original tenant/account headers and current native owner
or exact hospital employment-preparer authority are required. No person-preparer,
legacy directory or linked Staff authority is inferred or retried. Cursors bind
normalized body, account, route, actual hospital and verified tenant; mismatches
fail before scanning. Five-second source/idle expiry, dependency failure and
missing private database support disclose no partial result.

Candidate WF0112 omits Staff/period/command fields for this closed target-read
operation only and permits exact per-row version-only audit. All prior selected
preparation operations retain actual Staff/period/command scope and their write
fences. No HR, employment draft, document or operational write is authorized by
target selection. Existing Staff keyset/casefold code indexes are reused. Local proof, cursor and SDK cases pass. Core real owner/exact preparer,
revocation, minimal disclosure and inherited-manager denial cases passed in the
governance suite on CI 43506648429 (a258bc16). Complete target-support failure,
plan/load, browser and migration/rollback acceptance remain open. This does not complete the full Staff/pharmacy workflow or authorize its
merge/deployment.

The candidate employment-review screen now offers **Prepare new employment** only
in the governance access path and when discovery returns `can_submit`. It uses
the strict preparation SDK for private Staff search, selected source and draft
list/save; it does not request the Staff directory, person-review targets,
legacy intake flags or legacy sponsor route. The existing canonical terms form
is shared by the two explicit authority consumers. Sponsor options use the same
bounded minimal target reader and only active same-hospital candidates are shown;
the command owner revalidates the actual sponsor. A stale selected Staff version
requires a new selection. Opening an existing draft repeats its exact read; a
concurrent withdrawal prevents editing and offers a close/refresh path. An uncertain write preserves the exact command and
period ID for replay; source/grant denial cuts off the workspace. Changing the
account, hospital or access path remounts selection and form state. Draft reads
are never shown while an updated source/read is pending or failed. Dates retain
the existing explicit device-timezone display and convert to canonical instants.

This screen creates and edits draft terms only. Governance document metadata,
content/release/binding consumers and their UI remain required before the full
review/activation/operational access workflow can be accepted or merged. Local
selection/stale-source/no-fallback/replay/UI gating cases supplement the retained
Staff terms-form tests; current-head remote browser and PostgreSQL evidence is
still required.

Candidate WF0113 adds a separate native employment document consumer under
`/workforce/hospitals/:hospital/staff/:id/employment-preparation-documents`:

- `GET /`: a sealed, at-most-20-row page of current-basis employment evidence.
- `PUT /:file`: canonical document intent; only this Staff's enrollment record
  and `employment_evidence` category are admitted. Identical metadata replays the
  original file; differing metadata conflicts without relabelling the creator.
- `GET /:file`: exact metadata/status for bounded scan-status polling.
- `PUT /:file/content` and `GET /:file/content`: canonical bounded PNG/JPEG upload
  through quarantine/scanning, and reauthorized released bytes.
- `PUT /:file/revocation` and `PUT /:file/retries/:attempt`: canonical terminal
  revocation and eligible next-attempt recovery, preserving existing clocks and
  immutable generation/verdict guards.

Every route requires single original tenant/account headers and canonical positive
`staff_version`/`employment_epoch` query values (at most 9007199254740990), checked
against current locked Staff. Lists alone also accept a single `limit`/`cursor`;
legacy intake flags, category switches and other parameters are refused. Cursors
bind hospital, actual Staff, account, tenant and both basis values. Source denial
is 403, stale workspace/basis 409, hidden Staff/file 404 and unavailable source or
installed support 503; delegates retain source-before-topology privacy.

Each owner phase acquires a current native owner/exact employment-preparer source.
Its identity is retained across body/storage phases, while leases are refreshed;
replacement by another grant cannot inherit a pending disclosure. No locks span
storage I/O. File requests also have a 30-second overall context budget, within
the Global API's 35-second write deadline; shorter parent deadlines win. A closed private purpose binds actual file, Staff, basis, operation,
phase and validated canonical mutation. Read proof cannot stamp, queue, revoke,
retry, write a verdict or alter masters. Queue proof uses the actual verified
quarantine generation. Existing worker/verdict authority remains separate.

WF0113 extends existing permissive policies and adds restrictive purpose/delete
fences to all Workforce tables. Actual document/contributor/audit guards,
provenance trigger, policies, forced RLS and ready/valid document-page index are
required. Private creation/mutation provenance is atomic; read/phase audit is
minimized. Source identifiers and intent hashes are private, with no copied
filenames, document bytes or HR payload. General record choices, identity files,
credentials, qualifications and classification administration remain outside this
consumer. The earlier Staff, pharmacy and restricted worker document consumers
retain their authority and canonical engine.

Pure proof/phase regressions were observed red then green. Real owner/grant,
metadata replay/conflict, scan/release, changed-source disclosure, scope and
missing-support cases are included in the remote governance fixture but are not
yet accepted. Native document SDK/UI and exact employment-term binding remain
unfinished. WF0113 is unapplied and requires specific live migration/rollback
approval after concrete acceptance; no rollout is authorized by a source review.
