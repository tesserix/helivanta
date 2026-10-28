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
