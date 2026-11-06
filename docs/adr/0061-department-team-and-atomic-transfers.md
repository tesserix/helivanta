# ADR-0061: current department teams and paired posting transfers

- Status: proposed; implemented as a local candidate, pending review and release
- Date: 2026-11-06
- Owners: Workforce, Organization and IAM
- Extends: ADR-0052, ADR-0053, ADR-0055, ADR-0058 and ADR-0060

## Context

Approved posting history does not establish current team membership. Two separate
browser commands cannot atomically end an original responsibility and approve its
replacement. Workforce already owns both retained records, their audit and outbox
writes, and the hospital supervision gate. Organization owns current department
ancestry; IAM owns each scoped permission and the reviewed actor source.

Planning assumptions, not measured capacity: ten peak roster requests/second,
a read:write ratio of 20:1, and eight placements per page. A 1,000-person hospital
with six postings/person/year retains about 6,000 posting heads at 12 months and
18,000 at 36 months before growth. A page checks at most eight chains of at most
32 nodes, plus one keyset probe. Target p99 is two seconds within existing guarded
command deadlines; load acceptance remains necessary before claiming this SLO.
No new service, queue, cache or data master is justified by these assumptions.

## Decision

Expose a bounded, account-bound `GET /workforce/hospitals/{hospital}/department-team`
projection. Page approved placements using the existing cursor machinery. Return
an explicit `current` or `not_current` observation for every retained placement;
never infer live membership from approval or silently filter a candidate page to
an apparently empty team. Private rationale, HR documents and salaries are absent.

Require genuine proposal-read, Staff-actor-use and hospital source-use reservations.
For department readers, retain the current subtree lock and candidate filtering.
Only the separately held hospital source-use grant permits internal ancestor checks
outside that subtree. Save and restore the department frame before releasing the
identifier-only observation. This grants no ancestor directory or operating data.
Currentness includes employment, actor identity, exact source proof, current
Organization revisions and every ancestor cutoff. Unavailable owners fail503;
known invalid source chains produce `not_current`. Sensitive reads are audited.

Expose a separate `PUT .../posting-proposals/{replacement}/transfers/{command}`.
The first supported scope is one Staff member within one hospital, with an explicit
future replacement start. Do not reinterpret past draft starts as an immediate
transfer or silently rewrite approved source intervals. Cross-establishment moves
need their own employment process. A department-only reviewer cannot coordinate
across hospital departments.

Compose independently reserved review, actor-use, cutoff, source-use and lifecycle
support permissions. The existing bounded compound guard grows from three to five
reservations; this changes its capacity, not any role's permissions. Supervised
approval uses posting-read support; a management root uses management-review support.
All five reservations, actor evidence and sources remain held through commit.

Under the existing hospital supervision lock, independently validate the pending
replacement and current original posting. The original source must cover the
transfer instant, and constrain commit to precede that instant. Reject replacement chains that depend on the ended original.
Insert a scheduled cutoff at the replacement start and record approval, retained
lifecycle evidence, audit and both existing identifier-only outbox intents in the
same owner transaction. No IAM or clinical access is created.

The original cutoff and replacement approval share one immutable command UUID,
actor and normalized transfer fingerprint. This pair is the transfer receipt;
no second mutable transfer master is introduced. The normal decision fingerprint
and response remain byte-compatible. Exact transfer PUT replay reauthorizes and
recovers both receipts before checking whether the original remains usable.
Changed intent conflicts. A previous matching cutoff/approval pair prevents a
second replacement from consuming the same original, including after a reviewer
refreshes its cutoff version. Independent ordinary cutoff commands may still
shorten the original responsibility.

## Correction, failure and recovery

A scheduled transfer is retained history and is not undone by extending a cutoff.
To cancel the destination responsibility, use its ordinary immediate cutoff,
including before its nominal start. The original scheduled ending remains in
force. Continuing or returning to the original department requires an independently
reviewed new placement covering the required interval. The UI states this before
approval; it does not offer a misleading restore/cancel operation. Existing other
concurrent responsibilities remain untouched.

Dependency unavailability fails closed. Stale proposal, cutoff, source or interval
conflicts leave neither half committed. Audit or outbox failure rolls back both
halves and their source-use evidence. Lost responses are recovered by replaying the
original immutable PUT, never by generating another command ID. Concurrent writes
serialize at the owner supervision gate; competing expected versions cannot both
consume the original. Read observations expire and are never reusable authority.

No database migration is required: existing retained tables and uniqueness guards
hold both halves. Rollback removes the new consumers/routes while preserving
retained evidence and existing individual receipt/history readers. Do not erase
completed transfers. This ADR does not authorize a production rollout.

## Alternatives

Two UI writes were rejected because a failed second write leaves half a transfer.
A new permission family and transfer table were rejected because independently
checked existing permissions and immutable linked records meet this owner-local
transaction's needs. Copying current membership into IAM or a second department
tree was rejected because it would drift from live owner checks.

## Acceptance

See [the implementation contract](../api/workforce-postings.md) and
[department-team delivery evidence](../design/staff-and-rbac/department-team-management.md).
Real owner integration must cover scope isolation, currentness, exact and changed
retries, competing transfers and audit/outbox rollback. Browser acceptance must use
real Global API/owner responses and demonstrate lost-response recovery. Capacity,
production rollout and future statutory HOD policy are separate review items.
