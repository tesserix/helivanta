# Workforce posting lifecycle cutoffs

Status: implementation in progress. Local command/receipt routes, candidate
Workforce 0084/IAM 0027 and cutoff-aware legacy refusal are implemented. The
reviewer UI integration is implemented locally; normal-role browser acceptance
and operating consumers remain open.
Administrative discovery, exact context/history reads and the normal cutoff
workspace are implemented locally with API/component evidence; normal-account
browser acceptance remains open. No production migration or rollout has occurred.
The subsequent local candidates implement explicit lifecycle-review context,
version/deadline-bound positive decisions, normalized immutable review evidence
and SDK verification/replay. The reviewer UI now displays effective lifetimes and
binds approvals/recovery to the examined observations. Actual normal-role browser
acceptance, historical proof presentation and operating-consumer enforcement remain open. See the exact selector/envelope in the
[owner contract](../../api/workforce-postings.md).
Builds on [ADR-0058](../../adr/0058-retained-posting-cutoffs.md) and all five
[reusable Staff requirements](2026-11-02-reusable-staff-foundation.md). This work
does not settle standalone employer scope (ORG-D8), statutory pharmacy policy,
clinical privileges or department-scoped IAM assignments.

## Ownership and source findings

Workforce remains the only Staff/posting owner. Current approval validation is
in `backend/internal/modules/workforce/posting_chain_approval.go`; the current
named-chain view is in `posting_chain_context.go`. Before this candidate, neither
read a retained posting cutoff because that resource did not exist. The candidate
source validator now reads its exact scoped cutoff and refuses legacy review when
one is retained. `postingOwnerContext` reads the exact scoped Staff row without requiring
its active status, which permits ending stale target sources while the separate
`CurrentStaffActorGuard` still requires an eligible administrative actor.

The Global API routes `/v1/workforce/*` to its configured Workforce destination;
it does not need another owner or a client bypass. Canonical posting models live
in `contracts/workforce-postings.json`; IAM template/catalog definitions live in
`contracts/staff-access.json`. Generated Go/Zod/SQL are outputs, not edit targets.

## State and commands

For one exact approved posting revision, retain a current cutoff version plus
immutable commands. Each contains command ID, exact scope and approved revision,
expected/new cutoff version, mode, normalized request hash, actor, reason,
attestation, effective cutoff and recorded instant. No arbitrary JSON authority
or copied Staff/department fields are introduced.

| Prior state                                     | Command                                        | Result                                |
| ----------------------------------------------- | ---------------------------------------------- | ------------------------------------- |
| Approved, no cutoff                             | Immediate                                      | Retain server-owned cutoff/version 1  |
| Approved, no cutoff                             | Future scheduled cutoff                        | Retain selected instant/version 1     |
| Approved, scheduled cutoff                      | Strictly earlier scheduled cutoff              | Append next version                   |
| Approved, scheduled cutoff                      | Immediate before scheduled cutoff              | Append next version                   |
| Any committed command                           | Exact authorized replay/recovery               | Original receipt; no new cutoff/event |
| Any cutoff                                      | Extension, withdrawal, restoration or deletion | Deny                                  |
| Wrong approved revision or stale cutoff version | New command                                    | Conflict; no side effects             |
| Pending/rejected/withdrawn proposal             | New cutoff                                     | Conflict; no side effects             |

Immediate input omits the instant or supplies null; scheduled input requires an
explicit non-null instant strictly after the command's server observation. Reject
unknown fields, controls/empty reason, invalid versions/IDs, modes and incompatible
instant combinations. Normalize instants to PostgreSQL microseconds before
fingerprinting. The same immediate request must replay across time without
substituting a newly generated instant. An elapsed scheduled cutoff cannot be
extended or revived; retain its history. Cutting off before a future posting
starts is allowed: that appointment will never become effective.

Use immutable receipt recovery scoped to the original command actor and exact
request intent under current cutoff authority. A timeout leaves the command
unknown; GET receipt, then exact PUT retry after 404. A different command at the
same expected version can win only once. Failed commands leave no cutoff, actor-
use evidence, private audit or outbox publication committed.

## Compatibility and live checks

Preserve v1 approval receipts, status enum, nominal intervals, stored source
deadlines and management-only SDK helpers. Add explicit lifecycle-context models,
SDK helpers and selectors. The selected node and each ancestor carry their
observed cutoff version/instant; computed eligibility is separately named.
Validate ordered linkage, unique nodes, exact hospital/Staff/revision, clock
bounds and the earliest deadline. Approval binds that observation and rechecks
all owners while holding existing locks. A read never reserves future authority.

Legacy positive approval/context on a chain with a retained cutoff must return a
defined conflict requiring lifecycle review, rather than accepting a client that
did not examine that cutoff. Independent rejection, immutable history and exact
receipt recovery remain available. Supervisor discovery remains a hint; filter
known ended candidates within its existing bounded scan and never represent a
scheduled cutoff as the old stored source deadline. Its lifecycle-aware opt-in
projection must expose the new version/instant distinctly.

Ending an ancestor invalidates descendants' live authority at that instant.
Do not rewrite every descendant or delete their approvals. Current checks traverse
canonical references and compute the minimum deadline. A new source/identity,
rehire, new login link or restoration of old identity fields cannot revive the
cut-off posting. Transfer is a new reviewed appointment plus ending the old one;
this slice does not claim a multi-posting atomic transfer command.

## UI acceptance

An admitted administrator discovers a hospital and approved posting, examines
current cutoff/history, enters the required reason and source attestation, then
selects immediate ending or an explicit future instant. Show the local timezone
and resolved instant before submission. Do not present a cutoff as a Staff exit,
account deletion, replacement appointment or permission grant.

The UI retains the exact command, rationale, mode/instant and examined versions
across unknown results; it blocks navigation while unresolved and offers receipt
recovery/exact retry. Reload shows the persisted cutoff/history. Account/scope
change or revoked admission clears private panels and aborts in-flight requests.
An independently authorized reviewer sees the ancestor cutoff in lifecycle-aware
context and cannot approve using an old observation. Verify these normal UI paths
with separate synthetic admin/reviewer identities; do not substitute a DB rewrite
or injected browser token for that acceptance.

## Required proof and release boundary

Use actual Global API → owner → non-bypass-RLS Postgres/IAM/OpenFGA tests for
positive commands, foreign/sibling denial, missing capability, stale actor, stale
target ending, exact replay, changed-intent conflict, simultaneous writers,
approval-first/cutoff-first races, scheduled deadline crossing, ancestor cutoff,
history preservation and audit/outbox faults. Observe RED before implementation;
mutate the cutoff guard and prove the relevant assertion fails after GREEN.
Use controlled clocks/barriers rather than sleeping to assert expiry behavior.

Check full affected owner/API/architecture suites, Go vet/build/lint, canonical
generators/checks, registry/inventory parity, full affected SDK/shell checks and a
production shell build. Explain indexed lookup plans on representative data.
Persist exact migration IDs/hashes and release evidence. Browser failure is a
recorded acceptance gap, never a pass.

The owner cutoff workflow is a necessary vertical slice. It does not complete
Pharma operating enforcement: each actual product command must consume the
canonical live posting/actor guard and its own unit binding, credential,
designation and assignment policy. Record source-ending denial inside that real
consumer before accepting it. No production activation, release or whole-Staff
completion follows from this design document.
