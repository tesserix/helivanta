# Staff reviewer governance

Status: candidate in PR #1669; not merged or deployed. The current implementation
covers owner grant admission, retained creation/decision receipts, current auditor
review and an owner inbox. Effective Workforce reviewer consumers, acknowledgements
and UI remain unfinished; remote decision acceptance is still pending. Account initiation alone does not complete Staff onboarding.

IAM owns the governance ledger. Workforce owns the hospital/independent employer
roots and current hospital count. Native identity remains with the identity owner.
Canonical Go/Zod models derive from `contracts/staff-governance.json`; regenerate
with `node scripts/generate-staff-governance.mjs` and check with `--check`.
The [implementation design](../superpowers/specs/2026-11-13-staff-governance-bootstrap.md)
records ORG-D7, all remaining lifecycle consumers and operational assumptions.

## Candidate operations

Browser calls enter Global API with the IAM destination. Backend paths below
are `/v1`; preserve the Global API client's existing path mapping.

| Operation                           | Current authority and outcome                                                                                                                                                                                                                                                      |
| ----------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `PUT /v1/iam/governance-grants/:id` | `iam.governance.manage` plus a current IAM owner and credential watermark. A canonical UUIDv7 command admits a separately named recipient, subject to native eligibility, current membership incarnation and exact Workforce scope. Returns 202 with an original creation receipt. |
| `GET /v1/iam/governance-grants/:id` | `iam.governance.read` plus current owner or tenant-auditor source proof; returns retained origin with current stored status/version. It does not report effective route or product access.                                                                                         |
| `GET /v1/iam/governance-notices`    | `iam.governance.read` plus current owner proof; bounded cursor list limited to that owner's own recipient subject.                                                                                                                                                                 |

`PUT /v1/iam/governance-grants/:id/decisions/:command` requires
`iam.governance.review` and a current owner or tenant-auditor source. Both IDs
are canonical UUIDv7. The body contains observed `expected_version`, supported
`decision` (`approve`, `reject`, `revoke`) and reason only. The expected version
must leave room for its safe integer successor. A 202 decision receipt identifies
the authenticated actor and resulting version; it does not replace the current
head or imply product access.

Every operation refreshes authentication/route admission and requires exactly one
`X-Helivanta-Expected-Tenant` and `X-Helivanta-Expected-Subject`, matching the
verified session. They detect an unintended account/workspace switch and never
select authority. Responses are private and non-cacheable.

Creation accepts only subject, expected membership epoch, fixed template key,
scope kind, optional exact root ID, reason and effective interval. The body is
bounded to 4096 bytes. Duplicate/unknown/case-alias keys, actor/tenant/role fields,
nulls, invalid UTF-8, unsafe integers and malformed or precision-losing instants
are refused. Reasons have a shared 500 UTF16-unit bound and no control characters.
No contact, password, proof document or native organization is supplied.

The target must already have ordinary tenant membership and be an active verified
tenant-owned human. An owner cannot issue to themselves or another current owner.
Native eligibility is read outside database locks. Creation then rechecks owner,
credential, membership incarnation, exact employing root and topology under a
single guarded database command. The native read is an observation, not a
cross-service atomic transaction or proof of subsequent login.

One current owner and at most one hospital admit `provisional_active`, with
`single_owner_provisional` provenance. Multiple owners admit `pending_review`,
with `independent_review` provenance. A tenant with multiple hospitals and fewer
than two current owners is denied. No synthetic independent approval is recorded.
Second-hospital creation's owner-count admission guard remains unfinished; the
Workforce topology gate alone only serializes observations against root writes.

A command's canonicalized intent and original receipt are retained immutably.
Repeating the same command and intent returns the original 202 receipt; changed
intent or issuing actor returns 409. Receipt replay does not repeat native
provisioning, reissue a grant, or claim that its authority is still effective.
Creation history remains distinct from the mutable current head.

## Independent review and source lifetime

Issuer and recipient cannot approve/reject their own grant. A current owner may
withdraw their issued grant through `revoke`; a recipient cannot self-review.
Approval moves pending/provisional state to `active`, rejection to `rejected`,
and revocation to `revoked`. Decisions and original receipts are immutable; a
repeated command returns its original decision after later head changes. Changed
intent, actor, command namespace or observed version conflicts. Creating a new
command never bypasses a stale version.

Auditor authority comes from a current tenant-scoped
`iam.governance_auditor.v1` grant and current managed IAM membership incarnation.
Provisional authority requires exactly one current owner matching its original
issuer; active authority requires retained independent approval. Credential,
interval, owner/topology and source status are rechecked under retained locks.
The command deadline cannot outlive the auditor's source grant.

Source and target grant IDs are locked in sorted order before actor membership
and Workforce topology. This preserves revocation ordering and avoids inverted
cross-auditor locks. Private source/lock purposes disclose no parent payload to
the caller before authority validation. Source table RLS avoids recursive
self-lookups. Approval checks target native eligibility outside database locks,
then rechecks source, exact target root, membership incarnation and version.
Reject/revoke and retained receipt recovery do not re-enable or reprovision the
target account.

A retained decision records explicit owner/auditor provenance and, for delegated
review, the exact source grant/version and actor membership epoch. It does not
fabricate Staff or employment identifiers. A decision, head transition, new
notices for every current owner, audit and any routing projection change commit
together. Revocation denies from current source immediately; remaining future
or current unexpired grants retain the coarse routing identity. Generic role
creation/removal cannot manage that identity outside its source workflow.

## Atomicity, scope and routing

Grant creation, one durable notice for each current owner, audit and any managed
routing-membership projection intent commit together. Inbox evidence means a row
was recorded, not that an email was sent or read. Notice acknowledgement is not
yet exposed.

The single fixed `governance_delegate` membership is managed by this ledger.
The generic membership grant API returns 409 for it. Existing member outbox
consumers/reconciliation project this coarse routing identity asynchronously.
No direct native administrator role is assigned. A routing tuple or stored
`provisional_active` status is not effective hospital, HR, clinical, pharmacy,
stock or financial authority. Actual consumer guards must check current exact
source, interval, membership incarnation, credential and product prerequisites;
those consumers are not implemented by this candidate admission slice.

IAM retains an owner-count gate before target membership and Workforce topology
locks. Workforce root insert/update/delete statements take the exclusive topology
gate before row locks; the source provider retains its shared counterpart through
the consuming callback. Missing/duplicate canonical providers, unavailable
sources, context/tenant/database escape or an expired command deny.

Missing permission/current owner returns 403. Invisible grant or employer returns 404. Changed workspace, intent or source version returns 409. Invalid body is
400; a credential watermark denies with 401; unavailable authority/source or a
command deadline is 503. Preserve v1 `{error,message}` envelopes; request IDs
remain in headers.

## Client integration

`@helivanta/api` exposes `issueGovernanceGrant`, `readGovernanceGrant`,
`decideGovernanceGrant` and `newGovernanceCommandId`. Callers retain the command
ID and exact input for lost-response recovery. A recovered issue receipt remains
the original version; read the current head separately.

Requests use the account-scoped Global API client and cancel on workspace
transitions. The SDK validates canonical input before network I/O and binds
responses to the requested tenant, grant and decision command. Issue responses
also bind the issuer and original input; decision responses bind the reviewer,
input and successor version. Instant comparisons retain microsecond precision
and accept equivalent UTC/offset representations. These functions do not add a
governance screen or authorize Workforce consumers.

## Release and remaining acceptance

Candidate migrations are `0102_workforce_governance_topology` and
`0047_iam_governance`. Neither has been approved for live application. Before
rollout, complete source consumers, owner-count admission,
ledger/role admission guards, frontend, remote PostgreSQL/FGA/race/recovery tests
and tested rollback. Retain identities, original receipts, notices and decisions
through rollback; never erase evidence to retry onboarding.

The real PostgreSQL governance suite passed in CI run `43377058047`, including
independent decisions, stale source rejection between preflight and commit,
original receipts, owner-notice privacy and opposing auditor revocations. Its
first decision test returned 404 before implementation. The broader Go, web,
supplier and browser suites are still running; this evidence does not establish
Workforce consumer integration. The actual preserved tenant still needs recipient-operated
private setup, real distinct reviewers and positive/negative/revoked staff and
pharmacy-access verification before this workflow is complete.
