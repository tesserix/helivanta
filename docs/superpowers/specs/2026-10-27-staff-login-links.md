# Reviewed staff-login links

Implementation design for #1132 and #1053, within #1035/#118. This completes the
reviewed binding prerequisite; self-service leave and IAM scoped assignments
remain required subsequent work. This is not a production approval or deployment
record. Follow ADR-0044, ORG-AC-01/02/04/05/07/08 and the workforce API contract.

## Ownership and authority

Workforce owns the hospital-local staff-to-login binding and its review history.
IAM owns login membership and access assignments. Verify the supplied immutable
subject against the existing `platform.RoleLister` contract and the current tenant;
never query IAM tables, match by email/name, provision an account or grant a role.
Membership verification does not claim the identity provider has enabled login.

The existing verified tenant, hospital manager grant and OpenFGA route permission
are all mandatory. Add `workforce.login_links.manage` for proposal, withdrawal and
revocation, and `workforce.login_links.review` for independent approval/rejection.
Either permission permits private link/history reads, still requiring hospital
manager scope. These are administration actions, not clinical or payroll access.
The current seeded administrator supplies these permissions; no new implicit
clinical or HR grant is introduced. Reviewer must differ from both proposer and
target subject. A subject may propose their own link but cannot approve it.

Linking never grants authority. The future self-service command must additionally
check an approved IAM assignment, current membership, active staff/link state and
exact staff scope. Existing tenant/hospital grants remain unchanged. Do not expose
self-service by treating a binding as a new private grant vocabulary.

## Records and transitions

Canonical input/output models live in `contracts/workforce-login-links.json`,
with generated Go/Zod models. All requests enter Global API under
`/v1/workforce/hospitals/{hospital}/staff/{staff}/login-links`.

- `PUT /{link}` proposes `{subject, reason}` with a client UUIDv7 command ID.
- `POST /{link}/decision` accepts `{expected_version, decision, reason}`; decisions
  are `approve`, `reject`, `withdraw`, `revoke` and use the permissions above.
- `GET /` and `GET /{link}/history` are bounded cursor pages with `no-store`.

Each link has tenant/hospital/staff/link IDs, immutable subject/proposer/proposal
reason, status, version, created/updated timestamps and last actor/decision reason.
Each immutable history version records its status, actor, reason and timestamp.
Reasons and subjects remain protected HR records; audit/outbox payloads contain
only scope/resource IDs, version and state. Sensitive list/history reads are audited.

States: `pending -> active|rejected|withdrawn|revoked`; `active -> revoked`.
Withdrawal is proposer-only. Revocation requires manage permission and a reason;
rejection requires a reason. Every decision requires a nonempty reason for a clear
review record. Inactivation revokes pending/active links with the fixed reason
`Staff made inactive`. Reactivation restores no link; a fresh proposal and review
are required. Terminal records never change and are never deleted.

At most one pending/active link exists for a staff member and at most one for a
subject within a hospital. The same login may have separately reviewed bindings
in different hospitals. Bind only active staff. Staff and link rows use composite
tenant/hospital foreign keys and forced RLS. Database constraints/triggers enforce
uniqueness, immutable proposal/history, independent decisions and legal transitions.

## Concurrency, recovery and disclosure

Lock staff before link rows for every transition; staff inactivation takes the
same staff lock. Serialize races with partial unique indexes and expected versions.
Unchanged proposal retries, after current candidate-membership verification, return the same record without a second history/outbox
entry. Conflicting reuse returns 409. Retrying the immediately committed decision
with the same actor, expected version, decision and reason returns that result;
approval retries also require current candidate membership, so an identity outage
or membership removal can deny a retry even after the original command committed.
The authorized history endpoint remains the recovery lookup in that case;
other stale/terminal requests return 409. Refetch history after an ambiguous timeout.
No network call occurs inside the database transaction. Check candidate membership
before proposal/approval, then recheck staff/grant state in the write transaction.
Membership races cannot grant access because a link carries no authority.

State, private history, minimized audit and outbox commit together. Inactivation
revokes links in the staff update transaction. A deferred database guard refuses
an inactive staff member with a pending/active link even if a caller misses the
application update. Event publication uses the existing outbox; broker downtime
does not erase the committed state. Replays do not create grants or duplicate history.

404 hides foreign/missing staff, links and candidate membership; 403 denies missing
permission or self-review; 400 rejects invalid input; 409 reports duplicate/stale
state. An unavailable membership dependency returns 503, never an assumed member.
Opaque subject strings reject whitespace and OpenFGA wildcard/userset syntax.
Do not log request bodies, subjects or reasons in new error paths.

## UI and verification

The workforce directory opens a scoped Account link panel. Show pending/active/
terminal state, proposer and review history; only eligible actors see actions.
Label the immutable login identifier as Account ID; explain that linking does not
grant access. Preserve command IDs across retries, clear selection on tenant/
hospital changes, and show accessible loading, empty, error and conflict states.

Test with real PostgreSQL and OpenFGA: review separation, inactive staff, foreign
scope, missing permission, membership outage, duplicate IDs, competing links,
stale decisions, revocation, inactivation/approval races, history immutability,
RLS, audited reads and minimized outbox/audit. Test the client/panel with generated
models, retry preservation and scope changes, then the authenticated browser/API
journey through Global API. Passing backend tests alone does not complete this slice.

## Capacity and delivery

Reuse the repository architecture sizing assumptions; no measured Staff Connect
SLO exists. Verification uses a synthetic 1,000-staff hospital, one current link
per staff, the platform maximum of 200 rows per API page (client default 50) and
at most 8 KiB command bodies. No new
service, queue, cache or polling loop is introduced. Indexed hospital/staff/subject
lookups and history keyset scans are the expected access paths. Larger retained
history requires query-plan evidence before a performance claim.

Use an additive forward-only migration with bounded lock/statement timeouts.
Rollback preserves binding/history records; do not delete or reverse accepted HR
evidence. After links exist, older binaries attempting inactivation without link
revocation fail the database guard; deploy a forward fix or retain the new writer.
Live migration/rollout and any access-model migration require their own approval.
