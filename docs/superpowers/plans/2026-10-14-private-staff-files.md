# Private staff file implementation plan

Design: [private-staff-files-design](../specs/2026-10-14-private-staff-files-design.md).
Issues: #1105 / #483; parent #1035 / #979. Keep incomplete issues open.

Processing continuation: reproduce API verdict forgery despite a caller-set
worker flag; add an append-only migration bound to the distinct CNPG worker login
and all four ownership UUIDs. Mirror the identity, without table grants, in local
and test bootstrap. Prove API denial, exact-file worker access and denial of
personnel tables before connecting Temporal and release/delivery.

Continuation implemented and locally verified: scoped release/rejection/error
commits, crash-after-copy recovery, Temporal bounded retries/heartbeats and
retained-execution verification, private download reauthorization and explicit
idempotent retry/revocation APIs. Runtime/API/image wiring is present. Worker
role pre-registration and runtime IaC have been applied through infrastructure
PRs #1414 and #1415. The role remains NOLOGIN and the credential/upload/runtime
activation switches remain disabled. Generated contracts and the staff-document
UI are implemented; actual browser/cloud acceptance and private credentials,
activation and the live worker migration remain uncompleted.
Do not deploy `0008` against CNPG before its role exists, or call the complete
story accepted before real GCS and local/live browser proof.

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
