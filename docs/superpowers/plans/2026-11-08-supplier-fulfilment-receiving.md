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
rollback/replay checks. This fixture still requires successful CI execution.

Still required before completing this slice:

- Real IAM/Workforce-positive and negative admission tests for independent and
  hospital-owned supplier staff, both owners' physical dispatch, stock-sale
  competition, broker replay/outage recovery, receiving and quarantine release.
- Independently reviewed receiving corrections with history, summary-version
  checks and stock-custody constraints.
- Scoped order/allocation/shipment discovery, strict workflow SDK, usable supplier
  and hospital workspaces, and real browser acceptance tests.
- Green final CI and local builds, merge, then separately recorded Kargo rollout
  and acceptance against the shared development environment.

Financial posting, payment and invoice integration remain outside this narrowed
fulfilment/receiving slice. This document does not assert regulatory approval.
