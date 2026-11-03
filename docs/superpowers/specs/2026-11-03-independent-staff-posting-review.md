# Independent departmental Staff posting review

Status: implementation candidate; no operating authority or production rollout.
Continues the [effective posting specification](2026-11-02-effective-staff-postings.md)
and its complete parent obligations. Depends on scoped preparation PR #1532.
This specification pins the next command boundary; adding it does not implement it.

## Delivery sequence

The user requested smaller committed, merged and deployed PRs on 2026-11-04.
Deliver the independently assigned IAM catalogs as a bounded prerequisite first,
then the proposal owner API/SDK, then reviewer UI and finally effective posting
and downstream department/Pharma consumers. Catalog admission is not current
operating eligibility. Each slice retains its applicable security, persistence,
contract and test gates; full feature acceptance still requires this complete
specification and the parent lifecycle requirements. No positive placement or
Pharma authority is introduced by the catalog prerequisite.

## Owners, scope and capacity

Workforce owns one canonical Staff record, reviewed employment generation and
placement history. Organization owns the hospital department and its immutable
structural revision. IAM owns independently approved scoped capabilities and their
OpenFGA projection. Pharma will reference the resulting posting identity for its
professional acceptance and duties. A proposal, approval or posting never creates
an IAM assignment, another Staff record, or a product employee master.

Retain the parent planning assumptions: 50 peak requests/s, 80:20 reads/writes,
2–20 KiB bounded responses, p99 under 500 ms, 99.9% availability and
500,000/1.5 million Staff at 12/36 months. Lists use keyset cursors with a bounded
page/serialized byte budget. These are unmeasured budgets, not performance claims.
Use the existing shared database and root guarded command; no new service, queue,
cache, cloud resource or paid dependency is required. Incremental infrastructure
cost is existing database storage for bounded immutable command/audit history;
measure actual storage growth before claiming a monetary estimate.

## Actors and separate authority

A submitter needs a current reviewed digital Staff/login source and a current
hospital-scoped `workforce.posting.submit` assignment. A reviewer needs its own
current reviewed source and `workforce.posting.review` assignment at that hospital.
Tenant administrator, hospital management, ordinary membership, an active Staff
status or a department/category label alone satisfies neither requirement.
IAM assignment proposer, independent approver and recipient remain separate.

Append `workforce.posting_submitter.v1` and `workforce.posting_reviewer.v1`
in the new closed `staff-posting-reviews-v1` catalog to
`contracts/staff-access.json`; do not expand the deployed reader/preparer v1
snapshots. The submitter bundle explicitly includes draft read/preparation and
canonical choices; the reviewer bundle includes bounded proposal reading and
current structural verification. Both include `workforce.staff_actor.use`.
Do not confer clinical, dispensing, department-governance or general HR access.

Reviewer exclusion checks the submitter, every retained draft contributor, target
Staff, declared subject and current/historical Staff login associations. Updating
or revoking a login link cannot erase a conflict. The submitter may withdraw a
pending proposal; approval/rejection requires an independently eligible reviewer.

## Command and receipt contract

Extend the canonical `contracts/workforce-postings.json` and its existing Go/Zod
generator. All browser business traffic enters the Global API. The base below is
`/v1/workforce/hospitals/:hospital/posting-staff/:id`:

| Method/path                                           | Behavior                                                                                                                                                     |
| ----------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| GET `/posting-review-source`                          | Minimal current Staff/generation concurrency basis and explicit blocking states; selected draft is read separately; no identity/evidence/private HR content. |
| PUT `/posting-proposals/:proposal/commands/:command`  | Submit the exact draft revision and current reviewed employment generation.                                                                                  |
| GET same command path                                 | Recover the immutable submission receipt under current authority.                                                                                            |
| GET `/posting-proposals`                              | Bounded proposal heads for this canonical Staff target.                                                                                                      |
| GET `/posting-proposals/:proposal`                    | Current decision and derived eligibility; public fields exclude protected reasons and contributor identities.                                                |
| GET `/posting-proposals/:proposal/history`            | Bounded immutable revisions.                                                                                                                                 |
| PUT `/posting-proposals/:proposal/decisions/:command` | Independent approve/reject or proposer withdrawal with exact expected revision/version.                                                                      |
| GET same decision path                                | Recover its immutable receipt under current authority.                                                                                                       |

Hospital/Staff retain their existing nonzero UUID formats. Proposal, command,
revision, generation and structural IDs require their canonical UUID formats;
new identities are UUIDv7. Required single expected-tenant/subject headers bind the
original workspace and return 409 `session_changed` on mismatch before disclosure.
They are preconditions, never selectors or authority. Every response is no-store.

Submission supplies the exact draft ID/revision/version, target Staff
version/employment epoch, generation ID, a required protected reason, and explicit
confirmation that the sources were examined. It does not accept browser-supplied
employment eligibility, copied Staff attributes or a replacement department name.
The interval/department come from the selected immutable draft. Absent, null,
false, unknown fields and additional JSON values follow strict canonical rules.

The receipt pins the exact source/draft/structural IDs, versions, interval,
proposal status and recorded instants. `grants_access` is always false. The same
command key and normalized intent returns its original receipt after current
reauthorization; changed actor/intent/scope conflicts. A timeout is an unknown
outcome: recover that key rather than generate another one. Terminal decision
retries retain their original result; reads distinguish immutable decision history
from current operational eligibility.

## Source and supervision requirements

### Hospital proposal inbox

Candidate `GET /v1/workforce/hospitals/:hospital/posting-proposal-inbox` consumes
current hospital `workforce.posting.proposal.read` and Staff-actor-use authority
through one guarded owner transaction. Tenant administration and legacy hospital
grants do not substitute. Read the caller's own active link for admission, then
require the guarded actor to match that discovered Staff before querying rows;
a changed link must refuse, not fabricate an empty queue.

Return canonical nested public proposal and Staff summary DTOs plus an
`is_submitter` observation derived from the original submission and current
principal. This boolean grants no command authority. Independence stays in the
protected context/decision owner checks. Staff labels are current directory
labels; the proposal's versions and intervals remain its recorded source pins.
Exclude reasons, actor subjects, hashes and evidence. Default status is pending;
one explicit pending/approved/rejected/withdrawn filter is supported per request.

Use at most eight entries plus one probe, with newest-first `(created_at,id)`
keysets and an explicit 20-KiB wire-budget test. Retain the parent's 50 peak
requests/s, 80:20 reads/writes, p99 under 500 ms and 99.9% planning assumptions.
For sizing only, four retained proposals per Staff imply 2/6 million heads at
the inherited 12/36-month Staff volumes. These are not measured SLOs or policy.
Add the tenant/hospital/status/time/ID queue index through a separate concurrent
index migration. No new service, table, paid dependency or employee master is
required; incremental cost is existing database/index storage, to be measured.

Append owner 0076 for a read-only inbox purpose on proposal heads and only their
original/current revisions. Compose the single existing permissive predicate;
retain its original UPDATE/DELETE eligibility in restrictive policies and its
unchanged write check. Reuse the guarded
directory purpose for Staff labels. Keep prior SQL, write checks and destructive
guards unchanged. Bind the read purpose to exact tenant, hospital, current actor,
link/generation and positive scoped assignment/epoch pins in read mode. Audit the
hospital/status/page observation and current actor-use through commit; errors or
dependency failure withhold the whole page. No business write or outbox event is
performed. Missing providers are 503, missing authority 403, malformed queries or
cross-scope cursors 400. A healthy authorized empty queue remains explicit.

The new scoped-list wrapper binds opaque cursor positions to principal, actual
hospital, route and normalized status; existing list wrappers/cursors retain
their behavior. SDK validation binds every nested Staff/proposal to the requested
hospital and status, rejects duplicate/replayed pages and private extra fields,
and carries original-account headers/no-store/cancellation through Global API.
Normal UI must use the server context check when opening a row and reauthorize
every decision. Rollback retains additive policies/index/history; production
rollout/migration still needs its named revision and forward list approved.

### Protected proposal context

Candidate `GET /posting-proposals/:proposal/context` requires exactly one each
of `purpose=review|submission`, `expected_version` and `expected_revision_id`.
Unknown, duplicate or malformed query parameters return 400. The exact current
head must match both pins, otherwise 409; hidden targets/proposals return 404.
This is a protected current-head view, not a replacement for immutable receipts.

For `review`, require current hospital review and Staff-actor-use authority and
the existing independent decider predicate before loading any rationale. For
`submission`, require current submit and Staff-actor-use authority plus the
existing original-submitter predicate. Membership, tenant administration, a
proposal-read assignment or a different submitter cannot release this context.
The checks apply to pending and retained terminal heads. They never grant a
later decision; each command reauthorizes its exact intent.

Return the purpose, exact public proposal, its selected immutable public draft,
draft reason, original submission reason and nullable current terminal decision
reason. Each reason retains the existing 500-character bound. Never include actor
subjects, input hashes, identity dossiers, attachment bytes or copied HR data.
The retained reasons explain the examined intent; they do not certify present
employment, structural eligibility or positive approval. Current source and
Organization verification remain separate owner views.

Use the existing guarded decision-purpose transaction and supervision write gate
for this sensitive read. That gate is acquired before actor rows, preserving the
existing lock order and holding proposal/draft/contributor source writers through
commit without a read-to-write lock upgrade. No business head, revision or outbox
row changes; successful source-use and exact-version context-read audit commit
together. Audit, authorization, source or commit failure withholds every protected
byte. Responses are no-store and remain within the inherited 20-KiB response and
500-ms p99 planning budgets; these are unmeasured, not performance claims.
No new service, paid dependency, table or migration is needed. The canonical JSON
contract generates the Go/Zod DTOs; the SDK checks requested scope, purpose,
proposal pins and draft/source relationships before returning the context.

### Terminal decisions and incomplete supervision policy

The existing capacity/SLO and single-owner transaction design also apply to
terminal decisions; no additional service, store or infrastructure cost is
introduced. Pending proposals can be rejected by an independently assigned
reviewer or withdrawn by their original submitter. Both commands bind the exact
pending revision/version, retain their original key/intent/actor, append a new
immutable revision and atomically save audit/outbox evidence. Terminal proposals
cannot transition again. A new proposal may reuse a rejected/withdrawn draft;
the historical proposal and its receipts remain unchanged.

Rejection/withdrawal remove no completed authority, so stale target employment,
department or evidence does not block these cleanup commands. Their actors still
need current reviewed digital sources and current hospital-scoped IAM authority.
The reviewer must differ from the target, submitter and every retained draft
contributor, including historical login associations of the target and actor.
Workforce owns these checks; a narrow read-purpose extension of the existing
RLS USING expression permits only target/reviewer associations for this exact
proposal decision purpose. Existing write checks and delete guards are retained. No private
association or contributor data appears in receipts.

The pending-only schema is expanded only for rejected/withdrawn revisions.
Approval continues to return 409 `posting_supervision_required` after current
reviewer authorization, source scope and independence checks, without appending
a revision. This is an explicit incomplete policy state, not a positive approval
or an operating posting. Allowing approval without supervision was rejected
because it would violate the parent lifecycle requirements. Forcing valid target
sources before rejection/withdrawal was rejected because it would strand stale
pending proposals. Missing IAM/actor owners return 503; a revoked actor/assignment
returns 403/401; persistence/audit/outbox failure rolls back every decision row.

Assets are protected Staff associations, pending placement decisions and audit
history. Threat actors include a contributor reviewing their own work, a target
using another account, an unrelated tenant/hospital user and a revoked reviewer.
The trust boundary is each server-side current scoped command plus its exact
tenant/hospital/Staff/proposal RLS context. Actor source checks survive through
outer commit; historical target information is used only to prevent conflicts.

Submission and approval both validate the Workforce-owned current generation,
identity epoch, approved person/employment findings, appointment interval and
required evidence/classification versions. Reuse owner-private checks; no copied
eligibility implementation in IAM, Organization or Pharma. Pin the generation and
employment epoch. Changed/restored identity cannot revive the consumed source.
Non-digital target Staff may have placements; a reviewed login is required for
actors, not fabricated for non-digital workers.

An effective posting cannot start before its reviewed appointment or extend past
its appointment end. Operating eligibility stops at the current reviewed-source
cutoff even when an ongoing placement has no nominal end. Do not silently shorten
the proposed interval to represent source expiry. An
unset required owner timezone blocks calendar-date conversion. APIs retain exact
UTC instants `[start,end)` and microsecond precision.

Keep managerial supervision distinct from clinical and pharmacist supervision.
The parent requires posting-type rules, effective supervisor references, stable
multi-Staff locking, cycle refusal and supervisor source cutoff. These are
mandatory before an approval can be exposed as an effective placement. The
implementation must pin these fields/rules and test the bootstrap/root case
before enabling positive approvals; a missing supervisor policy must not default
to unrestricted approval. Do not infer department headship from job category.

An independently approved future posting is scheduled. Current eligibility is
derived from approval, intervals, current generation, structural status and required
supervisor state. Suspension/end/revocation preserves history and advances source
state. Resume/rehire requires new reviewed sources/approval and cannot resurrect
old IAM assignments or Pharma duties. Transfers retain the parent's single-command
atomic replacement semantics; separate end/create calls are insufficient.

## Transaction, source writers and failure

Lock order remains current IAM authentication/membership/assignments, Workforce
actor and target employment/posting/supervision sources, Organization structural
gate, then downstream product duty/business rows. Audit every source writer before
introducing a new multi-row lock order. Hold current owner checks through outer
commit, and constrain it to the earliest actor/assignment/target/supervisor cutoff.
Proof is callback/database/tenant/subject/purpose bound and cannot be cached.

Missing permission is 403; hidden foreign objects are 404; changed source,
conflicting intent/decision or ineligible state is a documented 409. Unavailable
IAM/OpenFGA/Organization/current source is 503, never legacy fallback or fabricated
success. Audit/outbox/persistence failure rolls back every row and withholds the
receipt. External evidence/FGA I/O does not retain product locks; after slow I/O,
recheck the exact authority/source before release or command admission.

Add forward-only owner tables for proposal/decision revisions and immutable
commands with composite tenant/hospital/Staff keys, restrictive RLS, safe integer
bounds, current-head integrity and actor/state SQL guards. Determine the actual
next migration IDs against current main before registration. Earlier SQL remains
byte-identical. Rollback retains additive schema, receipts and audits; it does not
erase decisions or operational history. A production migration/rollout needs the
specific revision and complete forward migration list approved separately.

## Proof required

Exercise real public commands and PostgreSQL/OpenFGA, including successful
submission/independent decision; required fields; subject/contributor self-review;
ordinary membership/admin-only refusal; foreign hospital/tenant; exact/conflicting
and concurrent retries; changed source/department/supervisor; contractor interval;
non-digital targets; cycles; pending/failed projection; audit/outbox rollback;
dependency failure and both source/command orders. Mutation-test critical predicates.

Deliver the canonical SDK and normal Staff UI in the same feature branch:
source/readiness, required submission fields, pending independent inbox/decision,
retained recovery, history, and inaccessible-state clearing. Complete normal
submitter, independent reviewer, target and unrelated-account sessions, then
read-only persistence proof. No backend-only acceptance or full Staff/Pharma tag.
Modern/legacy operating consumers and standalone employers remain explicit parent
requirements rather than being implied by departmental posting approval.

## Implementation checkpoint — 2026-11-04

The candidate branch now implements pending proposal submission, current-source
readiness, immutable receipt recovery and bounded proposal/history reads. These
paths use the canonical generated models and scoped-only SDK. Rejection and
withdrawal decisions now retain exact pending revision/version and command
recovery with atomic audit/outbox. Independent positive approval,
supervision/bootstrap, end/transfer/resume and UI journeys remain unfinished.

A proposal receipt explicitly includes its command ID. Source readiness returns
Staff version/employment epoch, nullable generation/start/end/cutoff, checked-at
and an explicit generation state. It does not expose names, identity hashes,
findings, protected reason, registration details or actor associations.

Placement end and captured source validity are distinct. A fixed appointment
cannot receive an unbounded/outlasting placement. An ongoing appointment can
receive an open-ended or longer-running placement, but the current reviewed-source
cutoff remains finite and source ending blocks dependent operations. A proposed
start beyond the reviewed source cutoff is refused. Every actual operating
consumer must intersect current source/posting/duty/grant validity; null placement
end does not extend those owners' authority. The SDK and owner tests distinguish
these cases rather than imposing a fabricated end on all permanent employment.

The candidate forward schema is Workforce `0075_workforce_posting_proposals`,
`0076_workforce_posting_proposal_browse` and concurrent-index registration
`0077_workforce_posting_proposal_inbox_index`. IAM
`0025_iam_staff_posting_review_templates` is the already deployed prerequisite. Verify the
complete merged/main migration set before any production approval. No installed
local persistent schema or production schema was changed by disposable tests.


## Review hospital discovery contract — candidate implementation

`GET /v1/workforce/posting-review-scopes` is a separate candidate bounded discovery
route for the reviewer workspace; it does not replace preparation discovery or
expand any immutable IAM catalog. It scans at most 20 current reviewed-link
candidates per page, reuses the caller/tenant/route-bound opaque cursor contract,
and preserves continuation on empty filtered pages. Its canonical projection is
`{hospital_id,name,can_submit,can_review}`. Hospital names come from the existing
Workforce-owned reference; no Staff profile or identity evidence is projected.

Admission requires verified tenant membership, exact original account headers,
current hospital-scoped proposal-read and Staff-actor-use assignments, and the
current reviewed digital actor at that hospital. Submit/review capabilities are
independently checked through the same current source/assignment command. They
are presentation observations; submission and decision commands reauthorize
through commit. A reader lacking submit/review authority may read permitted
proposal history but receives both capability flags false.

All current-source reads retain attributable audit evidence. Unavailable scope
or Staff-actor owners return 503 even when no candidate rows exist, rather than
claiming verified empty discovery. Explicit permission/source refusal filters the
candidate; unavailable authorization or audit failure withholds the page.
Ordinary membership and administrator roles do not substitute for scoped
capabilities. This route needs no new storage or broadened Staff/proposal RLS;
it consumes only existing caller-link discovery and exact-actor source purposes.
The existing capacity, timeout, no-store, keyset limits and additive rollback
policy apply. The hospital proposal inbox is specified above. Normal reviewer decision UI and
real-account browser acceptance remain mandatory follow-ups on this feature branch.

## Terminal decision UI and recovery

The normal reviewer page must expose rejection only after successful independent
protected-context admission; the original submitter may withdraw under current
submit capability. Positive approval remains unavailable pending supervision.
Before attesting to source examination, display both the immutable submitted
sources and a current owner employment-source observation, including changed or
expired state. A failed source read cannot be treated as a current source.

Use the canonical decision schema for mandatory rationale/attestation. Capture
one immutable command ID, exact selected head and parsed intent before sending.
An unconfirmed outcome locks editing and offers only that command's receipt
recovery or exact retry; a missing receipt is not proof that an in-flight command
cannot commit. Account/tenant changes clear private views. Navigation warns about
retained intent; ordinary focus/refetch must not silently discard it. Every write
continues to use original-account headers and owner reauthorization. UI capability
observations and source snapshots confer no operating or professional authority.
