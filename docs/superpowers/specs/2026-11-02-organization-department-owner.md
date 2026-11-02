# Organization department owner

Status: implementation in progress. This is the ownership prerequisite under
ADR-0044/ADR-0051, not completion of the reusable Staff goal.

First register an actual Workforce hospital reference through an idempotent
request and independent identifier-only owner observation. Current organization
governance permission is required per operation. No copied hospital/Staff master,
cross-owner SQL or authority derived from an asynchronous existence observation.

Then deliver the department/sub-unit tree with immutable revisions, immediate
server-effective changes, optimistic commands, parent/depth/cycle constraints,
retirement and bounded pages. Reconcile legacy HR department revisions. Follow
with Workforce postings, product acceptance and current lifecycle enforcement.

Initial APIs go through Global API's platform destination:

- PUT `/v1/organization/hospitals/:hospital/registrations/:request`: required
  reason, canonical nonzero hospital UUID, UUIDv7 request and
  `organization.structure.manage`; exact original tenant/subject assertions.
- GET that request: current `organization.structure.read`, exact tenant/hospital;
  result is pending/verified/rejected, never a clinical grant.
- GET `/v1/organization/hospitals`: bounded tenant-governance collection under
  the explicit read permission. No public enumeration.

Only one current registration per hospital. Pending/verified cannot be replaced.
A rejected registration permits a fresh command, retaining earlier history.
Cross-hospital key reuse or different normalized intent is 409; exact retry reads
its retained status without a duplicate audit/outbox write. Responses cannot
replace a newer request. Conflicting duplicate observations fail closed.

Required tests: exact role/session assertion gates, nonexistent/wrong hospital,
immutable intent, duplicate delivery, conflicts, old replies after a new request,
cross-tenant isolation, bounded lists and audit/outbox rollback. Use real Postgres,
actual Workforce consumer, signed Global API and normal UI before full exposure.
Pending registration grants no Staff or structural command authority.

The organization schema needs named production migration approval after complete
vertical acceptance. Previous activation approval does not cover this module.

## Department command and revision contract

Implements ORG-ST-01/02/05/07 metadata under the accepted owner; it does not
implement ORG-ST-03 access inheritance. Tenant-wide governance managers with
`organization.structure.manage` may change metadata; readers require the
independent read permission. Existing tenant-admin governance defaults apply;
no clinical or Staff authority follows. Scoped department management assignments
are not yet exposed and no open ORG-D decision is settled here.

Use PUT `/hospitals/:hospital/departments/:node/revisions/:revision`, with UUIDv7
node/revision, required expected_version (0 for creation), code, kind, name,
status and reason. Parent is omitted/null for a department; sub_unit needs an
active same-hospital department/sub-unit parent. Code is trimmed uppercase ASCII
letters/digits/underscore/hyphen (1–40), immutable and unique for the hospital
including retired rows. Kind is immutable; names are trimmed, non-control text
of 1–120 characters. Reasons are normalized non-control text of 1–500 characters.
Retirement is terminal, retains all history and requires no active children.
Root departments have no parent; nested sub-units have at most eight levels
including the department. Reject self/descendant parents and changes that would
push any subtree beyond that bound. Maximum 5,000 retained nodes per hospital.

The command is immediately effective at a monotonically advancing database
server timestamp. Every revision records original actor, normalized intent,
expected version and complete node snapshot. Exact retry reauthorizes and returns
the original receipt even after later edits; reuse for different intent/actor/node
conflicts. Node/version compare-and-set rejects stale edits. A hospital advisory
lock serializes all structural writes; command lock precedes hospital lock.
Database triggers independently enforce immutable identity, version, parent
scope/state, cycles/depth and retirement. Deferred consistency checks bind each
head to its immutable revision. Reads are bounded keyset collections; revision
history is addressed by node and every retained revision is scoped to tenant and
hospital. GET out of scope is 404, missing permission 403, pending/rejected
hospital registration 409. Authz dependency failure remains fail-closed 503.

Mutations emit identifier-only department_revision_changed.v1 through the atomic
outbox. Events and revision IDs are reference observations, never permission.
Products must consult their current owner guards; no cached tree grants access.
Audit/outbox failure rolls back head and revision. No network call occurs inside
the transaction. DB failure fails the command; read failure is explicit rather
than empty success. Historical hierarchy can be reconstructed from effective
revision times; moving a subtree does not rewrite descendant history or any access.

Use inherited ADR-0051 budgets (50 peak requests/s, 80:20 read/write, p99 <500ms,
99.9% availability); command bodies <=8KiB and pages <=100. At 5,000 nodes per
hospital, bounded recursive validation fits existing Postgres; no new service,
cache or paid dependency. Immutable history grows with actual metadata changes;
12/36 month Staff scale stays 0.5m/1.5m, with no separate unmeasured department
population assumption. Forward migration 0002_organization_departments is additive
and evidence-retaining; rollback disables callers and retains schema/history,
never drops it. Production rollout/migration needs named approval after full UI
acceptance. Scheduled changes, merges/splits, heads/cost centres, legacy HR
mapping, postings and product acceptance remain explicit next work.

Forward migration 0003 adds the deferred revision-to-head commit check found by
direct Postgres fault testing. It is separate because 0002 was already applied
to the local synthetic fixture; applied SQL was not rewritten. Local acceptance
and remaining product/Staff obligations are recorded in
`docs/qa/2026-11-02-organization-departments-local.md`.
