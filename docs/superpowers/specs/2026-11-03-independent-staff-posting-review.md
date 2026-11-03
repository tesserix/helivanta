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

The candidate forward schema is Workforce `0075_workforce_posting_proposals` plus
the existing candidate IAM `0025_iam_staff_posting_review_templates`. Verify the
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
policy apply. A hospital proposal inbox and the normal reviewer UI remain
separate mandatory follow-ups on this same feature branch.
