# Scoped staff access foundation

Implementation design for #1132/#1053/#426–#434, applying ADR-0044 and
ORG-AC-01/02/04/05/06/07/08. This is not a deployment or permission to migrate
live assignments. Existing organization decisions ORG-D2/D5/D6 remain open.

## Owners and acceptance

IAM owns approved assignments, immutable template revisions, validity, history,
current scope epochs and projection acknowledgments. Workforce owns staff,
hospital-local account links and employment status. The organization-structure
owner will own department scope; workforce must not create a second department
master. A link or posting is not an assignment. Existing tenant roles and product
grant tables cannot silently become approved assignments.

First consumers are hospital-scoped HR administration and staff-self leave.
A self-service read/command must resolve the caller's active reviewed link and
current IAM assignment server-side, apply the same leave rules as HR, and exclude
approval of the caller's own request. No user-supplied staff ID, epoch, permission
or template authorizes an operation. IAM administrative assignment commands need
independent approval, attributable reason and protected history. Only the owning
product can verify that a hospital or staff scope exists and belongs to the tenant;
IAM must use its owner contract, never cross-module tables.

Threat boundary: staff identities, private leave reasons, payroll and authority
are valuable to another tenant, an overprivileged insider or a compromised stale
worker. Verify exact tenant/hospital/resource scope and current authorization on
every command and disclosure. Staff self-service carries no clinical permission.

## Planning envelope

These are synthetic sizing assumptions, not measured service commitments: 100
hospitals × 1,000 staff, four assignments per staff (400,000 current assignments),
10% monthly churn (480,000 history versions at 12 months; 1.44 million at 36).
Plan for 200 authorization checks/s peak and two assignment mutations/s (100:1),
8 KiB command bodies, 200-row maximum pages and 32 permissions/template in the
first supported catalogue. Target p99 <300 ms and 99.9% monthly availability must
be validated under load before being offered as an SLO. No new service, queue or
cache: use the current Postgres, OpenFGA and outbox. Incremental infrastructure
count/cost is zero; storage and request cost still grow with retained versions.

## Projection invariant

Add separate OpenFGA `assignment` and `scoped_permission` types. Assignment
membership is the intersection of its explicit subject and current membership in
its tenant. A scoped permission resolves through an assignment; it does not enter
`perm`/`role`, so it cannot leak into tenant-wide Resolve or create membership.
All object keys bind tenant, hospital, scope kind/id, assignment ID and immutable
revision. The key is a tenant namespace plus SHA-256 of the complete canonical
snapshot, including subject and sorted permissions. This stays below OpenFGA's
256-character object limit even at the 100-character permission bound, without
putting subjects into object IDs. Conflicting content cannot share projection
keys, and reordering the same permission set preserves identity. Projecting an old revision cannot populate a different revision's keys.
The initial scope kinds are hospital and staff, both under a verified hospital.
Permission identifiers are platform-declared actions, never tenant-authored code.

This graph check is only one part of authorization. IAM must first provide the
current active assignment revision, template permissions, validity and current
scope epoch. Require the projection acknowledgment to match that epoch, check the
exact graph object with higher-consistency reads, then recheck the epoch before
returning an allow. Products additionally verify current object/link/staff state
and revalidate high-impact actions at their transaction boundary. Never accept a
revision supplied by the browser as current. Graph tuples alone cannot establish
current assignment validity, independent approval, entitlement or clinical fitness.

## Durable changes and failure behavior

### Owner verification transport

Use the existing transactional event transport for owner scope verification;
do not introduce synchronous private callbacks or IAM reads of workforce tables.
The first supported proposal is the immutable `workforce.leave_reviewer.v1`
template for a hospital, carrying `workforce.leave.approve` only. IAM verifies
the target's current authoritative tenant membership before recording a proposal.
Its new `iam.assignment.manage` permission initially has no explicit system-role
grant (the existing tenant-admin baseline applies). A proposal grants no access.

IAM commits `pending_scope` and a correlated verification request in the same
transaction. Workforce checks the hospital and the proposing actor's current
explicit manager grant in its own tables and returns a boolean result. The request
carries the actor subject because workforce cannot authorize that owner's scope
without it; it contains no target subject, private reason or staff details.
The response carries only proposal/request/hospital IDs and eligibility. IAM
accepts only the response matching its pending request in that tenant, rechecks
target membership and validity, and records `pending_review` or `scope_rejected`
with immutable history. Response order, retries and unknown proposals never
activate or reopen a record. An unavailable owner leaves verification pending.

This preliminary proof only admits a proposal to the review queue. The implemented
decision command requires `iam.assignment.review`, current IAM administrator
membership and a reviewer different from proposer and target. It records an
immutable requested action and enters `pending_decision_scope`, requesting fresh
owner verification for that reviewer. Owner denial or revoked administrator
membership returns to `pending_review`; successful rejection ends at `rejected`.
Successful approval rechecks target membership/expiry and ends at
`approved_pending_activation`, without effective access. The ledger describes
intent; proposal state/history determines the outcome. A proposer with manage
permission can withdraw any nonfinal proposal, including pending activation.
Withdrawal invalidates the request correlation, preventing delayed owner responses
from reopening it. Identical versioned retries are idempotent; competing commands
conflict. Reads recheck current IAM administrator membership as well as permission.

Approval must not reuse intake proof as current authorization. Projection, epoch checks,
product-scope checks and the independent reviewer are still mandatory. The
proposal API and read/history surfaces can be delivered first, but do not close
the approval story or authorize the proposed permission. Legacy access commands
remain unexposed by a new UI while this reviewed path is being implemented.

Commit assignment state/history, scope-epoch advance and outbox together. The
projection worker rereads authoritative state and writes immutable versioned keys
outside a database transaction; only acknowledge with a compare-and-set against
the still-current epoch. Duplicate projection is idempotent. A partial write or
worker crash leaves the epoch unacknowledged; retry completes it. An older worker
can write old keys but cannot acknowledge a newer epoch. Revoked/expired/suspended
assignments cannot be selected by the authority provider, even if old graph edges
remain. A future cleanup removes unreachable old projection keys after retention
and reconciliation evidence; deleting history is not compensation.

IAM unavailable, FGA unavailable, unknown scope, invalid input or stale/missing
projection never allows. Out-of-scope objects return 404, known in-scope actions
without permission return 403 and unavailable authority returns 503. Queue outage
leaves committed commands visible as awaiting projection, not effective access.
No network call is made while a database transaction holds locks.

## Delivery and migration gates

### Membership-only prerequisite

Add the system role `staff_member`, labelled "Staff member (membership only)".
It establishes tenant membership through the existing IAM grant/revoke path,
but receives no declared business permission. It is not an HR job category,
clinical template, hospital grant or staff-account link. An authorized IAM
membership administrator must explicitly grant it; no enrollment, link approval,
posting or startup operation automatically assigns it. Existing role memberships
are unchanged. This avoids requiring a clinical or administrative role merely to
establish the membership precondition for a scoped assignment.

IAM owns membership; workforce retains staff identity and hospital scope. The
existing `iam.member.manage` check controls grant/revoke; the role itself cannot
manage members, inspect the role catalogue, or use permission-guarded business
routes. Membership-only routes still apply their own object checks. Real OpenFGA
tests must prove empty tenant-wide permission resolution, cross-tenant denial,
membership revocation and compatibility with the separate scoped graph primitive.
The legacy membership event ordering limitation remains; this role does not
replace the current-state and epoch checks required by the assignment provider.

This adds a role key to the existing catalogue and member command, with no model,
schema, event-payload or frontend contract change. The catalogue uses string keys;
clients must not reinterpret this as a workforce job category. No live membership
is changed by adding the key. Removing support after assignments exist requires
an explicit membership migration; rollback must not substitute a clinical role.

### Remaining gates

1. Prove the versioned graph primitive with real FGA: tenant/hospital/staff
   isolation, missing membership, exact permission, invalid IDs, projection
   replay, stale versions and dependency outage. This alone exposes no API or
   active assignment. Preserve the legacy model and existing permission tests.
2. Implement IAM assignment/history/epoch storage with RLS, canonical contracts,
   reviewed commands, owner scope validation and bounded reconciliation. Add a
   fail-closed authority provider and wire its projection worker through actual
   bootstrap dependencies. Test crashes and concurrent revision changes.
3. Implement staff-self leave/balance/ledger endpoints and UI through Global API.
   Reuse owner command rules and assert no hospital-wide leave grants are needed.
4. Replace legacy hospital grants through a reviewed, auditable migration; remove
   implicit clinical administrator permissions as the distinct ADR-0044 migration
   slice. No silent reinterpretation of old grants or change to unrelated roles.
5. Run authenticated synthetic browser journeys, full contract and isolation
   gates, query-plan/load checks and downstream lifecycle reconciliation before
   claiming complete Staff Connect.

The simplest alternative—authorizing directly from a reviewed staff link—is
rejected because it creates module-private authority contrary to ORG-AC-08.
Mutable unversioned graph keys are rejected because delayed grant events can
restore revoked authority. New FGA types are additive; rollback to a binary that
cannot perform the new authority checks must disable new consumers, not fall back
to tenant-admin access. Live schema/access migration and rollout need the specific
approval required by the repository rules.

### Projection delivery detail

The next implementation uses a scope epoch keyed by verified tenant, hospital
and subject. Approval creates an immutable snapshot of the fixed template's
permission set and reviewed proposal revision, then advances that scope epoch.
Withdrawal advances the same epoch. State, snapshot, epoch and a projection
request containing only proposal ID commit together. An IAM-owned consumer uses
`AfterCommit` for all OpenFGA I/O; retries reread current IAM state rather than
trusting event content. It projects eligible snapshots in 200-row keyset pages,
then acknowledges only the epoch it read if still current. Other assignments in
the same scope are included because an epoch change invalidates the whole scope's
acknowledgment. A stale worker cannot acknowledge a later withdrawal or approval.
Snapshots retain permission, approval and validity evidence after withdrawal.

The projection foundation initially kept status `approved_pending_activation`;
the later activation/provider slice below implements the IAM activation gate.
Graph delivery alone is still not product access. The provider checks current
membership, validity and epoch/ack around the exact FGA check; products must add
their current staff/link/object checks. Membership removal/re-enrollment is now
covered, while staff lifecycle integration remains open. The
snapshot migration is forward-only; rollback disables new consumers and retains
review evidence. It does not migrate legacy grants or authorize a live rollout.

### Membership incarnation and re-enrollment

IAM owns a durable `(tenant, subject)` membership generation and current role
count. Database triggers update that count atomically for every membership insert
or delete, including onboarding. Adding/removing an extra role preserves the
generation; deleting the final role increments it. The row is retained at count
zero, so re-enrollment cannot reset the generation. Duplicate grants/revokes do
not change it. Membership identity fields cannot be rewritten in place. Final
removal requires an authenticated actor and atomically writes immutable ended-
generation evidence plus an audit entry; scripts without actor attribution fail.

Proposal creation captures the current nonempty generation under a shared row
lock. Owner verification, approval and retries recheck the same generation.
Approved snapshots copy it immutably. Removing the final role advances every
existing hospital-scope epoch for that subject in the same transaction, leaving
old acknowledgments behind. Projection only includes snapshots matching the
current nonempty membership generation; the final scope-epoch compare-and-set
also excludes a concurrent membership removal.
No graph network call runs inside those transactions. Raw stale graph tuples may
become true after re-enrollment; a product must use the current-state authority
provider, never an isolated graph decision.

The migration initializes counts from current IAM membership, but does not infer
historical membership continuity for prior proposals/snapshots. Those retain a
zero legacy binding and are ineligible; fresh proposal and independent review
are required. Generation changes are not approval-history rewrites. Reviewed
proposal status remains evidence of its review, not proof of current access.
No product may consume these projections until the provider, staff/link lifecycle
checks and negative integration tests are complete.

### Activation and current-state authority

Projection activates each successfully written immutable snapshot in a short
owner transaction, preserving its reviewed snapshot version and incrementing the
proposal/history version. The worker rechecks current membership generation,
expiry and scope epoch under locks; it never holds those locks during FGA I/O.
Activation does not advance the epoch again. The scope acknowledgment is committed
only after the current scope has been projected; an `active` proposal with a
missing/stale acknowledgment still cannot authorize. Future-dated active
assignments remain unusable until `valid_from`; expiry denies without waiting
for a background status transition. Active assignments can be withdrawn, which
advances the epoch and retains activation/history evidence. Activation is audited
as the IAM projection worker, not as a new human review.

IAM implements a shared authorization-infrastructure port. The caller supplies
only verified tenant/subject, hospital and a server-declared permission; never a
browser-selected assignment or revision. The provider selects current active,
valid snapshots matching the nonempty membership generation and current
acknowledged scope epoch. It checks exact immutable FGA objects outside database
transactions, then rereads the same version/generation/epoch/validity before
returning evidence for that single request. Allows and state-based denials are
audited. Database, graph or audit failure returns unavailable without a grant.
The request deadline is five seconds, with at most 200 candidate snapshots; an
overflow fails closed rather than truncating the authorization search silently.

The port belongs to authorization infrastructure, not a private cross-product
callback. Product modules consume route-scoped middleware evidence and verify
current staff/link/object state in their own transaction. No workforce route is
enabled solely by implementing this port; wiring the consumer and its owner
checks is a separate required gate. Returned evidence is not a bearer token,
cache entry, clinical privilege or reusable approval proof.

### Route gate and workforce consumer boundary

Routes opt into a declared hospital-scoped permission, naming the hospital path
parameter. Middleware builds the authority request exclusively from the verified
principal and that path value. Browser headers and assignment IDs are not proof.
The route registry retains the permission, scope kind and parameter for contract
review. A scoped grant is bound to that principal, action and hospital in private
request context, and is available only to the registered handler chain.

A legacy tenant permission remains an independent route gate; the product must
still verify its legacy hospital-manager authority. Middleware also resolves a
scoped grant when available, so a caller with both kinds of permissions can use
the appropriate product-owned path. Scoped dependency failure without a legacy
permission returns 503. With a legacy permission, the handler receives failure
state and may proceed only if its legacy owner check succeeds; it cannot treat a
failed scoped lookup as a grant. Missing scoped configuration fails closed.

The workforce consumer must separately lock current actor staff, then current
active reviewed login link, and verify active employment. It must exclude both
the request filer and the linked subject of the target leave. The scoped branch
will gain only target-bound leave request/day/event and consumption-ledger writes,
plus the configuration/target-standing reads needed to decide that request. It
must not set broad hospital-manager access or expose enrollment/profile/payroll
routes. Scope proof and owner link/version evidence belong in the decision audit.
RLS and positive/negative actual leave-decision tests remain required before this
route gate is applied to workforce's business endpoint.
