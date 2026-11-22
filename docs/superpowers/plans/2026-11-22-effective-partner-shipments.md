# Effective partner shipment implementation

Design: [ADR-0076](../../adr/0076-effective-partner-shipment-disclosure.md).
This is an execution plan, not a completion record.

1. Add failing exact-scope/minimal-wire and actor/header tests. Extend the canonical
   supplier-access projections using source-owned field references; generate Go/Zod
   and add strict SDK scope, cursor, duplicate and abort/deadline acceptance.
2. Consume `PartnerScopes.WithPartnerScope` in SupplyConnect's two bounded reads.
   Add the isolated source SELECT purpose, dependent-table/private-field and write
   denial, exact owner/order predicates, audit and concurrent index. Preserve
   existing source/seller, buyer and exchange purposes without widening them.
3. Run hosted native owner/RLS tests and positive real IAM/Staff/graph admission,
   including both source products, named and group recipients and current cutoff.
   A synthetic admission fixture is component evidence only.
4. Implement current recipient grant discovery and human source labels through
   owning ports, then a reachable account-bound UI with named choices and no raw-ID
   entry. Test refusal conceals loaded evidence and context changes clear selection.
5. Commit/push, run focused hosted builds/checks, then pass protected hosted
   native/E2E/visual gates and integrate normally. Do not run local builds, apps,
   databases or container runtimes, including Colima. No admin/check bypass.
6. Prepare a concrete named release/fixture/activation request, deploy only with its
   required approval and record authenticated acceptance. Keep catalogue/invoice
   consumers and wider supplier acceptance explicit; do not close the whole goal
   from this shipment checkpoint.

## Composed owner-check checkpoint

The hosted native regression at `9c1f2830` reproduced SQLSTATE `42703` in
the hospital buyer eligibility check: `pharmacy_staff_department_bindings`
does not own a `pharmacy_id` column. Both the trading endpoint and hospital
department guard must resolve pharmacy ownership through `pharmacy_outlets`,
joining on tenant and outlet while retaining the exact hospital, binding,
department where applicable, approved status and binding-version predicates.
The corrected queries share-lock both binding and outlet rows. This needs no
schema, permission or route change.

`supplier_compound_department_test.go` exercises both real owner ports inside
the real IAM source-administration guard. It covers the current buyer and denial
for another organization, outlet, hospital, binding, stale binding version and
another department. The pre-fix run is
[44975782135](https://github.com/tesserix/helivanta/actions/runs/44975782135);
fixed-head validation is
[44976648133](https://github.com/tesserix/helivanta/actions/runs/44976648133).
The fixed-head run passed its fresh-cache compile and the full native root under
the race detector (793.695 seconds). Both owner-port matrices, named/group grants
for both source products, graph-outage refusal/recovery and committed-audit
refusal/recovery passed. The root also completed group suspension, pharmacy
licence withdrawal and warehouse evidence withdrawal checks. Relationship and
grant consent records remain state-machine fixtures; this is composed
current-authority evidence, not public-consent, physical fulfilment or deployed
browser acceptance.

The continuation now connects independent source Staff/account links and
reviewed operating duties to the same fixture, explicitly supplying the real
`OperatingAuthorizer`. Source Staff have the baseline Staff role and receive
source/outlet-scoped duties rather than tenant-wide product administration.
The first native run exposed a real fulfilment-audit constraint failure after
account and duty review. The forward audit migration and its database negative
matrix passed in run
[44978972536](https://github.com/tesserix/helivanta/actions/runs/44978972536);
the full both-owner Staff scenario also passed under the race detector
(`1045.633s`). Both source products denied unreviewed and wrong-scope duties,
then admitted their real reviewed Staff through the actual stock-owner adapter.
The fix and forward migration are in draft PR
[#1806](https://github.com/tesserix/helivanta/pull/1806); protected checks and
specifically approved migration/Kargo/live acceptance remain open.

Dispatch-preflight regression-only run
[44978473013](https://github.com/tesserix/helivanta/actions/runs/44978473013)
then reproduced an unsupported `receive` peer operation. The caller now uses
the existing `dispatch` operation. Fixed run
[44979352134](https://github.com/tesserix/helivanta/actions/runs/44979352134)
passed the full native root under the race detector (`1001.301s`), including
both source products' refusal without a buyer dispatch policy and success after
independent TEST policy approval. An order policy cannot substitute for this
activity. These canonical accepted-order records isolate preflight; physical
dispatch remains a separate gate. The fix is in draft PR
[#1807](https://github.com/tesserix/helivanta/pull/1807).

The separate public-order candidate
[44979749923](https://github.com/tesserix/helivanta/actions/runs/44979749923)
uses actual department-scoped hospital preparer/reviewer grants, source catalogue
creation, Global API procurement commands, directed order delivery and a separately
reviewed source order-acceptor duty. It checks that a fulfiller cannot approve
orders, command replay/conflicts and accepted-state delivery back to the buyer.
That full native root passed under the race detector (`854.157s`), including
the actual public hospital order/source acceptance journey (`1.74s`). Bilateral
relationship and sharing consent are still state-machine fixtures, not a proven
public consent journey. This test creates no reservation or physical dispatch.

Physical-flow test candidate
[44980499417](https://github.com/tesserix/helivanta/actions/runs/44980499417)
stopped at a fixture typo: the reviewer template is canonically
`supply.department_goods_reviewer.v1`. Corrected candidate
[44981591803](https://github.com/tesserix/helivanta/actions/runs/44981591803)
extends that public journey with owner-API catalogue classification, source
quarantine/receipt/independent stock release, a ten-unit reservation and split
six/four-unit dispatch. It checks availability, over-dispatch refusal, replay,
source shipment records, directed hospital shipment delivery and both named/group
disclosure of the actual published shipments. Hospital Staff receive explicit
department goods-receiver/reviewer templates; sharing grants remain read-only.
Composed fixture consent is bounded to five minutes and capped by membership and
relationship expiry; generic component-fixture and production expiry rules are
unchanged. Run `44981591803` reached actual owner stock creation, choices and a
ten-unit reservation, then stopped on another fixture assumption: stock allocation
operations return `200`, not `201`. Corrected candidate
[44982717640](https://github.com/tesserix/helivanta/actions/runs/44982717640)
asserts that existing response contract and supplies the harness's optional
current-source client before constructing relationship/fulfilment owners, as in
production. The peer handler and owner guards remain real; no source check is
bypassed. This is fixture parity, not a claimed production dependency failure.
Corrected run
[44983715570](https://github.com/tesserix/helivanta/actions/runs/44983715570)
passed the full native root under the race detector (`1081.241s`). The actual
public hospital order, source acceptance, stock setup, reservation and split
dispatch journey passed (`9.67s`). It retained both source publications, both
directed hospital shipments and both named/group disclosures, with exact stock
balances and replay/over-dispatch checks. Source projection is asynchronous;
the test waits at most twenty seconds for both committed dispatches, then checks
the actual records. This proves the Pharmacy source path, not warehouse physical
fulfilment, public consent, financial or live/browser acceptance.

The receiving candidate extends the same public journey with a separate named
physical-dispatch subtest and then actual destination commands. Buyer-owned
catalogue/classification/quarantine/shelf setup precedes counted receipts,
idempotent recovery, short/rejected closure, current independent clearance and
release. Tests compare immutable source MRP/lot evidence and actual stock-ledger
positions: ten dispatched units become eight accepted/released, one rejected and
one short. Rejected/short units never enter sellable stock.

The receiver intentionally also holds the scoped reviewer capability in this
fixture. Same-Staff clearance/release must still conflict; a different current
Staff reviewer succeeds. This verifies business independence beyond UI or a
missing permission. Release before closure/clearance and excess release must also
fail without changing stock. Source discrepancy acknowledgement and financial
posting remain separate unfinished steps. Run `44984160694` passed the physical
subtest (`3.89s`) and created the first hospital receipt, then stopped on a fixture
stock-query timestamp exceeding the owner's microsecond precision. Corrected run
[44985411548](https://github.com/tesserix/helivanta/actions/runs/44985411548)
is pending; receipt clearance/release is not yet proven.

Still exercise warehouse reservation/dispatch and consume the published shipment
through completed hospital receiving,
discrepancy handling, quality release and financial records. The existing empty
shared-order assertions do not prove those steps; actual Pharmacy dispatched
sharing is recorded above. Public consent,
catalogue and source-issued invoice consumers and authenticated browser/live
acceptance also remain open.
