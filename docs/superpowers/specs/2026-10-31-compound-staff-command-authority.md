# Compound Staff command authority

Status: internal guard implementation in progress; no HR consumer or template
is activated. Full A01–A12 remains required. Consumers and storage phases are enumerated in the Staff intake
source inventory; ADR-0049 retains the legacy-cohort and employment cutoff gates.

## Required boundary

Workforce employment evidence requires BOTH enrollment and document authority.
IAM owns current assignments, membership/scope generations and credential
watermarks; Workforce retains live hospital/Staff/person/record/release checks.
Do not union unrelated actors, tenants or hospitals, invent a private grant table,
call two nested guarded transactions, or hold a database transaction over storage.
An ordinary assignment cannot manufacture a hospital-manager row, reviewed person
identity, employment approval, clinical privilege or payroll authority.

Add an internal Go compound guard accepting the existing request/grant evidence
for one or two distinct required permissions, the verified authentication time,
and one synchronous owner callback. Every check must bind the same verified
subject, tenant, hospital, membership generation and acknowledged scope epoch.
Require valid current receipts; missing, duplicated, conflicting, stale or foreign
checks must not run the callback. Treat dependency/invalid provider evidence as
unavailable and withdrawn/expired current authority as denied. Products derive
required permissions and principal from their own verified route, never JSON.

Keep the existing single-grant interface compatible by delegating its one check
to the same implementation. The new compound interface is additive; a missing
implementation must fail closed for compound consumers. It does not expose an
HTTP endpoint or transfer evidence to browsers.

## Locking and time

Use one existing guarded tenant transaction and one shared credential-watermark
lock. Do not sort/mutate caller input in place. Lock the complete set of active
proposal rows in stable ID order before the subject membership-continuity row,
then the hospital/subject scope-epoch row. Validate every exact snapshot/version,
permission, current membership and acknowledged epoch before owner work. Recheck
source state after lock waits and all validity bounds before commit; expiry of
any required grant must cancel/roll back the whole owner action. Keep source
locks until the owner callback/outbox/audit commits or rolls back together.

The current single guard locks joined proposal/membership/scope rows in one
query. Repeating it inside a compound transaction can hold a scope row before
locking another proposal, conflicting with withdrawal/projection lock order.
The proposed explicit shared order must be verified against actual withdrawal,
final membership removal, projection activation/acknowledgment and credential
revocation. In particular, do not lock pending proposals that a projection worker
may update after taking its scope lock. This ordering is a design to validate,
not a claim that existing concurrency tests already prove the new combination.

Graph/provider I/O stays outside the transaction. At each upload/download phase,
reauthorize the complete requirement and then enter a fresh guarded owner
transaction. Released-byte retrieval remains outside transactions; its final
read/release/audit phase must reauthorize again. No stale route-level proof can
substitute for the final owner boundary. Actor identity/link and Staff source
checks remain owner-owned under the same joined transaction.

## Budgets and compatibility

Retain the current five-second guarded command budget and the caller's verified
session deadline. The actual evidence consumer needs two permissions, bounded
indexed proposal/membership/scope reads and no new service, table, queue or
cache. Reuse unmeasured planning assumptions of 10 peak access commands/second
per large tenant; measure before latency/SLO claims. This adds query work, not a
new runtime infrastructure charge. Dependency failure denies/unavailable; callback
failure, cancellation or expiry rolls back all owner business writes/outbox.

Template definitions must use the canonical frozen migration catalog. That
catalog only controls generated history; an additive SQL mapping alone does not
prove an old/new application fleet enforces every new template precondition.
Actual consumer compatibility, template discovery/proposal rejection, supported
snapshot authorization and activation order must be tested before a new template
or employment cutoff is enabled. Preserve historical SQL and grants on forward
repair; do not restore old epochs/authority on application rollback.

## Proof and full-workflow gates

Exercise real IAM/Workforce services and ledgers, not copied repository models:
one approved template with both permissions or two independently reviewed current
assignments; missing either requirement; equal/mismatched generations; other
account/hospital/tenant; duplicate checks; immutable caller input; dependency
failure; exact source withdrawal/final membership removal/credential revocation
before and during lock waits; earliest expiry; owner callback failure/outbox
rollback; and concurrent single/compound commands with withdrawal/projection.
Preserve every existing single-grant journey and current denied/unavailable error.

Deliver the compound guard with its real HR owner transactions, template evolution,
capability/discovery/UI, current reviewed actor identity and legacy-governance
classification. Keep the normal Admin → worker → independent reviewer acceptance
and full employment submission, suspension/resumption, notice/exit, contract
expiry/rehire and cross-module cutoff scope. This document activates none of them.
