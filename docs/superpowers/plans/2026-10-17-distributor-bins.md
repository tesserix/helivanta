# Warehouse/depot bins implementation

Story #1137; spec ../specs/2026-10-17-distributor-operations.md.

1. Observe a real PostgreSQL HTTP test fail for an owned warehouse bin create/retry.
2. Add generated shared input contract, additive forced-RLS tables, immutable history,
   scoped constraints, permissions and versioned read/write/list/history endpoints.
3. Prove cross-tenant/party/premise denial, finance/warehouse denial, read-only auditors,
   immediate revocation, stale versions, concurrent retries, outbox rollback and pagination.
4. Observe UI test failure, then add a reusable bin panel inside the existing owned
   organisation premises workflow. Cover focus, retry IDs, errors, read-only access/history.
5. Run full required local gates and actual local browser/API synthetic acceptance.
6. Commit with resolved remote identity, publish a PR referencing (not closing) #1137,
   merge and release after gates, and record precise production verification. Continue
   the remaining slices; never close #1137 for bin configuration alone.
