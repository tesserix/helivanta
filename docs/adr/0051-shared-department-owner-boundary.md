# ADR-0051: Shared department owner and hospital references

Date: 2026-11-02. Status: local implementation decision extending accepted
ADR-0044; no production rollout or schema activation is approved here.

Workforce owns Staff and postings; platform `organization` owns structural
departments/sub-units. Pharma owns outlets, product acceptance and professional
authority. Existing Workforce HR department revisions stay historical profile
references until an explicit handoff/mapping is implemented. They must not become
a second editable structural tree.

Initial `organization.structure.read` and `organization.structure.manage` are
explicit tenant-wide operational-governance permissions. The reconciler's current
tenant-administrator default applies; no other default role receives them. This
grants no private Staff, patient, outlet, posting or professional authority.
Department-head/job roles need separate scoped assignments before their consumers
are exposed. No downward access inheritance is implemented by this decision.

Organization first registers a pending canonical hospital reference. Workforce,
the actual hospital owner, consumes an identifier-only request and returns only
whether that tenant-local reference exists. Existence is a retained catalog fact,
not an actor authorization check or a grant. Current human governance permission
is required on each command. There is no cross-module SQL, forwarded browser
credential or remote call inside a transaction. An unavailable owner leaves the
request explicitly pending and unable to host active structure.

Requests bind tenant, hospital, immutable client UUIDv7 request ID and normalized
reason. Exact retry returns that request's retained status (including eventual
observation), with no duplicate publication. Different intent/key reuse conflicts.
A result applies only to its original request and cannot replace a newer current
registration. Wrong-tenant/unknown hospital references are indistinguishably
rejected. Request, current registration, audit and outbox commit atomically.
Events carry identifiers and a boolean observation, never Staff/patient details.

The tree will use hospital-local immutable codes, UUIDv7 nodes, optimistic
commands, immutable revisions, immediate server-effective timestamps, parent-
kind/depth/cycle validation, retirement and bounded lists. A hospital write lock
serializes restructuring. Scheduling, HR-reference handoff, posting dependencies
and Pharma acceptance are unfinished until implemented and tested together.

Use the inherited unmeasured budgets: 50 peak Staff requests/s, 80:20 read/write,
p99 under 500 ms, 99.9% availability and 500,000/1.5m Staff at 12/36 months. The
planned structural tree is bounded at 5,000 nodes per hospital. Existing Postgres
and outbox suffice; no new service/cache/queue or runtime cost is introduced.
Rejected: product-owned Staff/department copies; shared cross-owner queries;
queued hospital verification as human authority; and premature service extraction.

Dependency or authority failures deny; failed audit/outbox rolls back. Rollback
retains additive schema and immutable evidence. Production migration/rollout
requires concrete named approval after the complete vertical acceptance.
