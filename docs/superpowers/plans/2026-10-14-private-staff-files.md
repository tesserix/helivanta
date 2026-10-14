# Private staff file implementation plan

Design: [private-staff-files-design](../specs/2026-10-14-private-staff-files-design.md).
Issues: #1105 / #483; parent #1035 / #979. Keep incomplete issues open.

1. Verify opaque naming and bounded ClamAV transport; add freshness/provenance
   and run actual isolated scanner acceptance fixtures.
2. Add pinned-generation, create-only GCS operations and bounded content
   validation; test vendor protocol failures and separately verify real GCS.
3. Add workforce-owned additive metadata/RLS/audit and idempotent API
   transitions with current locked hospital-manager and record authorization.
4. Connect atomic outbox to an after-commit JetStream/Temporal start and
   deterministic workflow activities with retry-safe promotion/release.
5. Generate shared contracts and implement accessible staff file UI using
   existing forms, dropdowns, query/mutation and hospital-switch safeguards.
6. Add workload-identity buckets and scanner/worker configuration through
   owning infrastructure GitOps; local CNPG/browser tests precede rollout.
7. Complete race/vet/build/lint, coverage, preflight, web/e2e and CI gates;
   resolve origin identity before commits, merge authorized scoped PRs, verify
   exact deployed source and synthetic live API/UI/logs, then close acceptance.
