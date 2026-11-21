# Supplier return history delivery

1. Reproduce missing SDK history, server route/query guard and workspace behaviors with tests.
2. Add canonical compact summary, generated models, owner collection and strict pagination/audit handling; cover native isolation, cursor continuation and audit failure.
3. Wire account-bound transport, actor-scoped cache keys, human-readable history/details and immutable cancellation with reference-only recovery into accounts. Preserve parent release gating.
4. Update domain/ADR/contracts and generated route/Staff inventory. Run affected tests, local production builds, lint/typechecks and hosted native/E2E gates. Remove generated local build output.
5. Commit/push and merge only after required checks pass. Migrations 0054/0055 and production rollout still need the specific approval documented in the parent PR. Record live verification before closing #1687.
