# ADR 0009: Hospital-scoped workforce foundation

Status: implementation slice; production release pending verification.

The target in ADR 0008 distinguishes tenants from hospitals. Existing clinical tables still use the earlier model and must not gain broader visibility. The first additive API slice runs inside the existing Go API and migration runner, using its verified session, current tenant membership, OpenFGA permissions, rate limits and PostgreSQL connection roles.

Planning assumptions for this slice (not measured demand): 100 tenants, 5 hospitals each, 1,000 staff per hospital gives 500,000 staff records at 12 months; 3x growth gives 1.5 million at 36 months. Peak 50 requests/second, 90% reads, request bodies capped at 8 KiB, list pages capped at 200. Target availability 99.9%, p99 below 500ms. These tables and indexes fit the existing PostgreSQL cluster; no additional always-on service or cache is required. Load testing must validate the assumptions before broad rollout.

## Decision

Add a workforce module owning hospitals, explicit hospital access grants, hospital-local staff directory entries and append-only audit records. Tenant IDs come only from verified sessions, ultimately mapped from the existing onboarding operator's tenant ID. This API does not create Zitadel organizations or substitute its own onboarding mechanism.

Hospital setup and access administration use `workforce.hospital.manage`, resolved by the existing OpenFGA permission chain. These operations do not automatically grant access to staff. Staff endpoints additionally require a current database grant for the authenticated subject and selected hospital: `reader` or `manager`. Revocation is a synchronous PostgreSQL write; no authorization cache is introduced. A tenant administrator without a hospital grant cannot read or write staff.

Every table has forced tenant RLS. Staff policies also require the selected hospital and current subject's hospital grant; INSERT/UPDATE require manager access. Transaction-local subject and hospital context is set by the server from the authenticated principal and validated path. Missing context denies access. Composite foreign keys prohibit cross-tenant hospital references. Lists use the existing cursor pagination contract. UUID-addressed PUT endpoints are idempotent; unchanged retries do not create another business mutation or audit entry.

Staff entries contain employee code, display name, job category and employment status. They are hospital-local employment records, not login identities or a tenant-wide personal/HR dossier. One person can have separate employment entries at multiple hospitals. Reviewed identity linking and selective workforce sharing are later slices; this release does not infer either from matching names or codes. Payroll, licences and patient information are excluded.

Audits record actor, action and resource IDs without names or request bodies. Database triggers append mutation records in the same transaction and reject audit updates/deletes. This is application-role tamper resistance, not an immutable external retention archive or SOC 2 attestation. Database owners remain privileged and require operational oversight.

## Threat and failure boundaries

Assets are staff identities, employment status and access grants. Threats include anonymous callers, another tenant's authenticated user and users probing sibling hospitals. The API independently checks authentication, current tenant membership and hospital grant; PostgreSQL enforces tenant/hospital policy again. UUIDs and client-supplied cursors are never access authority. Cross-tenant and inaccessible object reads/writes return 404; inaccessible hospital staff collections return empty pages without disclosing hospital existence.

PostgreSQL or OpenFGA failure denies requests; no stale grant fallback. Single-database transactions keep staff and audit writes atomic. No broker or Temporal operation is needed for these local writes. Future cross-product events must use the existing transactional outbox, and onboarding orchestration remains with the existing operator.

The migration only creates new tables/functions/triggers; it does not modify old rows or policies. Existing binaries tolerate the added schema. Application rollback retains new data and schema; no destructive down migration. Release through the existing image pipeline and Argo CD/Kargo path, after local real-Postgres tests and migration validation. A production schema migration and rollout require the specific approval mandated by the workspace guardrails.
