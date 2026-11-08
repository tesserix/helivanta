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
