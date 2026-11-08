# Independent supplier order and fulfilment continuation

This is work in progress on `feat/supplier-fulfilment-receiving-20261108`. The
candidate includes owner APIs, forced-RLS storage, immutable command recovery,
directed exchange, SDK validation and scoped web workbenches. Both stock owners
implement reservation and dispatch; hospital receiving includes corrections,
discrepancies, independent clearance and physical quarantine release. Final
real-owner database/broker CI and deployed browser acceptance remain pending.
Financial integration remains unfinished. An order snapshot is never operating
authority.

## Ownership and scope

SupplyConnect coordinates procurement/order consent. Its hospital-local order
copy belongs to the buyer tenant and exact hospital/outlet in the approved
relationship; its supplier-local copy belongs to the seller tenant and exact
supplying outlet or warehouse/depot. PharmaConnect owns every outlet stock
reservation, movement and receiving effect. SupplyConnect owns distributor stock
reservations and movements. Neither owner writes the other's stock tables.
Shared IAM owns user/group and operating-duty authority; Workforce owns current
Staff/account/posting eligibility. Drug Master remains the published wholesale
product/formulation owner; an outlet's private catalogue retains its own IDs and
versions. Financial owners retain prices, tax, invoices, payables and settlement.

`contracts/supplier-orders.json` and `generate-supplier-orders.mjs` are the
canonical models and product-version shapes. SupplyConnect product references
carry a product ID and published version UUID; PharmaConnect references carry
its private UUIDv7 product ID and numeric catalogue version. An order line retains
that owner's unit code and a positive integral count, without inventing a
cross-owner conversion or copying a product master. Lines have stable UUIDv7
identities, at most 100 per order, with at most one billion owner units per line.
These are technical bounds, not delegated financial limits.

The buyer records a PO reference and agreed source quote reference. A supplied
reference does not verify a quote or authorize a tax rate, price or payment.
Order review establishes willingness to fulfil the recorded quantities; financial
composition still needs the actual published source-document contracts.

## Consent and failure behavior being implemented

Preparation freezes exact current bilateral relationship terms, source/buyer
versions, category, expiry, line identities and units. Another authorized hospital
actor approves the order before a directed offer reaches the supplier. Another
authorized supplier actor accepts those same immutable lines. The source actor
cannot be the hospital signatory. Local preparation/review also binds the actual
server-verified Staff ID: another account for the preparing Staff member cannot
approve the order. Private local Staff IDs are retained only by that owner and
are absent from the directed exchange; system-created incoming records do not
invent a Staff identity. Actor/object/current source checks remain
mandatory in the eventual handlers; typed IDs and pure transition functions are
not those checks. The proposed user experience must expose current eligibility
failures, pending outcomes and original-command recovery without manufacturing
success.

Cancellation before publication is local and terminal. After publication the
buyer requests cancellation; it cannot assert that supplier reservations were
released. A cancellation arriving before approval leaves a terminal source
record, so an older approval cannot recreate the order. After source acceptance,
cancellation remains pending until the stock owner releases the remaining
reservations and acknowledges the outcome. Already dispatched physical goods
retain their dispatch and receiving/discrepancy history; they are not erased.
Approval signatures remain immutable evidence when another actor requests a
cancellation. Version and remote-epoch checks reject conflicting/reordered
commands, and unknown outcomes recover through original command receipts.

The current fulfilment design uses one source-owner transaction for each line
budget, stock effect, immutable command receipt, audit and outbox. The accepted
order owner provides an exact-purpose publication guard while that transaction
commits; it does not maintain a second stock budget. This supersedes the earlier
unimplemented reservation saga proposal. No network call or cross-module stock
write belongs inside the stock transaction. A future process extraction requires
a reviewed fencing protocol; the process-local guard is not an HTTP contract.

Cross-tenant shipment, receiving and reply projections use durable outbox and
idempotent consumers. A timeout recovers the original owner receipt; it does not
create another dispatch. Source dispatch is an attributable human-confirmed
physical action. Hospital receipt records actual counted, accepted, quarantined
and discrepant quantities under destination-owner authority with original source
lineage. Supplier replies annotate reconciliation without changing those facts.
Wider orchestration and financial acceptance remain separate open gates.

Before every new commitment, recheck current hospital/supplier Staff authority,
relationship consent, exact order/line budget, source and destination evidence,
reviewed category/activity/jurisdiction policy, current product, local-date
expiry, safety holds and source-owner stock availability. An outage denies new
commitments. A suspended relationship does not erase already dispatched goods;
receiving/corrections need a separately bounded retained-transaction purpose,
current local authority and the exact established shipment. A viewing grant
cannot place or approve an order, reserve stock, dispatch goods or post receipt.

## Remaining implementation and verification

The candidate implements order owner APIs, permissions and reviewed Staff
templates, forced-RLS storage, directed order consumers, both stock owners'
reservation/release/dispatch, hospital receiving and discrepancies, directed
supplier replies, receipt-bound physical release, strict SDKs and operational web
workbenches. Final CI/coverage, deployed browser acceptance, financial composition
and wider order orchestration acceptance remain open. This candidate does not
establish an approved live fulfilment workflow.
The application remains a modular monolith using existing owners and worker;
this introduces no new service or copied inventory/financial master.

Reuse the existing unmeasured 12/36-month planning assumptions: 50/150 tenants,
1,000/3,000 overall peak RPS, 80:20 reads/writes and typical 2–20 KiB payloads.
The proposed order request ceiling is 64 KiB with 100 lines; control-plane p99
below 500 ms and 99.9% availability remain targets requiring measurement, not
observed performance. External-owner calls need bounded deadlines before local
locks; Temporal persists waits/retries rather than keeping requests open. Partial
failure must be visible as pending/blocked with attributable recovery evidence.
Additive schema expansion retains accepted orders and physical facts on rollback;
there is no destructive reversal of completed stock movements. Existing database,
worker and outbox capacity must be measured before any additional resource/cost
claim. The simpler synchronous multi-owner transaction is rejected because it
would violate stock ownership and cannot survive process/network failure.

Initial participant scope remains India, Maharashtra and Odisha, with actual
licence, activity/category and qualified applicability review required before
live trade. No controlled-drug or cold-chain control is inferred from a category
label or a generic quality reason. Relevant contracts: supplier-trade,
supplier-access, operating-duties and supply-stock; acceptance IN-PH-02/03/04/05/
08/09/10 and ORG-AC. This document and the current source authorize no production
migration, rollout, physical shipment or financial action.

## Candidate owner APIs and authority contract

Hospital routes use
`/v1/supply/hospitals/{hospital}/departments/{department}/outlets/{outlet}/purchase-orders/{order}`.
PUT prepares immutable intent; GET reads current state; GET `proposal-receipt`
recovers the original actor-bound draft; PUT/GET `commands/{command}` records or
recovers the original decision. Source routes use
`/v1/supply/supplier-orders/{order}` with current state GET and PUT/GET
`commands/{command}`. Collection/discovery and workboard UI remain to be added;
these exact-resource APIs do not manufacture an available collection.

Hospital actions reserve `supply.purchase_order.read`, `.prepare`, `.review` or
`.cancel` together with **`workforce.purchase_order_actor.use`**. This is a new,
separately injected Workforce-owned nonclinical purpose, not a relaxation of the
reviewed governance/posting-only `workforce.staff_actor.use` interface. It shares
only the owner's private current employment/account-generation eligibility
implementation, enforces its own reserved permission and audit action, and does
not confer order or stock authority alone. Department ancestry and the exact
current hospital-owned pharmacy binding constrain new commitments. The three
closed department-only PO templates admit hospital establishments and no clinical,
stock or financial permission. Source acceptance/read require the separately
reviewed `supply.order_acceptor.v1` or `pharmacy.order_acceptor.v1` operating duty
at the exact owned stock location; ordinary tenant/admin roles are not fallback
operating authority.

New commands require the exact signed-in account/workspace headers before body
validation. Uncertain outcomes use GET recovery with the original command and
intent, not another write. Actual Staff IDs prevent alias-account self-review and
remain owner-private; they are not on the directed wire. Read/recovery rechecks
current local Staff/duty authority, while an expired or suspended relationship
does not erase the immutable local receipt. New approval/acceptance requires a
fresh bilateral proof and exact current source facts. Source acceptance also
checks product identity/version/unit through the real product owner, without
claiming stock availability. Publication fences remain on the outer command;
warehouse product checks are sequential to avoid nesting system transactions or
exhausting connection pools for a 100-line order.

Supply migration 0015 adds exact-purpose forced RLS, immutable input/proposal
receipts, versioned history and command identity. Pharma migration 0037 adds only
a narrow current-product read purpose, leaving catalogue writes unchanged. IAM
0042 adds department-only PO templates; 0043 adds source order duties. The exact
0041 SQL bytes are frozen as `operating_templates_v2.sql`. All earlier migration
SQL and template definitions remain immutable.

The bilateral order event carries only the agreed intent/terms and authenticated
signatory needed by the exact contracting party. Procurement notes and signatory
identifiers are explicitly privacy-classified; no private local Staff IDs,
patient data, balances or credentials are carried. The receiver validates actual
transport provenance, matching established relationship terms, remote epochs and
monotonic transitions. Cancellation after source acceptance remains pending until
stock-owner compensation exists; it never asserts release merely because an event
was received. The candidate exposes no successful reservation or dispatch result.

### Scoped fulfilment and receiving discovery

Buyer purchase-order pages use
`GET /v1/supply/hospitals/:hospital/departments/:department/outlets/:outlet/purchase-orders`.
Buyer shipment pages use the same base with `/supplier-shipments`.
Independent commercial source pages use
`/v1/supply/stock-owners/:product/organizations/:organization/locations/:location/supplier-orders`;
hospital pharmacy source pages use
`/v1/supply/hospitals/:hospital/departments/:department/pharmacies/:organization/outlets/:location/supplier-orders`.
Both source bases provide exact `/:order`, `/:order/commands/:command`,
`/:order/shipments` and `/:order/shipments/:shipment/receiving` routes.
Hospital source order acceptance and reconciliation use the separate department
pharmacy commercial reviewer template; physical stock authority remains separate.

Physical allocation pages belong to the actual stock owner: Supply party/premise
or Pharmacy pharmacy/outlet, with an explicit hospital/department variant for a
hospital-owned source. Each allocation collection is the exact registered
allocation path with `/:allocation` removed. Hospital receipt pages use
`GET /v1/pharmacy/hospitals/:hospital/departments/:department/outlets/:outlet/supplier-shipments/:shipment/receipts`;
correction history adds `/:receipt/corrections`. Original receipts remain unchanged.
The latest correction and current receiving summary describe effective receiving;
ordinary receipt disclosure does not permit another actor to retry its command.

These collections accept only `limit` (1–50) and `cursor`, reject unknown/duplicate
query values, and audit sensitive page reads. Cursor continuation binds the
verified subject, full route, all resource parameters and filters. Every page
rechecks current owner authorization; hospital source rows also recheck the current
Pharma department binding. Empty discovery is not proof of a grant or affiliation.
Clearance input/output are canonical `ReceivingClearanceInput` and
`ReceivingClearance`: `review_recorded` records independent review. Physical stock
release still uses the stock owner's quarantine-release command and safety guard.

The client implementation is in `packages/api/src/supplier-fulfilment.ts` and uses
the same-origin Global API with no-store and expected tenant/subject headers.
Its request tests check paths against the registered inventory. Discovery database
assertions, positive owner API tests and browser acceptance remain delivery gates;
these contracts alone do not establish a finished deployed workflow.

Stock selection for an accepted source line uses the physical owner's allocation
root with `/allocations/:allocation` replaced by
`/lines/:line/stock-choices`. Pharmacy also exposes
`/lines/:line/stock-choices/:stock/locations` for sellable shelves. Both use the
current reservation-purpose order proof, the owner's operating/hospital Staff
admission, owner RLS and audited resource-bound pagination (maximum 50). They do
not require inventory administration. `SupplierStockChoice` and
`SupplierStockLocationChoice` are generated from the canonical fulfilment schema.
Counts are observed free-stock hints after active holds, not reservation evidence;
actual reserve and dispatch commands retain current owner locks and validation.

Current source order acceptance access can be checked with
`GET {explicit-source-order-base}/:order/review-access`. This uses the same current
operating or hospital department permission and Staff admission as source
acceptance. It returns the canonical `SupplierOrderReviewAccess`, bound to the
verified subject, Staff identity and exact owner/order scope with an observation
time. It records an audited read, not an approval or a transferable capability.
The eventual review command still rechecks authority and all business references.

### Supplier receiving-response delivery

`ReceivingResponseInput` and `ReceivingResponseRecord` are canonical fulfilment
models. The existing PUT acknowledgement remains unchanged. Source response,
attributable audit and directed outbox work commit together; replay does not emit
another event. `helivanta.in.supply.supplier_receiving_response_exchange.v1` carries
the source-authored record to its exact buyer. Its receiver checks sender, buyer,
known order and dispatch, retains summary-version identity and rejects changed
replays. It has no authority to change buyer counts, stock, release or finance.

Source history is `GET {explicit-source-order-base}/:order/shipments/:shipment/receiving/responses`.
The exact `/:response` GET binds original actor and Staff identity for retry recovery.
Buyer history is
`GET /v1/supply/hospitals/:hospital/departments/:department/outlets/:outlet/purchase-orders/:order/shipments/:shipment/receiving-responses`.
Both collections have limit fifty, resource-bound cursors and sensitive-read audit.
Buyer admission includes current Staff, goods-read and Pharmacy department authority.
The record's `owner_tenant_id` identifies the supplier author; `buyer_tenant_id`
identifies the recipient projection. Readers must compare `summary_version` with
the effective hospital summary rather than presenting old supplier replies as
current confirmation. ADR 0067 records delivery, isolation and rollback decisions.

### Hospital receiving inspection choices

The shipment receiving root now declares bounded `GET /receiving/product-choices`,
`/receiving/quarantine-locations` and `/receiving/sellable-locations` pages.
Each read uses current hospital/department Staff authority, exact Supply shipment
proof and the destination Pharma owner transaction; it writes receiving read audit.
The limit is 50, and sealed cursors bind the caller and full shipment route.
Product candidates are active destination products with the shipment's exact unit
and pack size. These filters do not establish ingredient, strength or formulation
equivalence: the receiver must inspect the immutable shipment and destination
identity before confirmation. Location routes fix the stock status on the server.
The strict SDK rejects foreign tenant, outlet, pharmacy or shipment choices and
wrong-status locations. These APIs do not grant private catalogue/inventory editor
access or complete the hospital workbench and physical release acceptance.

Supplier-response migration write policy pins the exact destination tenant;
read visibility does not authorize cross-tenant inserts. The event privacy
classification explicitly records its commercial fields and restricted actor and
reason evidence. Native classification checks pass; full migration and broker
execution remain CI checks.

### Receipt-bound physical quarantine release candidate

`PUT /v1/pharmacy/hospitals/:hospital/departments/:department/outlets/:outlet/supplier-shipments/:shipment/receipts/:receipt/releases/:release`
uses current `ReviewReceiving` admission: goods-review and Pharma hospital-review
permissions, current Workforce Staff, department path and exact destination owner
binding. `ReceivingReleaseInput` supplies expected summary version, destination
sellable location, units and reason. The batch and quarantine shelf come from the
immutable receipt. The shipment must be closed, clearance must match its current
summary, and the physical reviewer must differ from the original recorder.
Cumulative release is bounded by that receipt's effective accepted quantity.

Pharma's ordinary release engine posts the release, both stock movements and
outbox entries with the new immutable receiving journal in one transaction.
Current source safety, product, tax, schedule, MRP, incident and expiry checks
remain required. Migration 0042 adds receipt-bound policies rather than private
inventory-editor grants. An unrelated ordinary release cannot be adopted by
reusing its ID. Retry binds original actor/Staff and exact input; GET at the same
path returns retained release evidence under current review authority. The SDK's
recovery additionally checks original account and frozen input. The journal is
physical evidence, distinct from `review_recorded` clearance. See ADR-0068.

Supplier responses now use `ReceivingSharedResponse` in directed delivery and
buyer history; private source Staff identity remains only in source records.
Source and buyer parsers remain strict. The received-response table has non-null
generated origin tenant/record provenance and immutable-history enforcement.

Receiving progress uses the canonical `ReceivingCurrent` envelope: `not_recorded`
requires a null summary, and `recorded` requires a verified summary. The SDK now
parses that owner response and returns the summary or null; malformed or foreign
scope envelopes fail closed. A current explicit Pharma hospital reviewer may read
receiving without the separate operator-use grant; fallback occurs only after an
explicit scoped denial, never when authorization is unavailable.

Product choices include an exact ingredient/strength/unit, formulation and route
inspection description from the owning catalogue. The bounded product child
`GET /receiving/product-choices/:product/batches` returns existing destination lots
for the shipment's exact lot number, active product, unit and pack. The SDK binds
these choices to that product and shipment; expiry, version and MRP remain visible
for inspection and owner validation. Existing batches retain their immutable facts.
Physical release validation and exact recovery-object route classification are
covered by native checks; fixture tax effectiveness now comes from the outlet's
current owner date rather than a fixed date that becomes invalid over time.

Receiving receipt history now includes bounded resource pages at
`/receipts/:receipt/clearances` and `/receipts/:receipt/releases`, each under exact
current shipment admission and original receipt proof, with audited reads and
caller/resource-bound cursors. The clearance page returns
`ReceivingClearanceEvidence` with the explicit `review_recorded` state; release
history contains actual committed release journals. These views preserve original
review actors, reasons and versions and never reinterpret clearance as stock movement.
The release movement RLS correlates fully qualified outer movement columns to the
receipt journal, including signed quantity and the exact source/destination shelf.
The owner integration test also rejects a forged mismatched movement with RLS
SQLSTATE 42501 before an existing source-reference uniqueness constraint.

Source `GET /:order/receiving-response-access` under each explicit independent or
hospital-pharmacy source root returns one current `SupplierOrderReviewAccess`
resource admitted with `supply.receiving_response.record`, current Staff and the
exact order scope. Its audit action is `supplier_receiving_response_access_read`.
It is distinct from `/:order/review-access`, which retains the source acceptance
purpose. Both return context evidence, not a transferable authorization token;
each subsequent command independently reauthorizes.

Hospital original-command recovery is distinct from ordinary receipt disclosure:
`GET /receipts/:receipt/creation-receipt` requires current recording purpose and
the original account **and** Staff identity. A different linked Staff identity on
the same account cannot adopt that proof. Ordinary `GET /receipts/:receipt` and
bounded receipt history retain authorized colleague access. Physical release GET
recovery likewise pins both original actor fields; ordinary release history is
separate. The SDK uses the dedicated original-proof endpoint and still verifies
frozen intent. Missing or foreign original proofs return 404.

Progress and individual receipt reads now explicitly audit
`pharmacy_receiving_summary_read`, `pharmacy_supplier_receipt_read`,
`pharmacy_supplier_receipt_recovery_read` and source
`supplier_receiving_summary_read`, in addition to existing list/read audit.
An audit failure does not yield a successful sensitive read.

The independent operating-authority guard preserves protected business callback
errors after validating its live duty. It does not reinterpret a missing order or
shipment as missing Staff authority, and it does not retry a failed business action
under another duty. Current-duty, current-identity, credential, source/location,
revocation and graph checks retain their fail-closed outcomes. The real IAM /
Workforce / OpenFGA source scenario covers hidden objects, denied duties, outage,
revocation, original recovery and once-only callback error propagation.
