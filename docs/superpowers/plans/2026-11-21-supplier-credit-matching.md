# Supplier-credit matching delivery

Spec: [matching and reversal](../specs/2026-11-21-supplier-credit-matching.md).

1. Capture hosted missing-endpoint baseline for partial/combined matching.
2. Generate canonical Go/Zod models; red/green explicit-input normalization.
3. Add immutable matching/reversal schema, exact scope/amount/concurrency guards
   and extend deferred projection proof; register additive migration0059.
4. Wire actor-bound commands/history/source choices with atomic audit/outbox.
5. Classify routes, regenerate contracts/inventories and run architecture gates.
6. Prove native partial/combined/excess/reversal/recovery/isolation/concurrency and
   rollback behavior; add SDK checks against canonical models.
7. Complete matching/allocation finance UI and real Global API/browser journeys
   with the dependent allocation work. Keep incomplete branches draft.
8. Run local lint/types/tests/builds without apps/databases, remove generated
   outputs, pass required hosted checks, merge and request named live rollout
   approval. Record actual live/release acceptance before closure/activation.
