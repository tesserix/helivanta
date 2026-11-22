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
passed: fresh Linux race compilation, contract generation/check, native audit
and the complete root (`1130.74s`). Actual receiving, closure, clearance and
release passed (`8.78s`), with the recorded eight/one/one stock outcome. Draft
[PR #1811](https://github.com/tesserix/helivanta/pull/1811) retains the change;
protected checks and live acceptance remain required.

Still exercise warehouse reservation/dispatch and consume the published shipment
through completed hospital receiving,
discrepancy handling, quality release and financial records. The existing empty
shared-order assertions do not prove those steps; actual Pharmacy dispatched
sharing is recorded above. Public consent,
catalogue and source-issued invoice consumers and authenticated browser/live
acceptance also remain open.


## Both-owner continuation: candidates awaiting runtime evidence

The following committed branches build on the verified Pharmacy source/receiving
journey. Their hosted runs are pending; the test descriptions below are intended
assertions, not passing or deployed behavior.

| Candidate | Hosted run | Intended additional evidence |
| --- | --- | --- |
| `test/supplier-warehouse-fulfilment-runtime` (`a1625687`) | [44987139551](https://github.com/tesserix/helivanta/actions/runs/44987139551) | Odisha source, published Drugmaster reference, ten packs explicitly verified as one hundred base capsules, independent source quality duty, actual reservation and split dispatch |
| `test/supplier-warehouse-receiving-runtime` (`2e0bbede`) | [44987469985](https://github.com/tesserix/helivanta/actions/runs/44987469985) | Maharashtra hospital receives both stock owners; warehouse shipment retains absent MRP while the hospital records inspected printed MRP; same discrepancy and independent physical-release checks |
| `test/supplier-receiving-response-runtime` (`f00f1eda`) | [44987593627](https://github.com/tesserix/helivanta/actions/runs/44987593627) | Source reads actual closed receiving, refuses stale versions, confirms/disputes with current Staff evidence, recovers original commands and delivers a private-Staff-minimized hospital projection without changing counts or asserting financial effects |
| `test/supplier-receipt-finance-runtime` (`bb9bd3ab`) | [44987677605](https://github.com/tesserix/helivanta/actions/runs/44987677605) | Actual accepted-stock acquisition choices, receipt-linked bills, duplicate receipt billing refusal, partial payment, credit, payment reversal, exact balances and unchanged physical stock |
| `test/supplier-public-relationship-runtime` (`3d143874`) | [44987872897](https://github.com/tesserix/helivanta/actions/runs/44987872897) | Public source proposal, independent approval, hospital acceptance, directed acknowledgements, matching terms/epochs, same-signatory denial and replay/conflicts |
| `test/supplier-public-grants-runtime` (`d054bc01`) | [44988074056](https://github.com/tesserix/helivanta/actions/runs/44988074056) | Public named/group source grants and current independently reviewed hospital recipient bases instead of seeded consent in the composed journey |

The warehouse source is an independent Supply owner in Bhubaneswar, Odisha; the
Pharmacy source and hospital destination remain in Maharashtra. Policies, licences,
prices, tax inputs, counterparties and payments are marked synthetic TEST evidence,
not regulatory certification or real bank instructions. Drugmaster publication
uses its existing independently staged/approved owner component data fixture;
its actual public reference API and stock-reference guard are then used. This is
not public master-catalogue authoring acceptance.

Hospital stock and operational payables remain Pharma-owned. The financial tests
use the independently authorized destination owner, not the goods receiver's
permission as finance authority. Supply source stock stays Supply-owned, Pharmacy
source stock stays Pharmacy-owned, and directed replies cannot mutate hospital
counts. External identity, workload transport and object-storage protocols remain
synthetic boundaries in the hosted native test; owned authorization, database,
stock, event and financial implementations remain actual.

Source-issued invoice and catalogue disclosure consumers are still missing, even
though the closed grant templates exist. Actual public consent, both-owner physical
and financial runtime results, required protected checks, specifically approved
main/Kargo/migration rollout, effective feature flags and authenticated deployed
browser tests remain completion gates. Do not enable supplier collaboration or
close the umbrella issues from this checkpoint.


### Observed warehouse and public-consent failures

The first warehouse runs `44987139551` and `44987469985` stopped on an
incorrect negative-test status: `wholesaleStockError` deliberately returns 404
for `ErrScopedCommandDenied`, hiding the receipt from an actor without quality
review authority. The test now expects that exact denial; product authorization
was not changed.

Corrected run [44988340105](https://github.com/tesserix/helivanta/actions/runs/44988340105)
passed real order acceptance, source intake, explicit pack/base identity checks,
independently assigned quality release and stock discovery of one hundred base
units. Reservation then returned 503 with a reproducible UUID scan error:
GORM treated a bare `uuid.UUID` scan destination as an array of bytes. The owner's
`Lock` now scans an `ID` field in a row struct, preserving the exact tenant,
organization, premise, receipt and `FOR UPDATE` query and the existing missing-row
result. The public regression also checks that a nonexistent receipt returns 404
and does not prevent the valid reservation using the same command identity.
The fix is committed as `a6125b98` and propagated through every continuation
branch. Hosted [run 44989322954](https://github.com/tesserix/helivanta/actions/runs/44989322954)
validates the fixed warehouse journey; its runtime result is still pending.

The first public relationship/grant candidates stopped at warehouse contract
review before the grant journey. A licence-compliance grant is insufficient for
`WithTradingAdministration(..., write=true)`, which requires the exact party's
administrator authority. The test retains this 404 denial, then has the current
source owner explicitly assign that authority to the independent source
signatory through the actual access API. Fulfilment Staff receive no such
administration grant. Public relationship retry `44988901751` and public grant
retry `44988930145` are pending; they started before the later receipt-scan fix.
The corrected finance retry `44988940713` also started before that scan fix.
A downstream failure in those older runs must be attributed to its actual first
error, not counted as executed financial or complete consent acceptance.

Receiving PR #1811 now has passing browser E2E, web, supplier, API-client, source
presentation, Staff-query-plan and compound-admission checks; its broad Go gate
remains running. Nothing here establishes an approved main/Kargo/migration rollout
or deployed acceptance.


Public grant retry [44988930145](https://github.com/tesserix/helivanta/actions/runs/44988930145)
subsequently passed the actual Supply named-recipient (`6.74s`) and group-recipient
(`6.20s`) public grant subtests. These follow the public source/hospital relationship
approvals and retain current Staff, recipient-basis, source, graph and audit
checks. The run then stopped on the older warehouse reservation UUID scan bug;
it is not a passing full root or Pharma public-consent result. The complete retry
[44989863836](https://github.com/tesserix/helivanta/actions/runs/44989863836) includes
both the explicit warehouse contract authority and the receipt-scan fix.
