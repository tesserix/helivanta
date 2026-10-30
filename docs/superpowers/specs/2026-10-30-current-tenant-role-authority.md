# Current IAM membership bounds graph permissions

Status: implementation design, 2026-10-30. Related: A02/A06/A11, #40, #118,
ADR-0047/0049, [ADR-0050](../../adr/0050-current-iam-role-admission.md) and
ORG-AC-06/07. No new role, route, migration or operational
employment authority is introduced by this change.

## Problem and threat boundary

IAM owns `iam_members`. Production currently passes the OpenFGA client directly
to permission resolution and route membership checks. Tuple delivery is
asynchronous; grant and revoke use separate durable consumers, and either may
retry. A removed role must not authorize a new request merely because its graph
tuple remains or an earlier grant is replayed. A remaining membership in another
role must not make removed permissions usable.

Assets are tenant-private data and administrative/operational commands. Threats
are a removed actor with an old signed session, another tenant and delayed or
replayed external projection updates. The verified session selects tenant and
subject. IAM SQL and OpenFGA must independently support an allow; neither caller
headers nor the graph projection substitutes for current membership.

## Decision

Add an IAM-owned authorization adapter implementing the existing permission
resolver and membership-checker interfaces. Main uses the same adapter for the
authenticated V1 chain and route membership gate. Resolve graph permissions
first, then query named current role keys in the exact verified tenant through
normal forced-RLS `WithTenant` access. Return only permissions both resolved by
the graph and declared for a currently held role by `platform.GrantsFor`.
Membership requires both graph membership and at least one current known IAM
role. Missing roles deny. SQL, graph, unknown-role corruption and context errors
fail closed. Bound the role query by the code-declared system-role catalogue;
never silently truncate a corrupt result.

Use canonical registry grants, including the existing administrator behavior,
rather than copying permission-to-role rules or changing clinical grant policy
inside a revocation fix. Scoped assignments retain their separate current
generation/projection/owner checks. No network call occurs inside a SQL
transaction. Source reads happen after graph reads, catching removal while a
graph response is delayed. This is request admission, not a transaction-held
employment or file-release fence; those remain explicit full-objective work.

Self-service routes retain their existing route markers. An empty current role
set produces an empty permission set, permitting authenticated recovery/sign-out
handlers to retain their existing behavior. An authorization outage keeps the
existing 503 behavior. No new business permission can be manufactured by the
adapter, including when graph grants are pending or absent.

## Alternatives, budgets and rollout

Relying on event ordering or a boot reconcile is rejected: an asynchronous
projection cannot prove current membership. Replacing graph decisions with SQL
alone is rejected: it would bypass the configured authorization dependency and
its denial semantics. A new service, datastore or cache is unnecessary.

Reuse existing unmeasured platform planning budgets: 1,000/3,000 peak RPS at
12/36 months, 80:20 read/write, 2–20 KiB typical API payloads, 30% hot-tenant load,
99.95% availability, p99 reads at most 500 ms and command acceptance at most
800 ms. This adds bounded indexed reads on the existing `(tenant_id,subject)`
membership index and no new infrastructure. Measure query plans and integration
latency; these planning targets are not observed production performance.

The additive code change needs no data migration. Validate legacy bootstrap,
existing-account enrollment, tenant switching, signed-session self-service and
scoped assignment consumers before release. Legacy graph-only authority will
deny until its actual IAM membership is reconciled through the existing owner;
do not silently backfill from graph tuples. Document the inventory before a new
production approval. Application rollback restores the earlier admission model
and therefore weakens this new protection; any production rollback needs its
own approval. Preserve all membership and audit history.

## Required proof

Real PostgreSQL/OpenFGA tests: removed administrator role while another role
remains; final membership removal; another tenant preserved; graph-only and
SQL-only grants denied; stale graph grant replay; dependency failures; current
source read after a delayed graph response; bounded/unknown roles; authenticated
self-service recovery. Verify production construction actually wires both gates
to this adapter. Run package race/coverage, relevant architecture/bootstrap
tests, vet/build/lint and CI. Follow with actual local and deployed UI through
the Global API. Do not mark A11 or the overall Access/RBAC area complete from
this admission fix alone.
