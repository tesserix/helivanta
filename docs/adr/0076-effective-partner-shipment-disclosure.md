# ADR-0076: Effective partner shipment disclosure

Status: development design; no release activation or deployed acceptance.
Continues the independent supplier request and #490. It does not replace the
external transport scope in that issue or close supplier returns #1784.

## Source findings and boundary

IAM's `WithPartnerScope` is wired but has no product disclosure consumer. Its
hospital-only `PartnerRecipientSource` is deliberately insufficient. SupplyConnect
already retains validated, immutable dispatched-shipment projections from both
physical owners in `supply_supplier_dispatched_shipments`, linked to its exact
seller-side order. PharmaConnect remains the outlet stock owner; SupplyConnect
remains the warehouse stock owner. A reader must use these published projections,
not another module's stock tables or a new shipment master.

The asset is one hospital's commercial shipment evidence. A different hospital,
source customer, revoked staff member, or account with another template must not
obtain it. The boundary is the current recipient/grant/relationship/graph check,
then a read-only source-owner purpose and an explicit field projection. Missing
owners, failed audit and unverifiable scope withhold the complete response.

## Decision

Use the existing partner authorizer for a selected hospital, grant and source
product. It rechecks named/group membership, posting, generation, current bilateral
terms, source eligibility and acknowledged graph content, then reacquires both
owners' guards. No source tenant membership, operating duty or account switch is
created. Each page repeats this check. The existing new-business relationship
rule applies: suspension denies this sharing path. Hospital-local receiving and
financial obligations keep their separately authorised retained routes.

Add candidate Global API paths under `/supply/hospitals/:hospital/partner-grants/
:product/:grant/terms/:terms` (the line break is prose only):

- `GET /shipment-orders`: at most 50 linked seller-side order references with published dispatch facts.
- `GET /shipment-orders/:order/shipments`: at most 50 dispatched facts for that
  exact visible order.

Both require `partner.shipment.read` through the current compound authorizer,
verified expected tenant/subject, a 15-second request deadline and sealed resource
cursors binding actor, hospital, grant, product and selected order. Unknown query
fields are refused. Hidden grant/object scope is 404, missing action authority is
403 where distinguishable without object disclosure, account changes are 409 and
unavailable authority/audit is 503. No automatic financial or physical retry exists
because these are reads.

Only the source projection owner's guarded tenant context may read its rows. Add
an isolated `partner_shipments` SELECT purpose, exact source/buyer/hospital/outlet/
relationship predicates and a no-write fence. Existing permissions and original
migrations stay unchanged. The new migration extends each selected table's
single permissive tenant policy and preserves its existing write check; restrictive
purpose and delete policies forbid writes and unrelated reads. The database's
one-permissive-policy gate remains unchanged. This purpose must not make order commands/history,
private reasons, internal actor or Staff references visible via parent-row RLS.
Use a concurrent `(tenant_id,relationship_id,created_at DESC,id DESC)` order index
and the existing per-order shipment keyset index. Audit the selected grant and
selected order inside the guarded source transaction before releasing any page.

Canonical projection fields belong in `contracts/supplier-access.json`, reusing
`SupplierGrantScope` and source-owned field definitions from supplier-orders and
supplier-fulfilment. Return human PO/handoff references, description, unit/pack,
quantity, lot, expiry and dispatch time, plus internal scope/correlation fields.
Exclude source actors/Staff, private command reasons, other customers, reservations,
costs, prices, patient records and raw source documents. SDKs check exact scope,
grant/order linkage, bounds, duplicates and continuation, use no-store/abort/deadline
and retain account-bound query keys. UI selectors show references, never require
raw identifiers, hide refused cached evidence and reset on account/resource change.

The complete entry flow also needs an authorised recipient grant chooser with
owner-supplied human source labels. A raw grant-ID form or an unmounted component
is not a completed UI. Keep the entire candidate dark under
`supplier_collaboration`; do not change eligibility/provider values here.

## Capacity and recovery

Planning assumptions, not measurements: 150 tenants at month 36, ten active
relationships per tenant, 100 orders per relationship/year and five shipments per
order imply 450,000 orders and 2.25 million shipments over three years. A 50-row
order page is about 20 KiB; a typical shipment page is about 60 KiB. Unicode/JSON escaping can be larger;
encoded upper-bound fixtures still need measurement. Plan for 50
peak sharing reads/s, read-only, 99.9% monthly availability and p99 below 500 ms.
A two-page interaction is independently authorised rather than holding a human
transaction open. Existing five-second final owner fences bound disclosure.
Measure representative plans and hosted timing before activation; tiny fixtures
are not load evidence. Added cost is one B-tree over existing order facts, not a
new service, worker, datastore or master. The schema rollout is additive; a code
rollback leaves the SELECT purpose/index unused and preserves all evidence.

## Acceptance and remaining work

Observe red then green for boundary and SDK assertions; compile/build locally
without an app/database/container. Hosted PostgreSQL verifies both owners, exact
named/group positive authorisation, other buyer/relationship/outlet denial,
revocation/rehire, stale graph/owner failure, audit rollback, cursor isolation,
private-table and write denial, and frame restoration. Built-browser acceptance
must exercise the real chooser, order and shipment reads and loss of current
authority; inspect retained synthetic evidence. No API registration, mock admission,
repository fixture or healthy deployment proves these current-authority gates.

Catalogue disclosure and source-issued invoice publication are separate consumers
of their closed templates and remain part of the wider requested supplier work.
Do not relabel a hospital payable as a source invoice, invent GST/ITC/GL treatment,
or imply this shipment read completes those consumers. R2 return/credit acceptance
and the platform reverse-acknowledgement boundary keep ADR-0075's existing scope.

Shipment sharing is restricted to the exact source/hospital-approved terms hash.
The `:terms` path segment is that canonical SHA-256, supplied by an authorised
chooser rather than entered by a user. It binds the sealed cursor; every query
also checks the owner order's original terms hash against the final current grant.
A changed agreement requires a refreshed choice/page. It does not broaden access
to another agreement's history; hospital-local retained obligations use their own
existing authority. Public projections include that `terms_sha256` for exact SDK
checking, without the private source document or its actors.

The native protocol/storage fixture now repeats for both `supply` and `pharmacy`
source product contracts. Pharmacy intent uses its versioned catalogue identity;
warehouse intent uses its formulation-version identity. A pure contract check was
observed rejecting the previously hardcoded warehouse intent for a pharmacy
agreement before correction. This expands retained-projection tests only: it does
not establish real current grant/Staff/graph admission, licence activation,
physical stock dispatch or browser acceptance for either owner.

## Current owner names for the source chooser

Read organisation and premise names only through each physical owner's local
endpoint-presentation guard after the current licence, policy and premise check.
The platform dispatches to that owner; SupplyConnect does not query pharmacy
master tables. IAM supplies the verified source endpoint fact descriptor with its
held grant;
SupplyConnect uses the owner presentation port inside that same guarded callback
before releasing shipment projections. The two canonical text fields are bounded
at 200
characters; invalid or missing labels withhold the new sharing projection rather
than substitute identifiers. Labels are presentation facts, never grant, graph,
permission or stock authority.

Keep the existing machine source-check request/reference wire unchanged: its
strict v1 clients must not receive unexpected metadata during a rolling deploy.
The presentation port is local and optional to the older fact-check interface;
shipment sharing requires it. Existing fact checks retain their eligibility
semantics. Reuse the source rows and locks already checked by the owner, restore
its private purpose before the callback, and audit the final sharing read.

At the existing 50 sharing reads/s planning peak this adds a bounded local owner
fact read, no service, cache, datastore, migration or infrastructure allocation.
The prior 500 ms p99 target and five-second held source deadline still apply;
measure the added read in hosted acceptance. Two 200-character labels repeated in
50 rows add roughly 40 KiB before JSON escaping if each character averages two
encoded
bytes. Go producer HTML escaping can add 120 KiB plus field syntax at the
upper name lengths. These are planning bounds, not measured
throughput or release evidence. Disabling the candidate leaves the old machine
protocol untouched and preserves historical stock and financial data.

## Shared source discovery

Reuse the current Staff hospital-choice port and add the hospital-owned IAM
`GET /iam/hospitals/:hospital/supplier-shipment-grants` directory under existing
`iam.supplier_grant.inbox.read` authority. The offer directory requires acceptance
authority and must not be reused by viewers. The new directory projects only
approved shipment-reader grant IDs, scope, relationship, exact terms, recipient
kind and creation time. It withholds reasons, signer identities and private basis.
These are hospital-local discovery metadata, not authority to disclose another
tenant's facts. Its transaction rechecks current hospital Staff and inbox authority,
commits a read audit and returns no-store pages capped at 50 with sealed
actor/hospital/resource cursors. No migration or new role permission is needed.
The chooser
resolves only approved shipment-reader records through a new singular
`GET .../partner-grants/:product/:grant/terms/:terms/shipment-source` resource.
This resource invokes the same full current compound grant and owner name guard
as the shipment lists and commits a disclosure audit. It returns only grant,
relationship, exact terms, scope and the two current source labels, including
when no published dispatch order exists. No query parameters or writes apply.

Keep the existing combined 50-read/s candidate planning budget, 99.9% availability
and 500 ms p99 target. A source response is under 3 KiB at the canonical bounds.
The chooser checks one inbox page at a time and at most five source resources
concurrently. Per-source denied/not-found records remain undisclosed; dependency
or account failures are explicit and cannot masquerade as an empty result. Use
bounded hospital-choice and resource-bound directory cursors; do not add a global cross-hospital table
scan, a new directory master, background job or infrastructure component. This
is a read-only extension under the same disabled capability; full real-grant
admission, UI/browser and live release acceptance remain required.
