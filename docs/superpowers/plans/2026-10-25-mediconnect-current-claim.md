# Current claim implementation

1. Observe the owner-bound public handler contract fail before implementation.
2. Implement exact-tuple forced-RLS read and workload-authenticated handler,
   preserving all existing subject writes and review delivery checks.
3. Exercise real Postgres and real workload verifier against a mocked external
   Kubernetes TokenReview API: current then withdrawn, all wrong tuple dimensions,
   ordinary/ambiguous credentials, input limits and audit rollback.
4. Add a canonical typed service client, explicit optional startup wiring and
   HTTP integration tests. No HTTP or TokenReview inside database transactions.
5. Update API/internal contract, generated registry, permissions/classifications
   as applicable, migration compatibility and delivery evidence. Run affected
   tests/race, build/vet/lint, drift checks and regression gates. Keep broad stories
   open until actual clinical grants, recipients and record owners enforce it.
