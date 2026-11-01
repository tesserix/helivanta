# Current-incarnation membership graph

Date: 2026-10-30. Status: implementation candidate, not accepted or deployed.
Implements the graph prerequisite of the [membership design](2026-10-30-membership-removal-and-recovery.md).
All A01–A12 acceptance and owner eligibility gates remain in force.

## Evidence and ownership

Three real regressions show old subject/role DELETE removing a replacement row,
historical grant delivery projecting an absent membership and a replacement row
borrowing the old row's graph authority. The first requires coordinated legacy
command migration. This design addresses the latter two and reordered revocation.
IAM owns row identity, subject, role, tenant and membership epoch. OpenFGA is an
authorization projection, not an identity or employment master. No other owner's
tables are written and no new business permission is introduced.

## Decision under implementation

Add a `membership` graph type with a `subject` relation accepting only `user`.
Its object key is tenant plus a content hash of canonical tenant ID, immutable
membership row ID, actual subject, known role and membership epoch. UUIDv4 legacy
row IDs remain valid. Changes to any binding produce another graph identity.

The canonical role's `assignee` relation additionally accepts a direct membership
principal and a membership's `subject` userset. The latter preserves existing
real-user graph relationships required by scoped assignment evaluation. The
former supports permission resolution for precisely one incarnation, without
collecting permission grants from old memberships of the same human.

Projection writes a subject binding, a direct membership-principal role edge, a
membership-subject userset role edge and the existing shared role-to-tenant edge.
Only the first three belong to one membership. Removing an incarnation never
deletes another membership's edges or the shared role/permission catalogue.
Tuple construction has one canonical implementation shared with reconciliation.

Before resolving a membership principal, verify its graph role set contains only
the expected canonical tenant/role, its actual-user subject edge exists and it
is a graph member of the expected tenant. Reject missing, extra, malformed or
truncated authority. Resolve permissions for the membership principal, never the
real user's aggregate role set; intersect with the source role's canonical
registry permissions as an additional bound. Subject usersets accept only real
users, so an old membership cannot confer permissions on a new membership
principal through its subject userset.

Read current source rows under normal forced RLS before network checks, then
read them again afterward. Return only proofs whose immutable IDs, role, tenant,
subject and membership epoch still match. Dependency failures return unavailable
authority; no partial permissions or SQL-only fallback. No graph network call is
held inside a database transaction.

## Discovery, projection and boot

Current IAM admission, permission reads, tenant discovery, switching, genuine
login and renewal must use the same source-bound role adapter. Graph discovery
supplies candidate tenants only; current IAM rows and their incarnation proofs
determine actual memberships. A strict result bound must reject truncation.
Do not introduce a system-pool cross-tenant subject lookup for ordinary login.

Existing v1 membership events become reconciliation hints, not instructions to
blindly grant/revoke subject/role tuples. Work runs after the event claim commits
and retries before broker acknowledgement. Read graph subject bindings before
reading the current source snapshot; project current rows and retire only
observed non-current incarnation bindings. Source/current-ID admission remains
the safety fence if an external timeout leaves residue. Reconciliation must
periodically/at boot clean residue; its absence is not proof of access.

Boot reconciliation must include row IDs and membership epochs, project the same
canonical tuples and prune obsolete incarnation/legacy tuples. Do not add a new
type that the pruner silently ignores. Read graph state before authoritative
source state, preserve the existing empty-source protection, and never delete a
tuple whose tenant ownership cannot be verified. Reconcile actual schema and
consumer construction; registry parity alone does not prove this behavior.

Implementation adds `0017_iam_membership_projection_reader`: SELECT only on the
existing IAM epoch table for the existing privileged startup reader. It changes
no app RLS policy and grants no write privilege. Canonical IAM migrations replace
the older copied test-table fixtures. Ordinary admission/discovery keeps normal
tenant RLS, not this system pool. Hospital onboarding uses the same post-commit
projection, and projection rereads source before acknowledging completion.

## Budgets and failure semantics

The current registry declares 147 permissions and 29 system roles. Source rows
are bounded by that catalogue per subject. A role-scoped graph permission set
is bounded independently of the number of historical incarnations, preventing
old histories from exhausting a 1,000-object permission result cap. Reject caps
instead of silently accepting partial discovery. Planning load remains the full
RBAC plan's unmeasured 1,000/3,000 peak RPS assumptions, not an observed SLO.
Measure added provider calls and source reads under the existing five-second
request budget before production acceptance. Do not claim a latency/cost target
without a measured provider/deployment configuration.

Issue #40 separately specifies p95 authorization targets of 25 ms including
cache and 60 ms for misses. Those targets remain unproven for this candidate;
the five-second deadline is a failure bound, not an acceptable latency claim.
Do not close #40 or replace its performance gate with the local index timings.

The SDK exposes server-side BatchCheck; verify compatibility with the actual
test/deployed provider before depending on it. Every item must have an explicit
allowed/denied result and no item-level error. Missing, duplicate or extra results
fail closed. Provider/source outages leave projection unacknowledged and retry;
recovery never fabricates readiness. No cache or distributed mutex is authority.

## Compatibility and rollout gates

The new model is additive, but old admission code does not enforce incarnation
proof. A mixed application cohort is not accepted for recovery activation.
Prepare the exact source/model/SQL delta, reconcile the synthetic cohort, prove
new readiness and traffic ownership, and gate new commands until the serving
cohort uses the new adapter. Rollback requires explicit approval and cannot
erase history or re-enable old generations. No production change is authorized
by this document.

Canonical role policy is unchanged here; legacy clinical/governance grant
classification remains a separate accepted-owner migration gate. Historical
Staff links and hospital/outlet/base grants still need owner-controlled generation
eligibility. Passing graph tests does not complete those integrations or A11.

## Required evidence

Use real OpenFGA/Postgres and signed sessions. Show two incarnations of the same
subject/role never share permission proof; delayed old cleanup preserves the new
one; wrong subject/role/tenant/epoch and extra graph roles deny; malformed and
partial provider results fail closed; source removal during delayed graph work
denies; projection retries after commit; boot pruning preserves current tuples;
ordinary login/switch/renewal discover the correct current rows. Keep all three
existing regression assertions and verify their original failing behavior. Run
affected race tests, model-upgrade and reconciliation tests, architectural and
construction checks, generated contracts and normal Admin/reviewer/recipient UI
journeys. Full legacy-command and owner-cutoff acceptance remains required.
