# Independent supplier order and fulfilment continuation

This is work in progress on `feat/supplier-orders-dispatch-20261108`. The candidate now includes owner APIs, forced-RLS storage, immutable command
recovery, directed exchange and SDK validation. Real-owner database/broker CI
acceptance is pending. Stock reservation, dispatch, receiving and financial
integration remain unimplemented. Do not consume an order snapshot as operating
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

The intended orchestration uses the existing Temporal worker for order waits,
reservation/release and stock-owner replies. Each owner commits its own immutable
command receipt, local stock effect, audit and outbox atomically. Order budget
permits must be persisted before stock commands; duplicate command/line identities
cannot allocate twice. A timeout after a stock commit leaves reconciliation
pending until the original owner receipt is recovered. No network call or
cross-module stock write belongs inside an order transaction. Source dispatch is
an attributable human-confirmed physical action; a published event or successful
reservation is not proof it happened. Hospital receipt records actual counted,
accepted, quarantined and discrepant quantities under the destination owner's
scope and retains the original source lineage.

Before every new commitment, recheck current hospital/supplier Staff authority,
relationship consent, exact order/line budget, source and destination evidence,
reviewed category/activity/jurisdiction policy, current product, local-date
expiry, safety holds and source-owner stock availability. An outage denies new
commitments. A suspended relationship does not erase already dispatched goods;
receiving/corrections need a separately bounded retained-transaction purpose,
current local authority and the exact established shipment. A viewing grant
cannot place or approve an order, reserve stock, dispatch goods or post receipt.

## Remaining implementation and verification

The owner APIs, permissions and reviewed Staff templates, forced-RLS storage,
directed consumers, Temporal activities, both stock owners' reservation/dispatch,
minimized effective-grant disclosure, SDK/UI and actual-owner/browser acceptance
remain implementation work. No endpoint or permission is declared approved here.
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
