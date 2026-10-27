# Staff supervision cycle implementation plan

Follow the [design](../specs/2026-10-28-staff-supervision-cycles.md).

1. Add an HTTP regression reproducing the observed two-person cycle against real
   PostgreSQL; record the failing result before implementation.
2. Add the owner lock and database acyclic guard migration; use the shared lock
   before staff locking and map the named failure without exposing the chain.
3. Prove indirect/concurrent/direct-SQL cases, rollback, repair and scope denial.
4. Measure long-chain queries, rerun owner/integration/architecture checks and
   regenerate registered contract evidence. Verify the existing UI error flow.
5. Rebuild the local API and repeat the synthetic browser journey, preserving
   fixture history. Commit/push a separately scoped PR; merge on complete checks.
6. Include the migration and owner data audit in the named GitOps release packet;
   retain the invariant on rollback and verify deployed behavior after approval.
