# Platform identity, tenancy and shared contracts

[Contract index](../README.md) · current implementation with target gaps

## Identity and scope

IAM owns membership grants/revocation, session activity/renewal/sign-out, current permission/membership queries and tenant switching. The verified Helivanta session selects tenant; IDs in request data or headers cannot override it. `GET /v1/iam/me/tenants` returns caller memberships; readable tenant names remain tracked in #1264. `GET /v1/iam/me/profile` returns the caller's Zitadel display name and email plus their role labels in the session tenant, failing closed with 503 (`authz_unavailable`, `identity_unavailable`) rather than a partial answer (#1428). Switching re-mints the session and clients must invalidate/rekey scoped state.

The current login boundary uses Zitadel and Helivanta session minting. Earlier README/backlog descriptions of hosted login/GIP/Keycloak may be historical. Current wiring in `backend/internal/bootstrap/unauthenticated.go`, IAM login handlers and `apps/shell` owns what is served. Session renewal/activity, absolute/idle lifetime, revocation watermarks and sign-out are security contracts, not optional frontend conventions. See [session lifetime](../../architecture/session-lifetime-and-remembered-login.md).

Tenant, hospital, pharmacy outlet, supply premise, emergency base and receiving facility have different scopes. A tenant can contain multiple hospitals; a standalone pharmacy is not a hospital. Forced RLS and handler object grants implement narrower boundaries in the existing domains, but the legacy visit/lab/dispense slice remains tenant-scoped. Do not claim universal hospital isolation until that slice is migrated and tested. Group-sharing architecture is proposed and does not make all same-tenant records mutually visible.

### Current identity on protected requests

The production V1 middleware constructor requires IAM's current-identity guard
after rate limiting and before permission resolution. A known non-active or
deleted Zitadel identity durably revokes its observed credential before returning 401. Reactivation does not restore that credential; a fresh authentication is
required. A fresh, indexed IAM watermark read also denies a request whose replica
missed invalidation, including revocation during the upstream state read.

Provider errors, unknown state and persistence failures return the existing
`503 identity_unavailable` envelope, without executing the business handler or
turning an unavailable answer into a permanent revocation. Only the registered
POST self sign-out command skips the upstream lookup, while retaining fresh IAM
and its existing authentication. Administrative revocation must verify current
identity because it changes another user's access.
An identity outage now blocks ordinary protected requests, not only renewal.
No active-state cache or new role/endpoint is introduced. The complete check has
a ten-second budget. See [ADR-0047](../../adr/0047-current-identity-on-protected-requests.md).
Migration `0013_iam_identity_guard` admits the new `identity_inactive` watermark
reason; the invalidation event payload is unchanged. This is distinct from
hospital employment inactivation, suspension and cross-owner offboarding.

### Current IAM role admission

The current-role admission candidate additionally intersects graph-resolved
permissions with current IAM membership roles and the canonical registry grants.
Both V1 permission resolution and route membership checks use the same IAM-owned
adapter. A role removed from PostgreSQL cannot authorize a new request through
its stale graph tuple, even while another membership role remains. Final
membership removal denies business routes while authenticated self-service
routes retain their existing markers. Another tenant's current roles are
unaffected. Missing graph authority cannot be replaced by a SQL membership.

The bounded forced-RLS role read follows the external graph read; source or
graph failure denies with the existing `503 authz_unavailable` behavior. This
adds no route, role, permission or schema and does not turn request admission
into a transaction-held employment cutoff. Tenant switching/discovery still
uses its existing role-list contract. See the
[design and acceptance limits](../../superpowers/specs/2026-10-30-current-tenant-role-authority.md)
and the [full A01–A12 plan](../../superpowers/plans/2026-10-30-staff-rbac-completion.md).

### Current source for administrative credential revocation

The explicit security action `POST /v1/iam/subjects/:subject/revoke` retains its
global session effect; it is not a hospital employment or membership removal.
The candidate honors the optional `X-Helivanta-Expected-Tenant` precondition
before external lookup (`409 session_changed` for stale/malformed assertions).
Graph lookup must succeed and bind the target to the verified acting tenant.

The write additionally requires the current administrative membership and fresh
credential watermark of the actor, plus a current known target membership in
that tenant. Deterministically ordered credential advisory locks and shared
membership row locks fence the existing watermark/outbox transaction, including
source changes during external lookup or lock waits. An absent target returns
404, a removed administrator returns 403, a revoked/expired acting credential
returns 401 and unavailable authorization returns 503. Persistence failure
returns 500 and rolls back the watermark and event. No new wire payload, event,
permission or migration is introduced. Memberships in other tenants are retained;
the existing target credential invalidation remains deliberately global.

Self-sign-out keeps its separate authenticated self-service path. See the
[revocation-scope design](../../superpowers/specs/2026-10-30-current-credential-revocation-scope.md).

## Shared capability contracts

| Capability                    | Current source                                                             | Contract/review requirement                                                                                                              |
| ----------------------------- | -------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| Identity/organization linkage | IAM and `pkg/organizationidentity`; `contracts/organization-identity.json` | Verify external identity authority and current linkage; unavailable authority is not an allow                                            |
| Branding/workspace            | `workspace`; [workspace API](../workspace.md)                              | Published login branding differs from authenticated branding management; display metadata cannot confer access                           |
| Country policy                | `workspace/country_policy.go`; `contracts/trading-policy.json`             | Submit/read/review/access/version lifecycle, cited evidence and reviewer authority; no hardcoded country exception replacing policy      |
| Patient identity              | `patientmaster`; [clinical spec](patients-and-clinical.md)                 | Hospital grants, consent/matching and access audit; ID alone does not grant care access                                                  |
| Private files                 | `pkg/attachments`, workforce/supply file owners                            | Registered owner/scope, size/type, exact generation/checksum, quarantine scan, authorized release and download, revoke/retry lifecycle   |
| Audit                         | `pkg/audit`, owner audit tables and migration triggers                     | Actor/tenant/object/action/time and attributable changes; no unrestricted PHI export or mutating historical evidence                     |
| Events                        | `pkg/events`, module `/contract` packages                                  | Atomic outbox/inbox, declared subjects, bounded delivery and scoped provenance; [integration specification](../integration-contracts.md) |
| Pagination/insights           | `pkg/pagination`, platform list/insight helpers                            | Cursor bounds and calendar/timezone/window semantics; reports state freshness and missing data                                           |
| Drug master                   | `drugmaster`; [runbook](../../drugmaster.md)                               | One global catalogue, read-only to the app; content only by a release staged and decided by different people; versions never rewritten   |

File workflows require GCS, exact-content ClamAV verdicts and a matching durable Temporal execution. Storage failure leaves uploads incomplete/quarantined; scanner unavailability cannot yield clean status; revoked/changed content cannot reuse an old verdict. Protected release/download is reauthorized, and a path prefix or signed URL is not the full security model. See [file storage/scanning architecture](../../architecture/07-file-storage-and-scanning.md) and each current owner API.

## Future external/service boundaries

### Staff membership enrollment

`/workforce/access` exposes an existing-account enrollment form to callers with
`iam.member.manage`, independently of assignment administration. It submits only
`staff_member` through the Global API platform destination. This role supplies
membership with no declared business permissions. It creates no identity,
reviewed login link, hospital grant, scoped assignment or clinical privilege.
Administrators must obtain the existing identity-provider subject; the generic
membership endpoint does not verify that identity's existence at the provider.

`POST /v1/iam/members` now honors the optional
`X-Helivanta-Expected-Tenant` precondition through the shared principal helper.
Mismatched, malformed, empty or repeated values return `409 session_changed`
before any write; headerless clients retain their existing contract. The header
never selects the tenant. Normal membership and `iam.member.manage` checks remain
mandatory. The general endpoint still accepts the existing system-role catalog;
the Staff UI fixes its request to `staff_member`.

`MemberGrantInput` and `MemberGrantResult` in `contracts/staff-access.json` are the
canonical generated request and `{id}` response models. An accepted `202` commits
the membership row and outbox event, not a verified login or synchronized access.
The tenant/subject/role uniqueness constraint preserves the membership ID for an
identical retry and republishes the event for graph repair. The form retains its
original subject and tenant after an uncertain result. A rejected workspace or
access check requires refreshing before another attempt.

### Membership-removal workspace precondition

`DELETE /v1/iam/members/:subject/:role` now honors the same optional `X-Helivanta-Expected-Tenant` precondition as member enrollment. If supplied, it must be exactly one nonzero UUID matching the verified session tenant. Empty, malformed, changed, duplicate or comma-joined values return `409 session_changed` with `Cache-Control: no-store` before membership mutation or outbox publication. The header never selects a tenant or grants permission: `iam.member.manage`, current authenticated tenant and forced RLS remain authoritative. Omitting the header preserves the existing `202` deletion/tuple-repair contract, including replay when the role row is already absent.

This does not add actor or membership-incarnation preconditions, a versioned removal command, global identity deletion, employment suspension/offboarding or an admin removal UI. Role removal remains distinct from hospital employment and global login state. A future removal UI must reconcile uncertain outcomes and re-enrollment rather than treating this optional tenant guard as a complete offboarding workflow.

### Assignment proposals and independent decisions

The Staff access page at `/workforce/access` uses the existing contracts below for
bounded proposal, history and decision reads, proposals, independent review and
proposer withdrawal. Requests carry the expected-tenant precondition and private
responses are not cached. Unknown command outcomes retain the original command
for an identical retry; stale versions require refreshing. The page distinguishes
hospital verification, review, activation and expiry. It does not expose legacy
hospital grant mutations. The only supported template is leave reviewer; hospital
administration, clinical authority and payroll access are not granted by it.
Product-route enforcement and the live independent-review journey remain separate
acceptance gates.

IAM owns `iam_assignment_proposals` and its append-only history, migration
`0008_iam_assignment_proposals`. Migration `0009_iam_assignment_decisions` adds
an immutable decision ledger and reviewed transitions. `iam.assignment.manage`
and `iam.assignment.review` have no explicit system-role grant; the current
implicit tenant-admin baseline applies. Every administrative read and command
also checks the current authoritative IAM `tenant_admin` membership, so stale
graph permissions cannot restore revoked administration. UI traffic uses the
Global API platform destination.

| Operation                                        | Contract                                                                                                                                                                      |
| ------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT /v1/iam/assignment-proposals/:id`           | Stable canonical UUIDv7 ID; body `{hospital_id,subject,template_key,reason,valid_from,expires_at}`; first template `workforce.leave_reviewer.v1` proposes leave approval only |
| `GET /v1/iam/assignment-proposals`               | Audited, bounded proposal list                                                                                                                                                |
| `GET /v1/iam/assignment-proposals/:id`           | Audited detail `{id,input:{...proposal fields},proposed_by,status,version,created_at,updated_at}`                                                                             |
| `GET /v1/iam/assignment-proposals/:id/history`   | Audited, immutable ID/version/status/timestamp history; original fields remain immutable on the retained proposal                                                             |
| `POST /v1/iam/assignment-proposals/:id/decision` | `{expected_version,decision,reason}`; decision is `approve`, `reject` or `withdraw`; returns the current proposal directly                                                    |
| `GET /v1/iam/assignment-proposals/:id/decisions` | Audited, paginated immutable command ledger with actor, reason, expected/result versions and timestamp                                                                        |

All reads are `no-store`; collections use keyset pagination, default 50/max 200.
Bodies are limited to 8 KiB, subjects to 200 characters without whitespace, NUL
or `*#:`, and reasons to 500 characters. Validity normalizes to UTC microseconds;
expiry must be future, after start, and at most 366 days after start. The target
must currently exist in IAM's authoritative tenant membership table. Workforce
subsequently checks the proposing actor's explicit hospital-manager grant.

Identical retries return the current proposal without another owner request.
Changed content or proposer for the same ID is 409. Membership and expiry are
rechecked on retry. Invalid input is 400; missing membership or inaccessible
proposal is 404; insufficient permission is 403. The optional
`X-Helivanta-Expected-Tenant` must contain exactly one value matching the verified
tenant, otherwise 409; it never supplies authority.

Creation commits `pending_scope`, version 1, history, audit and the correlated
owner-request outbox event atomically. IAM accepts a result only for its current
tenant/proposal/request/hospital, rechecks target membership and expiry, then
records version 2 as `pending_review` or `scope_rejected`. Unknown and unauthorized
hospitals have the same negative result. Duplicate/stale responses cannot reopen
or overwrite completed checks. Outages leave the proposal visibly pending;
outbox retries and existing dead-letter replay recover delivery. Rejection is
retained; a corrected proposal uses a new ID.

Reads require manage or review permission. Approve/reject require review permission
and an actor different from both proposer and target. Only the proposer with
manage permission can withdraw. Commands require a positive current version and
a nonblank reason of at most 500 characters. A matching actor/action/reason retry
at the same expected version returns the current proposal; changed commands,
stale versions and illegal transitions return 409. Approval also rechecks target
membership and expiry on retries.

Approve/reject change `pending_review` to `pending_decision_scope`, atomically
recording the decision and a fresh owner request for the reviewer. A denied owner
check or revoked reviewer administrator membership returns to `pending_review`;
it does not finalize the decision. Successful verification records `rejected` or
`approved_pending_activation`; approval also rechecks target membership/expiry,
otherwise recording `scope_rejected`. Each transition increments the version.
Withdrawal is allowed from `pending_scope`, `pending_review`,
`pending_decision_scope`, `approved_pending_activation` and `active`; it changes the request
correlation and records `withdrawn`, so delayed responses cannot reopen it.
Rejected and withdrawn proposals cannot reopen. The Staff access UI may prepare renewal from an activated assignment, including an expired one. Renewal submits a new proposal ID with fresh mandatory dates and reason for the same account/hospital/template; it does not mutate the source or inherit its authorization. The prior ID/version in the reason is a human reference, not a server-enforced predecessor relation. Current membership generation, hospital verification, independent review and acknowledged activation apply again. Withdrawn, rejected and unactivated records have no renewal shortcut. The decision ledger records
**requested actions**, not successful review outcomes: use proposal state and
history to determine the outcome. Reasons remain private to protected reads.

Canonical models come from `contracts/staff-access.json`. Intake proof is not
reusable authorization. Even `approved_pending_activation` grants **no access**.
Migration `0010_iam_assignment_projection` adds immutable permission snapshots
and epochs scoped by tenant, hospital and subject. Approval records the reviewed
proposal revision, approver, validity and exact fixed-template permission set.
Approval and withdrawal advance the epoch atomically with a projection request
in the outbox. The request publishes only the proposal ID.

The IAM projection consumer rereads current state, projects eligible snapshots
in keyset pages of at most 200 outside database transactions, and acknowledges
only the epoch it read. Retries repeat idempotent graph writes; dependency failure
leaves the epoch unacknowledged. Concurrent changes prevent a stale acknowledgment
and retry against current state. Withdrawal preserves immutable snapshots; graph
tuples may remain, so they must never be used without authoritative state checks.
The worker has a 30-second attempt deadline and the bus's retry/dead-letter path.

Migration `0011_iam_assignment_membership` tracks a durable membership generation
and role count for each tenant/subject. Final-role removal advances the generation
and all that subject's hospital-scope epochs atomically. Re-enrollment does not
reset it; adding/removing an extra role and duplicate grants/revokes preserve it.
Proposals capture a current generation under a shared lock, and approval, scope
responses and retries recheck it. Snapshots retain that binding; the projection
worker excludes old generations. Old proposal retries return 409, and approval
commands for an unavailable membership generation return 404. A fresh proposal
and independent review are required after re-enrollment.

Final-role removal requires the authenticated actor, which the member revoke
handler supplies to the transaction. It retains immutable
`iam_membership_revocations` evidence (ended generation, removed membership/role,
actor and time) and an audit entry. Unauthorized history modification and generation
reset fail at the database boundary. No private reason or new identity payload is
published. The role-count backfill locks the membership table and temporarily
disables forced RLS only for the schema owner's read, restoring it before commit;
application-role RLS stays enabled. Historical proposal/snapshot bindings remain
zero and cannot be assumed continuous. Rollback retains the prior database state.

Migration `0012_iam_assignment_activation` adds `active`. After a snapshot's graph
write succeeds, the worker rechecks membership, expiry and scope epoch in a short
transaction and records activation/history with actor
`system:iam-assignment-projector`. The proposal version increments; the reviewed
snapshot version is unchanged. Activation does not advance the scope epoch again.
An `active` state alone is not access: until the whole scope is acknowledged,
authorization still denies. Future-dated active records also deny until valid.

IAM's `authz.ScopedAuthorizer` implementation selects current valid active
snapshots for a verified tenant/subject/hospital and the server-declared action.
It requires matching current membership generation and scope acknowledgment,
checks the exact immutable FGA object, then rereads the same version, generation,
scope epoch and validity. The five-second request budget allows at most 200
candidates; overflow or any database, graph or audit failure returns unavailable
without a grant. State-based denials and allows are audited without private
reasons or staff profiles. Returned grant evidence belongs to one request
and must not be cached or treated as a bearer token.

API bootstrap now installs the provider in the shared router. Opt-in routes use
`POSTScopedHospital`, declaring their hospital parameter and permission. Registry
and contract export retain optional `scope_kind` and `scope_parameter` metadata.
Middleware keeps membership first and constructs scope requests from verified
principal and path values. Private request evidence is bound to principal,
hospital and action; browser assignment headers cannot supply it. Legacy route
permission remains an independent gate, with failure state available to the
product handler when the scoped lookup is unavailable.

The shared gate passes Global API tests with real IAM/Postgres/NATS/OpenFGA using
a test-only registered endpoint. Production workforce routes have not opted in.
Workforce staff/link lifecycle and object checks, broader
administrative revocation workflows and administration UI remain unfinished.
No business route gains access from these changes alone. Historical zero-generation
bindings require withdrawal and fresh review; no legacy grant is converted.

The worktree adds `staff_member` to the IAM system-role catalogue, labelled
"Staff member (membership only)". Existing `POST /v1/iam/members` and
`DELETE /v1/iam/members/:subject/:role` retain their `iam.member.manage`
requirement and asynchronous 202 behavior. The role grants tenant membership
and no declared business permissions. It does not create a workforce link,
hospital grant, scoped assignment or clinical privilege. Existing member events
retain their payload shape; `role_key` can now carry this additional value.
`GET /v1/iam/roles` exposes it to authorized administrators. No existing member
is automatically reassigned. The Global API keeps routing these paths to the
platform owner.

Machine delegation, audience/scopes, Istio workload identity, region/country routing, product entitlements, DPA activation, cross-hospital sharing and offboarding/retention require reviewed contracts before API extraction. A browser session API is not automatically an external partner interface. Preserve current resource IDs and a single writer during extraction; route/version rollout must include reconciliation and recovery for writes accepted by the new owner.

Adapters for FHIR/ABDM, HL7 analysers, DICOM/PACS, TPA/NHCX, payment, accounting/ERP, SMS/email/WhatsApp and government emergency systems need explicit tenant mapping, credential ownership, authenticity/replay checks, minimum disclosure, idempotency, timeout reconciliation and approved country policy. No connector is certified merely because its issue or route prefix is present.

Threat boundary: patient/staff/commercial personal data, financial/stock state and credentials are protected from unauthenticated callers, another tenant, overprivileged insiders and compromised adapters. Validate and authorize every boundary crossing, minimize broker/CRM payloads and logs, and fail closed for clinical/security authority. Record availability effects separately from permission denial.
