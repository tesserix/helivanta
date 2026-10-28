# Staff identity/background/photo review delivery

Design: [ADR-0048](../../adr/0048-staff-identity-review-authority.md).
Full objective remains the [Staff acceptance ledger](../../qa/staff-management-acceptance.md).

1. Extend canonical shared IAM templates, projection constraints, current authority
   and UI selection; preserve leave-only consumer compatibility. Reproduce the
   unsupported HR-template input before implementation and verify real projection,
   epoch/withdrawal/expiry and permission-isolation behavior.
2. Add owner employment generations and identity fingerprints. Test inactivation/
   reactivation, name/DOB changes, unrelated contact edits and concurrent writes.
3. Implement versioned cases, evidence binding and state transitions with real DB,
   scoped manager/reviewer authority, private source reads and transactional outbox.
4. Extend protected photo purpose and trusted processing dimensions. Provide scoped
   manager/reviewer screens and requested self-photo submission. Verify replay,
   self-review, foreign scope, stale source, revoked evidence and dependency errors.
5. Run the complete affected Go/TypeScript suites, race tests, builds, generators,
   contracts and lint. Exercise real local Admin, reviewer and staff UI through the
   Global API using synthetic fixtures. Record pending card/lifecycle/payroll work.
6. Commit, push, pass CI and merge; prepare exact GitOps rollout and named migration
   approval, then verify deployed behavior. Never tag #1399 fully tested before all
   card acceptance criteria are delivered and verified.

## Preparation checkpoint

The shared template catalogue, independent assignment family UI and guarded
identity-source view are implemented as a preparation slice. Both HR templates
can inspect the current source, with no legacy permission fallback. Evidence and
limits are recorded in [local QA](../../qa/staff-identity-source-preparation.md).
Case mutation/decision/evidence and card work above remain unfinished.

## Draft preparation checkpoint — 2026-10-29

Versioned draft create/edit/read/list/history is implemented as a bounded local
slice with canonical models, current manager/reviewer guards, required account
and tenant preconditions, forced-RLS snapshots and metadata-only outbox events.
Migration 0049 is local only. Evidence binding, submission/decisions, photo
processing/self-service and cards in steps 3–4 remain incomplete. Do not treat a
draft receipt as a verification or close #1399.
