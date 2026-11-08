# Supplier fulfilment and hospital receiving

Implementation specification; not release, licence or regulatory approval.
Scope: India, Maharashtra and Odisha, using the existing reviewed participant,
activity, category and jurisdiction policy. No new statutory rule is inferred.

## Ownership and atomicity

SupplyConnect owns bilateral order consent. Exactly one stock owner supplies an
order: SupplyConnect warehouse/depot or PharmaConnect outlet. That stock owner
owns the line allocation budget, lease, dispatch and immutable command receipts.
The source budget and physical stock decision commit in **one owner transaction**.
The accepted-order owner exposes a private publication guard for the exact tenant,
order, source organization/location and purpose. It freezes the current order
while the stock owner commits; it never writes stock or supplies a client grant.
No HTTP, broker publication or sleep occurs inside a stock transaction.

This replaces the unimplemented draft's separate coordinator-budget/Temporal
reservation saga. A coordinator budget would introduce an unnecessary second
writer/uncertain allocation. There is no distributed stock command to compensate
in this design. Lease eligibility is evaluated under the stock bucket's existing
serialization lock, using the database clock; an expired lease cannot dispatch.
Dispatch is a separate attributed human command, not a timer or event. Read views
report effective expiry without inventing a release movement. Cross-owner receipt
is a new physical fact, not rollback of the source dispatch. Durable outbox,
idempotent command/ack consumers and immutable receipts carry owner facts.
A future service extraction must replace process-local publication guards with a
reviewed fencing contract, not hold transactions across network calls.

## State and boundaries

- Reservation: exact accepted seller order/version, line, owner product/version,
  unit, source stock bucket, positive integer quantity and bounded expiry. Source
  staff need current reviewed operating duty; order acceptance is not stock
  authority. The current bilateral proof and licence/policy, product identity,
  safety, quality and premise-local expiry are rechecked for new commitments.
- Source owner serializes the order line and stock bucket. Live reserved plus
  dispatched quantity cannot exceed the order line. Multiple orders and ordinary
  pharmacy sales/corrections cannot consume reserved stock. Warehouse identity
  changes cannot reinterpret live or dispatched units.
- Release is attributable, idempotent and cannot erase a dispatch. Expiry permits
  reallocation only after the same stock lock and current clock check. An original
  command recovery always returns its immutable result, not the current view.
- Dispatch references the reserved allocation, exact current version and physical
  handoff reference/reason. It rechecks current authority, source eligibility and
  remaining reservation before committing a negative physical movement. Its
  immutable shipment retains order/line, owner product/version/unit, lot, expiry,
  quantity, source and exact destination, actor and time. No patient or private
  source Staff ID crosses tenants.
- The hospital receives only a verified shipment for its accepted immutable order
  and exact hospital/outlet. Current department and destination operating authority
  are independent checks. Suspension/expiry of new trading permission does not
  erase goods already dispatched; retained-transaction receipt never permits new
  source commitments.
- Receiving records counted, accepted and rejected quantities separately, explicit
  observed lot/expiry, destination catalogue/version and quarantine location.
  Accepted units require an attributable exact product mapping and matching batch
  facts; no name-based automatic match. Accepted goods enter quarantine, never
  sellable stock. Rejected/unidentified custody is retained separately and never
  becomes available. Partial counts are cumulative under a shipment lock; counts
  cannot exceed dispatch. Closing a short shipment records an explicit discrepancy.
  Supplier acknowledgement retains both parties' evidence and does not change
  physical facts, create a return, invoice, credit or settlement.
- Source cancellation releases only undispatched allocations. Acknowledgement of
  owner release is required before the order claims cancellation complete. Existing
  shipments and receiving/discrepancy history survive cancellation and replay.

Every command uses a UUIDv7 identity, expected current version where applicable,
server-derived actor/Staff identity, exact workspace headers, bounded strict JSON,
forced RLS, audit and atomic outbox. Changed-intent/actor retries conflict. Unknown
outcomes recover the original receipt; unavailable authority/catalogue/audit denies
new action. Scoped absence is 404, changed state 409, unavailable verification 503.

## Delivery, limits and acceptance

Reuse unmeasured planning assumptions: 50/150 tenants at 12/36 months, overall
1,000/3,000 peak RPS, 80:20 reads/writes, 2–20 KiB typical payloads. Commands are
bounded to 64 KiB, 100 order lines and one billion owner units per operation.
Reservations have explicit expiry no later than the order/relationship and a
24-hour technical lease maximum. Indexed owner/line/bucket queries and bounded
keyset pages are required. Control-plane p99 <500 ms and 99.9% availability are
measurement targets, not observed claims. Existing DB/outbox capacity is reused;
no new worker fleet or estimated saving is claimed.

Forward-only additive migrations preserve old stock, commands and shipments.
Rollback disables new commands and retains evidence; no destructive down migration
or compensating deletion of physical acts. Initial QA uses synthetic isolated
participants and fixtures, not live clinical dispensing or invented legal approval.

Acceptance: both stock owners; cross-tenant/outlet/department denial; current and
revoked Staff/duties; stale order, product, quality and licence; concurrent orders,
sales and correction; lease expiry and release/dispatch races; altered idempotency
keys; timeout recovery; duplicate/reordered source facts; partial/short/rejected
receipt; changed lot/unit/expiry; cancellation after dispatch; audit/outbox failure.
Native unit tests, real database/broker and Global API tests, strict SDK tests,
production builds and actual web acceptance are separate required evidence.
Financial composition, tax, invoices, payables, credits and settlement remain
outside this requested fulfilment/receiving scope. IN-PH-01/03/04/05/08/09/10 and
ORG-AC apply. Actual licence/category/state activation still requires qualified
participant review; code and passing tests are no regulatory certification.
