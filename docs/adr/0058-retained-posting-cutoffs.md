# ADR-0058: Retained Workforce posting cutoffs

Date: 2026-11-05. Status: proposed implementation contract; not implemented,
approved for production, or a claim of operating-consumer enforcement.

## Context

Workforce owns canonical Staff and postings. Organization owns structural
references; IAM owns assignments and their OpenFGA projection. Source checks in
`posting_chain_approval.go` reject changed employment/identity and ended nominal
intervals, but an approved open-ended posting has no owner command to end it or
shorten its validity. A department cannot safely rely on such a reference until
both the ending command and its live consumers exist. Relating issues: #417,
#429; accepted ORG-D4 and ORG-AU-05/07, ORG-ST-02/03/05 apply.

## Decision

Retain immutable, monotonically shortening cutoff commands separately from
proposal approval revisions. Preserve existing approval receipts, their status
vocabulary and their original interval/source pins. A cutoff is a new owner fact;
it cannot erase approval history, restore an old reviewed epoch, or grant access.

Each command names the exact approved revision and expected cutoff version.
Version zero means no retained cutoff. The first command creates version one;
subsequent commands must shorten the retained cutoff. Immediate mode uses a
server-owned instant; scheduled mode requires an explicit future instant. Replaying
the exact committed command recovers its original instant before checking current
time or expected head. Changing its actor, scope or normalized input conflicts.
No command may withdraw, extend or delete a cutoff. A new independently reviewed
posting is required to resume a responsibility.

Use a distinct hospital-scoped administrative permission
`workforce.posting.cutoff` and a new closed IAM template/catalog revision. Existing
reviewer and management template snapshots stay unchanged. The actor must have
current independently approved IAM authority and reviewed Staff actor use. The
target may already be suspended, ended or identity-stale: revocation must not
require that target to become eligible again. A cutoff does not require a second
posting approval or contributor-independence check; independently approved cutoff
authority is its administrative basis. It cannot appoint a replacement.

Serialize cutoff writes, approvals and live source checks with the existing
hospital supervision gate and owner transaction. Persist the cutoff, receipt,
attributable audit and identifier-only outbox notification atomically. A future
cutoff becomes effective by comparing the retained instant on every guarded use;
no timer, delivery acknowledgement or background job decides eligibility.

The live deadline is the minimum of nominal end, reviewed source deadline,
retained local cutoff and every ancestor's live deadline. Hold owner sources and
Organization ancestry through the consumer transaction and constrain its commit
deadline. Missing state or unavailable authority denies. Events can invalidate a
display/projection, but cannot authorize a command or replace this owner check.

Do not silently redefine existing strict context envelopes or the meaning of
stored `eligibility_until`. Introduce an explicit lifecycle context selector and
new canonical response containing per-node cutoff version/instant plus the
computed live deadline. New approval input binds the observed cutoff versions.
Legacy positive commands/context selectors must refuse a chain containing a
retained cutoff they cannot represent; exact historical receipt recovery and
independent rejection remain available. Final wire names and consumer migration
tests belong to the implementation before this ADR becomes accepted.

## Limits, failures and recovery

Reuse the unmeasured planning assumptions: 50 peak requests/s, 80:20 reads/writes,
500,000 Staff at 12 months, 1.5 million at 36 months, p99 below 500 ms and 99.9%
availability. Bound traversal at 32 nodes including target, command input at
4 KiB, lifecycle context at 128 KiB, and history pages at eight rows with opaque
keyset continuation. These are design budgets, not measured SLOs. Use the existing
modular monolith/Postgres/outbox; no additional service, cache or worker cost.

Staff identity, private reasons and authority history are assets. Foreign tenants,
sibling hospitals, stale sessions and insiders must not infer private records or
end a posting by supplying IDs. Verify session tenant, exact hospital, permission,
reviewed actor and object scope on every read, write and receipt recovery. Missing
capability is 403, invisible object 404, malformed input 400, stale/conflicting
intent 409, unavailable IAM/Staff/Organization authority 503. A failed audit or
outbox write rolls back the entire command. Reasons stay in owner-private data;
notifications contain canonical identifiers/version/instant only.

## Alternatives and migration

Changing `approved` into a new terminal status would break historical receipt
contracts. Editing `ends_at` loses the originally reviewed appointment. Deleting
postings loses accountability. Copying employee/posting authority into Pharma
creates a second master. A scheduled job alone leaves a delivery gap at expiry.
Separate retained cutoffs plus guarded owner use preserve these boundaries.

Candidate next migration numbers are Workforce 0084 and IAM 0027; recheck the
registry before allocation. Forward-only additive tables require tenant/hospital/
Staff/proposal/revision composite foreign keys, UUIDv7 command IDs, immutable
rows, forced purpose-bound RLS, bounded statement/lock timeouts and indexed head
lookup. Old permissions/catalogs are immutable. Do not apply these migrations or
promote a revision without approval naming the final release and migrations.

After cutoffs are written, rolling application code back to a consumer that
ignores them would restore authority incorrectly. Recovery keeps cutoff-aware
read/command enforcement deployed and disables new cutoff authoring if needed;
never remove retained cutoff rows to enable a rollback. A production rollback
needs a separately approved plan. See the [specification](../superpowers/specs/2026-11-05-posting-lifecycle-cutoffs.md)
and [implementation plan](../superpowers/plans/2026-11-05-posting-lifecycle-cutoffs.md).
