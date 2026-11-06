# Independent supplier grants and fulfilment

Requested scope: independent pharmacy outlets and distributor premises supplying
hospital receiving outlets in India, initially `IN-MH` and `IN-OD`. This extends
the partner-team foundation merged in #1614. It does not approve a real trader,
licence, category, tax policy or production transaction.

## Ownership and authority

SupplyConnect owns bilateral relationships, immutable terms and order exchange
(ADR-0031). Shared IAM owns external grant drafts, independent grant decisions,
typed named-person/group recipients and revocation. Workforce owns current home
employment, reviewed account links and postings. A hospital administers its own
team; a supplier chooses the exact relationship/actions/fields it discloses.
Neither operation creates employment or internal supplier authority.

PharmaConnect owns dispatch from a pharmacy outlet and receipt into a hospital
pharmacy. SupplyConnect must implement its own wholesale stock ledger before a
distributor dispatch can operate. Receiving, quarantine/acceptance and financial
settlement are distinct decisions with separate attributable evidence. A courier
delivery cannot post stock, approve a receipt or settle an invoice.

Governance grant preparation uses current IAM access-management authority and
credential/membership checks. Operational use additionally requires current
home employment and account/posting sources, bilateral relationship authority,
the same qualifying action/scope/field grant and actual object participation.
Unknown scope returns 404, missing permission 403, stale intent 409 and unavailable
authority 503. No tenant switch, foreign-tenant SQL, private cross-module reads,
positive eligibility cache or clinical permission is introduced.

## Ordered implementation

1. Immutable, typed, non-operating grant preparation, bounded owner-only lists,
   exact recovery and a clear UI. This stores requested intent only. Recipient,
   relationship, legal and operational verification are explicitly pending.
2. Bilateral relationship/recipient assurance and independent decisions on the
   same immutable revision. Current identity is verified by the onboarding owner;
   licence/category/activity and jurisdiction by their current owners. Any party
   may reduce access without waiting for consent. An offer is not acceptance.
3. Owner-authorized disclosure with exact audience, purpose, actor, object,
   relationship and field policy; every page/download and consequential delayed
   release repeats current authority checks. Supplier costs, other customers,
   private Staff and patient records are excluded from procurement views.
4. Approved hospital order, supplier acceptance, eligible source reservation and
   dispatch, physical receiving evidence, discrepancy/quarantine, independent
   acceptance and destination-owner receipt. Immutable line references and exact
   base units connect separate source/destination ledgers.
5. Invoice/payable/credit/settlement reconciliation retains source references and
   exact INR minor units. Receiving never infers payment. Temporal owns durable
   waits and compensation; local effects and outbox commit atomically, and both
   message IDs and business command/line/revision keys deduplicate effects.
6. Full synthetic browser/API/owner acceptance, required CI, exact-tree merge,
   image/freight verification and Kargo `kargo-helivanta/prod` promotion, followed
   by read-only health/schema checks and production-safe smoke tests.

Each step is a vertical checkpoint, not completion of the complete request.
Draft endpoints cannot activate a grant or be consumed as trade authority.
No general-purpose collaboration service, duplicate drug master or second stock
owner is needed. Existing planning assumptions remain unmeasured: 50/150 tenants,
1,000/3,000 peak RPS at months 12/36, 80:20 reads/writes and 2–20 KiB payloads.
Grant preparation uses a 16 KiB request bound and the existing signed cursor/page
limits; its target remains 99.9% availability and p99 below 500 ms. Measurement
and actual trade-volume approval remain release evidence, not inferred facts.

## Failure, recovery and release

Each preparation ID is UUIDv7, retained before sending. Repeating the same
actor/ID/intent returns the same committed draft; changed content conflicts.
Audit failure rolls back creation/read disclosure. Unknown command outcomes are
recovered by the original identity, never by creating a new one. Immutable
proposals are not editable grants; widening requires a new reviewed revision.

New tables use forced tenant RLS, explicit purposes, composite identities,
append-only audit/history and additive migrations. Rollback retains evidence;
there is no destructive down migration. The existing GitOps release/migration
hook remains the only production migration path. Reverting application code
cannot erase committed physical or financial acts.

Before live trade, the platform owner must finish onboarding-service #12 and
deliver the dedicated product machine/audience through OpenBao/ESO. Actual
supplier/buyer premises, current evidence and accountable applicability review
are required for both states and each activity/category. Missing facts block
activation; they do not block safe draft preparation or synthetic development.

Acceptance includes other-tenant and wrong-hospital denial; self-approval and
action/scope mixing; revoked/rehired members; source/graph outages; stale and
reordered messages; duplicate or conflicting retries; partial/unsafe receipts;
wrong units, recalled/expired batches; and financial mismatch/unknown outcomes.
Traceability: ORG-AU/ORG-AC, ADR-0049/0052/0054/0055 and IN-PH-02/03/04/05/08/09/10.
