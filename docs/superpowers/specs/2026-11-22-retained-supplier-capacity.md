# Retained supplier source capacity

Status: proposed implementation; #490 remains open. No availability change.

## Current defect and boundary

Pharmacy's destination release checks at most 1,000 receipt sources. Identical
shipment captures are deduplicated, but each distinct source check opens another
transaction while retaining its predecessor. The runtime app pool is limited to
five connections. A valid batch can therefore wait on its own held connections.
Raising the pool or dropping earlier source guards would conceal the defect or
allow safety facts to change during the protected continuation.

Supply owns warehouse safety and PharmaConnect owns outlet safety. Drugmaster
owns published pack/formulation facts. Existing persisted shipment derivation,
verified buyer tenant/outlet, Staff admission, maker/checker, release quantities,
and source safety checks remain mandatory. No consumer reads another owner's
tables. India IN-PH partial receipt, unsafe stock and cross-outlet criteria apply;
no Maharashtra/Odisha legal policy, drug schedule or financial rule changes.

## Decision

Add process-local retained transaction frames in tenantdb. Source owner guards
and the published catalogue guard reuse one non-BYPASSRLS app transaction in
their synchronous callback chain. Each nested frame sets and restores its exact
tenant/actor, with an empty tenant for global catalogue reads. It carries no
authority: each source owner still rederives its immutable dispatch and validates
current product/lot safety. Ordinary destination and authorization transactions
do not implicitly join this source transaction.

Frames bind the DB instance and callback lifetime. Escaped, concurrent or
wrong-database contexts fail closed. The active child receives a distinct context;
the parent cannot start a second child while the first is active. Cancellation
aborts the retained transaction. No HTTP or asynchronous work is admitted inside
these source frames. Existing source locks remain held through the innermost
destination continuation. All owner-specific GUC restoration remains in place.

The destination recursion must propagate the returned source context instead of
discarding it. Source connections become O(1), independent of the bounded 1,000
receipt scan. Work remains O(distinct shipments); this does not claim the existing
800ms command planning target at maximum volume. Existing gateway/request
deadlines apply; timeout rolls back destination work, which is recovered by its
existing immutable command ID. No increased pod, pool or infrastructure cost.

## Acceptance and delivery

First reproduce eight distinct physical dispatches exhausting the current
five-connection pool. Then prove the same real owner journey succeeds and invokes
the destination once. Real PostgreSQL frame tests must verify one backend PID
across tenant/global frames, forced-RLS isolation, restored tenant/actor, retained
locks, rollback, cancellation, wrong DB, escaped contexts and concurrent reuse.
Run existing unsafe-lot denial and both-owner compound receiving/release journeys.
Keep the current duplicate/conflicting receipt tests and 1,000-row admission cap.

Hosted focused race tests precede normal protected checks, merge, signed images,
reviewed release and Kargo deployment. No schema migration or wire contract change
is intended. Rollback uses the prior application image and preserves all audit and
physical history; the prior connection limitation returns. Supplier activation,
source commercial documents, returns/credits and positive live supplier acceptance
remain separate incomplete gates.
