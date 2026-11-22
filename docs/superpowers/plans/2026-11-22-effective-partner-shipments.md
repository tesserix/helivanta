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

The continuation connects independent source Staff/account links and reviewed
operating duties to the same fixture, supplying the real `OperatingAuthorizer`.
The first real product-admission test reproduced an audit-shape rejection in
run `44978021214`. The additive audit migration and its focused database negative
matrix passed in run
[44978972536](https://github.com/tesserix/helivanta/actions/runs/44978972536),
along with the full native root under the race detector (`1045.633s`). Both
source products deny unreviewed duties and wrong organization/location/tenant
scope, then admit their own reviewed Staff through the actual physical-owner
adapter. Staff hold the baseline Staff role, not tenant-wide source administration.
This is current admission evidence, not a completed stock movement.

Dispatch-preflight regression-only run
[44978473013](https://github.com/tesserix/helivanta/actions/runs/44978473013)
reproduced an unsupported `receive` current-source operation. The caller now
uses the existing `dispatch` operation. Fixed run
[44979352134](https://github.com/tesserix/helivanta/actions/runs/44979352134)
passed the full native root under the race detector (`1001.301s`), including
both owner types' refusal without a buyer dispatch policy and success after
independent TEST policy approval. These canonical accepted-order fixtures isolate
preflight; public procurement and physical stock movement remain separate gates.

Still exercise public order acceptance, reservation and physical dispatch for each
stock owner, then consume the published shipment in hospital receiving,
discrepancy handling, quality release and financial records. The existing empty
shared-order assertions do not prove any of those steps. Public consent,
catalogue and source-issued invoice consumers and authenticated browser/live
acceptance also remain open.
