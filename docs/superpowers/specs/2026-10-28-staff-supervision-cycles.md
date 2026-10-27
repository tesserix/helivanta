# Acyclic hospital-local Staff supervision

Status: implemented and verified locally following a reproduced UI defect; release pending.
Related coverage: #1035, #417, ADR-0015, ADR-0044 and ORG-AC-01/02/04/05/08.
This does not settle ORG-D2/D5/D6 or implement organization-wide authority.

## Evidence and ownership

The synthetic employee already reported to the synthetic reviewer. Through the
Staff Admin enrollment UI, setting the reviewer's supervisor to that employee
succeeded. Read-only SQL confirmed both links persisted. The reviewer link was
then cleared through the UI; read-only verification found no remaining two-person
cycle. No real employment or access decision was made.

Workforce owns the current hospital-local enrollment supervisor reference.
`enrollment.go` rejects self-reference; `enrollment.sql` enforces same-hospital
staff foreign keys and self-reference exclusion. Neither checks longer cycles.
The enrollment command locks only its own staff identity, so checking ancestors
without a shared lock would still allow concurrent A→B and B→A writes.
Consumers remain the existing enrollment UI/API and authorized HR projections.
A supervisor reference never grants IAM, approval or clinical authority.

Canonical enrollment JSON remains `contracts/workforce-enrollment.json`; this
change adds no wire fields or endpoints. Consult Workforce/HTTP/schema/Global API
contracts, organization access knowledge and backend standards. Generated route
inventory must remain aligned when the migration is registered.

## Requirements

- Reject direct and indirect circular reporting chains within the verified tenant
  and hospital, including concurrent opposite-edge writes and a new edge entering
  a pre-existing corrupt cycle. A directory staff record without an enrollment
  profile is a valid terminal node with no recorded supervisor.
- Preserve current permissions, explicit hospital manager authority, RLS, expected
  versions, exact unchanged retries, immutable audit and atomic outbox behavior.
- A failed cycle edit leaves the entire enrollment aggregate, version, audit and
  outbox unchanged. Do not disclose another staff member's private profile or the
  reporting path in errors.
- Allow a supervisor to be cleared to repair a bad chain. Preserve historical
  evidence; never silently rewrite existing relationships during migration.
- Enforce the invariant in PostgreSQL as well as through the supported command.
  Keep tenant/hospital predicates explicit and forced RLS active.

## Design and capacity

Use the existing modular monolith and PostgreSQL. Planning scenario only:
100 hospitals × 2,000 staff = 200,000 current profiles, with up to two enrollment
writes/s per hospital and predominantly reads (at least 90%). The existing HTTP
aggregate limit is 32 KiB. At 12/36 months the guard adds no per-write history
beyond existing audit/outbox; current graph storage remains one edge/profile.
Target the guard's local worst-case 2,000-node traversal below 250 ms and the
synthetic write below one second, then report measurements; these are evaluation
budgets, not a new production SLA. Existing service availability commitments
remain unchanged. No additional infrastructure or paid dependency is proposed.

Add one owner SQL lock function keyed by tenant/hospital, with a five-second
lock timeout. The enrollment handler takes it after authorization and before
its existing staff advisory/row locks. The database constraint trigger takes the
same transaction lock before validating an inserted/updated profile. Supported
commands therefore use hospital → staff lock order. PostgreSQL aborts arbitrary
conflicting direct SQL transactions rather than permitting an invalid graph.

A recursive query traverses only the proposed supervisor's ancestors, using
`UNION` over stable staff/supervisor pairs to terminate even on corrupt cycles. Reject when the
subject is reached, or when the visited component has no terminal node. A missing
profile or a null supervisor terminates a chain. Lateral one-row lookups bound each hop to the composite primary key; the row
limit follows key uniqueness and prevents a hospital-wide join rescan. Scoped
lookups to the owning tenant/hospital; no cross-module table read or network
call occurs while locks are held. This avoids an unbounded recursive loop and
quadratic path-array accumulation. All work remains bounded by the hospital's
current profile count and the request/database deadlines.

Use an additive forward-only Workforce migration for functions/constraint
trigger. Existing rows are not automatically repaired. Before live rollout,
perform a privacy-minimized owner audit and resolve any existing cycles through
attributed HR commands. The live migration requires named approval. Retaining
the guard on application rollback preserves correctness; old clients receive
an existing check-violation response. Removing the guard is not an automatic
rollback step. No destructive down migration is supplied.

Map the named acyclic constraint to a stable 409 `supervisor_cycle` response;
map lock contention to a recoverable 503 without disclosing paths. Existing
frontend error handling must retain edits and render that safe message. Critical
dependencies are PostgreSQL and current owner authorization; failure rolls back
or denies, never skips validation. No cache supplies hierarchy authority.

Alternatives rejected: UI-only filtering cannot prevent forged/concurrent
writes; ancestor reads without serialization race; serializable retries for the
whole aggregate add complexity while a hospital-local lock fits this write
rate; a new graph service duplicates the Workforce owner and adds a failure
boundary. Effective-dated posts, multiple reporting dimensions and approvals
remain separate organization/lifecycle work.

## Acceptance and verification

1. Real PostgreSQL HTTP tests first reproduce two-/three-person cycles, a valid
   chain, self/foreign-scope denial and repair by clearing a link. Assert status,
   retained graph/version and no failed-command audit/outbox publication.
2. Concurrent opposite edges commit at most one; direct authorized SQL cannot
   bypass the database constraint. A pre-existing cycle fixture must terminate
   and reject a new entrant, while repair remains possible.
3. Existing enrollment authorization, exact-retry and stale-version tests pass;
   current staff/profile rows and unrelated fields remain unchanged on failure.
4. Measure representative long chains and inspect the recursive plan with RLS.
5. Through local UI, reproduce the original journey against the changed API:
   safe conflict message, retained draft, valid reassignment/clear, reload and
   unrelated role denial. Business fixtures continue to be entered via UI.
6. Run affected race tests, vet/build/lint, contractdocs generation/check, canonical
   checks and relevant shell tests/build. Run full CI before merge. Deploy and
   repeat the browser journey only after the named release approval.

## Query-plan finding during implementation

The first recursive join failed the capacity gate: a 2,000-person chain took
48,262 ms in the constraint trigger. Its owner query plan used a nested loop
whose profile index condition contained only tenant/hospital, with staff ID in
a join filter; this repeated a hospital scan for each hop. This is a measured
failure, not an accepted performance result. Refined traversal uses a lateral
single-record lookup at each hop, with both identity and next edge in the
recursive set, avoiding a second full component-to-profile join. Preserve the
250 ms/one-second evaluation assertions and rerun actual plans with forced RLS.

The final refined repeat passed: extracted owner SELECT 37.510 ms (9,995
shared-buffer hits, zero reads), complete guarded UPDATE 40.381 ms (constraint
trigger 39.837 ms), and HTTP cycle refusal 98.091 ms locally. Neither evaluation
budget changed. The tagged test extracts the installed owner function's recursive
SQL, runs EXPLAIN ANALYZE with the application context and verifies full staff-key
lookups with at most one row per hop and the full 1,999-ancestor traversal. This
ran alongside background validation; it is not a production p95/p99 claim.

## Snapshot isolation boundary

A further regression reproduced two opposite direct SQL links committing under
repeatable read: waiting on an advisory lock does not refresh that transaction's
old snapshot. The supported owner command uses PostgreSQL read committed. The
database trigger therefore rejects non-null supervisor writes in repeatable-read
or serializable transactions with named `workforce_supervision_isolation`
(SQLSTATE 25001), rather than pretending their snapshot is current. PostgreSQL
read uncommitted has read-committed snapshot semantics and is also admitted.
Clearing a supervisor remains safe because it only removes an edge. The API maps
an unsupported owner transaction mode to 503 `supervision_unavailable`, without
path disclosure. No caller may weaken the guard through a header or request field.
The regression anchors two actual repeatable-read snapshots before opposite
writes, then checks that no cycle commits and both writes are refused. This avoids
adding a second hierarchy/version table solely for database lock coordination.
