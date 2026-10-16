# IAM membership synchronization

Issue #1173; epic #1133; related #1135 and #781. Owner Sam123ben.

## Decision

The existing grant consumer must create the role→tenant edge before assigning
its subject→role edge. Keep permission reconciliation first. Return errors from
either write so the existing transactional inbox and NATS redelivery retry the
whole grant. Both OpenFGA writes are idempotent; a crash between them leaves an
unused role edge, which grants no new subject access. Revocation removes only
the subject edge; deleting a shared role edge would deny other role holders.
Postgres remains authoritative and boot reconciliation remains the backstop.

Assets are tenant data and role integrity. Threats include an unrelated tenant
or an incorrectly privileged account. Trusted tenant/subject data comes only
from the audited membership API/outbox; no new endpoint, permission, bypass,
identity claim or credential is introduced. Existing route membership and
permission guards, party grants and forced RLS remain enforced.

This adds one idempotent OpenFGA write per membership grant, with the existing
consumer timeout/retry/dead-letter budget. It adds no requests to ordinary
reads, storage, service or recurring infrastructure cost. No new throughput or
SLO is asserted: this repairs the existing low-volume asynchronous grant path.
When OpenFGA is unavailable, the grant stays unprocessed and normal requests
remain denied until convergence. No distributed transaction is introduced.

Alternatives rejected: restart-only recovery masks the live consumer defect;
granting every role tenant membership at boot broadens the tenant graph beyond
roles actually held; changing the membership gate would weaken authorization.

## Verification and delivery

A public membership-API regression uses real disposable Postgres, NATS and
OpenFGA, with a previously unused supply_compliance role. Assert successful
tenant membership, normal guarded HTTP access, duplicate grants, independent
role holders, revoke, foreign tenant denial and retry after an injected external
write failure. Observe failure before repair. Full race/coverage, vet/build/lint
and repository web/hosted gates precede release.

No migration or UI contract change. A specifically approved application rollout
uses existing GitOps; rollback is the previous image and restores this defect,
so forward repair is preferred. Production acceptance must use the created
least-privileged synthetic reviewer, not an administrator bypass. This slice
does not complete broader onboarding, country policy or B2B stories.
