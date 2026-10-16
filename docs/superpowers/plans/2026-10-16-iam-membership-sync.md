# IAM membership synchronization implementation

Issue #1173; design: ../specs/2026-10-16-iam-membership-sync.md.

1. Reproduce first-role grant membership denial through the public IAM API,
   real Postgres outbox, NATS consumer and OpenFGA; record red output.
2. Add the tenant-role write before subject assignment; propagate errors.
3. Verify duplicates, failure/redelivery at either write, revocation without
   disrupting another role holder, and foreign-tenant denial.
4. Run full backend race/coverage, vet, build, lint and required web gates;
   secret-scan and publish a scoped PR assigned Sam123ben.
5. Obtain specific rollout approval for the tested PR, deploy through GitOps,
   then repeat reviewer login and licence decisions on synthetic evidence.
