# Current identity guard implementation

Design: [ADR-0047](../../adr/0047-current-identity-on-protected-requests.md).
Existing specification: [Staff lifecycle research](../specs/2026-10-27-staff-lifecycle-rnd.md).

1. Reproduce ordinary authenticated traffic reaching a handler after IdP disablement.
2. Add the IAM-owned guard using the existing client, watermark and transactional
   invalidation writer. Require it in the production V1 middleware constructor.
3. Verify real PostgreSQL/outbox behavior, signed cookies, cross-replica missed
   invalidation, reactivation, provider and persistence failures, observation races,
   absent authentication and rate-limit placement.
4. Exercise local Staff UI with synthetic identities, update the acceptance ledger
   and run package race tests, vet/build/lint and contract checks. Complete CI,
   merge and prepare the exact GitOps release before requesting rollout approval.
