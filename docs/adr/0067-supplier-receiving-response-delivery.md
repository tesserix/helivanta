# 0067 — Deliver supplier receiving responses to the buyer

Status: implementation under test; deployment and workflow acceptance pending.

A response recorded only in the supplier tenant does not complete discrepancy
collaboration. Supply remains the owner of commercial responses; Pharmacy remains
the owner of hospital receiving counts, corrections, custody and release.

The source response and a directed outbox event commit in one owner transaction.
Retries of a recorded command retain the original response and do not publish a
second event. The response records its summary version, source/buyer tenant and
order/shipment/resource scope and authenticated actor. Its reviewed Staff reference
stays source-local; the shared event and buyer projection exclude it. The receiver
checks the authenticated envelope's source and destination against the record and
its known buyer order/dispatch, then persists an immutable authorized projection.
Duplicate delivery is harmless; changed data for the same response is a conflict.

Buyer reads require current goods-read Staff admission, hospital/department/outlet
scope and the current Pharmacy department binding. Source history requires current
source order-read authority. Both are audited and bounded to fifty records with
resource-bound continuations. Ordinary history disclosure and original-actor retry
recovery are separate. Every response retains its original summary version: an old
response does not become confirmation of a later correction or closure.

A supplier response cannot change hospital counts, physical stock, clinical
release or financial liability. Reads must display supplier authorship separately
from the hospital's effective receiving state. Missing or stale delivery is not
confirmation. Broker failure leaves durable work queued; dependency failure denies
authorization instead of returning fabricated empty-success data.

No new service or datastore is introduced. Assumptions, not measured capacity:
at most a few responses per shipment, typically one per closed summary version;
reply payloads below 4 KB; indexed pages of fifty records; p99 read target 500 ms.
Outbox and projection backlog/retry metrics must be measured during acceptance.
The incremental infrastructure cost is zero. Forward Supply migration 0017 adds
the source event reference, buyer projection and read indexes. Rollback removes
exposure/consumption while preserving recorded replies and queued evidence; it does
not erase records or reverse stock. No regulatory certification follows from a
supplier acknowledging a commercial discrepancy.
