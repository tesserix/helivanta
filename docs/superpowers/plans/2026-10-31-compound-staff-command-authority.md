# Compound Staff command authority implementation

The full Access/RBAC A01–A12 objective remains open. Follow the matching
[design](../specs/2026-10-31-compound-staff-command-authority.md), ADR-0049 and
[owner/consumer inventory](../../qa/staff-intake-access-inventory.md).

1. Reproduce the missing shared guard through the real IAM provider and existing
   signed-session PostgreSQL/NATS/OpenFGA projection journey.
2. Add bounded request/evidence pairs and an additive compound interface. Keep
   the existing single-grant interface on the same implementation. Reject mixed
   subjects/scopes/generations, duplicate requirements and conflicting receipts.
3. Lock all active proposal rows in ID order, then current membership and scope;
   share the credential watermark lock. Revalidate each exact permission and
   snapshot after waits and before owner commit. Cancel at the earliest expiry.
4. Exercise two independently approved assignments, missing permissions, invalid
   evidence, unavailable dependencies, revoked credentials, withdrawal and owner
   rollback. Preserve single-grant journeys. Extend real lock-wait/expiry and
   projection concurrency acceptance before any consumer is activated.
5. Run IAM/authz/tenantdb and architecture race suites, vet/build/lint and
   contract freshness. Record actual results and keep browser/deployed evidence
   separate.
6. Complete HR intake template evolution, capability discovery and owner/UI
   transactions; then current-person basis, independent employment submission
   and decisions, effective eligibility, legacy-cohort classification and every
   module cutoff. Do not activate a template or describe this internal guard as
   complete provisioning, employment approval or lifecycle acceptance.

No production migration, role assignment, provider configuration or rollout is
performed by this internal prerequisite. IAM owns assignments and generations;
Workforce must retain current hospital, Staff, person and file/release checks.
