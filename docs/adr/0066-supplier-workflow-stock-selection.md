# 0066 — Stock selection for current supplier orders

Status: implemented incrementally with fulfilment workspaces; runtime acceptance pending.

Narrowly scoped supplier Staff must choose physical stock for the exact accepted
order line. Ordinary inventory administration is not their source of authority.
Choices belong to the physical stock owner, with the same current Staff, operating
or hospital department scope, current order/consent proof and owner RLS used for
reservation. No new service, store, permission or master is introduced.

Sizing assumptions, not measured load: ten simultaneous staff per busy outlet,
up to ten selector reads/second, pages of fifty records under 50 KB, and target p99
500 ms. Existing tenant/location/product and immutable creation-time indexes bound
reads; 100,000 lots per outlet over 36 months does not justify another datastore.
Infrastructure cost change is zero; query cost and latency remain measurement gates.

Choices are exact-line receipt/batch pages. Pharmacy follows with a separately
bounded sellable shelf page; Supply receipts have no shelf input. Counts are
observed hints, not holds. Reservation/dispatch still recheck quantities, active
holds, current versions, licences, safety and attributable Staff under owner locks.
Every page reauthorizes and audits its sensitive read. Dependency or identity
failure denies discovery; no stale inventory or fabricated empty-success fallback.

The routes and generated canonical choice models are additive. Removing their UI
and routes rolls this back without changing stock or financial history. Reusing
broad inventory directories was rejected because it requires unnecessary grants
and does not admit the hospital source's department-specific authority. Reads do
not grant pharmacy administration, stock release, prescribing or clinical authority.
