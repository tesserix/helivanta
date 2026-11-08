# Supplier fulfilment and receiving implementation

1. Generate canonical owner reservation, dispatch and receiving models. Observe
   failing tests for order-line budgets and receipt totals, then implement rules.
2. Add the exact-purpose accepted/retained order publication guard. Add owner-local
   allocation/command history, forced RLS and warehouse availability. Extend the
   pharmacy movement guard and sale availability to protect reservations.
3. Implement current Staff/duty-gated owner reserve/release/dispatch APIs, immutable
   recovery, current source checks and atomic minimized shipment outbox.
4. Implement verified bilateral shipment exchange and destination-owner receiving,
   quarantine/custody, partial-count budget, closure/discrepancy and source response.
5. Add strict SDK and user workspaces through Global API; expose pending/expired,
   unavailable, original recovery and discrepancy evidence without fabricated data.
6. Run native unit/race tests, owner DB/broker and Global API tests in CI (no local
   apps/containers), generator/contract checks, lints/typechecks and forced local
   production builds. Remove generated build outputs, commit/push scoped checkpoints
   and merge after required gates. Record live acceptance separately from CI.

## Delivery evidence, 8 November 2026

The implementation is on `feat/supplier-fulfilment-receiving-20261108` in draft
PR #1632. It is not merged, deployed, or accepted end to end.

Implemented code covers owner-local allocation budgets and live holds, partial
physical dispatch/release, immutable original command recovery, current hospital
and operating-Staff admission, directed shipment projections, hospital quarantine
receiving and rejected custody, shortage closure, source discrepancy responses,
reviewed receiving clearance, and source-safety checks before release. Source
cancellation now calls the stock owner in the same order-consumer transaction;
its terminal acknowledgement follows release of undispatched commitments.
Dispatched quantities and original evidence are retained.

Local verification has passed the Go production build, affected vet/lint checks,
race tests for the fulfilment/order/trade rules and stock provenance validation,
121 template-history/access-inventory tests, API typechecking, the Staff-assignment
SDK tests, targeted native architecture checks, contract generation/checks, and
the shell production build. The generated shell build output was removed.
SQL and PL/pgSQL were also parsed without starting a database. Those parser checks
are not migration execution, RLS verification or integration acceptance.

CI has exposed and driven corrections to migration syntax, dynamic policy SQL,
explicit tenant-visibility policies, directed-projection provenance and stale
migration fixtures. All of these gates must pass on the final commit. The owner
allocation fixture now creates real warehouse receipts with independent identity
and quality review, and real released pharmacy batches; it includes cancellation
rollback/replay checks. Both owners’ allocation, original recovery and cancellation fixture passed CI at 198952a4. The receiving correction fixture then exposed a receiving-head audit trigger that assumed an id column; receiving heads use shipment_id. The owner-local audit trigger now preserves this identity, and the correction fixture asserts committed audit and outbox counts. Its latest CI rerun remains pending.

Still required before completing this slice:

- Real IAM/Workforce-positive and negative admission tests for independent and
  hospital-owned supplier staff, both owners' physical dispatch, stock-sale
  competition, broker replay/outage recovery, receiving and quarantine release.
- Successful CI and owner-API acceptance of the implemented independently reviewed
  receiving corrections, history, version and quarantine-custody constraints.
- Scoped order/allocation/shipment discovery, strict workflow SDK, usable supplier
  and hospital workspaces, and real browser acceptance tests.
- Green final CI and local builds, merge, then separately recorded Kargo rollout
  and acceptance against the shared development environment.

Financial posting, payment and invoice integration remain outside this narrowed
fulfilment/receiving slice. This document does not assert regulatory approval.

### Scoped discovery checkpoint

Buyer orders now have department/outlet pages; independent suppliers have exact
product/organization/location pages and exact order/command paths. Hospital-owned
pharmacy sources use explicit hospital/department/pharmacy/outlet paths, current
Staff/department permissions and Pharma owner governance for every returned order.
Allocation pages stay in the selected physical stock owner and order. Limits are
50; signed continuations bind subject, route, every resource parameter and filters.
Every page reauthorizes and records the sensitive list read. Commands continue to
reject query strings and recover immutable actor-bound evidence.

IAM 0046 adds a separate department pharmacy commercial reviewer for supplier
acceptance and receiving reconciliation. The fulfiller template retains physical
stock authority without commercial acceptance; the reviewer cannot move stock.
Historical template migrations remain unchanged.

At this checkpoint native cursor isolation, malformed source response, source
fixture, migration IDs and pagination/permission architecture assertions passed.
The template-history suite passed 107 tests and access-inventory suite 15 tests;
contract generators/checks, API typechecking, Go build/vet and affected lint passed.
Database/API discovery and allocation continuation assertions run in CI. Shipment
and receiving discovery, strict fulfilment client, usable workspaces and full
positive API/browser acceptance remain required. No merge or deployment is claimed.

### Shipment and receiving discovery/client checkpoint

The prior checkpoint's CI passed both stock owners' physical allocation/recovery,
reviewed receiving corrections with stock-custody rollback, bilateral grants,
quality and grant-projection isolation. Remaining CI was still running when this
entry was written. The scanner classified the public catalog identifier
`department-supplier-commercial-v1` as a generic key; the exact public identifier
now has a narrow allowlist entry. Native full-history redacted scanning passed.

Buyer shipment discovery reauthorizes every returned order and its current Pharma
department binding. Source shipment discovery uses the exact commercial order and
source organization/location under owner RLS. Hospital receipt and correction
pages retain original immutable evidence rather than rewriting earlier receipts.
IAM 0046 and Pharma 0041 are additive migrations. Forward Pharma 0041 indexes
receiving evidence for bounded continuations.

The strict fulfilment SDK now supports both owners' reservation, current state,
original recovery, partial dispatch/release and immutable command recovery;
hospital receiving, original receipts, correction history, closure, independent
clearance review and shipment/order discovery. A clearance is a review record;
physical quarantine release still belongs to the stock owner's existing command.
Clearance input and output now use the canonical fulfilment contract and generated
Go/Zod schemas instead of a local map/wire struct. Every mocked SDK request is
checked against the generated route inventory, which caught and corrected the
pharmacy prefix and nonexistent receiving creation-receipt endpoint.

Native SDK tests cover foreign scopes, both physical owner paths, hospital source
versus recipient identity, immutable input/actor/version recovery, false expiry,
duplicate/oversized pages, receiving/correction intent, closure and clearance.
Database discovery assertions and positive API/browser acceptance remain pending.
No new user workspace, merge or deployment is claimed by this checkpoint.

Validation for this client/discovery checkpoint: full API suite 107 files / 1,037
tests passed before the final two precision/cache regressions; focused fulfilment
suite 15 tests passed afterward. API/shell typechecks, scoped ESLint, Go build/vet,
scoped golangci-lint, resource-query native regression, schema/route generators and
access inventory checks passed. The shell production build passed with the same
`API_URL=http://127.0.0.1:8081` rewrite configuration used in CI; no local server
was started. Generated `.next` and shell TypeScript build cache were removed.
The supplier CI job at 60a4a296 completed successfully, including the current
hospital Staff/membership-cutoff fixture. New discovery DB assertions still await
this checkpoint's CI. The repository remains public.

### Explicit source client and RLS gate follow-up

CI migration startup correctly rejected a second permissive source-shipment policy.
The source-read branch is now in the table's single tenant-isolation policy, with
the original exchange-only write check (exact order, remote buyer origin and system
shipment actor) preserved. This fix is pushed as bb5e64f4; its integration rerun is
pending. The gate was not weakened or waived.

Commercial source reads, acceptance and immutable command recovery now use the
explicit product/organization/location or hospital/department/pharmacy/outlet path.
The source scope and cache key include hospital and department. Partial hospital
scope and hospital Supply-owner paths fail before network access. Stock workflows
reuse the same source-order root; actual physical stock routes remain in the owner.
The source client regression covers hospital source acceptance and recovery as well
as exact independent source lookup. Full API tests passed 107 files / 1,040 tests;
focused source/fulfilment tests passed 23 tests after command assertions were added.
API typechecking and scoped ESLint passed. No user workspace or rollout is asserted.

### Owner stock-selection prerequisite

Added exact accepted-order-line stock selectors, guarded by the same current
order/consent publication and physical-owner Staff admission as reservation.
Supply choices retain current released identity/quality facts for the exact source
product version; Pharmacy choices are sellable batches followed by bounded shelf
choices. Observed quantities subtract active holds. No inventory-admin permissions
were added. ADR 0066 records scope, sizing assumptions and failure behavior.

The native route regression failed before registration and passed afterward. The
SDK selector regressions failed before implementation; all 17 focused fulfilment
SDK tests and 1,042 full API tests now pass. Native Go build/vet, affected lint,
canonical generators, route inventory and pagination architecture checks passed.
New DB assertions use the actual owner selector adapters against both owners'
physical stock fixtures, including continuation and free stock after a 60-unit
hold. These await CI; they do not prove current Staff API admission or full dispatch.

A prior receiving boundary test failed because its gateway fixture registered no
Pharmacy upstream. d9cfe27f adds the real Pharmacy module/upstream while retaining
missing Workforce/department owners for its intended unavailable-owner cases.
No assertion or authorization gate was weakened.

The selector DB regression at 34fb819a passed Supply and caught a Pharmacy defect:
`pharmacy_sellable_buckets` already returns quantities net of active supplier
holds. The new selector subtracted those holds again, hiding the remaining 40
units after reserving 60. Selectors now reuse the owner's net quantity directly;
the 100 → 40 stock/shelf assertions remain unchanged. Native build and affected
lint pass; the database rerun remains required.

### Source Staff workbench checkpoint

Source fulfilment is now reachable from warehouse operations and an independent
pharmacy's selected outlet. It uses real owner order/line/lot/shelf selectors,
source order acceptance/rejection, bounded allocation pages and partial dispatch
or hold release. The private inventory grant check is not used as source authority.
Hospital-owned pharmacy sources still need their department workbench.

Acceptance inputs are displayed only after a new exact-order source review-access
read passes current review-purpose Staff and operating/department permission checks.
It is a non-transferable observation, not approval; writes still reauthorize. The
canonical source review-access model and registered scoped routes are aligned.

The workbench freezes ID/input across uncertain responses, recovers original
receipts independently from live balances, blocks leaving unresolved actions,
and suppresses previous-scope records after access errors/scope changes. A first
owner invalid-input/current-state rejection refreshes data and permits an edited
new intent; a later rejection after an uncertain result keeps the original frozen
intent. Tests exercise lost reservation, partial dispatch and source acceptance
responses, duplicate clicks, read-only review denial, scope isolation and retry
rejection. The 59 affected screen tests, 1,043 API tests and 312 shared UI tests pass.
Native typechecks, scoped lint, Go build/vet and the shell production build passed
before the final retry-state follow-up; final validation remains required.

The previous full Go CI coverage gate reported Supply at 67% against its unchanged
70% floor. A new owner API scenario uses real IAM, Workforce, OpenFGA reviewed duty
activation/revocation and owner HTTP handlers for source pages, cursor binding,
immutable review recovery, dispatched shipment/receiving projections and supplier
responses. Only retained business records are seeded. The test also covers source
review versus fulfilment roles, graph outage and immediate revoke cutoff. It is
compiled/linted natively and awaits CI; no coverage waiver is introduced.

Source response replay is now bound to its exact order as well as response ID;
a wrong-order replay remains opaque rather than returning success for another
order's supplier acknowledgement. Buyer delivery of supplier responses is still
unfinished: the existing response handler persists only the source annotation.
It needs a durable owner event/projection and authorized buyer read before full
collaboration can be claimed. Hospital receiving selectors, workbench, purpose-
scoped physical quarantine release and full positive API/browser flows also remain.

The source workbench's confirmation copy distinguishes an original recorded
operation from current availability. It does not describe an expired or later
changed allocation as currently reserved merely because recovery succeeded.
A post-uncertainty rejection cannot clear the original intent. The new current
review-access endpoint prevents an ordinary source reader from entering an order
acceptance flow without verified review-purpose admission.

Final validation for the source workbench checkpoint: native shell production
build passed with the CI API rewrite origin; `.next` and shell TypeScript build
cache were removed. All 59 affected screen tests, 1,043 API tests, 312 shared UI
tests, source/shell typechecks, affected ESLint, Go build/vet/golangci-lint,
canonical generators, route documentation, access inventory and collection-route
architecture assertions passed. The real source Staff API scenario compiles and
lints; its database runtime and coverage contribution await CI. No local app or
container was started, and no merge/deployment is claimed.

### Durable supplier-response checkpoint

Added canonical response input/record and generated event payload, atomic source
outbox publication, authenticated directed buyer consumer and immutable buyer
projection, current buyer/source scoped read pages and original-actor/Staff exact
recovery. Source response acknowledgement shape is preserved. Forward Supply
migration 0017 adds source event identity, buyer projection and indexes; tables
have one tenant-isolation policy with exchange-only writes. The source event is
registered for publishing, directed delivery and receiving-table allowlisting.
Supplier replies do not alter hospital counts, stock, release or finances.

The native envelope regression failed before implementation and now passes for
sender/recipient binding, strict fields and invalid response semantics. New
protocol DB checks cover delivery replay/new-envelope replay, changed-body
rejection, pagination and wrong-department/tenant disclosure. The real Staff API
fixture checks source outbox commits once with replay. It also needed its seeded
receiving projection's required source_event_id. Those DB assertions await CI.
The strict SDK covers source/buyer response scopes, unchanged PUT acknowledgement,
summary bounds and actor-bound original recovery. Native full API tests and Go
build/lint pass; this is not full receiving-workspace or deployed acceptance.

Response checkpoint validation: 1,044 full API tests passed; native sender/recipient
regression passed with race instrumentation; affected Go packages compiled, vetted
and linted; contractdocs and canonical generators/checks passed; shell production
build passed with the CI rewrite origin and generated output was removed.
Source/buyer projection DB assertions are not claimed passed until CI runs them.

The previous Go job exposed two test-fixture issues before measuring the expanded
coverage: the real owner rejects an unassigned Staff caller with 403 order_forbidden
rather than the fixture's expected 404; and an intentionally disabled projection
could exhaust JetStream retries while denial was inspected. Expectations now match
purpose denial, and recovery explicitly enqueues the same duty/version as durable
replay after restoring the graph. Activation still requires the exact graph ack;
retry budgets, production authority and coverage thresholds are unchanged.
