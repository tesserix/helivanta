# Staff HR department handoff

Status: implementation candidate; no production rollout or migration approval.
Refs #417, #22, #287, #616; ADR-0044/0051; ORG-ST-01/02/05/07.

Workforce HR department vocabulary pins historical enrollment/payroll revisions.
Organization owns the structural tree. Replacing those historic IDs, matching by
name or silently making a second tree would break source lineage. Workforce will
retain an explicit, attributable mapping request from an exact HR revision to an
exact Organization node/revision. Neither owner queries the other's tables.

The existing verified tenant and OpenFGA `workforce.master.manage` permission,
plus current hospital manager access, authorize preparation. Read permission and
current hospital reader/manager access authorize bounded history and receipts.
Out-of-scope is 404, missing action permission 403, unavailable authorization 503.
The current Organization reader permission independently governs the UI picker.
Mapping observation is catalog evidence, never Staff eligibility, a posting,
professional review, clinical approval or an IAM/OpenFGA relationship grant.

PUT `/v1/workforce/hospitals/:hospital/hr/masters/:id/department-links/:request`
takes UUIDv7 request, expected mapping version (zero initially), current active HR
department revision UUID, Organization node/revision UUIDv7 and a required reason
of 1–500 normalized characters. Identity/tenant/actor are server derived. New
commands serialize per HR master, reject stale versions, and permit at most one
pending request. Version increments once per request, bounded to safe integers.
Exact actor/intent retries return the retained receipt, reauthorize, and republish
the identifier-only request while pending to repair delayed/dead-letter delivery.
GET that path recovers a receipt; the collection is bounded keyset history.

Organization observes whether the requested exact revision is the current active
node in that tenant/hospital, retaining its first answer and the complete request
identity. Duplicate identical delivery converges; changed reuse conflicts. The
answer contains only identifiers and an explicit boolean. Workforce applies it
only to the matching retained pending request, with immutable first observation.
States are `pending`, `matched`, `rejected`; `matched` is explicitly historical
snapshot evidence. A subsequent HR or Organization change does not rewrite it or
make it current authority. New postings must independently validate current owner
state and lifecycle; they may not trust this observation or broker timeliness.

Each owner commits state, audit and outbox together. Payload bounds, explicit bool
presence, exact envelope type/version/tenant and full identifier comparison fail
closed. Failed audit/outbox rolls back. Forced RLS, immutable request identity,
terminal decisions, gap-free versions and no removal are database constraints.
No client-scheduled effective time or automatic HR/profile rewrite is introduced.
No reasons, Staff data, HR labels or actor details are broadcast in events.

UI in HR setup selects an active canonical department/sub-unit from Organization,
shows exact pinned source versions and pending/matched/rejected history, requires
a reason and retains command identity for retry. It clears protected data when
scope/authority changes. It does not offer structural edits in the HR vocabulary
form. A stale picker is rejected by the owner observation rather than accepted
as a current posting. Existing profiles/payroll remain on retained HR revisions;
the explicit mapping is the handoff lineage for the forthcoming posting work.

Use the existing unmeasured 50 peak requests/s, 80:20 read/write, p99 <500 ms,
99.9% availability planning budgets. Request/answer <2 KiB, body <8 KiB, history
1–200 rows/page. Owner observation uses indexed exact lookups; request/history
indexes bind tenant/hospital/master, and no new service/cache/download is needed.
Additive migrations are forward-only: rollback disables consumers/routes/UI while
retaining immutable evidence. No production operation is included. Reject direct
cross-owner SQL, inferred name matching and reusing events as operating authority.

Acceptance: normal manager UI, read-only history, foreign/sibling denial, required
fields, stale HR/Organization revisions, exact and changed retries, concurrent
commands, withheld/reordered/contradictory observations, RLS/immutable SQL guards,
audit/outbox rollback and real OpenFGA revocation/outage. Complete posting and
Pharma eligibility remain required by the parent reusable-Staff objective.
