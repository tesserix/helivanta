# Workforce consumption of reviewed leave authority

Extends the accepted [scoped Staff design](2026-10-27-scoped-staff-access.md),
ORG-AC-01/02/04/05/06/07/08 and ADR-0044. Tracks #1132, #1053 and #434; this does
not close their wider acceptance scope. ORG-D2/D5/D6 remain open.

IAM already owns reviewed `workforce.leave_reviewer.v1` assignments, exact graph
snapshots, membership generations and acknowledged scope epochs. Workforce owns
staff employment, reviewed login links, leave decisions and the leave ledger.
Current production Workforce decision routes still require a tenant permission
and legacy hospital-manager grant. A reviewed assignment therefore cannot yet
complete the user-requested independent reviewer journey.

## Required behavior

An active, reviewed, hospital-local linked staff member with current IAM leave
review authority can approve or reject another staff member's pending request in
that hospital. They receive no staff-directory, payroll, enrollment, hospital
administration or clinical authority. Preserve the existing manager path and all
leave eligibility, balance, year closure, state, expected-version, event, audit
and exact-replay rules. The existing own-filer and linked-self exclusions remain
mandatory before both decisions and retries. Missing authority or a stale link
must never be repaired by creating a legacy manager grant.

The existing decision route must opt into the shared hospital-scoped gate, with
the hospital path parameter and `workforce.leave.approve` declared by the server.
Browser-supplied assignment identifiers or headers cannot create evidence. A
scoped authority outage denies the scoped path; a legacy caller may continue only
after the existing product-owned manager check succeeds.

## Transaction and disclosure boundary

Before enabling scoped row access, resolve only the actor's own active reviewed
link, lock its staff row for share, verify active employment, then reread and lock
the current link. Link mutations and staff inactivation already lock that staff
row for update; a competing revocation must either precede the decision and deny
it, or wait for the already-authorized decision to finish. Do not treat a previous
link read as current after obtaining the staff lock.

Bind transaction-local scoped state to the verified tenant, subject, hospital,
actor staff/link revision and exact target staff/request. Keep IAM evidence local
to this request. Do not set or emulate hospital-manager access.

The graph check runs before the command transaction. IAM then checks current owner
state and locks the proposal, membership generation and acknowledged scope epoch
in the same transaction that commits the product decision. Workforce joins through
the private tenant-command context and never queries IAM tables. Product staff,
link and object eligibility is checked inside that transaction. No network call
runs under those locks. The command has a five-second maximum budget, cancellation
at assignment expiry and a final SQL validity check before commit.

A shared subject advisory transaction lock protects the fresh credential watermark
read, including when no watermark row exists. The central IAM revocation writer
takes the matching exclusive lock. Revocation and product commit therefore have a
serial order. Revoked credentials cannot fall back to legacy business grants.
These guarantees cover this guarded consumer; they do not fix the separate normal
session cache/IdP-disablement gaps described in the lifecycle research.

Local regressions verify atomic rollback after product writes, revoked credentials,
revocation waiting for the guarded command, and obsolete membership receipts.
The actual Workforce endpoint test verifies rollback of the decision, leave ledger
and success audit when its guard fails after the product callback.

The combined Global API/IAM/PostgreSQL/NATS/OpenFGA test now passes with signed
sessions and real permission/membership resolution. It proves independent link
and assignment approval, zero hospital grants for the reviewer, one consumption
and success audit on exact replay, self-decision denial, foreign hospital/staff
denial, protected enrollment/payroll and empty unauthorized directory pages, link
revocation and fresh rebind. A controlled database-lock race proves withdrawal
commits before a waiting guard and denies the decision. A second race proves
assignment expiry while waiting for the product ledger lock returns 403 and leaves
the request pending with no consumption or success audit. The first expiry run
returned 500; preserving the guard's cancellation cause fixes that response.
Reviewer discovery/queue/UI and live two-person acceptance remain release gates.

Add forward-only RLS policies for the exact operation, preserving legacy policies:

- Actor verification can read only that subject's active link and associated
  staff standing, never the link history or another subject's links.
- Scoped decision reads/writes name one request and target staff. Request days
  are read-only; events are append-only, attributable and bound to that request.
- Ledger access is restricted to that target's relevant leave type/year; inserts
  can only consume the approved request, with existing ledger guards intact.
- Leave configuration, assigned structure, year status and employment standing
  are owner-authorized machine reads required for this decision. They do not
  become general HR read or mutation permissions.
- Any returned roster clashes must be restricted to the target and leave window,
  with only their associated roster metadata visible. Do not return an empty
  array to conceal an unavailable dependency or unauthorized disclosure.
- The audit records the exact IAM assignment revision/epochs and actor link
  revision as well as the existing decision evidence. Outbox publication remains
  atomic with the decision, consumption, event and audit.

Review policy recursion and query bounds against actual SQL. Do not widen the
shared `workforce_staff_allowed()` function: that would confer unrelated module
authority. No IAM table may be read by Workforce. No graph or other network call
may run while Workforce holds a database transaction.

## Usable reviewer surface

The shipped slice must include a usable reviewer queue through the Global API.
It must discover only the actor's current linked/authorized hospital scopes,
offer bounded pending-request reads and an exact-version decision form, and make
own-request exclusion, loading, empty, failure, changed workspace and uncertain
retry states explicit. A reviewer must not need the administrative Staff directory
to discover a target ID. Canonical owner-generated schemas must cover any added
queue/discovery contracts. New read routes require the same current authority
and disclosure audit as the command. Endpoint shapes and canonical models will be
fixed after tracing the existing owner query and pagination implementations.

## Evidence required before merge or release

1. A real-DB decision regression must fail because a properly linked, scoped-only
   reviewer has no legacy manager grant; it must then pass without adding one.
2. RLS tests must prove another tenant, hospital, staff/request, unrelated ledger,
   enrollment field surface, directory and payroll remain inaccessible.
3. Own-filer and linked-self decisions/replays deny; active-link revocation,
   inactive employment, assignment withdrawal/expiry, stale generation/epoch and
   dependency failure deny without changing the request or ledger.
4. Real Global API/IAM/Postgres/NATS/OpenFGA tests must exercise the actual
   Workforce decision endpoint, not the earlier test-only authorization route.
5. Browser tests must cover Staff Admin enrollment/link/proposal, independent
   administrative approval, reviewer queue/decision and forbidden scope, followed
   by live synthetic UI QA using the private credential JSON and user handoffs.

Production migration, security-sensitive access grants and any legacy-grant
migration remain separate named approval actions. Full Staff Management also
requires self-service, supervision/postings, departments and the broader delivery
matrix; this slice must not be labelled as that entire goal.


## Next implementation: reviewer discovery and queue

This is the next development boundary, not an implemented API. Reuse the parent
planning envelope of 100 hospitals × 1,000 staff and 200 peak authority checks/s.
At one leave request per staff/month, requests reach roughly 1.2 million after
12 months and 3.6 million after 36 months, before events/audits. Plan ten reads per
decision, 8 KiB decision bodies, 100 queue rows/page and 64 KiB response pages.
The provisional p99 300 ms / 99.9% targets are not measured commitments. Discovery
defaults to twenty candidate links and caps at twenty and allow no more than four
concurrent graph checks. Measure typical one-to-four-hospital bindings before
claiming performance. Reuse PostgreSQL/FGA; no new infrastructure or cache.

Workforce should own canonical `contracts/workforce-leave.json` models for
membership-gated `/workforce/leave-review-scopes` and scoped-hospital queue/detail
reads. Discovery derives only the caller's active reviewed link candidates, then
requires current IAM authority and locked active employment/link validation before
returning a hospital ID/name. Candidate collection alone is not authority. Share
the same infrastructure `ScopedAuthorizer` through platform dependencies; never
import IAM or read IAM tables from Workforce. Graph calls run outside database
locks; disclosure runs inside the existing IAM guard and product actor locks.

The queue returns only pending requests the reviewer can decide, excluding linked
self and original filer. Use keyset pagination and minimal target display,
request/target identifiers, dates, unit, amount and expected version. Release
private reasons only through the exact-target, purpose-bound detail. Keep general
Staff directory, enrollment, payroll, cancellation, revocation and request-history
authorization unchanged. Extend shared route registration for scoped GET/list
semantics with exact parameter validation, membership checks and registry metadata;
do not bypass the permission context with an unguarded route.

Dedicated read RLS must allow only this purpose and retain exact-target decision
write policies. Queue/detail reads are audited in the guarded transaction. No
queue context may edit staff, update unrelated requests, mutate the ledger or
release files. IAM/FGA/PostgreSQL are critical dependencies; unavailable authority
must fail the page instead of fabricating empty or partial success. Discovery's
cursor describes the bounded candidate scan; an empty filtered page may still
have more candidates. Bind cursor scope to verified tenant/subject/filter.

The UI must discover usable scopes without a directory lookup, provide an explicit
hospital selector, bounded pending queue and approve/reject form, clear workspace
state on change, preserve command/version/content on uncertain retry, and show
loading, empty, unavailable, denied and stale-version states. Reject requires a
reason. Browser, keyboard/focus and live independent-reviewer acceptance remain
required. Extend unreleased migration `0036` only while it has no production
application; retain evidence and fail closed on application rollback. A manager
grant is rejected because it exposes unrelated data; a direct IAM-table join is
rejected because it violates ownership. Existing database/FGA resources carry the
incremental query cost; query plans and page-size/failure measurements are release
gates. Live migration and rollout still need named approval.


Discovery is now implemented in development. The existing leave contract/generator
owns its hospital ID/name model. Platform `FilteredListRoute` owns bounded scanning,
probe trimming, caller/route-bound continuation and filtering, including empty
filtered pages with further candidates. IAM's same authorizer instance is provided
through platform dependencies. Workforce shares one locked actor/link check with
the decision path and audits each returned scope. Migration `0037` builds the
subject/status/keyset index through the existing concurrent-index mechanism; no
live application is established. The actual signed-session gateway test covers
link-without-assignment, active scope, link revocation, credential cutoff,
assignment withdrawal and expiry discovery. Queue/detail and UI implementation
and local browser evidence are recorded below and in the Staff acceptance ledger.
Retained-history measurements are recorded below; they do not establish the
provisional production latency target.


## Queue/detail and UI implementation update

Queue and detail are implemented in development through shared scoped GET/list
registration. The generic list envelope remains cursor-paged. The separate
single-target detail is classified with the same explicit bounded-resource
exception as the existing request detail; no collection-pagination rule was
removed. The detail retains the existing single-year day bound and maximum 1,000
roster clashes. A distinct read-only RLS purpose admits pending non-self,
non-filer requests and necessary display/day/roster data without granting mutation
or private profile access. Guard failure rolls back disclosure audits. All scoped
GET/POST routes now reject stale workspace assertions before owner work.

The Review leave UI and Staff navigation entry are implemented. Nine component
scenarios cover a permission-free reviewer, protected detail, reject-note
validation, approval, exact uncertain retry with frozen fields/close/queue reset,
empty filtered-scope continuation, unavailable authority, four access/version
refusals, and workspace-transition clearing. These are component tests; live
browser acceptance and independent administration setup remain open.

Migration `0038_workforce_leave_review_queue_index` adds the concurrent
hospital/status/keyset index. Migrations `0036`–`0038` are not deployed. Current
main must still be integrated and the resulting release revalidated. No broad
Staff, identity/background/card or employment lifecycle story is complete from
this consumer alone.

## Retained-history owner-query measurement — 2026-10-28

`TestLeaveReviewRetainedHistoryPlans` captures the actual discovery and queue SQL
from the owner endpoints, then runs `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` with
the corresponding application read contexts. The disposable PostgreSQL 16 fixture
retains RLS, constraints, request days/events and attributed command transitions:
100 historical staff plus the reviewer and seed staff, 100,000 cancelled requests, 100,000 day rows, 200,000 request events,
1,000 withdrawn login-link proposals, one current reviewed link and 52 pending
requests. The endpoint returns 50 queue items plus a two-item tail.

| Query | SQL rows | Planning ms | Execution ms | Shared hits | Shared reads |
| --- | ---: | ---: | ---: | ---: | ---: |
| Hospital discovery | 1 | 0.173 | 0.150 | 6 | 0 |
| Queue first page, including probe | 51 | 4.000 | 6.241 | 1,520 | 0 |
| Queue continuation | 2 | 4.271 | 3.255 | 1,469 | 0 |

Discovery used `workforce_login_links_review_discovery`; both queue pages used
`workforce_leave_requests_review_queue`. The test checks these plans, returned
row bounds and fewer than 5,000 shared hit/read blocks per query. Retained
cancelled history was not scanned by the queue index. The race-enabled test
passed in 397.02 seconds; fixture construction dominates that duration.

Run explicitly from `backend/` with a reachable Docker engine:

```sh
go test -tags workforce_perf -race ./internal/modules/workforce \
  -run '^TestLeaveReviewRetainedHistoryPlans$' -count=1 -v -timeout 15m
```

The separate build tag keeps this large fixture outside routine unit-test runs.
This is a single local warm-cache owner-query measurement, not a p95/p99 result,
concurrent throughput test or production SLA. The fixture uses the existing test
scoped-authority implementation and real OpenFGA membership; it does not measure
the full IAM guard or graph under load. Real signed-session authority integration
and browser acceptance have separate evidence. Production migration rollout and
fresh deployed browser verification remain open.

The initial performance fixture explicitly refreshed table statistics with
`WithAdmin`; the architecture suite correctly rejected that unallowlisted call.
The call was removed, without weakening the rule. The measurements above are
from the passing repeat with tenant-scoped fixture commands and ordinary database
statistics only. The full architecture race suite then passed in 36.192 seconds.
