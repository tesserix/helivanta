# Organization structure

Status: unreleased implementation with local owner/Global API/browser acceptance.
Platform owns structural metadata; Workforce owns hospital/Staff source and
postings. See [ADR-0051](../adr/0051-shared-department-owner-boundary.md),
[command specification](../superpowers/specs/2026-11-02-organization-department-owner.md)
and [local evidence](../qa/2026-11-02-organization-departments-local.md).

## Authority and owner boundary

Business HTTP uses Global API's platform destination. Explicit tenant governance
permissions are `organization.structure.read` and `organization.structure.manage`,
with defaults only for tenant administrators. Verified principal and exact original
account assertions are required; client assertions never establish authority.
The existing OpenFGA role/permission model and IAM current-authorization chain
remain the server boundary. No new clinical default or department membership grant
is created. Product owners retain independent narrower scope and eligibility.

## Hospital-reference protocol

| Method and path | Permission | Behavior |
| --- | --- | --- |
| PUT `/v1/organization/hospitals/:hospital/registrations/:request` | manage | Required UUIDv7 command and reason, 1–500 characters. Request an owner existence observation. |
| GET same request | read | Retained exact command in current tenant/hospital scope. |
| GET `/v1/organization/hospitals` | read | Bounded keyset list of current registration heads. |

Here and below read/manage mean the two explicit organization permissions above.
PUT/GET receipts return `hospital_id`, `request_id`, `status`, `created_at`.
Collection heads also return their monotonic `version`; their creation time is the
head's original creation, distinct from each retained request's time. Canonical
`OrganizationHospitalHead` models that collection explicitly.

Status is pending/verified/rejected. Same normalized intent/actor returns the
retained receipt without a second audit/outbox write; changed intent or cross-
hospital command reuse conflicts. Pending/verified heads cannot be replaced;
rejected permits a fresh retained request. Verification means only same-tenant
hospital existence, never professional approval or a permission.

Organization publishes
`helivanta.in.organization.hospital_reference_requested.v1` with `request_id` and
`hospital_id`. Workforce retains its first observation and publishes
`helivanta.in.workforce.organization_hospital_observed.v1` with those references
and explicit boolean `exists`. Duplicates cannot resample a changed catalogue;
old/contradictory replies cannot replace the current decision. Omission, null,
non-boolean values, unknown fields, foreign destinations and wrong event versions
are rejected; event payloads are bounded to 1024 bytes.

## Departments and sub-units

| Method and path | Permission | Behavior |
| --- | --- | --- |
| PUT `/v1/organization/hospitals/:hospital/departments/:node/revisions/:revision` | manage | Create/update/move/retire using immutable UUIDv7 node and command IDs. |
| GET same revision | read | Exact retained snapshot; singleton, not a collection. |
| GET `/v1/organization/hospitals/:hospital/departments` | read | Bounded current heads, including retired units. |
| GET `/v1/organization/hospitals/:hospital/departments/:node/revisions` | read | Bounded retained history for one node. |

Commands require `expected_version` (0 on creation), `code`, `name`, `kind`,
`status`, `reason`, and `parent_id` for a sub-unit. Code is normalized uppercase
ASCII letters/digits/underscore/hyphen, 1–40 characters, unique even after
retirement and immutable. Kind is immutable: department belongs directly to its
hospital; sub_unit needs an active same-hospital parent. Names are 1–120 trimmed
characters; reasons 1–500 normalized characters. Control text is rejected.

The root reference must be verified. At most 5,000 retained nodes and eight
hierarchy levels per hospital are allowed. Self/descendant parents, unavailable
parents, stale versions, subtree moves beyond the depth bound, identity changes,
retirement with active children and reactivation of retired units fail closed.
Retirement is terminal and preserves history; it is not physical deletion.

Every successful command records its full snapshot, original actor/reason,
expected version and monotonically advancing database effective time. Exact retry
reauthorizes and returns its original receipt even after later changes. Different
intent/actor/node at the same command ID conflicts. Hospital and command advisory
locks serialize writes; compare-and-set prevents lost updates.

Database constraints independently protect identity, transitions, parent scope,
cycles/depth, and retirement. Deferred checks bind every head to its immutable
snapshot and prevent a future revision committing without a head transition.
Audit, head, revision and outbox commit together; failures roll back all four.
`helivanta.in.organization.department_revision_changed.v1` carries only hospital,
node, revision IDs and version. No Staff details, private reason or grant is sent.

Response snapshots include node/hospital/revision IDs, version, code/name/kind,
optional parent, status, effective time and original node creation time. Actor and
private reason remain in owner storage. Lists use standard bounded keyset pages
(default 50, maximum 100); bodies are <=8KiB. Missing permissions are 403,
authorization dependency failure 503, out-of-scope objects 404, invalid input 400
and stale/invalid structural commands 409. Raw database errors are not disclosed.
Successful sensitive metadata reads and writes have atomic owner audit records.

## Migration and acceptance limits

Forward migrations `0001_organization`, `0002_organization_departments`,
`0003_organization_department_revision_integrity` and
`0069_workforce_organization_observations` are persisted and applied to the local
synthetic QA runtime, as well as disposable test databases. Workforce 0068 stays
reserved for the separate unfinished suspension worktree. No new production
migration or rollout occurred. Source registry/contractdocs generation is required.

The owner UI at `/settings/organization` exercises reference registration,
creation, parent linkage, editing, history and safe-removal refusal through the
local signed Global API chain. Real OpenFGA tests cover allow/deny, manager-role
revocation with retained read permission, and outage failure. These do not prove
all Pharma role or lifecycle journeys; see the
[Pharma specialization scope](../architecture/pharma-staff-specialization.md).

Legacy HR vocabulary handoff, canonical Workforce postings, standalone employer
scope, department-scoped delegation/inheritance, scheduled changes, merge/split,
heads/cost centres and product professional acceptance remain open. A department
row or its event never establishes Staff eligibility, outlet duty or clinical
permission. Consumers must not treat this metadata slice as completed staffing.

The [Workforce department handoff candidate](workforce-department-links.md) now retains explicit HR-to-structure revision mappings through owner messages. A matched snapshot creates no posting or grant; canonical posting migration and current eligibility remain open.
