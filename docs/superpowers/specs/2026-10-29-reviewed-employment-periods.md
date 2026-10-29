# Reviewed employment periods and contractor relationships

Status: reviewed lifecycle design. Private draft preparation has an owner implementation and local validation; independent decisions and lifecycle activation remain open. See the [draft API](../../api/workforce.md#employment-term-draft-preparation) and the delivery tracker for separate CI/release gates.

Parent prerequisite: PR #1454. Accepted access
migration policy: ADR-0049. Scope: #1035, #417, Staff lifecycle A07–A10; distinguish
facility vendor/service-contract work in #298.

## Verified current owner behavior

Workforce enrollment already allows employee, consultant, contractor, locum,
trainee and volunteer. Contract-type HR masters are classification references;
profile start/end dates and active/inactive are not reviewed employment periods.
The profile is replaced on update, so adding renewable contract facts there would
lose contract lineage. Existing employment_epoch/source hashing protects identity
preparation from old active/inactive incarnations; it does not model complete
suspension, notice, expiry, separation and rehire. IAM's scoped command guard is
transaction-bound, but its legacy static-permission fallback does not prove a
current employment basis. Every consumer needs inventory/migration under ADR-0049.

## Owner and record boundaries

Workforce owns employment periods, amendments, lifecycle requests/decisions,
immutable history and its current eligibility projection. IAM owns membership,
reviewed access assignments and scope epochs. Existing reviewed Workforce/login
bindings identify subjects; do not equate names/email/employee codes across scopes.
A period cannot itself grant clinical privileges or manufacture identity evidence.

A worker may have no application account. Reviewed employment, identity and card
records must still support that worker; do not create a global login just to issue
a staff ID/card. Digital permissions require the reviewed account/employment
basis. Physical access has its own owner decision and acknowledgment. Independent
review cannot treat a missing target subject as proof that self-review is safe;
use the reviewed Staff/person identity relationships available to the owner.

Model contracts/periods as their own records under verified tenant/hospital/Staff.
Use stable UUIDv7 period and command IDs, optimistic versions, immutable command
receipts/revision history, attributable actors and minimal outbox facts. Store
contract party details as an explicitly recorded snapshot/reference, not as a new
canonical supplier/company master or proof of an agency's licence. Agency supply
is a relationship separate from the worker's profession and hospital reporting
line. Keep vendor work orders, permits and service contracts with their own owner.

## Required intake and review

Require an engagement kind, recorded start, a duration choice (fixed or ongoing),
reason and reference to the applicable contract/appointment evidence. Fixed terms
require an unambiguous effective end; ongoing arrangements must be explicit rather
than accidentally omitting an end. Contracted/agency engagements need a named
accountable hospital sponsor and explicit contracting-party details. A sponsor is
not automatically a clinical supervisor or approver. Reject self-sponsorship and
foreign/missing references; preserve historical inactive references in history.
Do not infer mandatory government IDs, police checks, notice periods, tax rates,
dismissal eligibility or final-pay amounts from the engagement kind.

Intake must distinguish unreviewed drafts, submitted immutable snapshots and an
independent human decision. Reauthorization, current Staff identity/link checks,
source/period generation and current reviewer scope are required at each command.
A reviewed employment period still does not grant login/clinical access. Migration
of an old profile must preserve unknown dates/evidence as unresolved review work;
never fabricate historical facts or silently approve a legacy grant.

## State and cutoff behavior to implement

Keep requested actions separate from accepted/effective outcomes. Submission of
resignation does not end employment. Suspension, accepted resignation/termination
and contractor expiry have explicit effective instants and affected scope; no
universal 23:59 default. Extensions and withdrawals preserve decisions and reasons.
Future cutoff is enforced by authoritative current-time eligibility at the owner
boundary, not by a best-effort timer/event worker. Old cookies, graph tuples,
queued approvals or failed cleanup cannot retain affected operational authority.

Resumption/rehire creates/reviews the appropriate employment generation and new
links/assignments/cards; old approved history and stale events cannot resurrect
authority. Another hospital's employment and independently controlled tenant
ownership/recovery must remain unaffected. Keep clinical/financial/audit history.

Before activating enforcement, review the legacy cohort and every operational
consumer. Governance/recovery classification is explicit. Consumer readiness and
cleanup/handover acknowledgments are distinct: unpaid settlement or missing
property must not keep a separated worker authorized. The UI reports unresolved
obligations honestly and cannot label exit closed until required evidence or an
authorized exception is present.

## Delivery and proof

Implement vertical slices with real Staff Admin/independent reviewer/worker UI.
The first period-record slice must clearly distinguish reviewed terms from active
access; later cutoffs cannot be exposed as effective before every required guard
works. Add no nonfunctional suspension button or fabricated success.

Cover legacy migration, individual/agency contractor intake, fixed/ongoing terms,
renewal before/after expiry, suspension/resumption, notice acceptance/withdrawal,
termination, rehire, current identity failures, self-review, foreign hospital,
concurrent/stale decisions, exact replay, future-cutoff races and two-hospital
isolation. Use real owner services/ledgers and signed sessions; no cross-module SQL
seeding or direct product bypass. Country/state HR/payroll policies need the
applicable primary-source and qualified-owner review before activation.

## Planning budgets, atomicity and release boundaries

Reuse the Staff foundation's unmeasured planning assumptions: 500,000 Staff in
12 months and 1.5 million in 36 months, 50 peak requests/second, 99.9% availability
and p99 below 500 ms for owner operations. At two periods/Staff and three 1 KiB
revisions/period, the 12-month history assumption is about 3 GB before indexes and
replicas; at three periods/Staff and five revisions/period, the 36-month assumption
is about 22.5 GB. Validate against real deployment demand before capacity claims.
Keep the existing Postgres/monolith, bounded 8 KiB metadata commands and cursor
pages of 50 by default/200 maximum; evidence bytes use protected storage/scanning.
No new always-on service, cache or queue is justified by those assumptions.

Declare consistent lock order with IAM/current binding and owner rows before
implementation. Head, immutable history/receipt, minimized audit and outbox must
commit in one owner transaction; never call a remote identity/provider/storage
service while holding those locks. Authorization/owner availability failures deny
with an explicit unavailable response. Lost-response replay returns the immutable
command receipt only after current authorization; it cannot reactivate an ended
incarnation. Future cutoff must recheck owner time/state after waiting for locks.
Test transient errors, duplicate delivery, stale generations, deadlocks and clock
boundaries with real owner state and deterministic clocks rather than sleeps.

Use additive, bounded migrations with lock/statement timeouts, composite scoped
keys, immutable-history constraints and forced RLS. Add indexed single-record and
bounded history reads; validate query plans at the stated data volumes. Rolling
back application code must retain accepted employment/exit/history records and
must not reopen denied access. Enabling an authoritative employment cutoff is a
reviewed activation with a tested governance/recovery path, not a destructive
rollback or a reason to restore historical grants.
