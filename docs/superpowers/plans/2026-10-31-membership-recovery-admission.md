# Membership recovery admission implementation plan

Scope: [reviewed local design](../specs/2026-10-31-membership-recovery-admission.md),
A02/A11 only; the full A01–A12 acceptance checklist remains open.

1. Reproduce legacy HTTP restoration after exact final-role removal against real
   PostgreSQL/NATS. Require 409 and unchanged membership/epoch/audit/outbox, then
   prove explicit ordinary recovery still succeeds.
2. Add persisted forward-only 0019 on the existing continuity-row transition;
   map only its named violation to a bounded no-store 409. Preserve existing
   command guards, current-row duplicate repair and tenant isolation.
3. Verify direct runtime-role inserts, partial/forged command contexts, actual
   removal lock contention and same-subject enrollment in another tenant.
4. Run full IAM race tests, affected architecture/journey checks, vet/build/lint,
   contract generation/check and diff/secret checks. Correct legitimate positive
   fixtures through supported recovery without weakening their assertions.
5. Update API/migration/QA records, commit and push to the existing draft PR.
   CI, external-contract review, real Admin/recipient/reviewer UI and exact
   production migration/rollout approval remain required before acceptance.

The regression was observed returning 202 before implementation, then 409 with
explicit recovery still accepted after 0019. Keep the guard on forward repair;
a schema down migration would reopen the demonstrated bypass and is intentionally
not provided. All local migration validation uses disposable databases.
