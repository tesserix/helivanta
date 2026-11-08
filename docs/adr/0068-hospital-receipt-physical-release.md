# ADR-0068: Receipt-bound hospital quarantine release

Status: implementation candidate; deployment and acceptance pending.

Use the supplier fulfilment specification's existing capacity, payload and SLO
planning assumptions. This adds no service, datastore or worker. PharmaConnect
continues to own releases, both stock movements and the outbox atomically.

A current department goods reviewer may release a verified receipt only after
independent clearance at the current closed shipment summary version. Admission
uses Supply's existing `ReviewReceiving` purpose, current Workforce actor and
Pharma outlet governance. The original recorder cannot perform this review/release.
No private outlet editor permission is granted. A receipt-bound owner command
retains expected summary, destination shelf, quantity, reason and actor/Staff.
Batch and quarantine location derive from the immutable receipt. Cumulative
release cannot exceed the receipt's effective accepted quantity.

Reuse the ordinary Pharma release implementation, including source safety,
product status, expiry, approved schedule, effective tax, MRP, safety incidents,
clearance provenance, bucket serialization, immutable release and movement outbox.
Narrow RLS permits those reads and exact linked release writes in the current
review frame. A separate receipt/release journal binds actor, frozen input and
original response. A deferred FK requires the actual stock release in the same
transaction. Review alone never asserts that stock moved.

The shipment lock serializes corrections, closure, clearance and release; existing
stock locks serialize other inventory actions. Reused IDs with changed input or
actor conflict. Original retry returns original evidence under current authority,
even after later balance changes. Any authorization, safety, database or outbox
failure rolls back the entire new physical command. No remote HTTP or broker wait
is introduced in the transaction. Cross-owner safety remains the existing local
publication fence, subject to a new fencing contract before service extraction.

The alternative of granting private inventory editor access is rejected because
it permits unrelated outlet operations. Creating a second release engine is
rejected because it duplicates safety and stock rules. Forward additive migration
retains all evidence; rollback disables the route and retains completed acts,
with no physical reversal or destructive down migration.

IN-PH-01/03/05/08/09/10 and current department authority remain applicable. No
new Maharashtra/Odisha licence, tax or medicine policy is activated by this ADR.
