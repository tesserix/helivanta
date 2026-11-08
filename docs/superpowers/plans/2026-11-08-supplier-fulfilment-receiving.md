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
