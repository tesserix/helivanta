# Independent departmental Staff posting review

Status: implementation candidate; no operating authority or production rollout.
Continues the [effective posting specification](2026-11-02-effective-staff-postings.md)
and its complete parent obligations. Depends on scoped preparation PR #1532.
This specification pins the next command boundary; adding it does not implement it.

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

| Method/path                                           | Behavior                                                                                                                  |
| ----------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| GET `/posting-review-source`                          | Minimal current draft/generation concurrency basis and explicit blocking states; no identity/evidence/private HR content. |
| PUT `/posting-proposals/:proposal/commands/:command`  | Submit the exact draft revision and current reviewed employment generation.                                               |
| GET same command path                                 | Recover the immutable submission receipt under current authority.                                                         |
| GET `/posting-proposals`                              | Bounded proposal heads for this canonical Staff target.                                                                   |
| GET `/posting-proposals/:proposal`                    | Current decision and derived eligibility; public fields exclude protected reasons and contributor identities.             |
| GET `/posting-proposals/:proposal/history`            | Bounded immutable revisions.                                                                                              |
| PUT `/posting-proposals/:proposal/decisions/:command` | Independent approve/reject or proposer withdrawal with exact expected revision/version.                                   |
| GET same decision path                                | Recover its immutable receipt under current authority.                                                                    |

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

Submission and approval both validate the Workforce-owned current generation,
identity epoch, approved person/employment findings, appointment interval and
required evidence/classification versions. Reuse owner-private checks; no copied
eligibility implementation in IAM, Organization or Pharma. Pin the generation and
employment epoch. Changed/restored identity cannot revive the consumed source.
Non-digital target Staff may have placements; a reviewed login is required for
actors, not fabricated for non-digital workers.

An effective posting cannot start before its reviewed appointment or extend past
its end/eligibility cutoff. Do not silently shorten the proposed interval. An
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
