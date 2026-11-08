# ADR 0062: Hospital-owned partner groups in shared IAM

Status: implementation candidate; external consuming journeys and production
acceptance remain open. Parent: [partner collaboration design](../design/staff-and-rbac/partner-collaboration.md),
[#1136](https://github.com/tesserix/helivanta/issues/1136).

## Context and capacity

The requested independent pharmacy/distributor portal needs hospital-managed
recipients without making a hospital employee a supplier employee. India is the
first country; Maharashtra and Odisha are the initial trading jurisdictions.
Neither state metadata nor published group membership approves a drug activity.
Use ADR-0026's planning envelope: 100 organizations, 300 locations, 100 peak
requests/second, 4:1 reads:writes and 99.9% availability. This increment bounds
proposal bodies to 4 KiB and validity to 366 days, with a five-second command
budget. These are engineering bounds, not statutory periods. Starting at 20
hospital groups and 50 direct memberships per organization gives 2,000 groups
and 100,000 memberships; at tenfold growth the three-year planning bound is
20,000 groups and one million memberships, before history/audit retention.
Existing indexed Postgres and IAM are sufficient. Measure actual p99 and audit
storage before treating these estimates as an SLO or financial quotation.

## Decision

IAM owns published-group identity, immutable proposals, independently reviewed
membership and typed subject/group assignments. Workforce owns the canonical
hospital, Staff, employment generation, account link and current placement.
Pharmacy owns its trading relationship/disclosure; Supply retains its parties,
premises and source stock. No foreign product or tenant table is queried by a
partner page, and no group is encoded into an existing raw subject field.

Preparation requires the existing access-proposal administration permission and
a current IAM tenant-administrator row, checked with credential/session and
membership locks. This is governance metadata preparation, not operating
permission. It creates an immutable, purpose-bound hospital group proposal; its
response explicitly says `external_access_enabled:false`. The preparer cannot
select or manufacture a supplier grant through this command.

A source request and its audit commit with preparation. Workforce returns a
retained, identifier-only hospital-existence observation through an allowlisted
same-tenant contract. IAM binds the response to the exact group/hospital and
waits for it before offering human approval. A source observation is not staff
eligibility or trade authority. Missing source service leaves `pending_scope`;
unknown/non-hospital references are rejected. No stale allow is substituted.

A different current access authority reviews publication. Direct memberships
must bind to reviewed Staff and a current approved posting/employment generation
at the group's hospital. The command owner forbids self-enrolment and approval
by the member or preparer, including a second account linked to the same Staff
identity. Only direct eligible members are supported; nesting, wildcard and
foreign-hospital members are refused. Membership rotation never expands the
supplier-approved purpose, action, field or location ceiling.

A group is a recipient, not a login, clinical credential, employer or trading
licence. Access requires the same current grant to supply both action and scope,
current home eligibility, exact relationship revision, real object participation
and the owner's disclosure policy. Staff suspension/exit, membership removal,
expiry and unilateral grant/relationship suspension deny subsequent admissions.
No positive graph/cache grace period is permitted. Actual disclosure and physical
acts still belong to their product owner.

## Consistency, failure and recovery

Proposal identity is `(verified tenant, UUIDv7 group ID)` and immutable intent is
hospital, name, purpose, reason, validity and proposer membership incarnation.
An exact authorized retry returns retained evidence; changed intent is 409.
GET recovery reauthorizes the current administrator and audits disclosure.
Cross-tenant rows are 404; an in-scope caller without current permission is 403;
credential revocation is 401 and unavailable authority/audit is 503. Group
preparation cannot reveal another tenant's directory or grant any partner action.

Database/current IAM unavailability blocks commands and disclosure. Audit failure
rolls back the proposal and withholds read data. Broker failure retains the
outbox; publication is not source or human approval. Consumers deduplicate both
delivery and stable owner correlation, reject mismatched/late claims, and retain
source provenance. Review commands have immutable command IDs, expected versions,
retained receipts and bounded history pages. No business success is fabricated
while a dependency is pending. There is no distributed stock transaction in IAM.

Use the existing modular owner APIs and outbox/inbox contracts. This ADR does not
create a generic collaboration microservice, duplicate staff master or new policy
engine. Multi-owner fulfilment and waiting/recovery orchestration remain the
separate Temporal-backed business integration required by ADR-0026.

## Migration and rollout

Add owner tables with forced exact-tenant RLS, explicit indexes, immutable history
and bounded constraints. Do not alter historical IAM template migrations or widen
RLS. Canonical group models generate owner Go and SDK Zod from
`contracts/partner-groups.json`; contract export and source-review inventory must
be regenerated as registrations change.

Exercise source request/observation, independent review, membership/revocation,
SDK/UI, wrong hospital/tenant, dependency failure, duplicate/concurrent command,
read-audit failure and lost-response recovery before enabling a consumer.
Schema expansion is retained on application rollback; immutable group/review
history is not deleted. Production migration/promotion uses the owning GitOps and
Kargo path with exact revision and recovery evidence. No actual legal participant
or regulatory policy is activated by installing the capability.

Incremental cost is indexed IAM rows, bounded source messages and attributable
audit/history on the existing platform. There is no new service, database or cache
fleet. Monetary cost remains unmeasured pending retention and production traffic.
